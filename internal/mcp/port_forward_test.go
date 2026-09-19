package mcp

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/dlddu/homelab-k3s-mcp/internal/gatekeeper"
	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
)

// TestPortForwardMakesOneRoundTrip covers the half of scenario 14 that does not
// need a cluster: what the tool hands down, what pair it spends, and what it
// gives back. The other half — that the tunnel is really gone afterwards, so a
// second round trip cannot ride the same approval — is only observable against a
// real apiserver, and is the integration case's to prove.
func TestPortForwardMakesOneRoundTrip(t *testing.T) {
	t.Run("hands the coordinate, the port and the payload to the cluster", func(t *testing.T) {
		gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
		h, fake := testHandler(t, gate, toolRegistry)

		args := `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","port":8080,"payload":"GET / HTTP/1.0\r\n\r\n","readSeconds":3}`
		result, rerr := callTool(t, h, "resource_port_forward", args)
		if rerr != nil {
			t.Fatalf("tools/call = %v, want the approved forward to run", rerr)
		}

		ref, port, payload, readSeconds := fake.portForward()
		if ref.APIVersion != "v1" || ref.Kind != "Pod" || ref.Namespace == nil || *ref.Namespace != "ops" || ref.Name != "api" {
			t.Errorf("port forward coordinate = %+v, want v1 Pod ops/api", ref)
		}
		if port != 8080 {
			t.Errorf("port = %d, want 8080", port)
		}
		if string(payload) != "GET / HTTP/1.0\r\n\r\n" {
			t.Errorf("payload = %q, want the bytes verbatim", payload)
		}
		if readSeconds != 3 {
			t.Errorf("readSeconds = %d, want 3", readSeconds)
		}
		if fake.count() != 1 {
			t.Errorf("kubernetes calls = %d, want 1", fake.count())
		}

		structured := result.(map[string]any)["structuredContent"].(map[string]any)
		if got := structured["tunnelClosed"]; got != true {
			t.Errorf("tunnelClosed = %v, want true — AC14's whole shape is that it does not stay open", got)
		}
		if got := gate.calls[0].Pair.String(); got != "create on pods/portforward" {
			t.Errorf("gated pair = %q, want %q", got, "create on pods/portforward")
		}
	})

	t.Run("an omitted window takes AC14's default rather than none", func(t *testing.T) {
		h, fake := approvingHandler(t)
		if _, rerr := callTool(t, h, "resource_port_forward", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","port":8080}`); rerr != nil {
			t.Fatalf("tools/call = %v, want the approved forward to run", rerr)
		}
		if _, _, _, readSeconds := fake.portForward(); readSeconds != k8s.PortForwardDefaultReadSeconds {
			t.Errorf("readSeconds = %d, want the default %d", readSeconds, k8s.PortForwardDefaultReadSeconds)
		}
	})

	t.Run("no payload is a read-only round trip, not a refusal", func(t *testing.T) {
		h, fake := approvingHandler(t)
		if _, rerr := callTool(t, h, "resource_port_forward", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","port":6379}`); rerr != nil {
			t.Fatalf("tools/call = %v, want a banner read to be allowed", rerr)
		}
		if _, _, payload, _ := fake.portForward(); len(payload) != 0 {
			t.Errorf("payload = %q, want nothing sent", payload)
		}
	})

	t.Run("payloadBase64 carries bytes that are not text", func(t *testing.T) {
		h, fake := approvingHandler(t)
		encoded := base64.StdEncoding.EncodeToString([]byte{0x00, 0xff, 0x10})
		if _, rerr := callTool(t, h, "resource_port_forward", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","port":5432,"payloadBase64":"`+encoded+`"}`); rerr != nil {
			t.Fatalf("tools/call = %v, want the approved forward to run", rerr)
		}
		_, _, payload, _ := fake.portForward()
		if len(payload) != 3 || payload[1] != 0xff {
			t.Errorf("payload = %v, want the decoded bytes", payload)
		}
	})

	t.Run("a truncated answer says so rather than shortening silently", func(t *testing.T) {
		h, fake := approvingHandler(t)
		fake.portForwardOutcome = &k8s.PortForwardOutcome{
			Pod:               "api",
			Port:              8080,
			Response:          strings.Repeat("y", 100),
			ResponseEncoding:  "utf-8",
			ResponseTruncated: true,
			TunnelClosed:      true,
		}
		result, rerr := callTool(t, h, "resource_port_forward", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","port":8080}`)
		if rerr != nil {
			t.Fatalf("tools/call = %v, want a capped forward to still answer", rerr)
		}
		structured := result.(map[string]any)["structuredContent"].(map[string]any)
		if structured["responseTruncated"] != true {
			t.Errorf("responseTruncated = %v, want true", structured["responseTruncated"])
		}
	})

	t.Run("the encoding of the answer is reported, not guessed at", func(t *testing.T) {
		h, fake := approvingHandler(t)
		fake.portForwardOutcome = &k8s.PortForwardOutcome{
			Pod:              "api",
			Port:             8080,
			Response:         base64.StdEncoding.EncodeToString([]byte{0xff, 0xfe}),
			ResponseEncoding: "base64",
			TunnelClosed:     true,
		}
		result, rerr := callTool(t, h, "resource_port_forward", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","port":8080}`)
		if rerr != nil {
			t.Fatalf("tools/call = %v, want a binary answer to still answer", rerr)
		}
		structured := result.(map[string]any)["structuredContent"].(map[string]any)
		if structured["responseEncoding"] != "base64" {
			t.Errorf("responseEncoding = %v, want base64", structured["responseEncoding"])
		}
	})
}

// TestPortForwardRefusalsCostNoApproval is scenario 14's "상한 초과가 거부된다"
// and its neighbours. Each has to be refused with the kubernetes call count
// still at zero and no approval requested: this tool reaches ports a network
// policy closed, so an argument error that costs an operator an approval prompt
// has already spent the thing the gate exists to ration.
func TestPortForwardRefusalsCostNoApproval(t *testing.T) {
	refusals := []struct {
		name string
		args string
		want string
	}{
		{"port above the range", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","port":70000}`, "between 1 and 65535"},
		{"port below the range", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","port":0}`, "between 1 and 65535"},
		{"port not an integer", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","port":"8080"}`, "port must be an integer"},
		{"missing port", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api"}`, "port is required"},
		{"readSeconds over the cap", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","port":8080,"readSeconds":31}`, "between 1 and 30"},
		{"readSeconds below one", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","port":8080,"readSeconds":0}`, "between 1 and 30"},
		{"both payload spellings", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","port":8080,"payload":"a","payloadBase64":"YQ=="}`, "not both"},
		{"payload not a string", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","port":8080,"payload":3}`, "payload must be a string"},
		{"payloadBase64 not base64", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","port":8080,"payloadBase64":"!!!"}`, "not valid base64"},
		{"missing name", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","port":8080}`, "name is required"},
		{"missing kind", `{"apiVersion":"v1","namespace":"ops","name":"api","port":8080}`, "kind is required"},
		{"explicit subresource", `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","port":8080,"subresource":"portforward"}`, "pass no subresource"},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
			h, fake := testHandler(t, gate, toolRegistry)
			_, rerr := callTool(t, h, "resource_port_forward", tc.args)
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

// TestPortForwardContextCarriesPortAndPayload is AC3's side of the same call,
// and the reason AC14 names AC3 at all: the pair says "create on
// pods/portforward" for every forward this server can make, so the port and the
// payload are the only things on the screen that distinguish reaching a metrics
// endpoint from reaching a database.
func TestPortForwardContextCarriesPortAndPayload(t *testing.T) {
	reader := newStateReader()
	gate := &scriptedGate{decision: &gatekeeper.Decision{RequestID: "req-1"}}
	h, _, _ := testHandlerReading(t, gate, toolRegistry, reader)

	args := `{"apiVersion":"v1","kind":"Pod","namespace":"ops","name":"api","port":6379,"payload":"FLUSHALL\r\n"}`
	if _, rerr := callTool(t, h, "resource_port_forward", args); rerr != nil {
		t.Fatalf("tools/call = %v, want the approved forward to run", rerr)
	}
	ctx := gate.context(0)
	for _, want := range []string{"6379", "FLUSHALL", "create on pods/portforward", "v1 Pod ops/api"} {
		if !strings.Contains(ctx, want) {
			t.Errorf("approval context is missing %q:\n%s", want, ctx)
		}
	}
}
