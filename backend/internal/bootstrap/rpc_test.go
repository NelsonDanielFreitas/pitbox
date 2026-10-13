package bootstrap

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	identityv1 "github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/identity/v1"
	"github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/identity/v1/identityv1connect"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/config"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/httpserver"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/interceptors"
	platformlog "github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/log"
)

// fakeTeams is a TeamService whose behaviour each test chooses. It counts the calls
// that reach the service implementation, which is how a test proves that a request was
// stopped before it.
type fakeTeams struct {
	identityv1connect.UnimplementedTeamServiceHandler
	calls  atomic.Int32
	create func(ctx context.Context, req *identityv1.CreateTeamRequest) (*identityv1.CreateTeamResponse, error)
}

func (f *fakeTeams) CreateTeam(ctx context.Context, req *identityv1.CreateTeamRequest) (*identityv1.CreateTeamResponse, error) {
	f.calls.Add(1)
	if f.create == nil {
		return &identityv1.CreateTeamResponse{}, nil
	}
	return f.create(ctx, req)
}

type harness struct {
	url    string
	client identityv1connect.TeamServiceClient
	http   *http.Client
	logs   *syncBuffer
	spans  *tracetest.SpanRecorder
	teams  *fakeTeams
}

// newHarness assembles the real production wiring (newRPCServer, mountRPC, the HTTP
// server with its trace middleware) around a fake service, behind an httptest server.
func newHarness(t *testing.T, env map[string]string, teams *fakeTeams, extra ...func(*connect.Server)) *harness {
	t.Helper()
	cfg, err := config.LoadFrom(env)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	logs := &syncBuffer{}
	logger := platformlog.New(logs, slog.LevelDebug) // the per-RPC line of a success is debug
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))

	register := append([]func(*connect.Server){func(s *connect.Server) {
		identityv1connect.RegisterTeamServiceHandler(s, teams)
	}}, extra...)
	api, err := newRPCServer(cfg, logger, tp, register...)
	if err != nil {
		t.Fatalf("newRPCServer: %v", err)
	}
	reflection := newReflectionServer(cfg, logger, api)
	srv, err := httpserver.New(httpserver.Options{
		Logger: logger, TracerProvider: tp, ShutdownTimeout: time.Second,
		SkipTracing: rpcPaths(api, reflection),
		Routes:      func(mux *http.ServeMux) error { return mountRPC(mux, cfg, api, reflection) },
	})
	if err != nil {
		t.Fatalf("httpserver.New: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	return &harness{
		url: ts.URL, http: ts.Client(), logs: logs, spans: rec, teams: teams,
		client: identityv1connect.NewTeamServiceClient(connect.NewClient(connecthttp.NewTransport(ts.Client(), ts.URL))),
	}
}

func validRequest() *identityv1.CreateTeamRequest {
	return &identityv1.CreateTeamRequest{
		RequestId: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60", Name: "Equipa Nitro", CountryCode: "PT",
	}
}

func codeAndMessage(t *testing.T, err error) (connect.Code, string) {
	t.Helper()
	var ce *connect.Error
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v (%T), want a *connect.Error", err, err)
	}
	return ce.Code(), ce.Message()
}

// An internal error reaches the client as a code only: no message from the cause. This
// is the rule of ADR 0012, proved over a real HTTP round trip.
func TestInternalErrorsNeverReachTheClient(t *testing.T) {
	t.Parallel()

	h := newHarness(t, nil, &fakeTeams{create: func(context.Context, *identityv1.CreateTeamRequest) (*identityv1.CreateTeamResponse, error) {
		return nil, fmt.Errorf("pg: connection refused: host=db.internal password=hunter2")
	}})

	_, err := h.client.CreateTeam(context.Background(), validRequest())

	code, msg := codeAndMessage(t, err)
	if code != connect.CodeInternal || msg != "internal error" {
		t.Errorf("client saw %v %q, want internal %q", code, msg, "internal error")
	}
	for _, leak := range []string{"pg:", "hunter2", "db.internal", "connection refused"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("the client error %q leaks %q", err, leak)
		}
	}

	// The cause is in the log, with the trace id of the request.
	var found bool
	for _, r := range h.logs.records(t) {
		if r["msg"] == "rpc" && r["code"] == "internal" {
			found = true
			if !strings.Contains(fmt.Sprint(r["cause"]), "pg: connection refused") {
				t.Errorf("the cause is not in the log: %v", r)
			}
			if r["trace_id"] == nil || r["trace_id"] == "" {
				t.Errorf("the log line has no trace_id: %v", r)
			}
		}
	}
	if !found {
		t.Errorf("no rpc log line: %s", h.logs.String())
	}
}

func TestAuthoredErrorsKeepTheirCodeAndMessage(t *testing.T) {
	t.Parallel()

	h := newHarness(t, nil, &fakeTeams{create: func(context.Context, *identityv1.CreateTeamRequest) (*identityv1.CreateTeamResponse, error) {
		return nil, fmt.Errorf("get team: %w", connect.NewError(connect.CodeNotFound, "team not found"))
	}})

	_, err := h.client.CreateTeam(context.Background(), validRequest())

	code, msg := codeAndMessage(t, err)
	if code != connect.CodeNotFound || msg != "team not found" {
		t.Errorf("client saw %v %q", code, msg)
	}
}

func TestAPanicIsContainedAndTheServerKeepsServing(t *testing.T) {
	t.Parallel()

	var n atomic.Int32
	h := newHarness(t, nil, &fakeTeams{create: func(context.Context, *identityv1.CreateTeamRequest) (*identityv1.CreateTeamResponse, error) {
		if n.Add(1) == 1 {
			panic("boom: token=abc123")
		}
		return &identityv1.CreateTeamResponse{}, nil
	}})

	_, err := h.client.CreateTeam(context.Background(), validRequest())
	code, msg := codeAndMessage(t, err)
	if code != connect.CodeInternal || msg != "internal error" || strings.Contains(err.Error(), "abc123") {
		t.Errorf("client saw %v %q (%v)", code, msg, err)
	}
	if _, err := h.client.CreateTeam(context.Background(), validRequest()); err != nil {
		t.Errorf("the next call failed: %v", err)
	}

	var logged bool
	for _, r := range h.logs.records(t) {
		if r["msg"] == "panic in rpc handler" {
			logged = true
			if r["trace_id"] == nil || r["stack"] == nil {
				t.Errorf("the panic log lacks trace_id or stack: %v", r)
			}
		}
	}
	if !logged {
		t.Error("the panic was not logged")
	}
	// The span is marked as failed, which is why tracing wraps recovery.
	var failed bool
	for _, s := range h.spans.Ended() {
		if s.SpanKind() == oteltrace.SpanKindServer && s.Status().Code != 0 && strings.HasSuffix(s.Name(), "CreateTeam") {
			failed = true
		}
	}
	if !failed {
		t.Error("the RPC span does not show the failure")
	}
}

func TestMessageSizeLimit(t *testing.T) {
	t.Parallel()

	const limit = httpserver.MaxMessageBytes
	tests := []struct {
		name     string
		nameLen  int
		gzip     bool
		wantCode connect.Code
	}{
		// The oversized name breaks the "max 80 characters" rule too: ResourceExhausted
		// (not InvalidArgument) proves the size limit acts first.
		{name: "above the limit", nameLen: limit + 1024, wantCode: connect.CodeResourceExhausted},
		{name: "just below the limit passes the transport and fails validation", nameLen: limit - 1024, wantCode: connect.CodeInvalidArgument},
		// 5 MiB of one byte compresses to a few KiB on the wire.
		{name: "compression bomb", nameLen: 5 << 20, gzip: true, wantCode: connect.CodeResourceExhausted},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, nil, &fakeTeams{})

			// The client's own send limit is lifted: it is the server's limit under test.
			opts := []connecthttp.Option{connecthttp.WithSendMaxBytes(0)}
			if tc.gzip {
				opts = append(opts, connecthttp.WithSendGzip(), connecthttp.WithCompressMinBytes(1))
			}
			client := identityv1connect.NewTeamServiceClient(connect.NewClient(connecthttp.NewTransport(h.http, h.url, opts...)))

			req := validRequest()
			req.Name = strings.Repeat("a", tc.nameLen)
			_, err := client.CreateTeam(context.Background(), req)

			if code, _ := codeAndMessage(t, err); code != tc.wantCode {
				t.Errorf("code = %v (%v), want %v", code, err, tc.wantCode)
			}
			if got := h.teams.calls.Load(); got != 0 {
				t.Errorf("the service ran %d times, want 0", got)
			}
		})
	}
}

func TestValidationStopsInvalidRequestsAndEchoesNothing(t *testing.T) {
	t.Parallel()

	h := newHarness(t, nil, &fakeTeams{})
	req := validRequest()
	req.Name = strings.Repeat("S3NT1NEL", 11) // 88 characters: breaks "max 80"

	_, err := h.client.CreateTeam(context.Background(), req)

	code, msg := codeAndMessage(t, err)
	if code != connect.CodeInvalidArgument {
		t.Fatalf("code = %v (%v), want invalid_argument", code, err)
	}
	if !strings.Contains(msg, "name") {
		t.Errorf("the message should name the field: %q", msg)
	}
	var ce *connect.Error
	_ = errors.As(err, &ce)
	if len(ce.Details()) == 0 {
		t.Error("no violation detail")
	}
	all := msg
	for _, d := range ce.Details() {
		all += d.Type + string(d.Value) + string(d.Debug)
	}
	if strings.Contains(all, "S3NT1NEL") {
		t.Errorf("the sent value came back in the error: %q", all)
	}
	if got := h.teams.calls.Load(); got != 0 {
		t.Errorf("the service ran %d times for an invalid request", got)
	}
}

func TestDeadlines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clientWait  time.Duration // 0 = the client sets no deadline
		wantAtMost  time.Duration
		wantAtLeast time.Duration
	}{
		{name: "no client deadline gets the default", wantAtMost: 2 * time.Second, wantAtLeast: time.Second},
		{name: "a shorter client deadline is kept", clientWait: 1500 * time.Millisecond, wantAtMost: 1500 * time.Millisecond, wantAtLeast: 500 * time.Millisecond},
		{name: "a longer client deadline is cut", clientWait: 10 * time.Minute, wantAtMost: 2 * time.Second, wantAtLeast: time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var remaining atomic.Int64
			h := newHarness(t, map[string]string{"PITBOX_RPC_MAX_TIMEOUT": "2s"}, &fakeTeams{create: func(ctx context.Context, _ *identityv1.CreateTeamRequest) (*identityv1.CreateTeamResponse, error) {
				d, ok := ctx.Deadline()
				if !ok {
					return nil, errors.New("no deadline")
				}
				remaining.Store(int64(time.Until(d)))
				return &identityv1.CreateTeamResponse{}, nil
			}})

			ctx := context.Background()
			if tc.clientWait > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.clientWait)
				defer cancel()
			}
			if _, err := h.client.CreateTeam(ctx, validRequest()); err != nil {
				t.Fatalf("CreateTeam: %v", err)
			}
			if got := time.Duration(remaining.Load()); got > tc.wantAtMost || got < tc.wantAtLeast {
				t.Errorf("the service saw a deadline in %s, want between %s and %s", got, tc.wantAtLeast, tc.wantAtMost)
			}
		})
	}
}

// A client that sends its body slowly must not hold the connection past the unary
// ceiling: the context deadline alone would not interrupt a blocked body read.
func TestASlowBodyIsCutAtTheCeiling(t *testing.T) {
	t.Parallel()

	h := newHarness(t, map[string]string{"PITBOX_RPC_MAX_TIMEOUT": "1s"}, &fakeTeams{})
	conn, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", strings.TrimPrefix(h.url, "http://"))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Promise 1000 bytes, send 10, then stall.
	_, err = fmt.Fprintf(conn, "POST %s HTTP/1.1\r\nHost: x\r\nContent-Type: application/proto\r\nConnect-Protocol-Version: 1\r\nContent-Length: 1000\r\n\r\n0123456789",
		identityv1connect.TeamServiceCreateTeamProcedure)
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	start := time.Now()
	_ = conn.SetReadDeadline(time.Now().Add(8 * time.Second))
	_, _ = bufio.NewReader(conn).ReadString('\n') // a status line, or EOF when the server closes
	if took := time.Since(start); took > 4*time.Second {
		t.Errorf("the connection was held for %s with a 1s ceiling", took)
	}
	if got := h.teams.calls.Load(); got != 0 {
		t.Errorf("the service ran %d times", got)
	}
}

func TestAuditMetadataForWrites(t *testing.T) {
	t.Parallel()

	var got interceptors.AuditMeta
	var ok bool
	h := newHarness(t, nil, &fakeTeams{create: func(ctx context.Context, _ *identityv1.CreateTeamRequest) (*identityv1.CreateTeamResponse, error) {
		got, ok = interceptors.AuditMetaFromContext(ctx)
		return &identityv1.CreateTeamResponse{}, nil
	}})
	ctx, info := connect.NewClientContext(context.Background())
	info.RequestHeader().Set("User-Agent", strings.Repeat("é", 300))
	// A forged forwarding header must not become the audited address.
	info.RequestHeader().Set("X-Forwarded-For", "198.51.100.99")
	info.RequestHeader().Set("CF-Connecting-IP", "198.51.100.98")

	if _, err := h.client.CreateTeam(ctx, validRequest()); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	if !ok {
		t.Fatal("no audit metadata for a write")
	}
	if got.IP.String() != "127.0.0.1" {
		t.Errorf("IP = %v, want the TCP peer 127.0.0.1 (headers are not trusted)", got.IP)
	}
	if len(got.UserAgent) > 256 || len(got.UserAgent) < 200 {
		t.Errorf("user agent length = %d, want it cut at 256 bytes", len(got.UserAgent))
	}
	var traceID string
	for _, s := range h.spans.Ended() {
		if s.SpanKind() == oteltrace.SpanKindServer {
			traceID = s.SpanContext().TraceID().String()
		}
	}
	if got.TraceID == "" || got.TraceID != traceID {
		t.Errorf("TraceID = %q, want the RPC span's %q", got.TraceID, traceID)
	}
}

func TestReadsAreNotAudited(t *testing.T) {
	t.Parallel()

	var present atomic.Bool
	spec := connect.Spec{
		StreamType: connect.StreamTypeUnary, IdempotencyLevel: connect.IdempotencyNoSideEffects,
		Procedure: "/pitbox.identity.v1.TeamService/Peek",
		Schema:    identityv1.File_pitbox_identity_v1_team_service_proto.Services().ByName("TeamService").Methods().ByName("ListMyTeams"),
	}
	h := newHarness(t, nil, &fakeTeams{}, func(s *connect.Server) {
		s.Register(connect.Method{Spec: spec, Handler: func(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
			var req identityv1.ListMyTeamsRequest
			if err := stream.Receive(&req); err != nil {
				return err
			}
			_, has := interceptors.AuditMetaFromContext(ctx)
			present.Store(has)
			return stream.Send(&identityv1.ListMyTeamsResponse{})
		}})
	})
	client := connect.NewClient(connecthttp.NewTransport(h.http, h.url))

	if err := client.CallUnary(context.Background(), spec, &identityv1.ListMyTeamsRequest{}, &identityv1.ListMyTeamsResponse{}); err != nil {
		t.Fatalf("call: %v", err)
	}
	if present.Load() {
		t.Error("a NO_SIDE_EFFECTS RPC got audit metadata")
	}
}

// otelconnect, not otelhttp, traces an RPC, and a forced sampling decision from the
// caller does not become our parent.
func TestRPCsGetOneServerSpanAndDoNotTrustTheCaller(t *testing.T) {
	t.Parallel()

	h := newHarness(t, nil, &fakeTeams{})
	const remoteTrace = "4bf92f3577b34da6a3ce929d0e0e4736"
	ctx, info := connect.NewClientContext(context.Background())
	info.RequestHeader().Set("Traceparent", "00-"+remoteTrace+"-00f067aa0ba902b7-01")

	if _, err := h.client.CreateTeam(ctx, validRequest()); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	var servers []sdktrace.ReadOnlySpan
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(servers) == 0 {
		servers = servers[:0]
		for _, s := range h.spans.Ended() {
			if s.SpanKind() == oteltrace.SpanKindServer {
				servers = append(servers, s)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(servers) != 1 {
		t.Fatalf("server spans = %d, want exactly 1 (otelconnect)", len(servers))
	}
	s := servers[0]
	if !strings.HasSuffix(s.Name(), "TeamService/CreateTeam") {
		t.Errorf("span name = %q", s.Name())
	}
	if s.SpanContext().TraceID().String() == remoteTrace || s.Parent().IsValid() {
		t.Error("the caller's trace was adopted as our parent")
	}
	if len(s.Links()) != 1 || s.Links()[0].SpanContext.TraceID().String() != remoteTrace {
		t.Errorf("the remote trace should be a link: %v", s.Links())
	}
}

// A method that is not registered answers Unimplemented without entering the chain:
// no per-RPC log line and no span, so a scanner cannot create log lines or span names.
func TestUnknownProceduresAreUnimplementedAndQuiet(t *testing.T) {
	t.Parallel()

	h := newHarness(t, nil, &fakeTeams{})
	client := connect.NewClient(connecthttp.NewTransport(h.http, h.url))
	spec := connect.Spec{StreamType: connect.StreamTypeUnary, Procedure: "/pitbox.identity.v1.TeamService/Nope"}

	err := client.CallUnary(context.Background(), spec, &identityv1.ListMyTeamsRequest{}, &identityv1.ListMyTeamsResponse{})

	if code, _ := codeAndMessage(t, err); code != connect.CodeUnimplemented {
		t.Errorf("code = %v (%v), want unimplemented", code, err)
	}
	for _, r := range h.logs.records(t) {
		if r["msg"] == "rpc" {
			t.Errorf("an unknown procedure produced an rpc log line: %v", r)
		}
	}
}

// Every request type of every pitbox service must be in the validator's list: the
// validator refuses types it does not know, so a gap would reject every call to the
// service. This test makes the gap fail at build time instead.
func TestRequestMessagesCoverEveryService(t *testing.T) {
	t.Parallel()

	listed := make(map[protoreflect.FullName]bool)
	for _, m := range requestMessages() {
		listed[m.ProtoReflect().Descriptor().FullName()] = true
	}
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if !strings.HasPrefix(string(fd.Package()), "pitbox.") {
			return true
		}
		for i := 0; i < fd.Services().Len(); i++ {
			svc := fd.Services().Get(i)
			for j := 0; j < svc.Methods().Len(); j++ {
				in := svc.Methods().Get(j).Input().FullName()
				if !listed[in] {
					t.Errorf("%s: request type %s is missing from requestMessages()", svc.FullName(), in)
				}
			}
		}
		return true
	})
}

func TestReflectionIsLocalOnly(t *testing.T) {
	t.Parallel()

	const path = "/grpc.reflection.v1.ServerReflection/ServerReflectionInfo"
	tests := []struct {
		env       string
		wantMount bool
	}{
		{env: "local", wantMount: true},
		{env: "preview"}, {env: "staging"}, {env: "production"},
	}
	for _, tc := range tests {
		t.Run(tc.env, func(t *testing.T) {
			t.Parallel()
			env := map[string]string{"PITBOX_ENV": tc.env}
			if tc.env != "local" {
				// Authentication is mandatory outside local: a complete (fake) setup.
				for k, v := range map[string]string{
					"PITBOX_PUBLIC_URL": "https://api.example.eu", "PITBOX_OIDC_ISSUER": "https://auth.example.eu",
					"PITBOX_OIDC_CLIENT_ID": "client-id", "PITBOX_OIDC_CLIENT_SECRET": "client-secret", //nolint:gosec // fake value
					"PITBOX_AUTH_KEYS":       "k1:AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=",
					"PITBOX_VALKEY_PASSWORD": "valkey-password", "PITBOX_VALKEY_TLS": "true", //nolint:gosec // fake value
				} {
					env[k] = v
				}
			}
			h := newHarness(t, env, &fakeTeams{})

			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, h.url+path, strings.NewReader(""))
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Header.Set("Content-Type", "application/grpc")
			resp, err := h.http.Do(req)
			if err != nil {
				t.Fatalf("POST: %v", err)
			}
			_ = resp.Body.Close()
			if mounted := resp.StatusCode != http.StatusNotFound; mounted != tc.wantMount {
				t.Errorf("env %s: status %d, reflection mounted = %v, want %v", tc.env, resp.StatusCode, mounted, tc.wantMount)
			}
		})
	}
}
