// Package eventlog is the one place a tool call becomes a record
// (prd-event-log). The dispatcher emits exactly one Record per tools/call —
// success, refusal or error — and the auth layer contributes who made it.
//
// It is its own package rather than a corner of mcp because two layers write
// into it from opposite sides: auth knows the principal before the tool name
// exists, and mcp knows the tool and its outcome but never sees the
// credential. Neither may import the other, so the record's shape lives here
// and both depend on it.
package eventlog

import (
	"context"
	"log/slog"
	"time"
)

// Principal is who made the call, spelled the way AC1 asks: a JWT is its
// subject, an API key is its position in MCP_API_KEYS. The credential itself
// is never here — AC3 forbids it in any record, and the cheapest way to keep
// a value out of every record is to never let it into the type.
type Principal struct {
	// Method is how the caller authenticated: "jwt", "api_key", or "none"
	// when the deployment serves /mcp without authentication.
	Method string
	// ID is the subject (jwt) or the 1-based key index as text (api_key).
	ID string
}

// Anonymous is the principal of a deployment that authenticates nobody
// (MCP_AUTH_DISABLED). It is a value rather than a missing field so the
// principal column is never empty: an empty column cannot be told from a
// layer that forgot to fill it in.
var Anonymous = Principal{Method: "none"}

// Unauthenticated is the principal of a call auth refused (AC4). It is not
// Anonymous: that one passed an auth layer that asks nothing, this one failed
// an auth layer that asked. What it presented is not here either (AC3).
var Unauthenticated = Principal{Method: "unauthenticated"}

// String renders the principal for the record: "jwt:<sub>", "api_key:<n>",
// "anonymous" or "unauthenticated".
func (p Principal) String() string {
	switch p.Method {
	case "", "none":
		return "anonymous"
	case "unauthenticated":
		return "unauthenticated"
	default:
		return p.Method + ":" + p.ID
	}
}

type principalKey struct{}

// WithPrincipal stores the authenticated principal for the rest of the
// request.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom reads the principal auth stored, or Anonymous when no auth
// layer ran.
func PrincipalFrom(ctx context.Context) Principal {
	if p, ok := ctx.Value(principalKey{}).(Principal); ok {
		return p
	}
	return Anonymous
}

// Target is the resource coordinate a call addressed, for the tools that take
// one. All four fields are emitted even when empty: AC1 wants the column to
// exist on every record, so an aggregation over kind sees "no coordinate"
// rather than a missing key it has to special-case.
type Target struct {
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
}

// Result is how a call ended. Three values, no fourth (AC1): success, refused
// (the tool never ran — argument validation, the approval gate, an unknown
// name), error (the tool ran and reported failure).
type Result string

const (
	ResultSuccess Result = "success"
	ResultRefused Result = "refused"
	ResultError   Result = "error"
)

// Results is the closed set, for the same reason Reasons is one: the
// metrics of prd-metrics pre-register a series per value (AC1's "0 rather
// than absent"), and a list here cannot drift from the constants above.
var Results = []Result{ResultSuccess, ResultRefused, ResultError}

// Reason is why a refused call was refused (AC2, AC4).
type Reason string

const (
	ReasonAuthFailed       Reason = "auth_failed"
	ReasonInvalidInput     Reason = "invalid_input"
	ReasonUnconfigured     Reason = "unconfigured"
	ReasonGateRejected     Reason = "gate_rejected"
	ReasonGateExpired      Reason = "gate_expired"
	ReasonGateTimeout      Reason = "gate_timeout"
	ReasonGateUnreachable  Reason = "gate_unreachable"
	ReasonGateUnconfigured Reason = "gate_unconfigured"
)

// Reasons is the closed set, in prd-metrics AC2's order. It exists so the
// metrics of that PRD can pre-register every series from here rather than
// from a second list that can drift.
var Reasons = []Reason{
	ReasonAuthFailed, ReasonInvalidInput, ReasonUnconfigured,
	ReasonGateRejected, ReasonGateExpired, ReasonGateTimeout, ReasonGateUnreachable, ReasonGateUnconfigured,
}

// Gate is what the approval gate said about a gated call (AC2). Decision is
// the backend's verdict as the gate layer names it; it stays "approved" on a
// call the gate layer refused after the verdict (a target that moved, an
// approval already spent), which is exactly the case Reason and Decision are
// two fields for. AutoApproved is prd-approval-gate AC9's notice as a field.
type Gate struct {
	RequestID    string
	Decision     string
	AutoApproved bool
}

// Record is one tool call: AC1's five fields, plus AC2's reason and gate.
// The timestamp is not a field here because the sink stamps it at emission
// — a record built before the call and stamped after it would carry the
// wrong moment.
type Record struct {
	Tool      string
	Principal Principal
	Target    Target
	Result    Result
	Reason    Reason
	Gate      Gate

	// Duration is how long the dispatcher held the call with the gate wait
	// taken out, and GateWait how long the gate held it (prd-metrics AC3's
	// two histograms). Neither reaches the log line: the record's field set
	// is prd-event-log AC1/AC2's and closed, and "how long" is the metrics'
	// question. They ride on the record only because both surfaces derive
	// from this one emission point. Zero on a record the auth layer wrote —
	// that call was never timed because nothing of it ran.
	Duration time.Duration
	GateWait time.Duration
}

// Sink receives records. The dispatcher holds one so a test can capture
// records without scraping log output, and so the metrics of prd-metrics can
// later be derived at this same point rather than counted somewhere else
// (both PRDs' "표면 개요" require a single derivation point).
type Sink interface {
	Emit(ctx context.Context, r Record)
}

// Fanout is a Sink that hands each record to every sink it holds, in
// order. It is how the log line and the metrics of prd-metrics derive from
// the one emission point both PRDs require without the dispatcher knowing
// there are two of them.
type Fanout []Sink

// Emit implements Sink.
func (f Fanout) Emit(ctx context.Context, r Record) {
	for _, s := range f {
		s.Emit(ctx, r)
	}
}

// Log is the Sink that writes each record as one structured slog line on the
// default logger (AC5's stdout baseline). One line per record: the message is
// constant so a collector can select records by it, and everything that
// varies is an attribute.
type Log struct{}

// Message is the constant message of every record line.
const Message = "tool call"

// Emit implements Sink.
func (Log) Emit(ctx context.Context, r Record) {
	slog.Default().LogAttrs(ctx, slog.LevelInfo, Message, Attrs(r)...)
}

// Attrs renders a record as slog attributes, in the field order AC1 lists
// them (time is the handler's own attribute). The reason and gate keys are
// present on every record, empty on a success, for the same reason the
// target keys are.
func Attrs(r Record) []slog.Attr {
	return []slog.Attr{
		slog.String("tool", r.Tool),
		slog.String("principal", r.Principal.String()),
		slog.Group("target",
			slog.String("apiVersion", r.Target.APIVersion),
			slog.String("kind", r.Target.Kind),
			slog.String("namespace", r.Target.Namespace),
			slog.String("name", r.Target.Name),
		),
		slog.String("result", string(r.Result)),
		slog.String("reason", string(r.Reason)),
		slog.Group("gate",
			slog.String("request_id", r.Gate.RequestID),
			slog.String("decision", r.Gate.Decision),
			slog.Bool("auto_approved", r.Gate.AutoApproved),
		),
	}
}
