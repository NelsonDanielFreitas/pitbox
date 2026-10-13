package auth

import (
	"net/url"
	"strings"
)

const maxReturnTo = 512

// SafeReturnTo validates where to send the browser after a login. It returns the value
// unchanged when it is a same-origin absolute path (optionally with a query), and "/"
// with ok=false otherwise.
//
// An open redirect after login is a phishing gift: the victim authenticates on the real
// site and lands on the attacker's. The rules reject everything a browser could read as
// another host: "//host", "/\host", schemes, control characters, backslashes, a fragment,
// and any path in the /auth/ tree (a redirect back into the login would loop).
func SafeReturnTo(raw string) (path string, ok bool) {
	if raw == "" || len(raw) > maxReturnTo || raw[0] != '/' {
		return "/", false
	}
	if strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, "/\\") {
		return "/", false
	}
	for _, r := range raw {
		// Control characters (incl. tab, CR, LF, which browsers strip from URLs, turning
		// "/\t/evil.com" into "//evil.com"), DEL, backslash and non-ASCII.
		if r < 0x20 || r == 0x7f || r == '\\' || r > 0x7e {
			return "/", false
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.Fragment != "" || u.Opaque != "" || strings.Contains(raw, "#") {
		return "/", false
	}
	// The decoded path must also stay a plain path: "/%2f/evil.com" would decode to "//evil.com".
	if !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") || strings.Contains(u.Path, "\\") {
		return "/", false
	}
	// Nothing that could collapse into "//host" when the browser normalises the path:
	// empty segments ("//") and dot segments ("/./", "/../"), raw or decoded.
	for _, p := range []string{raw[:pathEnd(raw)], u.Path} {
		if strings.Contains(p, "//") {
			return "/", false
		}
		for _, seg := range strings.Split(p, "/") {
			if seg == "." || seg == ".." {
				return "/", false
			}
		}
	}
	if u.Path == "/auth" || strings.HasPrefix(u.Path, "/auth/") {
		return "/", false
	}
	return raw, true
}

// pathEnd returns where the path of an absolute-path reference ends (at "?" or the end).
func pathEnd(raw string) int {
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		return i
	}
	return len(raw)
}
