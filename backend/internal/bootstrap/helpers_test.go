package bootstrap

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"
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

// hostedAuthEnv is the authentication configuration every environment other than local
// needs, with fake values.
//
//nolint:gosec // fake credentials
func hostedAuthEnv() map[string]string {
	return map[string]string{
		"PITBOX_PUBLIC_URL":            "https://api.example.eu",
		"PITBOX_OIDC_ISSUER":           "https://auth.example.eu",
		"PITBOX_OIDC_CLIENT_ID":        "client-id",
		"PITBOX_OIDC_CLIENT_SECRET":    "client-secret",
		"PITBOX_AUTH_KEYS":             "k1:AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=",
		"PITBOX_SYNC_PASS_KEYS":        "k1:AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=",
		"PITBOX_VALKEY_PASSWORD":       "valkey-password",
		"PITBOX_VALKEY_TLS":            "true",
		"PITBOX_DATABASE_URL":          "postgres://pitbox_api:db-password@db.example.eu/pitbox?sslmode=verify-full",
		"PITBOX_OPENFGA_TLS":           "true",
		"PITBOX_OPENFGA_PRESHARED_KEY": "openfga-psk",
		"PITBOX_OPENFGA_STORE_ID":      "01JABCD0123456789ABCDEFGH",
	}
}
