package k8s

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
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

// AC18: a 403 is reported as this server's missing grant, naming the pair —
// not passed through as the apiserver's own wording, which reads like a
// transient failure worth retrying. AC18 names no file now; neither may this.
func TestForbiddenBecomesAGrantStatement(t *testing.T) {
	forbidden := apierrors.NewForbidden(
		schema.GroupResource{Group: "", Resource: "secrets"},
		"db",
		nil,
	)
	s := &KubeService{}

	err := s.apiCallError(forbidden, "get", "secrets")
	msg := err.Error()
	for _, want := range []string{"get", "secrets"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not mention %q", msg, want)
		}
	}
	if !strings.Contains(msg, "retrying will not change the answer") {
		t.Errorf("message %q does not say the call is not worth retrying", msg)
	}
	if strings.Contains(msg, "rbac.yaml") {
		t.Errorf("message %q sends the operator to a file that no longer decides the grant", msg)
	}
}

func TestNonForbiddenErrorsArePassedThrough(t *testing.T) {
	notFound := apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "api-0")
	s := &KubeService{}

	msg := s.apiCallError(notFound, "get", "pods").Error()
	if strings.Contains(msg, "refused:") {
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

func servedKinds() []APIResource {
	return []APIResource{
		{Group: "apps", Version: "v1", Kind: "Deployment"},
		{Group: "apps", Version: "v1", Kind: "DaemonSet"},
		{Group: "apps", Version: "v1", Kind: "StatefulSet"},
		{Group: "", Version: "v1", Kind: "Pod"},
		{Group: "", Version: "v1", Kind: "ConfigMap"},
		{Group: "policy", Version: "v1", Kind: "PodDisruptionBudget"},
	}
}

// AC20: the refusal carries candidates. The middle-of-the-word typo is the case
// the scenario names and the one containment alone never answered, so each row
// here is a shape of wrongness rather than a repetition of the same one.
func TestUnknownKindSuggestsCandidates(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind string
	}{
		{"letter dropped from the middle", "Deploymnt"},
		{"cut short", "Deploymen"},
		{"letter doubled", "Deploymentt"},
		{"typo in the wrong case", "deploymnt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := rankSimilarKinds(servedKinds(), tc.kind)
			if len(got) == 0 || got[0] != "apps/v1/Deployment" {
				t.Errorf("rankSimilarKinds(%q) = %v, want apps/v1/Deployment first", tc.kind, got)
			}
		})
	}
}

// The negative half: a budget wide enough to reach every kind would make the
// message noise, and the empty answer is what routes the caller to the
// api_resources listing instead.
func TestUnrelatedKindGetsNoCandidates(t *testing.T) {
	for _, kind := range []string{"Fluxcapacitor", "Xyzzy", "Quux"} {
		if got := rankSimilarKinds(servedKinds(), kind); len(got) != 0 {
			t.Errorf("rankSimilarKinds(%q) = %v, want no candidates", kind, got)
		}
	}
}

// Ordering is what makes the cut safe: sorted by label alone, the longest
// containment match would take a seat from a kind one letter away.
func TestCandidatesAreNearestFirstAndBounded(t *testing.T) {
	served := []APIResource{
		{Group: "", Version: "v1", Kind: "Pod"},
		{Group: "", Version: "v1", Kind: "PodTemplate"},
		{Group: "policy", Version: "v1", Kind: "PodDisruptionBudget"},
		{Group: "policy", Version: "v1beta1", Kind: "PodSecurityPolicy"},
		{Group: "metrics.k8s.io", Version: "v1beta1", Kind: "PodMetrics"},
		{Group: "scheduling.volcano.sh", Version: "v1beta1", Kind: "PodGroup"},
		{Group: "chaos-mesh.org", Version: "v1alpha1", Kind: "PodChaos"},
	}

	got := rankSimilarKinds(served, "Pod")
	if len(got) != 5 {
		t.Fatalf("rankSimilarKinds(Pod) = %v, want the list cut at five", got)
	}
	for _, want := range []string{
		"chaos-mesh.org/v1alpha1/PodChaos",
		"scheduling.volcano.sh/v1beta1/PodGroup",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("rankSimilarKinds(Pod) = %v, dropped the near candidate %q", got, want)
		}
	}
	if slices.Contains(got, "policy/v1/PodDisruptionBudget") {
		t.Errorf("rankSimilarKinds(Pod) = %v, kept the farthest match over a nearer one", got)
	}
}

func TestSimilarKindsDoNotRepeatALabel(t *testing.T) {
	served := []APIResource{
		{Group: "apps", Version: "v1", Kind: "Deployment", Name: "deployments"},
		{Group: "apps", Version: "v1", Kind: "Deployment", Name: "deployments/scale"},
	}
	if got := rankSimilarKinds(served, "Deploymnt"); len(got) != 1 {
		t.Errorf("rankSimilarKinds(Deploymnt) = %v, want one label per kind", got)
	}
}

// AC9: each name the tool accepts has to reach the apiserver as the content
// type that means what the name says. The media types are asserted by value
// rather than by round-tripping the constants — a mapping that sent "merge" as
// a strategic patch would be self-consistent and still apply the wrong
// semantics to a list field, which is the same class of error the Accept
// header's v=1 was.
func TestPatchTypesMapToApiserverMediaTypes(t *testing.T) {
	want := map[string]string{
		"merge":     "application/merge-patch+json",
		"strategic": "application/strategic-merge-patch+json",
		"json":      "application/json-patch+json",
		"apply":     "application/apply-patch+yaml",
	}
	for name, mediaType := range want {
		if !IsPatchType(name) {
			t.Errorf("IsPatchType(%q) = false, want the tool to accept it", name)
			continue
		}
		if got := string(patchTypes[name]); got != mediaType {
			t.Errorf("patchTypes[%q] = %q, want %q", name, got, mediaType)
		}
	}
	if len(patchTypes) != len(want) {
		t.Errorf("patchTypes has %d entries, want exactly the %d AC9 names", len(patchTypes), len(want))
	}
	if IsPatchType("yaml") {
		t.Error(`IsPatchType("yaml") = true, want a name outside the four refused`)
	}
	if got := strings.Join(PatchTypeNames(), ","); got != "apply,json,merge,strategic" {
		t.Errorf("PatchTypeNames() = %q, want a stable sorted list so the refusal message does not shuffle", got)
	}
}

// discoveryServer answers the one group-version endpoint requireSubresource
// reads. apps/v1 is the honest case: the apiserver serves deployments/scale and
// does not serve daemonsets/scale, which is the whole of AC8's DaemonSet rule.
func discoveryServer(t *testing.T) *httptest.Server {
	t.Helper()
	const resources = `{"kind":"APIResourceList","apiVersion":"v1","groupVersion":"apps/v1","resources":[` +
		`{"name":"deployments","kind":"Deployment","namespaced":true},` +
		`{"name":"deployments/scale","kind":"Scale","namespaced":true},` +
		`{"name":"daemonsets","kind":"DaemonSet","namespaced":true}]}`

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apis/apps/v1" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, resources)
	}))
}

// AC8: a kind with no replicas is refused for having no replicas. The negative
// half carries the criterion — the refusal an operator acts on is the one that
// says which of "no permission", "no object" and "no such thing" it was, and a
// bare PUT would have produced the same 404 for all three.
func TestUpdateScaleRejectsReplicalessKind(t *testing.T) {
	server := discoveryServer(t)
	defer server.Close()
	service := serviceAgainst(server.URL)
	gvr := func(resource string) schema.GroupVersionResource {
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: resource}
	}

	if err := service.requireSubresource(context.Background(), gvr("deployments"), ScaleSubresource, "Deployment"); err != nil {
		t.Fatalf("requireSubresource(deployments/scale) = %v, want it served", err)
	}

	err := service.requireSubresource(context.Background(), gvr("daemonsets"), ScaleSubresource, "DaemonSet")
	if err == nil {
		t.Fatal("requireSubresource(daemonsets/scale) = nil, want a refusal")
	}
	for _, want := range []string{"DaemonSet", "no replicas", "daemonsets/scale", "not about permission"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not mention %q", err, want)
		}
	}
}

// The scale body carries no resourceVersion: this tool holds the update verb
// alone, so there is no get to read one from. An assertion on its absence is
// the only thing standing between that design and a get-then-put creeping back
// in as "just one read".
func TestScaleObjectIsAnUnconditionalWrite(t *testing.T) {
	body := scaleObject("ops", "api", 3)

	metadata := body["metadata"].(map[string]any)
	if _, ok := metadata["resourceVersion"]; ok {
		t.Error("the scale body carries a resourceVersion, which this tool has no verb to read")
	}
	if metadata["name"] != "api" || metadata["namespace"] != "ops" {
		t.Errorf("metadata = %v, want the coordinate", metadata)
	}
	if body["apiVersion"] != "autoscaling/v1" || body["kind"] != "Scale" {
		t.Errorf("body = %v, want an autoscaling/v1 Scale", body)
	}
	if body["spec"].(map[string]any)["replicas"] != int64(3) {
		t.Errorf("spec = %v, want replicas 3", body["spec"])
	}
}

// A cluster-scoped kind gets no namespace in the body, rather than an empty
// string the apiserver would read as a namespace named "".
func TestScaleObjectOmitsAnEmptyNamespace(t *testing.T) {
	metadata := scaleObject("", "node-pool", 1)["metadata"].(map[string]any)
	if _, ok := metadata["namespace"]; ok {
		t.Errorf("metadata = %v, want no namespace key at all", metadata)
	}
}

// podLogServer answers pods/log the way the apiserver does. The JSON content
// type is load-bearing rather than incidental — it is what sends client-go down
// the branch podLogsError exists for — so a tidying pass must not drop it.
func podLogServer(t *testing.T, code int, status string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/log") {
			t.Errorf("request path = %q, want the log subresource", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = io.WriteString(w, status)
	}))
}

// logServiceAgainst is serviceAgainst for the one verb that goes through the
// typed clientset rather than the dynamic client.
func logServiceAgainst(t *testing.T, host string) *KubeService {
	t.Helper()
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "Pod"}, meta.RESTScopeNamespace)
	clientset, err := kubernetes.NewForConfig(&rest.Config{Host: host})
	if err != nil {
		t.Fatalf("NewForConfig: %v", err)
	}
	service := &KubeService{clientset: clientset, config: &rest.Config{Host: host}}
	service.mappers.mapper = mapper
	return service
}

func podLogRef(namespace string) ResourceRef {
	return ResourceRef{APIVersion: "v1", Kind: "Pod", Namespace: &namespace, Name: "api-0", Subresource: "log"}
}

// AC5: the refusal has to name the containers to choose between. Asserting only
// that the call failed would hold for the message this replaces, which failed
// without telling the caller anything they could act on.
func TestPodLogsKeepsTheApiserversContainerCandidates(t *testing.T) {
	const status = `{"kind":"Status","apiVersion":"v1","metadata":{},"status":"Failure",` +
		`"message":"a container name must be specified for pod api-0, choose one of: [app sidecar]",` +
		`"reason":"BadRequest","code":400}`
	server := podLogServer(t, http.StatusBadRequest, status)
	defer server.Close()

	_, err := logServiceAgainst(t, server.URL).GetResource(context.Background(), podLogRef("ops"))
	if err == nil {
		t.Fatal("GetResource = nil error, want the ambiguous container refused")
	}
	msg := err.Error()
	for _, want := range []string{"container name must be specified", "app", "sidecar"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not carry %q", msg, want)
		}
	}
	if strings.Contains(msg, "unknown reason") {
		t.Errorf("message %q is still client-go's placeholder", msg)
	}
}

// The control half. A version that answered every failure out of the body would
// pass the case above and quietly replace this server's own grant statement
// with the apiserver's wording, which does not say which grant is missing.
func TestPodLogsForbiddenStillReportsTheMissingGrant(t *testing.T) {
	const status = `{"kind":"Status","apiVersion":"v1","metadata":{},"status":"Failure",` +
		`"message":"pods \"api-0\" is forbidden: User \"x\" cannot get resource \"pods/log\"",` +
		`"reason":"Forbidden","code":403}`
	server := podLogServer(t, http.StatusForbidden, status)
	defer server.Close()

	_, err := logServiceAgainst(t, server.URL).GetResource(context.Background(), podLogRef("ops"))
	if err == nil {
		t.Fatal("GetResource = nil error, want the forbidden read refused")
	}
	msg := err.Error()
	if !strings.Contains(msg, "no (get, pods/log) grant") {
		t.Errorf("message %q lost this server's own grant statement", msg)
	}
	if !strings.Contains(msg, "retrying will not change the answer") {
		t.Errorf("message %q does not say the call is not worth retrying", msg)
	}
}

// A body that is not a Status — a proxy's HTML error page, a truncated
// response — must not become the message. Falling through to apiCallError is
// what keeps a garbled body from being reported as the apiserver's reasoning.
func TestPodLogsFallsBackWhenTheBodyIsNotAStatus(t *testing.T) {
	server := podLogServer(t, http.StatusBadGateway, "<html>502 Bad Gateway</html>")
	defer server.Close()

	_, err := logServiceAgainst(t, server.URL).GetResource(context.Background(), podLogRef("ops"))
	if err == nil {
		t.Fatal("GetResource = nil error, want the gateway failure refused")
	}
	if strings.Contains(err.Error(), "<html>") {
		t.Errorf("message %q reported a non-Status body as the apiserver's reasoning", err.Error())
	}
}
