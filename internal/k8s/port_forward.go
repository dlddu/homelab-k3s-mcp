package k8s

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// PortForwardDefaultReadSeconds and PortForwardMaxReadSeconds bound the half of
// the round trip this layer cannot predict. Writing the payload takes as long as
// it takes; waiting for whatever answers on the other side does not end on its
// own, because a port is not a command — nothing closes it when it is done. They
// are exported for the reason AttachDefaultReadSeconds is: the tool layer refuses
// an out-of-range window before the cluster is reached, and a second copy of the
// numbers is a second place for them to drift.
const (
	PortForwardDefaultReadSeconds = 5
	PortForwardMaxReadSeconds     = 30
)

// PortForwardResource opens a tunnel to one port of one named pod, writes the
// payload once, reads the answer inside the caps, and closes the tunnel
// (prd-resource-generic AC14).
//
// What this does not do is bind a local port. client-go's portforward.PortForwarder
// listens on localhost and proxies, which is the right shape for a persistent
// tunnel and the wrong one here: it would put a listener in this process's network
// namespace for the duration, reachable by anything else in the pod, to carry a
// single round trip this function already holds both ends of. The SPDY streams are
// opened directly instead — the same protocol the forwarder speaks, minus the
// listener.
func (s *KubeService) PortForwardResource(ctx context.Context, ref PortForwardRef, port int, payload []byte, readSeconds int) (*PortForwardOutcome, error) {
	if readSeconds <= 0 {
		readSeconds = PortForwardDefaultReadSeconds
	}
	if readSeconds > PortForwardMaxReadSeconds {
		return nil, apiErrorf("readSeconds must be at most %d (prd-resource-generic AC14)", PortForwardMaxReadSeconds)
	}
	if port < 1 || port > 65535 {
		return nil, apiErrorf("port must be between 1 and 65535 (prd-resource-generic AC14); %d was asked for", port)
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
		return nil, apiErrorf("%s does not serve a port forward this layer can run: portforward is the pods subresource (prd-resource-generic AC14)", ref.Kind)
	}
	if err := s.streamSubresourceServed(ctx, res.gvr, ref.Kind, "portforward"); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(readSeconds)*time.Second)
	defer cancel()

	req := s.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(ref.Name).
		Namespace(namespace).
		SubResource("portforward")

	transport, upgrader, err := spdy.RoundTripperFor(s.config)
	if err != nil {
		return nil, APIError(err.Error())
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", req.URL())
	conn, _, err := dialer.Dial(portforward.PortForwardProtocolV1Name)
	if err != nil {
		return nil, APIError(err.Error())
	}
	// Closing the connection is what makes the "does not stay open" part of AC14
	// true in code rather than only in the prose: every path out of here, including
	// the failures below, drops the tunnel.
	defer conn.Close()

	outcome, err := roundTrip(ctx, conn, port, payload, readSeconds)
	if err != nil {
		return nil, err
	}
	outcome.Pod = ref.Name
	return outcome, nil
}

// roundTrip is the part of the call that speaks the port-forward protocol, split
// out so the tunnel's lifetime stays visible in one place above and so a test can
// drive it over an in-memory connection.
//
// The protocol wants two streams per port carrying the same request id: an error
// stream the apiserver writes a per-port failure to, and a data stream. The error
// stream has to be created first — the apiserver pairs the data stream with an
// already-registered error stream, and a data stream that arrives alone is reset.
func roundTrip(ctx context.Context, conn httpstream.Connection, port int, payload []byte, readSeconds int) (*PortForwardOutcome, error) {
	headers := http.Header{}
	headers.Set(v1PortHeader, strconv.Itoa(port))
	headers.Set(v1RequestIDHeader, "0")

	headers.Set(v1StreamTypeHeader, v1StreamTypeError)
	errorStream, err := conn.CreateStream(headers)
	if err != nil {
		return nil, APIError(fmt.Sprintf("port forward error stream could not be opened: %v", err))
	}
	// Nothing is ever written to the error stream from this side, and the
	// apiserver waits for that half to close before it considers the port done.
	errorStream.Close()

	headers.Set(v1StreamTypeHeader, v1StreamTypeData)
	dataStream, err := conn.CreateStream(headers)
	if err != nil {
		return nil, APIError(fmt.Sprintf("port forward data stream could not be opened: %v", err))
	}

	errCh := make(chan string, 1)
	go func() {
		// A refused or unserved port is reported here, not by the stream
		// creation above: the apiserver accepts the streams and then says what
		// went wrong, so a silent empty response would otherwise be this call's
		// answer for "nothing is listening on that port".
		message, readErr := io.ReadAll(errorStream)
		if readErr != nil || len(message) == 0 {
			errCh <- ""
			return
		}
		errCh <- string(message)
	}()

	if len(payload) > 0 {
		if _, err := dataStream.Write(payload); err != nil {
			return nil, APIError(fmt.Sprintf("port forward payload could not be sent: %v", err))
		}
	}
	// Closing the write half is what tells the far side the request is complete.
	// Without it a server that reads until EOF — which is most of what one reaches
	// over a port forward — never starts answering, and the read below would spend
	// the whole window waiting for a reply that is waiting for this.
	dataStream.Close()

	body := &limitedWriter{limit: streamMaxOutputBytes}
	readCh := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(body, dataStream)
		readCh <- copyErr
	}()

	select {
	case <-readCh:
		// The far side closed, or the cap stopped the copy. Either way the
		// answer is whatever arrived; a read error after a close is how a
		// closed tunnel reads, not a failure to report.
	case <-ctx.Done():
		// The window expiring is an ending, not an error: AC14 caps the read
		// precisely because the far side is under no obligation to hang up.
	}

	if message := <-errCh; message != "" {
		return nil, apiErrorf("port %d on the pod refused the forward: %s", port, message)
	}

	return newPortForwardOutcome(port, payload, body, readSeconds), nil
}

// Port-forward stream headers. client-go spells these out in corev1 constants
// that are not exported together, and the protocol names them once here rather
// than at each of the three call sites that set them.
const (
	v1PortHeader       = "port"
	v1RequestIDHeader  = "requestID"
	v1StreamTypeHeader = "streamType"
	v1StreamTypeError  = "error"
	v1StreamTypeData   = "data"
)

// newPortForwardOutcome decides how the answer is carried. A port forward reaches
// whatever is listening, and plenty of what listens does not speak UTF-8 — Go's
// JSON encoder replaces invalid bytes with U+FFFD without saying so, which would
// hand back a corrupted body that looks like a body. So: valid UTF-8 travels as
// itself, anything else travels base64-encoded, and the response says which, so a
// caller never has to guess whether a replacement character was in the bytes or
// put there by this server.
func newPortForwardOutcome(port int, payload []byte, body *limitedWriter, readSeconds int) *PortForwardOutcome {
	raw := body.Bytes()
	outcome := &PortForwardOutcome{
		Port:              port,
		BytesSent:         len(payload),
		ReadSeconds:       readSeconds,
		ResponseTruncated: body.full,
		TunnelClosed:      true,
	}
	if utf8.Valid(raw) {
		outcome.Response = string(raw)
		outcome.ResponseEncoding = "utf-8"
		return outcome
	}
	outcome.Response = base64.StdEncoding.EncodeToString(raw)
	outcome.ResponseEncoding = "base64"
	return outcome
}
