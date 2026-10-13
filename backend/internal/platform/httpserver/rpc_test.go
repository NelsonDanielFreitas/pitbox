package httpserver

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
)

func registerSpecs(server *connect.Server, procedures ...string) {
	noop := func(connect.ServerStream) error { return nil }
	for _, p := range procedures {
		server.Register(connect.Method{
			Spec:    connect.Spec{StreamType: connect.StreamTypeUnary, Procedure: p},
			Handler: func(_ context.Context, _ connect.Spec, s connect.ServerStream) error { return noop(s) },
		})
	}
}

func TestMountRPCRejectsNonPositiveDurations(t *testing.T) {
	t.Parallel()

	for _, o := range []RPCOptions{{}, {MaxUnaryTimeout: time.Second}, {MaxStreamDuration: time.Second}, {MaxUnaryTimeout: -1, MaxStreamDuration: time.Second}} {
		if err := MountRPC(http.NewServeMux(), connect.NewServer(), o); err == nil {
			t.Errorf("MountRPC(%+v) should fail", o)
		}
	}
}

func TestMountRPCMountsTheProcedures(t *testing.T) {
	t.Parallel()

	server := connect.NewServer()
	registerSpecs(server, "/pitbox.a.v1.AService/One", "/pitbox.a.v1.AService/Two", "/pitbox.b.v1.BService/One")
	mux := http.NewServeMux()
	if err := MountRPC(mux, server, RPCOptions{MaxUnaryTimeout: time.Second, MaxStreamDuration: time.Minute}); err != nil {
		t.Fatalf("MountRPC: %v", err)
	}
	for _, p := range []string{"/pitbox.a.v1.AService/One", "/pitbox.b.v1.BService/One"} {
		_, pattern := mux.Handler(&http.Request{Method: http.MethodPost, URL: mustURL(t, p), Host: "x"})
		if pattern == "" {
			t.Errorf("%s is not mounted", p)
		}
	}
}

func TestServicePrefixes(t *testing.T) {
	t.Parallel()

	server := connect.NewServer()
	registerSpecs(server, "/pitbox.a.v1.AService/One", "/pitbox.a.v1.AService/Two", "/pitbox.b.v1.BService/One")

	got := ServicePrefixes(server)
	slices.Sort(got)
	want := []string{"/pitbox.a.v1.AService/", "/pitbox.b.v1.BService/"}
	if !slices.Equal(got, want) {
		t.Errorf("ServicePrefixes = %v, want %v", got, want)
	}
}

func mustURL(t *testing.T, path string) *url.URL {
	t.Helper()
	u, err := url.Parse(path)
	if err != nil {
		t.Fatalf("parse %q: %v", path, err)
	}
	return u
}
