package k8s

import "fmt"

// WorkloadKind enumerates the workload types the server can operate on.
type WorkloadKind int

const (
	Deployment WorkloadKind = iota
	StatefulSet
	DaemonSet
)

func (k WorkloadKind) String() string {
	switch k {
	case Deployment:
		return "Deployment"
	case StatefulSet:
		return "StatefulSet"
	case DaemonSet:
		return "DaemonSet"
	default:
		return "Unknown"
	}
}

// ParseWorkloadKind maps a user-supplied string to a WorkloadKind.
func ParseWorkloadKind(s string) (WorkloadKind, bool) {
	switch s {
	case "Deployment", "deployment", "deploy":
		return Deployment, true
	case "StatefulSet", "statefulset", "sts":
		return StatefulSet, true
	case "DaemonSet", "daemonset", "ds":
		return DaemonSet, true
	default:
		return 0, false
	}
}

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
}

// LogOptions controls a read of the log subresource.
type LogOptions struct {
	Container    *string
	TailLines    *int64
	Previous     bool
	Timestamps   bool
	SinceSeconds *int64
}
