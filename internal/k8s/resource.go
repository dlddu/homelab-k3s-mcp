package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
)

// tableAccept asks the apiserver for the same server-side rendering kubectl
// gets (prd-resource-generic AC2, AC17).
//
// The parameters are derived from metav1 rather than typed out. The apiserver
// matches them against a GroupVersionKind and treats one it does not recognise
// as "no alternate representation available", which — because the header also
// offers plain application/json — is answered with the ordinary list instead of
// an error. A typo there is therefore not a failed request but an empty table,
// and no test that feeds this package a Table it wrote itself can see it.
var tableAccept = fmt.Sprintf(
	"application/json;as=Table;v=%s;g=%s, application/json",
	metav1.SchemeGroupVersion.Version, metav1.SchemeGroupVersion.Group,
)

const (
	// ListDefaultLimit and ListMaxLimit are AC3's bounds.
	ListDefaultLimit int64 = 100
	ListMaxLimit     int64 = 500
)

// ListQuery addresses a set of objects by coordinate (prd-resource-generic AC1).
type ListQuery struct {
	APIVersion    string
	Kind          string
	Namespace     *string
	LabelSelector *string
	FieldSelector *string
	Limit         int64
	Continue      string
}

// ListColumn is one column of the apiserver's Table rendering.
type ListColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// ListResult is a Table response (AC2).
type ListResult struct {
	Resource   string       `json:"resource"`
	Namespaced bool         `json:"namespaced"`
	Columns    []ListColumn `json:"columns"`
	Rows       [][]any      `json:"rows"`
	Continue   string       `json:"continue"`
	Truncated  bool         `json:"truncated"`
	Remaining  *int64       `json:"remaining_item_count"`
}

// ResourceRef addresses a single object, optionally one of its subresources.
type ResourceRef struct {
	APIVersion  string
	Kind        string
	Namespace   *string
	Name        string
	Subresource string
	Log         LogOptions
}

// ResourceResult is one object, or the text a text-typed subresource returns.
type ResourceResult struct {
	Resource  string         `json:"resource"`
	Namespace string         `json:"namespace"`
	Object    map[string]any `json:"object,omitempty"`
	Text      string         `json:"text,omitempty"`
}

// APIResource is one row of the discovery answer (AC20).
type APIResource struct {
	Group      string   `json:"group"`
	Version    string   `json:"version"`
	Kind       string   `json:"kind"`
	Name       string   `json:"name"`
	Namespaced bool     `json:"namespaced"`
	ShortNames []string `json:"short_names"`
	Verbs      []string `json:"verbs"`
}

// listParameterCodec encodes ListOptions against a scheme holding nothing but
// meta/v1, the way client-go's own dynamic client does. The typed scheme would
// refuse to encode parameters for a group version it has never heard of, and a
// CRD is exactly that (AC1 puts arbitrary CRDs in scope).
var (
	metaV1GroupVersion  = schema.GroupVersion{Version: "v1"}
	listParameterScheme = runtime.NewScheme()
	listParameterCodec  = runtime.NewParameterCodec(listParameterScheme)
)

func init() {
	metav1.AddToGroupVersion(listParameterScheme, metaV1GroupVersion)
}

// resolved is a coordinate after discovery has answered.
type resolved struct {
	gvr        schema.GroupVersionResource
	namespaced bool
}

// mapperCache holds the discovery-derived RESTMapper. Discovery is a round trip
// per group, so it is built once; an unknown kind invalidates it exactly once
// before the kind is declared missing, which is what lets a CRD installed after
// start-up be listed without a restart (AC20).
type mapperCache struct {
	mu     sync.Mutex
	mapper meta.RESTMapper
}

func (s *KubeService) discoveryClient() (discovery.DiscoveryInterface, error) {
	dc, err := discovery.NewDiscoveryClientForConfig(s.config)
	if err != nil {
		return nil, unavailableErr(fmt.Sprintf("init discovery client: %v", err))
	}
	return dc, nil
}

func (s *KubeService) restMapper(refresh bool) (meta.RESTMapper, error) {
	s.mappers.mu.Lock()
	defer s.mappers.mu.Unlock()
	if s.mappers.mapper != nil && !refresh {
		return s.mappers.mapper, nil
	}
	dc, err := s.discoveryClient()
	if err != nil {
		return nil, err
	}
	groups, err := restmapper.GetAPIGroupResources(dc)
	if err != nil {
		return nil, APIError(fmt.Sprintf("discovery failed: %v", err))
	}
	s.mappers.mapper = restmapper.NewDiscoveryRESTMapper(groups)
	return s.mappers.mapper, nil
}

// resolve turns apiVersion+kind into a resource path. A kind the cluster does
// not serve is refused with candidates rather than guessed at — a guessed path
// produces a 404 that reads like "the object is gone" instead of "that kind
// does not exist here" (AC20).
func (s *KubeService) resolve(ctx context.Context, apiVersion, kind string) (resolved, error) {
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return resolved{}, apiErrorf("apiVersion %q is not a group/version: %v", apiVersion, err)
	}

	for _, refresh := range []bool{false, true} {
		mapper, err := s.restMapper(refresh)
		if err != nil {
			return resolved{}, err
		}
		mapping, err := mapper.RESTMapping(schema.GroupKind{Group: gv.Group, Kind: kind}, gv.Version)
		if err == nil {
			return resolved{
				gvr:        mapping.Resource,
				namespaced: mapping.Scope.Name() == meta.RESTScopeNameNamespace,
			}, nil
		}
		if !meta.IsNoMatchError(err) {
			return resolved{}, APIError(err.Error())
		}
	}
	return resolved{}, apiErrorf("%s", s.unknownKindMessage(ctx, apiVersion, kind))
}

func (s *KubeService) unknownKindMessage(ctx context.Context, apiVersion, kind string) string {
	msg := fmt.Sprintf("cluster serves no kind %q in %q", kind, apiVersion)
	candidates := s.similarKinds(ctx, kind)
	if len(candidates) == 0 {
		return msg + "; call api_resources for the kinds this cluster serves"
	}
	return msg + "; did you mean " + strings.Join(candidates, ", ") + "? (api_resources lists them all)"
}

func (s *KubeService) similarKinds(ctx context.Context, kind string) []string {
	all, err := s.APIResources(ctx)
	if err != nil {
		return nil
	}
	want := strings.ToLower(kind)
	seen := map[string]bool{}
	var out []string
	for _, r := range all {
		lower := strings.ToLower(r.Kind)
		if lower == want || !strings.Contains(lower, want) && !strings.Contains(want, lower) {
			continue
		}
		label := r.Kind
		if r.Group != "" {
			label = r.Group + "/" + r.Version + "/" + r.Kind
		}
		if seen[label] {
			continue
		}
		seen[label] = true
		out = append(out, label)
	}
	sort.Strings(out)
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

func (s *KubeService) restClientFor(gvr schema.GroupVersionResource) (rest.Interface, error) {
	cfg := rest.CopyConfig(s.config)
	gv := gvr.GroupVersion()
	cfg.GroupVersion = &gv
	cfg.APIPath = "/apis"
	if gv.Group == "" {
		cfg.APIPath = "/api"
	}
	cfg.NegotiatedSerializer = scheme.Codecs.WithoutConversion()
	client, err := rest.RESTClientFor(cfg)
	if err != nil {
		return nil, unavailableErr(fmt.Sprintf("init rest client: %v", err))
	}
	return client, nil
}

// ListResources answers with the apiserver's Table rendering of a coordinate.
func (s *KubeService) ListResources(ctx context.Context, q ListQuery) (*ListResult, error) {
	res, err := s.resolve(ctx, q.APIVersion, q.Kind)
	if err != nil {
		return nil, err
	}
	if !res.namespaced && q.Namespace != nil {
		return nil, apiErrorf(
			"%s is cluster-scoped; drop namespace rather than having it silently ignored",
			q.Kind,
		)
	}

	limit := q.Limit
	if limit <= 0 {
		limit = ListDefaultLimit
	}

	opts := metav1.ListOptions{Limit: limit, Continue: q.Continue}
	if q.LabelSelector != nil {
		opts.LabelSelector = *q.LabelSelector
	}
	if q.FieldSelector != nil {
		opts.FieldSelector = *q.FieldSelector
	}

	client, err := s.restClientFor(res.gvr)
	if err != nil {
		return nil, err
	}
	req := client.Get().Resource(res.gvr.Resource).SetHeader("Accept", tableAccept)
	if res.namespaced && q.Namespace != nil {
		req = req.Namespace(*q.Namespace)
	}
	raw, err := req.SpecificallyVersionedParams(&opts, listParameterCodec, metaV1GroupVersion).DoRaw(ctx)
	if err != nil {
		return nil, s.apiCallError(err, "list", res.gvr.Resource)
	}

	var table metav1.Table
	if err := json.Unmarshal(raw, &table); err != nil {
		return nil, APIError(fmt.Sprintf("apiserver returned a list this server cannot render as a table: %v", err))
	}
	// An ordinary list decodes into this struct without error and leaves every
	// field zero, so the only thing separating "the namespace is empty" from
	// "the table was never negotiated" is the kind the body declares.
	if table.Kind != "Table" {
		return nil, apiErrorf(
			"apiserver answered %s %s instead of a Table; this server renders only the "+
				"apiserver's own table and will not fall back to whole objects",
			table.APIVersion, table.Kind,
		)
	}

	columns := make([]ListColumn, 0, len(table.ColumnDefinitions))
	for _, c := range table.ColumnDefinitions {
		columns = append(columns, ListColumn{Name: c.Name, Type: c.Type})
	}
	rows := make([][]any, 0, len(table.Rows))
	for i := range table.Rows {
		rows = append(rows, table.Rows[i].Cells)
	}

	result := &ListResult{
		Resource:   res.gvr.Resource,
		Namespaced: res.namespaced,
		Columns:    columns,
		Rows:       rows,
		Continue:   table.ListMeta.Continue,
		Truncated:  table.ListMeta.Continue != "",
		Remaining:  table.ListMeta.RemainingItemCount,
	}
	return result, nil
}

// GetResource reads one object whole, or the text a text-typed subresource
// returns, with AC4's noise stripped.
func (s *KubeService) GetResource(ctx context.Context, ref ResourceRef) (*ResourceResult, error) {
	res, err := s.resolve(ctx, ref.APIVersion, ref.Kind)
	if err != nil {
		return nil, err
	}
	if !res.namespaced && ref.Namespace != nil {
		return nil, apiErrorf(
			"%s is cluster-scoped; drop namespace rather than having it silently ignored",
			ref.Kind,
		)
	}
	if res.namespaced && ref.Namespace == nil {
		return nil, apiErrorf("%s is namespaced; namespace is required", ref.Kind)
	}

	namespace := ""
	if ref.Namespace != nil {
		namespace = *ref.Namespace
	}

	if ref.Subresource == "log" {
		return s.podLogs(ctx, res, namespace, ref)
	}

	dyn, err := dynamic.NewForConfig(s.config)
	if err != nil {
		return nil, unavailableErr(fmt.Sprintf("init dynamic client: %v", err))
	}
	var api dynamic.ResourceInterface = dyn.Resource(res.gvr)
	if res.namespaced {
		api = dyn.Resource(res.gvr).Namespace(namespace)
	}

	var subresources []string
	if ref.Subresource != "" {
		subresources = append(subresources, ref.Subresource)
	}
	obj, err := api.Get(ctx, ref.Name, metav1.GetOptions{}, subresources...)
	if err != nil {
		return nil, s.apiCallError(err, "get", subresourcePath(res.gvr.Resource, ref.Subresource))
	}

	object := obj.UnstructuredContent()
	stripNoise(object)
	return &ResourceResult{
		Resource:  subresourcePath(res.gvr.Resource, ref.Subresource),
		Namespace: namespace,
		Object:    object,
	}, nil
}

func (s *KubeService) podLogs(ctx context.Context, res resolved, namespace string, ref ResourceRef) (*ResourceResult, error) {
	if res.gvr.Resource != "pods" || res.gvr.Group != "" {
		return nil, apiErrorf("subresource log is served by pods only, not by %s", ref.Kind)
	}

	opts := &corev1.PodLogOptions{
		Previous:   ref.Log.Previous,
		Timestamps: ref.Log.Timestamps,
	}
	if ref.Log.Container != nil {
		opts.Container = *ref.Log.Container
	}
	if ref.Log.TailLines != nil {
		opts.TailLines = ref.Log.TailLines
	}
	if ref.Log.SinceSeconds != nil {
		opts.SinceSeconds = ref.Log.SinceSeconds
	}

	raw, err := s.clientset.CoreV1().Pods(namespace).GetLogs(ref.Name, opts).DoRaw(ctx)
	if err != nil {
		return nil, s.apiCallError(err, "get", "pods/log")
	}
	return &ResourceResult{
		Resource:  "pods/log",
		Namespace: namespace,
		Text:      string(raw),
	}, nil
}

// APIResources reports what the cluster actually serves.
func (s *KubeService) APIResources(ctx context.Context) ([]APIResource, error) {
	dc, err := s.discoveryClient()
	if err != nil {
		return nil, err
	}
	lists, err := dc.ServerPreferredResources()
	if err != nil && len(lists) == 0 {
		return nil, APIError(fmt.Sprintf("discovery failed: %v", err))
	}

	out := make([]APIResource, 0, 128)
	for _, list := range lists {
		if list == nil {
			continue
		}
		gv, parseErr := schema.ParseGroupVersion(list.GroupVersion)
		if parseErr != nil {
			continue
		}
		for _, r := range list.APIResources {
			if strings.Contains(r.Name, "/") {
				continue
			}
			out = append(out, APIResource{
				Group:      gv.Group,
				Version:    gv.Version,
				Kind:       r.Kind,
				Name:       r.Name,
				Namespaced: r.Namespaced,
				ShortNames: r.ShortNames,
				Verbs:      r.Verbs,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		return out[i].Kind < out[j].Kind
	})
	return out, nil
}

// apiCallError converts a 403 into a statement about this server's grant rather
// than passing the apiserver's wording through (AC18).
func (s *KubeService) apiCallError(err error, verb, resource string) error {
	if apierrors.IsForbidden(err) {
		return apiErrorf(
			"refused: this server has no (%s, %s) grant. k8s/rbac.yaml is the single source for what it may do; retrying will not change the answer",
			verb, resource,
		)
	}
	return APIError(err.Error())
}

func subresourcePath(resource, subresource string) string {
	if subresource == "" {
		return resource
	}
	return resource + "/" + subresource
}

// stripNoise removes the two fields AC4 names as noise.
func stripNoise(object map[string]any) {
	metadata, ok := object["metadata"].(map[string]any)
	if !ok {
		return
	}
	delete(metadata, "managedFields")
	annotations, ok := metadata["annotations"].(map[string]any)
	if !ok {
		return
	}
	delete(annotations, "kubectl.kubernetes.io/last-applied-configuration")
	if len(annotations) == 0 {
		delete(metadata, "annotations")
	}
}
