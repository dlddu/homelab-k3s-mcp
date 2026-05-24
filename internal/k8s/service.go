package k8s

import "context"

// Service is the kubernetes-facing surface the MCP tools depend on.
type Service interface {
	ListNamespaces(ctx context.Context) ([]any, error)

	ListWorkloads(ctx context.Context, kind WorkloadKind, namespace *string) ([]any, error)

	RolloutRestart(ctx context.Context, kind WorkloadKind, namespace, name string) (string, error)

	// ExecInPod runs command inside the first Running pod matching labelSelector
	// in namespace. container is required when the pod has more than one
	// container; pass nil to default to the pod's only container.
	ExecInPod(ctx context.Context, namespace, labelSelector string, container *string, command []string) (*ExecOutcome, error)

	ScaleWorkload(ctx context.Context, kind WorkloadKind, namespace, name string, replicas int32) (int32, error)

	// WorkloadLogs fetches container logs from a pod backing the given workload.
	// It resolves the workload's pod selector and pulls logs from the first
	// Running pod (falling back to any matching pod if none is Running, so
	// Previous still works after a crash loop).
	WorkloadLogs(ctx context.Context, kind WorkloadKind, namespace, name string, opts LogOptions) (*LogResult, error)

	// DescribePod produces a kubectl-describe-style snapshot for a single pod.
	DescribePod(ctx context.Context, namespace string, target PodTarget) (*PodDescription, error)
}

// Unavailable is a Service that fails every call with the same reason. It is
// used when the kubernetes client could not be initialised.
type Unavailable struct {
	reason string
}

// NewUnavailable builds an Unavailable service with the given reason.
func NewUnavailable(reason string) *Unavailable {
	if reason == "" {
		reason = "kubernetes client is not configured"
	}
	return &Unavailable{reason: reason}
}

func (u *Unavailable) ListNamespaces(context.Context) ([]any, error) {
	return nil, unavailableErr(u.reason)
}

func (u *Unavailable) ListWorkloads(context.Context, WorkloadKind, *string) ([]any, error) {
	return nil, unavailableErr(u.reason)
}

func (u *Unavailable) RolloutRestart(context.Context, WorkloadKind, string, string) (string, error) {
	return "", unavailableErr(u.reason)
}

func (u *Unavailable) ExecInPod(context.Context, string, string, *string, []string) (*ExecOutcome, error) {
	return nil, unavailableErr(u.reason)
}

func (u *Unavailable) ScaleWorkload(context.Context, WorkloadKind, string, string, int32) (int32, error) {
	return 0, unavailableErr(u.reason)
}

func (u *Unavailable) WorkloadLogs(context.Context, WorkloadKind, string, string, LogOptions) (*LogResult, error) {
	return nil, unavailableErr(u.reason)
}

func (u *Unavailable) DescribePod(context.Context, string, PodTarget) (*PodDescription, error) {
	return nil, unavailableErr(u.reason)
}
