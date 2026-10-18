// Package bootstrap assembles each binary: configuration, logging, telemetry,
// modules and the HTTP server. The cmd/* main functions stay thin and call it, and
// the integration tests call it too.
package bootstrap

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/clock"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/config"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/httpserver"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/interceptors"
	platformlog "github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/log"
	platformotel "github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/otel"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/ratelimit"
	"go.opentelemetry.io/otel"
)

// RunAPI runs pitbox-api until ctx is cancelled. It returns nil after a clean
// shutdown. Logs go to logOut as JSON.
func RunAPI(ctx context.Context, logOut io.Writer) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger := platformlog.New(logOut, cfg.SlogLevel())
	return runAPI(ctx, cfg, logger, nil)
}

// runAPI is RunAPI with its dependencies injected. onListen, if not nil, receives
// the bound address (tests listen on port 0 and need to know the real port).
func runAPI(ctx context.Context, cfg config.Config, logger *slog.Logger, onListen func(net.Addr)) error {
	logger.InfoContext(ctx, "starting",
		slog.String("service", platformotel.DefaultServiceName),
		slog.String("go_version", runtime.Version()),
		slog.Any("config", cfg), // allow-list: see config.Config.LogValue
	)

	tel, err := platformotel.Setup(ctx, cfg, logger)
	if err != nil {
		return fmt.Errorf("setup telemetry: %w", err)
	}

	vk, err := OpenValkey(ctx, cfg)
	if err != nil {
		flushTelemetry(ctx, tel, logger)
		return fmt.Errorf("open valkey: %w", err)
	}
	defer vk.Close()

	authn, err := newAuthStack(ctx, cfg, logger, vk)
	if err != nil {
		flushTelemetry(ctx, tel, logger)
		return fmt.Errorf("set up authentication: %w", err)
	}
	defer authn.close()

	rlimiter, err := ratelimit.NewLimiter(
		vk,
		ratelimit.FromConfig(cfg),
		otel.GetMeterProvider().Meter("pitbox/ratelimit"),
		clock.Real(),
		logger,
	)
	if err != nil {
		flushTelemetry(ctx, tel, logger)
		return fmt.Errorf("set up rate limiting: %w", err)
	}

	authzClient, err := newAuthzClient(ctx, cfg, logger)
	if err != nil {
		flushTelemetry(ctx, tel, logger)
		return fmt.Errorf("set up authorization: %w", err)
	}

	// StepUp is wired with an empty sensitive-procedure list: no RPCs are registered yet
	// (modules bring their services from F1-06 onwards). Each sensitive procedure must be
	// added here when it is introduced.
	stepUp := interceptors.StepUp(clock.Real())

	api, err := newRPCServer(cfg, logger, tel.TracerProvider, authn.interceptor, interceptors.RateLimit(rlimiter), stepUp, authzClient)
	if err != nil {
		flushTelemetry(ctx, tel, logger)
		return fmt.Errorf("create rpc server: %w", err)
	}
	reflection := newReflectionServer(cfg, logger, api)

	server, err := httpserver.New(httpserver.Options{
		Logger:          logger,
		TracerProvider:  tel.TracerProvider,
		Drain:           cfg.ShutdownDrain,
		ShutdownTimeout: cfg.ShutdownTimeout,
		SkipTracing:     rpcPaths(api, reflection),
		Routes: func(mux *http.ServeMux) error {
			authn.mount(mux)
			return mountRPC(mux, cfg, api, reflection)
		},
	})
	if err != nil {
		flushTelemetry(ctx, tel, logger)
		return fmt.Errorf("create http server: %w", err)
	}

	// Listen before serving: a busy port fails here, synchronously, and tests can
	// ask for port 0.
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.HTTPAddr)
	if err != nil {
		flushTelemetry(ctx, tel, logger)
		return fmt.Errorf("listen on %s: %w", cfg.HTTPAddr, err)
	}
	logger.InfoContext(ctx, "listening", slog.String("addr", ln.Addr().String()))
	if onListen != nil {
		onListen(ln.Addr())
	}

	runErr := server.Run(ctx, ln)

	// Telemetry is flushed last, so the spans of the final requests are not lost.
	flushTelemetry(ctx, tel, logger)
	if runErr == nil {
		logger.InfoContext(ctx, "shutdown complete")
	}
	return runErr
}

// flushTelemetry flushes pending spans with its own deadline: ctx is already
// cancelled by the time the process shuts down.
func flushTelemetry(ctx context.Context, tel *platformotel.Telemetry, logger *slog.Logger) {
	flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), config.TelemetryFlushTimeout)
	defer cancel()
	if err := tel.Shutdown(flushCtx); err != nil {
		// Telemetry never decides whether the service is healthy: log and move on.
		logger.WarnContext(ctx, "flush telemetry failed", slog.Any("error", err))
	}
}
