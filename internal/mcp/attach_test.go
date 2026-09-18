package mcp

import (
	"strings"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/gatekeeper"
	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
)

// TestAttachJoinsTheRunningProcess covers the half of scenario 13 that does not
// need a cluster: what the tool hands down, what pair it spends, and what it
// gives back. The other half — that the output came from the existing process
// rather than a new one — is only observable against a real pod, and is the
// integration case's to prove.
func TestAttachJoinsTheRunningProcess(t *testing.T) {
	t.Run("hands the coordinate, the container and the window to the cluster", func(t *testing.T) {
		gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
		h, fake := testHandler(t, gate, toolRegistry)

		result, rerr := callTool(t, h, "resource_attach", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","container":"chatty","readSeconds":3}`)
		if rerr != nil {
			t.Fatalf("tools/call = %v, want the approved attach to run", rerr)
		}

		ref, container, stdin, readSeconds := fake.attach()
		if ref.APIVersion != "v1" || ref.Kind != "Pod" || ref.Namespace == nil || *ref.Namespace != "ops" || ref.Name != "api" {
			t.Errorf("attach coordinate = %+v, want v1 Pod ops/api", ref)
		}
		if container == nil || *container != "chatty" {
			t.Errorf("attach container = %v, want chatty", container)
		}
		if stdin != nil {
			t.Errorf("attach stdin = %q, want nil — none was asked for", *stdin)
		}
		if readSeconds != 3 {
			t.Errorf("readSeconds = %d, want 3", readSeconds)
		}
		if fake.count() != 1 {
			t.Errorf("kubernetes calls = %d, want 1", fake.count())
		}

		payload := result.(map[string]any)["structuredContent"].(map[string]any)
		if got := payload["stdout"]; got != "tick\n" {
			t.Errorf("stdout = %v, want the attached process's output", got)
		}
		if got := payload["stdinWritten"]; got != false {
			t.Errorf("stdinWritten = %v, want false", got)
		}
		if got := gate.calls[0].Pair.String(); got != "create on pods/attach" {
			t.Errorf("gated pair = %q, want %q", got, "create on pods/attach")
		}
	})

	t.Run("an omitted window takes AC13's default rather than none", func(t *testing.T) {
		h, fake := approvingHandler(t)
		if _, rerr := callTool(t, h, "resource_attach", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api"}`); rerr != nil {
			t.Fatalf("tools/call = %v, want the approved attach to run", rerr)
		}
		if _, _, _, readSeconds := fake.attach(); readSeconds != k8s.AttachDefaultReadSeconds {
			t.Errorf("readSeconds = %d, want the default %d", readSeconds, k8s.AttachDefaultReadSeconds)
		}
	})

	t.Run("stdin reaches the attached process and is reported as written", func(t *testing.T) {
		h, fake := approvingHandler(t)
		result, rerr := callTool(t, h, "resource_attach", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","stdin":"ping\n"}`)
		if rerr != nil {
			t.Fatalf("tools/call = %v, want the approved attach to run", rerr)
		}
		_, _, stdin, _ := fake.attach()
		if stdin == nil || *stdin != "ping\n" {
			t.Errorf("attach stdin = %v, want the payload verbatim", stdin)
		}
		payload := result.(map[string]any)["structuredContent"].(map[string]any)
		if payload["stdinWritten"] != true {
			t.Errorf("stdinWritten = %v, want true — a process that ignores its input says nothing about whether it arrived", payload["stdinWritten"])
		}
	})

	t.Run("an empty stdin is a payload, not an absence", func(t *testing.T) {
		h, fake := approvingHandler(t)
		if _, rerr := callTool(t, h, "resource_attach", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","stdin":""}`); rerr != nil {
			t.Fatalf("tools/call = %v, want the approved attach to run", rerr)
		}
		if _, _, stdin, _ := fake.attach(); stdin == nil {
			t.Error("attach stdin = nil, want an empty string — writing nothing and not attaching stdin are different calls")
		}
	})

	t.Run("stream truncation is marked, not silent", func(t *testing.T) {
		h, fake := approvingHandler(t)
		fake.attachOutcome = &k8s.AttachOutcome{
			Pod:             "api",
			Stdout:          strings.Repeat("y", 100),
			ReadSeconds:     5,
			StdoutTruncated: true,
			StderrTruncated: true,
		}
		result, rerr := callTool(t, h, "resource_attach", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api"}`)
		if rerr != nil {
			t.Fatalf("tools/call = %v, want a capped attach to still answer", rerr)
		}
		payload := result.(map[string]any)["structuredContent"].(map[string]any)
		if payload["stdoutTruncated"] != true || payload["stderrTruncated"] != true {
			t.Errorf("truncation flags = %v/%v, want both true", payload["stdoutTruncated"], payload["stderrTruncated"])
		}
	})
}

// TestAttachRefusalsCostNoApproval is scenario 13's `readSeconds=31` and its
// neighbours. Each has to be refused with the kubernetes call count still at
// zero: a refusal that arrives after an operator approved an attach is not the
// refusal AC13 asked for.
func TestAttachRefusalsCostNoApproval(t *testing.T) {
	refusals := []struct {
		name string
		args string
		want string
	}{
		{"readSeconds over the cap", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","readSeconds":31}`, "between 1 and 30"},
		{"readSeconds below one", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","readSeconds":0}`, "between 1 and 30"},
		{"readSeconds not an integer", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","readSeconds":"3"}`, "must be an integer"},
		{"stdin not a string", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","stdin":3}`, "stdin must be a string"},
		{"missing name", `{"apiVersion":"v1","kind":"Pod","namespace":"ops"}`, "name is required"},
		{"missing kind", `{"apiVersion":"v1","namespace":"ops","name":"api"}`, "kind is required"},
		{"explicit subresource", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","subresource":"attach"}`, "pass no subresource"},
		{"a command belongs to exec", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","command":["echo","hi"]}`, "takes no command"},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
			h, fake := testHandler(t, gate, toolRegistry)
			_, rerr := callTool(t, h, "resource_attach", tc.args)
			if rerr == nil {
				t.Fatal("tools/call = nil error, want a refusal")
			}
			if !strings.Contains(rerr.message, tc.want) {
				t.Errorf("error = %q, want it to mention %q", rerr.message, tc.want)
			}
			if fake.count() != 0 {
				t.Errorf("kubernetes calls = %d, want 0 — a rejected argument must not reach the cluster", fake.count())
			}
			if len(gate.calls) != 0 {
				t.Errorf("approval requests = %d, want 0 — the refusal has to land before the operator is asked", len(gate.calls))
			}
		})
	}
}

// TestAttachContextCarriesTheStdin is AC3's side of the same call: the approval
// screen has to show what is about to be written into a running process, or the
// operator is approving a coordinate rather than an action.
func TestAttachContextCarriesTheStdin(t *testing.T) {
	reader := newStateReader()
	gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
	h, _, _ := testHandlerReading(t, gate, toolRegistry, reader)

	args := `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","container":"chatty","stdin":"shutdown now\n"}`
	if _, rerr := callTool(t, h, "resource_attach", args); rerr != nil {
		t.Fatalf("tools/call = %v, want the approved attach to run", rerr)
	}
	ctx := gate.context(0)
	for _, want := range []string{"shutdown now", "chatty", "create on pods/attach", "v1 Pod ops/api"} {
		if !strings.Contains(ctx, want) {
			t.Errorf("approval context is missing %q:\n%s", want, ctx)
		}
	}
}
