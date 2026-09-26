//go:build e2e_undeclared_tool

// The startup variant for test-approval-gate.md#시나리오 1 (c). This file is
// compiled only under the e2e_undeclared_tool build tag.
//
// (c) asks for a deployment that registers a tool which declares none of the
// (verb, resource) pairs it exercises, and asserts that such a deployment does
// not come up. The registry and the advertised list are both compile-time
// constants, so no env knob or manifest edit can produce that variant — the
// only place it can exist is behind a build tag. The default build (`go build
// ./...`, the Dockerfile's runtime stage, the published image) never sees this
// file, so the shipped binary's tool surface is unchanged; what the tag adds is
// a way to *exercise* the startup check that already ships, not a way around it.
//
// The tool is advertised as well as registered on purpose. validateRegistry
// reports every problem it finds, so a tool that were registered without being
// advertised would also trip "registered but not advertised" — and then the
// e2e could not tell which of the two rules refused the deployment. Advertising
// it leaves the name sets in agreement, which narrows the refusal to
// validateDeclarations alone.
//
// The handler reaches the apiserver for the same reason: the scenario is about
// a tool that exercises a permission it never declared, not about an inert
// stub. It never runs — the process exits before it serves — but a handler that
// touched nothing would be honestly describable by a touchesNothing marker, and
// then the variant would be arguing with itself.

package mcp

import (
	"context"
	"encoding/json"

	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
)

// UndeclaredProbeTool is the name test-approval-gate.md#시나리오 1 (c) looks for
// in the refusal. tests/integration/approval_gate_ac1.py pins the same spelling.
const UndeclaredProbeTool = "e2e_undeclared_probe"

func init() {
	toolRegistry[UndeclaredProbeTool] = toolEntry{
		handle: func(h *Handler, ctx context.Context, _ json.RawMessage) (any, *rpcErr) {
			if _, err := h.k8s.ListResources(ctx, k8s.ListQuery{}); err != nil {
				return toolError(ctx, err), nil
			}
			return toolText("unreachable: this deployment never finishes starting", false), nil
		},
	}
	extraAdvertisedTools = append(extraAdvertisedTools, UndeclaredProbeTool)
}
