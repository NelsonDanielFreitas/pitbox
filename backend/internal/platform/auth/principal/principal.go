// Package principal holds the authenticated identity that travels in the request context.
// It lives in a subpackage so interceptors can read it without importing the larger auth
// package (which imports interceptors for audit metadata).
package principal

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

// With returns a context carrying p.
func With(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// From returns the authenticated principal of the request, if any.
func From(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}
