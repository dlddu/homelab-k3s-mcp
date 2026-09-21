package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dlddu/homelab-k3s-mcp/internal/gatekeeper"
	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
)

// attachArgs is AC13's one shape: a pod coordinate, an optional container, an
// optional stdin payload, and the read window.
type attachArgs struct {
	ref         k8s.AttachRef
	container   *string
	stdin       *string
	readSeconds int
}

// parseAttachTarget reads AC13's arguments. Both the pair resolver and the
// handler run it, for the reason parseExecTarget states: authorize runs before
// the handler, so a call the handler would reject has already cost an approval
// request by then, and pair resolution is the only thing that runs earlier.
// Scenario 13 asks for `readSeconds=31` to be refused, and a refusal that
// arrives after an operator has approved an attach is not the refusal that was
// asked for.
//
// What is deliberately absent, as in exec, is a container count check: whether
// the pod has more than one container is a cluster fact no argument carries,
// so the apiserver's own refusal (which names the candidates) answers it.
func parseAttachTarget(obj map[string]any) (attachArgs, *rpcErr) {
	apiVersion := optionalString(obj, "apiVersion")
	if apiVersion == nil {
		return attachArgs{}, errf(-32602, "apiVersion is required (e.g. \"v1\")")
	}
	kind := optionalString(obj, "kind")
	if kind == nil {
		return attachArgs{}, errf(-32602, "kind is required (call api_resources for the kinds this cluster serves)")
	}
	name := optionalString(obj, "name")
	if name == nil {
		return attachArgs{}, errf(-32602, "name is required; this tool attaches to one named pod, not a selection")
	}
	if sub, _ := obj["subresource"].(string); sub != "" {
		return attachArgs{}, errf(-32602, "resource_attach always exercises pods/attach; name the pod with apiVersion, kind and name, and pass no subresource")
	}
	if _, present := obj["command"]; present {
		// Refusing is kinder than ignoring. attach joins a process that is
		// already running, so a command argument means the caller wanted
		// resource_exec, and silently dropping it would return the output of
		// something else entirely.
		return attachArgs{}, errf(-32602, "resource_attach takes no command: it joins the process already running in the container. Use resource_exec to start a new one")
	}

	readSeconds := k8s.AttachDefaultReadSeconds
	if v, present := obj["readSeconds"]; present {
		n, ok := intValue(v)
		if !ok {
			return attachArgs{}, errf(-32602, "readSeconds must be an integer number of seconds")
		}
		if n < 1 || n > k8s.AttachMaxReadSeconds {
			return attachArgs{}, errf(-32602, "readSeconds must be between 1 and %d (prd-resource-generic AC13); %d was asked for", k8s.AttachMaxReadSeconds, n)
		}
		readSeconds = int(n)
	}

	var stdin *string
	if v, present := obj["stdin"]; present {
		s, ok := v.(string)
		if !ok {
			return attachArgs{}, errf(-32602, "stdin must be a string; it is written to the attached process once")
		}
		// An empty string is a legitimate payload to write — it is not the same
		// as never attaching stdin at all, which is what omitting the argument
		// means — so it is kept rather than folded into nil the way
		// optionalString would.
		stdin = &s
	}

	return attachArgs{
		ref: k8s.AttachRef{
			APIVersion: *apiVersion,
			Kind:       *kind,
			Namespace:  optionalString(obj, "namespace"),
			Name:       *name,
		},
		container:   optionalString(obj, "container"),
		stdin:       stdin,
		readSeconds: readSeconds,
	}, nil
}

// attachPairs is genericPairs("create") with AC13's own argument checks run
// first, for the reason execPairs states. The pair the call exercises is
// create on ⟨pods plural⟩/attach — attaching is something the call does to the
// pod, so the resource name hangs the subresource off the coordinate.
//
// The pair is the same grade as exec's on purpose: AC13 says so, because a pod
// whose PID 1 is an interactive shell cannot tell the two apart.
func attachPairs() func(json.RawMessage) ([]gatekeeper.Pair, error) {
	return func(rawArgs json.RawMessage) ([]gatekeeper.Pair, error) {
		obj, ok := decodeObject(rawArgs)
		if !ok {
			return nil, fmt.Errorf("arguments must be an object")
		}
		if _, rerr := parseAttachTarget(obj); rerr != nil {
			return nil, errors.New(rerr.message)
		}
		apiVersion, _ := obj["apiVersion"].(string)
		kind, _ := obj["kind"].(string)
		coordinate, err := json.Marshal(map[string]any{
			"apiVersion":  apiVersion,
			"kind":        kind,
			"subresource": "attach",
		})
		if err != nil {
			return nil, fmt.Errorf("attach coordinate cannot be encoded")
		}
		return genericPairs("create")(coordinate)
	}
}

// resourceAttach joins one named pod's streams after the approval. The approval
// screen already carries the stdin payload verbatim (AC3) — it is an argument
// like any other — and AC6's re-read ties the approval to the pod's uid, so a
// pod recreated under the same name is refused before anything is written.
func (h *Handler) resourceAttach(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	obj, ok := decodeObject(raw)
	if !ok {
		return nil, errf(-32602, "arguments must be an object")
	}
	args, rerr := parseAttachTarget(obj)
	if rerr != nil {
		return nil, rerr
	}

	outcome, err := h.k8s.AttachResource(ctx, args.ref, args.container, args.stdin, args.readSeconds)
	if err != nil {
		return toolError(ctx, err), nil
	}

	return successResult(map[string]any{
		"apiVersion":      args.ref.APIVersion,
		"kind":            args.ref.Kind,
		"namespace":       args.ref.Namespace,
		"name":            args.ref.Name,
		"container":       args.container,
		"pod":             outcome.Pod,
		"readSeconds":     outcome.ReadSeconds,
		"stdinWritten":    outcome.StdinWritten,
		"stdout":          outcome.Stdout,
		"stderr":          outcome.Stderr,
		"stdoutTruncated": outcome.StdoutTruncated,
		"stderrTruncated": outcome.StderrTruncated,
	}), nil
}
