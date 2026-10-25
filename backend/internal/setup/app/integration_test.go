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

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage"
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
	return newHarnessWithGarage(t, fakeGarage{})
}

func newHarnessWithGarage(t *testing.T, g app.Garage) *harness {
	t.Helper()
	pg := testutil.StartPostgres(t)
	pool, err := db.Open(context.Background(), pg.URL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	az := newFakeAuthz()
	svc, err := app.New(app.Deps{
		DB: db.New(pool), Store: store.NewRepo(), Authz: az, Clock: clock.Real(),
		Log: slog.New(slog.DiscardHandler), Idempotency: idempotency.NewStore(),
		Garage: g, Shares: store.NewShareReader(pool),
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

func (h *harness) engine(t *testing.T, team uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	h.pg.Exec(t, fmt.Sprintf(
		`INSERT INTO garage.engine (id, team_id, make, model) VALUES ('%s', '%s', 'REDS', 'R7')`, id, team))
	return id
}

func (h *harness) tyreSet(t *testing.T, team uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	h.pg.Exec(t, fmt.Sprintf(
		`INSERT INTO garage.tyre_set (id, team_id, label, make, model) VALUES ('%s', '%s', 'A', 'X', 'Y')`, id, team))
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

func TestAuthorizationMatrix(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	team := h.team(t, owner, "owner")
	pilot := h.user(t, "pilot")
	h.member(t, team, pilot, "pilot")
	guest := h.user(t, "guest")
	h.member(t, team, guest, "guest")
	other := h.user(t, "other")
	otherTeam := h.team(t, other, "owner")
	car := h.car(t, team)
	otherCar := h.car(t, otherTeam)

	setup, err := h.svc.CreateSetup(h.ctx, uid(owner), tid(team), app.CreateSetupInput{
		RequestID: uuid.Must(uuid.NewV7()), CarID: car, Name: "A",
	})
	require.NoError(t, err)
	otherSetup, err := h.svc.CreateSetup(h.ctx, uid(other), tid(otherTeam), app.CreateSetupInput{
		RequestID: uuid.Must(uuid.NewV7()), CarID: otherCar, Name: "B",
	})
	require.NoError(t, err)
	link, _, err := h.svc.CreateShareLink(h.ctx, uid(owner), tid(team), app.CreateShareLinkInput{
		RequestID: uuid.Must(uuid.NewV7()), SetupID: uuid.MustParse(setup.ID), Expiry: domain.ShareExpiry24h,
	})
	require.NoError(t, err)

	otherCalls := []struct {
		name string
		call func() error
	}{
		{"CreateVersion", func() error {
			_, err := h.svc.CreateVersion(h.ctx, uid(other), tid(otherTeam), app.CreateVersionInput{
				RequestID: uuid.Must(uuid.NewV7()), SetupID: uuid.MustParse(setup.ID), BaseVersion: 1, Data: map[string]any{},
			})
			return err
		}},
		{"CompareVersions", func() error {
			_, err := h.svc.CompareVersions(h.ctx, uid(other), tid(otherTeam), uuid.MustParse(setup.ID), 1, 1)
			return err
		}},
		{"ListVersions", func() error {
			_, _, err := h.svc.ListVersions(h.ctx, uid(other), tid(otherTeam), uuid.MustParse(setup.ID), 10, "")
			return err
		}},
		{"CreateShareLink", func() error {
			_, _, err := h.svc.CreateShareLink(h.ctx, uid(other), tid(otherTeam), app.CreateShareLinkInput{
				RequestID: uuid.Must(uuid.NewV7()), SetupID: uuid.MustParse(setup.ID), Expiry: domain.ShareExpiry24h,
			})
			return err
		}},
		{"ListShareLinks", func() error {
			_, err := h.svc.ListShareLinks(h.ctx, uid(other), tid(otherTeam), uuid.MustParse(setup.ID))
			return err
		}},
		{"RevokeShareLink", func() error {
			return h.svc.RevokeShareLink(h.ctx, uid(other), tid(otherTeam), uuid.MustParse(setup.ID), uuid.MustParse(link.ID), uuid.Must(uuid.NewV7()))
		}},
		{"CreateSetup with foreign source", func() error {
			_, err := h.svc.CreateSetup(h.ctx, uid(owner), tid(team), app.CreateSetupInput{
				RequestID: uuid.Must(uuid.NewV7()), CarID: car, Name: "copy",
				SourceSetupID: ptrUUID(uuid.MustParse(otherSetup.ID)),
			})
			return err
		}},
	}
	for _, tc := range otherCalls {
		t.Run("other team "+tc.name, func(t *testing.T) {
			require.ErrorIs(t, tc.call(), domain.ErrNotFound)
		})
	}

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"read", func() error {
			_, err := h.svc.GetVersion(h.ctx, uid(guest), tid(team), uuid.MustParse(setup.ID), nil)
			return err
		}},
		{"write", func() error {
			_, err := h.svc.CreateVersion(h.ctx, uid(guest), tid(team), app.CreateVersionInput{
				RequestID: uuid.Must(uuid.NewV7()), SetupID: uuid.MustParse(setup.ID), BaseVersion: 1, Data: map[string]any{},
			})
			return err
		}},
	} {
		t.Run("guest "+tc.name, func(t *testing.T) {
			require.ErrorIs(t, tc.call(), domain.ErrNotFound)
		})
	}

	pilotCalls := []struct {
		name string
		call func() error
	}{
		{"CreateSetup", func() error {
			_, err := h.svc.CreateSetup(h.ctx, uid(pilot), tid(team), app.CreateSetupInput{
				RequestID: uuid.Must(uuid.NewV7()), CarID: car, Name: "pilot",
			})
			return err
		}},
		{"CreateShareLink", func() error {
			_, _, err := h.svc.CreateShareLink(h.ctx, uid(pilot), tid(team), app.CreateShareLinkInput{
				RequestID: uuid.Must(uuid.NewV7()), SetupID: uuid.MustParse(setup.ID), Expiry: domain.ShareExpiry24h,
			})
			return err
		}},
		{"ListShareLinks", func() error {
			_, err := h.svc.ListShareLinks(h.ctx, uid(pilot), tid(team), uuid.MustParse(setup.ID))
			return err
		}},
		{"RevokeShareLink", func() error {
			return h.svc.RevokeShareLink(h.ctx, uid(pilot), tid(team), uuid.MustParse(setup.ID), uuid.MustParse(link.ID), uuid.Must(uuid.NewV7()))
		}},
	}
	for _, tc := range pilotCalls {
		t.Run("pilot "+tc.name, func(t *testing.T) {
			require.ErrorIs(t, tc.call(), domain.ErrForbidden)
		})
	}
}

func TestConcurrencyIdempotencyAndDemotion(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	team := h.team(t, owner, "owner")
	mechanic := h.user(t, "mechanic")
	h.member(t, team, mechanic, "mechanic")
	car := h.car(t, team)
	setup, err := h.svc.CreateSetup(h.ctx, uid(owner), tid(team), app.CreateSetupInput{
		RequestID: uuid.Must(uuid.NewV7()), CarID: car, Name: "race",
	})
	require.NoError(t, err)
	setupID := uuid.MustParse(setup.ID)

	_, err = h.svc.CreateVersion(h.ctx, uid(owner), tid(team), app.CreateVersionInput{
		RequestID: uuid.Must(uuid.NewV7()), SetupID: setupID, BaseVersion: 1, Data: nil,
	})
	require.ErrorIs(t, err, domain.ErrInvalid)

	// Six versions from the same base: exactly one wins.
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := h.svc.CreateVersion(h.ctx, uid(owner), tid(team), app.CreateVersionInput{
				RequestID: uuid.Must(uuid.NewV7()), SetupID: setupID, BaseVersion: 1,
				Data: map[string]any{"diffs": map[string]any{"front_cst": float64(500 + i)}},
			})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	ok, conflicts := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, domain.ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent error: %v", err)
		}
	}
	require.Equal(t, 1, ok)
	require.Equal(t, 5, conflicts)

	// A stale base is ABORTED.
	_, err = h.svc.CreateVersion(h.ctx, uid(owner), tid(team), app.CreateVersionInput{
		RequestID: uuid.Must(uuid.NewV7()), SetupID: setupID, BaseVersion: 1, Data: map[string]any{},
	})
	require.ErrorIs(t, err, domain.ErrConflict)

	// Same request id, different body or method, is INVALID_ARGUMENT.
	replayID := uuid.Must(uuid.NewV7())
	_, err = h.svc.CreateVersion(h.ctx, uid(owner), tid(team), app.CreateVersionInput{
		RequestID: replayID, SetupID: setupID, BaseVersion: 2, Data: map[string]any{},
	})
	require.NoError(t, err)
	_, err = h.svc.CreateVersion(h.ctx, uid(owner), tid(team), app.CreateVersionInput{
		RequestID: replayID, SetupID: setupID, BaseVersion: 2, Data: map[string]any{"diffs": map[string]any{"rear_cst": float64(300)}},
	})
	require.ErrorIs(t, err, domain.ErrInvalid)

	setupRequestID := uuid.Must(uuid.NewV7())
	_, err = h.svc.CreateSetup(h.ctx, uid(owner), tid(team), app.CreateSetupInput{
		RequestID: setupRequestID, CarID: car, Name: "method",
	})
	require.NoError(t, err)
	_, err = h.svc.CreateVersion(h.ctx, uid(owner), tid(team), app.CreateVersionInput{
		RequestID: setupRequestID, SetupID: setupID, BaseVersion: 3, Data: map[string]any{},
	})
	require.ErrorIs(t, err, domain.ErrInvalid)

	// A demoted mechanic is refused before an idempotent replay can return data.
	demotionID := uuid.Must(uuid.NewV7())
	in := app.CreateVersionInput{
		RequestID: demotionID, SetupID: setupID, BaseVersion: 3,
		Data: map[string]any{"diffs": map[string]any{"center_cst": float64(400)}},
	}
	_, err = h.svc.CreateVersion(h.ctx, uid(mechanic), tid(team), in)
	require.NoError(t, err)
	h.pg.Exec(t, fmt.Sprintf(`UPDATE identity.membership SET role = 'pilot' WHERE team_id = '%s' AND user_id = '%s'`, team, mechanic))
	h.authz.set(mechanic.String(), team.String(), "pilot")
	_, err = h.svc.CreateVersion(h.ctx, uid(mechanic), tid(team), in)
	require.ErrorIs(t, err, domain.ErrForbidden)
}

func TestPaginationAuditAndExpiredLink(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	team := h.team(t, owner, "owner")
	car := h.car(t, team)
	var ids []uuid.UUID
	for i := 0; i < 3; i++ {
		created, err := h.svc.CreateSetup(h.ctx, uid(owner), tid(team), app.CreateSetupInput{
			RequestID: uuid.Must(uuid.NewV7()), CarID: car, Name: fmt.Sprintf("setup %d", i),
		})
		require.NoError(t, err)
		ids = append(ids, uuid.MustParse(created.ID))
	}
	var got []uuid.UUID
	token := ""
	for {
		page, next, err := h.svc.ListSetups(h.ctx, uid(owner), tid(team), nil, 1, token)
		require.NoError(t, err)
		for _, s := range page {
			got = append(got, uuid.MustParse(s.ID))
		}
		if next == "" {
			break
		}
		token = next
	}
	require.Len(t, got, 3)
	require.ElementsMatch(t, ids, got)

	setup := ids[0]
	ref, err := h.svc.CreateVersion(h.ctx, uid(owner), tid(team), app.CreateVersionInput{
		RequestID: uuid.Must(uuid.NewV7()), SetupID: setup, BaseVersion: 1,
		Data: map[string]any{"diffs": map[string]any{"front_cst": float64(500)}},
	})
	require.NoError(t, err)
	link, tokenValue, err := h.svc.CreateShareLink(h.ctx, uid(owner), tid(team), app.CreateShareLinkInput{
		RequestID: uuid.Must(uuid.NewV7()), SetupID: setup, Expiry: domain.ShareExpiry24h,
	})
	require.NoError(t, err)
	expired, expiredToken, err := h.svc.CreateShareLink(h.ctx, uid(owner), tid(team), app.CreateShareLinkInput{
		RequestID: uuid.Must(uuid.NewV7()), SetupID: setup, Expiry: domain.ShareExpiry7d,
	})
	require.NoError(t, err)
	// Expiry is decided by the database clock, not the test's clock.
	h.pg.Exec(t, fmt.Sprintf(`UPDATE setup.share_link SET created_at = now() - interval '2 hours', expires_at = now() - interval '1 hour' WHERE id = '%s'`, expired.ID))
	_, err = h.svc.OpenShareLink(h.ctx, expiredToken)
	require.ErrorIs(t, err, domain.ErrNotFound)

	requestID := uuid.Must(uuid.NewV7())
	require.NoError(t, h.svc.RevokeShareLink(h.ctx, uid(owner), tid(team), setup, uuid.MustParse(link.ID), requestID))
	require.NoError(t, h.svc.RevokeShareLink(h.ctx, uid(owner), tid(team), setup, uuid.MustParse(link.ID), uuid.Must(uuid.NewV7())))

	for _, action := range []string{"setup.create", "setup.version.create", "share_link.create", "share_link.revoke"} {
		n := h.pg.Exec(t, fmt.Sprintf(`SELECT count(*) FROM audit.audit_log WHERE team_id = '%s' AND action = '%s'`, team, action))
		require.NotEqual(t, "0", n, action)
	}
	require.Equal(t, "1", h.pg.Exec(t, fmt.Sprintf(`SELECT count(*) FROM audit.audit_log WHERE team_id = '%s' AND action = 'share_link.revoke'`, team)))
	require.Equal(t, "0", h.pg.Exec(t, fmt.Sprintf(`SELECT count(*) FROM audit.audit_log WHERE team_id = '%s' AND (after::text LIKE '%%%s%%' OR before::text LIKE '%%%s%%')`, team, tokenValue, tokenValue)))
	_ = ref
}

func TestRealGarageReferences(t *testing.T) {
	h := newHarnessWithGarage(t, garage.NewDirectory())
	owner := h.user(t, "owner")
	team := h.team(t, owner, "owner")
	other := h.user(t, "other")
	otherTeam := h.team(t, other, "owner")
	car := h.car(t, team)
	engine := h.engine(t, team)
	tyre := h.tyreSet(t, team)
	otherEngine := h.engine(t, otherTeam)

	setup, err := h.svc.CreateSetup(h.ctx, uid(owner), tid(team), app.CreateSetupInput{
		RequestID: uuid.Must(uuid.NewV7()), CarID: car, Name: "garage",
	})
	require.NoError(t, err)
	setupID := uuid.MustParse(setup.ID)
	_, err = h.svc.CreateVersion(h.ctx, uid(owner), tid(team), app.CreateVersionInput{
		RequestID: uuid.Must(uuid.NewV7()), SetupID: setupID, BaseVersion: 1,
		Data: map[string]any{
			"engine": map[string]any{"engine_id": engine.String()},
			"tyres":  map[string]any{"tyre_set_id": tyre.String()},
		},
	})
	require.NoError(t, err)

	// A different engine from another team is NOT_FOUND.
	_, err = h.svc.CreateVersion(h.ctx, uid(owner), tid(team), app.CreateVersionInput{
		RequestID: uuid.Must(uuid.NewV7()), SetupID: setupID, BaseVersion: 2,
		Data: map[string]any{"engine": map[string]any{"engine_id": otherEngine.String()}},
	})
	require.ErrorIs(t, err, domain.ErrNotFound)

	// A reference that did not change is history: deleting the engine does not block it.
	h.pg.Exec(t, fmt.Sprintf(`UPDATE garage.engine SET deleted_at = now() WHERE id = '%s'`, engine))
	_, err = h.svc.CreateVersion(h.ctx, uid(owner), tid(team), app.CreateVersionInput{
		RequestID: uuid.Must(uuid.NewV7()), SetupID: setupID, BaseVersion: 2,
		Data: map[string]any{
			"engine": map[string]any{"engine_id": engine.String()},
			"tyres":  map[string]any{"tyre_set_id": tyre.String()},
		},
	})
	require.NoError(t, err)
}

func ptrUUID(id uuid.UUID) *uuid.UUID { return &id }
func ptrInt32(v int32) *int32         { return &v }
