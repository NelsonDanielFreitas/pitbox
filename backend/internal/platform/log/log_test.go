package log

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func decode(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("log line is not JSON: %v: %q", err, buf.String())
	}
	return m
}

func ctxWithSpan(t *testing.T) (context.Context, trace.SpanContext) {
	t.Helper()
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:     trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
		TraceFlags: trace.FlagsSampled,
	})
	return trace.ContextWithSpanContext(context.Background(), sc), sc
}

func TestRecordGetsTraceIDs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := New(&buf, slog.LevelInfo)
	ctx, sc := ctxWithSpan(t)

	logger.InfoContext(ctx, "hello")

	m := decode(t, &buf)
	if m["trace_id"] != sc.TraceID().String() || m["span_id"] != sc.SpanID().String() {
		t.Errorf("trace ids missing or wrong: %v", m)
	}
}

func TestRecordWithoutSpanHasNoTraceIDs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	New(&buf, slog.LevelInfo).InfoContext(context.Background(), "hello")

	m := decode(t, &buf)
	if _, ok := m["trace_id"]; ok {
		t.Errorf("trace_id must be absent without a valid span: %v", m)
	}
}

// A derived logger must keep the decoration (see the Go note on traceHandler).
func TestDerivedLoggersKeepTraceIDs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	derived := New(&buf, slog.LevelInfo).With("component", "api").WithGroup("g")
	ctx, sc := ctxWithSpan(t)

	derived.InfoContext(ctx, "hello", "k", "v")

	m := decode(t, &buf)
	g, _ := m["g"].(map[string]any)
	if g["trace_id"] != sc.TraceID().String() {
		t.Errorf("derived logger lost the trace id: %v", m)
	}
	if m["component"] != "api" {
		t.Errorf("With attributes lost: %v", m)
	}
}

func TestLevelIsRespected(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := New(&buf, slog.LevelWarn)
	logger.InfoContext(context.Background(), "dropped")
	if buf.Len() != 0 {
		t.Errorf("info record must be dropped at warn level: %q", buf.String())
	}
	logger.WarnContext(context.Background(), "kept")
	if buf.Len() == 0 {
		t.Error("warn record must be written")
	}
}
