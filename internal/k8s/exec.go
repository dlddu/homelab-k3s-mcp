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

// streamMaxOutputBytes caps each of stdout and stderr at the source, before a
// response is built (prd-resource-generic AC12, and AC13 for the same reason).
// `yes` never ends on its own, so a cap that only stopped buffering would leave
// the call hanging until the time cap — the byte cap has to stop the reader too.
const streamMaxOutputBytes = 256 * 1024

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

// Bytes hands back what was kept without deciding it is text. The streaming
// tools that read command output want the string; port forward reaches whatever
// is listening on a port and has to look at the bytes before it can say whether
// they are text at all (AC14).
func (w *limitedWriter) Bytes() []byte { return w.buf.Bytes() }

// streamSubresourceServed answers the discovery question requireSubresource
// answers for scale, worded for the streaming subresources — reusing it would
// have the refusal say "has no replicas", which is about scale, not about
// exec or attach. Discovery is not a resource permission (the scale precedent,
// AC8), and the sentence is about what the cluster serves, not about whether
// the object exists.
func (s *KubeService) streamSubresourceServed(ctx context.Context, gvr schema.GroupVersionResource, kind, subresource string) error {
	dc, err := s.discoveryClient()
	if err != nil {
		return err
	}
	list, err := dc.ServerResourcesForGroupVersion(gvr.GroupVersion().String())
	if err != nil {
		return APIError(fmt.Sprintf("discovery failed: %v", err))
	}
	want := subresourcePath(gvr.Resource, subresource)
	for _, r := range list.APIResources {
		if r.Name == want {
			return nil
		}
	}
	return apiErrorf(
		"%s does not serve an %s this layer can run: this cluster serves no %s. "+
			"This is not about permission, and not about whether the object exists",
		kind, subresource, want,
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
	if err := s.streamSubresourceServed(ctx, res.gvr, ref.Kind, "exec"); err != nil {
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

	stdout := &limitedWriter{limit: streamMaxOutputBytes}
	stderr := &limitedWriter{limit: streamMaxOutputBytes}
	// The caps that stop reading also stop listening: a cut stream must end the
	// call, not keep it open until the time cap answers the same question.
	stdout.onFull = cancel
	stderr.onFull = cancel

	streamErr := executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: stdout,
		Stderr: stderr,
	})

	// Both writers have stopped by the time StreamWithContext returns, so
	// reading their flags here races with nothing.
	exitCode, timeLimited, err := execStreamOutcome(streamErr, ctx.Err(), stdout.full || stderr.full)
	if err != nil {
		return nil, err
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

// execStreamOutcome turns how the stream ended into what the response carries.
// It takes only values so the classification can be asserted without a cluster:
// the SPDY executor cannot be reached from a unit test, which is why this
// decision went unwatched long enough for a real round trip to be the thing
// that caught it.
//
// The stream reports a non-zero exit as a CodeExitError and says nothing at all
// about a zero one — so a nil error is the success path, and reading the code
// only out of the error leaves it unset exactly when the command worked
// (prd-resource-generic AC12: the response's success field describes the run).
//
// capped says a byte cap filled. That cap cancels the context to stop
// listening, so the stream comes back cancelled rather than past its deadline;
// treating that as a transport failure would turn AC12's "the response says it
// was cut" into an error instead of an answer. The exit code stays unset
// because a command cut mid-stream never reported one.
//
// The order below is deliberate: every branch that answers correctly today
// keeps its place, and the two new ones only catch what currently answers
// wrongly. A cut stream with no error already reported no exit code, so
// requiring !capped for the success branch leaves that case exactly as it was.
func execStreamOutcome(streamErr, ctxErr error, capped bool) (exitCode *int32, timeLimited bool, err error) {
	var codeErr utilexec.CodeExitError
	switch {
	case streamErr == nil && !capped:
		zero := int32(0)
		return &zero, false, nil
	case errors.As(streamErr, &codeErr):
		code := int32(codeErr.Code)
		return &code, false, nil
	case errors.Is(ctxErr, context.DeadlineExceeded):
		// The time cap stopped the call, not the command itself. The outcome
		// is the answer "it ran past the cap"; an apiserver error would read
		// like the call was wrong.
		return nil, true, nil
	case capped:
		// The byte cap cancelled the stream on purpose. The bytes that fit are
		// still the answer, and the truncation flags carry the rest of it.
		return nil, false, nil
	default:
		return nil, false, APIError(streamErr.Error())
	}
}
