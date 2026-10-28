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

// Unavailable reports whether this is the "not configured" kind — the one
// the dispatcher records as a refusal, not an error.
func (e *Error) Unavailable() bool { return e.kind == kindUnavailable }

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
	// The caps travel beside the text so a cut response is not read as the command's whole output.
	StdoutTruncated bool `json:"stdout_truncated"`
	StderrTruncated bool `json:"stderr_truncated"`
	// TimeLimited marks the running-time cap as what stopped the call. It can
	// be set with no truncation at all — a slow command that said little.
	TimeLimited bool `json:"time_limited"`
}

// ExecRef names the pod resource_exec runs a command in, addressed by
// coordinate.
type ExecRef struct {
	APIVersion string
	Kind       string
	Namespace  *string
	Name       string
}

// AttachOutcome captures one read window on a running container's streams.
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
type AttachRef struct {
	APIVersion string
	Kind       string
	Namespace  *string
	Name       string
}

// PortForwardOutcome is one round trip through a tunnel that no longer exists by
// the time this is returned.
type PortForwardOutcome struct {
	Pod      string `json:"pod"`
	Port     int    `json:"port"`
	Response string `json:"response"`
	// ResponseEncoding is "utf-8" or "base64". It is always present rather than
	// omitted in the common case: a caller that has to test for the field's
	// absence to learn the encoding is a caller that will forget to.
	ResponseEncoding  string `json:"response_encoding"`
	ResponseTruncated bool   `json:"response_truncated"`
	BytesSent         int    `json:"bytes_sent"`
	ReadSeconds       int    `json:"read_seconds"`
	// TunnelClosed is constant true on purpose: a second round trip cannot ride this approval, and the response is where a transcript reader can see that.
	TunnelClosed bool `json:"tunnel_closed"`
}

// PortForwardRef names the pod resource_port_forward opens a tunnel to, addressed
// by coordinate.
type PortForwardRef struct {
	APIVersion string
	Kind       string
	Namespace  *string
	Name       string
}

// ProxyOutcome is one HTTP round trip made through the apiserver's proxy
// subresource.
type ProxyOutcome struct {
	// Method and Verb travel together because the second follows the first; a transcript reader should not have to reconstruct which permission a GET spent.
	Method string `json:"method"`
	Verb   string `json:"verb"`
	// Path echoes what was reached verbatim. It is the only field that says what the call did; the (verb, resource) pair cannot.
	Path string `json:"path"`
	Name string `json:"name"`
	// Status is the target's own HTTP status. A 404 from the proxied endpoint
	// is an answer rather than a failure of this tool, so it is reported here
	// instead of being turned into an error.
	Status int    `json:"status"`
	Body   string `json:"body"`
	// BodyEncoding is "utf-8" or "base64", always present rather than omitted
	// in the common case (the PortForwardOutcome.ResponseEncoding reasoning).
	BodyEncoding string `json:"body_encoding"`
	Truncated    bool   `json:"truncated"`
	ReadSeconds  int    `json:"read_seconds"`
}

// ProxyRef names the object resource_proxy reaches, addressed by coordinate.
// Unlike the other three stream refs this one is not pod-only: Service and the
// cluster-scoped Node are targets too.
type ProxyRef struct {
	APIVersion string
	Kind       string
	Namespace  *string
	Name       string
	Port       *string
}

// LogOptions controls a read of the log subresource.
type LogOptions struct {
	Container    *string
	TailLines    *int64
	Previous     bool
	Timestamps   bool
	SinceSeconds *int64
}
