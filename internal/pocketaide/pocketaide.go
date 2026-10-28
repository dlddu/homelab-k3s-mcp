// Package pocketaide sends plain push notifications to the phone through PocketAide (dlddu/pocket-aide).
package pocketaide

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dlddu/homelab-k3s-mcp/internal/approvalgate"
)

// MaxTitleRunes mirrors PocketAide's own limit so an over-long title is refused here, before a request is spent.
const MaxTitleRunes = 120

// MaxRequesterRunes mirrors PocketAide's limit on requester.
const MaxRequesterRunes = 200

const requestTimeout = 20 * time.Second

type errKind int

const (
	kindUnavailable errKind = iota
	kindInvalid
	kindAPI
)

// Error is the error type returned by Service.
type Error struct {
	kind errKind
	msg  string
}

func (e *Error) Error() string {
	switch e.kind {
	case kindUnavailable:
		return "pocket-aide unavailable: " + e.msg
	case kindInvalid:
		return "invalid notification: " + e.msg
	default:
		return "pocket-aide error: " + e.msg
	}
}

// Unavailable reports whether this is the "not configured" kind — the one
// the dispatcher records as a refusal, not an error.
func (e *Error) Unavailable() bool { return e.kind == kindUnavailable }

func unavailable(msg string) *Error { return &Error{kind: kindUnavailable, msg: msg} }
func invalid(msg string) *Error     { return &Error{kind: kindInvalid, msg: msg} }
func apiError(msg string) *Error    { return &Error{kind: kindAPI, msg: msg} }

// Result is PocketAide's answer: the owner's registered devices and how many APNs accepted.
type Result struct {
	Devices int `json:"devices"`
	Sent    int `json:"sent"`
}

// Service sends a notification to the user who registered this server as a PocketAide caller.
// requester is who asked for the notification, as the tool's caller wrote it; PocketAide records it beside that caller.
type Service interface {
	Notify(ctx context.Context, title, body, requester string) (*Result, error)
}

// Unavailable is a Service that fails every call with the same reason.
type Unavailable struct {
	reason string
}

// NewUnavailable builds an Unavailable service with the given reason.
func NewUnavailable(reason string) *Unavailable {
	if reason == "" {
		reason = "PocketAide is not configured (APPROVAL_GATE_URL)"
	}
	return &Unavailable{reason: reason}
}

// Notify always fails with the configured reason.
func (u *Unavailable) Notify(context.Context, string, string, string) (*Result, error) {
	return nil, unavailable(u.reason)
}

// Client is the live PocketAide implementation of Service.
type Client struct {
	baseURL   string
	tokenFile string
	http      *http.Client
}

// New builds a Client. tokenFile is read on every call because the kubelet rotates the projected token in place.
func New(baseURL, tokenFile string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: requestTimeout}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), tokenFile: tokenFile, http: httpClient}
}

// FromEnv reuses the approval gate's PocketAide address and service account
// token: the server is already registered there as a caller, and the
// notification endpoint accepts the same credential. It returns (nil, nil)
// when PocketAide is not configured.
func FromEnv() (*Client, error) {
	base := strings.TrimSpace(os.Getenv("APPROVAL_GATE_URL"))
	if base == "" {
		return nil, nil
	}
	tokenFile := strings.TrimSpace(os.Getenv("APPROVAL_GATE_TOKEN_FILE"))
	if tokenFile == "" {
		tokenFile = approvalgate.DefaultTokenFile
	}
	if _, err := readToken(tokenFile); err != nil {
		return nil, err
	}
	return New(base, tokenFile, nil), nil
}

func readToken(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("pocket-aide service account token: %w", err)
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", fmt.Errorf("pocket-aide service account token %s is empty", path)
	}
	return token, nil
}

type notifyRequest struct {
	Title     string `json:"title"`
	Body      string `json:"body,omitempty"`
	Requester string `json:"requester"`
}

// ValidateTitle applies PocketAide's title rule; the tool calls it before
// Notify so a bad title is refused the same way whether or not PocketAide is
// configured.
func ValidateTitle(title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return invalid("title is required")
	}
	if utf8.RuneCountInString(title) > MaxTitleRunes {
		return invalid(fmt.Sprintf("title must be %d characters or fewer", MaxTitleRunes))
	}
	return nil
}

// ValidateRequester requires a requester of 1 to MaxRequesterRunes characters after trimming.
func ValidateRequester(requester string) error {
	requester = strings.TrimSpace(requester)
	if requester == "" {
		return invalid("requester is required")
	}
	if utf8.RuneCountInString(requester) > MaxRequesterRunes {
		return invalid(fmt.Sprintf("requester must be %d characters or fewer", MaxRequesterRunes))
	}
	return nil
}

// Notify posts one notification and reports how many devices received it.
func (c *Client) Notify(ctx context.Context, title, body, requester string) (*Result, error) {
	if err := ValidateTitle(title); err != nil {
		return nil, err
	}
	if err := ValidateRequester(requester); err != nil {
		return nil, err
	}
	title = strings.TrimSpace(title)
	raw, err := json.Marshal(notifyRequest{Title: title, Body: strings.TrimSpace(body), Requester: strings.TrimSpace(requester)})
	if err != nil {
		return nil, apiError(err.Error())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/external/notifications", bytes.NewReader(raw))
	if err != nil {
		return nil, apiError(err.Error())
	}
	req.Header.Set("content-type", "application/json")
	token, err := readToken(c.tokenFile)
	if err != nil {
		return nil, apiError(err.Error())
	}
	req.Header.Set("authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, apiError("request failed: " + err.Error())
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, apiError("PocketAide did not accept this server's service account (401) — is it registered as a caller in PocketAide?")
	case resp.StatusCode == http.StatusServiceUnavailable:
		return nil, apiError("PocketAide cannot send push notifications right now (503) — APNs is not configured on that server")
	case resp.StatusCode == http.StatusBadRequest:
		return nil, apiError("PocketAide rejected the notification (400): " + serverMessage(resp.Body))
	default:
		return nil, apiError(fmt.Sprintf("PocketAide returned %d", resp.StatusCode))
	}

	var out Result
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, apiError("unreadable response: " + err.Error())
	}
	return &out, nil
}

func serverMessage(r io.Reader) string {
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(r, 4096)).Decode(&body); err != nil || body.Error == "" {
		return "no reason given"
	}
	return body.Error
}
