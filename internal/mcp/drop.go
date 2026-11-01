package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/dlddu/homelab-k3s-mcp/internal/drop"
)

var dropTokenIssueArguments = map[string]bool{"valid_minutes": true, "max_days": true, "label": true}

// 업로드 예시의 보관 일수. drop 의 기본값(7일)과 같게 두되, 토큰의 max_days 가 더 작으면 그 값으로
// 낮춘다 — 그대로 두면 max_days 를 줄여 받은 토큰으로 예시를 그대로 실행한 업로드가 400 으로 끝난다.
const dropExampleDays = 7

func (h *Handler) dropTokenIssue(ctx context.Context, raw json.RawMessage) (any, *rpcErr) {
	obj, isObject := decodeObject(raw)
	if !isObject && !isNullArg(raw) {
		return nil, errf(-32602, "arguments must be an object")
	}
	var unknown []string
	for key := range obj {
		if !dropTokenIssueArguments[key] {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, errf(-32602, "unknown argument(s): %s — drop_token_issue takes only valid_minutes, max_days and label; it never takes file contents", strings.Join(unknown, ", "))
	}

	var req drop.Request
	if v, present := obj["valid_minutes"]; present && v != nil {
		n, ok := intValue(v)
		if !ok {
			return nil, errf(-32602, "valid_minutes must be an integer")
		}
		if n < 1 || n > drop.MaxValidMinutes {
			return nil, errf(-32602, "valid_minutes must be between 1 and %d", drop.MaxValidMinutes)
		}
		req.ValidMinutes = int(n)
	}
	if v, present := obj["max_days"]; present && v != nil {
		n, ok := intValue(v)
		if !ok {
			return nil, errf(-32602, "max_days must be an integer")
		}
		if n < 1 || n > drop.MaxDays {
			return nil, errf(-32602, "max_days must be between 1 and %d", drop.MaxDays)
		}
		req.MaxDays = int(n)
	}
	if v, present := obj["label"]; present && v != nil {
		s, ok := v.(string)
		if !ok {
			return nil, errf(-32602, "label must be a string")
		}
		req.Label = s
	}
	if err := drop.Validate(req); err != nil {
		return nil, errf(-32602, "%s", err.Error())
	}

	tok, err := h.drop.IssueToken(ctx, req)
	if err != nil {
		return toolError(ctx, err), nil
	}
	return successResult(map[string]any{
		"token":      tok.Token,
		"expires_at": tok.ExpiresAt,
		"max_days":   tok.MaxDays,
		"base_url":   tok.BaseURL,
		"created_by": tok.CreatedBy,
		"usage":      dropUsage(tok),
	}), nil
}

// dropUsage 는 토큰을 쓰는 curl 명령 견본이다. 토큰 원문은 setup 한 줄에만 싣고 나머지는
// $DROP_TOKEN 으로 가리킨다 — 명령마다 원문을 박으면 같은 비밀이 대화 맥락에 여러 번 남고,
// 셸 이력에도 명령 수만큼 찍힌다.
func dropUsage(tok *drop.Token) map[string]any {
	base := tok.BaseURL
	days := dropExampleDays
	if tok.MaxDays > 0 && tok.MaxDays < days {
		days = tok.MaxDays
	}
	auth := `-H "Authorization: Bearer $DROP_TOKEN"`
	return map[string]any{
		"setup":         fmt.Sprintf("export DROP_TOKEN='%s'", tok.Token),
		"upload":        fmt.Sprintf("curl -fsS %s -F days=%d -F file=@./path/to/file %s/api/v1/drops", auth, days, base),
		"upload_note":   "Repeat -F file=@... for more files; optional -F id=<id> -F password=<6+ chars>. The JSON answer (id, password, url, files) is metadata only. One request must finish within 60 s; for large files use the chunked API: POST " + base + "/api/v1/uploads {files:[{name,size}],days} -> {id,upload_key,chunk_bytes}, PUT " + base + "/api/v1/uploads/<id>/<upload_key>/files/<n>?offset=<bytes> (409 returns {received} to resume), POST " + base + "/api/v1/uploads/<id>/<upload_key>/complete.",
		"list":          fmt.Sprintf("curl -fsS %s %s/api/v1/drops", auth, base),
		"delete":        fmt.Sprintf("curl -fsS %s -X DELETE %s/api/v1/drops/<id>", auth, base),
		"download_list": fmt.Sprintf("curl -fsS -u <id>:<password> -H 'Accept: application/json' %s/get/<id>", base),
		"download_file": fmt.Sprintf("curl -fsS -u <id>:<password> -o ./<name> %s/get/<id>/f/<n>", base),
		"download_zip":  fmt.Sprintf("curl -fsS -u <id>:<password> -o drop-<id>.zip %s/get/<id>/all.zip", base),
		"note":          "Downloads need only id and password, not the token. Always save downloads with -o so file contents never print into the conversation. When expires_at passes, call drop_token_issue again; drops are kept at most max_days (30 at most).",
	}
}
