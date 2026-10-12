package config

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestLoadFromDefaults(t *testing.T) {
	t.Parallel()

	c, err := LoadFrom(map[string]string{})
	if err != nil {
		t.Fatalf("defaults must be valid: %v", err)
	}
	if c.Env != EnvLocal || c.HTTPAddr != "127.0.0.1:8081" || c.LogLevel != "info" {
		t.Errorf("unexpected defaults: %+v", c)
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
func TestLoadFrom(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     map[string]string
		wantErr string // substring; empty = must load
	}{
		{name: "production over https", env: map[string]string{
			"PITBOX_ENV": "production", "OTEL_EXPORTER_OTLP_ENDPOINT": "https://otlp.example.eu",
		}},
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
		{name: "plain http endpoint refused in production", env: map[string]string{
			"PITBOX_ENV": "production", "OTEL_EXPORTER_OTLP_ENDPOINT": "http://otlp.example.eu",
		}, wantErr: "plain http"},
		{name: "plain http traces endpoint refused in staging", env: map[string]string{
			"PITBOX_ENV": "staging", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "HTTP://otlp.example.eu",
		}, wantErr: "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"},
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
