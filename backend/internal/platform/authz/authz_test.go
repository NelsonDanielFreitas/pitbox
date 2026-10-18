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
	require.NoError(t, c.WriteTuple(ctx, owner, "owner", team))

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
	require.NoError(t, c.WriteTuple(ctx, owner, "owner", teamA))
	require.NoError(t, c.WriteTuple(ctx, owner, "owner", teamB))

	objects, err := c.ListObjects(ctx, owner, "owner", "team")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{teamA, teamB}, objects)

	// A user with no tuples gets an empty list, not an error.
	other := userID()
	objects, err = c.ListObjects(ctx, other, "owner", "team")
	require.NoError(t, err)
	require.Empty(t, objects)
}

func TestDeleteTuple(t *testing.T) {
	t.Parallel()
	ofga := testutil.StartOpenFGA(t)
	ctx := context.Background()
	c := newClient(t, ctx, ofga)

	owner := userID()
	team := fmt.Sprintf("team:%s", uuid.NewString())
	require.NoError(t, c.WriteTuple(ctx, owner, "owner", team))
	require.NoError(t, c.Check(ctx, owner, "owner", team))

	require.NoError(t, c.DeleteTuple(ctx, owner, "owner", team))
	require.ErrorIs(t, c.Check(ctx, owner, "owner", team), authz.ErrDenied)
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
	require.NoError(t, c.WriteTuple(ctx, owner, "owner", team))
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
