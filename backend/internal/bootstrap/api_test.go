package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/config"
	platformlog "github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/log"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/testutil"
)

func localEnv(t *testing.T) map[string]string {
	t.Helper()
	vk := testutil.StartValkey(t)
	pg := testutil.StartPostgres(t)
	ofga := testutil.StartOpenFGA(t)
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("random: %v", err)
	}
	return map[string]string{ //nolint:gosec // fake credentials
		"PITBOX_PUBLIC_URL":            "http://localhost:8081",
		"PITBOX_OIDC_ISSUER":           "http://127.0.0.1:1",
		"PITBOX_OIDC_CLIENT_ID":        "client",
		"PITBOX_OIDC_CLIENT_SECRET":    "secret",
		"PITBOX_AUTH_KEYS":             "k1:" + base64.StdEncoding.EncodeToString(key),
		"PITBOX_VALKEY_ADDR":           vk.Addr,
		"PITBOX_VALKEY_USERNAME":       vk.Username,
		"PITBOX_VALKEY_PASSWORD":       vk.Password,
		"PITBOX_DATABASE_URL":          pg.URL,
		"PITBOX_OPENFGA_ADDR":          ofga.HTTPAddr,
		"PITBOX_OPENFGA_STORE_ID":      ofga.StoreID,
		"PITBOX_OPENFGA_MODEL_ID":      ofga.ModelID,
		"PITBOX_OPENFGA_PRESHARED_KEY": ofga.PresharedKey,
	}
}

// The whole assembly: config → logger → telemetry (off) → server → clean shutdown.
func TestRunAPIStartsServesAndShutsDownCleanly(t *testing.T) {
	t.Parallel()

	cfg, err := config.LoadFrom(localEnv(t))
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

	env := localEnv(t)
	env["PITBOX_HTTP_ADDR"] = busy.Addr().String()
	cfg, err := config.LoadFrom(env)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	logger := platformlog.New(&syncBuffer{}, cfg.SlogLevel())

	err = runAPI(context.Background(), cfg, logger, nil)
	if err == nil || !strings.Contains(err.Error(), "listen on") {
		t.Errorf("runAPI = %v, want a listen error", err)
	}
}

// The sync service is registered only when the pass key is configured, and when it is, it sits
// behind authentication like every other service: an anonymous call never reaches it.
func TestSyncServiceNeedsTheKeyAndAuthentication(t *testing.T) {
	t.Parallel()

	const pull = "/pitbox.sync.v1.SyncService/Pull"
	for _, tc := range []struct {
		name     string
		withKey  bool
		wantHTTP int
	}{
		{"configured: refused without a session", true, http.StatusUnauthorized},
		{"not configured: not served", false, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := localEnv(t)
			if tc.withKey {
				env["PITBOX_SYNC_PASS_KEYS"] = "k1:AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
			}
			cfg, err := config.LoadFrom(env)
			if err != nil {
				t.Fatalf("LoadFrom: %v", err)
			}
			var logs syncBuffer
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			addrCh := make(chan net.Addr, 1)
			done := make(chan error, 1)
			go func() {
				done <- runAPI(ctx, cfg, platformlog.New(&logs, cfg.SlogLevel()), func(a net.Addr) { addrCh <- a })
			}()
			var addr net.Addr
			select {
			case addr = <-addrCh:
			case err := <-done:
				t.Fatalf("runAPI returned early: %v", err)
			case <-time.After(10 * time.Second):
				t.Fatal("server did not start")
			}

			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr.String()+pull, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Connect-Protocol-Version", "1")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("POST %s: %v", pull, err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != tc.wantHTTP {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.wantHTTP)
			}
			if !tc.withKey && !strings.Contains(logs.String(), "sync service disabled") {
				t.Error("the log does not say that sync is disabled")
			}
			if strings.Contains(logs.String(), "AQEBAQEB") {
				t.Error("the pass key leaked into the log")
			}
		})
	}
}
