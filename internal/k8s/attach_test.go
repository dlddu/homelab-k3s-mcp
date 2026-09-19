package k8s

import (
	"io"
	"testing"
	"time"
)

// AC13 contracts a read window and a single stdin write together, and the way
// those two collide is not visible from AttachResource: the SPDY executor is out
// of reach for a unit test, so a reader that ends at the payload looked correct
// until a real round trip measured 0.016s against a readSeconds=3 window. These
// assertions sit on the reader for that reason — they are the only place the
// regression can be caught before a cluster is involved.
func TestAttachStdinReaderHoldsTheWindow(t *testing.T) {
	t.Run("the payload is yielded once, in order, without reaching EOF", func(t *testing.T) {
		done := make(chan struct{})
		defer close(done)
		r := &attachStdinReader{payload: []byte("hello"), done: done}

		// One byte at a time: a reader that re-sent or reordered the payload
		// would show it here, and so would one that ended early.
		var got []byte
		buf := make([]byte, 1)
		for i := 0; i < len(r.payload); i++ {
			n, err := r.Read(buf)
			if err != nil {
				t.Fatalf("Read = %v, want the payload while the window is open", err)
			}
			if n != 1 {
				t.Fatalf("Read = %d bytes, want 1", n)
			}
			got = append(got, buf[0])
		}
		if string(got) != "hello" {
			t.Errorf("payload read back as %q, want hello", got)
		}
	})

	t.Run("a drained payload withholds EOF until the window closes", func(t *testing.T) {
		done := make(chan struct{})
		r := &attachStdinReader{payload: []byte("hi"), done: done}

		if _, err := r.Read(make([]byte, 8)); err != nil {
			t.Fatalf("first Read = %v, want the payload", err)
		}

		returned := make(chan error, 1)
		go func() {
			_, err := r.Read(make([]byte, 8))
			returned <- err
		}()

		// This is the whole defect in one assertion: the read after the payload
		// must not answer, because answering is what closes the remote stdin
		// stream and takes the window with it.
		select {
		case err := <-returned:
			t.Fatalf("Read returned %v while the window was open — the stream would be torn down early", err)
		case <-time.After(50 * time.Millisecond):
		}

		close(done)
		select {
		case err := <-returned:
			if err != io.EOF {
				t.Errorf("Read = %v after the window closed, want io.EOF", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Read never returned after the window closed — the stdin goroutine would leak")
		}
	})

	t.Run("an empty stdin still holds the window", func(t *testing.T) {
		// An empty payload is a write of zero bytes, not the absence of one:
		// the tool still reports stdinWritten, so the window it promised has to
		// be there too.
		done := make(chan struct{})
		r := &attachStdinReader{payload: nil, done: done}

		returned := make(chan error, 1)
		go func() {
			_, err := r.Read(make([]byte, 8))
			returned <- err
		}()

		select {
		case err := <-returned:
			t.Fatalf("Read returned %v with an empty payload — an empty stdin must not collapse the window", err)
		case <-time.After(50 * time.Millisecond):
		}

		close(done)
		if err := <-returned; err != io.EOF {
			t.Errorf("Read = %v after the window closed, want io.EOF", err)
		}
	})
}
