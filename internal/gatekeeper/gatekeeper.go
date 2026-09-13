// Package gatekeeper talks to the gatekeeper (dlddu/gatekeeper) approval
// backend so that a tool call can be held until a human decides on it.
//
// The package implements the decision half of docs/prd-approval-gate.md: it
// creates an approval request (AC2), polls for the verdict (AC4) and folds
// every outcome that is not an observed APPROVED into a refusal (AC5). Which
// calls have to come through here is not decided in this package — that is the
// declaration table in internal/mcp, which selects by RBAC verb (AC1).
package gatekeeper

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultTimeout      = 300 * time.Second
	defaultPollInterval = 2 * time.Second
	defaultGatedKind    = "v1/Secret"
)

// Verdicts as reported by the gatekeeper backend.
const (
	StatusPending  = "PENDING"
	StatusApproved = "APPROVED"
	StatusRejected = "REJECTED"
	StatusExpired  = "EXPIRED"
)

// ErrNotConfigured is the refusal used when the gate has no backend to ask.
// AC5 lists an unset GATEKEEPER_BASE_URL/GATEKEEPER_API_KEY among the paths
// that must refuse, so this is a normal runtime outcome rather than a startup
// failure: the server keeps serving and only gated calls stop.
var ErrNotConfigured = errors.New("approval gate is not configured (GATEKEEPER_BASE_URL/GATEKEEPER_API_KEY): refusing")

// ErrConsumed reports a second attempt to spend one approval. AC7 gives an
// approval a budget of exactly one kubernetes API call.
var ErrConsumed = errors.New("approval has already been spent; request a new one")

// Pair is a kubernetes RBAC (verb, resource[/subresource]) pair. Resource
// carries the subresource inline the way RBAC writes it ("pods/exec"), because
// that is the unit the apiserver grants and revokes.
type Pair struct {
	Verb     string
	Resource string
}

func (p Pair) String() string { return p.Verb + " on " + p.Resource }

// Config is the gate's environment-provided configuration.
type Config struct {
	BaseURL      string
	APIKey       string
	UserID       string
	Timeout      time.Duration
	PollInterval time.Duration
	// GatedKinds are the sensitive kinds whose reads are gated too, in
	// "group/version/Kind" form. Left configurable because what counts as a
	// credential differs per cluster.
	GatedKinds []string
}

// FromEnv reads the gate configuration. It returns (nil, nil) when the gate is
// not configured at all, which the caller turns into a refusing gate rather
// than a fatal error.
func FromEnv() (*Config, error) {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("GATEKEEPER_BASE_URL")), "/")
	key := strings.TrimSpace(os.Getenv("GATEKEEPER_API_KEY"))
	if base == "" && key == "" {
		return nil, nil
	}
	if base == "" {
		return nil, errors.New("GATEKEEPER_API_KEY is set but GATEKEEPER_BASE_URL is not")
	}
	if key == "" {
		return nil, errors.New("GATEKEEPER_BASE_URL is set but GATEKEEPER_API_KEY is not")
	}

	timeout, err := durationFromEnv("GATEKEEPER_TIMEOUT_SECONDS", defaultTimeout)
	if err != nil {
		return nil, err
	}
	poll, err := durationFromEnv("GATEKEEPER_POLL_INTERVAL_SECONDS", defaultPollInterval)
	if err != nil {
		return nil, err
	}

	return &Config{
		BaseURL:      base,
		APIKey:       key,
		UserID:       strings.TrimSpace(os.Getenv("GATEKEEPER_USER_ID")),
		Timeout:      timeout,
		PollInterval: poll,
		GatedKinds:   gatedKindsFromEnv(),
	}, nil
}

func durationFromEnv(key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer, got %q", key, raw)
	}
	return time.Duration(n) * time.Second, nil
}

func gatedKindsFromEnv() []string {
	raw := strings.TrimSpace(os.Getenv("RESOURCE_GATED_KINDS"))
	if raw == "" {
		return []string{defaultGatedKind}
	}
	parts := strings.Split(raw, ",")
	kinds := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			kinds = append(kinds, s)
		}
	}
	if len(kinds) == 0 {
		return []string{defaultGatedKind}
	}
	return kinds
}

// Call describes the single kubernetes operation an approval is being sought
// for. Context is the operator-facing text of AC3; an empty one is refused
// rather than sent, because an approval screen that does not say what it is
// approving turns the button into a formality.
type Call struct {
	Tool    string
	Pair    Pair
	Context string
}

// Decision is an observed APPROVED verdict, carrying the audit fields of AC8
// and the auto-response flags of AC9. It is spendable exactly once (AC7).
type Decision struct {
	RequestID     string
	ExternalID    string
	ProcessedByID string
	AutoApproved  bool

	mu   sync.Mutex
	used bool
}

// Consume spends the approval. The second call fails, so a retry after a failed
// execution has to go back through the gate (AC7).
func (d *Decision) Consume() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.used {
		return ErrConsumed
	}
	d.used = true
	return nil
}

// Gate is the approval boundary the MCP dispatcher calls before it lets a gated
// call reach kubernetes.
type Gate interface {
	// Authorize returns a spendable Decision only when a human (or the
	// operator's configured auto-response) approved this exact call. Every
	// other outcome is an error — there is no third answer.
	Authorize(ctx context.Context, call Call) (*Decision, error)
}

// Unavailable is a Gate that refuses everything with a fixed reason. It stands
// in wherever the gate cannot be built, so an unconfigured deployment refuses
// gated calls instead of quietly running them (AC5).
type Unavailable struct{ reason error }

// NewUnavailable builds a refusing gate. A zero reason falls back to
// ErrNotConfigured.
func NewUnavailable(reason error) *Unavailable {
	if reason == nil {
		reason = ErrNotConfigured
	}
	return &Unavailable{reason: reason}
}

func (u *Unavailable) Authorize(context.Context, Call) (*Decision, error) {
	return nil, u.reason
}

// Client is the live gate backed by the gatekeeper HTTP API.
type Client struct {
	cfg       Config
	http      *http.Client
	requester string
	newID     func() (string, error)
	afterFunc func(time.Duration) <-chan time.Time
}

// New builds a live gate from cfg. requester names this server on the approval
// screen.
func New(cfg Config, requester string) *Client {
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = defaultPollInterval
	}
	return &Client{
		cfg:       cfg,
		http:      &http.Client{Timeout: 30 * time.Second},
		requester: requester,
		newID:     randomExternalID,
		afterFunc: time.After,
	}
}

// GatedKinds exposes the configured sensitive kinds so the declaration table
// can decide whether a read is gated.
func (c *Client) GatedKinds() []string { return c.cfg.GatedKinds }

type createRequestBody struct {
	ExternalID     string `json:"externalId"`
	Context        string `json:"context"`
	RequesterName  string `json:"requesterName"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
	UserID         string `json:"userId,omitempty"`
}

type requestResponse struct {
	ID            string `json:"id"`
	ExternalID    string `json:"externalId"`
	Status        string `json:"status"`
	ProcessedByID string `json:"processedById"`
	AutoApproved  bool   `json:"autoApproved"`
	AutoRejected  bool   `json:"autoRejected"`
}

// Authorize implements Gate. Every return path other than an observed APPROVED
// is an error, and none of them reaches kubernetes (AC5).
func (c *Client) Authorize(ctx context.Context, call Call) (*Decision, error) {
	if strings.TrimSpace(call.Context) == "" {
		return nil, fmt.Errorf("refusing %s: no approvable context could be built", call.Tool)
	}

	externalID, err := c.newID()
	if err != nil {
		return nil, fmt.Errorf("refusing %s: %w", call.Tool, err)
	}

	created, err := c.create(ctx, call, externalID)
	if err != nil {
		return nil, err
	}

	// An auto-response is settled at creation time; polling a decided request
	// only confirms what the create response already said.
	switch {
	case created.AutoApproved && created.Status == StatusApproved:
		return &Decision{
			RequestID:     created.ID,
			ExternalID:    externalID,
			ProcessedByID: created.ProcessedByID,
			AutoApproved:  true,
		}, nil
	case created.AutoRejected:
		return nil, fmt.Errorf("refusing %s: approval auto-rejected (request %s)", call.Tool, created.ID)
	}

	return c.poll(ctx, call, created.ID, externalID)
}

func (c *Client) create(ctx context.Context, call Call, externalID string) (*requestResponse, error) {
	body := createRequestBody{
		ExternalID:     externalID,
		Context:        call.Context,
		RequesterName:  c.requester,
		TimeoutSeconds: int(c.cfg.Timeout / time.Second),
		UserID:         c.cfg.UserID,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("refusing %s: %w", call.Tool, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/api/requests", bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("refusing %s: %w", call.Tool, err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", c.cfg.APIKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refusing %s: approval request failed: %w", call.Tool, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusConflict {
		return nil, fmt.Errorf("refusing %s: approval request id collided (409)", call.Tool)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("refusing %s: approval backend returned %d", call.Tool, resp.StatusCode)
	}

	var decoded requestResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("refusing %s: unreadable approval response: %w", call.Tool, err)
	}
	if decoded.ID == "" {
		return nil, fmt.Errorf("refusing %s: approval response carried no request id", call.Tool)
	}
	return &decoded, nil
}

// poll waits for the verdict. gatekeeper evaluates expiry when the request is
// read rather than in the background, so a request that is never polled is
// never observed to expire — hence the loop rather than trusting the create
// response (AC4).
func (c *Client) poll(ctx context.Context, call Call, requestID, externalID string) (*Decision, error) {
	deadline := c.afterFunc(c.cfg.Timeout)

	for {
		state, err := c.get(ctx, requestID)
		if err != nil {
			return nil, fmt.Errorf("refusing %s: %w", call.Tool, err)
		}

		switch state.Status {
		case StatusApproved:
			return &Decision{
				RequestID:     requestID,
				ExternalID:    externalID,
				ProcessedByID: state.ProcessedByID,
				AutoApproved:  state.AutoApproved,
			}, nil
		case StatusRejected:
			return nil, fmt.Errorf("refusing %s: approval rejected (request %s)", call.Tool, requestID)
		case StatusExpired:
			return nil, fmt.Errorf("refusing %s: approval expired (request %s)", call.Tool, requestID)
		case StatusPending, "":
			// keep waiting
		default:
			return nil, fmt.Errorf("refusing %s: unknown approval status %q (request %s)", call.Tool, state.Status, requestID)
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("refusing %s: %w", call.Tool, ctx.Err())
		case <-deadline:
			return nil, fmt.Errorf("refusing %s: no approval within %s (request %s)", call.Tool, c.cfg.Timeout, requestID)
		case <-c.afterFunc(c.cfg.PollInterval):
		}
	}
}

func (c *Client) get(ctx context.Context, requestID string) (*requestResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.BaseURL+"/api/requests/"+requestID, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.cfg.APIKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("approval poll failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("approval backend returned %d while polling", resp.StatusCode)
	}
	var decoded requestResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("unreadable approval poll response: %w", err)
	}
	return &decoded, nil
}

// randomExternalID gives every call its own id, so two identical calls are two
// approvals rather than one reused one (AC2).
func randomExternalID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("could not generate an approval request id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
