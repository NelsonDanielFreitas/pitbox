package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/config"
)

type recorder struct{ got *http.Request }

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.got = req
	return &http.Response{StatusCode: 200, Body: http.NoBody, Request: req}, nil
}

func TestIssuerDialRewritesOnlyTheIssuer(t *testing.T) {
	const issuer = "https://pc.tail1234.ts.net:8443"
	tests := []struct {
		name, url         string
		wantURL, wantHost string
	}{
		{"issuer discovery", issuer + "/.well-known/openid-configuration", "http://127.0.0.1:18080/.well-known/openid-configuration", "pc.tail1234.ts.net:8443"},
		{"issuer host in upper case", "https://PC.tail1234.ts.net:8443/oauth/v2/token", "http://127.0.0.1:18080/oauth/v2/token", "PC.tail1234.ts.net:8443"},
		{"same host, other port", "https://pc.tail1234.ts.net/x", "https://pc.tail1234.ts.net/x", ""},
		{"other tailnet host", "https://other.tail1234.ts.net:8443/x", "https://other.tail1234.ts.net:8443/x", ""},
		{"public host", "https://example.com/x", "https://example.com/x", ""},
		{"plain http to the issuer host", "http://pc.tail1234.ts.net:8443/x", "http://pc.tail1234.ts.net:8443/x", ""},
		{"loopback is not rewritten", "http://127.0.0.1:9999/x", "http://127.0.0.1:9999/x", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recorder{}
			rt, err := IssuerDial(rec, issuer, "127.0.0.1:18080", config.IsTailnetHost)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.url, nil)
			req.Host = ""
			resp, err := rt.RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if got := rec.got.URL.String(); got != tt.wantURL {
				t.Errorf("url = %q, want %q", got, tt.wantURL)
			}
			if tt.wantHost != "" && rec.got.Host != tt.wantHost {
				t.Errorf("Host = %q, want %q", rec.got.Host, tt.wantHost)
			}
		})
	}
}

func TestIssuerDialRefusesUnsafeSetups(t *testing.T) {
	tests := []struct{ name, issuer, dial string }{
		{"issuer not https", "http://pc.tail1.ts.net:8443", "127.0.0.1:1"},
		{"issuer not ts.net", "https://example.com", "127.0.0.1:1"},
		{"ts.net lookalike", "https://pc.ts.net.evil.example", "127.0.0.1:1"},
		{"dial not loopback", "https://pc.tail1.ts.net", "10.0.0.5:8080"},
		{"dial is a name", "https://pc.tail1.ts.net", "localhost:8080"},
		{"dial without port", "https://pc.tail1.ts.net", "127.0.0.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := IssuerDial(nil, tt.issuer, tt.dial, config.IsTailnetHost); err == nil || strings.Contains(err.Error(), "tail1") {
				t.Fatalf("err = %v, want a refusal", err)
			}
		})
	}
}
