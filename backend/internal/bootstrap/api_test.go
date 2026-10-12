package bootstrap

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/config"
	platformlog "github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/log"
)

// syncBuffer lets the test read the log while the server goroutine writes it.
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

// The whole assembly: config → logger → telemetry (off) → server → clean shutdown.
func TestRunAPIStartsServesAndShutsDownCleanly(t *testing.T) {
	t.Parallel()

	cfg, err := config.LoadFrom(map[string]string{"PITBOX_HTTP_ADDR": "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	var logs syncBuffer
	logger := platformlog.New(&logs, cfg.SlogLevel())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addrCh := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() { done <- runAPI(ctx, cfg, logger, func(a net.Addr) { addrCh <- a }) }()

	var addr net.Addr
	select {
	case addr = <-addrCh:
	case err := <-done:
		t.Fatalf("runAPI returned early: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not start")
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr.String()+"/healthz", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	cancel() // SIGTERM
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runAPI = %v, want nil after a clean shutdown", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown did not finish")
	}

	out := logs.String()
	for _, want := range []string{`"msg":"starting"`, `"msg":"listening"`, `"msg":"shutdown started"`, `"msg":"shutdown complete"`} {
		if !strings.Contains(out, want) {
			t.Errorf("log is missing %s:\n%s", want, out)
		}
	}
}

// A busy port must fail fast and synchronously, not later inside a goroutine.
func TestRunAPIFailsWhenThePortIsBusy(t *testing.T) {
	t.Parallel()

	busy, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = busy.Close() }()

	cfg, err := config.LoadFrom(map[string]string{"PITBOX_HTTP_ADDR": busy.Addr().String()})
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	logger := platformlog.New(&syncBuffer{}, cfg.SlogLevel())

	err = runAPI(context.Background(), cfg, logger, nil)
	if err == nil || !strings.Contains(err.Error(), "listen on") {
		t.Errorf("runAPI = %v, want a listen error", err)
	}
}
