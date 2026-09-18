package k8s

import "fmt"

// errKind distinguishes a client/config problem from an apiserver-level error.
type errKind int

const (
	kindUnavailable errKind = iota
	kindAPI
)

// Error is the error type returned by every Service method.
type Error struct {
	kind errKind
	msg  string
}

func (e *Error) Error() string {
	switch e.kind {
	case kindUnavailable:
		return "kubernetes client unavailable: " + e.msg
	default:
		return "kubernetes api error: " + e.msg
	}
}

// unavailableErr reports that the kubernetes integration is not usable.
func unavailableErr(msg string) *Error { return &Error{kind: kindUnavailable, msg: msg} }

// APIError wraps an error returned by the kubernetes apiserver.
func APIError(msg string) *Error { return &Error{kind: kindAPI, msg: msg} }

func apiErrorf(format string, args ...any) *Error {
	return APIError(fmt.Sprintf(format, args...))
}

// ExecOutcome captures the result of running a command inside a pod.
type ExecOutcome struct {
	Pod      string `json:"pod"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode *int32 `json:"exit_code"`
	Success  bool   `json:"success"`
	// AC12's caps travel beside the text: a response that says "cut" rather
	// than silently ending early is the difference between an answer and a lie
	// about how much the command said.
	StdoutTruncated bool `json:"stdout_truncated"`
	StderrTruncated bool `json:"stderr_truncated"`
	// TimeLimited marks the running-time cap as what stopped the call. It can
	// be set with no truncation at all — a slow command that said little.
	TimeLimited bool `json:"time_limited"`
}

// ExecRef names the pod resource_exec runs a command in, addressed by
// coordinate (prd-resource-generic AC12).
type ExecRef struct {
	APIVersion string
	Kind       string
	Namespace  *string
	Name       string
}

// LogOptions controls a read of the log subresource.
type LogOptions struct {
	Container    *string
	TailLines    *int64
	Previous     bool
	Timestamps   bool
	SinceSeconds *int64
}
