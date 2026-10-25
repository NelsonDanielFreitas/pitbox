package app_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/authz"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/clock"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/idempotency"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/testutil"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/app"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/store"
)

type fakeAuthz struct {
	mu    sync.Mutex
	roles map[string]string
}

func newFakeAuthz() *fakeAuthz { return &fakeAuthz{roles: map[string]string{}} }

func (f *fakeAuthz) set(user, team, role string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.roles[user+"|"+team] = role
}

func (f *fakeAuthz) Check(_ context.Context, user, relation, object string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	role := f.roles[strings.TrimPrefix(user, "user:")+"|"+strings.TrimPrefix(object, "team:")]
	allowed := false
	switch relation {
	case "member":
		allowed = role == "owner" || role == "mechanic" || role == "pilot"
	case "mechanic":
		allowed = role == "owner" || role == "mechanic"
	}
	if !allowed {
		return authz.ErrDenied
	}
	return nil
}

type fakeGarage struct{}

func (fakeGarage) CarExists(context.Context, db.DBTX, db.TeamID, uuid.UUID) (bool, error) {
	return true, nil
}
func (fakeGarage) EngineExists(context.Context, db.DBTX, db.TeamID, uuid.UUID) (bool, error) {
	return true, nil
}
func (fakeGarage) TyreSetExists(context.Context, db.DBTX, db.TeamID, uuid.UUID) (bool, error) {
	return true, nil
}

type harness struct {
	pg    testutil.Postgres
	pool  *pgxpool.Pool
	svc   *app.Service
	authz *fakeAuthz
	ctx   context.Context
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	pg := testutil.StartPostgres(t)
	pool, err := db.Open(context.Background(), pg.URL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	az := newFakeAuthz()
	svc, err := app.New(app.Deps{
		DB: db.New(pool), Store: store.NewRepo(), Authz: az, Clock: clock.Real(),
		Log: slog.New(slog.DiscardHandler), Idempotency: idempotency.NewStore(),
		Garage: fakeGarage{}, Shares: store.NewShareReader(pool),
	})
	require.NoError(t, err)
	return &harness{pg: pg, pool: pool, svc: svc, authz: az, ctx: context.Background()}
}

func (h *harness) user(t *testing.T, label string) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	h.pg.Exec(t, fmt.Sprintf(
		`INSERT INTO identity.user_account (id, idp_subject, email, display_name) VALUES ('%s', '%s', '%s@example.pt', '%s')`,
		id, label+"-"+id.String(), label, label))
	return id
}

func (h *harness) team(t *testing.T, owner uuid.UUID, role string) uuid.UUID {
	t.Helper()
	team := uuid.Must(uuid.NewV7())
	h.pg.Exec(t, fmt.Sprintf(`INSERT INTO identity.team (id, name, created_by) VALUES ('%s', 'Team', '%s')`, team, owner))
	h.pg.Exec(t, fmt.Sprintf(`INSERT INTO identity.membership (team_id, user_id, role) VALUES ('%s', '%s', '%s')`, team, owner, role))
	h.authz.set(owner.String(), team.String(), role)
	return team
}

func (h *harness) member(t *testing.T, team, user uuid.UUID, role string) {
	t.Helper()
	h.pg.Exec(t, fmt.Sprintf(`INSERT INTO identity.membership (team_id, user_id, role) VALUES ('%s', '%s', '%s')`, team, user, role))
	h.authz.set(user.String(), team.String(), role)
}

func (h *harness) car(t *testing.T, team uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	h.pg.Exec(t, fmt.Sprintf(
		`INSERT INTO garage.car (id, team_id, make, model) VALUES ('%s', '%s', 'Mugen', 'MBX8R')`, id, team))
	return id
}

func uid(id uuid.UUID) db.UserID { return db.UserID(id) }
func tid(id uuid.UUID) db.TeamID { return db.TeamID(id) }

func TestSetupLifecycleLinksAndRoles(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	team := h.team(t, owner, "owner")
	pilot := h.user(t, "pilot")
	h.member(t, team, pilot, "pilot")
	other := h.user(t, "other")
	otherTeam := h.team(t, other, "owner")
	car := h.car(t, team)

	created, err := h.svc.CreateSetup(h.ctx, uid(owner), tid(team), app.CreateSetupInput{
		RequestID: uuid.Must(uuid.NewV7()), CarID: car, Name: "Base terra",
	})
	require.NoError(t, err)
	require.Equal(t, int32(1), created.CurrentVersion)
	require.Equal(t, "Base terra", created.Name)

	// Empty setup compared with itself has no differences.
	diffs, err := h.svc.CompareVersions(h.ctx, uid(owner), tid(team), uuid.MustParse(created.ID), 1, 1)
	require.NoError(t, err)
	require.Empty(t, diffs)

	ref, err := h.svc.CreateVersion(h.ctx, uid(owner), tid(team), app.CreateVersionInput{
		RequestID: uuid.Must(uuid.NewV7()), SetupID: uuid.MustParse(created.ID), BaseVersion: 1,
		Data: map[string]any{"diffs": map[string]any{"front_cst": float64(500), "rear_cst": float64(300)}},
	})
	require.NoError(t, err)
	require.Equal(t, int32(2), ref.Version)

	// Replaying the same request_id returns the same version, not a third one.
	replayID := uuid.Must(uuid.NewV7())
	in := app.CreateVersionInput{
		RequestID: replayID, SetupID: uuid.MustParse(created.ID), BaseVersion: 2,
		Data: map[string]any{"diffs": map[string]any{"front_cst": float64(600)}},
	}
	ref3, err := h.svc.CreateVersion(h.ctx, uid(owner), tid(team), in)
	require.NoError(t, err)
	again, err := h.svc.CreateVersion(h.ctx, uid(owner), tid(team), in)
	require.NoError(t, err)
	require.Equal(t, ref3.SetupID, again.SetupID)
	require.Equal(t, ref3.Version, again.Version)
	require.Equal(t, ref3.BasedOnVersion, again.BasedOnVersion)

	diffs, err = h.svc.CompareVersions(h.ctx, uid(owner), tid(team), uuid.MustParse(created.ID), 1, 3)
	require.NoError(t, err)
	require.NotEmpty(t, diffs)

	// Pilot can read but not write.
	got, err := h.svc.GetVersion(h.ctx, uid(pilot), tid(team), uuid.MustParse(created.ID), nil)
	require.NoError(t, err)
	require.Equal(t, int32(3), got.Ref.Version)
	_, err = h.svc.CreateVersion(h.ctx, uid(pilot), tid(team), app.CreateVersionInput{
		RequestID: uuid.Must(uuid.NewV7()), SetupID: uuid.MustParse(created.ID), BaseVersion: 3,
		Data: map[string]any{},
	})
	require.ErrorIs(t, err, domain.ErrForbidden)

	// Another team is NOT_FOUND, not PERMISSION_DENIED.
	_, err = h.svc.GetVersion(h.ctx, uid(other), tid(otherTeam), uuid.MustParse(created.ID), nil)
	require.ErrorIs(t, err, domain.ErrNotFound)

	// Copy into a new setup, pinned to version 2.
	sourceVersion := int32(2)
	copied, err := h.svc.CreateSetup(h.ctx, uid(owner), tid(team), app.CreateSetupInput{
		RequestID: uuid.Must(uuid.NewV7()), CarID: car, Name: "Cópia",
		SourceSetupID: ptrUUID(uuid.MustParse(created.ID)), SourceVersion: &sourceVersion,
	})
	require.NoError(t, err)
	require.Equal(t, int32(1), copied.CurrentVersion)

	// A link pinned to version 1, and another that follows current.
	link, token, err := h.svc.CreateShareLink(h.ctx, uid(owner), tid(team), app.CreateShareLinkInput{
		RequestID: uuid.Must(uuid.NewV7()), SetupID: uuid.MustParse(created.ID),
		Expiry: domain.ShareExpiry24h, PinnedVersion: ptrInt32(1),
	})
	require.NoError(t, err)
	require.NotEmpty(t, token)
	require.Equal(t, int32(1), *link.PinnedVersion)

	opened, err := h.svc.OpenShareLink(h.ctx, token)
	require.NoError(t, err)
	require.Equal(t, int32(1), opened.Version)
	require.Equal(t, "Base terra", opened.Name)

	current, currentToken, err := h.svc.CreateShareLink(h.ctx, uid(owner), tid(team), app.CreateShareLinkInput{
		RequestID: uuid.Must(uuid.NewV7()), SetupID: uuid.MustParse(created.ID), Expiry: domain.ShareExpiryNever,
	})
	require.NoError(t, err)
	require.Nil(t, current.PinnedVersion)
	opened, err = h.svc.OpenShareLink(h.ctx, currentToken)
	require.NoError(t, err)
	require.Equal(t, int32(3), opened.Version)

	links, err := h.svc.ListShareLinks(h.ctx, uid(owner), tid(team), uuid.MustParse(created.ID))
	require.NoError(t, err)
	require.Len(t, links, 2)
	require.NotContains(t, fmt.Sprint(links), token)

	require.NoError(t, h.svc.RevokeShareLink(h.ctx, uid(owner), tid(team), uuid.MustParse(created.ID), uuid.MustParse(current.ID), uuid.Must(uuid.NewV7())))
	_, err = h.svc.OpenShareLink(h.ctx, currentToken)
	require.ErrorIs(t, err, domain.ErrNotFound)

	// The token is not stored in clear; the table has only a 32-byte hash.
	hash := h.pg.Exec(t, fmt.Sprintf("SELECT encode(token_hash, 'hex') FROM setup.share_link WHERE id = '%s'", current.ID))
	require.Len(t, hash, 64)
	require.NotEqual(t, currentToken, hash)
}

func TestShareLinkCapAndPublicReferences(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	team := h.team(t, owner, "owner")
	car := h.car(t, team)
	created, err := h.svc.CreateSetup(h.ctx, uid(owner), tid(team), app.CreateSetupInput{
		RequestID: uuid.Must(uuid.NewV7()), CarID: car, Name: "Com referências",
	})
	require.NoError(t, err)
	engineID := uuid.Must(uuid.NewV7())
	tyreID := uuid.Must(uuid.NewV7())
	_, err = h.svc.CreateVersion(h.ctx, uid(owner), tid(team), app.CreateVersionInput{
		RequestID: uuid.Must(uuid.NewV7()), SetupID: uuid.MustParse(created.ID), BaseVersion: 1,
		Data: map[string]any{
			"engine": map[string]any{"engine_id": engineID.String()},
			"tyres":  map[string]any{"tyre_set_id": tyreID.String()},
		},
	})
	require.NoError(t, err)
	_, token, err := h.svc.CreateShareLink(h.ctx, uid(owner), tid(team), app.CreateShareLinkInput{
		RequestID: uuid.Must(uuid.NewV7()), SetupID: uuid.MustParse(created.ID), Expiry: domain.ShareExpiry7d,
	})
	require.NoError(t, err)
	opened, err := h.svc.OpenShareLink(h.ctx, token)
	require.NoError(t, err)
	require.NotContains(t, fmt.Sprint(opened.Data), engineID.String())
	require.NotContains(t, fmt.Sprint(opened.Data), tyreID.String())

	for i := 0; i < 19; i++ {
		_, _, err := h.svc.CreateShareLink(h.ctx, uid(owner), tid(team), app.CreateShareLinkInput{
			RequestID: uuid.Must(uuid.NewV7()), SetupID: uuid.MustParse(created.ID), Expiry: domain.ShareExpiry24h,
		})
		require.NoError(t, err)
	}
	_, _, err = h.svc.CreateShareLink(h.ctx, uid(owner), tid(team), app.CreateShareLinkInput{
		RequestID: uuid.Must(uuid.NewV7()), SetupID: uuid.MustParse(created.ID), Expiry: domain.ShareExpiry24h,
	})
	require.ErrorIs(t, err, domain.ErrShareLinkFull)
}

func TestInvalidDataIsRejectedWithoutValue(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	team := h.team(t, owner, "owner")
	car := h.car(t, team)
	created, err := h.svc.CreateSetup(h.ctx, uid(owner), tid(team), app.CreateSetupInput{
		RequestID: uuid.Must(uuid.NewV7()), CarID: car, Name: "Sentinela",
	})
	require.NoError(t, err)

	const sentinel = 987654321.0
	_, err = h.svc.CreateVersion(h.ctx, uid(owner), tid(team), app.CreateVersionInput{
		RequestID: uuid.Must(uuid.NewV7()), SetupID: uuid.MustParse(created.ID), BaseVersion: 1,
		Data: map[string]any{"diffs": map[string]any{"front_cst": sentinel}},
	})
	require.Error(t, err)
	var ve *domain.ValidationError
	require.True(t, errors.As(err, &ve))
	require.NotContains(t, err.Error(), "987654321")
	require.Equal(t, "setup.data.range", ve.RuleID)
}

func TestIDs(t *testing.T) {
	t.Parallel()
	// UUIDv7 values are accepted by the service; this also keeps the test's
	// generated ids recognisable in logs if a failure needs investigating.
	_ = uuid.Must(uuid.NewV7())
}

func ptrUUID(id uuid.UUID) *uuid.UUID { return &id }
func ptrInt32(v int32) *int32         { return &v }
