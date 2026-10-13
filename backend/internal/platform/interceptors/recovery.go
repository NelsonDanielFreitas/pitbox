package interceptors

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"connectrpc.com/connect/v2"
)

const (
	// publicInternalMessage is the only text a client ever sees for an unexpected
	// failure. The cause stays in the log.
	publicInternalMessage = "internal error"

	maxPanicLogBytes = 512
	maxStackLogBytes = 8 << 10
)

// Recovery turns a panic in the handler (or in a later interceptor) into a
// CodeInternal error with a fixed message, and logs the panic with its stack.
//
// Go note: recover only works inside a deferred function, and only for panics in
// the same goroutine. A goroutine the handler starts on its own is not protected:
// it needs its own deferred recover.
func Recovery(logger *slog.Logger) connect.ServerInterceptor {
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) (err error) {
			defer func() {
				r := recover()
				if r == nil {
					return
				}
				// http.ErrAbortHandler is how a handler asks net/http to abort the response
				// without logging; swallowing it would change that contract.
				if e, ok := r.(error); ok && errors.Is(e, http.ErrAbortHandler) {
					panic(r) //nolint:forbidigo // re-panic: net/http expects ErrAbortHandler to propagate
				}
				logger.ErrorContext(ctx, "panic in rpc handler",
					slog.String("procedure", spec.Procedure),
					slog.String("panic", truncate(fmt.Sprint(r), maxPanicLogBytes)),
					slog.String("stack", truncate(string(debug.Stack()), maxStackLogBytes)),
				)
				err = connect.NewError(connect.CodeInternal, publicInternalMessage).
					WithCause(fmt.Errorf("panic: %v", r))
			}()
			return next(ctx, spec, stream)
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}
