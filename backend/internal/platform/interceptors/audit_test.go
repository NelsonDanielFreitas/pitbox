package interceptors

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"unicode/utf8"

	"connectrpc.com/connect/v2"
)

func TestAuditAttachesMetadataToWrites(t *testing.T) {
	t.Parallel()

	write := unarySpec // idempotency level unknown: counts as a write (fail closed)
	read := unarySpec
	read.IdempotencyLevel = connect.IdempotencyNoSideEffects
	idempotentWrite := unarySpec
	idempotentWrite.IdempotencyLevel = connect.IdempotencyIdempotent

	tests := []struct {
		name     string
		spec     connect.Spec
		wantMeta bool
	}{
		{name: "unknown level is a write", spec: write, wantMeta: true},
		{name: "idempotent is still a write", spec: idempotentWrite, wantMeta: true},
		{name: "no side effects is a read", spec: read, wantMeta: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			info := &connect.CallInfo{PeerAddr: "203.0.113.7:51234"}
			info.RequestHeader().Set("User-Agent", "PitBox/1.0")

			var got AuditMeta
			var ok bool
			err := call(t, Audit(""), tc.spec, info, func(ctx context.Context, _ connect.Spec, _ connect.ServerStream) error {
				got, ok = AuditMetaFromContext(ctx)
				return nil
			})
			if err != nil {
				t.Fatalf("call: %v", err)
			}
			if ok != tc.wantMeta {
				t.Fatalf("metadata present = %v, want %v", ok, tc.wantMeta)
			}
			if tc.wantMeta && (got.IP != netip.MustParseAddr("203.0.113.7") || got.UserAgent != "PitBox/1.0") {
				t.Errorf("meta = %+v", got)
			}
		})
	}
}

func TestClientIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		peer   string
		header string // name of the header to read, "" = peer
		value  string // its value
		want   string // "" = unknown
	}{
		{name: "ipv4 peer", peer: "203.0.113.7:443", want: "203.0.113.7"},
		{name: "ipv6 peer", peer: "[2001:db8::1]:443", want: "2001:db8::1"},
		{name: "ipv4-mapped ipv6 is unmapped", peer: "[::ffff:203.0.113.7]:443", want: "203.0.113.7"},
		{name: "peer without a port is unknown", peer: "203.0.113.7", want: ""},
		{name: "empty peer is unknown", peer: "", want: ""},
		{name: "hostname peer is unknown", peer: "example.com:443", want: ""},
		{name: "header is used when configured", peer: "10.0.0.1:1", header: "CF-Connecting-IP", value: "198.51.100.9", want: "198.51.100.9"},
		{name: "header with spaces", peer: "10.0.0.1:1", header: "CF-Connecting-IP", value: " 198.51.100.9 ", want: "198.51.100.9"},
		{name: "garbage in the header is unknown, never the raw text", peer: "10.0.0.1:1", header: "CF-Connecting-IP", value: "1.2.3.4; DROP TABLE", want: ""},
		{name: "header configured but absent does not fall back to the peer", peer: "10.0.0.1:1", header: "CF-Connecting-IP", value: "", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			info := &connect.CallInfo{PeerAddr: tc.peer}
			if tc.header != "" {
				info.RequestHeader().Set(tc.header, tc.value)
			}
			got := clientIP(info, tc.header)
			if tc.want == "" {
				if got.IsValid() {
					t.Errorf("got %v, want unknown", got)
				}
				return
			}
			if got != netip.MustParseAddr(tc.want) {
				t.Errorf("got %v, want %s", got, tc.want)
			}
		})
	}
}

func TestSanitizeUserAgent(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, in, want string }{
		{name: "plain", in: "PitBox/1.0 (Android 15)", want: "PitBox/1.0 (Android 15)"},
		{name: "control characters removed", in: "a\x00b\r\nc\x7fd", want: "abcd"},
		{name: "invalid utf-8 removed", in: "a\xffb", want: "ab"},
		{name: "cut on a character boundary", in: strings.Repeat("é", 200), want: strings.Repeat("é", 128)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := sanitizeUserAgent(tc.in, maxUserAgentBytes); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// The user agent comes from the client: whatever it sends, the result is valid UTF-8,
// within the limit and free of control characters.
func FuzzSanitizeUserAgent(f *testing.F) {
	for _, s := range []string{"", "PitBox/1.0", "\x00", "\xff\xfe", strings.Repeat("é", 300), "a\u0085b", "\U0001F600" + strings.Repeat("x", 254)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got := sanitizeUserAgent(s, maxUserAgentBytes)
		if !utf8.ValidString(got) {
			t.Fatalf("invalid UTF-8 in %q", got)
		}
		if len(got) > maxUserAgentBytes {
			t.Fatalf("%d bytes, over the limit", len(got))
		}
		for _, r := range got {
			if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
				t.Fatalf("control character %U in %q", r, got)
			}
		}
	})
}

func TestRequestMeta(t *testing.T) {
	t.Parallel()

	req := func(remote, header string) *http.Request {
		r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.example.eu/auth/login", nil)
		r.RemoteAddr = remote
		r.Header.Set("User-Agent", "agent\x00\n/1.0")
		if header != "" {
			r.Header.Set("CF-Connecting-IP", header)
		}
		return r
	}
	tests := []struct {
		name, remote, header, clientIPHeader, wantIP string
	}{
		{"peer", "203.0.113.7:51234", "", "", "203.0.113.7"},
		{"peer ipv6", "[2001:db8::1]:443", "", "", "2001:db8::1"},
		{"a forged header is ignored in peer mode", "203.0.113.7:1", "198.51.100.9", "", "203.0.113.7"},
		{"trusted header", "10.0.0.1:1", "198.51.100.9", "CF-Connecting-IP", "198.51.100.9"},
		{"unparsable header gives no address", "10.0.0.1:1", "not-an-ip", "CF-Connecting-IP", ""},
		{"unparsable peer gives no address", "garbage", "", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := RequestMeta(req(tc.remote, tc.header), tc.clientIPHeader)
			got := ""
			if m.IP.IsValid() {
				got = m.IP.String()
			}
			if got != tc.wantIP {
				t.Errorf("IP = %q, want %q", got, tc.wantIP)
			}
			if m.UserAgent != "agent/1.0" {
				t.Errorf("UserAgent = %q, want it sanitized", m.UserAgent)
			}
		})
	}
}
