package interceptors

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"connectrpc.com/connect/v2"
)

// syncBuffer lets a test read the log while a server goroutine writes it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// records returns the JSON log lines written so far.
func (b *syncBuffer) records(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(b.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line is not JSON: %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

func newLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// nopStream is a ServerStream for tests that only exercise interceptor logic.
type nopStream struct{}

func (nopStream) Receive(any) error  { return io.EOF }
func (nopStream) SendHeaders() error { return nil }
func (nopStream) Send(any) error     { return nil }

// call runs one RPC through a server that has only the given interceptor.
func call(t *testing.T, i connect.ServerInterceptor, spec connect.Spec, info *connect.CallInfo, h connect.ServerFunc) error {
	t.Helper()
	server := connect.NewServer(i)
	server.Register(connect.Method{Spec: spec, Handler: h})
	if info == nil {
		info = &connect.CallInfo{}
	}
	return server.Call(context.Background(), spec.Procedure, info, nopStream{})
}

var unarySpec = connect.Spec{StreamType: connect.StreamTypeUnary, Procedure: "/pitbox.test.v1.TestService/Do"}
