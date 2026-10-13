package auth

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect/v2"
)

// Fixed public messages (the archtest only admits literals or constants).
const (
	msgUnauthenticated = "unauthenticated"
	msgCrossOrigin     = "cross-origin request refused"
	msgAuthUnavailable = "authentication temporarily unavailable"
)

// Interceptor authenticates every RPC from the session cookie and puts the Principal in the
// context. It is slot 5 of the chain (architecture §4.4): before any payload work and
// before rate limiting, which needs the user.
//
// An RPC is refused unless its procedure is in public (default deny): a new service is
// closed until someone opens it on purpose, and the list is reviewed by the archtest.
//
// Browser protections for cookie-authenticated requests: a request from another origin or
// another site (Origin that is not ours, or Sec-Fetch-Site other than same-origin/none) is
// refused before the session is looked at. SameSite=Strict already keeps the cookie away
// from cross-site requests; this also covers same-site siblings (a compromised subdomain)
// and old browsers.
func Interceptor(a *Authenticator, origin string, public ...string) connect.ServerInterceptor {
	open := make(map[string]struct{}, len(public))
	for _, p := range public {
		open[p] = struct{}{}
	}
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			if _, ok := open[spec.Procedure]; ok {
				return next(ctx, spec, stream)
			}
			info, ok := connect.CallInfoForServerContext(ctx)
			if !ok {
				return connect.NewError(connect.CodeUnauthenticated, msgUnauthenticated)
			}
			hdr := info.RequestHeader()
			if o := hdr.Get("Origin"); o != "" && o != origin {
				return connect.NewError(connect.CodePermissionDenied, msgCrossOrigin)
			}
			switch hdr.Get("Sec-Fetch-Site") {
			case "", "same-origin", "none":
			default:
				return connect.NewError(connect.CodePermissionDenied, msgCrossOrigin)
			}

			h := http.Header{"Cookie": hdr.Values("Cookie")}
			p, err := a.Authenticate(ctx, SessionTokenFromHeader(h))
			switch {
			case errors.Is(err, ErrUnauthenticated):
				return connect.NewError(connect.CodeUnauthenticated, msgUnauthenticated)
			case err != nil:
				return connect.NewError(connect.CodeUnavailable, msgAuthUnavailable).WithCause(err)
			}
			return next(WithPrincipal(ctx, p), spec, stream)
		}
	}
}

// DenyAll refuses every RPC as unauthenticated. It is what the chain uses when
// authentication is not configured (local development only; Validate makes it mandatory
// elsewhere): the service stays up, answers its health probes, and serves nothing else.
func DenyAll() connect.ServerInterceptor {
	return func(connect.ServerFunc) connect.ServerFunc {
		return func(context.Context, connect.Spec, connect.ServerStream) error {
			return connect.NewError(connect.CodeUnauthenticated, msgUnauthenticated)
		}
	}
}
