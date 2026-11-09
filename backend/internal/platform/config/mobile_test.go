package config

import (
	"strings"
	"testing"
)

func mobileEnv(extra map[string]string) map[string]string {
	m := localAuth(map[string]string{
		"PITBOX_OIDC_MOBILE_CLIENT_ID": "398899912525217796",
		"PITBOX_CORS_ORIGINS":          "https://localhost",
	})
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func TestMobileConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string // empty = valid
	}{
		{"off by default", localAuth(nil), ""},
		{"on", mobileEnv(nil), ""},
		{"on, two origins", mobileEnv(map[string]string{"PITBOX_CORS_ORIGINS": "https://localhost,http://localhost"}), ""},
		{"CORS without the mobile login", localAuth(map[string]string{"PITBOX_CORS_ORIGINS": "https://localhost"}), "only valid with PITBOX_OIDC_MOBILE_CLIENT_ID"},
		{"mobile login without CORS", mobileEnv(map[string]string{"PITBOX_CORS_ORIGINS": ""}), "PITBOX_CORS_ORIGINS: required"},
		{"same client id as the web app", mobileEnv(map[string]string{"PITBOX_OIDC_MOBILE_CLIENT_ID": "client-id"}), "must differ"},
		{"bad client id", mobileEnv(map[string]string{"PITBOX_OIDC_MOBILE_CLIENT_ID": "a b"}), "not a valid client id"},
		{"web auth not configured", map[string]string{"PITBOX_OIDC_MOBILE_CLIENT_ID": "1", "PITBOX_CORS_ORIGINS": "https://localhost"}, "needs the web authentication"},
		{"wildcard origin", mobileEnv(map[string]string{"PITBOX_CORS_ORIGINS": "*"}), "wildcard"},
		{"origin with a path", mobileEnv(map[string]string{"PITBOX_CORS_ORIGINS": "https://localhost/app"}), "not a valid origin"},
		{"origin with a trailing slash", mobileEnv(map[string]string{"PITBOX_CORS_ORIGINS": "https://localhost/"}), "scheme://host[:port]"},
		{"origin with credentials", mobileEnv(map[string]string{"PITBOX_CORS_ORIGINS": "https://u:p@localhost"}), "not a valid origin"}, //nolint:gosec // not a credential: the test URL has userinfo on purpose
		{"plain http to a public host", mobileEnv(map[string]string{"PITBOX_CORS_ORIGINS": "http://example.com"}), "not a valid origin"},
		{"duplicate origin", mobileEnv(map[string]string{"PITBOX_CORS_ORIGINS": "https://localhost,https://localhost"}), "listed twice"},
		{"too many origins", mobileEnv(map[string]string{"PITBOX_CORS_ORIGINS": "https://a.example,https://b.example,https://c.example,https://d.example,https://e.example,https://f.example,https://g.example,https://h.example,https://i.example"}), "at most"},
		{"redirect: https", mobileEnv(map[string]string{"PITBOX_MOBILE_REDIRECT_URI": "https://pitbox.example/auth/callback"}), "reverse-domain"},
		{"redirect: scheme without a dot", mobileEnv(map[string]string{"PITBOX_MOBILE_REDIRECT_URI": "pitbox:/auth/callback"}), "reverse-domain"},
		{"redirect: another path", mobileEnv(map[string]string{"PITBOX_MOBILE_REDIRECT_URI": "app.pitbox.mobile:/other"}), "must be <scheme>:/auth/callback"},
		{"redirect: with authority", mobileEnv(map[string]string{"PITBOX_MOBILE_REDIRECT_URI": "app.pitbox.mobile://evil/auth/callback"}), "no authority"},
		{"redirect: with a query", mobileEnv(map[string]string{"PITBOX_MOBILE_REDIRECT_URI": "app.pitbox.mobile:/auth/callback?x=1"}), "no authority, query or fragment"},
		{"redirect: upper-case scheme", mobileEnv(map[string]string{"PITBOX_MOBILE_REDIRECT_URI": "App.Pitbox.Mobile:/auth/callback"}), "canonical form"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := LoadFrom(tt.env)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
			case tt.wantErr == "" && cfg.MobileEnabled() != (tt.env["PITBOX_OIDC_MOBILE_CLIENT_ID"] != ""):
				t.Fatalf("MobileEnabled() = %v", cfg.MobileEnabled())
			}
		})
	}
}

func TestMobileOutsideLocalNeedsHTTPSOrigins(t *testing.T) {
	t.Parallel()
	_, err := LoadFrom(hosted("production", mobileEnv(map[string]string{"PITBOX_CORS_ORIGINS": "http://localhost"})))
	if err == nil || !strings.Contains(err.Error(), "PITBOX_CORS_ORIGINS") {
		t.Fatalf("err = %v, want a PITBOX_CORS_ORIGINS refusal", err)
	}
}

// Errors about these settings never repeat what was set.
func TestMobileErrorsDoNotEchoValues(t *testing.T) {
	t.Parallel()
	const marker = "marker-value-123"
	for _, env := range []map[string]string{
		mobileEnv(map[string]string{"PITBOX_CORS_ORIGINS": "https://" + marker + ".example/x"}),
		mobileEnv(map[string]string{"PITBOX_MOBILE_REDIRECT_URI": marker + ".app:/other"}),
		mobileEnv(map[string]string{"PITBOX_OIDC_MOBILE_CLIENT_ID": marker + " bad"}),
	} {
		_, err := LoadFrom(env)
		if err == nil {
			t.Fatal("expected an error")
		}
		if strings.Contains(err.Error(), marker) {
			t.Errorf("the error repeats the value: %v", err)
		}
	}
}
