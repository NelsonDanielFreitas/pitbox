package bootstrap

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/valkey-io/valkey-go"
	"go.opentelemetry.io/otel"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/audit"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/auth"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/config"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
)

// authStack is everything authentication needs, built once at startup.
type authStack struct {
	interceptor connect.ServerInterceptor
	mount       func(mux *http.ServeMux)
	close       func()
}

// newAuthStack wires the login flow (ADR 0013): the Valkey session store, the OIDC provider
// (discovered lazily, so the process starts without Zitadel), the first-login directory and
// the audit writer, behind the interceptor and the /auth/* routes.
//
// Without configuration (possible only in local development, config.Validate) it returns the
// deny-all interceptor and routes that answer 503: the service stays up and serves nothing,
// it never serves unauthenticated.
func newAuthStack(ctx context.Context, cfg config.Config, logger *slog.Logger, vk valkey.Client) (*authStack, error) {
	if !cfg.AuthConfigured() {
		logger.WarnContext(ctx, "authentication is not configured: every RPC is refused and /auth/* answers 503")
		return &authStack{
			interceptor: auth.DenyAll(),
			mount: func(mux *http.ServeMux) {
				mux.HandleFunc("/auth/{rest...}", func(w http.ResponseWriter, _ *http.Request) {
					http.Error(w, "authentication is not configured", http.StatusServiceUnavailable)
				})
			},
			close: func() {},
		}, nil
	}

	keys, err := cfg.AuthKeyring()
	if err != nil {
		return nil, fmt.Errorf("auth keys: %w", err)
	}
	keyring, err := auth.NewKeyring(keys)
	if err != nil {
		return nil, fmt.Errorf("keyring: %w", err)
	}
	loginSealer, err := keyring.Sealer(auth.PurposeLoginCookie)
	if err != nil {
		return nil, fmt.Errorf("login sealer: %w", err)
	}
	sessionSealer, err := keyring.Sealer(auth.PurposeSessionToken)
	if err != nil {
		return nil, fmt.Errorf("session sealer: %w", err)
	}

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	origin := strings.TrimSuffix(cfg.PublicURL, "/")
	now := time.Now
	store := auth.NewStore(vk, sessionSealer, cfg.SessionIdleTimeout, cfg.SessionAbsoluteTimeout, now)
	provider := auth.NewProvider(auth.ProviderConfig{
		Issuer: cfg.OIDCIssuer, ClientID: cfg.OIDCClientID, ClientSecret: cfg.OIDCClientSecret,
		RedirectURL: origin + auth.CallbackPath, AllowedAlgs: cfg.OIDCAllowedAlgs, Now: now,
	})
	auditor := audit.NewWriter(pool)
	refusals, err := auth.NewRefusalCounter(otel.GetMeterProvider().Meter("pitbox/auth"))
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("auth metrics: %w", err)
	}
	handler := &auth.Handler{
		Provider: provider, Store: store, LoginCookies: auth.NewLoginCookies(loginSealer, now),
		Users: identity.NewLoginDirectory(pool), Audit: auditor, Log: logger, Refusals: refusals,
		PublicURL: origin, ClientIPHeader: cfg.ClientIPHeader(), SessionMaxAge: cfg.SessionAbsoluteTimeout, Now: now,
	}
	authenticator := &auth.Authenticator{Provider: provider, Store: store, Audit: auditor, Log: logger, Now: now}
	handler.Authn = authenticator

	return &authStack{
		// No public procedures yet: every RPC needs a session.
		interceptor: auth.Interceptor(authenticator, origin),
		mount:       handler.Mount,
		close: func() {
			pool.Close()
		},
	}, nil
}

// OpenValkey connects to the session store, retrying for a few seconds: in Compose and on
// Cloud Run the store may come up a moment after the API. It is exported so the rate limiter
// can share the same client.
func OpenValkey(ctx context.Context, cfg config.Config) (valkey.Client, error) {
	opt := valkey.ClientOption{
		InitAddress:      []string{cfg.ValkeyAddr},
		Username:         cfg.ValkeyUsername,
		Password:         cfg.ValkeyPassword,
		DisableCache:     true, // client-side caching would keep stale sessions
		ClientSetInfo:    []string{"pitbox-api", "1"},
		ConnWriteTimeout: 5 * time.Second,
	}
	if cfg.ValkeyTLS {
		opt.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	for attempt := 0; attempt < 10; attempt++ {
		c, err := valkey.NewClient(opt)
		if err == nil {
			return c, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	// The cause is not wrapped: a fixed message keeps the startup log free of surprises.
	return nil, errors.New("cannot connect to the session store (Valkey)")
}
