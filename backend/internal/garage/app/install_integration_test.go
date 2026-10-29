package app_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/app"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/domain"
)

type installFixture struct {
	*harness
	owner, team uuid.UUID
	part        domain.Part
	engine      domain.Engine
}

func newInstallFixture(t *testing.T, serviceLife bool, stock int64) installFixture {
	t.Helper()
	h := newHarness(t)
	owner := h.user(t, "owner")
	team := h.team(t, owner, "owner")
	part, err := h.svc.CreatePart(h.ctx, uid(owner), tid(team), app.CreatePartInput{
		RequestID: uuid.Must(uuid.NewV7()), Kind: domain.PartGlowPlug, Description: "Vela",
		Unit: "un", HasServiceLife: serviceLife, InitialQtyCenti: stock,
	})
	require.NoError(t, err)
	engine, err := h.svc.CreateEngine(h.ctx, uid(owner), tid(team), app.CreateEngineInput{
		RequestID: uuid.Must(uuid.NewV7()), Make: "REDS", Model: "R7",
	})
	require.NoError(t, err)
	return installFixture{harness: h, owner: owner, team: team, part: part, engine: engine}
}

func (f installFixture) install(t *testing.T) (domain.PartInstall, domain.Part) {
	t.Helper()
	i, p, err := f.svc.InstallPart(f.ctx, uid(f.owner), tid(f.team), app.InstallPartInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: f.part.ID, EngineID: &f.engine.ID,
	})
	require.NoError(t, err)
	return i, p
}

func (f installFixture) removeID(id uuid.UUID, destination, reason string) (domain.PartInstall, domain.Part, error) {
	return f.svc.RemovePart(f.ctx, uid(f.owner), tid(f.team), app.RemovePartInput{
		RequestID: uuid.Must(uuid.NewV7()), InstallID: id, Destination: destination, Reason: reason,
	})
}

func TestInstallTakesOneFromStockAndRemoveToStockReturnsIt(t *testing.T) {
	f := newInstallFixture(t, true, 500)

	install, part := f.install(t)
	require.True(t, install.Active())
	require.Equal(t, f.engine.ID, *install.EngineID)
	require.Nil(t, install.CarID)
	require.Equal(t, int64(400), part.QtyCenti)
	require.Zero(t, install.RunSeconds)

	closed, part, err := f.removeID(install.ID, domain.RemovalStock, "")
	require.NoError(t, err)
	require.False(t, closed.Active())
	require.Equal(t, domain.RemovalStock, closed.RemovalDestination)
	require.Empty(t, closed.RemovalReason)
	require.Equal(t, int64(500), part.QtyCenti)
	require.Equal(t, 1, f.count(t, fmt.Sprintf(`SELECT count(*) FROM garage.stock_movement WHERE part_id = '%s' AND reason = 'returned'`, f.part.ID)))
	require.Equal(t, 2, f.count(t, fmt.Sprintf(`SELECT count(*) FROM audit.audit_log WHERE team_id = '%s' AND action IN ('part.install','part.remove') AND request_id IS NOT NULL`, f.team)))
}

func TestScrapRecordsReasonWithoutReturningAndSpareLeavesStock(t *testing.T) {
	f := newInstallFixture(t, true, 500)

	install, _ := f.install(t)
	closed, part, err := f.removeID(install.ID, domain.RemovalScrap, domain.ReasonBroken)
	require.NoError(t, err)
	require.Equal(t, domain.ReasonBroken, closed.RemovalReason)
	require.Equal(t, int64(400), part.QtyCenti)

	install, _ = f.install(t)
	closed, part, err = f.removeID(install.ID, domain.RemovalSpare, "")
	require.NoError(t, err)
	require.Equal(t, domain.RemovalSpare, closed.RemovalDestination)
	require.Equal(t, int64(300), part.QtyCenti)
	require.Zero(t, f.count(t, fmt.Sprintf(`SELECT count(*) FROM garage.stock_movement WHERE part_id = '%s' AND reason = 'returned'`, f.part.ID)))

	_, _, err = f.removeID(install.ID, domain.RemovalScrap, "")
	require.ErrorIs(t, err, domain.ErrInvalid)
	_, _, err = f.removeID(install.ID, domain.RemovalStock, domain.ReasonWorn)
	require.ErrorIs(t, err, domain.ErrInvalid)
}

func TestInstallRefusals(t *testing.T) {
	f := newInstallFixture(t, false, 500)
	_, _, err := f.svc.InstallPart(f.ctx, uid(f.owner), tid(f.team), app.InstallPartInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: f.part.ID, EngineID: &f.engine.ID,
	})
	require.ErrorIs(t, err, domain.ErrInstallConflict)
	require.Zero(t, f.count(t, `SELECT count(*) FROM garage.part_install`))
	require.Equal(t, 0, f.count(t, fmt.Sprintf(`SELECT count(*) FROM garage.stock_movement WHERE part_id = '%s' AND reason = 'installed'`, f.part.ID)))

	g := newInstallFixture(t, true, 500)
	car := g.createCar(t, g.owner, g.team, "Kyosho", "MP10")
	_, _, err = g.svc.InstallPart(g.ctx, uid(g.owner), tid(g.team), app.InstallPartInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: g.part.ID, CarID: &car.ID, EngineID: &g.engine.ID,
	})
	require.ErrorIs(t, err, domain.ErrInvalid)
	_, _, err = g.svc.InstallPart(g.ctx, uid(g.owner), tid(g.team), app.InstallPartInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: g.part.ID,
	})
	require.ErrorIs(t, err, domain.ErrInvalid)
	future := time.Now().Add(time.Hour)
	_, _, err = g.svc.InstallPart(g.ctx, uid(g.owner), tid(g.team), app.InstallPartInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: g.part.ID, CarID: &car.ID, InstalledAt: &future,
	})
	require.ErrorIs(t, err, domain.ErrInvalid)

	g.pg.Exec(t, fmt.Sprintf(`UPDATE garage.car SET status = 'retired' WHERE id = '%s'`, car.ID))
	_, _, err = g.svc.InstallPart(g.ctx, uid(g.owner), tid(g.team), app.InstallPartInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: g.part.ID, CarID: &car.ID,
	})
	require.ErrorIs(t, err, domain.ErrInstallConflict)
}

func TestInstallRolesAndOtherTeams(t *testing.T) {
	f := newInstallFixture(t, true, 500)
	install, _ := f.install(t)

	pilot := f.user(t, "pilot")
	f.member(t, f.team, pilot, "pilot")
	_, _, err := f.svc.InstallPart(f.ctx, uid(pilot), tid(f.team), app.InstallPartInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: f.part.ID, EngineID: &f.engine.ID,
	})
	require.ErrorIs(t, err, domain.ErrForbidden)
	_, _, err = f.svc.RemovePart(f.ctx, uid(pilot), tid(f.team), app.RemovePartInput{
		RequestID: uuid.Must(uuid.NewV7()), InstallID: install.ID, Destination: domain.RemovalStock,
	})
	require.ErrorIs(t, err, domain.ErrForbidden)
	list, _, err := f.svc.ListPartInstalls(f.ctx, uid(pilot), tid(f.team), domain.InstallFilter{PartID: &f.part.ID}, false, 10, "")
	require.NoError(t, err)
	require.Len(t, list, 1)

	stranger := f.user(t, "stranger")
	otherTeam := f.harness.team(t, stranger, "owner")
	_, _, err = f.svc.InstallPart(f.ctx, uid(stranger), tid(f.team), app.InstallPartInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: f.part.ID, EngineID: &f.engine.ID,
	})
	require.ErrorIs(t, err, domain.ErrNotFound)
	// Own team, but another team's part and engine (BOLA).
	_, _, err = f.svc.InstallPart(f.ctx, uid(stranger), tid(otherTeam), app.InstallPartInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: f.part.ID, EngineID: &f.engine.ID,
	})
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, _, err = f.svc.RemovePart(f.ctx, uid(stranger), tid(otherTeam), app.RemovePartInput{
		RequestID: uuid.Must(uuid.NewV7()), InstallID: install.ID, Destination: domain.RemovalStock,
	})
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, _, err = f.svc.ListPartInstalls(f.ctx, uid(stranger), tid(f.team), domain.InstallFilter{PartID: &f.part.ID}, false, 10, "")
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, _, err = f.svc.ListPartInstalls(f.ctx, uid(stranger), tid(otherTeam), domain.InstallFilter{EngineID: &f.engine.ID}, false, 10, "")
	require.ErrorIs(t, err, domain.ErrNotFound)

	// A mechanic of this team cannot touch the stranger's part with this team's ids either.
	otherPart, err := f.svc.CreatePart(f.ctx, uid(stranger), tid(otherTeam), app.CreatePartInput{
		RequestID: uuid.Must(uuid.NewV7()), Kind: domain.PartGlowPlug, Description: "Vela", Unit: "un", HasServiceLife: true,
	})
	require.NoError(t, err)
	_, _, err = f.svc.InstallPart(f.ctx, uid(f.owner), tid(f.team), app.InstallPartInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: otherPart.ID, EngineID: &f.engine.ID,
	})
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestInstallAndRemoveAreIdempotent(t *testing.T) {
	f := newInstallFixture(t, true, 500)
	in := app.InstallPartInput{RequestID: uuid.Must(uuid.NewV7()), PartID: f.part.ID, EngineID: &f.engine.ID}

	first, _, err := f.svc.InstallPart(f.ctx, uid(f.owner), tid(f.team), in)
	require.NoError(t, err)
	again, part, err := f.svc.InstallPart(f.ctx, uid(f.owner), tid(f.team), in)
	require.NoError(t, err)
	require.Equal(t, first.ID, again.ID)
	require.Equal(t, int64(400), part.QtyCenti)
	require.Equal(t, 1, f.count(t, `SELECT count(*) FROM garage.part_install`))
	require.Equal(t, 1, f.count(t, `SELECT count(*) FROM garage.stock_movement WHERE reason = 'installed'`))

	rm := app.RemovePartInput{RequestID: uuid.Must(uuid.NewV7()), InstallID: first.ID, Destination: domain.RemovalStock}
	_, _, err = f.svc.RemovePart(f.ctx, uid(f.owner), tid(f.team), rm)
	require.NoError(t, err)
	_, part, err = f.svc.RemovePart(f.ctx, uid(f.owner), tid(f.team), rm)
	require.NoError(t, err)
	require.Equal(t, int64(500), part.QtyCenti)
	require.Equal(t, 1, f.count(t, `SELECT count(*) FROM garage.stock_movement WHERE reason = 'returned'`))

	// A different request on a closed installation is refused, not applied twice.
	_, _, err = f.removeID(first.ID, domain.RemovalStock, "")
	require.ErrorIs(t, err, domain.ErrInstallConflict)
	require.Equal(t, 1, f.count(t, `SELECT count(*) FROM garage.stock_movement WHERE reason = 'returned'`))
}

func TestConcurrentRemoveHasOneWinner(t *testing.T) {
	f := newInstallFixture(t, true, 500)
	install, _ := f.install(t)

	const workers = 6
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, errs[i] = f.removeID(install.ID, domain.RemovalStock, "")
		}()
	}
	wg.Wait()
	wins := 0
	for _, err := range errs {
		if err == nil {
			wins++
		} else {
			require.ErrorIs(t, err, domain.ErrInstallConflict)
		}
	}
	require.Equal(t, 1, wins)
	require.Equal(t, 1, f.count(t, `SELECT count(*) FROM garage.stock_movement WHERE reason = 'returned'`))
}

func TestListPartInstallsFiltersHistoryAndPages(t *testing.T) {
	f := newInstallFixture(t, true, 500)
	car := f.createCar(t, f.owner, f.team, "Kyosho", "MP10")
	first, _ := f.install(t)
	_, _, err := f.removeID(first.ID, domain.RemovalSpare, "")
	require.NoError(t, err)
	_, _ = f.install(t)
	_, _, err = f.svc.InstallPart(f.ctx, uid(f.owner), tid(f.team), app.InstallPartInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: f.part.ID, CarID: &car.ID,
	})
	require.NoError(t, err)

	active, _, err := f.svc.ListPartInstalls(f.ctx, uid(f.owner), tid(f.team), domain.InstallFilter{PartID: &f.part.ID}, false, 10, "")
	require.NoError(t, err)
	require.Len(t, active, 2)
	all, _, err := f.svc.ListPartInstalls(f.ctx, uid(f.owner), tid(f.team), domain.InstallFilter{PartID: &f.part.ID}, true, 10, "")
	require.NoError(t, err)
	require.Len(t, all, 3)
	byEngine, _, err := f.svc.ListPartInstalls(f.ctx, uid(f.owner), tid(f.team), domain.InstallFilter{EngineID: &f.engine.ID}, true, 10, "")
	require.NoError(t, err)
	require.Len(t, byEngine, 2)
	byCar, _, err := f.svc.ListPartInstalls(f.ctx, uid(f.owner), tid(f.team), domain.InstallFilter{CarID: &car.ID}, false, 10, "")
	require.NoError(t, err)
	require.Len(t, byCar, 1)

	page, next, err := f.svc.ListPartInstalls(f.ctx, uid(f.owner), tid(f.team), domain.InstallFilter{PartID: &f.part.ID}, true, 2, "")
	require.NoError(t, err)
	require.Len(t, page, 2)
	require.NotEmpty(t, next)
	rest, next, err := f.svc.ListPartInstalls(f.ctx, uid(f.owner), tid(f.team), domain.InstallFilter{PartID: &f.part.ID}, true, 2, next)
	require.NoError(t, err)
	require.Len(t, rest, 1)
	require.Empty(t, next)

	_, _, err = f.svc.ListPartInstalls(f.ctx, uid(f.owner), tid(f.team), domain.InstallFilter{}, true, 2, "")
	require.ErrorIs(t, err, domain.ErrInvalid)
}

func TestInstallCrossingTheMinimumWritesOneStockLowEvent(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	team := h.team(t, owner, "owner")
	part, err := h.svc.CreatePart(h.ctx, uid(owner), tid(team), app.CreatePartInput{
		RequestID: uuid.Must(uuid.NewV7()), Kind: domain.PartGlowPlug, Description: "Vela", Unit: "un",
		HasServiceLife: true, MinStockCenti: 100, InitialQtyCenti: 200,
	})
	require.NoError(t, err)
	engine, err := h.svc.CreateEngine(h.ctx, uid(owner), tid(team), app.CreateEngineInput{
		RequestID: uuid.Must(uuid.NewV7()), Make: "REDS", Model: "R7",
	})
	require.NoError(t, err)
	for range 2 {
		_, _, err = h.svc.InstallPart(h.ctx, uid(owner), tid(team), app.InstallPartInput{
			RequestID: uuid.Must(uuid.NewV7()), PartID: part.ID, EngineID: &engine.ID,
		})
		require.NoError(t, err)
	}
	require.Equal(t, 1, h.count(t, fmt.Sprintf(`SELECT count(*) FROM platform.outbox WHERE team_id = '%s' AND event_type = 'garage.stock_low'`, team)))
}
