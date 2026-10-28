package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"golang.org/x/oauth2"
)

const (
	// LoginCookieName uses the __Host- prefix: the browser only accepts it if it is Secure,
	// has Path=/ and no Domain, so a sibling subdomain cannot set or overwrite it.
	LoginCookieName = "__Host-pitbox_login"
	// LoginCookieMaxAge bounds how long a login attempt may take. Aligned with the
	// 30-minute email verification code: the user may need to open Mailpit and type
	// the code after registering.
	LoginCookieMaxAge = 30 * time.Minute
)

// ErrNoLoginState means the callback came without a valid pre-login cookie: it was never
// started here, it expired, or it was tampered with. The handler answers 400 without
// calling the identity provider.
var ErrNoLoginState = errors.New("auth: no valid login state")

// LoginState is what the callback needs to finish a login that /auth/login started. It
// travels in a sealed cookie, so the server keeps no state before the user is signed in
// (a flood of /auth/login costs memory in nobody).
type LoginState struct {
	State      string    `json:"s"`
	Nonce      string    `json:"n"`
	Verifier   string    `json:"v"` // PKCE code_verifier
	ReturnTo   string    `json:"r"`
	Expires    time.Time `json:"e"`
	StepUp     bool      `json:"su,omitempty"` // true when reauthenticating for step-up
	SessionRef string    `json:"sr,omitempty"` // existing session ref when StepUp is true
}

// NewLoginState creates the random values of one login attempt. returnTo must already have
// passed SafeReturnTo.
func NewLoginState(now time.Time, returnTo string) (LoginState, error) {
	state, err := randomToken()
	if err != nil {
		return LoginState{}, err
	}
	nonce, err := randomToken()
	if err != nil {
		return LoginState{}, err
	}
	return LoginState{
		State:    state,
		Nonce:    nonce,
		Verifier: oauth2.GenerateVerifier(),
		ReturnTo: returnTo,
		Expires:  now.Add(LoginCookieMaxAge),
	}, nil
}

// MatchesState compares the "state" parameter of the callback in constant time.
func (s LoginState) MatchesState(got string) bool {
	return subtle.ConstantTimeCompare([]byte(s.State), []byte(got)) == 1
}

// LoginCookies sets, reads and clears the pre-login cookie.
type LoginCookies struct {
	sealer *Sealer
	now    func() time.Time
}

// NewLoginCookies takes the sealer for PurposeLoginCookie.
func NewLoginCookies(sealer *Sealer, now func() time.Time) *LoginCookies {
	return &LoginCookies{sealer: sealer, now: now}
}

func (c *LoginCookies) aad() []byte { return []byte(LoginCookieName) }

// Set writes the cookie: Secure, HttpOnly, SameSite=Lax (the return from the identity
// provider is a cross-site navigation, and Strict would drop the cookie), Path=/, no Domain.
func (c *LoginCookies) Set(w http.ResponseWriter, st LoginState) error {
	raw, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("marshal login state: %w", err)
	}
	sealed, err := c.sealer.Seal(raw, c.aad())
	if err != nil {
		return fmt.Errorf("seal login state: %w", err)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     LoginCookieName,
		Value:    sealed,
		Path:     "/",
		MaxAge:   int(LoginCookieMaxAge / time.Second),
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// Read returns the state in the request's cookie, or ErrNoLoginState for anything that is
// missing, invalid or expired (one error, no detail: the cause is not the client's business).
func (c *LoginCookies) Read(r *http.Request) (LoginState, error) {
	ck, err := r.Cookie(LoginCookieName)
	if err != nil {
		return LoginState{}, ErrNoLoginState
	}
	raw, err := c.sealer.Open(ck.Value, c.aad())
	if err != nil {
		return LoginState{}, ErrNoLoginState
	}
	var st LoginState
	if err := json.Unmarshal(raw, &st); err != nil {
		return LoginState{}, ErrNoLoginState
	}
	if st.State == "" || st.Nonce == "" || st.Verifier == "" || !c.now().Before(st.Expires) {
		return LoginState{}, ErrNoLoginState
	}
	return st, nil
}

// Clear deletes the cookie (the attributes must match the ones it was set with).
func (c *LoginCookies) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: LoginCookieName, Value: "", Path: "/", MaxAge: -1,
		Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}

// randomToken returns 32 random bytes as base64url (256 bits).
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
