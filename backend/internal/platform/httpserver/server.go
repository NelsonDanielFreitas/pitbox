// Package httpserver is the HTTP server of pitbox-api: health endpoints, trace
// middleware and a graceful shutdown that fits the platform's kill deadline.
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	// Server limits against slow or oversized requests. There is deliberately no
	// ReadTimeout or WriteTimeout: they apply to the whole request and would cut
	// the streaming RPCs (sync, live timing). Per-request deadlines and body
	// limits belong to the Connect interceptors (F1-02).
	readHeaderTimeout = 5 * time.Second
	idleTimeout       = 120 * time.Second
	maxHeaderBytes    = 64 << 10 // the BFF cookie is small; the 1 MiB default is far too much

	healthPath = "/healthz"
	readyPath  = "/readyz"
)

// Options configures a Server.
type Options struct {
	Logger *slog.Logger
	// TracerProvider records the request spans. Required (use a no-op provider
	// when tracing is off).
	TracerProvider trace.TracerProvider
	// Drain is how long the server keeps serving, already "not ready", after a
	// shutdown starts.
	Drain time.Duration
	// ShutdownTimeout bounds http.Server.Shutdown.
	ShutdownTimeout time.Duration
	// Routes registers the application handlers on the mux.
	Routes func(mux *http.ServeMux) error
	// SkipTracing excludes requests from the HTTP trace middleware (nil: only the health
	// probes). RPC routes are excluded because otelconnect traces them: with both, every
	// RPC would have two server spans, and otelconnect would not apply its rule of not
	// trusting the caller's traceparent (it only does when there is no span yet).
	SkipTracing func(*http.Request) bool
}

// Server wraps http.Server with readiness state and graceful shutdown.
type Server struct {
	srv     *http.Server
	log     *slog.Logger
	ready   atomic.Bool
	drain   time.Duration
	timeout time.Duration
	// baseCancel cancels the context every request derives from. It is only used
	// when the graceful shutdown runs out of time.
	baseCancel context.CancelFunc
}

// New builds the server. It does not listen: see Run.
func New(o Options) (*Server, error) {
	if o.Logger == nil || o.TracerProvider == nil {
		return nil, errors.New("httpserver: Logger and TracerProvider are required")
	}

	mux := http.NewServeMux()
	s := &Server{log: o.Logger, drain: o.Drain, timeout: o.ShutdownTimeout}
	mux.HandleFunc("GET "+healthPath, handleHealth)
	mux.HandleFunc("GET "+readyPath, s.handleReady)
	if o.Routes != nil {
		if err := o.Routes(mux); err != nil {
			return nil, fmt.Errorf("register routes: %w", err)
		}
	}

	handler := otelhttp.NewHandler(mux, "http.server",
		otelhttp.WithTracerProvider(o.TracerProvider),
		otelhttp.WithPropagators(propagation.TraceContext{}),
		// The endpoint is reachable from the Internet: a caller's traceparent becomes
		// a link, not the parent, so a client can neither force sampling nor choose
		// our trace ids.
		otelhttp.WithPublicEndpointFn(func(*http.Request) bool { return true }),
		// Platform probes hit these every few seconds: thousands of useless traces.
		otelhttp.WithFilter(func(r *http.Request) bool {
			if r.URL.Path == healthPath || r.URL.Path == readyPath {
				return false
			}
			return o.SkipTracing == nil || !o.SkipTracing(r)
		}),
		// Method only: a span name with the raw path would have unbounded cardinality
		// (any scanner invents paths).
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string { return r.Method }),
	)

	// Requests derive from this context, not from the signal context, so that
	// in-flight requests can finish during a graceful shutdown.
	baseCtx, cancel := context.WithCancel(context.Background())
	s.baseCancel = cancel

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true) // gRPC over h2c locally; TLS terminates at the edge

	s.srv = &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
		Protocols:         protocols,
		BaseContext:       func(net.Listener) context.Context { return baseCtx },
		ErrorLog:          slog.NewLogLogger(o.Logger.Handler(), slog.LevelWarn),
	}
	s.ready.Store(true)
	return s, nil
}

// Handler returns the root handler, for tests that use httptest.
func (s *Server) Handler() http.Handler { return s.srv.Handler }

// Run serves on ln until ctx is cancelled, then shuts down gracefully:
//
//  1. readiness turns to 503 so the platform stops sending traffic;
//  2. the server keeps serving for the drain period;
//  3. http.Server.Shutdown waits for in-flight requests, up to the timeout;
//  4. if time runs out, request contexts are cancelled and connections closed.
//
// It returns nil after a clean shutdown.
func (s *Server) Run(ctx context.Context, ln net.Listener) error {
	serveErr := make(chan error, 1)
	go func() { serveErr <- s.srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		// Serve only returns before shutdown if something is wrong.
		s.baseCancel()
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	s.ready.Store(false)
	s.log.InfoContext(context.Background(), "shutdown started", slog.Duration("drain", s.drain))
	if s.drain > 0 {
		time.Sleep(s.drain)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	err := s.srv.Shutdown(shutdownCtx)
	if errors.Is(err, context.DeadlineExceeded) {
		// A long stream never goes idle. Cancel what is left so handlers see it.
		s.log.WarnContext(context.Background(), "graceful shutdown timed out: closing connections")
		s.baseCancel()
		err = errors.Join(err, s.srv.Close())
	}
	s.baseCancel()

	if serr := <-serveErr; !errors.Is(serr, http.ErrServerClosed) {
		err = errors.Join(err, serr)
	}
	if err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

// handleHealth is liveness: the process is up. It says nothing else on purpose
// (no version, no dependency state): it is for the platform, not for people.
func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeProbe(w, http.StatusOK, "ok\n")
}

// handleReady is readiness. It turns 503 when the shutdown starts. Dependency
// checks (Postgres, Valkey) join it when those dependencies exist.
func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	if !s.ready.Load() {
		writeProbe(w, http.StatusServiceUnavailable, "shutting down\n")
		return
	}
	writeProbe(w, http.StatusOK, "ready\n")
}

func writeProbe(w http.ResponseWriter, status int, body string) {
	h := w.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body) // a probe that cannot be written has nobody to tell
}
