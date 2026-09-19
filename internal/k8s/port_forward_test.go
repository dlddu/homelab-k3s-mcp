package k8s

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/httpstream"
)

// TestRoundTripSendsOnceAndCloses drives the port-forward protocol over an
// in-memory connection. What it is really asserting is the shape AC14 chose: one
// write, one read, and a write half that is closed before the read starts —
// because most of what answers on a forwarded port reads until EOF, and a call
// that never closed its side would spend the whole window waiting for a reply
// that is waiting for it.
func TestRoundTripSendsOnceAndCloses(t *testing.T) {
	conn := &fakeStreamConn{response: []byte("HTTP/1.1 200 OK\r\n\r\npong")}

	outcome, err := roundTrip(context.Background(), conn, 8080, []byte("GET / HTTP/1.0\r\n\r\n"), 5)
	if err != nil {
		t.Fatalf("roundTrip = %v, want the answer", err)
	}

	if got := string(conn.data.written.Bytes()); got != "GET / HTTP/1.0\r\n\r\n" {
		t.Errorf("payload written = %q, want the bytes verbatim", got)
	}
	if conn.data.writes != 1 {
		t.Errorf("writes = %d, want exactly 1 — AC14's round trip sends once", conn.data.writes)
	}
	if !conn.data.closedBeforeRead {
		t.Error("the write half was still open when the read started; a server that reads to EOF would never answer")
	}
	if !strings.Contains(outcome.Response, "pong") {
		t.Errorf("response = %q, want what the far side said", outcome.Response)
	}
	if outcome.BytesSent != len("GET / HTTP/1.0\r\n\r\n") {
		t.Errorf("bytesSent = %d, want the payload length", outcome.BytesSent)
	}
	if !outcome.TunnelClosed {
		t.Error("tunnelClosed = false, want true")
	}

	// The error stream has to be created before the data stream: the apiserver
	// pairs a data stream with an already-registered error stream and resets one
	// that arrives alone.
	if len(conn.order) != 2 || conn.order[0] != v1StreamTypeError || conn.order[1] != v1StreamTypeData {
		t.Errorf("stream order = %v, want the error stream first", conn.order)
	}
	for i, headers := range conn.headers {
		if headers.Get(v1PortHeader) != "8080" {
			t.Errorf("stream %d port header = %q, want 8080", i, headers.Get(v1PortHeader))
		}
		if headers.Get(v1RequestIDHeader) != conn.headers[0].Get(v1RequestIDHeader) {
			t.Errorf("stream %d requestID = %q, want both streams on the same request", i, headers.Get(v1RequestIDHeader))
		}
	}
}

// TestRoundTripSurfacesTheErrorStream is the difference between "nothing is
// listening on that port" and "the port said nothing". The apiserver accepts the
// streams either way and reports the refusal on the error stream, so a call that
// ignored it would answer an empty string to a forward that never connected.
func TestRoundTripSurfacesTheErrorStream(t *testing.T) {
	conn := &fakeStreamConn{errorMessage: "unable to do port forwarding: socat not found"}

	_, err := roundTrip(context.Background(), conn, 9999, nil, 5)
	if err == nil {
		t.Fatal("roundTrip = nil error, want the refusal the far side reported")
	}
	if !strings.Contains(err.Error(), "socat not found") {
		t.Errorf("error = %q, want it to carry what the apiserver said", err.Error())
	}
	if !strings.Contains(err.Error(), "9999") {
		t.Errorf("error = %q, want it to name the port", err.Error())
	}
}

// TestRoundTripStopsAtTheByteCap covers the cap AC14 asks for on the read. The
// time cap alone does not bound the answer: a port that streams fills memory for
// the whole window, and the window is allowed to be 30 seconds.
func TestRoundTripStopsAtTheByteCap(t *testing.T) {
	conn := &fakeStreamConn{response: bytes.Repeat([]byte("y"), streamMaxOutputBytes+4096)}

	outcome, err := roundTrip(context.Background(), conn, 8080, nil, 5)
	if err != nil {
		t.Fatalf("roundTrip = %v, want a capped read to still answer", err)
	}
	if len(outcome.Response) != streamMaxOutputBytes {
		t.Errorf("response length = %d, want the cap %d", len(outcome.Response), streamMaxOutputBytes)
	}
	if !outcome.ResponseTruncated {
		t.Error("responseTruncated = false, want true — a shortened answer that does not say so is a wrong answer")
	}
}

// TestRoundTripEndsWhenTheWindowDoes is the other cap. A port that stays open and
// says nothing is the normal case for plenty of protocols, and AC14 makes the
// window the ending rather than leaving the call to hang.
func TestRoundTripEndsWhenTheWindowDoes(t *testing.T) {
	conn := &fakeStreamConn{silent: true}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := roundTrip(ctx, conn, 8080, nil, 5); err != nil {
			t.Errorf("roundTrip = %v, want an empty answer rather than a failure", err)
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("roundTrip did not return when the window closed")
	}
}

// TestPortForwardOutcomeReportsItsEncoding is about not lying. Go's JSON encoder
// turns invalid UTF-8 into U+FFFD without a word, so a binary answer carried as a
// string comes back looking like a string and being a different set of bytes.
func TestPortForwardOutcomeReportsItsEncoding(t *testing.T) {
	t.Run("text travels as itself", func(t *testing.T) {
		body := &limitedWriter{limit: streamMaxOutputBytes}
		body.Write([]byte("+PONG\r\n"))

		outcome := newPortForwardOutcome(6379, nil, body, 5)
		if outcome.ResponseEncoding != "utf-8" {
			t.Errorf("responseEncoding = %q, want utf-8", outcome.ResponseEncoding)
		}
		if outcome.Response != "+PONG\r\n" {
			t.Errorf("response = %q, want the text unchanged", outcome.Response)
		}
	})

	t.Run("bytes that are not text travel base64", func(t *testing.T) {
		raw := []byte{0x00, 0xff, 0xfe, 0x10}
		body := &limitedWriter{limit: streamMaxOutputBytes}
		body.Write(raw)

		outcome := newPortForwardOutcome(5432, nil, body, 5)
		if outcome.ResponseEncoding != "base64" {
			t.Errorf("responseEncoding = %q, want base64", outcome.ResponseEncoding)
		}
		decoded, err := base64.StdEncoding.DecodeString(outcome.Response)
		if err != nil {
			t.Fatalf("response is not decodable base64: %v", err)
		}
		if !bytes.Equal(decoded, raw) {
			t.Errorf("decoded = %v, want the bytes that arrived %v", decoded, raw)
		}
	})
}

// fakeStreamConn is an httpstream.Connection that hands out the two streams the
// port-forward protocol asks for and scripts what the far side says.
type fakeStreamConn struct {
	mu           sync.Mutex
	order        []string
	headers      []http.Header
	response     []byte
	errorMessage string
	// silent makes the data stream stay open saying nothing, which is what a
	// connection-oriented protocol that is waiting for more input looks like.
	silent bool

	data   *fakeStream
	closed bool
}

func (c *fakeStreamConn) CreateStream(headers http.Header) (httpstream.Stream, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	streamType := headers.Get(v1StreamTypeHeader)
	c.order = append(c.order, streamType)
	c.headers = append(c.headers, headers.Clone())

	if streamType == v1StreamTypeError {
		return &fakeStream{reader: strings.NewReader(c.errorMessage)}, nil
	}
	stream := &fakeStream{}
	if c.silent {
		stream.blockUntil = make(chan struct{})
	} else {
		stream.reader = bytes.NewReader(c.response)
	}
	c.data = stream
	return stream, nil
}

func (c *fakeStreamConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func (c *fakeStreamConn) CloseChan() <-chan bool             { return make(chan bool) }
func (c *fakeStreamConn) SetIdleTimeout(time.Duration)       {}
func (c *fakeStreamConn) RemoveStreams(...httpstream.Stream) {}

// fakeStream is one half-duplex pair: what this side wrote, and what the far
// side has to say.
type fakeStream struct {
	mu               sync.Mutex
	written          bytes.Buffer
	writes           int
	closed           bool
	closedBeforeRead bool
	readStarted      bool

	reader     io.Reader
	blockUntil chan struct{}
}

func (s *fakeStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	return s.written.Write(p)
}

func (s *fakeStream) Read(p []byte) (int, error) {
	s.mu.Lock()
	if !s.readStarted {
		s.readStarted = true
		s.closedBeforeRead = s.closed
	}
	blocker, reader := s.blockUntil, s.reader
	s.mu.Unlock()

	if blocker != nil {
		<-blocker
		return 0, io.EOF
	}
	if reader == nil {
		return 0, io.EOF
	}
	return reader.Read(p)
}

func (s *fakeStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *fakeStream) Reset() error         { return s.Close() }
func (s *fakeStream) Headers() http.Header { return http.Header{} }
func (s *fakeStream) Identifier() uint32   { return 0 }
