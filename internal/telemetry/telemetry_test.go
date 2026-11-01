package telemetry

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func withRecorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})
	return rec
}

// The reason this package exists: a traceparent behind a long Authorization
// header (as Traefik writes it, alphabetically) must still be continued.
func TestHandlerContinuesTraceBehindLongAuthorization(t *testing.T) {
	rec := withRecorder(t)
	h := Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}), "/healthz")

	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 2000))
	req.Header.Set("Traceparent", "00-"+traceID+"-00f067aa0ba902b7-01")
	h.ServeHTTP(httptest.NewRecorder(), req)

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	if got := spans[0].SpanContext().TraceID().String(); got != traceID {
		t.Errorf("trace id = %s, want %s", got, traceID)
	}
	if got := spans[0].Parent().SpanID().String(); got != "00f067aa0ba902b7" {
		t.Errorf("parent span id = %s, want 00f067aa0ba902b7", got)
	}
	if got := spans[0].Name(); got != "POST /mcp" {
		t.Errorf("span name = %q, want %q", got, "POST /mcp")
	}
}

func TestHandlerSkipsProbePaths(t *testing.T) {
	rec := withRecorder(t)
	h := Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}), "/healthz", "/readyz")

	for _, p := range []string{"/healthz", "/readyz"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, p, nil))
	}
	if n := len(rec.Ended()); n != 0 {
		t.Errorf("got %d spans for probe paths, want 0", n)
	}
}

func TestSetupIsNoopWithoutEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	before := http.DefaultTransport
	shutdown, err := Setup(t.Context(), "svc", "0.0.0")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if err := shutdown(t.Context()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if http.DefaultTransport != before {
		t.Error("Setup replaced http.DefaultTransport while tracing is disabled")
	}
}

func TestKubeTransportNamesClientSpans(t *testing.T) {
	rec := withRecorder(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	t.Cleanup(srv.Close)

	client := &http.Client{Transport: KubeTransport(http.DefaultTransport)}
	resp, err := client.Get(srv.URL + "/api/v1/namespaces/default/pods")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	_ = resp.Body.Close()

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	if got, want := spans[0].Name(), "k8s GET /api/v1/namespaces/default/pods"; got != want {
		t.Errorf("span name = %q, want %q", got, want)
	}
}
