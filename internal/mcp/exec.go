package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dlddu/homelab-k3s-mcp/internal/gatekeeper"
	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
)

// parseExecTarget reads AC12's one shape: one pod coordinate, one command
// array, an optional container. Both sides run it, for the reason
// parseUpdateTarget states: authorize runs before the handler, so a call the
// handler would reject has already cost an approval request by then, and pair
// resolution is the only thing that runs earlier.
//
// What is deliberately absent is a container count check: whether the pod has
// more than one container is a cluster fact no argument carries, so the
// apiserver's own refusal (which names the candidates) is what answers
// scenario 11's "container 누락이 거부되고 후보 이름이 제시됨" — after an approval
// was spent, the same place scale's DaemonSet refusal lives.
func parseExecTarget(obj map[string]any) (k8s.ExecRef, []string, *string, *rpcErr) {
	apiVersion := optionalString(obj, "apiVersion")
	if apiVersion == nil {
		return k8s.ExecRef{}, nil, nil, errf(-32602, "apiVersion is required (e.g. \"v1\")")
	}
	kind := optionalString(obj, "kind")
	if kind == nil {
		return k8s.ExecRef{}, nil, nil, errf(-32602, "kind is required (call api_resources for the kinds this cluster serves)")
	}
	name := optionalString(obj, "name")
	if name == nil {
		return k8s.ExecRef{}, nil, nil, errf(-32602, "name is required; this tool runs one command in one named pod, not a selection")
	}
	if sub, _ := obj["subresource"].(string); sub != "" {
		return k8s.ExecRef{}, nil, nil, errf(-32602, "resource_exec always exercises pods/exec; name the pod with apiVersion, kind and name, and pass no subresource")
	}

	commandValue, present := obj["command"]
	if !present {
		return k8s.ExecRef{}, nil, nil, errf(-32602, "command is required: the array of argv the pod runs")
	}
	items, ok := commandValue.([]any)
	if !ok || len(items) == 0 {
		return k8s.ExecRef{}, nil, nil, errf(-32602, "command must be a non-empty array of strings")
	}
	command := make([]string, 0, len(items))
	for i, item := range items {
		part, ok := item.(string)
		if !ok || part == "" {
			return k8s.ExecRef{}, nil, nil, errf(-32602, "command element %d must be a non-empty string", i)
		}
		command = append(command, part)
	}

	container := optionalString(obj, "container")
	return k8s.ExecRef{
		APIVersion: *apiVersion,
		Kind:       *kind,
		Namespace:  optionalString(obj, "namespace"),
		Name:       *name,
	}, command, container, nil
}

// execPairs is genericPairs("create") with AC12's own argument checks run
// first, for the reason updatePairs states. The pair the call exercises is
// create on ⟨pods plural⟩/exec — exec is a verb the call performs on the pod,
// so the resource name hangs the subresource off the coordinate the way
// dear_baby_reset_user's declaration spells it.
func execPairs() func(json.RawMessage) ([]gatekeeper.Pair, error) {
	return func(rawArgs json.RawMessage) ([]gatekeeper.Pair, error) {
		obj, ok := decodeObject(rawArgs)
		if !ok {
			return nil, fmt.Errorf("arguments must be an object")
		}
		if _, _, _, rerr := parseExecTarget(obj); rerr != nil {
			return nil, errors.New(rerr.message)
		}
		apiVersion, _ := obj["apiVersion"].(string)
		kind, _ := obj["kind"].(string)
		coordinate, err := json.Marshal(map[string]any{
			"apiVersion":  apiVersion,
			"kind":        kind,
			"subresource": "exec",
		})
		if err != nil {
			return nil, fmt.Errorf("exec coordinate cannot be encoded")
		}
		return genericPairs("create")(coordinate)
	}
}

// resourceExec runs one command in one named pod after the approval. The
// approval screen already carries the command verbatim (AC3), and AC6's
// re-read ties the approval to the pod's uid — a pod recreated under the same
// name is refused before anything runs.
func (h *Handler) resourceExec(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	obj, ok := decodeObject(raw)
	if !ok {
		return nil, errf(-32602, "arguments must be an object")
	}
	ref, command, container, rerr := parseExecTarget(obj)
	if rerr != nil {
		return nil, rerr
	}

	outcome, err := h.k8s.ExecResource(ctx, ref, container, command)
	if err != nil {
		return toolError(ctx, err), nil
	}

	payload := map[string]any{
		"apiVersion":      ref.APIVersion,
		"kind":            ref.Kind,
		"namespace":       ref.Namespace,
		"name":            ref.Name,
		"container":       container,
		"pod":             outcome.Pod,
		"command":         command,
		"exitCode":        outcome.ExitCode,
		"stdout":          outcome.Stdout,
		"stderr":          outcome.Stderr,
		"success":         outcome.Success,
		"stdoutTruncated": outcome.StdoutTruncated,
		"stderrTruncated": outcome.StderrTruncated,
	}
	if outcome.TimeLimited {
		payload["timeLimited"] = true
	}
	return map[string]any{
		"content":           []any{map[string]any{"type": "text", "text": prettyJSON(payload)}},
		"structuredContent": payload,
		"isError":           !outcome.Success,
	}, nil
}
