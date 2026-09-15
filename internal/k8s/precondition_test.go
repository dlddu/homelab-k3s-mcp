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
// turns on: it reads the Accept parameters the way the real one does and answers
// PartialObjectMetadata only when they name a representation it serves.
//
// `honor` false is the server that does not — and the branch it takes is the
// documented hazard rather than an error. AC11 spells it out: "버전·그룹
// 파라미터가 어긋나도 apiserver 는 오류를 주지 않고 전체 객체로 답한다". So the
// wrong branch answers a whole Secret, with its value in it, and the test asserts
// the read is refused rather than that the request failed. A fake that returned
// 406 here would be testing a server that does not exist, and a fake that fed
// this package a PartialObjectMetadata it wrote itself could not see the
// mismatch at all — which is the trap tableAccept already documents.
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

// acceptsPartialObjectMetadata matches the way the apiserver does: on the
// parameter values, not on the string containing the right words.
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
// and comes back without the value, and a server that answers otherwise is
// refused rather than accepted.
//
// The negative half carries the AC. A fallback to the whole object would mean
// the value is in this process before anyone approved reading it, and from that
// point a rejection has prevented nothing — so "refused" and not "used anyway"
// is the entire property.
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

// The Accept header has to be one the apiserver actually matches, and the
// parameters come from metav1 so a hand-typed version cannot drift from it. This
// asserts the value rather than the shape for the reason AC11 gives: a mismatch
// is answered, not rejected, so "it mentions PartialObjectMetadata" would hold
// for a header that leaks every Secret it reads.
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
// is the value in it and a Deployment has none. The current replica count is the
// thing AC3 needs from here.
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

// A gated call with no named object cannot be described, and AC3 would rather
// refuse than put an approval request on screen that does not say what it is
// approving. The refusal comes before any request goes out.
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
