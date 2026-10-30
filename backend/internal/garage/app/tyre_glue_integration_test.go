package app_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/app"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/domain"
)

type glueFixture struct {
	*harness
	owner, team  uuid.UUID
	tyre, insert domain.Part
}

func newGlueFixture(t *testing.T, tyreQty, insertQty int64) glueFixture {
	t.Helper()
	h := newHarness(t)
	owner := h.user(t, "owner")
	team := h.team(t, owner, "owner")
	brand := "Sworkz"
	tyre, err := h.svc.CreatePart(h.ctx, uid(owner), tid(team), app.CreatePartInput{
		RequestID: uuid.Must(uuid.NewV7()), Kind: domain.PartTyre, Brand: &brand, Description: "Pneus 1/8 TT",
		Unit: "un", Specs: map[string]any{"compound": "soft"}, InitialQtyCenti: tyreQty,
	})
	require.NoError(t, err)
	insert, err := h.svc.CreatePart(h.ctx, uid(owner), tid(team), app.CreatePartInput{
		RequestID: uuid.Must(uuid.NewV7()), Kind: domain.PartInsert, Description: "Insertos", Unit: "un",
		Specs: map[string]any{"insert_type": "foam"}, InitialQtyCenti: insertQty,
	})
	require.NoError(t, err)
	return glueFixture{harness: h, owner: owner, team: team, tyre: tyre, insert: insert}
}

func (f glueFixture) glue(label string) (domain.TyreSet, domain.Part, domain.Part, error) {
	return f.glueInput(app.GlueTyreSetInput{
		RequestID: uuid.Must(uuid.NewV7()),
		Glue:      domain.GlueInput{TyrePartID: f.tyre.ID, InsertPartID: f.insert.ID, Label: label},
	})
}

func (f glueFixture) glueInput(in app.GlueTyreSetInput) (domain.TyreSet, domain.Part, domain.Part, error) {
	return f.svc.GlueTyreSet(f.ctx, uid(f.owner), tid(f.team), in)
}

func (f glueFixture) stock(t *testing.T, part domain.Part) int64 {
	t.Helper()
	p, err := f.svc.GetPart(f.ctx, uid(f.owner), tid(f.team), part.ID)
	require.NoError(t, err)
	return p.QtyCenti
}

func TestGlueCreatesSetAndTakesFourPlusFourFromStock(t *testing.T) {
	f := newGlueFixture(t, 800, 600)
	set, tyre, insert, err := f.glue("J4")
	require.NoError(t, err)
	require.Equal(t, "J4", set.Label)
	require.Equal(t, "Sworkz", set.Make)
	require.Equal(t, "Pneus 1/8 TT", set.Model)
	require.Equal(t, "soft", *set.Compound)
	require.Equal(t, "foam", *set.InsertType)
	require.Equal(t, f.tyre.ID, *set.TyrePartID)
	require.Equal(t, f.insert.ID, *set.InsertPartID)
	require.Equal(t, int64(400), tyre.QtyCenti)
	require.Equal(t, int64(200), insert.QtyCenti)
	require.Equal(t, int64(400), f.stock(t, f.tyre))
	require.Equal(t, int64(200), f.stock(t, f.insert))

	got, err := f.svc.GetTyreSet(f.ctx, uid(f.owner), tid(f.team), set.ID)
	require.NoError(t, err)
	require.Equal(t, set.TyrePartID, got.TyrePartID)
	require.Equal(t, 2, f.count(t, fmt.Sprintf(`SELECT count(*) FROM garage.stock_movement WHERE team_id = '%s' AND reason = 'consumed'`, f.team)))
	require.Equal(t, 1, f.count(t, fmt.Sprintf(`SELECT count(*) FROM audit.audit_log WHERE team_id = '%s' AND action = 'tyre.glue'`, f.team)))
}

func TestGlueIsAllOrNothing(t *testing.T) {
	f := newGlueFixture(t, 800, 800)
	_, _, _, err := f.glue("J4")
	require.NoError(t, err)

	// A duplicate label fails after nothing else was written: the stock stays as it was.
	_, _, _, err = f.glue("J4")
	require.ErrorIs(t, err, domain.ErrAlreadyExists)
	require.Equal(t, int64(400), f.stock(t, f.tyre))
	require.Equal(t, int64(400), f.stock(t, f.insert))
	require.Equal(t, 1, f.count(t, fmt.Sprintf(`SELECT count(*) FROM garage.tyre_set WHERE team_id = '%s'`, f.team)))
	require.Equal(t, 2, f.count(t, fmt.Sprintf(`SELECT count(*) FROM garage.stock_movement WHERE team_id = '%s' AND reason = 'consumed'`, f.team)))
}

func TestGlueRefusesShortStockWithoutEffects(t *testing.T) {
	f := newGlueFixture(t, 300, 800)
	_, _, _, err := f.glue("J4")
	require.ErrorIs(t, err, domain.ErrInsufficientStock)
	require.Equal(t, int64(300), f.stock(t, f.tyre))
	require.Equal(t, int64(800), f.stock(t, f.insert))
	require.Zero(t, f.count(t, fmt.Sprintf(`SELECT count(*) FROM garage.tyre_set WHERE team_id = '%s'`, f.team)))
}

func TestGlueRefusesWrongPartKinds(t *testing.T) {
	f := newGlueFixture(t, 800, 800)
	plug, err := f.svc.CreatePart(f.ctx, uid(f.owner), tid(f.team), app.CreatePartInput{
		RequestID: uuid.Must(uuid.NewV7()), Kind: domain.PartGlowPlug, Description: "Vela", Unit: "un", InitialQtyCenti: 800,
	})
	require.NoError(t, err)
	for name, in := range map[string]domain.GlueInput{
		"plug as tyre":    {TyrePartID: plug.ID, InsertPartID: f.insert.ID, Label: "J1"},
		"plug as insert":  {TyrePartID: f.tyre.ID, InsertPartID: plug.ID, Label: "J1"},
		"swapped":         {TyrePartID: f.insert.ID, InsertPartID: f.tyre.ID, Label: "J1"},
		"same part twice": {TyrePartID: f.tyre.ID, InsertPartID: f.tyre.ID, Label: "J1"},
	} {
		_, _, _, err := f.glueInput(app.GlueTyreSetInput{RequestID: uuid.Must(uuid.NewV7()), Glue: in})
		require.ErrorIs(t, err, domain.ErrInvalid, name)
	}
	require.Equal(t, int64(800), f.stock(t, f.tyre))
	require.Equal(t, int64(800), f.stock(t, f.insert))
}

func TestGlueRolesAndOtherTeams(t *testing.T) {
	f := newGlueFixture(t, 800, 800)
	pilot := f.user(t, "pilot")
	f.member(t, f.team, pilot, "pilot")
	_, _, _, err := f.svc.GlueTyreSet(f.ctx, uid(pilot), tid(f.team), app.GlueTyreSetInput{
		RequestID: uuid.Must(uuid.NewV7()),
		Glue:      domain.GlueInput{TyrePartID: f.tyre.ID, InsertPartID: f.insert.ID, Label: "J4"},
	})
	require.ErrorIs(t, err, domain.ErrForbidden)

	stranger := f.user(t, "stranger")
	otherTeam := f.harness.team(t, stranger, "owner")
	in := app.GlueTyreSetInput{
		RequestID: uuid.Must(uuid.NewV7()),
		Glue:      domain.GlueInput{TyrePartID: f.tyre.ID, InsertPartID: f.insert.ID, Label: "J4"},
	}
	// Not a member of the team.
	_, _, _, err = f.svc.GlueTyreSet(f.ctx, uid(stranger), tid(f.team), in)
	require.ErrorIs(t, err, domain.ErrNotFound)
	// Own team, but the other team's parts (BOLA).
	in.RequestID = uuid.Must(uuid.NewV7())
	_, _, _, err = f.svc.GlueTyreSet(f.ctx, uid(stranger), tid(otherTeam), in)
	require.ErrorIs(t, err, domain.ErrNotFound)
	// One own part and one foreign part.
	own, err := f.svc.CreatePart(f.ctx, uid(stranger), tid(otherTeam), app.CreatePartInput{
		RequestID: uuid.Must(uuid.NewV7()), Kind: domain.PartInsert, Description: "Insertos", Unit: "un", InitialQtyCenti: 800,
	})
	require.NoError(t, err)
	in.RequestID = uuid.Must(uuid.NewV7())
	in.Glue.InsertPartID = own.ID
	_, _, _, err = f.svc.GlueTyreSet(f.ctx, uid(stranger), tid(otherTeam), in)
	require.ErrorIs(t, err, domain.ErrNotFound)

	require.Equal(t, int64(800), f.stock(t, f.tyre))
	require.Equal(t, int64(800), f.stock(t, f.insert))
	require.Equal(t, int64(800), func() int64 {
		p, err := f.svc.GetPart(f.ctx, uid(stranger), tid(otherTeam), own.ID)
		require.NoError(t, err)
		return p.QtyCenti
	}())
}

func TestGlueIsIdempotent(t *testing.T) {
	f := newGlueFixture(t, 800, 800)
	in := app.GlueTyreSetInput{
		RequestID: uuid.Must(uuid.NewV7()),
		Glue:      domain.GlueInput{TyrePartID: f.tyre.ID, InsertPartID: f.insert.ID, Label: "J4"},
	}
	first, _, _, err := f.glueInput(in)
	require.NoError(t, err)
	again, _, _, err := f.glueInput(in)
	require.NoError(t, err)
	require.Equal(t, first.ID, again.ID)
	require.Equal(t, int64(400), f.stock(t, f.tyre))
	require.Equal(t, 1, f.count(t, fmt.Sprintf(`SELECT count(*) FROM garage.tyre_set WHERE team_id = '%s'`, f.team)))

	// The same request id with another body is a different request: refused, not replayed.
	in.Glue.Label = "J5"
	_, _, _, err = f.glueInput(in)
	require.Error(t, err)
	require.Equal(t, int64(400), f.stock(t, f.tyre))
}

func TestGlueCrossingTheMinimumWritesOneStockLowEvent(t *testing.T) {
	f := newGlueFixture(t, 500, 800)
	_, err := f.svc.UpdatePart(f.ctx, uid(f.owner), tid(f.team), app.UpdatePartInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: f.tyre.ID, Version: f.tyre.Version,
		Update: domain.PartUpdate{MinSet: true, MinStockCenti: 200},
	})
	require.NoError(t, err)
	_, _, _, err = f.glue("J4")
	require.NoError(t, err)
	require.Equal(t, 1, f.count(t, fmt.Sprintf(`SELECT count(*) FROM platform.outbox WHERE team_id = '%s' AND event_type = 'garage.stock_low'`, f.team)))
}

func TestConcurrentGluingNeverOverdrawsTheStock(t *testing.T) {
	// Stock for exactly one set: of two concurrent gluings one wins and the other is refused.
	f := newGlueFixture(t, 400, 400)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _, errs[i] = f.glue(fmt.Sprintf("J%d", i+1))
		}()
	}
	wg.Wait()
	wins := 0
	for _, err := range errs {
		if err == nil {
			wins++
			continue
		}
		require.ErrorIs(t, err, domain.ErrInsufficientStock)
	}
	require.Equal(t, 1, wins)
	require.Zero(t, f.stock(t, f.tyre))
	require.Zero(t, f.stock(t, f.insert))
}
