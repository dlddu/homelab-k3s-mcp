package k8s

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

// secretTokenInFixture is the value a Secret's body would carry. It exists so
// the assertions can say "this never entered the process" rather than "the
// object looked small".
const secretTokenInFixture = "unique-token-the-gate-must-not-read"

// metadataNegotiatingServer stands in for an apiserver at the one point AC11
// turns on.
//
// `honor` false is the server that does not — and the branch it takes is the
// documented hazard rather than an error.
func metadataNegotiatingServer(t *testing.T, honor bool) *httptest.Server {
	t.Helper()
	const metadata = `{"kind":"PartialObjectMetadata","apiVersion":"meta.k8s.io/v1",` +
		`"metadata":{"name":"db","namespace":"ops","resourceVersion":"4242","uid":"uid-db"}}`
	whole := `{"kind":"Secret","apiVersion":"v1",` +
		`"metadata":{"name":"db","namespace":"ops","resourceVersion":"4242","uid":"uid-db"},` +
		`"data":{"token":"` + secretTokenInFixture + `"}}`

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if honor && acceptsPartialObjectMetadata(r.Header.Get("Accept")) {
			_, _ = io.WriteString(w, metadata)
			return
		}
		_, _ = io.WriteString(w, whole)
	}))
}

// acceptsPartialObjectMetadata matches the way the apiserver does.
func acceptsPartialObjectMetadata(accept string) bool {
	for _, media := range strings.Split(accept, ",") {
		params := strings.Split(strings.TrimSpace(media), ";")
		got := map[string]string{}
		for _, param := range params[1:] {
			key, value, _ := strings.Cut(param, "=")
			got[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
		if got["as"] == partialObjectMetadataKind &&
			got["g"] == metav1.SchemeGroupVersion.Group &&
			got["v"] == metav1.SchemeGroupVersion.Version {
			return true
		}
	}
	return false
}

// targetServiceAgainst points a KubeService at a test server with Secret and
// Deployment already mapped, so a case exercises the read rather than discovery.
func targetServiceAgainst(host string) *KubeService {
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}, {Group: "apps", Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "Secret"}, meta.RESTScopeNamespace)
	mapper.Add(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, meta.RESTScopeNamespace)
	service := &KubeService{config: &rest.Config{Host: host}}
	service.mappers.mapper = mapper
	return service
}

func namespaceOf(s string) *string { return &s }

// AC11: the gate's read of a sensitive kind goes out as PartialObjectMetadata
// and comes back without the value.
func TestPreconditionUsesPartialObjectMetadataForGatedKinds(t *testing.T) {
	t.Run("honoured", func(t *testing.T) {
		server := metadataNegotiatingServer(t, true)
		defer server.Close()

		state, err := targetServiceAgainst(server.URL).ReadTarget(context.Background(), TargetRef{
			APIVersion:   "v1",
			Kind:         "Secret",
			Namespace:    namespaceOf("ops"),
			Name:         "db",
			MetadataOnly: true,
		})
		if err != nil {
			t.Fatalf("ReadTarget() = %v, want the metadata", err)
		}
		if state.ResourceVersion != "4242" {
			t.Errorf("resourceVersion = %q, want the precondition AC6 compares against", state.ResourceVersion)
		}
		if state.UID != "uid-db" {
			t.Errorf("uid = %q, want the object identity AC6 compares against", state.UID)
		}
		if state.Replicas != nil {
			t.Errorf("replicas = %v, want nil — PartialObjectMetadata carries no spec", *state.Replicas)
		}
	})

	t.Run("ignored", func(t *testing.T) {
		server := metadataNegotiatingServer(t, false)
		defer server.Close()

		state, err := targetServiceAgainst(server.URL).ReadTarget(context.Background(), TargetRef{
			APIVersion:   "v1",
			Kind:         "Secret",
			Namespace:    namespaceOf("ops"),
			Name:         "db",
			MetadataOnly: true,
		})
		if err == nil {
			t.Fatalf("ReadTarget() = %+v, want a refusal rather than a whole Secret", state)
		}
		if strings.Contains(err.Error(), secretTokenInFixture) {
			t.Errorf("the refusal quotes the value it was refusing to read: %v", err)
		}
		if !strings.Contains(err.Error(), partialObjectMetadataKind) {
			t.Errorf("refusal = %q, want it to name the representation that was asked for", err)
		}
	})
}

func TestMetadataAcceptIsDerivedAndOffersNoWholeObjectFallback(t *testing.T) {
	want := "application/json;as=PartialObjectMetadata;v=" +
		metav1.SchemeGroupVersion.Version + ";g=" + metav1.SchemeGroupVersion.Group
	if metadataAccept != want {
		t.Errorf("metadataAccept = %q, want %q", metadataAccept, want)
	}
	// tableAccept offers plain json alongside on purpose; this must not. Listing
	// the fallback is what lets a parameter the server cannot serve be answered
	// with the whole object instead of refused.
	if strings.Contains(metadataAccept, ", application/json") {
		t.Errorf("metadataAccept offers a whole-object fallback: %q", metadataAccept)
	}
}

// An ordinary kind is read whole, because the reason for cutting the body away
// is the value in it and a Deployment has none.
func TestReadTargetOfAnOrdinaryKindCarriesTheReplicaCount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); strings.Contains(got, partialObjectMetadataKind) {
			t.Errorf("Accept = %q, want no metadata restriction for a kind that carries no credential", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"kind":"Deployment","apiVersion":"apps/v1",`+
			`"metadata":{"name":"api","namespace":"ops","resourceVersion":"77"},"spec":{"replicas":3}}`)
	}))
	defer server.Close()

	state, err := targetServiceAgainst(server.URL).ReadTarget(context.Background(), TargetRef{
		APIVersion:  "apps/v1",
		Kind:        "Deployment",
		Namespace:   namespaceOf("ops"),
		Name:        "api",
		Subresource: ScaleSubresource,
	})
	if err != nil {
		t.Fatalf("ReadTarget() = %v", err)
	}
	if state.Replicas == nil || *state.Replicas != 3 {
		t.Fatalf("replicas = %v, want the current count AC3 shows beside the target", state.Replicas)
	}
	if state.ResourceVersion != "77" {
		t.Errorf("resourceVersion = %q, want 77", state.ResourceVersion)
	}
}

// An approval that cannot be tied to a state is not an approval of a state. A
// body with no resourceVersion is refused rather than treated as "unchanged",
// which is what an empty-string comparison would silently do.
func TestReadTargetRefusesABodyWithNoResourceVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"kind":"Deployment","apiVersion":"apps/v1","metadata":{"name":"api"}}`)
	}))
	defer server.Close()

	_, err := targetServiceAgainst(server.URL).ReadTarget(context.Background(), TargetRef{
		APIVersion: "apps/v1", Kind: "Deployment", Namespace: namespaceOf("ops"), Name: "api",
	})
	if err == nil {
		t.Fatal("ReadTarget() = nil error, want a refusal for a body with no resourceVersion")
	}
}

// The refusal comes before any request goes out.
func TestReadTargetRefusesAnUnnamedObject(t *testing.T) {
	reached := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	defer server.Close()

	_, err := targetServiceAgainst(server.URL).ReadTarget(context.Background(), TargetRef{
		APIVersion: "v1", Kind: "Secret", Namespace: namespaceOf("ops"),
	})
	if err == nil {
		t.Fatal("ReadTarget() = nil error, want a refusal when no object is named")
	}
	if reached {
		t.Error("the apiserver was called for an object with no name")
	}
}
