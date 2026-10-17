package interceptors

import (
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"go.opentelemetry.io/otel/trace/noop"

	identityv1 "github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/identity/v1"
)

func testOptions(t *testing.T) Options {
	t.Helper()
	v, err := NewValidator(&identityv1.CreateTeamRequest{})
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	return Options{
		Logger: newLogger(&syncBuffer{}), TracerProvider: noop.NewTracerProvider(), Validator: v,
		MaxUnaryTimeout: 30 * time.Second, MaxStreamDuration: 55 * time.Minute,
		Auth:      func(next connect.ServerFunc) connect.ServerFunc { return next },
		RateLimit: func(next connect.ServerFunc) connect.ServerFunc { return next },
		StepUp:    func(next connect.ServerFunc) connect.ServerFunc { return next },
	}
}

// The order is a security decision (see build): this test makes changing it a
// conscious act.
func TestChainOrder(t *testing.T) {
	t.Parallel()

	steps, err := build(testOptions(t))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var names []string
	for _, s := range steps {
		names = append(names, s.name)
	}
	want := []string{"tracing", "recovery", "errors", "limits", "authentication", "ratelimit", "stepup", "validation", "audit"}
	if !slices.Equal(names, want) {
		t.Errorf("order = %v, want %v", names, want)
	}
}

func TestChainOrderWithoutStepUp(t *testing.T) {
	t.Parallel()

	o := testOptions(t)
	o.StepUp = nil
	steps, err := build(o)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var names []string
	for _, s := range steps {
		names = append(names, s.name)
	}
	want := []string{"tracing", "recovery", "errors", "limits", "authentication", "ratelimit", "validation", "audit"}
	if !slices.Equal(names, want) {
		t.Errorf("order = %v, want %v", names, want)
	}
}

func TestBuildRequiresItsDependencies(t *testing.T) {
	t.Parallel()

	for name, mutate := range map[string]func(*Options){
		"no logger":     func(o *Options) { o.Logger = nil },
		"no tracer":     func(o *Options) { o.TracerProvider = nil },
		"no validator":  func(o *Options) { o.Validator = nil },
		"no auth":       func(o *Options) { o.Auth = nil },
		"no rate limit": func(o *Options) { o.RateLimit = nil },
		"no unary":      func(o *Options) { o.MaxUnaryTimeout = 0 },
		"no stream":     func(o *Options) { o.MaxStreamDuration = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			o := testOptions(t)
			mutate(&o)
			if _, err := Server(o); err == nil {
				t.Error("Server must fail")
			}
		})
	}
}

// With lazy compilation off, a type missing from the list is an error, never a
// silent pass.
func TestValidatorRefusesUnlistedTypes(t *testing.T) {
	t.Parallel()

	v, err := NewValidator(&identityv1.CreateTeamRequest{})
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	if err := v.Validate(&identityv1.ListMyTeamsRequest{}); err == nil {
		t.Error("an unlisted message type must not validate")
	}
}
