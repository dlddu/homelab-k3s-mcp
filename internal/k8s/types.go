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

// AttachOutcome captures one read window on a running container's streams. It
// is a separate type from ExecOutcome because nothing was started, so nothing
// ended: the process is still running when the window closes and there is no
// exit code to report.
type AttachOutcome struct {
	Pod    string `json:"pod"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	// ReadSeconds echoes the window actually used, so a response can be read
	// without knowing whether the caller passed one or took the default.
	ReadSeconds int `json:"read_seconds"`
	// StdinWritten answers a question the output cannot: a process that ignores
	// its input says nothing, so "did the payload arrive" would otherwise be
	// indistinguishable from "it arrived and was dropped".
	StdinWritten    bool `json:"stdin_written"`
	StdoutTruncated bool `json:"stdout_truncated"`
	StderrTruncated bool `json:"stderr_truncated"`
}

// AttachRef names the pod resource_attach connects to, addressed by coordinate
// (prd-resource-generic AC13).
type AttachRef struct {
	APIVersion string
	Kind       string
	Namespace  *string
	Name       string
}

// PortForwardOutcome is one round trip through a tunnel that no longer exists by
// the time this is returned (prd-resource-generic AC14).
type PortForwardOutcome struct {
	Pod      string `json:"pod"`
	Port     int    `json:"port"`
	Response string `json:"response"`
	// ResponseEncoding is "utf-8" or "base64". It is always present rather than
	// omitted in the common case: a caller that has to test for the field's
	// absence to learn the encoding is a caller that will forget to.
	ResponseEncoding  string `json:"response_encoding"`
	ResponseTruncated bool   `json:"response_truncated"`
	// BytesSent reports what was written before the read, so a response can be
	// read without the request beside it.
	BytesSent   int `json:"bytes_sent"`
	ReadSeconds int `json:"read_seconds"`
	// TunnelClosed is constant true, and says so on purpose: AC14's contract is
	// that a second round trip cannot ride this approval, and the response is
	// where an operator reading a transcript can see that it did not.
	TunnelClosed bool `json:"tunnel_closed"`
}

// PortForwardRef names the pod resource_port_forward opens a tunnel to, addressed
// by coordinate (prd-resource-generic AC14).
type PortForwardRef struct {
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
