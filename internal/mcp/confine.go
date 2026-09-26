package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/dlddu/homelab-k3s-mcp/internal/gatekeeper"
	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
)

// resourceName turns one call's coordinate into the RBAC resource name a
// declaration is written in. It is apimachinery's guess rather than discovery's
// answer for the reason genericPairs states, and it lives here — in one
// function both the declaration side and the confinement side call — because
// the two have to agree exactly. A second normalisation would drift, and the
// drift would read as "this tool exercises something it did not declare" on a
// call that is perfectly correct.
func resourceName(apiVersion, kind, subresource string) (string, error) {
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return "", fmt.Errorf("apiVersion %q is not a group/version", apiVersion)
	}
	plural, _ := meta.UnsafeGuessKindToResource(gv.WithKind(kind))
	resource := plural.Resource
	if subresource != "" {
		resource += "/" + subresource
	}
	return resource, nil
}

// invoke runs one tool with the kubernetes client it receives confined to what
// this call declared — the call-time half of prd-approval-gate AC1.
//
// The confinement is built here, at the dispatcher, for the same reason the
// gate call is: a handler that narrows its own client is a handler that can
// forget to. Every path that reaches a handler goes through this function, and
// a handler only ever sees the confined client, because it is handed a copy of
// the Handler whose k8s field is the confined one.
//
// gateReader and gateCollectionReader are deliberately copied untouched. Their
// scope comes from AC11's own table rather than from the tool's declaration
// (AC1 says so in as many words), and confining them to the tool's pairs would
// stop the gate reading the object it has to describe before anyone has
// approved anything.
func (h *Handler) invoke(ctx context.Context, name string, entry toolEntry, rawArgs json.RawMessage) (any, *rpcErr) {
	confined, err := confine(h.k8s, name, entry.decl, rawArgs)
	if err != nil {
		return nil, errf(-32602, "%s", err.Error())
	}
	scoped := *h
	scoped.k8s = confined
	return entry.handle(&scoped, ctx, rawArgs)
}

// confine builds the client for one call of one tool.
//
// A declaration resolved per call rather than per tool is the point: the
// generic tools name their kind in the arguments, so "what this tool may do"
// is only answerable once there is a call. A marker tool (AC1's ⑵) gets a
// client that permits what the marker's kind permits and nothing else —
// discovery for discoveryOnly, nothing at all for touchesNothing.
func confine(inner k8s.Service, tool string, d toolDeclaration, rawArgs json.RawMessage) (k8s.Service, error) {
	if marker := d.noResourcePermission; marker != nil {
		return &confinedService{
			inner:     inner,
			tool:      tool,
			discovery: marker.kind == discoveryOnly,
		}, nil
	}
	pairs, err := d.callPairs(rawArgs)
	if err != nil {
		return nil, err
	}
	allowed := make(map[gatekeeper.Pair]bool, len(pairs))
	for _, p := range pairs {
		allowed[p] = true
	}
	return &confinedService{inner: inner, tool: tool, allowed: allowed}, nil
}

// confinedService is a k8s.Service that answers only for the pairs one call
// declared and refuses everything else before the request is built.
//
// It wraps the whole interface rather than the few methods a tool happens to
// use today: the guarantee AC1 asks for is about the permissions a handler
// *can* reach, and a passthrough for any method would be a hole that opens the
// day some handler calls it.
type confinedService struct {
	inner k8s.Service
	tool  string

	// allowed is the pair set of this call. It is nil for a marker tool, which
	// is the same thing as empty: a marker says the tool exercises no resource
	// permission, so no pair can be in the set.
	allowed map[gatekeeper.Pair]bool

	// discovery is the one thing a marker can still permit — AC1 carves
	// discovery out of "resource permission" by name, so discoveryOnly has to
	// pass APIResources while touchesNothing does not.
	discovery bool
}

var _ k8s.Service = (*confinedService)(nil)

// permit is the check every coordinate-addressed method runs: name the pair
// this request would exercise, then refuse unless the call declared it.
func (c *confinedService) permit(verb, apiVersion, kind, subresource string) error {
	resource, err := resourceName(apiVersion, kind, subresource)
	if err != nil {
		return fmt.Errorf("refusing %s: %s", c.tool, err.Error())
	}
	return c.allow(gatekeeper.Pair{Verb: verb, Resource: resource})
}

func (c *confinedService) allow(p gatekeeper.Pair) error {
	if c.allowed[p] {
		return nil
	}
	return fmt.Errorf(
		"refusing %s: this call declared %s, and %s is outside it — "+
			"the request is not sent to the apiserver (prd-approval-gate AC1)",
		c.tool, declaredText(c.allowed, c.discovery), p.String())
}

// declaredText renders what the call was allowed to do, so a refusal says
// which of the two mistakes it caught: a declaration that is missing a pair,
// or a handler exercising one it should not.
func declaredText(allowed map[gatekeeper.Pair]bool, discovery bool) string {
	if len(allowed) == 0 {
		if discovery {
			return "discovery only"
		}
		return "no kubernetes access"
	}
	pairs := make([]gatekeeper.Pair, 0, len(allowed))
	for p := range allowed {
		pairs = append(pairs, p)
	}
	return pairsText(pairs)
}

func (c *confinedService) APIResources(ctx context.Context) ([]k8s.APIResource, error) {
	if !c.discovery {
		return nil, fmt.Errorf(
			"refusing %s: this call declared %s, and discovery is outside it — "+
				"the request is not sent to the apiserver (prd-approval-gate AC1)",
			c.tool, declaredText(c.allowed, c.discovery))
	}
	return c.inner.APIResources(ctx)
}

func (c *confinedService) ListResources(ctx context.Context, query k8s.ListQuery) (*k8s.ListResult, error) {
	if err := c.permit("list", query.APIVersion, query.Kind, ""); err != nil {
		return nil, err
	}
	return c.inner.ListResources(ctx, query)
}

func (c *confinedService) WatchResources(ctx context.Context, query k8s.WatchQuery) (*k8s.WatchResult, error) {
	if err := c.permit("watch", query.APIVersion, query.Kind, ""); err != nil {
		return nil, err
	}
	return c.inner.WatchResources(ctx, query)
}

func (c *confinedService) GetResource(ctx context.Context, ref k8s.ResourceRef) (*k8s.ResourceResult, error) {
	if err := c.permit("get", ref.APIVersion, ref.Kind, ref.Subresource); err != nil {
		return nil, err
	}
	return c.inner.GetResource(ctx, ref)
}

func (c *confinedService) CreateResource(ctx context.Context, ref k8s.CreateRef) (*k8s.ResourceResult, error) {
	if err := c.permit("create", ref.APIVersion, ref.Kind, ""); err != nil {
		return nil, err
	}
	return c.inner.CreateResource(ctx, ref)
}

func (c *confinedService) UpdateResource(ctx context.Context, ref k8s.UpdateRef) (*k8s.ResourceResult, error) {
	if err := c.permit("update", ref.APIVersion, ref.Kind, ref.Subresource); err != nil {
		return nil, err
	}
	return c.inner.UpdateResource(ctx, ref)
}

// PatchResource asks about the object and never a subresource, because PatchRef
// addresses the object: resource_patch refuses a subresource argument before it
// builds one. Deriving the pair from what the request actually carries is what
// makes this check the one AC1 wants — a declaration that named a subresource
// while the request patches the object is exactly the mismatch it asks to
// catch, not an exception to wave through.
func (c *confinedService) PatchResource(ctx context.Context, ref k8s.PatchRef) (*k8s.ResourceResult, error) {
	if err := c.permit("patch", ref.APIVersion, ref.Kind, ""); err != nil {
		return nil, err
	}
	return c.inner.PatchResource(ctx, ref)
}

func (c *confinedService) DeleteResource(ctx context.Context, ref k8s.DeleteRef) (*k8s.ResourceResult, error) {
	if err := c.permit("delete", ref.APIVersion, ref.Kind, ""); err != nil {
		return nil, err
	}
	return c.inner.DeleteResource(ctx, ref)
}

func (c *confinedService) DeleteCollection(ctx context.Context, ref k8s.DeleteCollectionRef) (*k8s.DeleteCollectionResult, error) {
	if err := c.permit("deletecollection", ref.APIVersion, ref.Kind, ""); err != nil {
		return nil, err
	}
	return c.inner.DeleteCollection(ctx, ref)
}

// ExecInPod needs both of its pairs, and it is the one method whose pairs are
// constants: it finds the pod by label rather than by coordinate, so there is
// no kind in the arguments to guess a resource name from. A tool that declared
// only the exec half would have its selector read pass unnoticed, which is the
// silent gap the two-pair check closes.
func (c *confinedService) ExecInPod(ctx context.Context, namespace, labelSelector string, container *string, command []string) (*k8s.ExecOutcome, error) {
	for _, p := range []gatekeeper.Pair{{Verb: "list", Resource: "pods"}, {Verb: "create", Resource: "pods/exec"}} {
		if err := c.allow(p); err != nil {
			return nil, err
		}
	}
	return c.inner.ExecInPod(ctx, namespace, labelSelector, container, command)
}

func (c *confinedService) ExecResource(ctx context.Context, ref k8s.ExecRef, container *string, command []string) (*k8s.ExecOutcome, error) {
	if err := c.permit("create", ref.APIVersion, ref.Kind, "exec"); err != nil {
		return nil, err
	}
	return c.inner.ExecResource(ctx, ref, container, command)
}

func (c *confinedService) AttachResource(ctx context.Context, ref k8s.AttachRef, container *string, stdin *string, readSeconds int) (*k8s.AttachOutcome, error) {
	if err := c.permit("create", ref.APIVersion, ref.Kind, "attach"); err != nil {
		return nil, err
	}
	return c.inner.AttachResource(ctx, ref, container, stdin, readSeconds)
}

func (c *confinedService) PortForwardResource(ctx context.Context, ref k8s.PortForwardRef, port int, payload []byte, readSeconds int) (*k8s.PortForwardOutcome, error) {
	if err := c.permit("create", ref.APIVersion, ref.Kind, "portforward"); err != nil {
		return nil, err
	}
	return c.inner.PortForwardResource(ctx, ref, port, payload, readSeconds)
}

// ProxyResource reads its verb from the HTTP method the same way proxyPairs
// does (AC15), so a GET through the proxy is confined as a get on <kind>/proxy
// and a POST as a create — the method decides both sides or neither.
func (c *confinedService) ProxyResource(ctx context.Context, ref k8s.ProxyRef, method, path string, body []byte, contentType string, readSeconds int) (*k8s.ProxyOutcome, error) {
	if err := c.permit(k8s.ProxyVerbForMethod(method), ref.APIVersion, ref.Kind, "proxy"); err != nil {
		return nil, err
	}
	return c.inner.ProxyResource(ctx, ref, method, path, body, contentType, readSeconds)
}
