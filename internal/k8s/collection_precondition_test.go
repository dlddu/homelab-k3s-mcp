package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

func collectionFixture(name string) metav1.PartialObjectMetadata {
	return metav1.PartialObjectMetadata{
		TypeMeta:   metav1.TypeMeta{Kind: "PartialObjectMetadata", APIVersion: "meta.k8s.io/v1"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ops", UID: types.UID("uid-" + name), ResourceVersion: "rv-" + name},
	}
}

func collectionPage(cursor string, items ...metav1.PartialObjectMetadata) metav1.PartialObjectMetadataList {
	return metav1.PartialObjectMetadataList{
		TypeMeta: metav1.TypeMeta{Kind: "PartialObjectMetadataList", APIVersion: "meta.k8s.io/v1"},
		ListMeta: metav1.ListMeta{ResourceVersion: "opaque/list-rv", Continue: cursor},
		Items:    items,
	}
}

func collectionRef() CollectionTargetRef {
	return CollectionTargetRef{APIVersion: "v1", Kind: "Secret", Namespace: "ops"}
}

func TestCollectionSnapshotReadsAllPagesWithMetadataOnly(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/namespaces/ops/secrets" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		media, params, err := mime.ParseMediaType(r.Header.Get("Accept"))
		if err != nil || media != "application/json" || params["as"] != "PartialObjectMetadataList" || params["g"] != "meta.k8s.io" || params["v"] != "v1" {
			t.Errorf("Accept = %q, err = %v", r.Header.Get("Accept"), err)
		}
		q := r.URL.Query()
		if q.Get("labelSelector") != "app in (api,worker)" || q.Get("fieldSelector") != "metadata.name!=keep" || q.Get("limit") != "500" || q.Has("resourceVersion") {
			t.Errorf("query = %v", q)
		}
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1:
			if q.Get("continue") != "" {
				t.Errorf("first cursor = %q", q.Get("continue"))
			}
			item := collectionFixture("z-last")
			item.Annotations = map[string]string{"not-returned": secretTokenInFixture}
			_ = json.NewEncoder(w).Encode(collectionPage("opaque+/=cursor", item))
		case 2:
			if q.Get("continue") != "opaque+/=cursor" {
				t.Errorf("second cursor = %q", q.Get("continue"))
			}
			_ = json.NewEncoder(w).Encode(collectionPage("", collectionFixture("a-first")))
		default:
			t.Errorf("unexpected request %d", calls)
			http.Error(w, "unexpected", 500)
		}
	}))
	defer server.Close()
	ref := collectionRef()
	ref.LabelSelector, ref.FieldSelector = "app in (api,worker)", "metadata.name!=keep"
	snapshot, err := targetServiceAgainst(server.URL).ListTargets(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	want := &CollectionTargetState{ResourceVersion: "opaque/list-rv", Targets: []CollectionTarget{
		{Name: "a-first", Namespace: "ops", UID: "uid-a-first", ResourceVersion: "rv-a-first"},
		{Name: "z-last", Namespace: "ops", UID: "uid-z-last", ResourceVersion: "rv-z-last"},
	}}
	if calls != 2 || !reflect.DeepEqual(snapshot, want) {
		t.Fatalf("snapshot = %+v, calls = %d", snapshot, calls)
	}
	encoded, _ := json.Marshal(snapshot)
	if strings.Contains(string(encoded), secretTokenInFixture) {
		t.Fatal("snapshot exposes metadata beyond the approved identity fields")
	}
}

func TestCollectionSnapshotEmptyAndGroupedResources(t *testing.T) {
	for _, tc := range []struct{ api, kind, path string }{
		{"v1", "Secret", "/api/v1/namespaces/ops/secrets"},
		{"apps/v1", "Deployment", "/apis/apps/v1/namespaces/ops/deployments"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != tc.path {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(collectionPage(""))
			}))
			defer server.Close()
			snapshot, err := targetServiceAgainst(server.URL).ListTargets(context.Background(), CollectionTargetRef{APIVersion: tc.api, Kind: tc.kind, Namespace: "ops"})
			if err != nil || snapshot == nil || snapshot.Targets == nil || len(snapshot.Targets) != 0 || snapshot.ResourceVersion == "" {
				t.Fatalf("empty snapshot = %+v, err = %v", snapshot, err)
			}
		})
	}
}

func TestCollectionSnapshotRejectsScopeBeforeAnyRequest(t *testing.T) {
	for _, namespace := range []string{"", " ", "../ops", "all/namespaces"} {
		t.Run(namespace, func(t *testing.T) {
			snapshot, err := (&KubeService{}).ListTargets(context.Background(), CollectionTargetRef{Namespace: namespace})
			if err == nil || snapshot != nil {
				t.Fatalf("snapshot = %+v, err = %v", snapshot, err)
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Errorf("unexpected %s", r.URL) }))
	defer server.Close()
	svc := targetServiceAgainst(server.URL)
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{{Version: "v1"}})
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "Node"}, meta.RESTScopeRoot)
	svc.mappers.mapper = mapper
	snapshot, err := svc.ListTargets(context.Background(), CollectionTargetRef{APIVersion: "v1", Kind: "Node", Namespace: "ops"})
	if err == nil || snapshot != nil {
		t.Fatalf("cluster-scoped snapshot = %+v, err = %v", snapshot, err)
	}
	snapshot, err = NewUnavailableTargetReader("offline").ListTargets(context.Background(), collectionRef())
	if err == nil || snapshot != nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("unavailable snapshot = %+v, err = %v", snapshot, err)
	}
}

func TestCollectionSnapshotRefusesUntrustedOrIncompletePages(t *testing.T) {
	cases := map[string]func(*metav1.PartialObjectMetadataList){
		"whole object list":        func(p *metav1.PartialObjectMetadataList) { p.Kind, p.APIVersion = "SecretList", "v1" },
		"wrong group":              func(p *metav1.PartialObjectMetadataList) { p.APIVersion = "v1" },
		"wrong version":            func(p *metav1.PartialObjectMetadataList) { p.APIVersion = "meta.k8s.io/v1beta1" },
		"missing list version":     func(p *metav1.PartialObjectMetadataList) { p.ResourceVersion = "" },
		"changed list version":     func(p *metav1.PartialObjectMetadataList) { p.ResourceVersion = "new-snapshot" },
		"whole item":               func(p *metav1.PartialObjectMetadataList) { p.Items[0].Kind, p.Items[0].APIVersion = "Secret", "v1" },
		"wrong item version":       func(p *metav1.PartialObjectMetadataList) { p.Items[0].APIVersion = "meta.k8s.io/v1beta1" },
		"missing name":             func(p *metav1.PartialObjectMetadataList) { p.Items[0].Name = "" },
		"missing identity":         func(p *metav1.PartialObjectMetadataList) { p.Items[0].UID = "" },
		"missing object version":   func(p *metav1.PartialObjectMetadataList) { p.Items[0].ResourceVersion = "" },
		"outside namespace":        func(p *metav1.PartialObjectMetadataList) { p.Items[0].Namespace = "other" },
		"duplicate name":           func(p *metav1.PartialObjectMetadataList) { p.Items[0] = collectionFixture("first") },
		"repeated cursor":          func(p *metav1.PartialObjectMetadataList) { p.Continue = "cursor-1" },
		"missing remaining page":   func(p *metav1.PartialObjectMetadataList) { n := int64(1); p.RemainingItemCount = &n },
		"negative remaining count": func(p *metav1.PartialObjectMetadataList) { n := int64(-1); p.RemainingItemCount = &n },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				page := collectionPage("cursor-1", collectionFixture("first"))
				if calls > 1 {
					page = collectionPage("", collectionFixture("second"))
					mutate(&page)
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(page)
			}))
			defer server.Close()
			snapshot, err := targetServiceAgainst(server.URL).ListTargets(context.Background(), collectionRef())
			if err == nil || snapshot != nil || calls != 2 {
				t.Fatalf("snapshot = %+v, err = %v, calls = %d; partial results must not escape", snapshot, err, calls)
			}
		})
	}
}

func TestCollectionSnapshotDoesNotRestartAfterAPIOrDecodeFailure(t *testing.T) {
	for _, status := range []int{200, 403, 406, 410, 500, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				if calls == 1 {
					_ = json.NewEncoder(w).Encode(collectionPage("cursor-1", collectionFixture("first")))
					return
				}
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "invalid body")
			}))
			defer server.Close()
			snapshot, err := targetServiceAgainst(server.URL).ListTargets(context.Background(), collectionRef())
			if err == nil || snapshot != nil || calls != 2 {
				t.Fatalf("snapshot = %+v, err = %v, requests = %d", snapshot, err, calls)
			}
		})
	}
}

func TestCollectionSnapshotBoundsAndCancellation(t *testing.T) {
	for _, targets := range []bool{false, true} {
		t.Run(fmt.Sprintf("target-bound=%v", targets), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				page := collectionPage(fmt.Sprint(calls))
				if targets {
					for i := 0; i < collectionPageSize; i++ {
						page.Items = append(page.Items, collectionFixture(fmt.Sprintf("%d-%d", calls, i)))
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(page)
			}))
			defer server.Close()
			svc := targetServiceAgainst(server.URL)
			svc.config.QPS = -1
			snapshot, err := svc.ListTargets(context.Background(), collectionRef())
			wantCalls := collectionMaxPages
			if targets {
				wantCalls = collectionMaxTargets/collectionPageSize + 1
			}
			if err == nil || snapshot != nil || calls != wantCalls {
				t.Fatalf("snapshot = %+v, err = %v, requests = %d", snapshot, err, calls)
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	snapshot, err := targetServiceAgainst(server.URL).ListTargets(ctx, collectionRef())
	if err == nil || snapshot != nil {
		t.Fatalf("cancelled snapshot = %+v, err = %v", snapshot, err)
	}
}

func TestCollectionSnapshotRefusesMissingItemsAndSecretBodies(t *testing.T) {
	for _, body := range []string{
		`{"apiVersion":"meta.k8s.io/v1","kind":"PartialObjectMetadataList","metadata":{"resourceVersion":"42"}}`,
		`{"apiVersion":"v1","kind":"SecretList","metadata":{"resourceVersion":"42"},"items":[{"data":{"token":"` + secretTokenInFixture + `"}}]}`,
	} {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, body)
		}))
		snapshot, err := targetServiceAgainst(server.URL).ListTargets(context.Background(), collectionRef())
		server.Close()
		if err == nil || snapshot != nil || calls != 1 {
			t.Fatalf("snapshot = %+v, err = %v, requests = %d", snapshot, err, calls)
		}
		if strings.Contains(err.Error(), secretTokenInFixture) {
			t.Fatal("the rejection echoes a Secret payload")
		}
	}
}
