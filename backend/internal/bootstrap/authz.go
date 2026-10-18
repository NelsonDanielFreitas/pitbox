package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/authz"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/config"
)

// newAuthzClient connects to OpenFGA. In local development it can auto-create the
// store when no store or model id is configured; in every other environment the
// configuration must be explicit so the service fails closed on a mistake.
func newAuthzClient(ctx context.Context, cfg config.Config, logger *slog.Logger) (*authz.Client, error) {
	c, err := authz.New(ctx, authz.Config{
		Addr:         cfg.OpenFGAAddr,
		StoreName:    "pitbox",
		StoreID:      cfg.OpenFGAStoreID,
		ModelID:      cfg.OpenFGAModelID,
		PresharedKey: cfg.OpenFGAPresharedKey,
		TLS:          cfg.OpenFGATLSEnabled,
		Timeout:      cfg.OpenFGATimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("connect to openfga: %w", err)
	}
	logger.InfoContext(ctx, "openfga connected",
		slog.String("store_id", c.StoreID()),
		slog.String("model_id", c.ModelID()),
	)
	return c, nil
}
