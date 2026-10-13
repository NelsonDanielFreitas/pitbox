package interceptors

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"unicode"
	"unicode/utf8"

	"connectrpc.com/connect/v2"
	"go.opentelemetry.io/otel/trace"
)

const maxUserAgentBytes = 256

// AuditMeta is the request context a use case needs to write audit.audit_log in the
// same transaction as the change: trace id, client address and user agent. The actor,
// the object and before/after only exist in the use case, which also writes the
// request_id (it is a field of the request message, not a header).
//
// The IP address is personal data: it belongs in audit.audit_log only, never in
// technical logs or traces.
type AuditMeta struct {
	TraceID   string
	IP        netip.Addr // zero value when unknown
	UserAgent string     // sanitized and cut to 256 bytes
}

type auditKey struct{}

// AuditMetaFromContext returns the metadata attached by Audit for write RPCs.
func AuditMetaFromContext(ctx context.Context) (AuditMeta, bool) {
	m, ok := ctx.Value(auditKey{}).(AuditMeta)
	return m, ok
}

// Audit attaches AuditMeta to the context of every RPC that may change data. An RPC
// counts as a read only when its schema says idempotency_level = NO_SIDE_EFFECTS; one
// that says nothing counts as a write (fail closed). The metadata is context only:
// recording the event is the use case's job, inside its transaction, which fails
// closed. The per-RPC log line (Errors) is the best-effort trail.
//
// clientIPHeader is "" for the TCP peer, or the name of a header set by a trusted
// proxy (config.Config.ClientIPHeader; only valid once the origin accepts Cloudflare
// traffic alone, F5-05).
func Audit(clientIPHeader string) connect.ServerInterceptor {
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			if spec.IdempotencyLevel == connect.IdempotencyNoSideEffects {
				return next(ctx, spec, stream)
			}
			meta := AuditMeta{TraceID: traceID(ctx)}
			if info, ok := connect.CallInfoForServerContext(ctx); ok {
				meta.IP = clientIP(info, clientIPHeader)
				meta.UserAgent = sanitizeUserAgent(info.RequestHeader().Get("User-Agent"), maxUserAgentBytes)
			}
			return next(context.WithValue(ctx, auditKey{}, meta), spec, stream)
		}
	}
}

func traceID(ctx context.Context) string {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		return sc.TraceID().String()
	}
	return ""
}

// clientIP reads the client address from the configured header, or from the peer.
// An unparsable value yields the zero address, never the raw text.
func clientIP(info *connect.CallInfo, header string) netip.Addr {
	if header != "" {
		a, err := netip.ParseAddr(strings.TrimSpace(info.RequestHeader().Get(header)))
		if err != nil {
			return netip.Addr{}
		}
		return a.Unmap()
	}
	host, _, err := net.SplitHostPort(info.PeerAddr)
	if err != nil {
		return netip.Addr{}
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return a.Unmap()
}

// sanitizeUserAgent returns s with invalid UTF-8 and control characters removed,
// cut to at most max bytes on a character boundary. The input comes from the client.
func sanitizeUserAgent(s string, max int) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
