package httpserver

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
)

// MaxMessageBytes is the size limit of one RPC message, in both directions
// (architecture §4.4). It applies after decompression too: a small gzip body that
// expands beyond it is refused with CodeResourceExhausted.
const MaxMessageBytes = 4 << 20

// RPCOptions configures MountRPC.
type RPCOptions struct {
	// MaxUnaryTimeout bounds how long the body of a unary RPC may take to arrive.
	MaxUnaryTimeout time.Duration
	// MaxStreamDuration bounds the read side of a streaming RPC.
	MaxStreamDuration time.Duration
}

// MountRPC serves the Connect server's procedures on mux with explicit limits:
//
//   - MaxMessageBytes in both directions. connecthttp.WithReadMaxBytes and
//     WithSendMaxBytes are only called here, with a positive value (a test in
//     internal/archtest enforces it): zero would mean "no limit".
//   - a read deadline per request, so a client that sends its body slowly cannot hold a
//     connection: the context deadline does not interrupt a blocked body read.
//
// It needs no total-body limit for unary calls: the body is one message, already
// limited by MaxMessageBytes.
func MountRPC(mux connecthttp.ServeMux, server *connect.Server, o RPCOptions) error {
	if o.MaxUnaryTimeout <= 0 || o.MaxStreamDuration <= 0 {
		return errors.New("httpserver: MountRPC needs positive durations")
	}
	limits := make(map[string]time.Duration)
	for spec := range server.Specs() {
		d := o.MaxUnaryTimeout
		if spec.StreamType != connect.StreamTypeUnary {
			d = o.MaxStreamDuration
		}
		limits[spec.Procedure] = d
	}
	connecthttp.Mount(readDeadlineMux{mux: mux, limits: limits, fallback: o.MaxUnaryTimeout}, server,
		connecthttp.WithReadMaxBytes(MaxMessageBytes),
		connecthttp.WithSendMaxBytes(MaxMessageBytes),
	)
	return nil
}

// readDeadlineMux wraps every handler Mount registers with a read deadline.
type readDeadlineMux struct {
	mux      connecthttp.ServeMux
	limits   map[string]time.Duration // procedure path -> limit
	fallback time.Duration            // unknown procedures (the per-service catch-all)
}

func (m readDeadlineMux) Handle(pattern string, h http.Handler) {
	m.mux.Handle(pattern, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit, ok := m.limits[r.URL.Path]
		if !ok {
			limit = m.fallback
		}
		// Not every ResponseWriter supports it; the limit is best effort there.
		_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(limit))
		h.ServeHTTP(w, r)
	}))
}

// ServicePrefixes returns the URL prefix ("/pkg.Service/") of every service the server
// has procedures for, used to keep RPC routes out of the HTTP trace middleware.
func ServicePrefixes(server *connect.Server) []string {
	seen := make(map[string]struct{})
	var prefixes []string
	for spec := range server.Specs() {
		i := strings.LastIndex(spec.Procedure, "/")
		if i <= 0 {
			continue
		}
		p := spec.Procedure[:i+1]
		if _, dup := seen[p]; !dup {
			seen[p] = struct{}{}
			prefixes = append(prefixes, p)
		}
	}
	return prefixes
}
