package db

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/apperr"
)

var (
	// ErrNotMember means the user is not a member of the team (or the team or the user does
	// not exist or was deleted). All of those are one error, because the answer to the client
	// is the same, NOT_FOUND (it wraps apperr.ErrNotFound): the response must not tell a team
	// that exists from one that does not.
	ErrNotMember = fmt.Errorf("db: the user is not a member of the team: %w", apperr.ErrNotFound)

	// ErrTxClosed is returned by a DBTX used after the function it was given to has returned.
	ErrTxClosed = errors.New("db: the transaction has ended")
)

// DBTX is what a use case may do inside a transaction: run statements. There is no Commit,
// Rollback or Begin: WithTeamTx decides when the transaction ends, so a function cannot
// commit half of its work, or open a second transaction without the team context. It is
// the same shape as the interface sqlc generates, plus the batch and copy methods.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
	CopyFrom(ctx context.Context, table pgx.Identifier, columns []string, src pgx.CopyFromSource) (int64, error)
}

// Membership is the user's place in the team, read from identity.membership in the same
// transaction (the table is the source of truth for roles, ADR 0005).
type Membership struct {
	Role string // owner, mechanic, pilot or guest
}

// DB opens transactions with the row-level-security context.
type DB struct {
	pool   *pgxpool.Pool
	tracer trace.Tracer
}

// New wraps a pool made by Open.
func New(pool *pgxpool.Pool) *DB {
	return &DB{pool: pool, tracer: otel.Tracer("pitbox/db")}
}

// SQL, in one place. Everything is parameterised: the ids reach the database as values,
// never as part of the text. SET LOCAL cannot take parameters; set_config can, and with
// is_local = true the value lasts until the end of the transaction and no longer, so a
// pooled connection never carries a context to the next request.
const (
	setContextSQL = `SELECT set_config('app.user_id', $1, true), set_config('app.team_id', $2, true)`

	// Runs under the context just set: the policies of membership, team and user_account
	// show only what belongs to it. A deleted team or user is not a membership.
	membershipSQL = `
SELECT m.role::text
  FROM identity.membership m
  JOIN identity.team t         ON t.id = m.team_id AND t.deleted_at IS NULL
  JOIN identity.user_account u ON u.id = m.user_id AND u.deleted_at IS NULL
 WHERE m.team_id = $1 AND m.user_id = $2`
)

// WithTeamTx runs fn in a read-write transaction in which the database only shows the team's
// rows (the second barrier of ADR 0005). The use case calls it after authz.Check.
//
// Before fn runs, in the same round trip as the context, it checks that the user is a member
// of the team and not deleted. If not, nothing runs and the error is ErrNotMember, which the
// errors interceptor answers as NOT_FOUND. That check does not replace OpenFGA (who may do
// what): it is the source of truth for who belongs to which team, and it closes the window in
// which OpenFGA still has a member the worker has not removed yet.
//
// The transaction commits only if fn returns nil. If fn returns an error, or panics (the
// panic continues to the recovery interceptor), it rolls back. Do no network I/O inside fn:
// it holds a connection and the locks. Do the OpenFGA Check before, and write the outbox
// events and the audit entry inside.
func (d *DB) WithTeamTx(ctx context.Context, user UserID, team TeamID, fn func(ctx context.Context, tx DBTX, m Membership) error) error {
	return d.run(ctx, user, &team, false, fn)
}

// WithTeamReadTx is WithTeamTx for reads: the transaction is read-only, so a bug in a handler
// that should only read cannot write (SQLSTATE 25006). Use it in RPCs marked NO_SIDE_EFFECTS.
func (d *DB) WithTeamReadTx(ctx context.Context, user UserID, team TeamID, fn func(ctx context.Context, tx DBTX, m Membership) error) error {
	return d.run(ctx, user, &team, true, fn)
}

// WithUserTx runs fn with only the user's context (no team): for what belongs to the user, such
// as listing their teams (team_visible shows the teams they are a member of) or their
// notification preferences. It does not create teams: the policies of team and membership ask
// for the new team's id in the context at INSERT, so team creation has its own variant (F1-06).
func (d *DB) WithUserTx(ctx context.Context, user UserID, fn func(ctx context.Context, tx DBTX) error) error {
	return d.run(ctx, user, nil, false, func(ctx context.Context, tx DBTX, _ Membership) error { return fn(ctx, tx) })
}

func (d *DB) run(ctx context.Context, user UserID, team *TeamID, readOnly bool, fn func(context.Context, DBTX, Membership) error) (err error) {
	if fn == nil || user.IsZero() || (team != nil && team.IsZero()) {
		return ErrInvalidScope
	}

	ctx, span := d.tracer.Start(ctx, "db.tx", trace.WithAttributes(attribute.Bool("db.read_only", readOnly)))
	defer func() {
		if err != nil {
			// No RecordError: the error text can carry data. The status is enough to find it.
			span.SetStatus(codes.Error, "transaction failed")
		}
		span.End()
	}()

	mode := pgx.ReadWrite
	if readOnly {
		mode = pgx.ReadOnly
	}
	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: mode})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	scope := &scoped{tx: tx}
	committed := false
	defer func() {
		scope.done.Store(true)
		if !committed {
			// Not the request's context: the rollback must happen even if the caller gave up.
			rb, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_ = tx.Rollback(rb) // after a failed commit or a closed connection there is nothing to undo
		}
	}()

	m, err := setContext(ctx, tx, user, team)
	if err != nil {
		return err
	}
	if err := fn(ctx, scope, m); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	committed = true
	return nil
}

// setContext sets the context and, for a team, reads the membership, in one round trip.
func setContext(ctx context.Context, tx pgx.Tx, user UserID, team *TeamID) (Membership, error) {
	teamArg, userArg := "", user.String()
	if team != nil {
		teamArg = team.String()
	}
	b := &pgx.Batch{}
	b.Queue(setContextSQL, userArg, teamArg)
	if team != nil {
		b.Queue(membershipSQL, teamArg, userArg)
	}
	br := tx.SendBatch(ctx, b)
	defer func() { _ = br.Close() }()

	var u, t string
	if err := br.QueryRow().Scan(&u, &t); err != nil {
		return Membership{}, fmt.Errorf("set transaction context: %w", err)
	}
	if team == nil {
		return Membership{}, nil
	}
	var m Membership
	switch err := br.QueryRow().Scan(&m.Role); {
	case errors.Is(err, pgx.ErrNoRows):
		return Membership{}, ErrNotMember
	case err != nil:
		return Membership{}, fmt.Errorf("check membership: %w", err)
	}
	return m, nil
}

// scoped is what fn receives. It hides the pgx.Tx (no type assertion reaches Commit) and stops
// working when the transaction has ended, so a function that keeps it, or hands it to a goroutine,
// cannot run statements outside the transaction.
type scoped struct {
	tx   pgx.Tx
	done atomic.Bool
}

func (s *scoped) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if s.done.Load() {
		return pgconn.CommandTag{}, ErrTxClosed
	}
	return s.tx.Exec(ctx, sql, args...)
}

func (s *scoped) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if s.done.Load() {
		return nil, ErrTxClosed
	}
	return s.tx.Query(ctx, sql, args...)
}

func (s *scoped) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if s.done.Load() {
		return closedRow{}
	}
	return s.tx.QueryRow(ctx, sql, args...)
}

func (s *scoped) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	if s.done.Load() {
		return closedBatch{}
	}
	return s.tx.SendBatch(ctx, b)
}

func (s *scoped) CopyFrom(ctx context.Context, table pgx.Identifier, columns []string, src pgx.CopyFromSource) (int64, error) {
	if s.done.Load() {
		return 0, ErrTxClosed
	}
	return s.tx.CopyFrom(ctx, table, columns, src)
}

type closedRow struct{}

func (closedRow) Scan(...any) error { return ErrTxClosed }

type closedBatch struct{}

func (closedBatch) Exec() (pgconn.CommandTag, error) { return pgconn.CommandTag{}, ErrTxClosed }
func (closedBatch) Query() (pgx.Rows, error)         { return nil, ErrTxClosed }
func (closedBatch) QueryRow() pgx.Row                { return closedRow{} }
func (closedBatch) Close() error                     { return ErrTxClosed }
