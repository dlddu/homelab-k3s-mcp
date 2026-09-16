package k8s

import (
	"bytes"
	"encoding/json"
	"strings"
)

func approvedPatch(ref PatchRef) ([]byte, error) {
	if ref.ApprovedResourceVersion == "" || ref.ApprovedUID == "" {
		return nil, APIError("refusing patch: approved target version and identity are required")
	}
	if ref.PatchType == "json" {
		var ops []json.RawMessage
		if json.Unmarshal(ref.Patch, &ops) != nil || ops == nil {
			return nil, APIError("refusing patch: JSON Patch must be an array")
		}
		tests := make([]json.RawMessage, 0, 2)
		for _, condition := range [][2]string{{"resourceVersion", ref.ApprovedResourceVersion}, {"uid", ref.ApprovedUID}} {
			op, _ := json.Marshal(map[string]string{"op": "test", "path": "/metadata/" + condition[0], "value": condition[1]})
			tests = append(tests, op)
		}
		guarded := make([]json.RawMessage, 0, len(ops)+4)
		guarded = append(guarded, tests...)
		guarded = append(guarded, ops...)
		guarded = append(guarded, tests...)
		return json.Marshal(guarded)
	}
	var patch map[string]json.RawMessage
	if json.Unmarshal(ref.Patch, &patch) != nil || patch == nil {
		return nil, APIError("refusing patch: patch must be an object")
	}
	metadata := map[string]json.RawMessage{}
	if raw, present := patch["metadata"]; present {
		if json.Unmarshal(raw, &metadata) != nil || metadata == nil {
			return nil, APIError("refusing patch: metadata must be an object")
		}
	}
	if ref.PatchType == "strategic" {
		for _, fields := range []map[string]json.RawMessage{patch, metadata} {
			for key := range fields {
				if strings.HasPrefix(key, "$") {
					return nil, APIError("refusing patch: strategic directives at the root or metadata can remove approved-target preconditions")
				}
			}
		}
	}
	for _, condition := range [][2]string{{"resourceVersion", ref.ApprovedResourceVersion}, {"uid", ref.ApprovedUID}} {
		encoded, _ := json.Marshal(condition[1])
		if raw, present := metadata[condition[0]]; present {
			var value string
			if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &value) != nil || value != condition[1] {
				return nil, apiErrorf("refusing patch: metadata.%s differs from the approved target", condition[0])
			}
		}
		if ref.PatchType == "apply" && condition[0] == "uid" {
			continue
		}
		metadata[condition[0]] = encoded
	}
	patch["metadata"], _ = json.Marshal(metadata)
	return json.Marshal(patch)
}
