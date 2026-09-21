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

// String renders the principal for the record: "jwt:<sub>", "api_key:<n>",
// or "anonymous".
func (p Principal) String() string {
	switch p.Method {
	case "", "none":
		return "anonymous"
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

// Record is one tool call: AC1's five fields. The timestamp is not a field
// here because the sink stamps it at emission — a record built before the
// call and stamped after it would carry the wrong moment.
type Record struct {
	Tool      string
	Principal Principal
	Target    Target
	Result    Result
}

// Sink receives records. The dispatcher holds one so a test can capture
// records without scraping log output, and so the metrics of prd-metrics can
// later be derived at this same point rather than counted somewhere else
// (both PRDs' "표면 개요" require a single derivation point).
type Sink interface {
	Emit(ctx context.Context, r Record)
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
// them (time is the handler's own attribute).
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
	}
}
