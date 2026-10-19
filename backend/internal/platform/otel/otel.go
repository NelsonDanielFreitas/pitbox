// Package otel configures OpenTelemetry tracing for the process.
//
// Tracing is opt-in: without an OTLP endpoint in the environment nothing is
// exported and no exporter is created. Telemetry failures never stop the service
// (fail open): they are logged and the process keeps running.
//
// Metrics arrive with the Connect interceptor in F1-02, which records traces and
// metrics together (architecture §4.4).
package otel

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// DefaultServiceName is used unless OTEL_SERVICE_NAME overrides it.
const DefaultServiceName = "pitbox-api"

// Telemetry is the handle returned by Setup.
type Telemetry struct {
	// TracerProvider is never nil: it is a no-op provider when tracing is off.
	TracerProvider trace.TracerProvider
	// Shutdown flushes pending spans and releases the exporter. Safe to call once.
	Shutdown func(context.Context) error
}

// Settings is the part of the process configuration telemetry needs. Both the API
// and the worker satisfy it; the worker's configuration deliberately has no
// authentication secrets.
type Settings interface {
	TracingEnabled() bool
	Environment() string
}

// Setup configures the global propagator and, if an OTLP endpoint is set, the
// tracer provider. The exporter is the OTLP/HTTP one, so the process does not
// link the gRPC stack just for tracing.
func Setup(ctx context.Context, cfg Settings, logger *slog.Logger) (*Telemetry, error) {
	// Only W3C trace context travels. Baggage is deliberately not enabled: on a
	// public endpoint it is arbitrary client data entering the system.
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// Exporter errors go to our JSON logger instead of the standard library log.
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.WarnContext(context.Background(), "opentelemetry error", slog.Any("error", err))
	}))

	if !cfg.TracingEnabled() {
		logger.InfoContext(ctx, "tracing disabled: no OTLP endpoint configured")
		return &Telemetry{
			TracerProvider: noop.NewTracerProvider(),
			Shutdown:       func(context.Context) error { return nil },
		}, nil
	}

	res, err := newResource(ctx, cfg.Environment())
	if err != nil {
		return nil, fmt.Errorf("build otel resource: %w", err)
	}
	// The endpoint, headers and TLS settings come from the standard OTEL_EXPORTER_OTLP_*
	// variables, read by the exporter. The URL scheme decides TLS (https by default).
	exp, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}

	tp := newTracerProvider(res, sdktrace.WithBatcher(exp))
	otel.SetTracerProvider(tp)
	logger.InfoContext(ctx, "tracing enabled")

	return &Telemetry{TracerProvider: tp, Shutdown: tp.Shutdown}, nil
}

// newTracerProvider builds a provider with the given processor or exporter
// options. Tests pass a span recorder here.
//
// The sampler is left to the SDK, which honours OTEL_TRACES_SAMPLER (default:
// parent-based, always on). Requests from the Internet never carry a sampling
// decision into the process because the HTTP middleware treats the endpoint as
// public.
func newTracerProvider(res *resource.Resource, opts ...sdktrace.TracerProviderOption) *sdktrace.TracerProvider {
	return sdktrace.NewTracerProvider(append([]sdktrace.TracerProviderOption{sdktrace.WithResource(res)}, opts...)...)
}

// newResource describes this process. The defaults come first and
// resource.WithFromEnv last, so OTEL_SERVICE_NAME and OTEL_RESOURCE_ATTRIBUTES
// override them.
//
// resource.Merge with resource.Default() is avoided on purpose: it fails with
// "conflicting Schema URL" when the semconv versions differ.
func newResource(ctx context.Context, environment string) (*resource.Resource, error) {
	return resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.name", DefaultServiceName),
			attribute.String("service.version", buildVersion()),
			attribute.String("deployment.environment.name", environment),
		),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
	)
}

func buildVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "unknown"
}
