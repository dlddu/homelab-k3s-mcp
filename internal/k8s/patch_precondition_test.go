package k8s

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	jsonpatch "gopkg.in/evanphx/json-patch.v4"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

func patchRefForTest(kind string) PatchRef {
	return PatchRef{APIVersion: "v1", Kind: "Namespace", Name: "ops", PatchType: kind,
		Patch:                   []byte(`{"metadata":{"labels":{"team":"ops"}},"spec":{"large":9007199254740993}}`),
		ApprovedResourceVersion: "100", ApprovedUID: "uid-1"}
}

func TestApprovedPatchPreservesDataAndConditions(t *testing.T) {
	for _, kind := range []string{"merge", "strategic", "apply"} {
		t.Run(kind, func(t *testing.T) {
			ref := patchRefForTest(kind)
			original := string(ref.Patch)
			raw, err := approvedPatch(ref)
			if err != nil {
				t.Fatal(err)
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatal(err)
			}
			var metadata map[string]any
			if err := json.Unmarshal(body["metadata"], &metadata); err != nil {
				t.Fatal(err)
			}
			if metadata["resourceVersion"] != "100" || metadata["uid"] != "uid-1" {
				t.Fatalf("conditions: %s", raw)
			}
			if !strings.Contains(string(body["spec"]), "9007199254740993") {
				t.Fatalf("numeric data changed: %s", raw)
			}
			if string(ref.Patch) != original {
				t.Fatal("caller bytes mutated")
			}
			if metadata["labels"].(map[string]any)["team"] != "ops" {
				t.Fatal("caller labels lost")
			}
		})
	}
}

func TestJSONPatchGuardsCurrentAndResultIdentity(t *testing.T) {
	cases := []struct {
		name, body string
		wantOK     bool
	}{
		{"ordinary", `[{"op":"add","path":"/data/key","value":"new"}]`, true},
		{"empty", `[]`, true},
		{"replace version", `[{"op":"replace","path":"/metadata/resourceVersion","value":"101"}]`, false},
		{"remove version", `[{"op":"remove","path":"/metadata/resourceVersion"}]`, false},
		{"replace identity", `[{"op":"replace","path":"/metadata/uid","value":"other"}]`, false},
		{"remove metadata", `[{"op":"remove","path":"/metadata"}]`, false},
		{"replace root", `[{"op":"replace","path":"","value":{"metadata":{"resourceVersion":"101","uid":"uid-1"}}}]`, false},
		{"copy identity", `[{"op":"copy","from":"/metadata/resourceVersion","path":"/metadata/uid"}]`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref := patchRefForTest("json")
			ref.Patch = []byte(tc.body)
			raw, err := approvedPatch(ref)
			if err != nil {
				t.Fatal(err)
			}
			patch, err := jsonpatch.DecodePatch(raw)
			if err != nil {
				t.Fatal(err)
			}
			for _, rv := range []string{"100", "101"} {
				object := []byte(`{"metadata":{"resourceVersion":"` + rv + `","uid":"uid-1"},"data":{"key":"old"}}`)
				_, err = patch.Apply(object)
				wantOK := tc.wantOK && rv == "100"
				if (err == nil) != wantOK {
					t.Fatalf("rv=%s err=%v, want success=%v", rv, err, wantOK)
				}
			}
			_, err = patch.Apply([]byte(`{"metadata":{"resourceVersion":"100","uid":"new-uid"},"data":{"key":"old"}}`))
			if err == nil {
				t.Fatal("recreated target accepted")
			}
		})
	}
}

func TestApprovedPatchRefusesUnsafeInputsBeforeNetwork(t *testing.T) {
	cases := []struct{ kind, body string }{
		{"merge", "null"}, {"merge", "[]"}, {"merge", `{"metadata":null}`},
		{"merge", `{"metadata":{"resourceVersion":"101"},"spec":{"large":9007199254740993}}`},
		{"merge", `{"metadata":{"resourceVersion":100}}`},
		{"merge", `{"metadata":{"uid":null}}`},
		{"apply", `{"metadata":{"uid":"other"}}`},
		{"strategic", `{"$patch":"delete"}`},
		{"strategic", `{"$retainKeys":["spec"]}`},
		{"strategic", `{"metadata":{"$patch":"replace"}}`},
		{"json", "null"}, {"json", "{}"},
	}
	for _, tc := range cases {
		ref := patchRefForTest(tc.kind)
		ref.Patch = []byte(tc.body)
		if _, err := (&KubeService{}).PatchResource(context.Background(), ref); err == nil {
			t.Fatalf("accepted %s %s", tc.kind, tc.body)
		}
	}
	for _, field := range []string{"version", "uid"} {
		ref := patchRefForTest("merge")
		if field == "version" {
			ref.ApprovedResourceVersion = ""
		} else {
			ref.ApprovedUID = ""
		}
		if _, err := (&KubeService{}).PatchResource(context.Background(), ref); err == nil {
			t.Fatalf("missing %s accepted", field)
		}
	}
}

func TestConditionalPatchWirePathsAndNoRetries(t *testing.T) {
	for _, kind := range []string{"merge", "strategic", "json", "apply"} {
		for _, status := range []int{200, 409, 422, 429, 503, 403} {
			t.Run(kind+"/"+http.StatusText(status), func(t *testing.T) {
				calls := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					if r.Method != "PATCH" || r.URL.Path != "/api/v1/namespaces/ops" {
						t.Errorf("request %s %s", r.Method, r.URL.Path)
					}
					if r.Header.Get("Content-Type") != string(patchTypes[kind]) {
						t.Errorf("media type %s", r.Header.Get("Content-Type"))
					}
					manager := fieldManager
					if kind == "apply" {
						manager = "operator"
					}
					if r.URL.Query().Get("fieldManager") != manager || r.URL.Query().Has("force") {
						t.Errorf("options %s", r.URL.RawQuery)
					}
					raw, _ := io.ReadAll(r.Body)
					if !strings.Contains(string(raw), `"100"`) || !strings.Contains(string(raw), `"uid-1"`) {
						t.Errorf("conditions missing: %s", raw)
					}
					w.Header().Set("Content-Type", "application/json")
					if status == 429 || status == 503 {
						w.Header().Set("Retry-After", "1")
					}
					w.WriteHeader(status)
					if status == 200 {
						_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"ops","uid":"uid-1","resourceVersion":"101"}}`))
						return
					}
					reason := map[int]string{409: "Conflict", 422: "Invalid", 429: "TooManyRequests", 503: "ServiceUnavailable", 403: "Forbidden"}[status]
					_ = json.NewEncoder(w).Encode(map[string]any{"apiVersion": "v1", "kind": "Status", "status": "Failure", "reason": reason, "code": status, "message": "private-patch-value"})
				}))
				defer server.Close()
				ref := patchRefForTest(kind)
				if kind == "json" {
					ref.Patch = []byte(`[{"op":"add","path":"/metadata/labels","value":{"team":"ops"}}]`)
				}
				if kind == "apply" {
					ref.FieldManager = "operator"
				}
				result, err := serviceAgainst(server.URL).PatchResource(context.Background(), ref)
				if status == 200 && (err != nil || result == nil) {
					t.Fatalf("result=%v err=%v", result, err)
				}
				if status == 200 && result.Object["spec"].(map[string]any)["large"] != int64(9007199254740993) {
					t.Fatal("response integer was rounded")
				}
				if status != 200 && err == nil {
					t.Fatal("failure returned success")
				}
				if err != nil && strings.Contains(err.Error(), "private-patch-value") {
					t.Fatal("API body leaked into error")
				}
				if calls != 1 {
					t.Fatalf("requests=%d, want one", calls)
				}
			})
		}
	}
}

func TestConditionalPatchGroupedNamespacedPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" || r.URL.Path != "/apis/apps/v1/namespaces/ops/deployments/api" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"api","namespace":"ops","resourceVersion":"101"}}`))
	}))
	defer server.Close()
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Group: "apps", Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, meta.RESTScopeNamespace)
	svc := &KubeService{config: &rest.Config{Host: server.URL}}
	svc.mappers.mapper = mapper
	namespace := "ops"
	ref := patchRefForTest("merge")
	ref.APIVersion = "apps/v1"
	ref.Kind = "Deployment"
	ref.Namespace = &namespace
	ref.Name = "api"
	result, err := svc.PatchResource(context.Background(), ref)
	if err != nil || result.Namespace != "ops" {
		t.Fatalf("result=%v err=%v", result, err)
	}
}
