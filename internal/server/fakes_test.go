package server_test

import (
	"context"
	"sync"

	"github.com/dlddu/homelab-k3s-mcp/internal/awsconfig"
	"github.com/dlddu/homelab-k3s-mcp/internal/github"
	"github.com/dlddu/homelab-k3s-mcp/internal/grafana"
	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
	"github.com/dlddu/homelab-k3s-mcp/internal/opensearch"
)

type execCall struct {
	namespace string
	selector  string
	container *string
	command   []string
}

type logCall struct {
	kind      k8s.WorkloadKind
	namespace string
	name      string
	options   k8s.LogOptions
}

type describeCall struct {
	namespace string
	target    k8s.PodTarget
}

type restartCall struct {
	kind      k8s.WorkloadKind
	namespace string
	name      string
}

type scaleCall struct {
	kind      k8s.WorkloadKind
	namespace string
	name      string
	replicas  int32
}

type listCall struct {
	kind      k8s.WorkloadKind
	namespace *string
}

type fakeK8s struct {
	mu sync.Mutex

	items      []any
	namespaces []any

	namespaceCalls int
	lastList       *listCall
	restarts       []restartCall
	scales         []scaleCall
	execCalls      []execCall
	logCalls       []logCall
	describeCalls  []describeCall

	scaleResponse    func() (int32, error)
	execResponse     func() (*k8s.ExecOutcome, error)
	logResponse      func() (*k8s.LogResult, error)
	describeResponse func() (*k8s.PodDescription, error)
}

func (f *fakeK8s) ListNamespaces(context.Context) ([]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.namespaceCalls++
	return f.namespaces, nil
}

func (f *fakeK8s) ListWorkloads(_ context.Context, kind k8s.WorkloadKind, namespace *string) ([]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastList = &listCall{kind: kind, namespace: namespace}
	return f.items, nil
}

func (f *fakeK8s) RolloutRestart(_ context.Context, kind k8s.WorkloadKind, namespace, name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restarts = append(f.restarts, restartCall{kind: kind, namespace: namespace, name: name})
	return "2026-05-07T00:00:00Z", nil
}

func (f *fakeK8s) ScaleWorkload(_ context.Context, kind k8s.WorkloadKind, namespace, name string, replicas int32) (int32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scales = append(f.scales, scaleCall{kind: kind, namespace: namespace, name: name, replicas: replicas})
	if f.scaleResponse != nil {
		return f.scaleResponse()
	}
	return replicas, nil
}

func (f *fakeK8s) WorkloadLogs(_ context.Context, kind k8s.WorkloadKind, namespace, name string, opts k8s.LogOptions) (*k8s.LogResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logCalls = append(f.logCalls, logCall{kind: kind, namespace: namespace, name: name, options: opts})
	if f.logResponse != nil {
		return f.logResponse()
	}
	return &k8s.LogResult{Pod: name + "-pod-0", Container: opts.Container, Logs: ""}, nil
}

func (f *fakeK8s) DescribePod(_ context.Context, namespace string, target k8s.PodTarget) (*k8s.PodDescription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.describeCalls = append(f.describeCalls, describeCall{namespace: namespace, target: target})
	if f.describeResponse != nil {
		return f.describeResponse()
	}
	var inferred string
	switch target.Mode {
	case k8s.TargetName:
		inferred = target.Name
	case k8s.TargetSelector:
		inferred = "pod-for-" + target.Selector
	case k8s.TargetWorkload:
		inferred = target.Kind.String() + "-" + target.WorkloadName + "-0"
	}
	return &k8s.PodDescription{
		Name:            inferred,
		Namespace:       namespace,
		Labels:          map[string]string{},
		Annotations:     map[string]string{},
		NodeSelector:    map[string]string{},
		OwnerReferences: []any{},
		Conditions:      []k8s.PodConditionInfo{},
		InitContainers:  []k8s.ContainerInfo{},
		Containers:      []k8s.ContainerInfo{},
		Events:          []k8s.PodEventInfo{},
	}, nil
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

func unavailableK8s() k8s.Service         { return k8s.NewUnavailable("") }
func unavailableGitHub() github.Service   { return github.NewUnavailable("") }
func unavailableAWS() awsconfig.Service   { return awsconfig.NewUnavailable("") }
func unavailableGrafana() grafana.Service { return grafana.NewUnavailable("") }

func unavailableOpenSearch() opensearch.Service { return opensearch.NewUnavailable("") }

func int32Ptr(v int32) *int32 { return &v }
