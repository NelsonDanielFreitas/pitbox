// Package identity is the identity module: teams, members, invitations, devices and the
// first-login mapping of an OIDC subject to a user. Other modules and the platform use only
// what this file exposes.
package identity

import (
	"context"
	"errors"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/store"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/auth"
)

// LoginDirectory is the auth.UserDirectory backed by the identity module.
type LoginDirectory struct{ logins *store.Logins }

// NewLoginDirectory takes the pool of the application role.
func NewLoginDirectory(db store.Querier) *LoginDirectory {
	return &LoginDirectory{logins: store.NewLogins(db)}
}

var _ auth.UserDirectory = (*LoginDirectory)(nil)

// ResolveLogin maps the module's errors to the ones the login flow understands. An invalid
// subject or email is not a refusal of the person: it is data the identity provider should
// never send, so it stays an internal failure.
func (d *LoginDirectory) ResolveLogin(ctx context.Context, subject, email, displayName string) (string, error) {
	id, err := d.logins.ResolveLogin(ctx, subject, email, displayName)
	switch {
	case errors.Is(err, domain.ErrEmailInUse):
		return "", auth.ErrEmailInUse
	case errors.Is(err, domain.ErrAccountDeleted):
		return "", auth.ErrAccountDeleted
	}
	return id, err
}
