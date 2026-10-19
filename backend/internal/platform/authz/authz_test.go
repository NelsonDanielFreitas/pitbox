package authz_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/authz"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCheck(t *testing.T) {
	t.Parallel()
	ofga := testutil.StartOpenFGA(t)
	ctx := context.Background()
	c := newClient(t, ctx, ofga)

	owner := userID()
	team := fmt.Sprintf("team:%s", uuid.NewString())
	require.NoError(t, c.SetTeamRole(ctx, owner, team, rolePtr("owner")))

	require.NoError(t, c.Check(ctx, owner, "owner", team), "owner should own the team")
	require.NoError(t, c.Check(ctx, owner, "mechanic", team), "owner is also mechanic")
	require.NoError(t, c.Check(ctx, owner, "member", team), "owner is also member")

	other := userID()
	require.ErrorIs(t, c.Check(ctx, other, "owner", team), authz.ErrDenied)
	require.ErrorIs(t, c.Check(ctx, other, "member", team), authz.ErrDenied)
}

func TestCheckRequiresExistingTuple(t *testing.T) {
	t.Parallel()
	ofga := testutil.StartOpenFGA(t)
	ctx := context.Background()
	c := newClient(t, ctx, ofga)

	user := userID()
	team := fmt.Sprintf("team:%s", uuid.NewString())
	require.ErrorIs(t, c.Check(ctx, user, "owner", team), authz.ErrDenied)
}

func TestListObjects(t *testing.T) {
	t.Parallel()
	ofga := testutil.StartOpenFGA(t)
	ctx := context.Background()
	c := newClient(t, ctx, ofga)

	owner := userID()
	teamA := fmt.Sprintf("team:%s", uuid.NewString())
	teamB := fmt.Sprintf("team:%s", uuid.NewString())
	require.NoError(t, c.SetTeamRole(ctx, owner, teamA, rolePtr("owner")))
	require.NoError(t, c.SetTeamRole(ctx, owner, teamB, rolePtr("owner")))

	objects, err := c.ListObjects(ctx, owner, "owner", "team")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{teamA, teamB}, objects)

	// A user with no tuples gets an empty list, not an error.
	other := userID()
	objects, err = c.ListObjects(ctx, other, "owner", "team")
	require.NoError(t, err)
	require.Empty(t, objects)
}

func TestSetTeamRoleRemovesAccess(t *testing.T) {
	t.Parallel()
	ofga := testutil.StartOpenFGA(t)
	ctx := context.Background()
	c := newClient(t, ctx, ofga)

	owner := userID()
	team := fmt.Sprintf("team:%s", uuid.NewString())
	require.NoError(t, c.SetTeamRole(ctx, owner, team, rolePtr("owner")))
	require.NoError(t, c.Check(ctx, owner, "owner", team))

	require.NoError(t, c.SetTeamRole(ctx, owner, team, nil))
	require.ErrorIs(t, c.Check(ctx, owner, "owner", team), authz.ErrDenied)
	require.ErrorIs(t, c.Check(ctx, owner, "member", team), authz.ErrDenied)
}

func TestSetTeamRoleTransitions(t *testing.T) {
	t.Parallel()
	ofga := testutil.StartOpenFGA(t)
	ctx := context.Background()
	c := newClient(t, ctx, ofga)

	user := userID()
	team := fmt.Sprintf("team:%s", uuid.NewString())
	steps := []struct {
		role       string
		want       []string
		wantAbsent []string
	}{
		{"owner", []string{"owner", "mechanic", "member"}, nil},
		{"mechanic", []string{"mechanic", "member"}, []string{"owner"}},
		{"pilot", []string{"member"}, []string{"owner", "mechanic"}},
		{"guest", nil, []string{"owner", "mechanic", "member"}},
	}
	for _, s := range steps {
		require.NoError(t, c.SetTeamRole(ctx, user, team, rolePtr(s.role)))
		for _, relation := range s.want {
			require.NoError(t, c.Check(ctx, user, relation, team), "role %s should have %s", s.role, relation)
		}
		for _, relation := range s.wantAbsent {
			require.ErrorIs(t, c.Check(ctx, user, relation, team), authz.ErrDenied, "role %s should not have %s", s.role, relation)
		}
	}
}

func TestSetTeamRoleIsIdempotent(t *testing.T) {
	t.Parallel()
	ofga := testutil.StartOpenFGA(t)
	ctx := context.Background()
	c := newClient(t, ctx, ofga)

	user := userID()
	team := fmt.Sprintf("team:%s", uuid.NewString())
	require.NoError(t, c.SetTeamRole(ctx, user, team, rolePtr("mechanic")))
	require.NoError(t, c.SetTeamRole(ctx, user, team, rolePtr("mechanic")))
	require.NoError(t, c.Check(ctx, user, "mechanic", team))
}

func TestSetTeamRoleRejectsAnUnknownRole(t *testing.T) {
	t.Parallel()
	ofga := testutil.StartOpenFGA(t)
	ctx := context.Background()
	c := newClient(t, ctx, ofga)

	err := c.SetTeamRole(ctx, userID(), fmt.Sprintf("team:%s", uuid.NewString()), rolePtr("admin"))
	require.ErrorIs(t, err, authz.ErrInvalidRole)
}

func TestClientDiscoversLatestModelWhenNotConfigured(t *testing.T) {
	t.Parallel()
	ofga := testutil.StartOpenFGA(t)
	ctx := context.Background()
	c, err := authz.New(ctx, authz.Config{
		Addr:         ofga.HTTPAddr,
		StoreID:      ofga.StoreID,
		PresharedKey: ofga.PresharedKey,
		Timeout:      defaultTimeout,
	})
	require.NoError(t, err)
	require.Equal(t, ofga.ModelID, c.ModelID())

	owner := userID()
	team := fmt.Sprintf("team:%s", uuid.NewString())
	require.NoError(t, c.SetTeamRole(ctx, owner, team, rolePtr("owner")))
	require.NoError(t, c.Check(ctx, owner, "owner", team))
}

func TestClientFailsClosedOnBadCredentials(t *testing.T) {
	t.Parallel()
	ofga := testutil.StartOpenFGA(t)
	ctx := context.Background()
	_, err := authz.New(ctx, authz.Config{
		Addr:         ofga.HTTPAddr,
		StoreID:      ofga.StoreID,
		PresharedKey: "wrong-key",
		Timeout:      defaultTimeout,
	})
	require.Error(t, err)
	require.False(t, errors.Is(err, authz.ErrDenied))
}

const defaultTimeout = 5 * time.Second // generous for integration tests; local OpenFGA is fast.

func newClient(t testing.TB, ctx context.Context, ofga testutil.OpenFGA) *authz.Client {
	t.Helper()
	c, err := authz.New(ctx, authz.Config{
		Addr:         ofga.HTTPAddr,
		StoreID:      ofga.StoreID,
		ModelID:      ofga.ModelID,
		PresharedKey: ofga.PresharedKey,
		Timeout:      defaultTimeout,
	})
	require.NoError(t, err)
	return c
}

func userID() string {
	return fmt.Sprintf("user:%s", uuid.NewString())
}

func rolePtr(s string) *string { return &s }
