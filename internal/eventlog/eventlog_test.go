package eventlog

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestPrincipalRendersWithoutTheCredential(t *testing.T) {
	cases := map[Principal]string{
		{Method: "jwt", ID: "operator@example.test"}: "jwt:operator@example.test",
		{Method: "api_key", ID: "2"}:                 "api_key:2",
		{}:                                           "anonymous",
		Anonymous:                                    "anonymous",
	}
	for p, want := range cases {
		if got := p.String(); got != want {
			t.Errorf("%+v.String() = %q, want %q", p, got, want)
		}
	}
}

func TestPrincipalRoundTripsThroughContext(t *testing.T) {
	if got := PrincipalFrom(context.Background()); got != Anonymous {
		t.Fatalf("PrincipalFrom(empty) = %+v, want Anonymous", got)
	}
	want := Principal{Method: "api_key", ID: "1"}
	if got := PrincipalFrom(WithPrincipal(context.Background(), want)); got != want {
		t.Fatalf("PrincipalFrom(WithPrincipal) = %+v, want %+v", got, want)
	}
}

// AC1: the target column exists on every record, coordinate or not. A tool
// with no coordinate must still emit the four keys, empty — an aggregation
// over kind has to see "none" rather than a missing key.
func TestLogWritesOneLineWithEveryField(t *testing.T) {
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	Log{}.Emit(context.Background(), Record{Tool: "ping", Principal: Anonymous, Result: ResultSuccess})

	line := buf.String()
	if n := strings.Count(line, "\n"); n != 1 {
		t.Fatalf("record spans %d lines, want 1:\n%s", n, line)
	}
	for _, want := range []string{
		"time=", `msg="tool call"`, "tool=ping", "principal=anonymous",
		`target.apiVersion=""`, `target.kind=""`, `target.namespace=""`, `target.name=""`,
		"result=success",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("record %q is missing %q", line, want)
		}
	}
}
