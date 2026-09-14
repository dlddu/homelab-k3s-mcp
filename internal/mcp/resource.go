package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dlddu/homelab-k3s-mcp/internal/k8s"
)

const (
	logsDefaultTailLines int64 = 200
	logsMaxTailLines     int64 = 5000
)

// getSubresources are the read-only subresources resource_get serves (AC5).
var getSubresources = map[string]bool{"log": true, "scale": true, "status": true}

// coordinate is the apiVersion+kind pair every generic tool starts from.
type coordinate struct {
	apiVersion string
	kind       string
	namespace  *string
}

func parseCoordinate(obj map[string]any) (coordinate, *rpcErr) {
	apiVersion := optionalString(obj, "apiVersion")
	if apiVersion == nil {
		return coordinate{}, errf(-32602, "apiVersion is required (e.g. \"v1\", \"apps/v1\")")
	}
	kind := optionalString(obj, "kind")
	if kind == nil {
		return coordinate{}, errf(-32602, "kind is required (call api_resources for the kinds this cluster serves)")
	}
	return coordinate{apiVersion: *apiVersion, kind: *kind, namespace: optionalString(obj, "namespace")}, nil
}

func (h *Handler) apiResources(ctx context.Context) (any, *rpcErr) {
	items, err := h.k8s.APIResources(ctx)
	if err != nil {
		return toolError(err), nil
	}
	return successResult(map[string]any{"items": items}), nil
}

func (h *Handler) resourceList(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	obj, ok := decodeObject(raw)
	if !ok {
		return nil, errf(-32602, "arguments must be an object")
	}
	coord, rerr := parseCoordinate(obj)
	if rerr != nil {
		return nil, rerr
	}

	limit := int64(0)
	if v, present := obj["limit"]; present {
		n, ok := intValue(v)
		if !ok {
			return nil, errf(-32602, "limit must be an integer")
		}
		if n < 1 {
			return nil, errf(-32602, "limit must be >= 1")
		}
		if n > k8s.ListMaxLimit {
			return nil, errf(-32602, "limit must be <= %d", k8s.ListMaxLimit)
		}
		limit = n
	}

	query := k8s.ListQuery{
		APIVersion:    coord.apiVersion,
		Kind:          coord.kind,
		Namespace:     coord.namespace,
		LabelSelector: optionalString(obj, "labelSelector"),
		FieldSelector: optionalString(obj, "fieldSelector"),
		Limit:         limit,
	}
	if c := optionalString(obj, "continue"); c != nil {
		query.Continue = *c
	}

	result, err := h.k8s.ListResources(ctx, query)
	if err != nil {
		return toolError(err), nil
	}

	effectiveLimit := limit
	if effectiveLimit == 0 {
		effectiveLimit = k8s.ListDefaultLimit
	}
	payload := map[string]any{
		"apiVersion": coord.apiVersion,
		"kind":       coord.kind,
		"resource":   result.Resource,
		"namespace":  coord.namespace,
		"limit":      effectiveLimit,
		"columns":    result.Columns,
		"rows":       result.Rows,
		"truncated":  result.Truncated,
		"continue":   result.Continue,
		"remaining":  result.Remaining,
	}
	return map[string]any{
		"content":           []any{map[string]any{"type": "text", "text": renderTable(result, effectiveLimit)}},
		"structuredContent": payload,
		"isError":           false,
	}, nil
}

func (h *Handler) resourceGet(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	obj, ok := decodeObject(raw)
	if !ok {
		return nil, errf(-32602, "arguments must be an object")
	}
	coord, rerr := parseCoordinate(obj)
	if rerr != nil {
		return nil, rerr
	}
	name := optionalString(obj, "name")
	if name == nil {
		return nil, errf(-32602, "name is required; call resource_list first to find it — resolving a selector here would exercise the list verb, and this tool holds exactly one")
	}

	subresource := ""
	if s := optionalString(obj, "subresource"); s != nil {
		if !getSubresources[*s] {
			return nil, errf(-32602, "subresource must be one of log, scale, status")
		}
		subresource = *s
	}

	ref := k8s.ResourceRef{
		APIVersion:  coord.apiVersion,
		Kind:        coord.kind,
		Namespace:   coord.namespace,
		Name:        *name,
		Subresource: subresource,
	}

	logArgs := []string{"container", "tailLines", "previous", "timestamps", "sinceSeconds"}
	if subresource != "log" {
		for _, key := range logArgs {
			if _, present := obj[key]; present {
				return nil, errf(-32602, "%s applies to subresource=log only", key)
			}
		}
	} else {
		logOpts, rerr := parseLogOptions(obj)
		if rerr != nil {
			return nil, rerr
		}
		ref.Log = logOpts
	}

	result, err := h.k8s.GetResource(ctx, ref)
	if err != nil {
		return toolError(err), nil
	}

	payload := map[string]any{
		"apiVersion":  coord.apiVersion,
		"kind":        coord.kind,
		"namespace":   coord.namespace,
		"name":        *name,
		"resource":    result.Resource,
		"subresource": subresource,
	}
	text := ""
	if result.Object != nil {
		payload["object"] = result.Object
		text = prettyJSON(result.Object)
	} else {
		payload["text"] = result.Text
		text = result.Text
		if text == "" {
			text = "(no output)"
		}
	}
	return map[string]any{
		"content":           []any{map[string]any{"type": "text", "text": text}},
		"structuredContent": payload,
		"isError":           false,
	}, nil
}

func (h *Handler) resourcePatch(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	obj, ok := decodeObject(raw)
	if !ok {
		return nil, errf(-32602, "arguments must be an object")
	}
	coord, rerr := parseCoordinate(obj)
	if rerr != nil {
		return nil, rerr
	}
	name := optionalString(obj, "name")
	if name == nil {
		return nil, errf(-32602, "name is required; this tool patches one object, not a selection")
	}
	patchType := optionalString(obj, "patchType")
	if patchType == nil {
		return nil, errf(-32602, "patchType is required (one of %s)", strings.Join(k8s.PatchTypeNames(), ", "))
	}
	if !k8s.IsPatchType(*patchType) {
		return nil, errf(-32602, "patchType must be one of %s", strings.Join(k8s.PatchTypeNames(), ", "))
	}

	// The body is taken from the raw arguments rather than re-encoded from the
	// decoded map: what the apiserver receives is then the same bytes the
	// operator approved, down to key order.
	body := rawPatchBody(raw)
	if len(body) == 0 {
		return nil, errf(-32602, "patch is required (an object, or an array for patchType=json)")
	}

	// fieldManager is server-side apply's ownership key, not a label, so apply
	// cannot default it — two callers sharing a name share the fields they own.
	// The other three types have no use for it, and accepting it there would
	// advertise an argument that does nothing.
	fieldManager := optionalString(obj, "fieldManager")
	if *patchType == "apply" && fieldManager == nil {
		return nil, errf(-32602, "fieldManager is required for patchType=apply; it is the ownership key server-side apply records")
	}
	if *patchType != "apply" && fieldManager != nil {
		return nil, errf(-32602, "fieldManager applies to patchType=apply only")
	}

	ref := k8s.PatchRef{
		APIVersion: coord.apiVersion,
		Kind:       coord.kind,
		Namespace:  coord.namespace,
		Name:       *name,
		PatchType:  *patchType,
		Patch:      body,
	}
	if fieldManager != nil {
		ref.FieldManager = *fieldManager
	}

	result, err := h.k8s.PatchResource(ctx, ref)
	if err != nil {
		return toolError(err), nil
	}

	payload := map[string]any{
		"apiVersion": coord.apiVersion,
		"kind":       coord.kind,
		"namespace":  coord.namespace,
		"name":       *name,
		"resource":   result.Resource,
		"patchType":  *patchType,
		"object":     result.Object,
	}
	return map[string]any{
		"content":           []any{map[string]any{"type": "text", "text": prettyJSON(result.Object)}},
		"structuredContent": payload,
		"isError":           false,
	}, nil
}

// rawPatchBody lifts the patch out of the raw arguments untouched. A JSON patch
// is an array and the other three are objects, so the field is taken as raw
// JSON and handed on as-is.
func rawPatchBody(raw json.RawMessage) []byte {
	var args struct {
		Patch json.RawMessage `json:"patch"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil
	}
	body := bytes.TrimSpace(args.Patch)
	if len(body) == 0 || string(body) == "null" {
		return nil
	}
	return body
}

// parseLogOptions carries workload_logs' bounds over unchanged (AC5).
func parseLogOptions(obj map[string]any) (k8s.LogOptions, *rpcErr) {
	previous, _, rerr := boolArg(obj, "previous")
	if rerr != nil {
		return k8s.LogOptions{}, rerr
	}
	timestamps, _, rerr := boolArg(obj, "timestamps")
	if rerr != nil {
		return k8s.LogOptions{}, rerr
	}

	tailLines := logsDefaultTailLines
	if v, present := obj["tailLines"]; present {
		n, ok := intValue(v)
		if !ok {
			return k8s.LogOptions{}, errf(-32602, "tailLines must be an integer")
		}
		if n < 1 {
			return k8s.LogOptions{}, errf(-32602, "tailLines must be >= 1")
		}
		if n > logsMaxTailLines {
			return k8s.LogOptions{}, errf(-32602, "tailLines must be <= %d", logsMaxTailLines)
		}
		tailLines = n
	}

	var sinceSeconds *int64
	if v, present := obj["sinceSeconds"]; present {
		n, ok := intValue(v)
		if !ok {
			return k8s.LogOptions{}, errf(-32602, "sinceSeconds must be an integer")
		}
		if n < 1 {
			return k8s.LogOptions{}, errf(-32602, "sinceSeconds must be >= 1")
		}
		sinceSeconds = &n
	}

	tail := tailLines
	return k8s.LogOptions{
		Container:    optionalString(obj, "container"),
		TailLines:    &tail,
		Previous:     previous,
		Timestamps:   timestamps,
		SinceSeconds: sinceSeconds,
	}, nil
}

// renderTable prints the apiserver's columns the way kubectl does (AC3).
func renderTable(result *k8s.ListResult, limit int64) string {
	if len(result.Rows) == 0 {
		return "(no resources found)"
	}

	widths := make([]int, len(result.Columns))
	for i, c := range result.Columns {
		widths[i] = len(c.Name)
	}
	cells := make([][]string, 0, len(result.Rows))
	for _, row := range result.Rows {
		line := make([]string, len(result.Columns))
		for i := range result.Columns {
			if i < len(row) {
				line[i] = fmt.Sprintf("%v", row[i])
			}
			if len(line[i]) > widths[i] {
				widths[i] = len(line[i])
			}
		}
		cells = append(cells, line)
	}

	var b strings.Builder
	writeRow := func(values []string) {
		for i, v := range values {
			if i > 0 {
				b.WriteString("   ")
			}
			b.WriteString(v)
			if i < len(values)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-len(v)))
			}
		}
		b.WriteString("\n")
	}

	header := make([]string, len(result.Columns))
	for i, c := range result.Columns {
		header[i] = strings.ToUpper(c.Name)
	}
	writeRow(header)
	for _, line := range cells {
		writeRow(line)
	}

	if result.Truncated {
		fmt.Fprintf(&b, "\n(truncated at limit=%d — pass continue=%q for the next page", limit, result.Continue)
		if result.Remaining != nil {
			fmt.Fprintf(&b, "; %d more", *result.Remaining)
		}
		b.WriteString(")\n")
	}
	return b.String()
}
