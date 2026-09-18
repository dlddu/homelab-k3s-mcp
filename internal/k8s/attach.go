package k8s

import (
	"context"
	"errors"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

// AttachDefaultReadSeconds and AttachMaxReadSeconds are AC13's read window.
// Attach has no natural end — the stream it joins outlives the call — so the
// window is what turns a stream into an answer, and the cap is what keeps one
// call from holding a connection open indefinitely. They are exported because
// the tool layer refuses an out-of-range window before the cluster is reached,
// and a second copy of the numbers is a second place for them to drift.
const (
	AttachDefaultReadSeconds = 5
	AttachMaxReadSeconds     = 30
)

// AttachResource joins the streams of the process already running in one named
// pod's container, collects whatever it says for readSeconds, and returns it
// (prd-resource-generic AC13). Unlike ExecResource it starts nothing: there is
// no command, no exit code, and the process keeps running after the window
// closes.
//
// stdin, when given, is written once right after attaching. AC13 says "한 번
// 써 넣는다" and means it literally — the reader is handed the payload and
// nothing else, so a process that reads a line gets that line and then EOF
// rather than a stream that stays open waiting for a second turn.
func (s *KubeService) AttachResource(ctx context.Context, ref AttachRef, container *string, stdin *string, readSeconds int) (*AttachOutcome, error) {
	if readSeconds <= 0 {
		readSeconds = AttachDefaultReadSeconds
	}
	if readSeconds > AttachMaxReadSeconds {
		return nil, apiErrorf("readSeconds must be at most %d (prd-resource-generic AC13)", AttachMaxReadSeconds)
	}

	res, err := s.resolve(ctx, ref.APIVersion, ref.Kind)
	if err != nil {
		return nil, err
	}
	namespace, err := objectNamespace(res, ref.Kind, ref.Namespace)
	if err != nil {
		return nil, err
	}
	if res.gvr.Group != "" || res.gvr.Resource != "pods" {
		return nil, apiErrorf("%s does not serve an attach this layer can run: attach is the pods subresource (prd-resource-generic AC13)", ref.Kind)
	}
	if err := s.streamSubresourceServed(ctx, res.gvr, ref.Kind, "attach"); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(readSeconds)*time.Second)
	defer cancel()

	attachOpts := &corev1.PodAttachOptions{
		Stdout: true,
		Stderr: true,
		Stdin:  stdin != nil,
	}
	if container != nil {
		attachOpts.Container = *container
	}

	req := s.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(ref.Name).
		Namespace(namespace).
		SubResource("attach").
		VersionedParams(attachOpts, scheme.ParameterCodec)

	attacher, err := remotecommand.NewSPDYExecutor(s.config, "POST", req.URL())
	if err != nil {
		return nil, APIError(err.Error())
	}

	stdoutBuf := &limitedWriter{limit: streamMaxOutputBytes}
	stderrBuf := &limitedWriter{limit: streamMaxOutputBytes}
	// A cut stream ends the window rather than waiting it out: past the cap
	// nothing more is kept, so staying attached only delays the answer.
	stdoutBuf.onFull = cancel
	stderrBuf.onFull = cancel

	opts := remotecommand.StreamOptions{Stdout: stdoutBuf, Stderr: stderrBuf}
	if stdin != nil {
		opts.Stdin = strings.NewReader(*stdin)
	}

	streamErr := attacher.StreamWithContext(ctx, opts)
	if streamErr != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, APIError(streamErr.Error())
	}
	// The window expiring is this tool's normal ending, not a failure. Exec
	// reports its deadline as timeLimited because a command was supposed to
	// finish; here the deadline *is* the contract, so there is nothing to flag.

	return &AttachOutcome{
		Pod:             ref.Name,
		Stdout:          stdoutBuf.String(),
		Stderr:          stderrBuf.String(),
		ReadSeconds:     readSeconds,
		StdinWritten:    stdin != nil,
		StdoutTruncated: stdoutBuf.full,
		StderrTruncated: stderrBuf.full,
	}, nil
}
