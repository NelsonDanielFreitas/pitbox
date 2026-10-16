package interceptors

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"connectrpc.com/connect/v2"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/auth/principal"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/ratelimit"
)

// Fixed public text: the quota and keys never reach the client.
const msgRateLimited = "rate limit exceeded"

// rateLimitDecider is the subset of ratelimit.Limiter used by the interceptor. It keeps
// the interceptor testable without a real Valkey client.
type rateLimitDecider interface {
	Allow(ctx context.Context, scope ratelimit.Scope, id string, write bool) ratelimit.Decision
}

// RateLimit enforces per-user, per-team and per-public-procedure quotas. It sits in slot 6
// of the chain (architecture §4.4): after authentication, before validation, so invalid
// requests that exceed a quota are rejected before spending CPU on protovalidate or the use
// case. For requests whose input message has a team_id field, the interceptor peeks the
// first message to extract the team and applies the team limit before passing the message
// on to the rest of the chain.
func RateLimit(limiter rateLimitDecider) connect.ServerInterceptor {
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			write := spec.IdempotencyLevel != connect.IdempotencyNoSideEffects

			principal, ok := principal.From(ctx)
			if !ok {
				// Public endpoint: no local fallback, fail closed if Valkey is unavailable.
				switch limiter.Allow(ctx, ratelimit.ScopePublic, procedureHash(spec.Procedure), write) {
				case ratelimit.RateLimited:
					return connect.NewError(connect.CodeResourceExhausted, msgRateLimited)
				case ratelimit.Unavailable:
					return connect.NewError(connect.CodeUnavailable, msgRateLimited)
				}
				return next(ctx, spec, stream)
			}

			// Authenticated: first the user limit.
			switch limiter.Allow(ctx, ratelimit.ScopeUser, principal.UserID, write) {
			case ratelimit.RateLimited:
				return connect.NewError(connect.CodeResourceExhausted, msgRateLimited)
			case ratelimit.Unavailable:
				return connect.NewError(connect.CodeUnavailable, msgRateLimited)
			}

			// Then the team limit, if the procedure's input carries a team_id.
			if teamID, wrapped, hasTeam := peekTeamID(spec, stream); hasTeam {
				switch limiter.Allow(ctx, ratelimit.ScopeTeam, teamID, write) {
				case ratelimit.RateLimited:
					return connect.NewError(connect.CodeResourceExhausted, msgRateLimited)
				case ratelimit.Unavailable:
					return connect.NewError(connect.CodeUnavailable, msgRateLimited)
				}
				stream = wrapped
			}

			return next(ctx, spec, stream)
		}
	}
}

// peekTeamID returns the team_id from the first message of the stream when the procedure's
// input message has a team_id field. It also returns a wrapped stream that replays the
// peeked message to the rest of the chain so validation and the handler still see it.
//
// If the message cannot be peeked (e.g., malformed body), the wrapped stream propagates the
// error on the next Receive; the team limit is skipped so validation can produce the proper
// client error.
func peekTeamID(spec connect.Spec, stream connect.ServerStream) (string, connect.ServerStream, bool) {
	desc, err := requestDescriptor(spec.Procedure)
	if err != nil {
		return "", nil, false
	}
	if desc.Fields().ByName("team_id") == nil {
		return "", nil, false
	}

	wrapped := newPeekingStream(stream, desc)
	msg, err := wrapped.peek()
	if err != nil {
		// The error will be replayed by the wrapper; do not apply a team limit without data.
		return "", wrapped, false
	}

	teamID, ok := extractTeamID(msg)
	return teamID, wrapped, ok
}

// requestDescriptor derives the input message descriptor of a Connect procedure from its
// name. The convention is `/package.Service/Method` → `package.MethodRequest`.
func requestDescriptor(procedure string) (protoreflect.MessageDescriptor, error) {
	parts := strings.Split(procedure, "/")
	if len(parts) != 3 || parts[0] != "" || parts[1] == "" || parts[2] == "" {
		return nil, fmt.Errorf("invalid procedure %q", procedure)
	}
	svc := parts[1]
	method := parts[2]
	lastDot := strings.LastIndex(svc, ".")
	if lastDot == -1 {
		return nil, fmt.Errorf("invalid service name %q", svc)
	}
	pkg := svc[:lastDot]
	fullName := protoreflect.FullName(pkg + "." + method + "Request")

	desc, err := protoregistry.GlobalFiles.FindDescriptorByName(fullName)
	if err != nil {
		return nil, fmt.Errorf("lookup %s: %w", fullName, err)
	}
	msgDesc, ok := desc.(protoreflect.MessageDescriptor)
	if !ok {
		return nil, fmt.Errorf("descriptor %s is not a message", fullName)
	}
	return msgDesc, nil
}

func extractTeamID(msg proto.Message) (string, bool) {
	r := msg.ProtoReflect()
	field := r.Descriptor().Fields().ByName("team_id")
	if field == nil || field.Kind() != protoreflect.StringKind {
		return "", false
	}
	v := r.Get(field).String()
	return v, v != ""
}

// peekingStream caches the first Receive so the interceptor can inspect the request before
// passing it down the chain. Subsequent receives pass through to the underlying stream.
type peekingStream struct {
	connect.ServerStream
	desc protoreflect.MessageDescriptor

	mu    sync.Mutex
	done  bool
	first proto.Message
	err   error
}

func newPeekingStream(stream connect.ServerStream, desc protoreflect.MessageDescriptor) *peekingStream {
	return &peekingStream{ServerStream: stream, desc: desc}
}

// peek reads the first message into a dynamic message. It is safe to call more than once.
func (s *peekingStream) peek() (proto.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return s.first, s.err
	}
	s.done = true
	s.first = dynamicpb.NewMessage(s.desc)
	s.err = s.ServerStream.Receive(s.first)
	return s.first, s.err
}

// Receive returns the cached first message once, then passes through to the underlying stream.
func (s *peekingStream) Receive(msg any) error {
	s.mu.Lock()
	if !s.done {
		s.done = true
		s.first = dynamicpb.NewMessage(s.desc)
		s.err = s.ServerStream.Receive(s.first)
		s.mu.Unlock()
		if s.err != nil {
			return s.err
		}
		return mergeInto(msg, s.first)
	}
	if s.first != nil {
		first := s.first
		err := s.err
		s.first = nil
		s.err = nil
		s.mu.Unlock()
		if err != nil {
			return err
		}
		return mergeInto(msg, first)
	}
	s.mu.Unlock()
	return s.ServerStream.Receive(msg)
}

func mergeInto(dst any, src proto.Message) error {
	target, ok := dst.(proto.Message)
	if !ok {
		return fmt.Errorf("rate limit peek: target %T is not a proto.Message", dst)
	}
	proto.Reset(target)
	proto.Merge(target, src)
	return nil
}

func procedureHash(procedure string) string {
	h := sha256.Sum256([]byte(procedure))
	return hex.EncodeToString(h[:8])
}
