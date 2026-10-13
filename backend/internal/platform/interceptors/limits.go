package interceptors

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect/v2"
)

// Limits gives every RPC a deadline: maxUnary for unary calls and maxStream for
// streaming ones. A deadline the client already sent is kept when it is shorter.
//
// Go note: context.WithTimeout never extends a deadline. If the parent context
// ends in 2 s, WithTimeout(ctx, 30*time.Second) still ends in 2 s, so there is no
// need to compare deadlines by hand. Always call the returned cancel (defer), or
// the timer stays alive until the deadline.
//
// The deadline reaches code that watches the context (the handler, the database).
// A client that sends its body slowly is stopped by the read deadline set in
// httpserver.MountRPC, because blocking in the transport does not look at it.
func Limits(maxUnary, maxStream time.Duration) (connect.ServerInterceptor, error) {
	if maxUnary <= 0 || maxStream <= 0 {
		return nil, errors.New("interceptors: Limits needs positive durations")
	}
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			limit := maxUnary
			if spec.StreamType != connect.StreamTypeUnary {
				limit = maxStream
			}
			ctx, cancel := context.WithTimeout(ctx, limit)
			defer cancel()
			return next(ctx, spec, stream)
		}
	}, nil
}
