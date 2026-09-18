package k8s

import (
	"sync"
	"testing"
)

// AC12's caps: a capped stream keeps what fits, marks itself full and stops
// listening — a response that ends early without saying so would read like
// the command said less than it did.
func TestLimitedWriterCaps(t *testing.T) {
	t.Run("under the cap it is a plain buffer", func(t *testing.T) {
		w := &limitedWriter{limit: 10}
		if n, err := w.Write([]byte("hi")); n != 2 || err != nil {
			t.Fatalf("Write = %d, %v", n, err)
		}
		if w.full {
			t.Error("full = true, want false below the cap")
		}
		if w.String() != "hi" {
			t.Errorf("String = %q, want hi", w.String())
		}
	})

	t.Run("over the cap it keeps the prefix and marks the cut", func(t *testing.T) {
		w := &limitedWriter{limit: 3}
		if n, err := w.Write([]byte("abcde")); n != 5 || err != nil {
			t.Fatalf("Write = %d, %v — the stream must be drained even past the cap", n, err)
		}
		if !w.full {
			t.Error("full = false, want true over the cap")
		}
		if w.String() != "abc" {
			t.Errorf("String = %q, want the first 3 bytes", w.String())
		}
		// After the cut the writer keeps draining without keeping anything, so
		// `yes` cannot wedge the transport the cap is trying to close.
		if n, err := w.Write([]byte("fg")); n != 2 || err != nil {
			t.Fatalf("Write after full = %d, %v", n, err)
		}
		if w.String() != "abc" {
			t.Errorf("String = %q, want abc unchanged", w.String())
		}
	})

	t.Run("going full stops listening", func(t *testing.T) {
		var mu sync.Mutex
		stopped := false
		w := &limitedWriter{limit: 1, onFull: func() { mu.Lock(); stopped = true; mu.Unlock() }}
		if _, err := w.Write([]byte("ab")); err != nil {
			t.Fatalf("Write = %v", err)
		}
		mu.Lock()
		defer mu.Unlock()
		if !stopped {
			t.Error("onFull did not fire — the byte cap cannot stop the reader it caps")
		}
	})

	t.Run("a write that exactly fills the cap is not a cut", func(t *testing.T) {
		w := &limitedWriter{limit: 2}
		if _, err := w.Write([]byte("ab")); err != nil {
			t.Fatalf("Write = %v", err)
		}
		if w.full {
			t.Error("full = true, want false — nothing was discarded at exactly the cap")
		}
		if got := w.String(); got != "ab" {
			t.Errorf("String = %q, want ab", got)
		}
	})
}
