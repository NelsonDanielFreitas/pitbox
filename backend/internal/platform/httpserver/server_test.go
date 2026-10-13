package httpserver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func testLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

type running struct {
	base string
	stop context.CancelFunc
	done chan struct{} // closed when Run returns; err is valid after that
	err  error
}

// start runs a server on a free loopback port and returns when it is reachable.
func start(t *testing.T, o Options) *running {
	t.Helper()
	if o.Logger == nil {
		o.Logger = testLogger()
	}
	if o.TracerProvider == nil {
		o.TracerProvider = noop.NewTracerProvider()
	}
	if o.ShutdownTimeout == 0 {
		o.ShutdownTimeout = 2 * time.Second
	}
	s, err := New(o)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, stop := context.WithCancel(context.Background())
	r := &running{base: "http://" + ln.Addr().String(), stop: stop, done: make(chan struct{})}
	go func() {
		r.err = s.Run(ctx, ln)
		close(r.done)
	}()
	t.Cleanup(func() {
		stop()
		select {
		case <-r.done:
		case <-time.After(5 * time.Second):
			t.Error("server did not stop")
		}
	})
	return r
}

// reply is a fully read response: the body is closed inside get, so no test can
// forget to close it.
type reply struct {
	Status     int
	Proto      string
	ProtoMajor int
	Header     http.Header
	Body       string
}

func get(t *testing.T, client *http.Client, url string, hdr http.Header) reply {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	for k, v := range hdr {
		req.Header[k] = v
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return reply{Status: resp.StatusCode, Proto: resp.Proto, ProtoMajor: resp.ProtoMajor, Header: resp.Header, Body: string(b)}
}

func TestNewRequiresDependencies(t *testing.T) {
	t.Parallel()
	if _, err := New(Options{}); err == nil {
		t.Error("New without Logger and TracerProvider must fail")
	}
}

func TestProbes(t *testing.T) {
	t.Parallel()
	r := start(t, Options{})

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantBody   string
	}{
		{name: "liveness", method: http.MethodGet, path: "/healthz", wantStatus: http.StatusOK, wantBody: "ok\n"},
		{name: "readiness", method: http.MethodGet, path: "/readyz", wantStatus: http.StatusOK, wantBody: "ready\n"},
		{name: "head is allowed", method: http.MethodHead, path: "/healthz", wantStatus: http.StatusOK},
		{name: "post is refused", method: http.MethodPost, path: "/healthz", wantStatus: http.StatusMethodNotAllowed},
		{name: "unknown path is a plain 404", method: http.MethodGet, path: "/nope", wantStatus: http.StatusNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req, _ := http.NewRequestWithContext(context.Background(), tc.method, r.base+tc.path, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if tc.wantBody != "" {
				got, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
				if string(got) != tc.wantBody {
					t.Errorf("body = %q, want %q", got, tc.wantBody)
				}
				if resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
					t.Errorf("missing hardening headers: %v", resp.Header)
				}
			}
		})
	}
}

func TestOversizedHeadersAreRefused(t *testing.T) {
	t.Parallel()
	r := start(t, Options{})

	resp := get(t, http.DefaultClient, r.base+"/healthz", http.Header{"X-Big": {strings.Repeat("a", 2*maxHeaderBytes)}})
	if resp.Status != http.StatusRequestHeaderFieldsTooLarge {
		t.Errorf("status = %d, want 431", resp.Status)
	}
}

// The listener speaks HTTP/2 without TLS (h2c), which is how gRPC reaches the
// server locally. The client below only knows h2c, so the request fails unless the
// server accepts it.
func TestServesH2C(t *testing.T) {
	t.Parallel()
	r := start(t, Options{})

	p := new(http.Protocols)
	p.SetUnencryptedHTTP2(true)
	client := &http.Client{Transport: &http.Transport{Protocols: p}}

	resp := get(t, client, r.base+"/healthz", nil)
	if resp.ProtoMajor != 2 {
		t.Errorf("proto = %s, want HTTP/2", resp.Proto)
	}
}

func TestTracing(t *testing.T) {
	t.Parallel()

	rec := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider(trace.WithSpanProcessor(rec))
	r := start(t, Options{TracerProvider: tp})

	// Probes must not create traces; a normal request must.
	get(t, http.DefaultClient, r.base+"/healthz", nil)
	get(t, http.DefaultClient, r.base+"/readyz", nil)
	if n := len(rec.Ended()); n != 0 {
		t.Fatalf("health probes created %d spans, want 0", n)
	}

	// A client-supplied traceparent that forces sampling must not become our parent.
	const remoteTrace = "4bf92f3577b34da6a3ce929d0e0e4736"
	hdr := http.Header{"Traceparent": {"00-" + remoteTrace + "-00f067aa0ba902b7-01"}}
	get(t, http.DefaultClient, r.base+"/does-not-exist", hdr)

	// The span ends after the response is written: wait briefly for it.
	var spans []trace.ReadOnlySpan
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if spans = rec.Ended(); len(spans) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	sp := spans[0]

	if sp.Name() != http.MethodGet {
		t.Errorf("span name = %q, want the bare method (the path has unbounded cardinality)", sp.Name())
	}
	if sp.SpanContext().TraceID().String() == remoteTrace {
		t.Error("the caller's trace id was adopted: a client could choose our trace ids")
	}
	if sp.Parent().IsValid() {
		t.Error("the remote span became the parent of ours")
	}
	if len(sp.Links()) != 1 || sp.Links()[0].SpanContext.TraceID().String() != remoteTrace {
		t.Errorf("the remote trace should be kept as a link: %v", sp.Links())
	}
	if sp.SpanKind() != oteltrace.SpanKindServer {
		t.Errorf("kind = %v, want server", sp.SpanKind())
	}
}

func TestGracefulShutdownLetsInFlightRequestsFinish(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	started := make(chan struct{})
	r := start(t, Options{
		Drain:           300 * time.Millisecond,
		ShutdownTimeout: 3 * time.Second,
		Routes: func(mux *http.ServeMux) error {
			mux.HandleFunc("GET /slow", func(w http.ResponseWriter, _ *http.Request) {
				close(started)
				<-release
				w.WriteHeader(http.StatusOK)
			})
			return nil
		},
	})

	slowStatus := make(chan int, 1)
	go func() {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, r.base+"/slow", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			slowStatus <- -1
			return
		}
		_ = resp.Body.Close()
		slowStatus <- resp.StatusCode
	}()
	<-started

	r.stop() // SIGTERM

	// During the drain the server still answers, but is no longer ready.
	deadline := time.Now().Add(2 * time.Second)
	for {
		resp := get(t, http.DefaultClient, r.base+"/readyz", nil)
		if resp.Status == http.StatusServiceUnavailable {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("readiness never turned 503 (status %d)", resp.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}

	close(release)
	if got := <-slowStatus; got != http.StatusOK {
		t.Errorf("in-flight request ended with %d, want 200", got)
	}
	select {
	case <-r.done:
		if r.err != nil {
			t.Errorf("clean shutdown returned %v", r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
}

func TestShutdownTimeoutCancelsStreams(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	cancelled := make(chan struct{})
	r := start(t, Options{
		ShutdownTimeout: 150 * time.Millisecond,
		Routes: func(mux *http.ServeMux) error {
			mux.HandleFunc("GET /stream", func(w http.ResponseWriter, req *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				close(started)
				<-req.Context().Done() // a stream that never ends by itself
				close(cancelled)
			})
			return nil
		},
	})

	go func() {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, r.base+"/stream", nil)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	}()
	<-started

	r.stop()

	select {
	case <-r.done:
		if !errors.Is(r.err, context.DeadlineExceeded) {
			t.Errorf("Run = %v, want a deadline error after the forced close", r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return: a stuck stream blocked the shutdown")
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Error("the handler never saw its context cancelled")
	}
}
