package k8s

import "context"

// Service is the kubernetes-facing surface the MCP tools depend on.
type Service interface {
	// APIResources reports the kinds the cluster actually serves, which is what
	// the other two methods resolve a coordinate against.
	APIResources(ctx context.Context) ([]APIResource, error)

	ListResources(ctx context.Context, query ListQuery) (*ListResult, error)

	GetResource(ctx context.Context, ref ResourceRef) (*ResourceResult, error)

	UpdateResource(ctx context.Context, ref UpdateRef) (*ResourceResult, error)

	PatchResource(ctx context.Context, ref PatchRef) (*ResourceResult, error)

	// ExecInPod runs command inside the first Running pod matching labelSelector
	// in namespace. container is required when the pod has more than one
	// container; pass nil to default to the pod's only container.
	ExecInPod(ctx context.Context, namespace, labelSelector string, container *string, command []string) (*ExecOutcome, error)

	ScaleWorkload(ctx context.Context, kind WorkloadKind, namespace, name string, replicas int32) (int32, error)
}

// Unavailable is a Service that fails every call with the same reason.
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

func (u *Unavailable) APIResources(context.Context) ([]APIResource, error) {
	return nil, unavailableErr(u.reason)
}

func (u *Unavailable) ListResources(context.Context, ListQuery) (*ListResult, error) {
	return nil, unavailableErr(u.reason)
}

func (u *Unavailable) GetResource(context.Context, ResourceRef) (*ResourceResult, error) {
	return nil, unavailableErr(u.reason)
}

func (u *Unavailable) UpdateResource(context.Context, UpdateRef) (*ResourceResult, error) {
	return nil, unavailableErr(u.reason)
}

func (u *Unavailable) PatchResource(context.Context, PatchRef) (*ResourceResult, error) {
	return nil, unavailableErr(u.reason)
}

func (u *Unavailable) ExecInPod(context.Context, string, string, *string, []string) (*ExecOutcome, error) {
	return nil, unavailableErr(u.reason)
}

func (u *Unavailable) ScaleWorkload(context.Context, WorkloadKind, string, string, int32) (int32, error) {
	return 0, unavailableErr(u.reason)
}
