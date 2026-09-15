package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

func createServiceAgainst(host string) *KubeService {
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}, {Group: "apps", Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}, meta.RESTScopeRoot)
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, meta.RESTScopeNamespace)
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "Secret"}, meta.RESTScopeNamespace)
	mapper.Add(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, meta.RESTScopeNamespace)
	service := &KubeService{config: &rest.Config{Host: host}}
	service.mappers.mapper = mapper
	return service
}

func namedCreate(apiVersion, kind, namespace string) CreateRef {
	metadata := map[string]any{"name": "new-object"}
	ref := CreateRef{APIVersion: apiVersion, Kind: kind, Name: "new-object"}
	if namespace != "" {
		ref.Namespace = &namespace
		metadata["namespace"] = namespace
	}
	ref.Manifest = map[string]any{"apiVersion": apiVersion, "kind": kind, "metadata": metadata}
	return ref
}

func TestCreateDoesNotOverwrite(t *testing.T) {
	for _, tc := range []struct{ apiVersion, kind, namespace, path string }{
		{"v1", "ConfigMap", "ops", "/api/v1/namespaces/ops/configmaps"},
		{"v1", "Namespace", "", "/api/v1/namespaces"},
		{"apps/v1", "Deployment", "ops", "/apis/apps/v1/namespaces/ops/deployments"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			calls := 0
			var original []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				body, _ := io.ReadAll(r.Body)
				if r.Method != http.MethodPost || r.URL.Path != tc.path || r.URL.Query().Get("fieldManager") != fieldManager {
					t.Errorf("request = %s %s", r.Method, r.URL)
				}
				w.Header().Set("Content-Type", "application/json")
				if calls == 1 {
					original = append([]byte(nil), body...)
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write(body)
				} else {
					w.WriteHeader(http.StatusConflict)
					fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","code":409,"reason":"AlreadyExists","message":"sensitive-body-from-api"}`)
				}
			}))
			defer server.Close()
			service := createServiceAgainst(server.URL)
			ref := namedCreate(tc.apiVersion, tc.kind, tc.namespace)
			if _, err := service.CreateResource(context.Background(), ref); err != nil {
				t.Fatal(err)
			}
			_, err := service.CreateResource(context.Background(), ref)
			if err == nil || !strings.Contains(err.Error(), "409") || strings.Contains(err.Error(), "sensitive-body") || calls != 2 {
				t.Fatalf("conflict=%v calls=%d", err, calls)
			}
			want, _ := json.Marshal(ref.Manifest)
			if string(original) != string(want) {
				t.Fatal("create changed the manifest")
			}
		})
	}
}

func TestCreateNeverRetriesOrEchoesAPIValues(t *testing.T) {
	for _, code := range []int{403, 422, 429, 500, 503} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(code)
				fmt.Fprintf(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","code":%d,"reason":"Invalid","message":"secret-from-api"}`, code)
			}))
			defer server.Close()
			_, err := createServiceAgainst(server.URL).CreateResource(context.Background(), namedCreate("v1", "Secret", "ops"))
			if err == nil || strings.Contains(err.Error(), "secret-from-api") || calls != 1 {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
			if code == 403 && !strings.Contains(err.Error(), "(create, secrets)") {
				t.Fatalf("403 lost the denied pair: %v", err)
			}
		})
	}
}

func TestCreateRefusesNamespaceAndCoordinateMismatch(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	refs := []CreateRef{namedCreate("v1", "ConfigMap", ""), namedCreate("v1", "Namespace", "ops")}
	bad := namedCreate("v1", "ConfigMap", "ops")
	bad.Manifest["metadata"].(map[string]any)["namespace"] = "elsewhere"
	refs = append(refs, bad)
	bad = namedCreate("v1", "ConfigMap", "ops")
	bad.Manifest["metadata"].(map[string]any)["name"] = "different"
	refs = append(refs, bad)
	for _, ref := range refs {
		if _, err := createServiceAgainst(server.URL).CreateResource(context.Background(), ref); err == nil {
			t.Fatal("mismatched coordinate was accepted")
		}
	}
	if calls != 0 {
		t.Fatalf("invalid coordinates sent %d requests", calls)
	}
}
