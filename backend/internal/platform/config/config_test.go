package config

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// A key long enough for the keyring (32 bytes of 0x01, base64). Fake: it protects nothing.
const testKey = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="

// hosted returns the settings every environment other than local needs: authentication is
// mandatory there. The extras override or add to them.
func hosted(env string, extra map[string]string) map[string]string {
	m := map[string]string{ //nolint:gosec // fake credentials
		"PITBOX_ENV":                env,
		"PITBOX_PUBLIC_URL":         "https://api.example.eu",
		"PITBOX_OIDC_ISSUER":        "https://auth.example.eu",
		"PITBOX_OIDC_CLIENT_ID":     "client-id",
		"PITBOX_OIDC_CLIENT_SECRET": "client-secret", //nolint:gosec // fake value
		"PITBOX_AUTH_KEYS":          "k1:" + testKey,
		"PITBOX_VALKEY_PASSWORD":    "valkey-password", //nolint:gosec // fake value
		"PITBOX_VALKEY_TLS":         "true",
		"PITBOX_DATABASE_URL":       "postgres://pitbox_api:db-password@db.example.eu:5432/pitbox?sslmode=verify-full", //nolint:gosec // fake value
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func localAuth(extra map[string]string) map[string]string {
	m := map[string]string{ //nolint:gosec // fake credentials
		"PITBOX_OIDC_CLIENT_ID":     "client-id",
		"PITBOX_OIDC_CLIENT_SECRET": "client-secret", //nolint:gosec // fake value
		"PITBOX_AUTH_KEYS":          "k1:" + testKey,
		"PITBOX_VALKEY_PASSWORD":    "valkey-password",                                         //nolint:gosec // fake value
		"PITBOX_DATABASE_URL":       "postgres://pitbox_api:db-password@127.0.0.1:5432/pitbox", //nolint:gosec // fake value
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func TestLoadFromDefaults(t *testing.T) {
	t.Parallel()

	c, err := LoadFrom(map[string]string{})
	if err != nil {
		t.Fatalf("defaults must be valid: %v", err)
	}
	if c.Env != EnvLocal || c.HTTPAddr != "127.0.0.1:8081" || c.LogLevel != "info" {
		t.Errorf("unexpected defaults: %+v", c)
	}
	if c.RPCMaxTimeout != 30*time.Second || c.RPCStreamMaxDuration != 55*time.Minute || c.ClientIPMode != "peer" || c.ClientIPHeader() != "" {
		t.Errorf("unexpected rpc defaults: %+v", c)
	}
	if c.ShutdownDrain != 0 || c.ShutdownTimeout != 6*time.Second {
		t.Errorf("unexpected shutdown defaults: drain=%s timeout=%s", c.ShutdownDrain, c.ShutdownTimeout)
	}
	if c.TracingEnabled() {
		t.Error("tracing must be off without an OTLP endpoint")
	}
	if c.SlogLevel() != slog.LevelInfo {
		t.Errorf("SlogLevel = %v, want info", c.SlogLevel())
	}
}

// Go note: a table-driven test keeps one slice of cases and runs each as a
// subtest, so a failure names the exact case and the others still run.
//
//nolint:gosec // the table has fake credentials in URLs: the test proves they are accepted or refused, never used
func TestLoadFrom(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     map[string]string
		wantErr string // substring; empty = must load
	}{
		{name: "production over https", env: hosted("production", map[string]string{
			"OTEL_EXPORTER_OTLP_ENDPOINT": "https://otlp.example.eu",
		})},
		{name: "local over plain http", env: map[string]string{
			"OTEL_EXPORTER_OTLP_ENDPOINT": "http://127.0.0.1:4318",
		}},
		{name: "unknown environment", env: map[string]string{"PITBOX_ENV": "prod"}, wantErr: "PITBOX_ENV"},
		{name: "environment is case sensitive", env: map[string]string{"PITBOX_ENV": "Production"}, wantErr: "PITBOX_ENV"},
		{name: "address without port", env: map[string]string{"PITBOX_HTTP_ADDR": "127.0.0.1"}, wantErr: "PITBOX_HTTP_ADDR"},
		{name: "address with port out of range", env: map[string]string{"PITBOX_HTTP_ADDR": "127.0.0.1:70000"}, wantErr: "PITBOX_HTTP_ADDR"},
		{name: "address with port zero (tests)", env: map[string]string{"PITBOX_HTTP_ADDR": "127.0.0.1:0"}},
		{name: "invalid log level", env: map[string]string{"PITBOX_LOG_LEVEL": "verbose"}, wantErr: "PITBOX_LOG_LEVEL"},
		{name: "unparsable duration", env: map[string]string{"PITBOX_SHUTDOWN_TIMEOUT": "soon"}, wantErr: "parse environment"},
		{name: "negative drain", env: map[string]string{"PITBOX_SHUTDOWN_DRAIN": "-1s"}, wantErr: "PITBOX_SHUTDOWN_DRAIN"},
		{name: "zero timeout", env: map[string]string{"PITBOX_SHUTDOWN_TIMEOUT": "0s"}, wantErr: "PITBOX_SHUTDOWN_TIMEOUT"},
		{name: "shutdown budget exceeded", env: map[string]string{
			"PITBOX_SHUTDOWN_DRAIN": "4s", "PITBOX_SHUTDOWN_TIMEOUT": "6s",
		}, wantErr: "shutdown budget"},
		{name: "shutdown budget exactly at the limit", env: map[string]string{
			"PITBOX_SHUTDOWN_DRAIN": "2s", "PITBOX_SHUTDOWN_TIMEOUT": "6s",
		}},
		{name: "rpc timeout lowered", env: map[string]string{"PITBOX_RPC_MAX_TIMEOUT": "10s"}},
		{name: "rpc timeout above the architecture ceiling", env: map[string]string{"PITBOX_RPC_MAX_TIMEOUT": "31s"}, wantErr: "PITBOX_RPC_MAX_TIMEOUT"},
		{name: "rpc timeout below one second", env: map[string]string{"PITBOX_RPC_MAX_TIMEOUT": "500ms"}, wantErr: "PITBOX_RPC_MAX_TIMEOUT"},
		{name: "stream duration at the platform ceiling", env: map[string]string{"PITBOX_RPC_STREAM_MAX_DURATION": "60m"}},
		{name: "stream duration above the platform ceiling", env: map[string]string{"PITBOX_RPC_STREAM_MAX_DURATION": "2h"}, wantErr: "PITBOX_RPC_STREAM_MAX_DURATION"},
		{name: "stream duration zero", env: map[string]string{"PITBOX_RPC_STREAM_MAX_DURATION": "0s"}, wantErr: "PITBOX_RPC_STREAM_MAX_DURATION"},
		{name: "client ip from a header behind cloudflare", env: hosted("production", map[string]string{
			"PITBOX_CLIENT_IP_MODE": "header:CF-Connecting-IP", "PITBOX_ORIGIN_CLOUDFLARE_ONLY": "true",
		})},
		{name: "client ip header refused without the origin guarantee", env: hosted("production", map[string]string{
			"PITBOX_CLIENT_IP_MODE": "header:CF-Connecting-IP",
		}), wantErr: "PITBOX_CLIENT_IP_MODE"},
		{name: "client ip header refused locally even if declared", env: map[string]string{
			"PITBOX_CLIENT_IP_MODE": "header:X-Forwarded-For", "PITBOX_ORIGIN_CLOUDFLARE_ONLY": "true",
		}, wantErr: "PITBOX_CLIENT_IP_MODE"},
		{name: "client ip mode unknown", env: map[string]string{"PITBOX_CLIENT_IP_MODE": "forwarded"}, wantErr: "PITBOX_CLIENT_IP_MODE"},
		{name: "client ip header name with illegal characters", env: hosted("production", map[string]string{
			"PITBOX_CLIENT_IP_MODE": "header:X Real\nIP", "PITBOX_ORIGIN_CLOUDFLARE_ONLY": "true",
		}), wantErr: "PITBOX_CLIENT_IP_MODE"},
		{name: "plain http endpoint refused in production", env: hosted("production", map[string]string{
			"OTEL_EXPORTER_OTLP_ENDPOINT": "http://otlp.example.eu",
		}), wantErr: "plain http"},
		{name: "plain http traces endpoint refused in staging", env: hosted("staging", map[string]string{
			"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "HTTP://otlp.example.eu",
		}), wantErr: "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"},
		// --- Authentication (F1-03, ADR 0013) ---
		{name: "local without authentication still loads (fails closed at runtime)", env: map[string]string{}},
		{name: "local fully configured", env: localAuth(nil)},
		{name: "local half configured", env: map[string]string{"PITBOX_OIDC_CLIENT_ID": "x"}, wantErr: "partly configured"},
		{name: "production without authentication", env: map[string]string{
			"PITBOX_ENV": "production", "PITBOX_PUBLIC_URL": "https://api.example.eu", "PITBOX_OIDC_ISSUER": "https://auth.example.eu", "PITBOX_VALKEY_TLS": "true",
		}, wantErr: "PITBOX_DATABASE_URL"},
		{name: "production without valkey tls", env: hosted("production", map[string]string{"PITBOX_VALKEY_TLS": "false"}), wantErr: "PITBOX_VALKEY_TLS"},
		{name: "production with the default http public url", env: hosted("production", map[string]string{"PITBOX_PUBLIC_URL": "http://localhost:8081"}), wantErr: "PITBOX_PUBLIC_URL"},
		{name: "production with an http issuer", env: hosted("production", map[string]string{"PITBOX_OIDC_ISSUER": "http://auth.example.eu"}), wantErr: "PITBOX_OIDC_ISSUER"},
		{name: "local issuer on a non-loopback host over http", env: map[string]string{"PITBOX_OIDC_ISSUER": "http://auth.example.eu"}, wantErr: "only allowed for localhost"},
		{name: "public url with a path", env: hosted("production", map[string]string{"PITBOX_PUBLIC_URL": "https://api.example.eu/app"}), wantErr: "without a path"},
		{name: "public url with credentials", env: hosted("production", map[string]string{"PITBOX_PUBLIC_URL": "https://user:pw@api.example.eu"}), wantErr: "credentials"}, //nolint:gosec // fake credentials: the test proves they are refused
		{name: "public url with a query", env: hosted("production", map[string]string{"PITBOX_PUBLIC_URL": "https://api.example.eu/?a=b"}), wantErr: "query"},
		{name: "public url that is not a url", env: hosted("production", map[string]string{"PITBOX_PUBLIC_URL": "api.example.eu"}), wantErr: "PITBOX_PUBLIC_URL"},
		{name: "issuer with a path is fine", env: hosted("production", map[string]string{"PITBOX_OIDC_ISSUER": "https://auth.example.eu/tenant"})},
		{name: "two allowed algorithms", env: hosted("production", map[string]string{"PITBOX_OIDC_ALLOWED_ALGS": "ES256,RS256"})},
		{name: "algorithm none", env: hosted("production", map[string]string{"PITBOX_OIDC_ALLOWED_ALGS": "none"}), wantErr: "PITBOX_OIDC_ALLOWED_ALGS"},
		{name: "algorithm HS256", env: hosted("production", map[string]string{"PITBOX_OIDC_ALLOWED_ALGS": "ES256,HS256"}), wantErr: "HS256"},
		{name: "algorithm listed twice", env: hosted("production", map[string]string{"PITBOX_OIDC_ALLOWED_ALGS": "ES256,ES256"}), wantErr: "twice"},
		{name: "valkey address without port", env: hosted("production", map[string]string{"PITBOX_VALKEY_ADDR": "valkey.example.eu"}), wantErr: "PITBOX_VALKEY_ADDR"},
		{name: "two keys (rotation)", env: hosted("production", map[string]string{"PITBOX_AUTH_KEYS": "k2:" + testKey + ",k1:" + testKey})},
		{name: "key too short", env: hosted("production", map[string]string{"PITBOX_AUTH_KEYS": "k1:AQEB"}), wantErr: "shorter than 32 bytes"},
		{name: "key not base64", env: hosted("production", map[string]string{"PITBOX_AUTH_KEYS": "k1:%%%"}), wantErr: "base64"},
		{name: "key without kid", env: hosted("production", map[string]string{"PITBOX_AUTH_KEYS": testKey}), wantErr: "<kid>:<base64>"},
		{name: "key with an upper case kid", env: hosted("production", map[string]string{"PITBOX_AUTH_KEYS": "K1:" + testKey}), wantErr: "<kid>:<base64>"},
		{name: "repeated kid", env: hosted("production", map[string]string{"PITBOX_AUTH_KEYS": "k1:" + testKey + ",k1:" + testKey}), wantErr: "repeats the kid"},
		{name: "database url without ssl outside local", env: hosted("production", map[string]string{"PITBOX_DATABASE_URL": "postgres://u:p@db.example.eu/pitbox"}), wantErr: "verify-full"}, //nolint:gosec // fake credentials
		{name: "database url with sslmode disable outside local", env: hosted("production", map[string]string{"PITBOX_DATABASE_URL": "postgres://u:p@db.example.eu/pitbox?sslmode=disable"}), wantErr: "verify-full"},
		{name: "database url with sslmode require does not verify the certificate", env: hosted("production", map[string]string{"PITBOX_DATABASE_URL": "postgres://u:p@db.example.eu/pitbox?sslmode=require"}), wantErr: "verify-full"},
		{name: "database url with sslmode verify-ca does not check the host name", env: hosted("production", map[string]string{"PITBOX_DATABASE_URL": "postgres://u:p@db.example.eu/pitbox?sslmode=verify-ca"}), wantErr: "verify-full"},
		{name: "database url with sslmode prefer", env: hosted("production", map[string]string{"PITBOX_DATABASE_URL": "postgres://u:p@db.example.eu/pitbox?sslmode=prefer"}), wantErr: "verify-full"},
		{name: "database url with sslmode verify-full", env: hosted("production", map[string]string{"PITBOX_DATABASE_URL": "postgres://u:p@db.example.eu/pitbox?sslmode=verify-full"})},
		{name: "staging is as strict as production", env: hosted("staging", map[string]string{"PITBOX_DATABASE_URL": "postgres://u:p@db.example.eu/pitbox?sslmode=require"}), wantErr: "verify-full"},
		{name: "database url not a url", env: hosted("production", map[string]string{"PITBOX_DATABASE_URL": "host=db user=u password=secret"}), wantErr: "PITBOX_DATABASE_URL"},
		{name: "database url with another scheme", env: hosted("production", map[string]string{"PITBOX_DATABASE_URL": "mysql://u:p@db.example.eu/x"}), wantErr: "PITBOX_DATABASE_URL"},
		{name: "database url without a user", env: hosted("production", map[string]string{"PITBOX_DATABASE_URL": "postgres://db.example.eu/x?sslmode=require"}), wantErr: "PITBOX_DATABASE_URL"},
		{name: "local database url may use plain connections", env: localAuth(map[string]string{"PITBOX_DATABASE_URL": "postgres://pitbox_api:p@127.0.0.1:5432/pitbox?sslmode=disable"})},
		{name: "session idle below a minute", env: hosted("production", map[string]string{"PITBOX_SESSION_IDLE_TIMEOUT": "30s"}), wantErr: "PITBOX_SESSION_IDLE_TIMEOUT"},
		{name: "session idle above absolute", env: hosted("production", map[string]string{"PITBOX_SESSION_IDLE_TIMEOUT": "100h", "PITBOX_SESSION_ABSOLUTE_TIMEOUT": "50h"}), wantErr: "must not exceed"},
		{name: "session absolute above the zitadel limit", env: hosted("production", map[string]string{"PITBOX_SESSION_ABSOLUTE_TIMEOUT": "721h"}), wantErr: "PITBOX_SESSION_ABSOLUTE_TIMEOUT"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := LoadFrom(tc.env)
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.wantErr != "" && err == nil:
				t.Fatalf("expected an error containing %q", tc.wantErr)
			case tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr):
				t.Fatalf("error %q does not contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateReportsAllProblems(t *testing.T) {
	t.Parallel()

	_, err := LoadFrom(map[string]string{"PITBOX_ENV": "nope", "PITBOX_LOG_LEVEL": "loud"})
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"PITBOX_ENV", "PITBOX_LOG_LEVEL"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %s", err, want)
		}
	}
}

// The log value is an allow-list: the OTLP endpoints (a URL can carry
// credentials) and any future field must not appear unless added on purpose.
func TestLogValueIsAnAllowList(t *testing.T) {
	t.Parallel()

	c, err := LoadFrom(map[string]string{ //nolint:gosec // fake credentials: the test proves they are never logged
		"OTEL_EXPORTER_OTLP_ENDPOINT": "http://user:s3cr3t-token@127.0.0.1:4318",
	})
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	logger.LogAttrs(context.Background(), slog.LevelInfo, "config", slog.Any("config", c))

	out := buf.String()
	for _, secret := range []string{"s3cr3t-token", "user:", "4318", "OTLP"} {
		if strings.Contains(out, secret) {
			t.Errorf("log output leaks %q: %s", secret, out)
		}
	}
	if !strings.Contains(out, `"tracing_enabled":true`) {
		t.Errorf("log output should say tracing is enabled: %s", out)
	}
}

func TestClientIPHeader(t *testing.T) {
	t.Parallel()

	c, err := LoadFrom(hosted("production", map[string]string{
		"PITBOX_CLIENT_IP_MODE": "header:CF-Connecting-IP", "PITBOX_ORIGIN_CLOUDFLARE_ONLY": "true",
	}))
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if got := c.ClientIPHeader(); got != "CF-Connecting-IP" {
		t.Errorf("ClientIPHeader = %q", got)
	}
}

func TestAuthDefaults(t *testing.T) {
	t.Parallel()

	c, err := LoadFrom(map[string]string{})
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if c.AuthConfigured() {
		t.Error("authentication must not count as configured without secrets")
	}
	if got := strings.Join(c.OIDCAllowedAlgs, ","); got != "ES256" {
		t.Errorf("default algorithms = %q, want ES256", got)
	}
	if c.SessionIdleTimeout != 168*time.Hour || c.SessionAbsoluteTimeout != 720*time.Hour {
		t.Errorf("session defaults: idle=%s absolute=%s", c.SessionIdleTimeout, c.SessionAbsoluteTimeout)
	}
	if c.ValkeyUsername != "pitbox_api" {
		t.Errorf("valkey user = %q, the API must not default to the default user", c.ValkeyUsername)
	}
}

func TestAuthKeyring(t *testing.T) {
	t.Parallel()

	c, err := LoadFrom(hosted("production", map[string]string{"PITBOX_AUTH_KEYS": "k2:" + testKey + ",k1:" + testKey}))
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	keys, err := c.AuthKeyring()
	if err != nil {
		t.Fatalf("AuthKeyring: %v", err)
	}
	if len(keys) != 2 || keys[0].ID != "k2" || keys[1].ID != "k1" || len(keys[0].Secret) != 32 {
		t.Errorf("unexpected keyring: %v", keys)
	}
}

// The secrets must stay out of every way a Config can be printed, and out of the errors
// about them. This test fails if a new secret field is logged or formatted on purpose or
// by accident.
//
//nolint:gosec // fake credentials in URLs: the test proves they never leak
func TestSecretsNeverLeak(t *testing.T) {
	t.Parallel()

	const (
		dbPassword   = "very-secret-database-value" //nolint:gosec // fake value
		clientSecret = "very-secret-client-value"   //nolint:gosec // fake value
		valkeyPass   = "very-secret-valkey-value"   //nolint:gosec // fake value
	)
	c, err := LoadFrom(hosted("production", map[string]string{
		"PITBOX_OIDC_CLIENT_SECRET": clientSecret, "PITBOX_VALKEY_PASSWORD": valkeyPass,
		"PITBOX_DATABASE_URL": "postgres://pitbox_api:" + dbPassword + "@db.example.eu/pitbox?sslmode=verify-full",
	}))
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).LogAttrs(context.Background(), slog.LevelInfo, "config", slog.Any("config", c))
	outputs := []string{
		buf.String(), fmt.Sprintf("%v", c), fmt.Sprintf("%+v", c), fmt.Sprintf("%#v", c), fmt.Sprint(c), c.String(),
	}
	for _, out := range outputs {
		for _, secret := range []string{clientSecret, valkeyPass, dbPassword, testKey, "client-id"} {
			if strings.Contains(out, secret) {
				t.Errorf("output leaks %q: %s", secret, out)
			}
		}
	}

	// The error for a bad database URL must not echo it (it holds the password).
	_, err = LoadFrom(hosted("production", map[string]string{"PITBOX_DATABASE_URL": "mysql://u:very-secret-looking-pw@db/x"}))
	if err == nil || strings.Contains(err.Error(), "very-secret-looking-pw") {
		t.Errorf("error for a bad database URL: %v", err)
	}

	// The error for a bad key must not echo the key.
	_, err = LoadFrom(hosted("production", map[string]string{"PITBOX_AUTH_KEYS": "k1:not-base64-but-secret-looking!!"}))
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "secret-looking") {
		t.Errorf("error echoes the key: %v", err)
	}
}

// The env library turns an empty variable into the default, so these two rules are only
// reachable through the struct: Validate still has to refuse them.
func TestValidateRefusesEmptyAlgorithmsAndValkeyUser(t *testing.T) {
	t.Parallel()

	c, err := LoadFrom(hosted("production", nil))
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	noAlgs := c
	noAlgs.OIDCAllowedAlgs = nil
	if err := noAlgs.Validate(); err == nil || !strings.Contains(err.Error(), "PITBOX_OIDC_ALLOWED_ALGS") {
		t.Errorf("no algorithms: %v", err)
	}
	noUser := c
	noUser.ValkeyUsername = ""
	if err := noUser.Validate(); err == nil || !strings.Contains(err.Error(), "PITBOX_VALKEY_USERNAME") {
		t.Errorf("no valkey user: %v", err)
	}
}
