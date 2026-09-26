package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dlddu/homelab-k3s-mcp/internal/gatekeeper"
	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
)

// portForwardArgs is AC14's one shape: a pod coordinate, the port, the bytes to
// send, and the read window.
type portForwardArgs struct {
	ref         k8s.PortForwardRef
	port        int
	payload     []byte
	readSeconds int
}

// parsePortForwardTarget reads AC14's arguments. Both the pair resolver and the
// handler run it, for the reason parseAttachTarget states: authorize runs before
// the handler, so a call the handler would reject has already cost an approval
// request by then, and pair resolution is the only thing that runs earlier.
// Scenario 14 asks for over-cap arguments to be refused, and a refusal that
// arrives after an operator has approved reaching a port is not that refusal.
func parsePortForwardTarget(obj map[string]any) (portForwardArgs, *rpcErr) {
	apiVersion := optionalString(obj, "apiVersion")
	if apiVersion == nil {
		return portForwardArgs{}, errf(-32602, "apiVersion is required (e.g. \"v1\")")
	}
	kind := optionalString(obj, "kind")
	if kind == nil {
		return portForwardArgs{}, errf(-32602, "kind is required (call api_resources for the kinds this cluster serves)")
	}
	name := optionalString(obj, "name")
	if name == nil {
		return portForwardArgs{}, errf(-32602, "name is required; this tool forwards to one named pod, not a selection")
	}
	if sub, _ := obj["subresource"].(string); sub != "" {
		return portForwardArgs{}, errf(-32602, "resource_port_forward always exercises pods/portforward; name the pod with apiVersion, kind and name, and pass no subresource")
	}

	portValue, present := obj["port"]
	if !present {
		return portForwardArgs{}, errf(-32602, "port is required; this tool opens one port, and which one is what the approval screen asks about")
	}
	portNumber, ok := intValue(portValue)
	if !ok {
		return portForwardArgs{}, errf(-32602, "port must be an integer")
	}
	if portNumber < 1 || portNumber > 65535 {
		return portForwardArgs{}, errf(-32602, "port must be between 1 and 65535; %d was asked for", portNumber)
	}

	readSeconds := k8s.PortForwardDefaultReadSeconds
	if v, present := obj["readSeconds"]; present {
		n, ok := intValue(v)
		if !ok {
			return portForwardArgs{}, errf(-32602, "readSeconds must be an integer number of seconds")
		}
		if n < 1 || n > k8s.PortForwardMaxReadSeconds {
			return portForwardArgs{}, errf(-32602, "readSeconds must be between 1 and %d (prd-resource-generic AC14); %d was asked for", k8s.PortForwardMaxReadSeconds, n)
		}
		readSeconds = int(n)
	}

	payload, rerr := parsePortForwardPayload(obj)
	if rerr != nil {
		return portForwardArgs{}, rerr
	}

	return portForwardArgs{
		ref: k8s.PortForwardRef{
			APIVersion: *apiVersion,
			Kind:       *kind,
			Namespace:  optionalString(obj, "namespace"),
			Name:       *name,
		},
		port:        int(portNumber),
		payload:     payload,
		readSeconds: readSeconds,
	}, nil
}

// parsePortForwardPayload reads the bytes to send. There are two spellings
// because AC14 says bytes and JSON has no way to say them: `payload` covers the
// text case, which is nearly all of what is reached over a forwarded port, and
// `payloadBase64` covers the rest without making every caller encode a plain
// HTTP request. Passing both is refused rather than resolved in favour of one —
// a caller who sent two payloads meant one of them, and guessing which would send
// bytes nobody approved.
//
// Omitting both is allowed and means "open it, send nothing, read what it says":
// a banner-speaking service answers on connect, and there is nothing to send it.
func parsePortForwardPayload(obj map[string]any) ([]byte, *rpcErr) {
	raw, hasRaw := obj["payload"]
	encoded, hasEncoded := obj["payloadBase64"]
	if hasRaw && hasEncoded {
		return nil, errf(-32602, "pass payload or payloadBase64, not both; they are two spellings of the same argument")
	}
	if hasRaw {
		s, ok := raw.(string)
		if !ok {
			return nil, errf(-32602, "payload must be a string; use payloadBase64 for bytes that are not text")
		}
		return []byte(s), nil
	}
	if hasEncoded {
		s, ok := encoded.(string)
		if !ok {
			return nil, errf(-32602, "payloadBase64 must be a base64 string")
		}
		decoded, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, errf(-32602, "payloadBase64 is not valid base64: %v", err)
		}
		return decoded, nil
	}
	return nil, nil
}

// portForwardPairs is genericPairs("create") with AC14's own argument checks run
// first, for the reason attachPairs states. The pair the call exercises is
// create on ⟨pods plural⟩/portforward — forwarding is something the call does to
// the pod, so the resource name hangs the subresource off the coordinate.
func portForwardPairs() func(json.RawMessage) ([]gatekeeper.Pair, error) {
	return func(rawArgs json.RawMessage) ([]gatekeeper.Pair, error) {
		obj, ok := decodeObject(rawArgs)
		if !ok {
			return nil, fmt.Errorf("arguments must be an object")
		}
		if _, rerr := parsePortForwardTarget(obj); rerr != nil {
			return nil, errors.New(rerr.message)
		}
		apiVersion, _ := obj["apiVersion"].(string)
		kind, _ := obj["kind"].(string)
		coordinate, err := json.Marshal(map[string]any{
			"apiVersion":  apiVersion,
			"kind":        kind,
			"subresource": "portforward",
		})
		if err != nil {
			return nil, fmt.Errorf("port forward coordinate cannot be encoded")
		}
		return genericPairs("create")(coordinate)
	}
}

// resourcePortForward makes the single round trip after the approval.
func (h *Handler) resourcePortForward(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	obj, ok := decodeObject(raw)
	if !ok {
		return nil, errf(-32602, "arguments must be an object")
	}
	args, rerr := parsePortForwardTarget(obj)
	if rerr != nil {
		return nil, rerr
	}

	outcome, err := h.k8s.PortForwardResource(ctx, args.ref, args.port, args.payload, args.readSeconds)
	if err != nil {
		return toolError(ctx, err), nil
	}

	return successResult(map[string]any{
		"apiVersion":        args.ref.APIVersion,
		"kind":              args.ref.Kind,
		"namespace":         args.ref.Namespace,
		"name":              args.ref.Name,
		"pod":               outcome.Pod,
		"port":              outcome.Port,
		"bytesSent":         outcome.BytesSent,
		"readSeconds":       outcome.ReadSeconds,
		"response":          outcome.Response,
		"responseEncoding":  outcome.ResponseEncoding,
		"responseTruncated": outcome.ResponseTruncated,
		"tunnelClosed":      outcome.TunnelClosed,
	}), nil
}
