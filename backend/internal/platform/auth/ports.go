package auth

import (
	"context"
	"errors"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/interceptors"
)

// Errors a UserDirectory returns for a login that must be refused. The handler answers
// both with the same generic message: the person at the keyboard learns nothing about
// which accounts exist.
var (
	// ErrEmailInUse: the email belongs to a different account (never linked by email).
	ErrEmailInUse = errors.New("auth: email belongs to another account")
	// ErrAccountDeleted: the account was deleted.
	ErrAccountDeleted = errors.New("auth: account deleted")
)

// UserDirectory maps the OIDC subject to the PitBox user, creating it on the first login.
// The identity module implements it with identity.resolve_login.
type UserDirectory interface {
	ResolveLogin(ctx context.Context, subject, email, displayName string) (userID string, err error)
	// IsOwner reports whether the user owns any team. It is used to enforce MFA for owners
	// at login time (F1-03c).
	IsOwner(ctx context.Context, userID string) (bool, error)
}

// RecoveryCodeStore persists and consumes single-use recovery codes for MFA bypass. The auth
// package defines the interface; the identity module implements it.
type RecoveryCodeStore interface {
	// Replace deletes all unused codes for the user and inserts the new hashes atomically.
	// keyID identifies the active key used to hash them.
	Replace(ctx context.Context, userID, keyID string, codeHashes [][]byte) error
	// Consume marks one code as used if any of the candidate hashes exists and is unused.
	Consume(ctx context.Context, userID string, codeHashes [][]byte) (bool, error)
}

// ActorKind mirrors the actor_kind column of audit.audit_log.
type ActorKind string

const (
	ActorUser   ActorKind = "user"   // a known account; ActorID is the user id
	ActorSystem ActorKind = "system" // not (yet) a known account; ActorID is "anonymous" or "sub:<subject>"
)

// AuditEvent is one entry for audit.audit_log with team_id NULL (platform event). It never
// carries tokens, codes, state, emails or claims: the actor is the user id or, before it
// is known, "anonymous"; Reason is a fixed code.
type AuditEvent struct {
	Action     string // auth.login, auth.login_failed, auth.logout, auth.session_revoked
	ActorKind  ActorKind
	ActorID    string
	SessionRef string // hash of the cookie value; "" when there is no session
	Reason     string // fixed code, e.g. "email_in_use"
	Meta       interceptors.AuditMeta
}

// Auditor records AuditEvents. A failure is returned: the caller decides whether the
// action may proceed without its trail.
type Auditor interface {
	Record(ctx context.Context, e AuditEvent) error
}
