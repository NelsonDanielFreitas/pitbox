package interceptors

import (
	"context"
	"io"
	"testing"

	"connectrpc.com/connect/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	identityv1 "github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/identity/v1"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/auth/principal"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/ratelimit"
)

// fakeDecider records decisions and returns programmed answers per key.
type fakeDecider struct {
	calls []deciderCall
	rules map[string]ratelimit.Decision
}

type deciderCall struct {
	scope ratelimit.Scope
	id    string
	write bool
}

func (f *fakeDecider) Allow(_ context.Context, scope ratelimit.Scope, id string, write bool) ratelimit.Decision {
	f.calls = append(f.calls, deciderCall{scope: scope, id: id, write: write})
	if d, ok := f.rules[id]; ok {
		return d
	}
	return ratelimit.Allowed
}

func TestRateLimitAppliesUserLimit(t *testing.T) {
	t.Parallel()

	d := &fakeDecider{rules: map[string]ratelimit.Decision{"user-1": ratelimit.RateLimited}}
	i := RateLimit(d)
	ctx := principal.With(context.Background(), principal.Principal{UserID: "user-1"})

	err := callUnary(t, i, unarySpec, ctx, &identityv1.ListMyTeamsRequest{}, func(context.Context, connect.Spec, connect.ServerStream) error { return nil })
	require.Error(t, err)
	ce := &connect.Error{}
	require.ErrorAs(t, err, &ce)
	require.Equal(t, connect.CodeResourceExhausted, ce.Code())
	require.Equal(t, "rate limit exceeded", ce.Message())
	require.Len(t, d.calls, 1)
	require.Equal(t, ratelimit.ScopeUser, d.calls[0].scope)
	require.Equal(t, "user-1", d.calls[0].id)
}

func TestRateLimitPublicUnavailable(t *testing.T) {
	t.Parallel()

	d := &fakeDecider{rules: map[string]ratelimit.Decision{procedureHash(unarySpec.Procedure): ratelimit.Unavailable}}
	i := RateLimit(d)

	err := call(t, i, unarySpec, nil, func(context.Context, connect.Spec, connect.ServerStream) error { return nil })
	require.Error(t, err)
	require.Equal(t, connect.CodeUnavailable, connect.CodeOf(err))
	require.Len(t, d.calls, 1)
	require.Equal(t, ratelimit.ScopePublic, d.calls[0].scope)
}

func TestRateLimitSkipsTeamLimitWhenNoTeamID(t *testing.T) {
	t.Parallel()

	d := &fakeDecider{}
	i := RateLimit(d)
	ctx := principal.With(context.Background(), principal.Principal{UserID: "user-1"})

	spec := connect.Spec{StreamType: connect.StreamTypeUnary, Procedure: "/pitbox.identity.v1.TeamService/ListMyTeams"}
	err := callUnary(t, i, spec, ctx, &identityv1.ListMyTeamsRequest{}, func(context.Context, connect.Spec, connect.ServerStream) error {
		return nil
	})
	require.NoError(t, err)
	require.Len(t, d.calls, 1)
	require.Equal(t, ratelimit.ScopeUser, d.calls[0].scope)
}

// callUnary sends a single request message through a one-handler Connect server.
func callUnary(t *testing.T, i connect.ServerInterceptor, spec connect.Spec, ctx context.Context, req proto.Message, h connect.ServerFunc) error {
	t.Helper()
	server := connect.NewServer(i)
	server.Register(connect.Method{Spec: spec, Handler: h})
	if ctx == nil {
		ctx = context.Background()
	}
	return server.Call(ctx, spec.Procedure, &connect.CallInfo{}, &singleMessageStream{req: req})
}

type singleMessageStream struct {
	nopStream
	req      proto.Message
	received bool
}

func (s *singleMessageStream) Receive(msg any) error {
	if s.received {
		return io.EOF
	}
	s.received = true
	return mergeInto(msg, s.req)
}

func (s *singleMessageStream) Send(msg any) error {
	_ = msg
	return nil
}

func TestExtractTeamID(t *testing.T) {
	t.Parallel()

	desc := testTeamDescriptor(t)
	msg := dynamicpb.NewMessage(desc)
	msg.Set(desc.Fields().ByName("team_id"), protoreflect.ValueOfString("team-1"))

	id, ok := extractTeamID(msg)
	require.True(t, ok)
	require.Equal(t, "team-1", id)

	empty := dynamicpb.NewMessage(desc)
	_, ok = extractTeamID(empty)
	require.False(t, ok)
}

type errorStream struct {
	nopStream
	err error
}

func (s *errorStream) Receive(msg any) error {
	_ = msg
	return s.err
}

func TestPeekingStreamPropagatesReceiveError(t *testing.T) {
	t.Parallel()

	desc := testTeamDescriptor(t)
	wantErr := context.Canceled
	peek := newPeekingStream(&errorStream{err: wantErr}, desc)

	_, err := peek.peek()
	require.ErrorIs(t, err, wantErr)

	msg := dynamicpb.NewMessage(desc)
	err = peek.Receive(msg)
	require.ErrorIs(t, err, wantErr)
}

func TestPeekingStreamReplaysFirstMessage(t *testing.T) {
	t.Parallel()

	desc := testTeamDescriptor(t)
	original := dynamicpb.NewMessage(desc)
	original.Set(desc.Fields().ByName("team_id"), protoreflect.ValueOfString("team-1"))

	base := &singleMessageStream{req: original}
	peek := newPeekingStream(base, desc)

	first, err := peek.peek()
	require.NoError(t, err)
	require.Equal(t, "team-1", first.ProtoReflect().Get(desc.Fields().ByName("team_id")).String())

	second := dynamicpb.NewMessage(desc)
	err = peek.Receive(second)
	require.NoError(t, err)
	require.Equal(t, "team-1", second.Get(desc.Fields().ByName("team_id")).String())

	third := dynamicpb.NewMessage(desc)
	err = peek.Receive(third)
	require.Equal(t, io.EOF, err)
}

func testTeamDescriptor(t *testing.T) protoreflect.MessageDescriptor {
	t.Helper()
	fd, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:    proto.String("test.proto"),
		Package: proto.String("pitbox.test.v1"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("TeamRequest"),
			Field: []*descriptorpb.FieldDescriptorProto{{
				Name:   proto.String("team_id"),
				Number: proto.Int32(1),
				Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
			}},
		}},
	}, nil)
	require.NoError(t, err)
	return fd.Messages().ByName("TeamRequest")
}
