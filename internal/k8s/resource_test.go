package k8s

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

// AC4: the two fields that dominate an object's size go, and nothing else does.
// The negative half is the point — a stripper that also took spec or status
// would be indistinguishable from this one on a smoke test.
func TestStripNoiseRemovesOnlyTheNoise(t *testing.T) {
	object := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name":          "api",
			"managedFields": []any{map[string]any{"manager": "kubectl"}},
			"annotations": map[string]any{
				"kubectl.kubernetes.io/last-applied-configuration": "{\"spec\":{}}",
				"deployment.kubernetes.io/revision":                "7",
			},
		},
		"spec":   map[string]any{"replicas": 3},
		"status": map[string]any{"readyReplicas": 3},
	}

	stripNoise(object)

	metadata := object["metadata"].(map[string]any)
	if _, ok := metadata["managedFields"]; ok {
		t.Error("managedFields survived")
	}
	annotations := metadata["annotations"].(map[string]any)
	if _, ok := annotations["kubectl.kubernetes.io/last-applied-configuration"]; ok {
		t.Error("last-applied-configuration survived")
	}
	if annotations["deployment.kubernetes.io/revision"] != "7" {
		t.Errorf("annotations = %v, want the unrelated one kept", annotations)
	}
	if object["spec"].(map[string]any)["replicas"] != 3 {
		t.Error("spec was altered")
	}
	if object["status"].(map[string]any)["readyReplicas"] != 3 {
		t.Error("status was altered")
	}
	if metadata["name"] != "api" {
		t.Error("metadata.name was altered")
	}
}

// An object carrying nothing but the noise annotation loses the empty map too,
// so the caller does not read "annotations: {}" as "the object has none set".
func TestStripNoiseDropsAnEmptiedAnnotationMap(t *testing.T) {
	object := map[string]any{"metadata": map[string]any{
		"annotations": map[string]any{"kubectl.kubernetes.io/last-applied-configuration": "{}"},
	}}
	stripNoise(object)
	if _, ok := object["metadata"].(map[string]any)["annotations"]; ok {
		t.Error("an annotation map emptied by stripping should go with it")
	}
}

func TestStripNoiseToleratesObjectsWithoutMetadata(t *testing.T) {
	object := map[string]any{"kind": "Status"}
	stripNoise(object)
	if object["kind"] != "Status" {
		t.Error("stripNoise altered an object it had nothing to strip from")
	}
}

// AC18: a 403 is reported as this server's missing grant, naming the pair and
// the file that decides it, rather than passed through as the apiserver's own
// wording — which reads like a transient failure worth retrying.
func TestForbiddenBecomesAGrantStatement(t *testing.T) {
	forbidden := apierrors.NewForbidden(
		schema.GroupResource{Group: "", Resource: "secrets"},
		"db",
		nil,
	)
	s := &KubeService{}

	err := s.apiCallError(forbidden, "get", "secrets")
	msg := err.Error()
	for _, want := range []string{"get", "secrets", "k8s/rbac.yaml"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not mention %q", msg, want)
		}
	}
	if !strings.Contains(msg, "retrying will not change the answer") {
		t.Errorf("message %q does not say the call is not worth retrying", msg)
	}
}

func TestNonForbiddenErrorsArePassedThrough(t *testing.T) {
	notFound := apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "api-0")
	s := &KubeService{}

	msg := s.apiCallError(notFound, "get", "pods").Error()
	if strings.Contains(msg, "k8s/rbac.yaml") {
		t.Errorf("a 404 was reported as a permission problem: %q", msg)
	}
	if !strings.Contains(msg, "api-0") {
		t.Errorf("message %q lost the apiserver's own detail", msg)
	}
}

func TestSubresourcePathJoinsOnlyWhenThereIsOne(t *testing.T) {
	if got := subresourcePath("pods", ""); got != "pods" {
		t.Errorf("subresourcePath(pods, \"\") = %q", got)
	}
	if got := subresourcePath("pods", "log"); got != "pods/log" {
		t.Errorf("subresourcePath(pods, log) = %q", got)
	}
}

// tableNegotiatingServer stands in for an apiserver at the one point that
// matters here: it reads the Accept parameters the way the real one does and
// answers a Table only when they name a representation it serves. `honor` false
// is the older apiserver that has no Table for this resource at all. Neither
// branch ever errors — the fallback to the ordinary list is the documented
// behaviour of offering plain application/json alongside, and it is what makes
// a wrong parameter look like an empty result rather than a failed call.
func tableNegotiatingServer(t *testing.T, honor bool) *httptest.Server {
	t.Helper()
	const table = `{"kind":"Table","apiVersion":"meta.k8s.io/v1",` +
		`"metadata":{"continue":"next-page"},` +
		`"columnDefinitions":[{"name":"Name","type":"string"},{"name":"Status","type":"string"}],` +
		`"rows":[{"cells":["kube-system","Active"]},{"cells":["homelab-k3s-mcp","Active"]}]}`
	const list = `{"kind":"NamespaceList","apiVersion":"v1","metadata":{},` +
		`"items":[{"metadata":{"name":"kube-system"}},{"metadata":{"name":"homelab-k3s-mcp"}}]}`

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if honor && acceptsTable(r.Header.Get("Accept")) {
			_, _ = io.WriteString(w, table)
			return
		}
		_, _ = io.WriteString(w, list)
	}))
}

func acceptsTable(accept string) bool {
	for _, media := range strings.Split(accept, ",") {
		params := strings.Split(strings.TrimSpace(media), ";")
		got := map[string]string{}
		for _, param := range params[1:] {
			key, value, _ := strings.Cut(param, "=")
			got[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
		if got["as"] == "Table" &&
			got["g"] == metav1.SchemeGroupVersion.Group &&
			got["v"] == metav1.SchemeGroupVersion.Version {
			return true
		}
	}
	return false
}

// serviceAgainst points a KubeService at a test server with the mapper already
// resolved, so the case exercises the list round trip and not discovery.
func serviceAgainst(host string) *KubeService {
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "Namespace"}, meta.RESTScopeRoot)
	service := &KubeService{config: &rest.Config{Host: host}}
	service.mappers.mapper = mapper
	return service
}

// The header this package sends has to be one the apiserver actually matches.
// Asserting its shape ("starts with as=Table", "mentions meta.k8s.io") is what
// the first version of this test did, and it held for a header carrying v=1 —
// a version no apiserver serves — for as long as nothing put the string in
// front of a server that negotiates.
func TestListResourcesGetsTheTableFromAServerThatNegotiates(t *testing.T) {
	server := tableNegotiatingServer(t, true)
	defer server.Close()

	got, err := serviceAgainst(server.URL).ListResources(
		context.Background(), ListQuery{APIVersion: "v1", Kind: "Namespace"},
	)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(got.Columns) != 2 || got.Columns[0].Name != "Name" {
		t.Errorf("columns = %+v, want the apiserver's own column definitions", got.Columns)
	}
	if len(got.Rows) != 2 {
		t.Errorf("rows = %+v, want one per object", got.Rows)
	}
	if !got.Truncated || got.Continue != "next-page" {
		t.Errorf("truncated = %v, continue = %q, want the page marker carried through",
			got.Truncated, got.Continue)
	}
}

// The negative half is the point. A decoder that shrugs at a non-Table body
// reports an empty table, which reads as "there is nothing there" — the one
// answer a list must never invent.
func TestListResourcesRefusesABodyThatIsNotATable(t *testing.T) {
	server := tableNegotiatingServer(t, false)
	defer server.Close()

	got, err := serviceAgainst(server.URL).ListResources(
		context.Background(), ListQuery{APIVersion: "v1", Kind: "Namespace"},
	)
	if err == nil {
		t.Fatalf("ListResources returned %+v, want a refusal", got)
	}
	if !strings.Contains(err.Error(), "NamespaceList") {
		t.Errorf("error %q does not name what came back instead", err)
	}
}
