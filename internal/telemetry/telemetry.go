// Package telemetry wires OpenTelemetry tracing: an OTLP/HTTP exporter, W3C
// trace-context propagation, a server middleware and an instrumented
// http.DefaultTransport.
//
// Tracing is opt-in. With OTEL_EXPORTER_OTLP_ENDPOINT (or
// OTEL_EXPORTER_OTLP_TRACES_ENDPOINT) unset, Setup leaves the global no-op
// tracer in place, so tests, the kind e2e and local runs export nothing.
//
// Why an SDK at all when OBI (eBPF) already traces this process: OBI's Go
// server probe looks for the traceparent header only in the first 1024 bytes
// of the request buffer. Traefik writes headers in alphabetical order, so a
// long Authorization (OAuth bearer) pushes Traceparent past that window and
// /mcp spans started a new trace instead of continuing Traefik's. The SDK
// reads the parsed headers, so the edge-to-app link holds. OBI skips processes
// that export OTLP themselves (discovery.exclude_otel_instrumented_services).
package telemetry

import (
	"context"
	"errors"
	"net/http"
	"os"

	"github.com/aws/smithy-go/tracing"
	"github.com/aws/smithy-go/tracing/smithyoteltracing"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
)

// Enabled reports whether an OTLP endpoint is configured.
func Enabled() bool {
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" ||
		os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != ""
}

// Setup installs the global tracer provider and propagator and wraps
// http.DefaultTransport so outgoing requests from clients that use it carry
// traceparent and get client spans. serviceName is the default for
// service.name; OTEL_SERVICE_NAME and OTEL_RESOURCE_ATTRIBUTES override it.
// The returned function flushes and stops the exporter.
func Setup(ctx context.Context, serviceName, serviceVersion string) (func(context.Context) error, error) {
	noop := func(context.Context) error { return nil }
	if !Enabled() {
		return noop, nil
	}

	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return noop, err
	}
	// Later options win: the compiled-in name is the default and
	// OTEL_SERVICE_NAME / OTEL_RESOURCE_ATTRIBUTES override it.
	res, err := resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(serviceVersion),
		),
		resource.WithFromEnv(),
	)
	if err != nil && !errors.Is(err, resource.ErrPartialResource) {
		return noop, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	// The OTLP exporter keeps its own transport, so its export requests are
	// not traced by this wrapper.
	http.DefaultTransport = otelhttp.NewTransport(http.DefaultTransport)

	return tp.Shutdown, nil
}

// Handler wraps h with a server span per request. Requests to any of
// skipPaths (probes, scrapes) get no span.
func Handler(h http.Handler, skipPaths ...string) http.Handler {
	skip := make(map[string]bool, len(skipPaths))
	for _, p := range skipPaths {
		skip[p] = true
	}
	return otelhttp.NewHandler(h, "http.server",
		otelhttp.WithFilter(func(r *http.Request) bool { return !skip[r.URL.Path] }),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + r.URL.Path
		}),
	)
}

// KubeTransport wraps a client-go transport so API requests get client
// spans named "k8s METHOD path". Use it only on configs for plain
// request/response calls: exec, attach and port-forward upgrade the
// connection to SPDY and must keep an unwrapped config, since the wrapper
// replaces the response body the upgrade hijacks.
func KubeTransport(rt http.RoundTripper) http.RoundTripper {
	return otelhttp.NewTransport(rt,
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return "k8s " + r.Method + " " + r.URL.Path
		}),
	)
}

// AWSTracerProvider returns the global tracer provider in the form the AWS
// SDK's built-in tracing takes. Set it on each service client's Options
// (TracerProvider) so its calls (S3, STS, credential refreshes) get client
// spans: the SDK builds its own HTTP client, so the http.DefaultTransport
// wrapper in Setup does not reach it. Call it after Setup, which installs the
// provider this captures.
func AWSTracerProvider() tracing.TracerProvider {
	return smithyoteltracing.Adapt(otel.GetTracerProvider())
}
