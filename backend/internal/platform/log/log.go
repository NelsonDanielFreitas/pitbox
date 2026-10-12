// Package log builds the process logger: log/slog with JSON output on stdout, and
// the trace_id and span_id of the active span on every record that has one.
//
// Never log tokens, cookies, passwords, emails, setup values or request bodies
// (see .claude/rules/go-backend.md). Use the *Context methods so the trace ids
// are attached: sloglint enforces it.
package log

import (
	"context"
	"io"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// New returns a JSON logger that writes to w at the given level.
func New(w io.Writer, level slog.Level) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	return slog.New(traceHandler{next: h})
}

// traceHandler decorates another slog.Handler with the trace context.
//
// Go note: slog.Handler is an interface, so decorating it is just a struct that
// wraps another handler and forwards the calls. WithAttrs and WithGroup must
// re-wrap the result, otherwise a logger derived with logger.With(...) would
// silently lose the decoration.
type traceHandler struct{ next slog.Handler }

func (h traceHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h traceHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.next.Handle(ctx, r)
}

func (h traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return traceHandler{next: h.next.WithAttrs(attrs)}
}

func (h traceHandler) WithGroup(name string) slog.Handler {
	return traceHandler{next: h.next.WithGroup(name)}
}
