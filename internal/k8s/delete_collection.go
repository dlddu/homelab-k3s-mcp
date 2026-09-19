package k8s

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
)

// DeleteCollectionRef names the selection resource_delete_collection removes
// (prd-resource-generic AC11).
//
// Namespace is a value where every other Ref in this package uses a pointer:
// "absent" is not one of the states this coordinate can be in, and a pointer
// would make the widest possible deletion representable.
type DeleteCollectionRef struct {
	APIVersion    string
	Kind          string
	Namespace     string
	LabelSelector string
	FieldSelector string

	GracePeriodSeconds *int64
}

// DeleteCollectionResult reports the request that was made, not what was
// removed: the apiserver answers before finalizers and grace periods run, so a
// count of deleted objects would be a number this server did not observe. The
// count the operator wanted is on the approval screen, read by the gate before
// anyone clicked.
type DeleteCollectionResult struct {
	Resource      string
	Namespace     string
	LabelSelector string
	FieldSelector string
}

// DeleteCollection exercises the deletecollection verb over one namespace's
// selection (prd-resource-generic AC11).
//
// A cluster-scoped kind is refused rather than allowed through unconfined: it
// has no namespace to be held to, so the requirement is unanswerable here
// rather than merely unmet.
//
// Like DeleteResource this sends no Preconditions, and for a sharper reason: a
// whole selection cannot be pinned by one resourceVersion, so AC6 is served by
// re-reading the selection just before this runs.
func (s *KubeService) DeleteCollection(ctx context.Context, ref DeleteCollectionRef) (*DeleteCollectionResult, error) {
	if ref.Namespace == "" {
		return nil, apiErrorf("namespace is required; a collection delete is confined to one namespace")
	}
	res, err := s.resolve(ctx, ref.APIVersion, ref.Kind)
	if err != nil {
		return nil, err
	}
	if !res.namespaced {
		return nil, apiErrorf(
			"%s is cluster-scoped; a collection delete is confined to one namespace and has nothing to confine here",
			ref.Kind,
		)
	}

	dyn, err := dynamic.NewForConfig(s.config)
	if err != nil {
		return nil, unavailableErr(fmt.Sprintf("init dynamic client: %v", err))
	}

	opts := metav1.DeleteOptions{}
	if ref.GracePeriodSeconds != nil {
		grace := *ref.GracePeriodSeconds
		opts.GracePeriodSeconds = &grace
	}

	// The selectors go over as they arrived. Narrowing or normalising them here
	// would have the approval screen describe one selection and the apiserver
	// receive another — and the gate counted its targets with these same two
	// strings.
	list := metav1.ListOptions{
		LabelSelector: ref.LabelSelector,
		FieldSelector: ref.FieldSelector,
	}

	if err := dyn.Resource(res.gvr).Namespace(ref.Namespace).DeleteCollection(ctx, opts, list); err != nil {
		return nil, s.apiCallError(err, "deletecollection", res.gvr.Resource)
	}

	return &DeleteCollectionResult{
		Resource:      res.gvr.Resource,
		Namespace:     ref.Namespace,
		LabelSelector: ref.LabelSelector,
		FieldSelector: ref.FieldSelector,
	}, nil
}
