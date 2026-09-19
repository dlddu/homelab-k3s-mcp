package k8s

import "context"

// Service is the kubernetes-facing surface the MCP tools depend on.
type Service interface {
	// APIResources reports the kinds the cluster actually serves, which is what
	// the other two methods resolve a coordinate against.
	APIResources(ctx context.Context) ([]APIResource, error)

	ListResources(ctx context.Context, query ListQuery) (*ListResult, error)

	// WatchResources collects the change events of one bounded window and
	// returns them; it does not hand back a stream, because the tool layer
	// answers one request at a time (prd-resource-generic AC6).
	WatchResources(ctx context.Context, query WatchQuery) (*WatchResult, error)

	GetResource(ctx context.Context, ref ResourceRef) (*ResourceResult, error)

	CreateResource(ctx context.Context, ref CreateRef) (*ResourceResult, error)

	UpdateResource(ctx context.Context, ref UpdateRef) (*ResourceResult, error)

	PatchResource(ctx context.Context, ref PatchRef) (*ResourceResult, error)

	DeleteResource(ctx context.Context, ref DeleteRef) (*ResourceResult, error)

	// DeleteCollection removes one namespace's selection with the
	// deletecollection verb (prd-resource-generic AC11).
	DeleteCollection(ctx context.Context, ref DeleteCollectionRef) (*DeleteCollectionResult, error)

	// ExecInPod runs command inside the first Running pod matching labelSelector
	// in namespace. container is required when the pod has more than one
	// container; pass nil to default to the pod's only container.
	ExecInPod(ctx context.Context, namespace, labelSelector string, container *string, command []string) (*ExecOutcome, error)

	// ExecResource is the coordinate-addressed half of exec
	// (prd-resource-generic AC12): one named pod, one command array, stdout and
	// stderr returned separately under AC12's byte and time caps.
	ExecResource(ctx context.Context, ref ExecRef, container *string, command []string) (*ExecOutcome, error)

	// AttachResource joins the streams of the process already running in one
	// named pod (prd-resource-generic AC13).
	AttachResource(ctx context.Context, ref AttachRef, container *string, stdin *string, readSeconds int) (*AttachOutcome, error)
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

func (u *Unavailable) WatchResources(context.Context, WatchQuery) (*WatchResult, error) {
	return nil, unavailableErr(u.reason)
}

func (u *Unavailable) GetResource(context.Context, ResourceRef) (*ResourceResult, error) {
	return nil, unavailableErr(u.reason)
}

func (u *Unavailable) CreateResource(context.Context, CreateRef) (*ResourceResult, error) {
	return nil, unavailableErr(u.reason)
}

func (u *Unavailable) UpdateResource(context.Context, UpdateRef) (*ResourceResult, error) {
	return nil, unavailableErr(u.reason)
}

func (u *Unavailable) PatchResource(context.Context, PatchRef) (*ResourceResult, error) {
	return nil, unavailableErr(u.reason)
}

func (u *Unavailable) DeleteResource(context.Context, DeleteRef) (*ResourceResult, error) {
	return nil, unavailableErr(u.reason)
}

func (u *Unavailable) DeleteCollection(context.Context, DeleteCollectionRef) (*DeleteCollectionResult, error) {
	return nil, unavailableErr(u.reason)
}

func (u *Unavailable) ExecInPod(context.Context, string, string, *string, []string) (*ExecOutcome, error) {
	return nil, unavailableErr(u.reason)
}

func (u *Unavailable) ExecResource(context.Context, ExecRef, *string, []string) (*ExecOutcome, error) {
	return nil, unavailableErr(u.reason)
}

func (u *Unavailable) AttachResource(context.Context, AttachRef, *string, *string, int) (*AttachOutcome, error) {
	return nil, unavailableErr(u.reason)
}
