package httpserver

import (
	"net/http"
	"strings"
)

// Fixed CORS answers (ADR 0032, section 6). Nothing here is taken from the request: not the
// methods, not the headers, so a preflight cannot widen what is allowed.
const (
	corsMethods = "GET, POST"
	corsHeaders = "Authorization, Content-Type, Connect-Protocol-Version, Connect-Timeout-Ms, " +
		"Connect-Content-Encoding, Connect-Accept-Encoding"
	// corsMaxAge lets the browser cache a preflight for 10 minutes: long enough to save
	// round trips, short enough that a change of the allow-list takes effect soon.
	corsMaxAge = "600"
)

// cors lets the listed origins (exact match, scheme://host[:port]) call the API from a
// browser context such as the Capacitor WebView (https://localhost).
//
// What it deliberately does not do:
//
//   - no wildcard and no reflection of an unlisted origin: the answer to an unlisted origin
//     carries no CORS header at all, so the browser blocks the response;
//   - no Access-Control-Allow-Credentials: the allowed origins authenticate with a Bearer
//     in a header, never with the ambient cookie, so a cross-origin page cannot ride the
//     user's cookie. A cookie request from another origin is still refused by the
//     authentication layer (foreignRequest);
//   - a preflight from an unlisted origin is refused here and never reaches the handlers.
//
// With no origins configured the middleware is the identity.
func cors(origins []string, next http.Handler) http.Handler {
	if len(origins) == 0 {
		return next
	}
	allowed := make(map[string]struct{}, len(origins))
	for _, o := range origins {
		allowed[o] = struct{}{}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		// The answer depends on the Origin header: caches must key on it.
		h.Add("Vary", "Origin")
		origin := r.Header.Get("Origin")
		if origin == "" || len(r.Header.Values("Origin")) != 1 {
			next.ServeHTTP(w, r)
			return
		}
		_, ok := allowed[origin]
		preflight := r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != ""
		switch {
		case ok && preflight:
			h.Add("Vary", "Access-Control-Request-Method")
			h.Add("Vary", "Access-Control-Request-Headers")
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Methods", corsMethods)
			h.Set("Access-Control-Allow-Headers", corsHeaders)
			h.Set("Access-Control-Max-Age", corsMaxAge)
			w.WriteHeader(http.StatusNoContent)
		case preflight:
			// Not a listed origin: no CORS headers and no handler.
			http.Error(w, "forbidden", http.StatusForbidden)
		case ok:
			h.Set("Access-Control-Allow-Origin", origin)
			next.ServeHTTP(w, r)
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// corsOriginsValid reports whether every origin has the exact form the middleware compares
// against (config.Validate already checks this; the server refuses to start otherwise).
func corsOriginsValid(origins []string) bool {
	for _, o := range origins {
		scheme, host, found := strings.Cut(o, "://")
		if !found || scheme == "" || host == "" || strings.ContainsAny(o, " \t?#") || strings.Contains(host, "/") || strings.Contains(o, "*") {
			return false
		}
	}
	return true
}
