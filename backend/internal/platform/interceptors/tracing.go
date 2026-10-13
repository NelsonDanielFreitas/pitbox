package interceptors

import (
	"fmt"

	"connectrpc.com/connect/v2"
	"connectrpc.com/otelconnect"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Tracing records one server span per RPC (and RED metrics once a MeterProvider is
// installed: F5-07; until then the instruments are no-ops).
//
// otelconnect does not trust the caller's traceparent by default: a remote parent
// becomes a link and the RPC starts its own trace, as the HTTP layer does. It does
// that only when the context has no span yet, which is why the RPC routes are
// excluded from the otelhttp middleware (see httpserver.Options.SkipTracing).
// WithServerPeerAttributes is deliberately not enabled: it would put every client's
// IP address in the traces.
func Tracing(tp trace.TracerProvider) (connect.ServerInterceptor, error) {
	i, err := otelconnect.NewServerInterceptor(
		otelconnect.WithTracerProvider(tp),
		otelconnect.WithPropagator(propagation.TraceContext{}),
	)
	if err != nil {
		return nil, fmt.Errorf("create otelconnect interceptor: %w", err)
	}
	return i, nil
}
