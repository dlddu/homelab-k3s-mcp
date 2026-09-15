package k8s

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// CreateRef identifies one named object to create.
type CreateRef struct {
	APIVersion string
	Kind       string
	Namespace  *string
	Name       string
	Manifest   map[string]any
}

// CreateResource sends one create request without retrying it.
func (s *KubeService) CreateResource(ctx context.Context, ref CreateRef) (*ResourceResult, error) {
	metadata, ok := ref.Manifest["metadata"].(map[string]any)
	if !ok || ref.Name == "" || metadata["name"] != ref.Name ||
		ref.Manifest["apiVersion"] != ref.APIVersion || ref.Manifest["kind"] != ref.Kind {
		return nil, apiErrorf("create manifest must match its named coordinate")
	}
	ns, hasNS := metadata["namespace"]
	if (ref.Namespace == nil && hasNS) || (ref.Namespace != nil && ns != *ref.Namespace) {
		return nil, apiErrorf("create manifest namespace must match its coordinate")
	}
	res, err := s.resolve(ctx, ref.APIVersion, ref.Kind)
	if err != nil {
		return nil, err
	}
	namespace, err := objectNamespace(res, ref.Kind, ref.Namespace)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(ref.Manifest)
	if err != nil {
		return nil, apiErrorf("create manifest cannot be encoded")
	}
	client, err := s.restClientFor(res.gvr)
	if err != nil {
		return nil, err
	}
	request := client.Post().Resource(res.gvr.Resource).
		Param("fieldManager", fieldManager).Body(body).MaxRetries(0)
	if res.namespaced {
		request = request.Namespace(namespace)
	}
	if err := request.Do(ctx).Error(); err != nil {
		var status apierrors.APIStatus
		if errors.As(err, &status) {
			code := status.Status().Code
			if code == http.StatusForbidden {
				return nil, apiErrorf("403 Forbidden: this server has no (create, %s) grant; retrying will not change the answer", res.gvr.Resource)
			}
			if code == http.StatusConflict {
				return nil, apiErrorf("409 Conflict: create was refused; no overwrite or retry was attempted")
			}
			if code >= http.StatusInternalServerError {
				return nil, apiErrorf("create failed: HTTP %d %s; outcome may be unknown, inspect the target before requesting a new approval", code, http.StatusText(int(code)))
			}
			return nil, apiErrorf("create failed: HTTP %d %s; no retry was attempted", code, http.StatusText(int(code)))
		}
		return nil, apiErrorf("create failed without a confirmed API result; outcome may be unknown, inspect the target before requesting a new approval")
	}
	return &ResourceResult{Resource: res.gvr.Resource, Namespace: namespace}, nil
}
