package k8s

import (
	"context"
	"errors"
	"sync"
	"testing"

	utilexec "k8s.io/client-go/util/exec"
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

// AC12 again, one layer up: what the response says about how the run ended.
// The SPDY executor is out of a unit test's reach, so asserting through
// ExecResource was never an option and this classification went unwatched — a
// real round trip is what eventually caught it reporting a successful command
// as a failure. Splitting the decision out is what lets it be watched here.
func TestExecStreamOutcome(t *testing.T) {
	t.Run("a command that ended 0 is reported as success", func(t *testing.T) {
		// The regression: the stream says nothing when a command exits 0, so
		// deriving the code from the error alone leaves it nil exactly when the
		// run worked, and Success (exitCode != nil && *exitCode == 0) can then
		// never be true. Observed live before it was asserted here.
		code, timeLimited, err := execStreamOutcome(nil, nil, false)
		if err != nil {
			t.Fatalf("err = %v, want nil — a clean stream is the success path", err)
		}
		if code == nil || *code != 0 {
			t.Fatalf("exitCode = %v, want 0", code)
		}
		if timeLimited {
			t.Error("timeLimited = true, want false — nothing stopped this command")
		}
	})

	t.Run("a non-zero exit carries its own code", func(t *testing.T) {
		code, timeLimited, err := execStreamOutcome(utilexec.CodeExitError{Err: errors.New("exit 3"), Code: 3}, nil, false)
		if err != nil {
			t.Fatalf("err = %v, want nil — a failed command is an answer, not a transport fault", err)
		}
		if code == nil || *code != 3 {
			t.Fatalf("exitCode = %v, want 3", code)
		}
		if timeLimited {
			t.Error("timeLimited = true, want false")
		}
	})

	t.Run("the time cap is reported, not raised as an error", func(t *testing.T) {
		code, timeLimited, err := execStreamOutcome(context.DeadlineExceeded, context.DeadlineExceeded, false)
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if !timeLimited {
			t.Error("timeLimited = false, want true")
		}
		if code != nil {
			t.Errorf("exitCode = %v, want nil — a command cut at the cap reported none", *code)
		}
	})

	t.Run("a filled byte cap answers instead of failing", func(t *testing.T) {
		// The cap cancels the context to stop listening, so the stream comes
		// back cancelled rather than past its deadline. Classifying that as a
		// transport fault would return an error where AC12 promises a response
		// that says it was cut.
		code, timeLimited, err := execStreamOutcome(context.Canceled, context.Canceled, true)
		if err != nil {
			t.Fatalf("err = %v, want nil — the caller still gets the bytes that fit", err)
		}
		if code != nil {
			t.Errorf("exitCode = %v, want nil — a command cut mid-stream reported none", *code)
		}
		if timeLimited {
			t.Error("timeLimited = true, want false — the byte cap stopped this, not the clock")
		}
	})

	t.Run("a cut stream reports no exit code even when it ended cleanly", func(t *testing.T) {
		// Unchanged from before the fix, and the reason the success branch
		// asks for !capped: the command never got to report an exit.
		code, timeLimited, err := execStreamOutcome(nil, nil, true)
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if code != nil {
			t.Errorf("exitCode = %v, want nil — the run was cut short", *code)
		}
		if timeLimited {
			t.Error("timeLimited = true, want false")
		}
	})

	t.Run("a cancel with no cap filled is still a fault", func(t *testing.T) {
		// Without the cap flag there is nothing to distinguish this from a
		// caller walking away mid-call, so it must stay an error.
		if _, _, err := execStreamOutcome(context.Canceled, context.Canceled, false); err == nil {
			t.Error("err = nil, want a fault — nothing explains this cancellation")
		}
	})

	t.Run("any other stream failure stays a fault", func(t *testing.T) {
		_, _, err := execStreamOutcome(errors.New("upgrade request failed"), nil, false)
		if err == nil {
			t.Fatal("err = nil, want the transport failure surfaced")
		}
		var apiErr *Error
		if !errors.As(err, &apiErr) {
			t.Errorf("err = %T, want *Error", err)
		}
	})
}
