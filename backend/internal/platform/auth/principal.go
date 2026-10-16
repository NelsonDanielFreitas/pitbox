package auth

// Principal and its context helpers moved to the auth/principal subpackage so that other
// platform packages (in particular interceptors) can read the authenticated identity without
// creating an import cycle. The names below are kept as aliases for existing callers.

import (
	"context"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/auth/principal"
)

// Principal is an alias for backward compatibility.
type Principal = principal.Principal

// WithPrincipal returns a context carrying p.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return principal.With(ctx, p)
}

// PrincipalFrom returns the authenticated principal of the request, if any.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	return principal.From(ctx)
}
