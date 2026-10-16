package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"github.com/valkey-io/valkey-go"
	"go.opentelemetry.io/otel/trace/noop"

	identityv1 "github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/identity/v1"
	"github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/identity/v1/identityv1connect"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/auth"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/config"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/httpserver"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/testutil"
)

// headerTransport adds fixed headers to every request, the way a browser adds its cookie.
type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (h headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for k, v := range h.headers {
		r.Header.Set(k, v)
	}
	return h.base.RoundTrip(r)
}

// Authentication wired for real (the same newAuthStack as the binary): real Valkey with the
// compose ACL, real PostgreSQL. The identity provider is not reachable (a closed port), which
// is the point of the lazy discovery: the process works, /auth/login says 503, and a session
// that exists is honoured. Sessions are created straight in the store, as the login does.
func TestAuthStackWiring(t *testing.T) {
	t.Parallel()

	vk := testutil.StartValkey(t)
	pg := testutil.StartPostgres(t)
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("random: %v", err)
	}
	keys := "k1:" + base64.StdEncoding.EncodeToString(key)
	cfg, err := config.LoadFrom(map[string]string{
		"PITBOX_PUBLIC_URL": "http://localhost:8081", "PITBOX_OIDC_ISSUER": "http://127.0.0.1:1",
		"PITBOX_OIDC_CLIENT_ID": "client", "PITBOX_OIDC_CLIENT_SECRET": "secret", //nolint:gosec // fake value
		"PITBOX_AUTH_KEYS": keys, "PITBOX_VALKEY_ADDR": vk.Addr, "PITBOX_VALKEY_USERNAME": vk.Username,
		"PITBOX_VALKEY_PASSWORD": vk.Password, "PITBOX_DATABASE_URL": pg.URL,
	})
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if !cfg.AuthConfigured() {
		t.Fatal("the test configuration must configure authentication")
	}

	logs := &syncBuffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	vkClient, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{vk.Addr}, Username: vk.Username, Password: vk.Password, DisableCache: true})
	if err != nil {
		t.Fatalf("valkey: %v", err)
	}
	t.Cleanup(vkClient.Close)

	stack, err := newAuthStack(context.Background(), cfg, logger, vkClient)
	if err != nil {
		t.Fatalf("newAuthStack: %v", err)
	}
	t.Cleanup(stack.close)

	var seen *auth.Principal
	teams := &fakeTeams{create: func(ctx context.Context, _ *identityv1.CreateTeamRequest) (*identityv1.CreateTeamResponse, error) {
		if p, ok := auth.PrincipalFrom(ctx); ok {
			seen = &p
		}
		return &identityv1.CreateTeamResponse{}, nil
	}}
	api, err := newRPCServer(cfg, logger, noop.NewTracerProvider(), stack.interceptor, noOpRateLimit(), func(s *connect.Server) {
		identityv1connect.RegisterTeamServiceHandler(s, teams)
	})
	if err != nil {
		t.Fatalf("newRPCServer: %v", err)
	}
	srv, err := httpserver.New(httpserver.Options{
		Logger: logger, TracerProvider: noop.NewTracerProvider(), ShutdownTimeout: time.Second,
		Routes: func(mux *http.ServeMux) error { stack.mount(mux); return mountRPC(mux, cfg, api, nil) },
	})
	if err != nil {
		t.Fatalf("httpserver.New: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	// A session, created the way the login creates it.
	keyring, _ := cfg.AuthKeyring()
	kr, _ := auth.NewKeyring(keyring)
	sealer, _ := kr.Sealer(auth.PurposeSessionToken)
	store := auth.NewStore(vkClient, sealer, cfg.SessionIdleTimeout, cfg.SessionAbsoluteTimeout, time.Now)
	token, err := store.Create(context.Background(), auth.Session{
		UserID: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60", Subject: "sub-ana", AMR: []string{"pwd"},
		AuthTime: time.Now(), AccessExpires: time.Now().Add(10 * time.Minute), RefreshToken: "rt", IDToken: "idt",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	rpc := func(headers map[string]string) (*identityv1.CreateTeamResponse, error) {
		c := &http.Client{Transport: headerTransport{base: ts.Client().Transport, headers: headers}}
		return identityv1connect.NewTeamServiceClient(connect.NewClient(connecthttp.NewTransport(c, ts.URL))).CreateTeam(context.Background(), validRequest())
	}

	t.Run("without a session the service is not reached", func(t *testing.T) {
		before := teams.calls.Load()
		_, err := rpc(nil)
		code, msg := codeAndMessage(t, err)
		if code != connect.CodeUnauthenticated || msg != "unauthenticated" || teams.calls.Load() != before {
			t.Errorf("code %v, message %q, calls %d", code, msg, teams.calls.Load()-before)
		}
	})
	t.Run("with the session cookie the service sees the principal", func(t *testing.T) {
		if _, err := rpc(map[string]string{"Cookie": auth.SessionCookieName + "=" + token}); err != nil {
			t.Fatalf("CreateTeam: %v", err)
		}
		if seen == nil || seen.UserID != "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60" || seen.Subject != "sub-ana" {
			t.Errorf("principal = %+v", seen)
		}
	})
	t.Run("a foreign origin is refused even with a valid session", func(t *testing.T) {
		before := teams.calls.Load()
		_, err := rpc(map[string]string{"Cookie": auth.SessionCookieName + "=" + token, "Origin": "https://evil.example"})
		if code, _ := codeAndMessage(t, err); code != connect.CodePermissionDenied || teams.calls.Load() != before {
			t.Errorf("code %v, calls %d", code, teams.calls.Load()-before)
		}
	})
	t.Run("login says 503 while the identity provider is unreachable, and the logs stay clean", func(t *testing.T) {
		resp, err := http.Get(ts.URL + auth.LoginPath) //nolint:noctx,bodyclose // closed below; plain test request
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("status %d, want 503", resp.StatusCode)
		}
		for _, secret := range []string{token, keys, vk.Password, "secret"} {
			if strings.Contains(logs.String(), secret) {
				t.Errorf("the log contains a secret")
			}
		}
	})
}

// With no authentication configured (allowed only in local) nothing is served.
func TestUnconfiguredAuthenticationRefusesEverything(t *testing.T) {
	t.Parallel()

	cfg, err := config.LoadFrom(map[string]string{})
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	stack, err := newAuthStack(context.Background(), cfg, slog.New(slog.NewJSONHandler(&syncBuffer{}, nil)), nil)
	if err != nil {
		t.Fatalf("newAuthStack: %v", err)
	}
	mux := http.NewServeMux()
	stack.mount(mux)
	for _, path := range []string{"/auth/login", "/auth/callback", "/auth/logout", "/auth/anything/else"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil))
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: status %d, want 503", path, w.Code)
		}
	}
	h := newHarnessWithAuth(t, map[string]string{}, &fakeTeams{}, stack.interceptor)
	if _, err := h.client.CreateTeam(context.Background(), validRequest()); err == nil {
		t.Fatal("an RPC was served without authentication")
	} else if code, _ := codeAndMessage(t, err); code != connect.CodeUnauthenticated {
		t.Errorf("code %v", code)
	}
}
