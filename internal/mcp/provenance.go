package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	SessionIDHeader = "X-Session-Platform-Session-Id"

	maxSessionIDRunes = 128
	maxReasonRunes    = 2000

	reasonArgument = "reason"

	reasonDescription = "운영자의 승인 화면에 그대로 보이는, 이 호출을 하려는 이유. 한국어 한두 문장."
)

type sessionIDKey struct{}

type reasonKey struct{}

func withSessionID(ctx context.Context, header string) context.Context {
	id := strings.TrimSpace(header)
	if id == "" {
		return ctx
	}
	if !utf8.ValidString(id) || utf8.RuneCountInString(id) > maxSessionIDRunes {
		slog.Warn("ignoring "+SessionIDHeader+": not valid UTF-8 or longer than the approval backend accepts",
			"max_runes", maxSessionIDRunes)
		return ctx
	}
	return context.WithValue(ctx, sessionIDKey{}, id)
}

func sessionIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(sessionIDKey{}).(string)
	return id
}

func withReason(ctx context.Context, reason string) context.Context {
	if reason == "" {
		return ctx
	}
	return context.WithValue(ctx, reasonKey{}, reason)
}

func reasonFrom(ctx context.Context) string {
	reason, _ := ctx.Value(reasonKey{}).(string)
	return reason
}

func approvalLog(ctx context.Context, level slog.Level, msg string, args ...any) {
	if id := sessionIDFrom(ctx); id != "" {
		args = append(args, "session_id", id)
	}
	if reason := reasonFrom(ctx); reason != "" {
		args = append(args, "reason", reason)
	}
	slog.Log(ctx, level, msg, args...)
}

var sensitiveProbe = json.RawMessage(`{"apiVersion":"v1","kind":"Secret","namespace":"probe","name":"probe"}`)

// mayAskForApproval resolves a per-call declaration against a Secret coordinate
// (resolving never touches the cluster). A resolver that refuses the probe for
// lack of its own arguments counts as asking, so a new tool errs towards
// advertising reason rather than handing it to a handler that never declared it.
func (d toolDeclaration) mayAskForApproval() bool {
	if d.outsideGate != "" {
		return false
	}
	if d.alwaysGated != "" {
		return true
	}
	if d.resolve == nil && len(d.pairs) == 0 {
		return false
	}
	gated, err := d.gatedPairs([]string{defaultSensitiveKind}, sensitiveProbe)
	return err != nil || len(gated) > 0
}

// splitReason re-emits every other key in its original order with its value's
// bytes untouched instead of round-tripping through a map: the rest is shown
// verbatim on the approval screen. Anything that is not a well-formed object
// comes back as it came, for the layers below to refuse in their own words.
func splitReason(raw json.RawMessage) (json.RawMessage, string, *rpcErr) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return raw, "", nil
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	if _, err := dec.Token(); err != nil {
		return raw, "", nil
	}
	var kept bytes.Buffer
	kept.WriteByte('{')
	var value json.RawMessage
	found := false
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return raw, "", nil
		}
		key, _ := tok.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return raw, "", nil
		}
		if key == reasonArgument {
			value, found = v, true
			continue
		}
		if kept.Len() > 1 {
			kept.WriteByte(',')
		}
		kept.Write(encodeKey(key))
		kept.WriteByte(':')
		kept.Write(v)
	}
	if _, err := dec.Token(); err != nil {
		return raw, "", nil
	}
	if !found {
		return raw, "", nil
	}
	kept.WriteByte('}')

	if string(bytes.TrimSpace(value)) == "null" {
		return kept.Bytes(), "", nil
	}
	var reason string
	if err := json.Unmarshal(value, &reason); err != nil {
		return nil, "", errf(-32602, "reason must be a string — the sentence shown to the operator on the approval screen")
	}
	reason = strings.TrimSpace(reason)
	if n := utf8.RuneCountInString(reason); n > maxReasonRunes {
		return nil, "", errf(-32602, "reason must be at most %d characters, got %d", maxReasonRunes, n)
	}
	return kept.Bytes(), reason, nil
}

func encodeKey(s string) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return bytes.TrimRight(b.Bytes(), "\n")
}

func validateReasonArgument(registered map[string]toolEntry, toolsList string) error {
	var doc struct {
		Tools []struct {
			Name        string `json:"name"`
			InputSchema struct {
				Properties map[string]json.RawMessage `json:"properties"`
				Required   []string                   `json:"required"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal([]byte(toolsList), &doc); err != nil {
		return fmt.Errorf("tools/list payload is not valid JSON: %w", err)
	}

	var problems []string
	for _, tool := range doc.Tools {
		entry, ok := registered[tool.Name]
		if !ok {
			continue
		}
		raw, advertised := tool.InputSchema.Properties[reasonArgument]
		want := entry.decl.mayAskForApproval()
		switch {
		case want && !advertised:
			problems = append(problems, fmt.Sprintf("%s may ask for approval but does not advertise %q", tool.Name, reasonArgument))
		case !want && advertised:
			problems = append(problems, fmt.Sprintf("%s never asks for approval but advertises %q, which the dispatcher would pass to its handler", tool.Name, reasonArgument))
		case advertised:
			var prop struct {
				Type        string `json:"type"`
				MaxLength   int    `json:"maxLength"`
				Description string `json:"description"`
			}
			if err := json.Unmarshal(raw, &prop); err != nil || prop.Type != "string" ||
				prop.MaxLength != maxReasonRunes || prop.Description != reasonDescription {
				problems = append(problems, fmt.Sprintf("%s advertises %q with a schema other than the shared one", tool.Name, reasonArgument))
			}
			for _, r := range tool.InputSchema.Required {
				if r == reasonArgument {
					problems = append(problems, fmt.Sprintf("%s makes %q required; it is optional", tool.Name, reasonArgument))
				}
			}
		}
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf("tools/list disagrees with the declarations about %q: %s", reasonArgument, strings.Join(problems, "; "))
}
