package k8s

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// metadataAccept asks the apiserver for PartialObjectMetadata — metadata cut
// server-side, with the object's own body never put on the wire
// (prd-approval-gate AC11).
//
// The parameters are derived rather than typed out for the reason tableAccept
// records. What is different here is the cost of getting them wrong: there the
// fallback yields an empty table, here it yields a whole Secret read before
// anyone approved reading it, which is the exact failure AC11 exists to stop.
//
// So this header, unlike tableAccept, offers no `application/json` alongside.
// Listing the fallback is what lets a mismatch be answered rather than refused;
// with only this media type an unservable parameter comes back 406, which is a
// failed read instead of a leaked one. readTargetState checks the response kind
// as well, because a server that answers a whole object anyway has to hit
// something.
var metadataAccept = fmt.Sprintf(
	"application/json;as=PartialObjectMetadata;v=%s;g=%s",
	metav1.SchemeGroupVersion.Version, metav1.SchemeGroupVersion.Group,
)

// partialObjectMetadataKind is the kind a PartialObjectMetadata response
// declares. An ordinary object declares its own kind instead, which is what
// separates "the header was honoured" from "the header was ignored".
const partialObjectMetadataKind = "PartialObjectMetadata"

// TargetRef addresses the object the gate reads on its own behalf before it
// asks a human about it (prd-approval-gate AC11).
type TargetRef struct {
	APIVersion string
	Kind       string
	Namespace  *string
	Name       string

	// Subresource is carried for the audit trail and for the caller's own
	// bookkeeping; the read itself always addresses the object, because the two
	// facts the gate needs (resourceVersion, spec.replicas) live there and
	// AC11's declared pair is `get on ⟨kind⟩` rather than on a subresource.
	Subresource string

	// MetadataOnly restricts the read to PartialObjectMetadata. The caller sets
	// it for a sensitive kind: reading a Secret whole to learn its
	// resourceVersion would put the value in this process before anyone
	// approved it, and a rejection after that point has prevented nothing.
	MetadataOnly bool
}

// TargetState is what the gate learns about the target. It is deliberately
// narrow — the gate needs enough to describe the change and to notice the
// object moving underneath the approval, and nothing beyond that has a reason
// to be here.
type TargetState struct {
	// ResourceVersion is the precondition of AC6: the same value read again at
	// execution time means nobody changed the object while the operator looked
	// at it.
	ResourceVersion string

	// UID distinguishes a recreated object from the one that was approved. A
	// pod with the same name is a different pod (AC6).
	UID string

	// Replicas is spec.replicas when the object carries one, for the "current →
	// target" of AC3. A PartialObjectMetadata response has no spec, so this is
	// nil for sensitive kinds — which is correct rather than unfortunate: no
	// sensitive kind has replicas.
	Replicas *int64
}

// TargetReader is the gate's own kubernetes read surface.
type TargetReader interface {
	ReadTarget(ctx context.Context, ref TargetRef) (*TargetState, error)
}

// UnavailableTargetReader refuses every read with a fixed reason, so a
// deployment with no kubernetes client refuses gated calls instead of
// describing them from nothing.
type UnavailableTargetReader struct {
	reason string
}

// NewUnavailableTargetReader builds a refusing reader.
func NewUnavailableTargetReader(reason string) *UnavailableTargetReader {
	if reason == "" {
		reason = "kubernetes client is not configured"
	}
	return &UnavailableTargetReader{reason: reason}
}

func (u *UnavailableTargetReader) ReadTarget(context.Context, TargetRef) (*TargetState, error) {
	return nil, unavailableErr(u.reason)
}

// ReadTarget reads the object a gated call names. *KubeService implements both
// this and Service; that one object can do both jobs does not make them one
// permission, which is why they are separate interfaces.
func (s *KubeService) ReadTarget(ctx context.Context, ref TargetRef) (*TargetState, error) {
	if ref.Name == "" {
		return nil, apiErrorf("a gated call needs a named object before it can be approved; got none for %s", ref.Kind)
	}
	res, err := s.resolve(ctx, ref.APIVersion, ref.Kind)
	if err != nil {
		return nil, err
	}
	namespace, err := objectNamespace(res, ref.Kind, ref.Namespace)
	if err != nil {
		return nil, err
	}

	client, err := s.restClientFor(res.gvr)
	if err != nil {
		return nil, err
	}
	req := client.Get().Resource(res.gvr.Resource).Name(ref.Name)
	if res.namespaced {
		req = req.Namespace(namespace)
	}
	if ref.MetadataOnly {
		req = req.SetHeader("Accept", metadataAccept)
	}
	raw, err := req.DoRaw(ctx)
	if err != nil {
		return nil, s.apiCallError(err, "get", res.gvr.Resource)
	}
	return readTargetState(raw, ref)
}

// readTargetState decodes what the apiserver sent and, for a sensitive kind,
// refuses anything that is not the representation that was asked for.
//
// The decode is by hand rather than into a typed object because the two shapes
// this has to accept — PartialObjectMetadata and an arbitrary kind's whole body
// — have no common Go type, and the three fields wanted from either are in the
// same places.
func readTargetState(raw []byte, ref TargetRef) (*TargetState, error) {
	var body struct {
		Kind       string `json:"kind"`
		APIVersion string `json:"apiVersion"`
		Metadata   struct {
			ResourceVersion string `json:"resourceVersion"`
			UID             string `json:"uid"`
		} `json:"metadata"`
		Spec struct {
			Replicas *int64 `json:"replicas"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, APIError(fmt.Sprintf("apiserver answered the pre-approval read with a body this server cannot decode: %v", err))
	}

	if ref.MetadataOnly && body.Kind != partialObjectMetadataKind {
		// Reached when a server answers the whole object despite being asked
		// only for metadata. Falling back to it would mean the value is already
		// here, so there is nothing left for a rejection to protect.
		return nil, apiErrorf(
			"refusing to describe %s %s/%s: asked the apiserver for %s and it answered %s %s; "+
				"this server does not fall back to whole objects for a gated kind",
			ref.Kind, ref.Name, ref.Subresource,
			partialObjectMetadataKind, body.APIVersion, body.Kind,
		)
	}
	if body.Metadata.ResourceVersion == "" {
		return nil, apiErrorf(
			"apiserver answered the pre-approval read of %s %s without a resourceVersion; "+
				"without it the approval cannot be tied to a state",
			ref.Kind, ref.Name,
		)
	}

	return &TargetState{
		ResourceVersion: body.Metadata.ResourceVersion,
		UID:             body.Metadata.UID,
		Replicas:        body.Spec.Replicas,
	}, nil
}
