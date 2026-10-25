package app_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/app"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/store"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/authz"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/clock"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/idempotency"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/testutil"
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
	log := slog.New(slog.DiscardHandler)
	svc, err := app.New(app.Deps{
		DB: db.New(pool), Store: store.NewRepo(), Authz: az, Clock: clock.Real(), Log: log,
		Idempotency: idempotency.NewStore(),
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

func uid(id uuid.UUID) db.UserID { return db.UserID(id) }
func tid(id uuid.UUID) db.TeamID { return db.TeamID(id) }

func (h *harness) createCar(t *testing.T, owner, team uuid.UUID, make, model string) domain.Car {
	t.Helper()
	car, err := h.svc.CreateCar(h.ctx, uid(owner), tid(team), app.CreateCarInput{
		RequestID: uuid.Must(uuid.NewV7()), Make: make, Model: model,
	})
	require.NoError(t, err)
	return car
}

func TestCarLifecycleAndRoles(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	team := h.team(t, owner, "owner")
	pilot := h.user(t, "pilot")
	h.member(t, team, pilot, "pilot")
	guest := h.user(t, "guest")
	h.member(t, team, guest, "guest")
	otherOwner := h.user(t, "other")
	otherTeam := h.team(t, otherOwner, "owner")

	car, err := h.svc.CreateCar(h.ctx, uid(owner), tid(team), app.CreateCarInput{
		RequestID: uuid.Must(uuid.NewV7()), Make: "Mugen", Model: "MBX8R",
		Status: "active", Scale: "1/8", Powertrain: "nitro",
	})
	require.NoError(t, err)
	require.Equal(t, "1/8", car.Scale)
	require.Equal(t, "nitro", car.Powertrain)
	require.Equal(t, int32(1), car.Version)

	read, err := h.svc.GetCar(h.ctx, uid(pilot), tid(team), car.ID)
	require.NoError(t, err)
	require.Equal(t, car.ID, read.ID)

	_, err = h.svc.CreateCar(h.ctx, uid(pilot), tid(team), app.CreateCarInput{
		RequestID: uuid.Must(uuid.NewV7()), Make: "X", Model: "Y",
	})
	require.ErrorIs(t, err, domain.ErrForbidden)

	_, err = h.svc.GetCar(h.ctx, uid(otherOwner), tid(otherTeam), car.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, err = h.svc.GetCar(h.ctx, uid(guest), tid(team), car.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)

	name := "Novo nome"
	updated, err := h.svc.UpdateCar(h.ctx, uid(owner), tid(team), app.UpdateCarInput{
		RequestID: uuid.Must(uuid.NewV7()), CarID: car.ID, Version: 1,
		Update: domain.CarUpdate{NicknameSet: true, Nickname: &name},
	})
	require.NoError(t, err)
	require.Equal(t, int32(2), updated.Version)
	_, err = h.svc.UpdateCar(h.ctx, uid(owner), tid(team), app.UpdateCarInput{
		RequestID: uuid.Must(uuid.NewV7()), CarID: car.ID, Version: 1,
		Update: domain.CarUpdate{NicknameSet: true, Nickname: &name},
	})
	require.ErrorIs(t, err, domain.ErrConflict)

	require.NoError(t, h.svc.DeleteCar(h.ctx, uid(owner), tid(team), uuid.Must(uuid.NewV7()), car.ID, 2))
	_, err = h.svc.GetCar(h.ctx, uid(owner), tid(team), car.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestCarIdempotencyAndPagination(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	team := h.team(t, owner, "owner")
	otherTeam := h.team(t, owner, "owner")
	requestID := uuid.Must(uuid.NewV7())
	in := app.CreateCarInput{RequestID: requestID, Make: "Mugen", Model: "MBX8R", Status: "active", Scale: "1/8", Powertrain: "nitro"}

	first, err := h.svc.CreateCar(h.ctx, uid(owner), tid(team), in)
	require.NoError(t, err)
	replay, err := h.svc.CreateCar(h.ctx, uid(owner), tid(team), in)
	require.NoError(t, err)
	require.Equal(t, first.ID, replay.ID)

	in.Model = "Other"
	_, err = h.svc.CreateCar(h.ctx, uid(owner), tid(team), in)
	require.ErrorIs(t, err, domain.ErrInvalid)

	firstReq := uuid.Must(uuid.NewV7())
	_, err = h.svc.CreateCar(h.ctx, uid(owner), tid(otherTeam), app.CreateCarInput{
		RequestID: firstReq, Make: "A", Model: "B", Status: "active", Scale: "1/8", Powertrain: "nitro",
	})
	require.NoError(t, err)
	_, err = h.svc.CreateCar(h.ctx, uid(owner), tid(otherTeam), app.CreateCarInput{
		RequestID: firstReq, Make: "A", Model: "B", Status: "active", Scale: "1/8", Powertrain: "nitro",
	})
	require.NoError(t, err)
	_, err = h.svc.CreateCar(h.ctx, uid(owner), tid(team), app.CreateCarInput{
		RequestID: firstReq, Make: "A", Model: "B", Status: "active", Scale: "1/8", Powertrain: "nitro",
	})
	require.ErrorIs(t, err, domain.ErrInvalid)

	for i := 0; i < 2; i++ {
		h.createCar(t, owner, team, "M", fmt.Sprintf("M-%d", i))
	}
	page, next, err := h.svc.ListCars(h.ctx, uid(owner), tid(team), 1, "")
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.NotEmpty(t, next)
	_, _, err = h.svc.ListCars(h.ctx, uid(owner), tid(team), 1, next)
	require.NoError(t, err)
}

func TestPartCostAndStockLow(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	team := h.team(t, owner, "owner")
	pilot := h.user(t, "pilot")
	h.member(t, team, pilot, "pilot")

	part, err := h.svc.CreatePart(h.ctx, uid(owner), tid(team), app.CreatePartInput{
		RequestID: uuid.Must(uuid.NewV7()), Kind: domain.PartGlowPlug, Description: "Vela",
		Unit: "un", MinStockCenti: 1000, InitialQtyCenti: 2000,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2000), part.QtyCenti)
	cost := int32(1250)
	_, err = h.svc.UpdatePart(h.ctx, uid(owner), tid(team), app.UpdatePartInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: part.ID, Version: part.Version,
		Update: domain.PartUpdate{CostSet: true, UnitCostCents: &cost},
	})
	require.NoError(t, err)

	asPilot, err := h.svc.GetPart(h.ctx, uid(pilot), tid(team), part.ID)
	require.NoError(t, err)
	require.Nil(t, asPilot.UnitCostCents)
	asOwner, err := h.svc.GetPart(h.ctx, uid(owner), tid(team), part.ID)
	require.NoError(t, err)
	require.NotNil(t, asOwner.UnitCostCents)
	pilotList, _, err := h.svc.ListParts(h.ctx, uid(pilot), tid(team), 10, "")
	require.NoError(t, err)
	require.Len(t, pilotList, 1)
	require.Nil(t, pilotList[0].UnitCostCents)

	_, movement, err := h.svc.AddStockMovement(h.ctx, uid(owner), tid(team), app.AddStockMovementInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: part.ID, QtyDeltaCenti: -1500, Reason: domain.StockConsumed,
	})
	require.NoError(t, err)
	require.Equal(t, int64(-1500), movement.QtyDeltaCenti)
	require.Equal(t, 1, h.count(t, fmt.Sprintf(`SELECT count(*) FROM platform.outbox WHERE team_id = '%s' AND event_type = 'garage.stock_low'`, team)))
	_, _, err = h.svc.AddStockMovement(h.ctx, uid(owner), tid(team), app.AddStockMovementInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: part.ID, QtyDeltaCenti: -100, Reason: domain.StockConsumed,
	})
	require.NoError(t, err)
	require.Equal(t, 1, h.count(t, fmt.Sprintf(`SELECT count(*) FROM platform.outbox WHERE team_id = '%s' AND event_type = 'garage.stock_low'`, team)))
}

func TestEngineMountLifecycle(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	team := h.team(t, owner, "owner")
	car1 := h.createCar(t, owner, team, "A", "1")
	car2 := h.createCar(t, owner, team, "B", "2")
	engine, err := h.svc.CreateEngine(h.ctx, uid(owner), tid(team), app.CreateEngineInput{
		RequestID: uuid.Must(uuid.NewV7()), Make: "REDS", Model: "R7",
	})
	require.NoError(t, err)

	mount, err := h.svc.MountEngine(h.ctx, uid(owner), tid(team), uuid.Must(uuid.NewV7()), engine.ID, car1.ID)
	require.NoError(t, err)
	require.Equal(t, car1.ID, mount.CarID)
	_, err = h.svc.MountEngine(h.ctx, uid(owner), tid(team), uuid.Must(uuid.NewV7()), engine.ID, car2.ID)
	require.ErrorIs(t, err, domain.ErrMountConflict)

	moved, err := h.svc.MoveEngine(h.ctx, uid(owner), tid(team), uuid.Must(uuid.NewV7()), engine.ID, car2.ID)
	require.NoError(t, err)
	require.Equal(t, car2.ID, moved.CarID)
	require.NoError(t, h.svc.UnmountEngine(h.ctx, uid(owner), tid(team), uuid.Must(uuid.NewV7()), engine.ID))
	err = h.svc.UnmountEngine(h.ctx, uid(owner), tid(team), uuid.Must(uuid.NewV7()), engine.ID)
	require.ErrorIs(t, err, domain.ErrMountConflict)

	mounts, _, err := h.svc.ListEngineMounts(h.ctx, uid(owner), tid(team), engine.ID, 10, "")
	require.NoError(t, err)
	require.Len(t, mounts, 2)
}

func (h *harness) count(t *testing.T, query string) int {
	t.Helper()
	n, err := strconv.Atoi(h.pg.Exec(t, query))
	require.NoError(t, err)
	return n
}

var _ = errors.Is

func TestTyreSetLifecycleAndUniqueLabel(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	team := h.team(t, owner, "owner")

	t1, err := h.svc.CreateTyreSet(h.ctx, uid(owner), tid(team), app.CreateTyreSetInput{
		RequestID: uuid.Must(uuid.NewV7()), Label: "J1", Make: "X", Model: "Y",
	})
	require.NoError(t, err)
	_, err = h.svc.CreateTyreSet(h.ctx, uid(owner), tid(team), app.CreateTyreSetInput{
		RequestID: uuid.Must(uuid.NewV7()), Label: "J1", Make: "X", Model: "Y",
	})
	require.ErrorIs(t, err, domain.ErrAlreadyExists)

	label := "J2"
	updated, err := h.svc.UpdateTyreSet(h.ctx, uid(owner), tid(team), app.UpdateTyreSetInput{
		RequestID: uuid.Must(uuid.NewV7()), TyreSetID: t1.ID, Version: 1,
		Update: domain.TyreUpdate{Label: &label},
	})
	require.NoError(t, err)
	require.Equal(t, "J2", updated.Label)
	require.Equal(t, int32(2), updated.Version)

	require.NoError(t, h.svc.DeleteTyreSet(h.ctx, uid(owner), tid(team), uuid.Must(uuid.NewV7()), t1.ID, 2))
	_, err = h.svc.GetTyreSet(h.ctx, uid(owner), tid(team), t1.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestCatalogValidationAndGarageAPI(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	team := h.team(t, owner, "owner")
	otherTeam := h.team(t, owner, "owner")
	car := h.createCar(t, owner, team, "Mugen", "MBX8R")

	dir := garage.NewCarDirectory()
	database := db.New(h.pool)
	var exists, otherExists bool
	err := database.WithTeamTx(h.ctx, uid(owner), tid(team), func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
		var err error
		exists, err = dir.Exists(ctx, tx, tid(team), car.ID)
		return err
	})
	require.NoError(t, err)
	require.True(t, exists)
	err = database.WithTeamTx(h.ctx, uid(owner), tid(otherTeam), func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
		var err error
		otherExists, err = dir.Exists(ctx, tx, tid(otherTeam), car.ID)
		return err
	})
	require.NoError(t, err)
	require.False(t, otherExists)

	catalog, err := garage.NewCatalogReader(h.pool).Load(h.ctx)
	require.NoError(t, err)
	require.NotEmpty(t, catalog.Scales)
	require.NotEmpty(t, catalog.Powertrains)
	require.Len(t, catalog.Version, 64)
	again, err := garage.NewCatalogReader(h.pool).Load(h.ctx)
	require.NoError(t, err)
	require.Equal(t, catalog.Version, again.Version)

	h.pg.Exec(t, `INSERT INTO garage.scale (code, name, active, sort_order) VALUES ('1/10', '1/10', false, 20)`)
	_, err = h.svc.CreateCar(h.ctx, uid(owner), tid(team), app.CreateCarInput{
		RequestID: uuid.Must(uuid.NewV7()), Make: "X", Model: "Y", Status: "active",
		Scale: "1/10", Powertrain: "nitro",
	})
	require.ErrorIs(t, err, domain.ErrCatalogInactive)
	scale := "1/10"
	_, err = h.svc.UpdateCar(h.ctx, uid(owner), tid(team), app.UpdateCarInput{
		RequestID: uuid.Must(uuid.NewV7()), CarID: car.ID, Version: car.Version,
		Update: domain.CarUpdate{Scale: &scale},
	})
	require.ErrorIs(t, err, domain.ErrCatalogInactive)
}

func TestGarageCrossTeamGuestAndPilotWrites(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	team := h.team(t, owner, "owner")
	pilot := h.user(t, "pilot")
	h.member(t, team, pilot, "pilot")
	guest := h.user(t, "guest")
	h.member(t, team, guest, "guest")
	other := h.user(t, "other")
	otherTeam := h.team(t, other, "owner")

	car := h.createCar(t, owner, team, "Mugen", "MBX8R")
	engine, err := h.svc.CreateEngine(h.ctx, uid(owner), tid(team), app.CreateEngineInput{
		RequestID: uuid.Must(uuid.NewV7()), Make: "REDS", Model: "R7",
	})
	require.NoError(t, err)
	tyre, err := h.svc.CreateTyreSet(h.ctx, uid(owner), tid(team), app.CreateTyreSetInput{
		RequestID: uuid.Must(uuid.NewV7()), Label: "J1", Make: "X", Model: "Y",
	})
	require.NoError(t, err)
	part, err := h.svc.CreatePart(h.ctx, uid(owner), tid(team), app.CreatePartInput{
		RequestID: uuid.Must(uuid.NewV7()), Kind: domain.PartGlowPlug, Description: "Vela",
		Unit: "un", MinStockCenti: 100,
	})
	require.NoError(t, err)

	for _, user := range []uuid.UUID{other, guest} {
		_, err := h.svc.GetCar(h.ctx, uid(user), tid(team), car.ID)
		require.ErrorIs(t, err, domain.ErrNotFound)
		_, err = h.svc.GetEngine(h.ctx, uid(user), tid(team), engine.ID)
		require.ErrorIs(t, err, domain.ErrNotFound)
		_, err = h.svc.GetTyreSet(h.ctx, uid(user), tid(team), tyre.ID)
		require.ErrorIs(t, err, domain.ErrNotFound)
		_, err = h.svc.GetPart(h.ctx, uid(user), tid(team), part.ID)
		require.ErrorIs(t, err, domain.ErrNotFound)
	}
	_, _, err = h.svc.ListCars(h.ctx, uid(other), tid(otherTeam), 10, "")
	require.NoError(t, err)
	_, _, err = h.svc.ListCars(h.ctx, uid(other), tid(team), 10, "")
	require.ErrorIs(t, err, domain.ErrNotFound)

	_, err = h.svc.CreateCar(h.ctx, uid(pilot), tid(team), app.CreateCarInput{
		RequestID: uuid.Must(uuid.NewV7()), Make: "X", Model: "Y",
	})
	require.ErrorIs(t, err, domain.ErrForbidden)
	_, err = h.svc.CreateEngine(h.ctx, uid(pilot), tid(team), app.CreateEngineInput{
		RequestID: uuid.Must(uuid.NewV7()), Make: "X", Model: "Y",
	})
	require.ErrorIs(t, err, domain.ErrForbidden)
	_, err = h.svc.CreateTyreSet(h.ctx, uid(pilot), tid(team), app.CreateTyreSetInput{
		RequestID: uuid.Must(uuid.NewV7()), Label: "J3", Make: "X", Model: "Y",
	})
	require.ErrorIs(t, err, domain.ErrForbidden)
	_, err = h.svc.CreatePart(h.ctx, uid(pilot), tid(team), app.CreatePartInput{
		RequestID: uuid.Must(uuid.NewV7()), Kind: domain.PartGlowPlug, Description: "X", Unit: "un",
	})
	require.ErrorIs(t, err, domain.ErrForbidden)
	_, err = h.svc.MountEngine(h.ctx, uid(pilot), tid(team), uuid.Must(uuid.NewV7()), engine.ID, car.ID)
	require.ErrorIs(t, err, domain.ErrForbidden)
	_, _, err = h.svc.AddStockMovement(h.ctx, uid(pilot), tid(team), app.AddStockMovementInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: part.ID, QtyDeltaCenti: 100, Reason: domain.StockPurchase,
	})
	require.ErrorIs(t, err, domain.ErrForbidden)

	// The transponder index is per team.
	transponder := "1234567"
	h.pg.Exec(t, fmt.Sprintf(`UPDATE garage.car SET transponder_number = '%s' WHERE id = '%s'`, transponder, car.ID))
	_, err = h.svc.CreateCar(h.ctx, uid(owner), tid(team), app.CreateCarInput{
		RequestID: uuid.Must(uuid.NewV7()), Make: "X", Model: "Y", TransponderNumber: &transponder,
	})
	require.ErrorIs(t, err, domain.ErrAlreadyExists)
	_, err = h.svc.CreateCar(h.ctx, uid(other), tid(otherTeam), app.CreateCarInput{
		RequestID: uuid.Must(uuid.NewV7()), Make: "X", Model: "Y", TransponderNumber: &transponder,
	})
	require.NoError(t, err)
}
