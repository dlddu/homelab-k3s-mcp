package sessionplatform

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func configStub(t *testing.T, configStatus int, config string) (*Client, *[]lifecycleRequest) {
	t.Helper()
	var seen []lifecycleRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, lifecycleRequest{method: r.Method, path: r.URL.Path})
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == configPath:
			w.WriteHeader(configStatus)
			_, _ = w.Write([]byte(config))
		case r.Method == http.MethodPost && r.URL.Path == sessionsPath:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(createdShell))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &Client{endpoint: srv.URL, userAgent: "homelab-k3s-mcp/test", http: srv.Client(), create: srv.Client()}, &seen
}

const offeredConfig = `{"claudeCode":{},"workloadTypes":[
  {"type":"shell","takesModel":false},{"type":"claude-code","takesModel":true},
  {"type":"minimax-code","takesModel":true},{"type":"approval-gated","takesModel":true}]}`

func TestCreateSessionAcceptsAnOfferedType(t *testing.T) {
	c, seen := configStub(t, http.StatusOK, offeredConfig)
	if _, err := c.CreateSession(context.Background(), CreateRequest{Name: "mm", WorkloadType: "minimax-code"}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if len(*seen) != 2 || (*seen)[0].path != configPath || (*seen)[1].method != http.MethodPost {
		t.Fatalf("requests = %+v, want GET config then POST sessions", *seen)
	}
}

func TestCreateSessionRefusesATypeTheControlPlaneDoesNotOffer(t *testing.T) {
	c, seen := configStub(t, http.StatusOK, `{"workloadTypes":[{"type":"shell"},{"type":"claude-code"}]}`)
	_, err := c.CreateSession(context.Background(), CreateRequest{Name: "x", WorkloadType: "minimax-code"})
	var e *Error
	if !errors.As(err, &e) || e.kind != kindInvalidArgument {
		t.Fatalf("error = %v, want a kindInvalidArgument *Error", err)
	}
	if !strings.Contains(err.Error(), "workloadType must be one of shell, claude-code") {
		t.Fatalf("error = %q, want the offered list", err.Error())
	}
	for _, r := range *seen {
		if r.method == http.MethodPost {
			t.Fatalf("requests = %+v, want no create", *seen)
		}
	}
}

func TestCreateSessionLooksNothingUpForShell(t *testing.T) {
	for _, workload := range []string{"", "shell"} {
		c, seen := configStub(t, http.StatusInternalServerError, `{}`)
		if _, err := c.CreateSession(context.Background(), CreateRequest{Name: "x", WorkloadType: workload}); err != nil {
			t.Fatalf("CreateSession(%q): %v", workload, err)
		}
		if len(*seen) != 1 || (*seen)[0].method != http.MethodPost {
			t.Fatalf("requests for %q = %+v, want only the create", workload, *seen)
		}
	}
}

func TestCreateSessionDefersToAControlPlaneWithoutAList(t *testing.T) {
	c, seen := configStub(t, http.StatusOK, `{"claudeCode":{}}`)
	if _, err := c.CreateSession(context.Background(), CreateRequest{Name: "x", WorkloadType: "claude-code"}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if len(*seen) != 2 {
		t.Fatalf("requests = %+v, want the lookup and the create", *seen)
	}
}

func TestCreateSessionReportsAnUnreadableConfigAsAnAPIError(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"server error", `{"error":"boom"}`, http.StatusInternalServerError},
		{"not json", `<html>`, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, seen := configStub(t, tc.status, tc.body)
			_, err := c.CreateSession(context.Background(), CreateRequest{Name: "x", WorkloadType: "claude-code"})
			var e *Error
			if !errors.As(err, &e) || e.kind != kindAPI {
				t.Fatalf("error = %v, want a kindAPI *Error", err)
			}
			if len(*seen) != 1 {
				t.Fatalf("requests = %+v, want only the lookup", *seen)
			}
		})
	}
}
