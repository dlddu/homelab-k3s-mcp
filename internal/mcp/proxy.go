package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/dlddu/homelab-k3s-mcp/internal/gatekeeper"
	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
)

// proxyArgs is AC15's one shape: an object coordinate, the HTTP method, the
// path to reach, and the bytes to send.
type proxyArgs struct {
	ref         k8s.ProxyRef
	method      string
	path        string
	body        []byte
	contentType string
	readSeconds int
}

// parseProxyTarget reads AC15's arguments. Both the pair resolver and the
// handler run it, for the reason parsePortForwardTarget states: authorize runs
// before the handler, so a call the handler would reject has already cost an
// approval request by then. That matters more here than anywhere else — this is
// the tool that can reach kubelet's /exec, so an argument typo must not be what
// puts a high-power path on an operator's screen.
func parseProxyTarget(obj map[string]any) (proxyArgs, *rpcErr) {
	apiVersion := optionalString(obj, "apiVersion")
	if apiVersion == nil {
		return proxyArgs{}, errf(-32602, "apiVersion is required (e.g. \"v1\")")
	}
	kind := optionalString(obj, "kind")
	if kind == nil {
		return proxyArgs{}, errf(-32602, "kind is required; this tool proxies to Pod, Service or Node")
	}
	name := optionalString(obj, "name")
	if name == nil {
		return proxyArgs{}, errf(-32602, "name is required; this tool reaches one named object, not a selection")
	}
	if sub, _ := obj["subresource"].(string); sub != "" {
		return proxyArgs{}, errf(-32602, "resource_proxy always exercises ⟨kind⟩/proxy; name the object with apiVersion, kind and name, and pass no subresource")
	}

	method := optionalString(obj, "method")
	if method == nil {
		return proxyArgs{}, errf(-32602, "method is required; the verb this call spends follows it (prd-resource-generic AC15)")
	}
	upper := strings.ToUpper(*method)
	if k8s.ProxyVerbForMethod(upper) == "" {
		return proxyArgs{}, errf(-32602, "method must be one of GET, POST, PUT, PATCH, DELETE; %q has no verb to map to", *method)
	}

	path := optionalString(obj, "path")
	if path == nil {
		return proxyArgs{}, errf(-32602, "path is required; the (verb, resource) pair cannot say what this call does, and the path is what can")
	}
	if !strings.HasPrefix(*path, "/") {
		return proxyArgs{}, errf(-32602, "path must start with \"/\"; %q would be read relative to the proxy root and reach somewhere else", *path)
	}

	readSeconds := k8s.ProxyDefaultReadSeconds
	if v, present := obj["readSeconds"]; present {
		n, ok := intValue(v)
		if !ok {
			return proxyArgs{}, errf(-32602, "readSeconds must be an integer number of seconds")
		}
		if n < 1 || n > k8s.ProxyMaxReadSeconds {
			return proxyArgs{}, errf(-32602, "readSeconds must be between 1 and %d (prd-resource-generic AC15); %d was asked for", k8s.ProxyMaxReadSeconds, n)
		}
		readSeconds = int(n)
	}

	body, rerr := parseProxyBody(obj)
	if rerr != nil {
		return proxyArgs{}, rerr
	}
	contentType := ""
	if v := optionalString(obj, "contentType"); v != nil {
		contentType = *v
	}

	return proxyArgs{
		ref: k8s.ProxyRef{
			APIVersion: *apiVersion,
			Kind:       *kind,
			Namespace:  optionalString(obj, "namespace"),
			Name:       *name,
		},
		method:      upper,
		path:        *path,
		body:        body,
		contentType: contentType,
		readSeconds: readSeconds,
	}, nil
}

// parseProxyBody reads the bytes to send, in the two spellings
// parsePortForwardPayload uses and for the same reason: AC15 says a body and
// JSON has no way to say bytes. Passing both is refused rather than resolved in
// favour of one — a caller who sent two bodies meant one of them, and guessing
// which would send bytes nobody approved.
func parseProxyBody(obj map[string]any) ([]byte, *rpcErr) {
	raw, hasRaw := obj["body"]
	encoded, hasEncoded := obj["bodyBase64"]
	if hasRaw && hasEncoded {
		return nil, errf(-32602, "pass body or bodyBase64, not both; they are two spellings of the same argument")
	}
	if hasRaw {
		s, ok := raw.(string)
		if !ok {
			return nil, errf(-32602, "body must be a string; use bodyBase64 for bytes that are not text")
		}
		return []byte(s), nil
	}
	if hasEncoded {
		s, ok := encoded.(string)
		if !ok {
			return nil, errf(-32602, "bodyBase64 must be a base64 string")
		}
		decoded, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, errf(-32602, "bodyBase64 is not valid base64: %v", err)
		}
		return decoded, nil
	}
	return nil, nil
}

// proxyPairs reports the single pair this call exercises, which is the one
// place in the registry where the verb comes out of the arguments rather than
// being fixed by the tool. genericPairs cannot be reused for it: that helper
// takes its verb as a constant, and AC15's whole point is that the verb follows
// the HTTP method. The resource name is apimachinery's guess from the kind, for
// the reason genericPairs states — the gate has to reach its verdict before the
// apiserver is touched.
func proxyPairs() func(json.RawMessage) ([]gatekeeper.Pair, error) {
	return func(rawArgs json.RawMessage) ([]gatekeeper.Pair, error) {
		obj, ok := decodeObject(rawArgs)
		if !ok {
			return nil, fmt.Errorf("arguments must be an object")
		}
		args, rerr := parseProxyTarget(obj)
		if rerr != nil {
			return nil, errors.New(rerr.message)
		}
		gv, err := schema.ParseGroupVersion(args.ref.APIVersion)
		if err != nil {
			return nil, fmt.Errorf("apiVersion %q is not a group/version", args.ref.APIVersion)
		}
		plural, _ := meta.UnsafeGuessKindToResource(gv.WithKind(args.ref.Kind))
		return []gatekeeper.Pair{{
			Verb:     k8s.ProxyVerbForMethod(args.method),
			Resource: plural.Resource + "/proxy",
		}}, nil
	}
}

// resourceProxy makes the round trip after the approval. Nothing here inspects
// the path, and the omission is load-bearing: a refusal added at this line
// would be the path allowlist AC15 declined to build, arriving by the back door
// and defeating the defence AC15 chose in its place.
func (h *Handler) resourceProxy(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	obj, ok := decodeObject(raw)
	if !ok {
		return nil, errf(-32602, "arguments must be an object")
	}
	args, rerr := parseProxyTarget(obj)
	if rerr != nil {
		return nil, rerr
	}

	outcome, err := h.k8s.ProxyResource(ctx, args.ref, args.method, args.path, args.body, args.contentType, args.readSeconds)
	if err != nil {
		return toolError(ctx, err), nil
	}

	return successResult(map[string]any{
		"apiVersion":   args.ref.APIVersion,
		"kind":         args.ref.Kind,
		"namespace":    args.ref.Namespace,
		"name":         args.ref.Name,
		"method":       outcome.Method,
		"verb":         outcome.Verb,
		"path":         outcome.Path,
		"status":       outcome.Status,
		"body":         outcome.Body,
		"bodyEncoding": outcome.BodyEncoding,
		"truncated":    outcome.Truncated,
		"readSeconds":  outcome.ReadSeconds,
	}), nil
}
