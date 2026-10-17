// Package interceptors builds the Connect server interceptor chain (architecture §4.4).
//
// The chain is a stack: the first interceptor wraps all the others, sees every call
// first and the error last.
package interceptors

import (
	"errors"
	"log/slog"
	"time"

	"buf.build/go/protovalidate"
	"connectrpc.com/connect/v2"
	"go.opentelemetry.io/otel/trace"
)

// Options configures the chain.
type Options struct {
	Logger         *slog.Logger
	TracerProvider trace.TracerProvider
	Validator      protovalidate.Validator
	// MaxUnaryTimeout is the default and the ceiling of a unary RPC's deadline.
	MaxUnaryTimeout time.Duration
	// MaxStreamDuration is the ceiling for streaming RPCs.
	MaxStreamDuration time.Duration
	// ClientIPHeader is "" for the TCP peer (see Audit).
	ClientIPHeader string
	// Auth authenticates the call (internal/platform/auth). Required: a chain without it
	// would serve everything to everyone, so building one is an error.
	Auth connect.ServerInterceptor
	// RateLimit enforces per-user, per-team and public-procedure quotas (F1-04b).
	// Required: the chain refuses to build without it.
	RateLimit connect.ServerInterceptor
	// StepUp refuses sensitive RPCs when the caller has not reauthenticated recently
	// with a strong factor (F1-03c). Optional: if nil, no procedure is protected.
	StepUp connect.ServerInterceptor
}

type step struct {
	name        string
	interceptor connect.ServerInterceptor
}

// Server returns the interceptors in the order they must be passed to
// connect.NewServer.
func Server(o Options) ([]connect.ServerInterceptor, error) {
	steps, err := build(o)
	if err != nil {
		return nil, err
	}
	out := make([]connect.ServerInterceptor, len(steps))
	for i, s := range steps {
		out[i] = s.interceptor
	}
	return out, nil
}

// build lists the steps in order. The order is part of the security design:
//
//  1. tracing: outermost, so the span sees the final error and the trace id is in
//     the context of every log line below (the panic log included). A panic caught
//     by 2 reaches the span as an error.
//  2. recovery.
//  3. errors: inside recovery but outside everything else, so the per-RPC log line
//     also covers the refusals of authentication, rate limiting and validation.
//  4. limits: the deadline.
//  5. authentication: before any payload work, and before rate limiting (which needs the
//     user). Default deny: only procedures listed as public pass without a session.
//  6. rate limiting: per user and per team (peek of team_id) before validation, so
//     requests above quota are rejected before spending CPU.
//  7. step-up: sensitive RPCs require recent reauthentication with a strong factor.
//  8. validation: before the service implementation runs.
//  9. audit: metadata for the use cases that write.
//
// The message size limit is not an interceptor: it belongs to the transport
// (httpserver.MountRPC), which enforces it before and after decompression.
func build(o Options) ([]step, error) {
	if o.Logger == nil || o.TracerProvider == nil || o.Validator == nil || o.Auth == nil || o.RateLimit == nil {
		return nil, errors.New("interceptors: Logger, TracerProvider, Validator, Auth and RateLimit are required")
	}
	tracing, err := Tracing(o.TracerProvider)
	if err != nil {
		return nil, err
	}
	limits, err := Limits(o.MaxUnaryTimeout, o.MaxStreamDuration)
	if err != nil {
		return nil, err
	}
	steps := []step{
		{"tracing", tracing},
		{"recovery", Recovery(o.Logger)},
		{"errors", Errors(o.Logger)},
		{"limits", limits},
		{"authentication", o.Auth},
		{"ratelimit", o.RateLimit},
	}
	if o.StepUp != nil {
		steps = append(steps, step{"stepup", o.StepUp})
	}
	steps = append(steps,
		step{"validation", Validation(o.Validator)},
		step{"audit", Audit(o.ClientIPHeader)},
	)
	return steps, nil
}
