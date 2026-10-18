package interceptors

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"connectrpc.com/connect/v2"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/apperr"
)

// Fixed, public texts. Anything else a client could read would have to be written
// here on purpose.
const (
	publicDeadlineMessage   = "deadline exceeded"
	publicCanceledMessage   = "canceled"
	publicNotFoundMessage   = "not found"
	publicInvalidMessage    = "invalid argument"
	publicPermissionMessage = "permission denied"
)

// Errors makes sure the only errors that leave the server are ones written for the
// client, and logs one line per RPC.
//
//   - A *connect.Error created by our code keeps its code and message (even if
//     wrapped with fmt.Errorf("...: %w", err): errors.As walks the chain).
//   - A remote *connect.Error (from a Connect client of ours) is not forwarded.
//   - apperr.ErrNotFound (a resource of another team, or one that does not exist) becomes
//     NOT_FOUND with a fixed message. It is the one place that maps it.
//   - apperr.ErrInvalidArgument (a client value the contract could not refuse) becomes
//     INVALID_ARGUMENT with a fixed message, never an INTERNAL error that the client could
//     provoke at will.
//   - Context errors keep their meaning (deadline, canceled).
//   - Everything else becomes CodeInternal with a fixed message; the cause is only
//     logged.
//
// The connect library already refuses to serialize a plain error (the client would
// see CodeUnknown); this interceptor turns that into the right code and logs it.
func Errors(logger *slog.Logger) connect.ServerInterceptor {
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			start := time.Now()
			err := publicError(next(ctx, spec, stream))
			logRPC(ctx, logger, spec, err, time.Since(start))
			return err
		}
	}
}

// publicError returns an error that is safe to send to a client.
func publicError(err error) error {
	if err == nil {
		return nil
	}
	var ce *connect.Error
	if errors.As(err, &ce) && !ce.IsRemote() {
		return err
	}
	switch {
	case errors.Is(err, apperr.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, publicNotFoundMessage).WithCause(err)
	case errors.Is(err, apperr.ErrInvalidArgument):
		return connect.NewError(connect.CodeInvalidArgument, publicInvalidMessage).WithCause(err)
	case errors.Is(err, apperr.ErrPermissionDenied):
		return connect.NewError(connect.CodePermissionDenied, publicPermissionMessage).WithCause(err)
	case errors.Is(err, context.DeadlineExceeded):
		return connect.NewError(connect.CodeDeadlineExceeded, publicDeadlineMessage).WithCause(err)
	case errors.Is(err, context.Canceled):
		return connect.NewError(connect.CodeCanceled, publicCanceledMessage).WithCause(err)
	}
	return connect.NewError(connect.CodeInternal, publicInternalMessage).WithCause(err)
}

// serverFault reports whether the code means the server, not the caller, failed.
// Only those errors have their cause logged: the cause of a client error can echo
// request data.
func serverFault(code connect.Code) bool {
	switch code {
	case connect.CodeInternal, connect.CodeUnknown, connect.CodeDataLoss,
		connect.CodeUnavailable, connect.CodeDeadlineExceeded:
		return true
	default:
		return false
	}
}

func logLevel(code connect.Code, failed bool) slog.Level {
	switch {
	case !failed:
		return slog.LevelDebug // traces and metrics cover the successes
	case code == connect.CodeDeadlineExceeded:
		return slog.LevelWarn
	case serverFault(code):
		return slog.LevelError
	default:
		return slog.LevelInfo // the client's mistake or choice (including Canceled)
	}
}

func logRPC(ctx context.Context, logger *slog.Logger, spec connect.Spec, err error, took time.Duration) {
	code := "ok"
	attrs := []slog.Attr{
		slog.String("procedure", spec.Procedure),
		slog.Float64("duration_ms", float64(took.Microseconds())/1000),
	}
	level := slog.LevelDebug
	if err != nil {
		c := connect.CodeOf(err)
		code = c.String()
		level = logLevel(c, true)
		if serverFault(c) {
			// connect.Error.Error() leaves the cause out (it is never sent to clients),
			// so it is logged on its own.
			attrs = append(attrs, slog.String("error", err.Error()))
			var ce *connect.Error
			if errors.As(err, &ce) && ce.Unwrap() != nil {
				attrs = append(attrs, slog.String("cause", ce.Unwrap().Error()))
			}
		}
	}
	attrs = append(attrs, slog.String("code", code))
	logger.LogAttrs(ctx, level, "rpc", attrs...)
}
