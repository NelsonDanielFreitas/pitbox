package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/authz"
)

// openFGAConfig builds the OpenFGA client settings shared by the API and the worker.
func openFGAConfig(addr, storeID, modelID, presharedKey string, tls bool, timeout time.Duration) authz.Config {
	return authz.Config{
		Addr:         addr,
		StoreName:    "pitbox",
		StoreID:      storeID,
		ModelID:      modelID,
		PresharedKey: presharedKey,
		TLS:          tls,
		Timeout:      timeout,
	}
}

// newAuthzClient connects to OpenFGA. In local development it can auto-create the
// store when no store or model id is configured; in every other environment the
// configuration must be explicit so the service fails closed on a mistake.
func newAuthzClient(ctx context.Context, cfg authz.Config, logger *slog.Logger) (*authz.Client, error) {
	c, err := authz.New(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to openfga: %w", err)
	}
	logger.InfoContext(ctx, "openfga connected",
		slog.String("store_id", c.StoreID()),
		slog.String("model_id", c.ModelID()),
	)
	return c, nil
}
