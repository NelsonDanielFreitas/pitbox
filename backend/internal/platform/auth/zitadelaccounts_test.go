package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testPAT = "pat-segredo-de-teste"

func accountsServer(t *testing.T, handler http.HandlerFunc) *ZitadelAccounts {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	z, err := NewZitadelAccounts(srv.URL, testPAT, srv.Client())
	if err != nil {
		t.Fatalf("NewZitadelAccounts: %v", err)
	}
	return z
}

func userJSON(id, state, email string, verified bool) string {
	return `{"userId":"` + id + `","state":"` + state + `","human":{"email":{"email":"` + email + `","isVerified":` +
		map[bool]string{true: "true", false: "false"}[verified] + `}}}`
}

func TestFindUserByEmail(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		reply   string
		status  int
		wantSub string
		wantOK  bool
		wantErr bool
	}{
		{"one active verified human", `{"result":[` + userJSON("1001", "USER_STATE_ACTIVE", "ana@example.com", true) + `]}`, 200, "1001", true, false},
		{"none", `{"result":[]}`, 200, "", false, false},
		{"ambiguous: never guess", `{"result":[` + userJSON("1", "USER_STATE_ACTIVE", "ana@example.com", true) + `,` + userJSON("2", "USER_STATE_ACTIVE", "ana@example.com", true) + `]}`, 200, "", false, false},
		{"inactive", `{"result":[` + userJSON("1001", "USER_STATE_LOCKED", "ana@example.com", true) + `]}`, 200, "", false, false},
		{"email not verified", `{"result":[` + userJSON("1001", "USER_STATE_ACTIVE", "ana@example.com", false) + `]}`, 200, "", false, false},
		{"a machine user", `{"result":[{"userId":"1001","state":"USER_STATE_ACTIVE"}]}`, 200, "", false, false},
		{"another email than asked", `{"result":[` + userJSON("1001", "USER_STATE_ACTIVE", "other@example.com", true) + `]}`, 200, "", false, false},
		{"hostile user id", `{"result":[` + userJSON("1001/../../admin", "USER_STATE_ACTIVE", "ana@example.com", true) + `]}`, 200, "", false, true},
		{"not json", `<html>`, 200, "", false, true},
		{"server error", `{}`, 500, "", false, true},
		{"unauthorized", `{}`, 401, "", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			z := accountsServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+testPAT {
					t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
				}
				if r.Method != http.MethodPost || r.URL.Path != "/v2/users" {
					t.Errorf("%s %s", r.Method, r.URL.Path)
				}
				var q struct {
					Queries []struct {
						Email struct {
							Address string `json:"emailAddress"`
							Method  string `json:"method"`
						} `json:"emailQuery"`
					} `json:"queries"`
					Query struct {
						Limit int `json:"limit"`
					} `json:"query"`
				}
				if err := json.NewDecoder(r.Body).Decode(&q); err != nil || len(q.Queries) != 1 ||
					q.Queries[0].Email.Address != "ana@example.com" || q.Queries[0].Email.Method != "TEXT_QUERY_METHOD_EQUALS" || q.Query.Limit != 2 {
					t.Errorf("unexpected query: %+v %v", q, err)
				}
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.reply)
			})
			sub, ok, err := z.FindUserByEmail(context.Background(), "ana@example.com")
			if (err != nil) != tt.wantErr || ok != tt.wantOK || sub != tt.wantSub {
				t.Fatalf("got (%q, %v, %v), want (%q, %v, err=%v)", sub, ok, err, tt.wantSub, tt.wantOK, tt.wantErr)
			}
		})
	}
}

func TestAuthMethods(t *testing.T) {
	t.Parallel()
	z := accountsServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v2/users/1001/authentication_methods" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"authMethodTypes":["AUTHENTICATION_METHOD_TYPE_PASSKEY","AUTHENTICATION_METHOD_TYPE_TOTP","AUTHENTICATION_METHOD_TYPE_IDP"]}`)
	})
	got, err := z.AuthMethods(context.Background(), "1001")
	if err != nil || !got.Passkey || got.Password {
		t.Fatalf("got %+v, %v", got, err)
	}
}

// A subject that is not a plain number never reaches a URL.
func TestSubjectIsCheckedBeforeItGoesInAPath(t *testing.T) {
	t.Parallel()
	z := accountsServer(t, func(http.ResponseWriter, *http.Request) { t.Error("a request was made") })
	for _, bad := range []string{"", "abc", "1/../../2", "1?x=1", "1#", "1 2", "12345678901234567890123456789012345", "-1", "1%2f2", "\x00"} {
		if _, err := z.AuthMethods(context.Background(), bad); err == nil {
			t.Errorf("AuthMethods(%q) accepted", bad)
		}
		if err := z.SendPasskeyRegistrationLink(context.Background(), bad); err == nil {
			t.Errorf("SendPasskeyRegistrationLink(%q) accepted", bad)
		}
	}
}

// The link is always emailed (sendLink), never returned to this process (returnCode).
func TestSendPasskeyRegistrationLinkAsksForAnEmail(t *testing.T) {
	t.Parallel()
	z := accountsServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v2/users/1001/passkeys/registration_link" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		if strings.TrimSpace(string(raw)) != `{"sendLink":{}}` {
			t.Errorf("body %q: the code must be emailed, not returned", raw)
		}
		_, _ = io.WriteString(w, `{"details":{}}`)
	})
	if err := z.SendPasskeyRegistrationLink(context.Background(), "1001"); err != nil {
		t.Fatalf("send: %v", err)
	}
}

func TestAccountsBoundTheirInputAndTime(t *testing.T) {
	t.Parallel()
	t.Run("huge reply", func(t *testing.T) {
		t.Parallel()
		z := accountsServer(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"authMethodTypes":["`+strings.Repeat("x", accountsMaxBody+10)+`"]}`)
		})
		if _, err := z.AuthMethods(context.Background(), "1"); err == nil {
			t.Fatal("a reply over the limit was accepted")
		}
	})
	t.Run("slow reply", func(t *testing.T) {
		t.Parallel()
		z := accountsServer(t, func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		if _, err := z.AuthMethods(ctx, "1"); err == nil {
			t.Fatal("no error from a call that never answered")
		}
	})
}

// Nothing of the token, the address or the id reaches an error or a log value.
func TestAccountsErrorsAndFormattingNeverCarrySecrets(t *testing.T) {
	t.Parallel()
	z := accountsServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"message":"`+testPAT+` ana@example.com 1001"}`)
	})
	_, _, err := z.FindUserByEmail(context.Background(), "ana@example.com")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, text := range []string{err.Error(), z.String(), z.GoString(), z.LogValue().String(), strings.TrimSpace(strings.Join([]string{}, ""))} {
		if strings.Contains(text, testPAT) || strings.Contains(text, "ana@") || strings.Contains(text, "1001") {
			t.Errorf("secret in %q", text)
		}
	}
}

func TestNewZitadelAccountsRefusesBadInput(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ issuer, pat string }{
		"no scheme":     {"zitadel.local", "x"},
		"ftp":           {"ftp://zitadel.local", "x"},
		"with a path":   {"https://zitadel.local/evil", "x"},
		"empty token":   {"https://zitadel.local", ""},
		"token spaces":  {"https://zitadel.local", "a b"},
		"token newline": {"https://zitadel.local", "a\nb"},
	} {
		if _, err := NewZitadelAccounts(tc.issuer, tc.pat, nil); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
