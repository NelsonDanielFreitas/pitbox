// Package db opens the PostgreSQL connection pool of the application role and runs every
// use case's transaction with the row-level-security context (WithTeamTx, WithTeamReadTx,
// WithUserTx).
//
// This is the only package that sets app.user_id and app.team_id (the architecture test
// refuses the strings anywhere else). Those settings are plain session variables: any SQL
// that runs as pitbox_api could set them to another team. The protection is that no SQL of
// ours is built from outside data (sqlc and parameters only) and that nothing but this package
// writes them.
package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Open creates the pool. It does not connect: the process starts without the database, and
// the first use reports the problem (as with the identity provider). The connection string
// holds the password, so no error here repeats it.
func Open(ctx context.Context, url string, opts ...Option) (*pgxpool.Pool, error) {
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

	// Every new connection proves it is the application role before it serves anything.
	cfg.AfterConnect = verifyRole
	for _, o := range opts {
		o(cfg)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: create pool: %w", errors.New("invalid configuration"))
	}
	return pool, nil
}

// Option changes the pool configuration. Only tests need it.
type Option func(*pgxpool.Config)

// MaxConns sets the pool size (tests use 1 to force one connection to serve every request).
func MaxConns(n int32) Option { return func(c *pgxpool.Config) { c.MaxConns = n } }

// errWrongRole is deliberately generic: it says what is wrong with the role, and no name,
// address or password.
var errWrongRole = errors.New("db: the connection role is not the application role " +
	"(it must be a member of pitbox_app, not a superuser, without BYPASSRLS, not a member of pitbox_owner, " +
	"and the connection must start without an RLS context)")

// verifyRole runs on every new connection (AfterConnect) as an allow-list: the role must be a
// member of pitbox_app and nothing that bypasses row-level security. The table owner is exempt
// from RLS, so a URL that names pitbox_owner or the migrator (a member of it, and
// ALTER ROLE ... SET role = pitbox_owner makes its sessions run as the owner) would switch the
// isolation off without a single error. current_user, not session_user: the migrator logs in as
// itself and then becomes the owner. A connection that fails the check is closed, and the
// pool reports the error to whoever asked for it (fail closed).
//
// The connection must also start with no RLS context. A URL with options=-c app.team_id=... (or
// ALTER ROLE / ALTER DATABASE ... SET app.team_id) would give a team context to everything that
// runs outside WithTeamTx, such as the audit writer, before any transaction sets its own.
func verifyRole(ctx context.Context, conn *pgx.Conn) error {
	var super, bypass, app, owner, clean bool
	err := conn.QueryRow(ctx, `
SELECT r.rolsuper, r.rolbypassrls,
       pg_has_role(current_user, 'pitbox_app', 'member'),
       pg_has_role(current_user, 'pitbox_owner', 'member'),
       coalesce(current_setting('app.team_id', true), '') = '' AND coalesce(current_setting('app.user_id', true), '') = ''
  FROM pg_roles r
 WHERE r.rolname = current_user`).Scan(&super, &bypass, &app, &owner, &clean)
	if err != nil || super || bypass || !app || owner || !clean {
		return errWrongRole
	}
	return nil
}
