package auth

import (
	"net/http"
	"time"
)

// SessionCookieName uses __Host-: Secure, Path=/ and no Domain are enforced by the browser.
const SessionCookieName = "__Host-pitbox_session"

// SetSessionCookie writes the session cookie: HttpOnly (no script can read it), Secure and
// SameSite=Strict (the browser never sends it on a cross-site request). MaxAge is the
// absolute timeout; the idle timeout is enforced by the store.
func SetSessionCookie(w http.ResponseWriter, token string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookieName, Value: token, Path: "/", MaxAge: int(maxAge / time.Second),
		Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
}

// ClearSessionCookie deletes the cookie (same attributes as when it was set).
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookieName, Value: "", Path: "/", MaxAge: -1,
		Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
}

// SessionTokenFromHeader extracts the session cookie value from request headers. A request
// with several cookies of that name is refused (""): which one is meant is ambiguous, and a
// sibling subdomain could otherwise shadow the real one.
func SessionTokenFromHeader(h http.Header) string {
	r := http.Request{Header: h}
	var found string
	for _, c := range r.Cookies() {
		if c.Name != SessionCookieName {
			continue
		}
		if found != "" {
			return ""
		}
		found = c.Value
	}
	return found
}
