package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	collectionPageSize   = 500
	collectionMaxPages   = 100
	collectionMaxTargets = 10000
)

var collectionMetadataAccept = fmt.Sprintf(
	"application/json;as=PartialObjectMetadataList;v=%s;g=%s",
	metav1.SchemeGroupVersion.Version, metav1.SchemeGroupVersion.Group,
)

// CollectionTargetRef selects the namespace-local objects the gate will describe.
type CollectionTargetRef struct {
	APIVersion    string
	Kind          string
	Namespace     string
	LabelSelector string
	FieldSelector string
}

// CollectionTarget identifies one object in a pre-approval collection snapshot.
type CollectionTarget struct {
	Name            string
	Namespace       string
	UID             string
	ResourceVersion string
}

// CollectionTargetState contains every selected object's identity and version.
type CollectionTargetState struct {
	ResourceVersion string
	Targets         []CollectionTarget
}

// CollectionTargetReader is the gate's collection read surface, separate from Service.
type CollectionTargetReader interface {
	ListTargets(context.Context, CollectionTargetRef) (*CollectionTargetState, error)
}

var _ CollectionTargetReader = (*KubeService)(nil)
var _ CollectionTargetReader = (*UnavailableTargetReader)(nil)

// ListTargets refuses collection reads when the gate's Kubernetes client is absent.
func (u *UnavailableTargetReader) ListTargets(context.Context, CollectionTargetRef) (*CollectionTargetState, error) {
	return nil, unavailableErr(u.reason)
}

// ListTargets reads a complete, bounded metadata snapshot for a future collection approval.
func (s *KubeService) ListTargets(ctx context.Context, ref CollectionTargetRef) (*CollectionTargetState, error) {
	if ref.Namespace == "" || len(validation.IsDNS1123Label(ref.Namespace)) != 0 {
		return nil, apiErrorf("a collection approval requires one valid namespace")
	}
	res, err := s.resolve(ctx, ref.APIVersion, ref.Kind)
	if err != nil {
		return nil, err
	}
	if !res.namespaced {
		return nil, apiErrorf("a collection approval cannot select cluster-scoped resources")
	}
	client, err := s.restClientFor(res.gvr)
	if err != nil {
		return nil, err
	}
	snapshot := &CollectionTargetState{Targets: []CollectionTarget{}}
	names := make(map[string]bool)
	cursors := make(map[string]bool)
	cursor := ""
	for pageIndex := 0; pageIndex < collectionMaxPages; pageIndex++ {
		req := client.Get().Namespace(ref.Namespace).Resource(res.gvr.Resource).
			SetHeader("Accept", collectionMetadataAccept).
			Param("limit", fmt.Sprint(collectionPageSize)).MaxRetries(0)
		if ref.LabelSelector != "" {
			req = req.Param("labelSelector", ref.LabelSelector)
		}
		if ref.FieldSelector != "" {
			req = req.Param("fieldSelector", ref.FieldSelector)
		}
		if cursor != "" {
			req = req.Param("continue", cursor)
		}
		raw, err := req.DoRaw(ctx)
		if err != nil {
			return nil, s.apiCallError(err, "list", res.gvr.Resource)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil || fields["items"] == nil {
			return nil, apiErrorf("collection metadata has no decodable items field")
		}
		var page metav1.PartialObjectMetadataList
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, apiErrorf("cannot decode the collection metadata snapshot")
		}
		if page.Kind != "PartialObjectMetadataList" || page.APIVersion != metav1.SchemeGroupVersion.String() {
			return nil, apiErrorf("collection approval requires meta.k8s.io/v1 PartialObjectMetadataList; whole-object fallback is refused")
		}
		if page.ResourceVersion == "" || (pageIndex > 0 && page.ResourceVersion != snapshot.ResourceVersion) {
			return nil, apiErrorf("collection pages do not identify one resourceVersion snapshot")
		}
		snapshot.ResourceVersion = page.ResourceVersion
		if len(snapshot.Targets)+len(page.Items) > collectionMaxTargets {
			return nil, apiErrorf("collection snapshot exceeds %d targets; narrow the selection", collectionMaxTargets)
		}
		for _, item := range page.Items {
			if item.Kind != "PartialObjectMetadata" || item.APIVersion != metav1.SchemeGroupVersion.String() ||
				item.Name == "" || item.Namespace != ref.Namespace || item.UID == "" || item.ResourceVersion == "" {
				return nil, apiErrorf("collection metadata contains an incomplete or out-of-namespace target")
			}
			if names[item.Name] {
				return nil, apiErrorf("collection metadata contains a duplicate target")
			}
			names[item.Name] = true
			snapshot.Targets = append(snapshot.Targets, CollectionTarget{
				Name: item.Name, Namespace: item.Namespace,
				UID: string(item.UID), ResourceVersion: item.ResourceVersion,
			})
		}
		if page.RemainingItemCount != nil && (*page.RemainingItemCount < 0 || (page.Continue == "" && *page.RemainingItemCount != 0)) {
			return nil, apiErrorf("collection metadata reports an incomplete snapshot")
		}
		if page.Continue == "" {
			sort.Slice(snapshot.Targets, func(i, j int) bool { return snapshot.Targets[i].Name < snapshot.Targets[j].Name })
			return snapshot, nil
		}
		if cursors[page.Continue] {
			return nil, apiErrorf("collection metadata repeats a continuation token")
		}
		cursors[page.Continue] = true
		cursor = page.Continue
	}
	return nil, apiErrorf("collection snapshot exceeds %d pages; narrow the selection", collectionMaxPages)
}
