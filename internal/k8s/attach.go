package k8s

import (
	"context"
	"errors"
	"io"
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
// (prd-resource-generic AC13).
//
// stdin, when given, is written once right after attaching — and then the read
// half stays open for the rest of the window, which is what attachStdinReader
// is for. AC13 contracts both halves at once, and a reader that ends at the
// payload makes the second half cancel the first.
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
		// done is the window's own context, so the payload outlives the write
		// exactly as long as the read does — and a byte cap that cancels early
		// releases the reader with it instead of leaving a goroutine parked.
		opts.Stdin = &attachStdinReader{payload: []byte(*stdin), done: ctx.Done()}
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

// attachStdinReader yields the payload once and then withholds EOF until done
// closes, which is how AC13's two halves — "collect output for readSeconds" and
// "write stdin once right after attaching" — can both be true at the same time.
//
// Handing the stream a reader that ends at the payload makes the second half
// cancel the first. client-go closes the remote stdin stream as soon as its copy
// finishes, the API server reads that close as the attach ending, and the window
// is gone: the same pod at readSeconds=3 stayed attached 3.00s without stdin and
// 0.016s with it, returning an empty stdout as a *success* (isError:false,
// stdinWritten:true), so no assertion counted it. Waiting out the window here
// keeps "write it once" literally true — the payload is still yielded exactly
// once, in order, and never re-sent. What changes is only what the target
// process sees afterwards: silence instead of EOF, and neither AC13 nor the PRD
// contracts EOF.
//
// It is a named type rather than a closure over StreamOptions because the
// regression has to be assertable without a cluster. The SPDY executor cannot be
// reached from a unit test, so a decision left inside AttachResource goes
// unwatched until a real round trip catches it — which is exactly how this one
// survived two assessments (the execStreamOutcome precedent).
type attachStdinReader struct {
	payload []byte
	off     int
	done    <-chan struct{}
}

func (r *attachStdinReader) Read(p []byte) (int, error) {
	if r.off < len(r.payload) {
		if len(p) == 0 {
			return 0, nil
		}
		n := copy(p, r.payload[r.off:])
		r.off += n
		return n, nil
	}
	<-r.done
	return 0, io.EOF
}
