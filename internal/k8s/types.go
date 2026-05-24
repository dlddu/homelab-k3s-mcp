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

// ParseWorkloadKind maps a user-supplied string to a WorkloadKind. The bool is
// false when the input does not name a known kind.
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

// LogOptions controls a workload_logs request.
type LogOptions struct {
	Container    *string
	TailLines    *int64
	Previous     bool
	Timestamps   bool
	SinceSeconds *int64
}

// LogResult is the output of a workload_logs request.
type LogResult struct {
	Pod       string
	Container *string
	Logs      string
}

// ContainerInfo mirrors a single container's status in a pod snapshot.
type ContainerInfo struct {
	Name         string  `json:"name"`
	Image        string  `json:"image"`
	Ready        bool    `json:"ready"`
	Started      *bool   `json:"started"`
	RestartCount int32   `json:"restart_count"`
	State        *string `json:"state"`
	Reason       *string `json:"reason"`
	Message      *string `json:"message"`
	StartedAt    *string `json:"started_at"`
	FinishedAt   *string `json:"finished_at"`
	ExitCode     *int32  `json:"exit_code"`
	LastState    *string `json:"last_state"`
	LastReason   *string `json:"last_reason"`
	LastExitCode *int32  `json:"last_exit_code"`
}

// PodConditionInfo mirrors a pod condition.
type PodConditionInfo struct {
	Type               string  `json:"type"`
	Status             string  `json:"status"`
	Reason             *string `json:"reason"`
	Message            *string `json:"message"`
	LastTransitionTime *string `json:"last_transition_time"`
}

// PodEventInfo mirrors a single event involving the pod.
type PodEventInfo struct {
	Type           string  `json:"type"`
	Reason         string  `json:"reason"`
	Message        string  `json:"message"`
	Count          int32   `json:"count"`
	FirstTimestamp *string `json:"first_timestamp"`
	LastTimestamp  *string `json:"last_timestamp"`
	Source         *string `json:"source"`
}

// PodDescription is a kubectl-describe-style snapshot of a single pod.
type PodDescription struct {
	Name              string             `json:"name"`
	Namespace         string             `json:"namespace"`
	Node              *string            `json:"node"`
	Phase             *string            `json:"phase"`
	PodIP             *string            `json:"pod_ip"`
	HostIP            *string            `json:"host_ip"`
	ServiceAccount    *string            `json:"service_account"`
	Priority          *int32             `json:"priority"`
	PriorityClassName *string            `json:"priority_class_name"`
	QOSClass          *string            `json:"qos_class"`
	StartTime         *string            `json:"start_time"`
	CreationTimestamp *string            `json:"creation_timestamp"`
	Labels            map[string]string  `json:"labels"`
	Annotations       map[string]string  `json:"annotations"`
	NodeSelector      map[string]string  `json:"node_selector"`
	OwnerReferences   []any              `json:"owner_references"`
	Conditions        []PodConditionInfo `json:"conditions"`
	InitContainers    []ContainerInfo    `json:"init_containers"`
	Containers        []ContainerInfo    `json:"containers"`
	Events            []PodEventInfo     `json:"events"`
}

// TargetMode selects how DescribePod resolves the pod to inspect.
type TargetMode int

const (
	TargetName TargetMode = iota
	TargetSelector
	TargetWorkload
)

// PodTarget identifies the pod that DescribePod should snapshot.
type PodTarget struct {
	Mode         TargetMode
	Name         string
	Selector     string
	Kind         WorkloadKind
	WorkloadName string
}

// Summary structs are the JSON items returned by list operations. Field names
// are serialised verbatim (snake_case) to match the documented tool output.

type namespaceSummary struct {
	Name              string  `json:"name"`
	Phase             *string `json:"phase"`
	CreationTimestamp *string `json:"creation_timestamp"`
}

type deploymentSummary struct {
	Name              string  `json:"name"`
	Namespace         string  `json:"namespace"`
	Replicas          int32   `json:"replicas"`
	ReadyReplicas     int32   `json:"ready_replicas"`
	UpdatedReplicas   int32   `json:"updated_replicas"`
	AvailableReplicas int32   `json:"available_replicas"`
	CreationTimestamp *string `json:"creation_timestamp"`
}

type statefulSetSummary struct {
	Name              string  `json:"name"`
	Namespace         string  `json:"namespace"`
	Replicas          int32   `json:"replicas"`
	ReadyReplicas     int32   `json:"ready_replicas"`
	UpdatedReplicas   int32   `json:"updated_replicas"`
	CurrentReplicas   int32   `json:"current_replicas"`
	CreationTimestamp *string `json:"creation_timestamp"`
}

type daemonSetSummary struct {
	Name                   string  `json:"name"`
	Namespace              string  `json:"namespace"`
	DesiredNumberScheduled int32   `json:"desired_number_scheduled"`
	CurrentNumberScheduled int32   `json:"current_number_scheduled"`
	NumberReady            int32   `json:"number_ready"`
	NumberAvailable        int32   `json:"number_available"`
	UpdatedNumberScheduled int32   `json:"updated_number_scheduled"`
	CreationTimestamp      *string `json:"creation_timestamp"`
}
