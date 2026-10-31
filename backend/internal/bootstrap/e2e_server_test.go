//go:build e2e

package bootstrap

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/valkey-io/valkey-go"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/auth"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/authz"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/config"
	platformlog "github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/log"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/testutil"
)

// The server the browser tests (F1-14) talk to. It is the real pitbox-api (runAPI: sync,
// signatures, RLS, OpenFGA, Valkey sessions) over throwaway containers, with synthetic data.
// Only the identity provider is missing: the sessions are created in the store the way the
// login would, so nothing in the production code knows about the tests. This file is a test
// file under a build tag, so it is compiled into no binary.
//
// Run it with scripts/e2e-server.sh; it serves until SIGTERM.

const (
	e2eEnableVar  = "PITBOX_E2E_SERVE"
	e2eStateVar   = "PITBOX_E2E_STATE"
	e2eAPIVar     = "PITBOX_E2E_API_ADDR"
	e2eControlVar = "PITBOX_E2E_CONTROL_ADDR"
	e2eOriginVar  = "PITBOX_E2E_ORIGIN"
)

type e2eUser struct {
	ID      string `json:"id"`
	Session string `json:"session"` // value of the __Host- cookie
}

// e2eState is what the Playwright side reads. The sessions are for a disposable environment
// with synthetic users; the file is written 0600 and is git-ignored.
type e2eState struct {
	TeamID       string  `json:"teamId"`
	OtherTeamID  string  `json:"otherTeamId"`
	Owner        e2eUser `json:"owner"`
	Pilot        e2eUser `json:"pilot"`
	Outsider     e2eUser `json:"outsider"`
	ControlURL   string  `json:"controlUrl"`
	ControlToken string  `json:"controlToken"`
}

// e2eQueries is the whole read surface of the control port: fixed statements, no SQL from
// the outside. They return metadata and identifiers, never tokens, e-mails or keys.
var e2eQueries = map[string]string{
	"setups":     `SELECT coalesce(json_agg(t ORDER BY t.created_at), '[]') FROM (SELECT id, team_id, name, created_at FROM setup.setup) t`,
	"versions":   `SELECT coalesce(json_agg(t ORDER BY t.setup_id, t.version), '[]') FROM (SELECT setup_id, version, created_by FROM setup.setup_version) t`,
	"feedback":   `SELECT coalesce(json_agg(t ORDER BY t.created_at), '[]') FROM (SELECT id, setup_id, version, author_id, created_at FROM setup.setup_feedback) t`,
	"cars":       `SELECT coalesce(json_agg(t ORDER BY t.created_at), '[]') FROM (SELECT id, team_id, nickname, created_at FROM garage.car) t`,
	"operations": `SELECT coalesce(json_agg(t ORDER BY t.received_at), '[]') FROM (SELECT op_id, entity, entity_id, kind, status, (device_signature IS NOT NULL) AS signed, received_at FROM sync.operation) t`,
	"conflicts":  `SELECT coalesce(json_agg(t), '[]') FROM (SELECT id, entity, entity_id, resolved_at IS NOT NULL AS resolved FROM sync.conflict) t`,
	"devices":    `SELECT coalesce(json_agg(t), '[]') FROM (SELECT id, user_id, revoked_at IS NOT NULL AS revoked FROM identity.device_key) t`,
}

func TestE2EServer(t *testing.T) {
	if os.Getenv(e2eEnableVar) != "1" {
		t.Skipf("%s is not set: this test only serves the browser tests", e2eEnableVar)
	}
	statePath := os.Getenv(e2eStateVar)
	if statePath == "" {
		t.Fatalf("%s must say where to write the state file", e2eStateVar)
	}
	apiAddr := envOr(e2eAPIVar, "127.0.0.1:8081")
	controlAddr := envOr(e2eControlVar, "127.0.0.1:8089")
	origin := envOr(e2eOriginVar, "http://localhost:4173")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	vk := testutil.StartValkey(t)
	pg := testutil.StartPostgres(t)
	ofga := testutil.StartOpenFGA(t)
	env := map[string]string{ //nolint:gosec // throwaway containers, random secrets
		"PITBOX_HTTP_ADDR":             apiAddr,
		"PITBOX_PUBLIC_URL":            origin, // the browser's Origin: the interceptor compares it
		"PITBOX_OIDC_ISSUER":           "http://127.0.0.1:1",
		"PITBOX_OIDC_CLIENT_ID":        "e2e",
		"PITBOX_OIDC_CLIENT_SECRET":    randomKey(t),
		"PITBOX_AUTH_KEYS":             "k1:" + randomKey(t),
		"PITBOX_SYNC_PASS_KEYS":        "k1:" + randomKey(t),
		"PITBOX_VALKEY_ADDR":           vk.Addr,
		"PITBOX_VALKEY_USERNAME":       vk.Username,
		"PITBOX_VALKEY_PASSWORD":       vk.Password,
		"PITBOX_DATABASE_URL":          pg.URL,
		"PITBOX_OPENFGA_ADDR":          ofga.HTTPAddr,
		"PITBOX_OPENFGA_STORE_ID":      ofga.StoreID,
		"PITBOX_OPENFGA_MODEL_ID":      ofga.ModelID,
		"PITBOX_OPENFGA_PRESHARED_KEY": ofga.PresharedKey,
	}
	cfg, err := config.LoadFrom(env)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}

	logger := platformlog.New(os.Stderr, cfg.SlogLevel())
	listening := make(chan struct{})
	done := make(chan error, 1)
	apiCtx, cancelAPI := context.WithCancel(ctx)
	defer cancelAPI()
	go func() { done <- runAPI(apiCtx, cfg, logger, func(net.Addr) { close(listening) }) }()
	select {
	case <-listening:
	case err := <-done:
		t.Fatalf("runAPI returned early: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("the API did not start")
	}

	state := seedE2E(ctx, t, cfg, pg)

	control, token := serveControl(t, controlAddr, pg)
	state.ControlURL, state.ControlToken = "http://"+control, token
	writeState(t, statePath, state)
	t.Logf("e2e server ready: api %s, control %s", apiAddr, control)

	select {
	case <-ctx.Done():
	case err := <-done:
		t.Fatalf("the API stopped: %v", err)
	}
	cancelAPI()
	<-done
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func randomKey(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random: %v", err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// seedE2E creates two teams with synthetic people and a session for each person.
func seedE2E(ctx context.Context, t *testing.T, cfg config.Config, pg testutil.Postgres) e2eState {
	t.Helper()

	newUser := func(label string) string {
		id := uuid.Must(uuid.NewV7()).String()
		pg.Exec(t, fmt.Sprintf(`INSERT INTO identity.user_account (id, idp_subject, email, display_name) VALUES ('%s','e2e-%s','%s@e2e.invalid','%s')`,
			id, id, label, label))
		return id
	}
	newTeam := func(name, owner string) string {
		id := uuid.Must(uuid.NewV7()).String()
		pg.Exec(t, fmt.Sprintf(`INSERT INTO identity.team (id, name, created_by) VALUES ('%s','%s','%s')`, id, name, owner))
		return id
	}
	ana, rui, beto := newUser("ana"), newUser("rui"), newUser("beto")
	team, other := newTeam("Equipa E2E", ana), newTeam("Equipa Alheia", beto)

	fga, err := authz.New(ctx, openFGAConfig(cfg.OpenFGAAddr, cfg.OpenFGAStoreID, cfg.OpenFGAModelID,
		cfg.OpenFGAPresharedKey, cfg.OpenFGATLSEnabled, cfg.OpenFGATimeout))
	if err != nil {
		t.Fatalf("openfga: %v", err)
	}
	member := func(teamID, userID, role string) {
		pg.Exec(t, fmt.Sprintf(`INSERT INTO identity.membership (team_id, user_id, role) VALUES ('%s','%s','%s')`, teamID, userID, role))
		if err := fga.SetTeamRole(ctx, "user:"+userID, "team:"+teamID, &role); err != nil {
			t.Fatalf("SetTeamRole(%s): %v", role, err)
		}
	}
	member(team, ana, "owner")
	member(team, rui, "pilot")
	member(other, beto, "owner")

	vk, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{cfg.ValkeyAddr}, Username: cfg.ValkeyUsername, Password: cfg.ValkeyPassword, DisableCache: true,
	})
	if err != nil {
		t.Fatalf("valkey: %v", err)
	}
	t.Cleanup(vk.Close)
	ring, err := cfg.AuthKeyring()
	if err != nil {
		t.Fatalf("AuthKeyring: %v", err)
	}
	kr, err := auth.NewKeyring(ring)
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	sealer, err := kr.Sealer(auth.PurposeSessionToken)
	if err != nil {
		t.Fatalf("Sealer: %v", err)
	}
	store := auth.NewStore(vk, sealer, cfg.SessionIdleTimeout, cfg.SessionAbsoluteTimeout, time.Now)
	session := func(userID, label string) e2eUser {
		now := time.Now()
		tok, err := store.Create(ctx, auth.Session{
			UserID: userID, Subject: "e2e-" + label, AMR: []string{"pwd", "mfa"}, AuthTime: now,
			AccessExpires: now.Add(11 * time.Hour), // no refresh: there is no identity provider
			RefreshToken:  "e2e-refresh-" + label, IDToken: "e2e-id-" + label,
		})
		if err != nil {
			t.Fatalf("session %s: %v", label, err)
		}
		return e2eUser{ID: userID, Session: tok}
	}
	return e2eState{
		TeamID: team, OtherTeamID: other,
		Owner: session(ana, "ana"), Pilot: session(rui, "rui"), Outsider: session(beto, "beto"),
	}
}

// serveControl is the read-only port the browser tests use to look at the server's data. It
// listens on loopback only, wants a random bearer token and runs fixed queries.
func serveControl(t *testing.T, addr string, pg testutil.Postgres) (string, string) {
	t.Helper()
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("random: %v", err)
	}
	token := hex.EncodeToString(raw)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /data/{name}", func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare(got, []byte("Bearer "+token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		query, ok := e2eQueries[r.PathValue("name")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		out, err := pg.TryExec(query)
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(out))
	})
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", addr)
	if err != nil {
		t.Fatalf("control listen: %v", err)
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().String(), token
}

func writeState(t *testing.T, path string, s e2eState) {
	t.Helper()
	body, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	_ = os.Remove(path)                                     //nolint:gosec // the path is the test runner's own argument (PITBOX_E2E_STATE)
	if err := os.WriteFile(path, body, 0o600); err != nil { //nolint:gosec // same: a path chosen by the test runner
		t.Fatalf("write state: %v", err)
	}
}
