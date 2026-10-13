// Package db opens the PostgreSQL connection pool of the application role.
//
// WithTeamTx (SET LOCAL app.team_id / app.user_id) arrives with F1-04; until then only code
// that does not touch team data uses the pool: the first-login function and the audit trail
// of platform events (team_id NULL).
package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Open creates the pool. It does not connect: the process starts without the database, and
// the first use reports the problem (as with the identity provider). The connection string
// holds the password, so no error here repeats it.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, errors.New("db: invalid database url")
	}
	cfg.MaxConns = 10
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = time.Minute
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	// Server-side limits: a stuck query or transaction cannot hold a connection (or a lock)
	// beyond what any RPC deadline allows.
	cfg.ConnConfig.RuntimeParams["application_name"] = "pitbox-api"
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "15000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "5000"
	cfg.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "15000"

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: create pool: %w", errors.New("invalid configuration"))
	}
	return pool, nil
}
