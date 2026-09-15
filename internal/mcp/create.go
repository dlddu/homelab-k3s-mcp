package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/dlddu/homelab-k3s-mcp/internal/gatekeeper"
	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

type createDocument struct {
	ref  k8s.CreateRef
	raw  json.RawMessage
	pair gatekeeper.Pair
}

func parseCreateDocuments(raw json.RawMessage) ([]createDocument, *rpcErr) {
	args, ok := decodeObject(raw)
	if !ok || len(args) != 1 {
		return nil, errf(-32602, "arguments must contain only manifest")
	}
	var objects []map[string]any
	switch manifest := args["manifest"].(type) {
	case map[string]any:
		objects = append(objects, manifest)
	case string:
		reader := utilyaml.NewYAMLReader(bufio.NewReader(strings.NewReader(manifest)))
		for {
			data, err := reader.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, errf(-32602, "manifest contains an invalid YAML document")
			}
			data, err = yaml.YAMLToJSONStrict(data)
			if err != nil {
				return nil, errf(-32602, "manifest contains invalid YAML/JSON or duplicate fields")
			}
			if strings.TrimSpace(string(data)) == "null" {
				continue
			}
			object, ok := decodeObject(data)
			if !ok {
				return nil, errf(-32602, "each manifest document must be an object")
			}
			objects = append(objects, object)
		}
	default:
		return nil, errf(-32602, "manifest must be an object or a YAML/JSON string")
	}
	if len(objects) == 0 {
		return nil, errf(-32602, "manifest must contain at least one object")
	}
	documents := make([]createDocument, 0, len(objects))
	for i, object := range objects {
		apiVersion, _ := object["apiVersion"].(string)
		kind, _ := object["kind"].(string)
		metadata, _ := object["metadata"].(map[string]any)
		name, _ := metadata["name"].(string)
		if strings.TrimSpace(apiVersion) == "" || strings.TrimSpace(kind) == "" || strings.TrimSpace(name) == "" {
			return nil, errf(-32602, "manifest document %d requires apiVersion, kind and metadata.name", i+1)
		}
		if _, err := schema.ParseGroupVersion(apiVersion); err != nil {
			return nil, errf(-32602, "manifest document %d has an invalid apiVersion", i+1)
		}
		ref := k8s.CreateRef{APIVersion: apiVersion, Kind: kind, Name: name, Manifest: object}
		if value, present := metadata["namespace"]; present {
			namespace, ok := value.(string)
			if !ok || strings.TrimSpace(namespace) == "" {
				return nil, errf(-32602, "manifest document %d metadata.namespace must be a non-empty string", i+1)
			}
			ref.Namespace = &namespace
		}
		coordinate, _ := json.Marshal(map[string]any{"apiVersion": apiVersion, "kind": kind})
		pairs, err := genericPairs("create")(coordinate)
		if err != nil {
			return nil, errf(-32602, "manifest document %d has an invalid coordinate", i+1)
		}
		single, err := json.Marshal(map[string]any{"manifest": object})
		if err != nil {
			return nil, errf(-32602, "manifest document %d cannot be encoded", i+1)
		}
		documents = append(documents, createDocument{ref: ref, raw: single, pair: pairs[0]})
	}
	return documents, nil
}

func createPairs(raw json.RawMessage) ([]gatekeeper.Pair, error) {
	docs, rerr := parseCreateDocuments(raw)
	if rerr != nil {
		return nil, errors.New(rerr.message)
	}
	pairs := make([]gatekeeper.Pair, len(docs))
	for i, doc := range docs {
		pairs[i] = doc.pair
	}
	return pairs, nil
}

func createCoordinate(ref k8s.CreateRef) map[string]any {
	return map[string]any{"apiVersion": ref.APIVersion, "kind": ref.Kind, "namespace": ref.Namespace, "name": ref.Name}
}

func (h *Handler) resourceCreate(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	docs, rerr := parseCreateDocuments(raw)
	if rerr != nil {
		return nil, rerr
	}
	if len(docs) != 1 {
		return nil, errf(-32603, "create execution requires exactly one approved document")
	}
	if _, err := h.k8s.CreateResource(ctx, docs[0].ref); err != nil {
		return toolError(err), nil
	}
	return successResult(createCoordinate(docs[0].ref)), nil
}
