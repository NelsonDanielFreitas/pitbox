package interceptors

import (
	"context"

	"connectrpc.com/connect/v2"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/auth/principal"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/clock"
)

// Fixed public messages for step-up failures.
const (
	msgStepUp            = "reauthentication required"
	msgStepUpNoPrincipal = "unauthenticated"
)

// StepUpOptions splits the sensitive procedures in two lists:
//
//   - Sensitive: a recent step-up (strong factor and auth_time within the window).
//   - StrongFactor: a strong factor is required, but the session may be older
//     (for example CreateTeam, which makes the caller an owner).
//
// The lists are part of the security surface: a procedure not listed is not
// protected. They should be reviewed in code review.
type StepUpOptions struct {
	Sensitive    []string
	StrongFactor []string
}

// StepUp refuses sensitive RPCs when the caller has not authenticated recently
// with a strong factor. It sits in slot 7 of the chain, after rate limiting and
// before validation.
func StepUp(clk clock.Clock, sensitive ...string) connect.ServerInterceptor {
	return StepUpWithOptions(clk, StepUpOptions{Sensitive: sensitive})
}

// StepUpWithOptions is StepUp with a second list that only requires a strong
// factor (no recency window). It is used by CreateTeam: whoever enters with a
// password must not become an owner without MFA, even with a fresh session.
func StepUpWithOptions(clk clock.Clock, opts StepUpOptions) connect.ServerInterceptor {
	sensitive := make(map[string]struct{}, len(opts.Sensitive))
	for _, p := range opts.Sensitive {
		sensitive[p] = struct{}{}
	}
	strong := make(map[string]struct{}, len(opts.StrongFactor))
	for _, p := range opts.StrongFactor {
		strong[p] = struct{}{}
	}
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			_, needsStrong := strong[spec.Procedure]
			_, needsRecent := sensitive[spec.Procedure]
			if !needsStrong && !needsRecent {
				return next(ctx, spec, stream)
			}
			p, ok := principal.From(ctx)
			if !ok {
				// This should not happen: the authentication interceptor is default-deny and
				// runs before this one. Treat as unauthenticated to fail closed.
				return connect.NewError(connect.CodeUnauthenticated, msgStepUpNoPrincipal)
			}
			if needsStrong && !principal.IsStrongFactor(p.AMR) {
				return connect.NewError(connect.CodeUnauthenticated, msgStepUp)
			}
			if needsRecent && principal.RequiresReauthentication(p, clk.Now()) {
				return connect.NewError(connect.CodeUnauthenticated, msgStepUp)
			}
			return next(ctx, spec, stream)
		}
	}
}
