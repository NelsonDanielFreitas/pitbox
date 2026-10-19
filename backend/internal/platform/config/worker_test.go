package config

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// workerHosted is the minimum a production worker needs: database, OpenFGA and
// telemetry. It deliberately has no PITBOX_AUTH_KEYS, OIDC or Valkey settings.
func workerHosted(extra map[string]string) map[string]string {
	m := map[string]string{ //nolint:gosec // fake credentials
		"PITBOX_ENV":                   "production",
		"PITBOX_DATABASE_URL":          "postgres://pitbox_api:db-password@db.example.eu:5432/pitbox?sslmode=verify-full", //nolint:gosec // fake value
		"PITBOX_OPENFGA_ADDR":          "openfga.example.eu:8080",
		"PITBOX_OPENFGA_TLS":           "true",
		"PITBOX_OPENFGA_PRESHARED_KEY": "openfga-psk", //nolint:gosec // fake value
		"PITBOX_OPENFGA_STORE_ID":      "01JABCD0123456789ABCDEFGH",
		"OTEL_EXPORTER_OTLP_ENDPOINT":  "https://otlp.example.eu",
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func TestProductionWorkerNeedsNoAuthenticationSecrets(t *testing.T) {
	t.Parallel()

	w, err := LoadWorkerFrom(workerHosted(nil))
	if err != nil {
		t.Fatalf("the worker must load without authentication settings: %v", err)
	}
	if w.Env != EnvProduction || w.OutboxLease != 30*time.Second || w.OutboxBatchSize != 20 {
		t.Errorf("unexpected worker config: %+v", w)
	}
	if !w.TracingEnabled() || w.SlogLevel() != slog.LevelInfo {
		t.Errorf("unexpected telemetry/log config: %+v", w)
	}

	// The same environment is not enough for the API: authentication is mandatory
	// there, so the API must keep failing closed.
	if _, err := LoadFrom(workerHosted(nil)); err == nil {
		t.Fatal("the API must refuse to load without authentication settings")
	}
}

func TestWorkerConfigLogValueHasNoAPISecrets(t *testing.T) {
	t.Parallel()

	w, err := LoadWorkerFrom(workerHosted(nil))
	if err != nil {
		t.Fatalf("LoadWorkerFrom: %v", err)
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	logger.LogAttrs(context.Background(), slog.LevelInfo, "config", slog.Any("config", w))

	out := buf.String()
	for _, forbidden := range []string{
		"auth_configured", "public_url", "oidc_issuer", "oidc_client", "auth_keys",
		"valkey", "session_idle", "session_absolute", "rl_user", "rl_team", "rl_public",
		"db-password", "openfga-psk",
	} {
		if strings.Contains(out, forbidden) {
			t.Errorf("worker log output contains %q: %s", forbidden, out)
		}
	}
	for _, required := range []string{`"database_url_set":true`, `"outbox_lease":`, `"tracing_enabled":true`} {
		if !strings.Contains(out, required) {
			t.Errorf("worker log output is missing %s: %s", required, out)
		}
	}
}

func TestWorkerConfigValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{name: "production", env: workerHosted(nil)},
		{name: "missing database", env: workerHosted(map[string]string{"PITBOX_DATABASE_URL": ""}), wantErr: "PITBOX_DATABASE_URL"},
		{name: "database needs verify-full outside local", env: workerHosted(map[string]string{ //nolint:gosec // fake credentials
			"PITBOX_DATABASE_URL": "postgres://pitbox_api:db-password@db.example.eu:5432/pitbox?sslmode=require",
		}), wantErr: "verify-full"},
		{name: "openfga needs tls outside local", env: workerHosted(map[string]string{"PITBOX_OPENFGA_TLS": "false"}), wantErr: "PITBOX_OPENFGA_TLS"},
		{name: "plain http telemetry outside local", env: workerHosted(map[string]string{
			"OTEL_EXPORTER_OTLP_ENDPOINT": "http://otlp.example.eu",
		}), wantErr: "plain http"},
		{name: "outbox batch out of range", env: workerHosted(map[string]string{"PITBOX_OUTBOX_BATCH_SIZE": "201"}), wantErr: "PITBOX_OUTBOX_BATCH_SIZE"},
		{name: "unknown environment", env: workerHosted(map[string]string{"PITBOX_ENV": "prod"}), wantErr: "PITBOX_ENV"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := LoadWorkerFrom(tc.env)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}
