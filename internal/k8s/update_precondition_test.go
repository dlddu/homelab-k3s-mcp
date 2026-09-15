package k8s

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestUpdateUsesApprovedVersionOnTheWire(t *testing.T) {
	for _, shape := range []string{"object", "object-with-current-version", "scale"} {
		scale := shape == "scale"
		for _, conflict := range []bool{false, true} {
			name := shape
			if conflict {
				name += "/changed-after-gate-recheck"
			} else {
				name += "/unchanged"
			}
			t.Run(name, func(t *testing.T) {
				var writes atomic.Int32
				bodies := make(chan map[string]any, 1)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					if r.Method == http.MethodGet && r.URL.Path == "/apis/apps/v1" {
						_, _ = io.WriteString(w, `{"kind":"APIResourceList","apiVersion":"v1","groupVersion":"apps/v1","resources":[{"name":"deployments/scale","kind":"Scale","namespaced":true}]}`)
						return
					}
					wantPath := "/apis/apps/v1/namespaces/ops/deployments/api"
					if scale {
						wantPath += "/scale"
					}
					if r.Method != http.MethodPut || r.URL.Path != wantPath {
						t.Errorf("unexpected API request: %s %s", r.Method, r.URL.Path)
						http.Error(w, "unexpected request", http.StatusBadRequest)
						return
					}
					writes.Add(1)
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Errorf("decode PUT: %v", err)
						http.Error(w, "bad body", http.StatusBadRequest)
						return
					}
					select {
					case bodies <- body:
					default:
						t.Error("update retried a consumed approval")
					}
					if conflict {
						w.WriteHeader(http.StatusConflict)
						_, _ = io.WriteString(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Conflict","code":409,"message":"private-value-from-apiserver"}`)
						return
					}
					if body["metadata"].(map[string]any)["resourceVersion"] != "100" {
						w.WriteHeader(http.StatusConflict)
						_, _ = io.WriteString(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Conflict","code":409}`)
						return
					}
					_ = json.NewEncoder(w).Encode(body)
				}))
				defer server.Close()

				manifest := map[string]any{
					"apiVersion": "apps/v1", "kind": "Deployment",
					"metadata": map[string]any{"name": "api", "namespace": "ops"},
					"spec":     map[string]any{"replicas": int64(3)},
				}
				if shape == "object-with-current-version" {
					manifest["metadata"].(map[string]any)["resourceVersion"] = "100"
				}
				before, _ := json.Marshal(manifest)
				ref := UpdateRef{
					APIVersion: "apps/v1", Kind: "Deployment",
					Namespace: namespaceOf("ops"), Name: "api",
					ApprovedResourceVersion: "100", Manifest: manifest,
				}
				if scale {
					replicas := int64(3)
					ref.Subresource, ref.Replicas, ref.Manifest = ScaleSubresource, &replicas, nil
				}
				result, err := targetServiceAgainst(server.URL).UpdateResource(context.Background(), ref)
				if conflict {
					if err == nil || result != nil || !strings.Contains(err.Error(), "new approval") {
						t.Fatalf("UpdateResource = (%v, %v), want refusal requiring a new approval", result, err)
					}
					if strings.Contains(err.Error(), "private-value-from-apiserver") {
						t.Error("conflict leaked the upstream error body")
					}
				} else if err != nil || result == nil {
					t.Fatalf("UpdateResource = (%v, %v), want success", result, err)
				}
				if writes.Load() != 1 {
					t.Fatalf("PUT count = %d, want exactly one", writes.Load())
				}
				body := <-bodies
				if got := body["metadata"].(map[string]any)["resourceVersion"]; got != "100" {
					t.Errorf("wire resourceVersion = %v, want gate-approved 100", got)
				}
				if got := body["spec"].(map[string]any)["replicas"]; got != float64(3) {
					t.Errorf("replicas = %v, want 3", got)
				}
				if scale && (body["kind"] != "Scale" || body["apiVersion"] != "autoscaling/v1") {
					t.Errorf("scale identity = %v/%v", body["apiVersion"], body["kind"])
				}
				after, _ := json.Marshal(manifest)
				if string(before) != string(after) {
					t.Error("caller manifest was mutated while adding the precondition")
				}
			})
		}
	}
}

func TestUpdateRejectsMissingApprovalBeforeAnyAPIRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "must not reach the API", http.StatusInternalServerError)
	}))
	defer server.Close()
	for _, subresource := range []string{"", ScaleSubresource} {
		replicas := int64(3)
		_, err := targetServiceAgainst(server.URL).UpdateResource(context.Background(), UpdateRef{
			APIVersion: "apps/v1", Kind: "Deployment", Name: "api",
			Namespace: namespaceOf("ops"), Subresource: subresource, Replicas: &replicas,
		})
		if err == nil || !strings.Contains(err.Error(), "new approval") {
			t.Errorf("subresource %q: error = %v, want missing approval refusal", subresource, err)
		}
	}
	if calls.Load() != 0 {
		t.Errorf("API requests = %d, want zero", calls.Load())
	}
}

func TestUpdateDoesNotReplaceACallersConflictingVersion(t *testing.T) {
	for _, supplied := range []any{"101", "", nil, float64(100), map[string]any{"invalid": true}} {
		t.Run(strings.ReplaceAll(string(mustJSON(t, supplied)), "/", "_"), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				http.Error(w, "must not reach the API", http.StatusInternalServerError)
			}))
			defer server.Close()
			metadata := map[string]any{"name": "api", "namespace": "ops", "resourceVersion": supplied}
			ref := UpdateRef{
				APIVersion: "apps/v1", Kind: "Deployment", Namespace: namespaceOf("ops"), Name: "api",
				ApprovedResourceVersion: "100",
				Manifest:                map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": metadata},
			}
			_, err := targetServiceAgainst(server.URL).UpdateResource(context.Background(), ref)
			if err == nil || !strings.Contains(err.Error(), "new approval") {
				t.Fatalf("error = %v, want conflicting manifest refusal", err)
			}
			if calls.Load() != 0 {
				t.Errorf("API requests = %d, want zero", calls.Load())
			}
			if !reflect.DeepEqual(metadata["resourceVersion"], supplied) {
				t.Error("caller resourceVersion was overwritten")
			}
		})
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
