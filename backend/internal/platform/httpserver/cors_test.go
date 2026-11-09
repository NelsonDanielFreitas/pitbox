package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func corsRequest(h http.Handler, method, origin string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequestWithContext(context.Background(), method, "/x", nil)
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestCORS(t *testing.T) {
	t.Parallel()
	reached := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { reached++; w.WriteHeader(http.StatusOK) })
	h := cors([]string{"https://localhost", "http://localhost:5173"}, next)
	preflight := map[string]string{"Access-Control-Request-Method": "POST", "Access-Control-Request-Headers": "authorization, x-evil"}

	t.Run("preflight from a listed origin", func(t *testing.T) {
		w := corsRequest(h, http.MethodOptions, "https://localhost", preflight)
		if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != "https://localhost" {
			t.Fatalf("%d %v", w.Code, w.Header())
		}
		if w.Header().Get("Access-Control-Allow-Headers") != corsHeaders || w.Header().Get("Access-Control-Allow-Methods") != corsMethods {
			t.Error("the allowed methods or headers are not the fixed ones (the request must not widen them)")
		}
		if strings.Contains(w.Header().Get("Access-Control-Allow-Headers"), "x-evil") {
			t.Error("requested header echoed")
		}
	})
	t.Run("preflight from an unlisted origin", func(t *testing.T) {
		before := reached
		w := corsRequest(h, http.MethodOptions, "https://evil.example", preflight)
		if w.Code != http.StatusForbidden || w.Header().Get("Access-Control-Allow-Origin") != "" || reached != before {
			t.Fatalf("%d %v reached=%v", w.Code, w.Header(), reached != before)
		}
	})
	t.Run("actual request from a listed origin", func(t *testing.T) {
		w := corsRequest(h, http.MethodPost, "http://localhost:5173", nil)
		if w.Code != http.StatusOK || w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
			t.Fatalf("%d %v", w.Code, w.Header())
		}
	})
	t.Run("actual request from an unlisted origin gets no CORS header", func(t *testing.T) {
		for _, o := range []string{"https://evil.example", "https://localhost.evil.example", "https://localhost:8443", "http://localhost", "HTTPS://LOCALHOST", "null"} {
			w := corsRequest(h, http.MethodPost, o, nil)
			if w.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Errorf("%q was allowed", o)
			}
		}
	})
	t.Run("never credentials, never wildcard, always Vary", func(t *testing.T) {
		for _, w := range []*httptest.ResponseRecorder{
			corsRequest(h, http.MethodOptions, "https://localhost", preflight),
			corsRequest(h, http.MethodPost, "https://localhost", nil),
			corsRequest(h, http.MethodPost, "", nil),
		} {
			if w.Header().Get("Access-Control-Allow-Credentials") != "" || w.Header().Get("Access-Control-Allow-Origin") == "*" {
				t.Errorf("headers %v", w.Header())
			}
			if !strings.Contains(strings.Join(w.Header().Values("Vary"), ","), "Origin") {
				t.Error("no Vary: Origin")
			}
		}
	})
	t.Run("two Origin headers are not trusted", func(t *testing.T) {
		r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/x", nil)
		r.Header.Add("Origin", "https://localhost")
		r.Header.Add("Origin", "https://evil.example")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Error("allowed with two Origin headers")
		}
	})
}

func TestCORSOffWithoutOrigins(t *testing.T) {
	t.Parallel()
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	w := corsRequest(cors(nil, next), http.MethodOptions, "https://localhost", map[string]string{"Access-Control-Request-Method": "POST"})
	if w.Code != http.StatusTeapot || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("%d %v", w.Code, w.Header())
	}
}

func TestCORSOriginsValid(t *testing.T) {
	t.Parallel()
	for o, want := range map[string]bool{"https://localhost": true, "*": false, "": false, "localhost": false, "https://a/b": false, "https://a b": false} {
		if got := corsOriginsValid([]string{o}); got != want {
			t.Errorf("%q: %v", o, got)
		}
	}
}
