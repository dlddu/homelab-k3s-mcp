package k8s

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"
)

// execMaxOutputBytes caps each of stdout and stderr at the source, before a
// response is built (prd-resource-generic AC12). `yes` never ends on its own,
// so a cap that only stopped buffering would leave the call hanging until the
// time cap — the byte cap has to stop the reader too.
const execMaxOutputBytes = 256 * 1024

// execMaxDuration is AC12's running-time cap. A command may outlive it in the
// pod; the tool's job is to answer, and an unanswered call helps nobody.
const execMaxDuration = 30 * time.Second

// limitedWriter copies into a bounded buffer and, once the cap is hit, keeps
// draining its input while marking itself full — the stream must be drained
// (its transport would wedge otherwise) but nothing past the cap is kept. The
// boolean travels beside the text so a response can say "this was cut" rather
// than silently shortening it, and onFull stops listening (AC12).
type limitedWriter struct {
	limit  int
	buf    bytes.Buffer
	full   bool
	onFull func()
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.full {
		return len(p), nil
	}
	space := w.limit - w.buf.Len()
	if space > 0 {
		if len(p) <= space {
			w.buf.Write(p)
			return len(p), nil
		}
		w.buf.Write(p[:space])
	}
	w.full = true
	if w.onFull != nil {
		w.onFull()
	}
	return len(p), nil
}

func (w *limitedWriter) String() string { return w.buf.String() }

// execServed answers the discovery question requireSubresource answers for
// scale, worded for exec — reusing it would have the refusal say "has no
// replicas", which is about scale, not exec. Discovery is not a resource
// permission (the scale precedent, AC8), and the sentence is about what the
// cluster serves, not about whether the object exists.
func (s *KubeService) execServed(ctx context.Context, gvr schema.GroupVersionResource, kind string) error {
	dc, err := s.discoveryClient()
	if err != nil {
		return err
	}
	list, err := dc.ServerResourcesForGroupVersion(gvr.GroupVersion().String())
	if err != nil {
		return APIError(fmt.Sprintf("discovery failed: %v", err))
	}
	want := subresourcePath(gvr.Resource, "exec")
	for _, r := range list.APIResources {
		if r.Name == want {
			return nil
		}
	}
	return apiErrorf(
		"%s does not serve an exec this layer can run: this cluster serves no %s. "+
			"This is not about permission, and not about whether the object exists",
		kind, want,
	)
}

// ExecResource runs command inside one named pod (the exec subresource) and
// returns its stdout and stderr separately. It is the coordinate-addressed
// half of exec; ExecInPod is the selector-addressed variant the exempt tool
// uses. The kind must serve an exec subresource — discovery answers that, not
// permission.
func (s *KubeService) ExecResource(ctx context.Context, ref ExecRef, container *string, command []string) (*ExecOutcome, error) {
	res, err := s.resolve(ctx, ref.APIVersion, ref.Kind)
	if err != nil {
		return nil, err
	}
	namespace, err := objectNamespace(res, ref.Kind, ref.Namespace)
	if err != nil {
		return nil, err
	}
	if res.gvr.Group != "" || res.gvr.Resource != "pods" {
		return nil, apiErrorf("%s does not serve an exec this layer can run: exec is the pods subresource (prd-resource-generic AC12)", ref.Kind)
	}
	if err := s.execServed(ctx, res.gvr, ref.Kind); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, execMaxDuration)
	defer cancel()

	execOpts := &corev1.PodExecOptions{
		Command: command,
		Stdout:  true,
		Stderr:  true,
	}
	if container != nil {
		execOpts.Container = *container
	}

	req := s.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(ref.Name).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(execOpts, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(s.config, "POST", req.URL())
	if err != nil {
		return nil, APIError(err.Error())
	}

	stdout := &limitedWriter{limit: execMaxOutputBytes}
	stderr := &limitedWriter{limit: execMaxOutputBytes}
	// The caps that stop reading also stop listening: a cut stream must end the
	// call, not keep it open until the time cap answers the same question.
	stdout.onFull = cancel
	stderr.onFull = cancel

	streamErr := executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: stdout,
		Stderr: stderr,
	})

	var exitCode *int32
	var timeLimited bool
	if streamErr != nil {
		var codeErr utilexec.CodeExitError
		switch {
		case errors.As(streamErr, &codeErr):
			code := int32(codeErr.Code)
			exitCode = &code
		case ctx.Err() != nil && errors.Is(ctx.Err(), context.DeadlineExceeded):
			// The time cap stopped the call, not the command itself. The
			// outcome is the answer "it ran past the cap"; an apiserver error
			// would read like the call was wrong.
			timeLimited = true
		default:
			return nil, APIError(streamErr.Error())
		}
	}

	return &ExecOutcome{
		Pod:             ref.Name,
		Stdout:          stdout.String(),
		Stderr:          stderr.String(),
		ExitCode:        exitCode,
		Success:         exitCode != nil && *exitCode == 0,
		StdoutTruncated: stdout.full,
		StderrTruncated: stderr.full,
		TimeLimited:     timeLimited,
	}, nil
}
