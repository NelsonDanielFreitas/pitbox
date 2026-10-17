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

// StepUp refuses sensitive RPCs when the caller has not authenticated recently with a
// strong factor (passkey or TOTP). It sits in slot 7 of the chain, after rate limiting and
// before validation, so malformed requests still pay the cost of validation; if preferred,
// it can be moved before validation by reordering the chain.
//
// The sensitive procedure list is part of the security surface: a procedure not listed is
// not protected. The list should be reviewed in code review.
func StepUp(clk clock.Clock, sensitive ...string) connect.ServerInterceptor {
	set := make(map[string]struct{}, len(sensitive))
	for _, p := range sensitive {
		set[p] = struct{}{}
	}
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			if _, ok := set[spec.Procedure]; !ok {
				return next(ctx, spec, stream)
			}
			p, ok := principal.From(ctx)
			if !ok {
				// This should not happen: the authentication interceptor is default-deny and
				// runs before this one. Treat as unauthenticated to fail closed.
				return connect.NewError(connect.CodeUnauthenticated, msgStepUpNoPrincipal)
			}
			if principal.RequiresReauthentication(p, clk.Now()) {
				return connect.NewError(connect.CodeUnauthenticated, msgStepUp)
			}
			return next(ctx, spec, stream)
		}
	}
}
