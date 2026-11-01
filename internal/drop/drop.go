// Package drop 은 이 서버의 ServiceAccount 토큰을 파일 공유 서비스 drop(dlddu/drop)의 단기 API 토큰으로 교환한다.
package drop

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

// DefaultTokenFile 은 k8s/deployment.yaml 이 audience "drop" 토큰을 projected 로 두는 자리다.
const DefaultTokenFile = "/var/run/secrets/drop/token"

// 아래 한도는 drop 의 POST /api/v1/sa/token 과 같다 — 어긋난 값을 요청을 쓰기 전에 여기서 거부한다.
const (
	DefaultValidMinutes = 60
	MaxValidMinutes     = 24 * 60
	MaxDays             = 30

	// drop 은 라벨 앞에 「sa:<namespace>/<serviceaccount> 」 를 붙여 합친 길이를 80자로 자른다.
	// 운영 접두사가 35자(sa:homelab-k3s-mcp/homelab-k3s-mcp )라 80 을 그대로 받으면 41자부터 drop 이
	// 400 으로 거부한다. 40 은 그 접두사에 여유를 둔 값이다.
	MaxLabelRunes = 40
)

// drop 은 운영에서 Sablier 뒤에 있어 잠든 파드를 깨우는 첫 요청이 10~20초, 최장 1분가량 붙잡힌다.
// 45초는 internal/opensearch 가 같은 이유로 고른 값이다.
const requestTimeout = 45 * time.Second

type errKind int

const (
	kindUnavailable errKind = iota
	kindInvalid
	kindAPI
)

// Error 는 Service 가 돌려주는 에러 타입이다.
type Error struct {
	kind errKind
	msg  string
}

func (e *Error) Error() string {
	switch e.kind {
	case kindUnavailable:
		return "drop unavailable: " + e.msg
	case kindInvalid:
		return "invalid drop token request: " + e.msg
	default:
		return "drop error: " + e.msg
	}
}

// Unavailable 은 「설정되지 않음」 종류인지 알려 준다 — 디스패처가 에러가 아니라 거부로 기록하는 종류다.
func (e *Error) Unavailable() bool { return e.kind == kindUnavailable }

func unavailable(msg string) *Error { return &Error{kind: kindUnavailable, msg: msg} }
func invalid(msg string) *Error     { return &Error{kind: kindInvalid, msg: msg} }
func apiError(msg string) *Error    { return &Error{kind: kindAPI, msg: msg} }

// Request 는 도구가 drop 에 요청하는 값이다. ValidMinutes 0 은 DefaultValidMinutes, MaxDays 0 은 drop 의 상한을 따른다는 뜻이다.
type Request struct {
	ValidMinutes int
	MaxDays      int
	Label        string
}

// Token 은 drop 의 응답 — 토큰과 그것을 쓰는 데 필요한 사실들이다.
type Token struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
	MaxDays   int    `json:"max_days"`
	CreatedBy string `json:"created_by"`
	BaseURL   string `json:"base_url"`
}

// Service 는 drop 토큰을 발급한다.
type Service interface {
	IssueToken(ctx context.Context, req Request) (*Token, error)
}

// Unavailable 은 모든 호출을 같은 사유로 실패시키는 Service 다.
type Unavailable struct {
	reason string
}

// NewUnavailable 은 주어진 사유로 Unavailable 을 만든다.
func NewUnavailable(reason string) *Unavailable {
	if reason == "" {
		reason = "drop is not configured (DROP_URL)"
	}
	return &Unavailable{reason: reason}
}

// IssueToken 은 언제나 설정된 사유로 실패한다.
func (u *Unavailable) IssueToken(context.Context, Request) (*Token, error) {
	return nil, unavailable(u.reason)
}

type doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client 는 실제 drop 을 부르는 Service 구현이다.
type Client struct {
	baseURL   string
	tokenFile string
	http      doer
}

// New 는 Client 를 만든다. kubelet 이 projected 토큰을 제자리에서 갈아 끼우므로 tokenFile 은 호출마다 다시 읽는다.
func New(baseURL, tokenFile string, httpClient *http.Client) *Client {
	var d doer = &http.Client{}
	if httpClient != nil {
		d = httpClient
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), tokenFile: tokenFile, http: d}
}

// FromEnv 는 DROP_URL·DROP_TOKEN_FILE 로 Client 를 만든다. DROP_URL 이 없으면 (nil, nil) 이다.
func FromEnv() (*Client, error) {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("DROP_URL")), "/")
	if base == "" {
		return nil, nil
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("DROP_URL must be an absolute http(s) URL, got %q", base)
	}
	tokenFile := strings.TrimSpace(os.Getenv("DROP_TOKEN_FILE"))
	if tokenFile == "" {
		tokenFile = DefaultTokenFile
	}
	if _, err := readToken(tokenFile); err != nil {
		return nil, err
	}
	return New(base, tokenFile, nil), nil
}

func readToken(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("drop service account token: %w", err)
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", fmt.Errorf("drop service account token %s is empty", path)
	}
	return token, nil
}

// Validate 는 drop 의 인자 규칙을 적용한다. 도구가 IssueToken 보다 먼저 불러, drop 설정 여부와
// 무관하게 같은 입력에 같은 거부가 나오게 한다.
func Validate(req Request) error {
	if req.ValidMinutes < 0 || req.ValidMinutes > MaxValidMinutes {
		return invalid(fmt.Sprintf("valid_minutes must be between 1 and %d", MaxValidMinutes))
	}
	if req.MaxDays < 0 || req.MaxDays > MaxDays {
		return invalid(fmt.Sprintf("max_days must be between 1 and %d", MaxDays))
	}
	if utf8.RuneCountInString(strings.TrimSpace(req.Label)) > MaxLabelRunes {
		return invalid(fmt.Sprintf("label must be %d characters or fewer", MaxLabelRunes))
	}
	return nil
}

type issueRequest struct {
	ValidMinutes int    `json:"valid_minutes"`
	MaxDays      int    `json:"max_days,omitempty"`
	Label        string `json:"label,omitempty"`
}

// IssueToken 은 이 서버의 ServiceAccount 토큰을 drop API 토큰으로 교환한다.
func (c *Client) IssueToken(ctx context.Context, req Request) (*Token, error) {
	if err := Validate(req); err != nil {
		return nil, err
	}
	if req.ValidMinutes == 0 {
		req.ValidMinutes = DefaultValidMinutes
	}
	raw, err := json.Marshal(issueRequest{ValidMinutes: req.ValidMinutes, MaxDays: req.MaxDays, Label: strings.TrimSpace(req.Label)})
	if err != nil {
		return nil, apiError(err.Error())
	}
	sa, err := readToken(c.tokenFile)
	if err != nil {
		return nil, apiError(err.Error())
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	// bytes.NewReader 라야 http.Request 가 GetBody 를 갖는다 — 잠든 drop 앞의 Sablier 가 같은 URL 로
	// 307 을 주면 클라이언트가 본문째 다시 보내고, 그렇지 않으면 본문 없이 재요청돼 기본값으로 발급된다.
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/sa/token", bytes.NewReader(raw))
	if err != nil {
		return nil, apiError(err.Error())
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+sa)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, apiError("request failed: " + err.Error())
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return nil, apiError("read response: " + err.Error())
	}

	switch resp.StatusCode {
	case http.StatusCreated:
	case http.StatusBadRequest:
		return nil, apiError("drop rejected the request (400): " + serverMessage(body))
	case http.StatusUnauthorized:
		return nil, apiError("drop did not accept this server's service account token (401) — it must be projected with audience \"drop\": " + serverMessage(body))
	case http.StatusForbidden:
		return nil, apiError("drop does not allow this server's service account (403) — add it to drop's SA_ALLOW: " + serverMessage(body))
	case http.StatusBadGateway:
		return nil, apiError("drop could not verify the service account (502) — is drop's ServiceAccount bound to system:auth-delegator?: " + serverMessage(body))
	case http.StatusServiceUnavailable:
		return nil, apiError("drop has no service account exchange configured (503) — SA_ALLOW is empty or drop is not running in a pod: " + serverMessage(body))
	default:
		return nil, apiError(fmt.Sprintf("drop returned %d: %s", resp.StatusCode, snippet(body)))
	}

	var out Token
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, apiError("unreadable response: " + err.Error())
	}
	if out.Token == "" {
		return nil, apiError("drop answered 201 without a token: " + snippet(body))
	}
	if out.BaseURL == "" {
		out.BaseURL = c.baseURL
	}
	out.BaseURL = strings.TrimRight(out.BaseURL, "/")
	return &out, nil
}

func serverMessage(body []byte) string {
	var parsed struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.Error == "" {
		return snippet(body)
	}
	return parsed.Error
}

func snippet(data []byte) string {
	s := strings.TrimSpace(string(data))
	if s == "" {
		return "no reason given"
	}
	const limit = 300
	if len(s) > limit {
		return s[:limit] + "..."
	}
	return s
}
