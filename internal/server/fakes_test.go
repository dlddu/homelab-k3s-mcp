package server_test

import (
	"context"
	"sync"

	"github.com/dlddu/homelab-k3s-mcp/internal/awsconfig"
	"github.com/dlddu/homelab-k3s-mcp/internal/github"
	"github.com/dlddu/homelab-k3s-mcp/internal/grafana"
	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
	"github.com/dlddu/homelab-k3s-mcp/internal/opensearch"
	"github.com/dlddu/homelab-k3s-mcp/internal/sessionplatform"
)

type execCall struct {
	namespace string
	selector  string
	container *string
	command   []string
}

type listResourceCall struct {
	query k8s.ListQuery
}

type watchResourceCall struct {
	query k8s.WatchQuery
}

type getResourceCall struct {
	ref k8s.ResourceRef
}

type patchResourceCall struct {
	ref k8s.PatchRef
}

type updateResourceCall struct {
	ref k8s.UpdateRef
}

type deleteResourceCall struct {
	ref k8s.DeleteRef
}

type fakeK8s struct {
	mu sync.Mutex

	apiResources []k8s.APIResource

	apiResourceCalls int
	listCalls        []listResourceCall
	watchCalls       []watchResourceCall
	getCalls         []getResourceCall
	updateCalls      []updateResourceCall
	patchCalls       []patchResourceCall
	deleteCalls      []deleteResourceCall
	execCalls        []execCall

	execResponse  func() (*k8s.ExecOutcome, error)
	listResponse  func() (*k8s.ListResult, error)
	getResponse   func() (*k8s.ResourceResult, error)
	watchResponse func() (*k8s.WatchResult, error)
}

func (f *fakeK8s) APIResources(context.Context) ([]k8s.APIResource, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.apiResourceCalls++
	return f.apiResources, nil
}

func (f *fakeK8s) ListResources(_ context.Context, query k8s.ListQuery) (*k8s.ListResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls = append(f.listCalls, listResourceCall{query: query})
	if f.listResponse != nil {
		return f.listResponse()
	}
	return &k8s.ListResult{Resource: "pods", Namespaced: true}, nil
}

func (f *fakeK8s) WatchResources(_ context.Context, query k8s.WatchQuery) (*k8s.WatchResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.watchCalls = append(f.watchCalls, watchResourceCall{query: query})
	if f.watchResponse != nil {
		return f.watchResponse()
	}
	return &k8s.WatchResult{Resource: "deployments", Namespaced: true, Events: []k8s.WatchEvent{}}, nil
}

func (f *fakeK8s) GetResource(_ context.Context, ref k8s.ResourceRef) (*k8s.ResourceResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCalls = append(f.getCalls, getResourceCall{ref: ref})
	if f.getResponse != nil {
		return f.getResponse()
	}
	return &k8s.ResourceResult{Resource: "pods", Namespace: "default", Object: map[string]any{}}, nil
}

func (f *fakeK8s) UpdateResource(_ context.Context, ref k8s.UpdateRef) (*k8s.ResourceResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updateCalls = append(f.updateCalls, updateResourceCall{ref: ref})
	return &k8s.ResourceResult{Resource: "deployments", Namespace: "default", Object: map[string]any{}}, nil
}

func (f *fakeK8s) PatchResource(_ context.Context, ref k8s.PatchRef) (*k8s.ResourceResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.patchCalls = append(f.patchCalls, patchResourceCall{ref: ref})
	return &k8s.ResourceResult{Resource: "deployments", Namespace: "default", Object: map[string]any{}}, nil
}

func (f *fakeK8s) DeleteResource(_ context.Context, ref k8s.DeleteRef) (*k8s.ResourceResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteCalls = append(f.deleteCalls, deleteResourceCall{ref: ref})
	return &k8s.ResourceResult{Resource: "configmaps", Namespace: "default"}, nil
}

func (f *fakeK8s) ExecInPod(_ context.Context, namespace, labelSelector string, container *string, command []string) (*k8s.ExecOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execCalls = append(f.execCalls, execCall{namespace: namespace, selector: labelSelector, container: container, command: command})
	if f.execResponse != nil {
		return f.execResponse()
	}
	zero := int32(0)
	return &k8s.ExecOutcome{Pod: "dear-baby-abcd", ExitCode: &zero, Success: true}, nil
}

type installationTokenCall struct {
	repositories []string
	permissions  map[string]any
}

type fakeGitHub struct {
	mu       sync.Mutex
	calls    []installationTokenCall
	response func() (*github.InstallationToken, error)
}

func (f *fakeGitHub) CreateInstallationToken(_ context.Context, repositories []string, permissions map[string]any) (*github.InstallationToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, installationTokenCall{repositories: repositories, permissions: permissions})
	if f.response != nil {
		return f.response()
	}
	return &github.InstallationToken{
		Token:               "ghs_fake",
		ExpiresAt:           "2026-05-07T01:00:00Z",
		Permissions:         map[string]any{"contents": "read"},
		RepositorySelection: "all",
	}, nil
}

type fakeAWS struct {
	mu       sync.Mutex
	calls    int
	response func() (*awsconfig.Object, error)
}

func (f *fakeAWS) GetConfig(context.Context) (*awsconfig.Object, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.response != nil {
		return f.response()
	}
	return &awsconfig.Object{
		Bucket:  "homelab-config",
		Key:     "aws/config",
		Content: "[default]\nregion = ap-northeast-2\n",
		Size:    32,
	}, nil
}

type fakeGrafana struct {
	mu       sync.Mutex
	calls    int
	response func() (*grafana.Credentials, error)
}

func (f *fakeGrafana) CreateToken(context.Context) (*grafana.Credentials, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.response != nil {
		return f.response()
	}
	return &grafana.Credentials{
		Token:       "glc_fake",
		ExpiresAt:   "2026-05-27T01:00:00Z",
		MetricsURL:  "https://prometheus-fake.grafana.net/api/prom",
		MetricsUser: "111111",
		LogsURL:     "https://logs-fake.grafana.net",
		LogsUser:    "222222",
	}, nil
}

type searchCall struct {
	query string
	index *string
	size  *int64
}

type putCall struct {
	index    string
	id       *string
	document map[string]any
}

type deleteCall struct {
	index string
	id    string
}

type fakeOpenSearch struct {
	mu sync.Mutex

	searchCalls []searchCall
	putCalls    []putCall
	deleteCalls []deleteCall

	searchResponse func() (*opensearch.SearchResult, error)
	putResponse    func() (*opensearch.PutResult, error)
	deleteResponse func() (*opensearch.DeleteResult, error)
}

func (f *fakeOpenSearch) Search(_ context.Context, query string, index *string, size *int64) (*opensearch.SearchResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.searchCalls = append(f.searchCalls, searchCall{query: query, index: index, size: size})
	if f.searchResponse != nil {
		return f.searchResponse()
	}
	return &opensearch.SearchResult{Total: 0, Hits: []opensearch.Hit{}}, nil
}

func (f *fakeOpenSearch) PutDocument(_ context.Context, index string, id *string, document map[string]any) (*opensearch.PutResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.putCalls = append(f.putCalls, putCall{index: index, id: id, document: document})
	if f.putResponse != nil {
		return f.putResponse()
	}
	docID := "auto-generated"
	if id != nil {
		docID = *id
	}
	return &opensearch.PutResult{Index: index, ID: docID, Result: "created"}, nil
}

func (f *fakeOpenSearch) DeleteDocument(_ context.Context, index, id string) (*opensearch.DeleteResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteCalls = append(f.deleteCalls, deleteCall{index: index, id: id})
	if f.deleteResponse != nil {
		return f.deleteResponse()
	}
	return &opensearch.DeleteResult{Index: index, ID: id, Result: "deleted"}, nil
}

type readCall struct {
	id     string
	offset int64
}

type writeCall struct {
	id      string
	payload string
}

type fakeSessionPlatform struct {
	mu sync.Mutex

	listCalls  int
	readCalls  []readCall
	writeCalls []writeCall

	listResponse  func() ([]sessionplatform.Session, error)
	readResponse  func(id string, offset int64) (*sessionplatform.ReadResult, error)
	writeResponse func(id, payload string) (*sessionplatform.WriteResult, error)
}

func (f *fakeSessionPlatform) ListSessions(context.Context) ([]sessionplatform.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls++
	if f.listResponse != nil {
		return f.listResponse()
	}
	return []sessionplatform.Session{}, nil
}

func (f *fakeSessionPlatform) ReadSession(_ context.Context, id string, offset int64) (*sessionplatform.ReadResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readCalls = append(f.readCalls, readCall{id: id, offset: offset})
	if f.readResponse != nil {
		return f.readResponse(id, offset)
	}
	return &sessionplatform.ReadResult{
		Session: sessionplatform.Session{ID: id, State: "active"},
		Path:    "active",
	}, nil
}

func (f *fakeSessionPlatform) WriteSession(_ context.Context, id, payload string) (*sessionplatform.WriteResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writeCalls = append(f.writeCalls, writeCall{id: id, payload: payload})
	if f.writeResponse != nil {
		return f.writeResponse(id, payload)
	}
	return &sessionplatform.WriteResult{
		Session: sessionplatform.Session{ID: id, State: "active"},
		Path:    "active",
	}, nil
}

func unavailableK8s() k8s.Service         { return k8s.NewUnavailable("") }
func unavailableGitHub() github.Service   { return github.NewUnavailable("") }
func unavailableAWS() awsconfig.Service   { return awsconfig.NewUnavailable("") }
func unavailableGrafana() grafana.Service { return grafana.NewUnavailable("") }

func unavailableOpenSearch() opensearch.Service { return opensearch.NewUnavailable("") }

func unavailableSessionPlatform() sessionplatform.Service { return sessionplatform.NewUnavailable("") }

func int32Ptr(v int32) *int32 { return &v }
