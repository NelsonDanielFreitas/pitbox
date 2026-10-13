package auth

import (
	"context"
	"time"
)

// Principal is who is making a request, taken from the session. The teams and roles are
// not here: the authorization engine checks them on every call (ADR 0004).
type Principal struct {
	UserID     string
	Subject    string
	SessionRef string
	AMR        []string
	AuthTime   time.Time
}

type principalKey struct{}

// WithPrincipal returns a context carrying p.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom returns the authenticated principal of the request, if any.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}
