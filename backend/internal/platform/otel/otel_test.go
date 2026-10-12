package otel

import (
	"context"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/config"
)

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestSetupWithoutEndpointIsANoop(t *testing.T) {
	cfg, err := config.LoadFrom(map[string]string{})
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}

	tel, err := Setup(context.Background(), cfg, discardLogger())
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if tel.TracerProvider == nil {
		t.Fatal("TracerProvider must never be nil")
	}
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown of a no-op provider: %v", err)
	}
	if _, isSDK := tel.TracerProvider.(*sdktrace.TracerProvider); isSDK {
		t.Error("no exporter may be created without an endpoint")
	}
}

// Setup installs the process-wide propagator; this test checks that it only
// carries W3C trace context and never baggage.
func TestPropagatorIsTraceContextOnly(t *testing.T) {
	cfg, err := config.LoadFrom(map[string]string{})
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if _, err := Setup(context.Background(), cfg, discardLogger()); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	fields := otel.GetTextMapPropagator().Fields()
	for _, f := range fields {
		if f == "baggage" {
			t.Errorf("baggage must not be propagated, fields = %v", fields)
		}
	}
	want := propagation.TraceContext{}.Fields()
	if len(fields) != len(want) {
		t.Errorf("fields = %v, want %v", fields, want)
	}
}

func TestResource(t *testing.T) {
	tests := []struct {
		name        string
		serviceName string // OTEL_SERVICE_NAME ("" = unset)
		want        string
	}{
		{name: "default service name", want: DefaultServiceName},
		{name: "environment variable wins", serviceName: "pitbox-api-test", want: "pitbox-api-test"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.serviceName != "" {
				t.Setenv("OTEL_SERVICE_NAME", tc.serviceName)
			} else {
				t.Setenv("OTEL_SERVICE_NAME", "")
			}
			res, err := newResource(context.Background(), "staging")
			if err != nil {
				t.Fatalf("newResource: %v", err)
			}
			got := map[string]string{}
			for _, kv := range res.Attributes() {
				got[string(kv.Key)] = kv.Value.String()
			}
			if got["service.name"] != tc.want {
				t.Errorf("service.name = %q, want %q", got["service.name"], tc.want)
			}
			if got["deployment.environment.name"] != "staging" {
				t.Errorf("deployment.environment.name = %q", got["deployment.environment.name"])
			}
			if got["service.version"] == "" {
				t.Error("service.version is empty")
			}
		})
	}
}

func TestNewTracerProviderRecordsSpans(t *testing.T) {
	t.Parallel()

	res, err := newResource(context.Background(), "local")
	if err != nil {
		t.Fatalf("newResource: %v", err)
	}
	rec := tracetest.NewSpanRecorder()
	tp := newTracerProvider(res, sdktrace.WithSpanProcessor(rec))

	_, span := tp.Tracer("test").Start(context.Background(), "op")
	span.End()

	if got := len(rec.Ended()); got != 1 {
		t.Errorf("ended spans = %d, want 1", got)
	}
}
