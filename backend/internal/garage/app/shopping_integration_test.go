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

type shopFixture struct {
	*harness
	owner, team uuid.UUID
}

func newShopFixture(t *testing.T) shopFixture {
	t.Helper()
	h := newHarness(t)
	owner := h.user(t, "owner")
	return shopFixture{harness: h, owner: owner, team: h.team(t, owner, "owner")}
}

func (f shopFixture) part(t *testing.T, desc string, min, initial int64, reorder *int64, cost *int32) domain.Part {
	t.Helper()
	p, err := f.svc.CreatePart(f.ctx, uid(f.owner), tid(f.team), app.CreatePartInput{
		RequestID: uuid.Must(uuid.NewV7()), Kind: domain.PartGlowPlug, Description: desc, Unit: "un",
		MinStockCenti: min, InitialQtyCenti: initial, ReorderToCenti: reorder, UnitCostCents: cost,
	})
	require.NoError(t, err)
	return p
}

func (f shopFixture) list(t *testing.T, user uuid.UUID) []domain.ShoppingLine {
	t.Helper()
	lines, _, err := f.svc.ListShoppingList(f.ctx, uid(user), tid(f.team), 100, "")
	require.NoError(t, err)
	return lines
}

func (f shopFixture) move(t *testing.T, part uuid.UUID, delta int64, reason string, details domain.MovementDetails) {
	t.Helper()
	_, _, err := f.svc.AddStockMovement(f.ctx, uid(f.owner), tid(f.team), app.AddStockMovementInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: part, QtyDeltaCenti: delta, Reason: reason,
		UnitCostCents: details.UnitCostCents, LossReason: details.LossReason,
	})
	require.NoError(t, err)
}

func (f shopFixture) remove(part uuid.UUID) error {
	return f.svc.RemoveShoppingItem(f.ctx, uid(f.owner), tid(f.team), app.RemoveShoppingItemInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: part,
	})
}

func lineFor(lines []domain.ShoppingLine, part uuid.UUID) (domain.ShoppingLine, bool) {
	for _, l := range lines {
		if l.PartID == part {
			return l, true
		}
	}
	return domain.ShoppingLine{}, false
}

func TestPurchaseStoresPriceAndChangingTheCostDoesNotRewriteHistory(t *testing.T) {
	f := newShopFixture(t)
	nine := int32(950)
	p := f.part(t, "Vela", 0, 0, nil, &nine)

	eleven := int32(1100)
	f.move(t, p.ID, 400, domain.StockPurchase, domain.MovementDetails{UnitCostCents: &nine})
	updated, err := f.svc.UpdatePart(f.ctx, uid(f.owner), tid(f.team), app.UpdatePartInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: p.ID, Version: p.Version,
		Update: domain.PartUpdate{CostSet: true, UnitCostCents: &eleven},
	})
	require.NoError(t, err)
	require.Equal(t, int32(1100), *updated.UnitCostCents)
	f.move(t, p.ID, 100, domain.StockPurchase, domain.MovementDetails{UnitCostCents: &eleven})

	moves, _, err := f.svc.ListStockMovements(f.ctx, uid(f.owner), tid(f.team), p.ID, 10, "")
	require.NoError(t, err)
	require.Len(t, moves, 2)
	require.Equal(t, int32(1100), *moves[0].UnitCostCents) // newest first
	require.Equal(t, int32(950), *moves[1].UnitCostCents)  // the earlier purchase kept its price
}

func TestLossNeedsReasonAndPriceOnlyOnPurchase(t *testing.T) {
	f := newShopFixture(t)
	p := f.part(t, "Vela", 0, 500, nil, nil)
	price := int32(100)

	_, _, err := f.svc.AddStockMovement(f.ctx, uid(f.owner), tid(f.team), app.AddStockMovementInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: p.ID, QtyDeltaCenti: -100, Reason: domain.StockLost,
	})
	require.ErrorIs(t, err, domain.ErrInvalid)
	_, _, err = f.svc.AddStockMovement(f.ctx, uid(f.owner), tid(f.team), app.AddStockMovementInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: p.ID, QtyDeltaCenti: -100, Reason: domain.StockConsumed, UnitCostCents: &price,
	})
	require.ErrorIs(t, err, domain.ErrInvalid)
	_, m, err := f.svc.AddStockMovement(f.ctx, uid(f.owner), tid(f.team), app.AddStockMovementInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: p.ID, QtyDeltaCenti: -100, Reason: domain.StockLost, LossReason: domain.LossExpired,
	})
	require.NoError(t, err)
	require.Equal(t, domain.LossExpired, m.LossReason)
	require.Equal(t, 1, f.count(t, fmt.Sprintf(`SELECT count(*) FROM garage.stock_movement WHERE part_id = '%s' AND loss_reason = 'expired'`, p.ID)))
}

func TestShoppingListSuggestsUpToReorderToAndMinimum(t *testing.T) {
	f := newShopFixture(t)
	target := int64(2500)
	cost := int32(1000)
	plain := f.part(t, "Combustível", 1000, 300, nil, &cost)
	restock := f.part(t, "Óleo", 1000, 300, &target, nil)
	f.part(t, "Folga", 1000, 5000, nil, nil)

	lines := f.list(t, f.owner)
	require.Len(t, lines, 2)
	l, ok := lineFor(lines, plain.ID)
	require.True(t, ok)
	require.True(t, l.BelowMin)
	require.Equal(t, int64(700), l.QtyCenti)
	require.Zero(t, l.ItemVersion)
	require.Equal(t, int64(7000), *l.EstimatedCostCents())
	l, ok = lineFor(lines, restock.ID)
	require.True(t, ok)
	require.Equal(t, int64(2200), l.QtyCenti)
	require.Equal(t, int64(2200), l.SuggestedQtyCenti)
}

func TestManualLineIsSeenByAnotherMemberAndPilotsSeeNoCosts(t *testing.T) {
	f := newShopFixture(t)
	cost := int32(2000)
	spare := f.part(t, "Combustível", 0, 5000, nil, &cost)
	qty := int64(400)
	line, err := f.svc.SetShoppingItem(f.ctx, uid(f.owner), tid(f.team), app.SetShoppingItemInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: spare.ID, QtyCenti: &qty,
	})
	require.NoError(t, err)
	require.False(t, line.BelowMin)
	require.Equal(t, int32(1), line.ItemVersion)

	mechanic := f.user(t, "mechanic")
	f.member(t, f.team, mechanic, "mechanic")
	pilot := f.user(t, "pilot")
	f.member(t, f.team, pilot, "pilot")

	seen, ok := lineFor(f.list(t, mechanic), spare.ID)
	require.True(t, ok)
	require.Equal(t, int64(400), seen.QtyCenti)
	require.Equal(t, int64(8000), *seen.EstimatedCostCents())
	asPilot, ok := lineFor(f.list(t, pilot), spare.ID)
	require.True(t, ok)
	require.Nil(t, asPilot.EstimatedCostCents())
	require.Nil(t, asPilot.UnitCostCents)

	// A pilot reads the list but cannot write it, and sees no price on the movements.
	_, err = f.svc.SetShoppingItem(f.ctx, uid(pilot), tid(f.team), app.SetShoppingItemInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: spare.ID, QtyCenti: &qty, ExpectedVersion: 1,
	})
	require.ErrorIs(t, err, domain.ErrForbidden)
	require.ErrorIs(t, f.svc.RemoveShoppingItem(f.ctx, uid(pilot), tid(f.team), app.RemoveShoppingItemInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: spare.ID,
	}), domain.ErrForbidden)
	f.move(t, spare.ID, 100, domain.StockPurchase, domain.MovementDetails{UnitCostCents: &cost})
	moves, _, err := f.svc.ListStockMovements(f.ctx, uid(pilot), tid(f.team), spare.ID, 10, "")
	require.NoError(t, err)
	for _, m := range moves {
		require.Nil(t, m.UnitCostCents)
	}
	moves, _, err = f.svc.ListStockMovements(f.ctx, uid(mechanic), tid(f.team), spare.ID, 10, "")
	require.NoError(t, err)
	require.NotNil(t, moves[0].UnitCostCents)
}

func TestPurchaseRemovesTheLineInTheSameTransaction(t *testing.T) {
	f := newShopFixture(t)
	p := f.part(t, "Vela", 1000, 200, nil, nil)
	qty := int64(300)
	_, err := f.svc.SetShoppingItem(f.ctx, uid(f.owner), tid(f.team), app.SetShoppingItemInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: p.ID, QtyCenti: &qty,
	})
	require.NoError(t, err)
	l, _ := lineFor(f.list(t, f.owner), p.ID)
	require.Equal(t, int64(300), l.QtyCenti) // the adjusted quantity wins over the suggestion

	f.move(t, p.ID, 300, domain.StockPurchase, domain.MovementDetails{})
	require.Zero(t, f.count(t, fmt.Sprintf(`SELECT count(*) FROM garage.shopping_item WHERE part_id = '%s'`, p.ID)))
	// Still below the minimum: the automatic line is back with a fresh suggestion.
	l, ok := lineFor(f.list(t, f.owner), p.ID)
	require.True(t, ok)
	require.Equal(t, int64(500), l.SuggestedQtyCenti)
	require.Zero(t, l.ItemVersion)
	f.move(t, p.ID, 600, domain.StockPurchase, domain.MovementDetails{})
	_, ok = lineFor(f.list(t, f.owner), p.ID)
	require.False(t, ok)
}

func TestRemovingAnAutomaticLineHidesItUntilTheStockIsBackAtTheMinimum(t *testing.T) {
	f := newShopFixture(t)
	p := f.part(t, "Vela", 1000, 200, nil, nil)

	require.NoError(t, f.remove(p.ID))
	_, ok := lineFor(f.list(t, f.owner), p.ID)
	require.False(t, ok)
	require.ErrorIs(t, f.remove(p.ID), domain.ErrNotFound) // already hidden

	// Spending more stock keeps it hidden.
	f.move(t, p.ID, -100, domain.StockConsumed, domain.MovementDetails{})
	_, ok = lineFor(f.list(t, f.owner), p.ID)
	require.False(t, ok)
	require.Equal(t, 1, f.count(t, fmt.Sprintf(`SELECT count(*) FROM garage.shopping_item WHERE part_id = '%s' AND dismissed`, p.ID)))

	// Back at the minimum: the hidden line is dropped, so the next shortage shows again.
	f.move(t, p.ID, 1000, domain.StockAdjustment, domain.MovementDetails{})
	require.Zero(t, f.count(t, fmt.Sprintf(`SELECT count(*) FROM garage.shopping_item WHERE part_id = '%s'`, p.ID)))
	f.move(t, p.ID, -500, domain.StockConsumed, domain.MovementDetails{})
	_, ok = lineFor(f.list(t, f.owner), p.ID)
	require.True(t, ok)
}

func TestLoweringTheMinimumClearsAHiddenLineAndManualLinesCanBeRemoved(t *testing.T) {
	f := newShopFixture(t)
	p := f.part(t, "Vela", 1000, 200, nil, nil)
	require.NoError(t, f.remove(p.ID))
	_, err := f.svc.UpdatePart(f.ctx, uid(f.owner), tid(f.team), app.UpdatePartInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: p.ID, Version: p.Version,
		Update: domain.PartUpdate{MinSet: true, MinStockCenti: 100},
	})
	require.NoError(t, err)
	require.Zero(t, f.count(t, fmt.Sprintf(`SELECT count(*) FROM garage.shopping_item WHERE part_id = '%s'`, p.ID)))

	qty := int64(100)
	_, err = f.svc.SetShoppingItem(f.ctx, uid(f.owner), tid(f.team), app.SetShoppingItemInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: p.ID, QtyCenti: &qty,
	})
	require.NoError(t, err)
	require.NoError(t, f.remove(p.ID))
	require.Zero(t, f.count(t, fmt.Sprintf(`SELECT count(*) FROM garage.shopping_item WHERE part_id = '%s'`, p.ID)))
	require.ErrorIs(t, f.remove(p.ID), domain.ErrNotFound)
}

func TestShoppingValidationAndVersions(t *testing.T) {
	f := newShopFixture(t)
	ok := f.part(t, "Folga", 0, 5000, nil, nil)
	_, err := f.svc.SetShoppingItem(f.ctx, uid(f.owner), tid(f.team), app.SetShoppingItemInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: ok.ID,
	})
	require.ErrorIs(t, err, domain.ErrInvalid) // not below the minimum and no quantity
	zero := int64(0)
	_, err = f.svc.SetShoppingItem(f.ctx, uid(f.owner), tid(f.team), app.SetShoppingItemInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: ok.ID, QtyCenti: &zero,
	})
	require.ErrorIs(t, err, domain.ErrInvalid)

	qty := int64(100)
	set := func(version int32, q int64) (domain.ShoppingLine, error) {
		return f.svc.SetShoppingItem(f.ctx, uid(f.owner), tid(f.team), app.SetShoppingItemInput{
			RequestID: uuid.Must(uuid.NewV7()), PartID: ok.ID, QtyCenti: &q, ExpectedVersion: version,
		})
	}
	_, err = set(3, qty)
	require.ErrorIs(t, err, domain.ErrConflict) // no line stored yet
	line, err := set(0, qty)
	require.NoError(t, err)
	_, err = set(0, 200)
	require.ErrorIs(t, err, domain.ErrConflict) // a line is already stored
	line, err = set(line.ItemVersion, 200)
	require.NoError(t, err)
	require.Equal(t, int32(2), line.ItemVersion)
	_, err = set(1, 300)
	require.ErrorIs(t, err, domain.ErrConflict) // stale version
}

func TestShoppingIsIdempotentAndDeletingAPartDropsItsLine(t *testing.T) {
	f := newShopFixture(t)
	p := f.part(t, "Vela", 1000, 200, nil, nil)
	in := app.SetShoppingItemInput{RequestID: uuid.Must(uuid.NewV7()), PartID: p.ID}
	qty := int64(500)
	in.QtyCenti = &qty
	first, err := f.svc.SetShoppingItem(f.ctx, uid(f.owner), tid(f.team), in)
	require.NoError(t, err)
	again, err := f.svc.SetShoppingItem(f.ctx, uid(f.owner), tid(f.team), in)
	require.NoError(t, err)
	require.Equal(t, first, again)
	require.Equal(t, 1, f.count(t, `SELECT count(*) FROM garage.shopping_item`))
	require.Equal(t, 1, f.count(t, fmt.Sprintf(`SELECT count(*) FROM audit.audit_log WHERE team_id = '%s' AND action = 'shopping.set'`, f.team)))

	rm := app.RemoveShoppingItemInput{RequestID: uuid.Must(uuid.NewV7()), PartID: p.ID}
	require.NoError(t, f.svc.RemoveShoppingItem(f.ctx, uid(f.owner), tid(f.team), rm))
	require.NoError(t, f.svc.RemoveShoppingItem(f.ctx, uid(f.owner), tid(f.team), rm)) // replay
	require.Equal(t, 1, f.count(t, `SELECT count(*) FROM garage.shopping_item WHERE dismissed`))

	require.NoError(t, f.svc.DeletePart(f.ctx, uid(f.owner), tid(f.team), uuid.Must(uuid.NewV7()), p.ID, p.Version))
	require.Zero(t, f.count(t, `SELECT count(*) FROM garage.shopping_item`))
	require.Empty(t, f.list(t, f.owner))
}

func TestShoppingOtherTeamsAndPagination(t *testing.T) {
	f := newShopFixture(t)
	p := f.part(t, "Vela", 1000, 200, nil, nil)
	stranger := f.user(t, "stranger")
	other := f.harness.team(t, stranger, "owner")
	qty := int64(100)

	_, _, err := f.svc.ListShoppingList(f.ctx, uid(stranger), tid(f.team), 10, "")
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, err = f.svc.SetShoppingItem(f.ctx, uid(stranger), tid(f.team), app.SetShoppingItemInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: p.ID, QtyCenti: &qty,
	})
	require.ErrorIs(t, err, domain.ErrNotFound)
	// Their own team, but this team's part (BOLA).
	_, err = f.svc.SetShoppingItem(f.ctx, uid(stranger), tid(other), app.SetShoppingItemInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: p.ID, QtyCenti: &qty,
	})
	require.ErrorIs(t, err, domain.ErrNotFound)
	require.ErrorIs(t, f.svc.RemoveShoppingItem(f.ctx, uid(stranger), tid(other), app.RemoveShoppingItemInput{
		RequestID: uuid.Must(uuid.NewV7()), PartID: p.ID,
	}), domain.ErrNotFound)
	lines, _, err := f.svc.ListShoppingList(f.ctx, uid(stranger), tid(other), 10, "")
	require.NoError(t, err)
	require.Empty(t, lines)

	f.part(t, "Segunda", 1000, 0, nil, nil)
	f.part(t, "Terceira", 1000, 0, nil, nil)
	page, next, err := f.svc.ListShoppingList(f.ctx, uid(f.owner), tid(f.team), 2, "")
	require.NoError(t, err)
	require.Len(t, page, 2)
	require.NotEmpty(t, next)
	rest, next, err := f.svc.ListShoppingList(f.ctx, uid(f.owner), tid(f.team), 2, next)
	require.NoError(t, err)
	require.Len(t, rest, 1)
	require.Empty(t, next)
}

func TestConcurrentSetShoppingItemHasOneWinner(t *testing.T) {
	f := newShopFixture(t)
	p := f.part(t, "Folga", 0, 5000, nil, nil)
	const workers = 6
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			q := int64(100 + i)
			_, errs[i] = f.svc.SetShoppingItem(f.ctx, uid(f.owner), tid(f.team), app.SetShoppingItemInput{
				RequestID: uuid.Must(uuid.NewV7()), PartID: p.ID, QtyCenti: &q,
			})
		}()
	}
	wg.Wait()
	wins := 0
	for _, err := range errs {
		if err == nil {
			wins++
		} else {
			require.ErrorIs(t, err, domain.ErrConflict)
		}
	}
	require.Equal(t, 1, wins)
	require.Equal(t, 1, f.count(t, `SELECT count(*) FROM garage.shopping_item`))
}

func TestPartSpecsUnitAndReorderToAreValidatedOnTheService(t *testing.T) {
	f := newShopFixture(t)
	_, err := f.svc.CreatePart(f.ctx, uid(f.owner), tid(f.team), app.CreatePartInput{
		RequestID: uuid.Must(uuid.NewV7()), Kind: domain.PartFuel, Description: "Combustível", Unit: "l",
		Specs: map[string]any{"nitro_pct": 25.0, "oil_pct": 9.0},
	})
	require.NoError(t, err)
	_, err = f.svc.CreatePart(f.ctx, uid(f.owner), tid(f.team), app.CreatePartInput{
		RequestID: uuid.Must(uuid.NewV7()), Kind: domain.PartFuel, Description: "Combustível", Unit: "l",
		Specs: map[string]any{"colour": "red"},
	})
	require.ErrorIs(t, err, domain.ErrInvalid)
	_, err = f.svc.CreatePart(f.ctx, uid(f.owner), tid(f.team), app.CreatePartInput{
		RequestID: uuid.Must(uuid.NewV7()), Kind: domain.PartFuel, Description: "Combustível", Unit: "kg",
	})
	require.ErrorIs(t, err, domain.ErrInvalid)
	low := int64(500)
	_, err = f.svc.CreatePart(f.ctx, uid(f.owner), tid(f.team), app.CreatePartInput{
		RequestID: uuid.Must(uuid.NewV7()), Kind: domain.PartFuel, Description: "Combustível", Unit: "l",
		MinStockCenti: 1000, ReorderToCenti: &low,
	})
	require.ErrorIs(t, err, domain.ErrInvalid)
}
