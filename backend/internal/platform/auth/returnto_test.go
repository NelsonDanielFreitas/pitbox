package auth

import (
	"net/url"
	"strings"
	"testing"
)

func TestSafeReturnTo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in string
		ok bool
	}{
		{"/", true},
		{"/equipas", true},
		{"/equipas/123?tab=setups&x=1", true},
		{"/a%20b", true},
		{"", false},
		{"equipas", false},
		{"//evil.example", false},
		{"/\\evil.example", false},
		{"/%5Cevil.example", false}, // decodes to a backslash: refused although a browser would not read it as a host
		{"/%2f/evil.example", false},
		{"/%2F%2Fevil.example", false}, // decodes to "//"
		{"https://evil.example", false},
		{"javascript:alert(1)", false},
		{"/\t/evil.example", false},
		{"/\n/evil.example", false},
		{"/\r/evil.example", false},
		{"/ok\x00", false},
		{"/ok\x7f", false},
		{"/olá", false},
		{"/path#frag", false},
		{"/path\\x", false},
		{"/auth/login", false},
		{"/.//x", false},
		{"/a/../b", false},
		{"/a/./b", false},
		{"/a//b", false},
		{"/a/b/", true},
		{"/a.b/c..d", true},
		{"/auth", false},
		{"/authors", true},
		{"/" + strings.Repeat("a", maxReturnTo), false},
		{"/" + strings.Repeat("a", maxReturnTo-1), true},
		{"/?next=//evil.example", true}, // a query value is not a host
	}
	for _, tc := range tests {
		got, ok := SafeReturnTo(tc.in)
		if ok != tc.ok {
			t.Errorf("SafeReturnTo(%q) ok = %v, want %v", tc.in, ok, tc.ok)
		}
		switch {
		case ok && got != tc.in:
			t.Errorf("SafeReturnTo(%q) changed the value to %q", tc.in, got)
		case !ok && got != "/":
			t.Errorf("SafeReturnTo(%q) fallback = %q, want /", tc.in, got)
		}
	}
}

// The property that matters: whatever is accepted, resolving it against the site's origin
// stays on that origin.
func FuzzSafeReturnTo(f *testing.F) {
	for _, seed := range []string{"/", "//x", "/\\x", "/%2f/x", "/a?b=c", "https://x", "/\t/x", "/auth/x", "/%5c", "/;//x", "/.//x", "/..//x"} {
		f.Add(seed)
	}
	base, _ := url.Parse("https://app.example/")
	f.Fuzz(func(t *testing.T, raw string) {
		got, ok := SafeReturnTo(raw)
		if !ok {
			if got != "/" {
				t.Fatalf("fallback = %q", got)
			}
			return
		}
		if got != raw || !strings.HasPrefix(got, "/") || strings.HasPrefix(got, "//") {
			t.Fatalf("accepted a bad value: %q", raw)
		}
		u, err := url.Parse(got)
		if err != nil {
			t.Fatalf("accepted an unparsable value %q: %v", raw, err)
		}
		if r := base.ResolveReference(u); r.Host != "app.example" || r.Scheme != "https" {
			t.Fatalf("%q resolves to %s", raw, r)
		}
		// Browsers also normalise "/.//x" style paths: the resolved path must not become "//...".
		if strings.HasPrefix(base.ResolveReference(u).Path, "//") {
			t.Fatalf("%q resolves to a protocol-relative path", raw)
		}
	})
}
