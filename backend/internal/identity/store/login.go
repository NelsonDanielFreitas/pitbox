// Package store is the identity module's persistence.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/domain"
)

// Querier is what the store needs from a pool.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Logins resolves the OIDC subject to the user, creating it on the first login, through
// identity.resolve_login (SECURITY DEFINER: the row-level security hides the account by
// subject from the application role before app.user_id exists).
type Logins struct{ db Querier }

// NewLogins takes the pool of the application role.
func NewLogins(db Querier) *Logins { return &Logins{db: db} }

// ResolveLogin returns the id of the user. SQLSTATE codes of the function become domain
// errors; anything else is an infrastructure failure and keeps its cause for the log.
func (l *Logins) ResolveLogin(ctx context.Context, subject, email, displayName string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var id string
	err := l.db.QueryRow(ctx, `SELECT identity.resolve_login($1, $2::citext, $3)::text`, subject, email, displayName).Scan(&id)
	if err == nil {
		return id, nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "PB001":
			return "", domain.ErrEmailInUse
		case "PB002":
			return "", domain.ErrAccountDeleted
		case "22023":
			return "", domain.ErrInvalidLogin
		}
	}
	return "", fmt.Errorf("resolve login: %w", err)
}

// IsOwner reports whether the user owns any team.
func (l *Logins) IsOwner(ctx context.Context, userID string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var isOwner bool
	err := l.db.QueryRow(ctx, `SELECT identity.is_owner($1)::boolean`, userID).Scan(&isOwner)
	if err != nil {
		return false, fmt.Errorf("is owner: %w", err)
	}
	return isOwner, nil
}
