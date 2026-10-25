package mcp

import (
	"context"
	"encoding/json"

	"github.com/dlddu/homelab-k3s-mcp/internal/toolscope"
)

type advertisedTool struct {
	name string
	raw  json.RawMessage
}

var advertisedTools = mustSplitToolsList()

func mustSplitToolsList() []advertisedTool {
	var doc struct {
		Tools []json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal([]byte(toolsListJSON), &doc); err != nil {
		panic("tools/list payload is not valid JSON: " + err.Error())
	}
	out := make([]advertisedTool, 0, len(doc.Tools))
	for _, raw := range doc.Tools {
		var t struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &t); err != nil {
			panic("tools/list entry is not valid JSON: " + err.Error())
		}
		out = append(out, advertisedTool{name: t.Name, raw: raw})
	}
	return out
}

func toolsList(ctx context.Context) any {
	if !toolscope.Restricted(ctx) {
		return json.RawMessage(toolsListJSON)
	}
	tools := []json.RawMessage{}
	for _, t := range advertisedTools {
		if toolscope.Allows(ctx, t.name) {
			tools = append(tools, t.raw)
		}
	}
	return map[string]any{"tools": tools}
}
