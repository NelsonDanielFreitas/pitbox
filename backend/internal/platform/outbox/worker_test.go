package outbox_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/authz"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/outbox"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/outbox/sqlcgen"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/testutil"
)

// These tests need real PostgreSQL and OpenFGA (Docker). Without Docker they skip,
// unless PITBOX_REQUIRE_DOCKER=1 (CI).

func TestWorkerAppliesMembershipState(t *testing.T) {
	pg := testutil.StartPostgres(t)
	ofga := testutil.StartOpenFGA(t)
	pool := openPool(t, pg.URL)
	roles := openAuthz(t, ofga)
	ctx := context.Background()

	team := uuid.Must(uuid.NewV7())
	user := uuid.Must(uuid.NewV7())
	proc := newProcessor(t, pool, roles)

	insert(t, pool, team, user, ptr("owner"))
	require.NoError(t, proc.ProcessBatch(ctx))

	require.NoError(t, roles.Check(ctx, subject(user), "owner", object(team)))
	require.NoError(t, roles.Check(ctx, subject(user), "member", object(team)))

	// Removal: role_after null removes the team relations.
	insert(t, pool, team, user, nil)
	require.NoError(t, proc.ProcessBatch(ctx))
	require.ErrorIs(t, roles.Check(ctx, subject(user), "member", object(team)), authz.ErrDenied)

	// A new role after a removal works (state, not delta).
	insert(t, pool, team, user, ptr("pilot"))
	require.NoError(t, proc.ProcessBatch(ctx))
	require.NoError(t, roles.Check(ctx, subject(user), "member", object(team)))
	require.ErrorIs(t, roles.Check(ctx, subject(user), "mechanic", object(team)), authz.ErrDenied)
}

func TestWorkerReplaysAreIdempotent(t *testing.T) {
	pg := testutil.StartPostgres(t)
	ofga := testutil.StartOpenFGA(t)
	pool := openPool(t, pg.URL)
	roles := openAuthz(t, ofga)
	ctx := context.Background()

	team := uuid.Must(uuid.NewV7())
	user := uuid.Must(uuid.NewV7())
	proc := newProcessor(t, pool, roles)

	insert(t, pool, team, user, ptr("mechanic"))
	require.NoError(t, proc.ProcessBatch(ctx))
	require.NoError(t, roles.Check(ctx, subject(user), "mechanic", object(team)))

	// Make the event claimable again, as a crash after the Write but before the
	// mark would: replaying it must leave the same state (idempotent).
	_, err := pool.Exec(ctx, `UPDATE platform.outbox SET published_at = NULL WHERE aggregate_id = $1`, user)
	require.NoError(t, err)
	require.NoError(t, proc.ProcessBatch(ctx))
	require.NoError(t, roles.Check(ctx, subject(user), "mechanic", object(team)))
	require.ErrorIs(t, roles.Check(ctx, subject(user), "owner", object(team)), authz.ErrDenied)
}

func TestWorkerKeepsOrderPerKey(t *testing.T) {
	pg := testutil.StartPostgres(t)
	pool := openPool(t, pg.URL)
	ctx := context.Background()

	teamA := uuid.Must(uuid.NewV7())
	userA := uuid.Must(uuid.NewV7())
	teamC := uuid.Must(uuid.NewV7())
	userC := uuid.Must(uuid.NewV7())

	failA := true
	roles := &recordingRoles{fail: func(user, team string) bool {
		return failA && user == subject(userA) && team == object(teamA)
	}}
	proc := newProcessor(t, pool, roles)

	// Same key: A then B. Another key: C. All inserted in this order.
	insert(t, pool, teamA, userA, ptr("owner"))
	insert(t, pool, teamA, userA, ptr("mechanic"))
	insert(t, pool, teamC, userC, ptr("pilot"))

	// First pass: A fails, B must not be claimed (it is behind A), C is independent.
	require.NoError(t, proc.ProcessBatch(ctx))
	calls := roles.snapshot()
	require.Len(t, calls, 2)
	require.Contains(t, calls, roleCall{user: subject(userA), team: object(teamA), role: "owner", failed: true})
	require.Contains(t, calls, roleCall{user: subject(userC), team: object(teamC), role: "pilot"})
	require.False(t, roles.called(subject(userA), object(teamA), "mechanic"), "B must wait for A")

	// The backoff would hold A for a few seconds; force the retry now. B is still
	// behind A because A was never published.
	failA = false
	_, err := pool.Exec(ctx, `UPDATE platform.outbox SET locked_until = NULL WHERE aggregate_id = $1`, userA)
	require.NoError(t, err)

	require.NoError(t, proc.ProcessBatch(ctx))
	require.True(t, roles.called(subject(userA), object(teamA), "owner"), "A is retried")
	require.False(t, roles.called(subject(userA), object(teamA), "mechanic"), "B still waits")

	require.NoError(t, proc.ProcessBatch(ctx))
	require.True(t, roles.called(subject(userA), object(teamA), "mechanic"), "B runs after A")

	// Removing the NOT EXISTS from the claim would put B in the first pass (and
	// reverse the order); the assertion above is the regression test.
}

func TestClaimCapsAttemptsAtSmallintMax(t *testing.T) {
	pg := testutil.StartPostgres(t)
	pool := openPool(t, pg.URL)
	ctx := context.Background()

	teamA := uuid.Must(uuid.NewV7())
	userA := uuid.Must(uuid.NewV7())
	teamB := uuid.Must(uuid.NewV7())
	userB := uuid.Must(uuid.NewV7())

	insert(t, pool, teamA, userA, ptr("owner"))
	insert(t, pool, teamB, userB, ptr("owner"))
	_, err := pool.Exec(ctx, `UPDATE platform.outbox SET attempts = 32767 WHERE aggregate_id = $1`, userA)
	require.NoError(t, err)

	roles := &recordingRoles{}
	proc := newProcessor(t, pool, roles)
	require.NoError(t, proc.ProcessBatch(ctx))
	require.Len(t, roles.snapshot(), 2, "the poisoned row must not block the other key")
	require.True(t, roles.called(subject(userA), object(teamA), "owner"))
	require.True(t, roles.called(subject(userB), object(teamB), "owner"))

	var attempts int16
	require.NoError(t, pool.QueryRow(ctx, `SELECT attempts FROM platform.outbox WHERE aggregate_id = $1`, userA).Scan(&attempts))
	require.Equal(t, int16(32767), attempts)
}

func TestTwoInstancesDoNotClaimTheSameEvent(t *testing.T) {
	pg := testutil.StartPostgres(t)
	pool := openPool(t, pg.URL)
	ctx := context.Background()

	insert(t, pool, uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), ptr("owner"))
	insert(t, pool, uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), ptr("owner"))

	q := sqlcgen.New(pool)
	params := sqlcgen.ClaimOutboxParams{LeaseSeconds: 30, BatchSize: 10}
	first, err := q.ClaimOutbox(ctx, params)
	require.NoError(t, err)
	require.Len(t, first, 2)

	// The lease hides them from the second instance.
	second, err := q.ClaimOutbox(ctx, params)
	require.NoError(t, err)
	require.Empty(t, second)
}

func TestShutdownMarksOnlyWhatItApplied(t *testing.T) {
	pg := testutil.StartPostgres(t)
	pool := openPool(t, pg.URL)

	insert(t, pool, uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), ptr("owner"))
	insert(t, pool, uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), ptr("owner"))

	ctx, cancel := context.WithCancel(context.Background())
	roles := &recordingRoles{after: cancel}
	proc := newProcessor(t, pool, roles)

	require.NoError(t, proc.ProcessBatch(ctx))
	require.Len(t, roles.snapshot(), 1, "the second event must not start after cancellation")

	var published, unpublished int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FILTER (WHERE published_at IS NOT NULL), count(*) FILTER (WHERE published_at IS NULL) FROM platform.outbox`).Scan(&published, &unpublished))
	require.Equal(t, 1, published)
	require.Equal(t, 1, unpublished)
}

// --- helpers ---------------------------------------------------------------

func newProcessor(t *testing.T, pool *pgxpool.Pool, roles outbox.RoleWriter) *outbox.Processor {
	t.Helper()
	opts := outbox.DefaultOptions()
	opts.BatchSize = 10
	opts.Lease = 5 * time.Second
	opts.BaseBackoff = 100 * time.Millisecond
	opts.MaxBackoff = time.Second
	opts.PollInterval = time.Second
	opts.AlertAfter = 2
	log := slog.New(slog.DiscardHandler)
	p, err := outbox.NewProcessor(pool, roles, opts, log, nil, nil)
	require.NoError(t, err)
	return p
}

func openPool(t *testing.T, url string) *pgxpool.Pool {
	t.Helper()
	pool, err := db.Open(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func openAuthz(t *testing.T, ofga testutil.OpenFGA) *authz.Client {
	t.Helper()
	c, err := authz.New(context.Background(), authz.Config{
		Addr:         ofga.HTTPAddr,
		StoreID:      ofga.StoreID,
		ModelID:      ofga.ModelID,
		PresharedKey: ofga.PresharedKey,
		Timeout:      5 * time.Second,
	})
	require.NoError(t, err)
	return c
}

func insert(t *testing.T, pool *pgxpool.Pool, team, user uuid.UUID, role *string) {
	t.Helper()
	require.NoError(t, outbox.Record(context.Background(), pool, outbox.MembershipChanged(team, user, role)))
}

func subject(u uuid.UUID) string { return "user:" + u.String() }
func object(t uuid.UUID) string  { return "team:" + t.String() }
func ptr(s string) *string       { return &s }

type roleCall struct {
	user, team string
	role       string
	failed     bool
}

type recordingRoles struct {
	mu    sync.Mutex
	calls []roleCall
	fail  func(user, team string) bool
	after func()
}

func (r *recordingRoles) SetTeamRole(_ context.Context, user, team string, role *string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	call := roleCall{user: user, team: team}
	if role != nil {
		call.role = *role
	}
	if r.fail != nil && r.fail(user, team) {
		call.failed = true
		r.calls = append(r.calls, call)
		return errors.New("apply failed")
	}
	r.calls = append(r.calls, call)
	if r.after != nil {
		after := r.after
		r.after = nil
		after()
	}
	return nil
}

func (r *recordingRoles) snapshot() []roleCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]roleCall(nil), r.calls...)
}

func (r *recordingRoles) called(user, team, role string) bool {
	for _, c := range r.snapshot() {
		if c.user == user && c.team == team && c.role == role {
			return true
		}
	}
	return false
}

func TestDrainKeyAppliesInOrderAndNeverOverwritesANewerEvent(t *testing.T) {
	pg := testutil.StartPostgres(t)
	pool := openPool(t, pg.URL)
	ctx := context.Background()
	teamID := uuid.Must(uuid.NewV7())
	userID := uuid.Must(uuid.NewV7())

	roles := &recordingRoles{}
	proc := newProcessor(t, pool, roles)
	insert(t, pool, teamID, userID, ptr("mechanic"))
	insert(t, pool, teamID, userID, nil)

	require.NoError(t, proc.DrainKey(ctx, teamID, userID))
	calls := roles.snapshot()
	require.Len(t, calls, 2)
	require.Equal(t, "mechanic", calls[0].role)
	require.Equal(t, "", calls[1].role, "the removal wins")

	// The delayed syncRole calls the drain again: the key is already published,
	// so it must not write the old role back over the removal.
	require.NoError(t, proc.DrainKey(ctx, teamID, userID))
	require.Len(t, roles.snapshot(), 2)
}

func TestDrainKeyDoesNotStealAKeyTheWorkerHolds(t *testing.T) {
	pg := testutil.StartPostgres(t)
	pool := openPool(t, pg.URL)
	ctx := context.Background()
	teamID := uuid.Must(uuid.NewV7())
	userID := uuid.Must(uuid.NewV7())

	roles := &recordingRoles{}
	proc := newProcessor(t, pool, roles)
	insert(t, pool, teamID, userID, ptr("mechanic"))
	insert(t, pool, teamID, userID, nil)
	// Simulate the worker having claimed the oldest event.
	_, err := pool.Exec(ctx, `UPDATE platform.outbox SET locked_until = now() + interval '1 minute' WHERE team_id = $1 AND aggregate_id = $2 AND id = (SELECT min(id) FROM platform.outbox WHERE team_id = $1 AND aggregate_id = $2)`, teamID, userID)
	require.NoError(t, err)

	require.NoError(t, proc.DrainKey(ctx, teamID, userID))
	require.Empty(t, roles.snapshot(), "the worker owns the key; the drain must not jump the removal")
}
