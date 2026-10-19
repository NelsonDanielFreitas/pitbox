package interceptors

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/stretchr/testify/require"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/auth/principal"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/clock"
)

func callStepUp(t *testing.T, i connect.ServerInterceptor, ctx context.Context, spec connect.Spec, h connect.ServerFunc) error {
	t.Helper()
	server := connect.NewServer(i)
	server.Register(connect.Method{Spec: spec, Handler: h})
	return server.Call(ctx, spec.Procedure, &connect.CallInfo{}, nopStream{})
}

func TestStepUpAllowsNonSensitiveProcedures(t *testing.T) {
	t.Parallel()

	i := StepUp(clock.Real(), "/pitbox.identity.v1.UserService/DeleteAccount")
	called := false
	err := callStepUp(t, i, context.Background(), connect.Spec{Procedure: "/pitbox.identity.v1.UserService/ListMyTeams"},
		func(context.Context, connect.Spec, connect.ServerStream) error {
			called = true
			return nil
		})
	require.NoError(t, err)
	require.True(t, called)
}

func TestStepUpAllowsRecentStrongAuth(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	i := StepUp(clock.NewFake(now), "/pitbox.identity.v1.UserService/DeleteAccount")
	ctx := principal.With(context.Background(), principal.Principal{
		UserID:   "u1",
		AuthTime: now.Add(-2 * time.Minute),
		AMR:      []string{"user"},
	})

	called := false
	err := callStepUp(t, i, ctx, connect.Spec{Procedure: "/pitbox.identity.v1.UserService/DeleteAccount"},
		func(context.Context, connect.Spec, connect.ServerStream) error {
			called = true
			return nil
		})
	require.NoError(t, err)
	require.True(t, called)
}

func TestStepUpRejectsOldAuth(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	i := StepUp(clock.NewFake(now), "/pitbox.identity.v1.UserService/DeleteAccount")
	ctx := principal.With(context.Background(), principal.Principal{
		UserID:   "u1",
		AuthTime: now.Add(-10 * time.Minute),
		AMR:      []string{"user"},
	})

	err := callStepUp(t, i, ctx, connect.Spec{Procedure: "/pitbox.identity.v1.UserService/DeleteAccount"},
		func(context.Context, connect.Spec, connect.ServerStream) error { return nil })
	require.Error(t, err)
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	var ce *connect.Error
	require.ErrorAs(t, err, &ce)
	require.Equal(t, msgStepUp, ce.Message())
}

func TestStepUpRejectsWeakFactor(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	i := StepUp(clock.NewFake(now), "/pitbox.identity.v1.UserService/DeleteAccount")
	ctx := principal.With(context.Background(), principal.Principal{
		UserID:   "u1",
		AuthTime: now.Add(-30 * time.Second),
		AMR:      []string{"pwd"},
	})

	err := callStepUp(t, i, ctx, connect.Spec{Procedure: "/pitbox.identity.v1.UserService/DeleteAccount"},
		func(context.Context, connect.Spec, connect.ServerStream) error { return nil })
	require.Error(t, err)
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}

func TestStepUpRejectsMissingPrincipal(t *testing.T) {
	t.Parallel()

	i := StepUp(clock.Real(), "/pitbox.identity.v1.UserService/DeleteAccount")
	err := callStepUp(t, i, context.Background(), connect.Spec{Procedure: "/pitbox.identity.v1.UserService/DeleteAccount"},
		func(context.Context, connect.Spec, connect.ServerStream) error { return nil })
	require.Error(t, err)
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}

func TestStrongFactorNeedsMFANotARecentLogin(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	const create = "/pitbox.identity.v1.TeamService/CreateTeam"
	i := StepUpWithOptions(clock.NewFake(now), StepUpOptions{StrongFactor: []string{create}})

	// A fresh password session must still be refused: it has no MFA.
	weak := principal.With(context.Background(), principal.Principal{
		UserID: "u1", AuthTime: now.Add(-30 * time.Second), AMR: []string{"pwd"},
	})
	err := callStepUp(t, i, weak, connect.Spec{Procedure: create},
		func(context.Context, connect.Spec, connect.ServerStream) error { return nil })
	require.Error(t, err)
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))

	// A strong factor is enough even when the login is old: no recency window.
	strong := principal.With(context.Background(), principal.Principal{
		UserID: "u1", AuthTime: now.Add(-24 * time.Hour), AMR: []string{"user"},
	})
	called := false
	err = callStepUp(t, i, strong, connect.Spec{Procedure: create},
		func(context.Context, connect.Spec, connect.ServerStream) error {
			called = true
			return nil
		})
	require.NoError(t, err)
	require.True(t, called)
}
