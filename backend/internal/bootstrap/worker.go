package bootstrap

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"runtime"

	"go.opentelemetry.io/otel"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/config"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	platformlog "github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/log"
	platformotel "github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/otel"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/outbox"
)

// RunWorker runs pitbox-worker until ctx is cancelled. It returns nil after a
// clean shutdown. Logs go to logOut as JSON.
func RunWorker(ctx context.Context, logOut io.Writer) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger := platformlog.New(logOut, cfg.SlogLevel())
	return runWorker(ctx, cfg, logger)
}

func runWorker(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	logger.InfoContext(ctx, "starting",
		slog.String("service", "pitbox-worker"),
		slog.String("go_version", runtime.Version()),
		slog.Any("config", cfg),
	)

	tel, err := platformotel.Setup(ctx, cfg, logger)
	if err != nil {
		return fmt.Errorf("setup telemetry: %w", err)
	}

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		flushTelemetry(ctx, tel, logger)
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()

	authzClient, err := newAuthzClient(ctx, cfg, logger)
	if err != nil {
		flushTelemetry(ctx, tel, logger)
		return fmt.Errorf("set up authorization: %w", err)
	}

	opts := outbox.DefaultOptions()
	opts.BatchSize = cfg.OutboxBatchSize
	opts.Lease = cfg.OutboxLease
	opts.MaxBackoff = cfg.OutboxMaxBackoff
	opts.PollInterval = cfg.OutboxPollInterval
	opts.AlertAfter = cfg.OutboxAlertAttempts

	proc, err := outbox.NewProcessor(pool, authzClient, opts, logger, otel.GetMeterProvider(), tel.TracerProvider)
	if err != nil {
		flushTelemetry(ctx, tel, logger)
		return fmt.Errorf("create outbox processor: %w", err)
	}

	// A hard deadline bounds a stuck database call (the pool already has server-side
	// timeouts); the loop keeps the process alive between batches.
	runErr := proc.Run(ctx)

	flushTelemetry(ctx, tel, logger)
	if runErr == nil {
		logger.InfoContext(ctx, "shutdown complete")
	}
	return runErr
}
