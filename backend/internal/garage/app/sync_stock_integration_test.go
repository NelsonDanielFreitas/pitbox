package app_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/syncapply"
)

// TestStockAppliers covers stock movements (append only) and the shopping list: the device
// id is kept, the same rules as the normal use cases hold, the effects on the shopping list
// run, a pilot never receives a price, and another team's part cannot be reached.
func TestStockAppliers(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	team := h.team(t, owner, "owner")
	other := h.user(t, "other")
	otherTeam := h.team(t, other, "owner")

	moves := garage.NewStockMovementApplier()
	shop := garage.NewShoppingItemApplier()
	parts := garage.NewPartApplier()

	applyTo := func(a syncapply.Applier, team, user uuid.UUID, o syncapply.Op) (int32, error) {
		var v int32
		err := h.syncTx(t, user, team, func(tx db.DBTX) (err error) { v, err = a.Apply(h.ctx, tx, o); return err })
		return v, err
	}
	apply := func(a syncapply.Applier, o syncapply.Op) (int32, error) { return applyTo(a, team, owner, o) }
	read := func(a syncapply.Applier, role string, id uuid.UUID) (map[string]any, bool) {
		var raw json.RawMessage
		var found bool
		require.NoError(t, h.syncTx(t, owner, team, func(tx db.DBTX) (err error) {
			raw, found, err = a.Read(h.ctx, tx, tid(team), role, id)
			return err
		}))
		if !found {
			return nil, false
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal(raw, &m))
		return m, true
	}
	newPart := func(min int) uuid.UUID {
		id := uuid.Must(uuid.NewV7())
		_, err := apply(parts, syncOp(owner, team, syncapply.Create, id, 0,
			fmt.Sprintf(`{"kind":"glow_plug","description":"Plug","min_stock_centi":%d}`, min)))
		require.NoError(t, err)
		return id
	}
	stock := func(part uuid.UUID) string {
		return h.pg.Exec(t, fmt.Sprintf(`SELECT (COALESCE(sum(qty_delta),0)*100)::bigint FROM garage.stock_movement WHERE part_id = '%s'`, part))
	}
	move := func(part uuid.UUID, fields string) (uuid.UUID, error) {
		id := uuid.Must(uuid.NewV7())
		_, err := apply(moves, syncOp(owner, team, syncapply.Append, id, 0,
			fmt.Sprintf(`{"part_id":"%s",%s}`, part, fields)))
		return id, err
	}

	t.Run("a purchase keeps the device id and adds stock", func(t *testing.T) {
		part := newPart(200)
		id, err := move(part, `"qty_delta_centi":500,"reason":"purchase","unit_cost_cents":350,"note":"Loja: X"`)
		require.NoError(t, err)
		require.Equal(t, "500", stock(part))
		require.Equal(t, "purchase|350|"+owner.String(), h.pg.Exec(t, fmt.Sprintf(
			`SELECT reason||'|'||unit_cost_cents||'|'||created_by FROM garage.stock_movement WHERE id = '%s'`, id)))
	})

	t.Run("a purchase takes the part off the shopping list and consuming below min can add it back", func(t *testing.T) {
		part := newPart(300)
		line := uuid.Must(uuid.NewV7())
		_, err := apply(shop, syncOp(owner, team, syncapply.Create, line, 0, fmt.Sprintf(`{"part_id":"%s","qty_centi":400}`, part)))
		require.NoError(t, err)
		_, err = move(part, `"qty_delta_centi":1000,"reason":"purchase"`)
		require.NoError(t, err)
		require.Equal(t, "0", h.pg.Exec(t, fmt.Sprintf(`SELECT count(*) FROM garage.shopping_item WHERE part_id = '%s'`, part)))
		_, found := read(shop, "owner", line)
		require.False(t, found, "the Pull sends a tombstone for the line")
	})

	t.Run("a hidden line comes back once the stock is at the minimum", func(t *testing.T) {
		part := newPart(100)
		line := uuid.Must(uuid.NewV7())
		_, err := apply(shop, syncOp(owner, team, syncapply.Create, line, 0, fmt.Sprintf(`{"part_id":"%s","dismissed":true}`, part)))
		require.NoError(t, err)
		_, err = move(part, `"qty_delta_centi":100,"reason":"adjustment"`)
		require.NoError(t, err)
		require.Equal(t, "0", h.pg.Exec(t, fmt.Sprintf(`SELECT count(*) FROM garage.shopping_item WHERE part_id = '%s'`, part)))
	})

	t.Run("rules of the use case hold", func(t *testing.T) {
		part := newPart(0)
		for name, fields := range map[string]string{
			"unknown reason":      `"qty_delta_centi":100,"reason":"gift"`,
			"purchase negative":   `"qty_delta_centi":-100,"reason":"purchase"`,
			"consumed positive":   `"qty_delta_centi":100,"reason":"consumed"`,
			"zero":                `"qty_delta_centi":0,"reason":"adjustment"`,
			"cost off a purchase": `"qty_delta_centi":100,"reason":"adjustment","unit_cost_cents":5`,
			"lost without reason": `"qty_delta_centi":-100,"reason":"lost"`,
			"float quantity":      `"qty_delta_centi":1.5,"reason":"adjustment"`,
			"unknown field":       `"qty_delta_centi":100,"reason":"adjustment","team_id":"x"`,
		} {
			_, err := move(part, fields)
			require.ErrorIs(t, err, syncapply.ErrInvalidFields, name)
		}
		require.Equal(t, "0", stock(part))
		_, err := apply(moves, syncOp(owner, team, syncapply.Create, uuid.Must(uuid.NewV7()), 0, fmt.Sprintf(`{"part_id":"%s","qty_delta_centi":100,"reason":"purchase"}`, part)))
		require.ErrorIs(t, err, syncapply.ErrInvalidFields, "a movement is only ever appended")
	})

	t.Run("the same id twice is refused, not doubled", func(t *testing.T) {
		part := newPart(0)
		id := uuid.Must(uuid.NewV7())
		o := syncOp(owner, team, syncapply.Append, id, 0, fmt.Sprintf(`{"part_id":"%s","qty_delta_centi":100,"reason":"purchase"}`, part))
		_, err := apply(moves, o)
		require.NoError(t, err)
		_, err = apply(moves, o)
		require.ErrorIs(t, err, syncapply.ErrInvalidFields)
		require.Equal(t, "100", stock(part))
	})

	t.Run("the part must exist in this team", func(t *testing.T) {
		foreign := uuid.Must(uuid.NewV7())
		_, err := applyTo(parts, otherTeam, other, syncOp(other, otherTeam, syncapply.Create, foreign, 0, `{"kind":"glow_plug","description":"Theirs"}`))
		require.NoError(t, err)
		_, err = move(foreign, `"qty_delta_centi":100,"reason":"purchase"`)
		require.ErrorIs(t, err, syncapply.ErrInvalidRef)
		_, err = move(uuid.Must(uuid.NewV7()), `"qty_delta_centi":100,"reason":"purchase"`)
		require.ErrorIs(t, err, syncapply.ErrInvalidRef)
		require.Equal(t, "0", h.pg.Exec(t, fmt.Sprintf(`SELECT count(*) FROM garage.stock_movement WHERE part_id = '%s'`, foreign)))
	})

	t.Run("occurred_at is kept inside a sane window", func(t *testing.T) {
		part := newPart(0)
		recent := time.Now().Add(-3 * time.Hour).UTC().Truncate(time.Second)
		id, err := move(part, fmt.Sprintf(`"qty_delta_centi":100,"reason":"purchase","occurred_at":"%s"`, recent.Format(time.RFC3339)))
		require.NoError(t, err)
		require.Equal(t, "t", h.pg.Exec(t, fmt.Sprintf(`SELECT occurred_at = '%s' FROM garage.stock_movement WHERE id = '%s'`, recent.Format(time.RFC3339), id)))
		future, err := move(part, fmt.Sprintf(`"qty_delta_centi":100,"reason":"purchase","occurred_at":"%s"`, time.Now().Add(48*time.Hour).UTC().Format(time.RFC3339)))
		require.NoError(t, err)
		require.Equal(t, "t", h.pg.Exec(t, fmt.Sprintf(`SELECT occurred_at < now() + interval '1 minute' FROM garage.stock_movement WHERE id = '%s'`, future)))
		_, err = move(part, `"qty_delta_centi":100,"reason":"purchase","occurred_at":"yesterday"`)
		require.ErrorIs(t, err, syncapply.ErrInvalidFields)
	})

	t.Run("read hides the price from a pilot", func(t *testing.T) {
		part := newPart(0)
		id, err := move(part, `"qty_delta_centi":100,"reason":"purchase","unit_cost_cents":900`)
		require.NoError(t, err)
		m, found := read(moves, "mechanic", id)
		require.True(t, found)
		require.EqualValues(t, 900, m["unit_cost_cents"])
		m, found = read(moves, "pilot", id)
		require.True(t, found)
		require.NotContains(t, m, "unit_cost_cents")
		require.EqualValues(t, 100, m["qty_delta_centi"])
	})

	t.Run("shopping line create, patch, delete", func(t *testing.T) {
		part := newPart(500)
		line := uuid.Must(uuid.NewV7())
		v, err := apply(shop, syncOp(owner, team, syncapply.Create, line, 0, fmt.Sprintf(`{"part_id":"%s","qty_centi":700,"note":"cola"}`, part)))
		require.NoError(t, err)
		require.Equal(t, int32(1), v)
		_, err = apply(shop, syncOp(owner, team, syncapply.Create, uuid.Must(uuid.NewV7()), 0, fmt.Sprintf(`{"part_id":"%s","qty_centi":100}`, part)))
		require.ErrorIs(t, err, syncapply.ErrInvalidFields, "one line per part")

		_, err = apply(shop, syncOp(owner, team, syncapply.Patch, line, 5, `{"qty_centi":900}`))
		require.ErrorIs(t, err, syncapply.ErrStale)
		_, err = apply(shop, syncOp(owner, team, syncapply.Patch, line, 1, fmt.Sprintf(`{"part_id":"%s"}`, uuid.New())))
		require.ErrorIs(t, err, syncapply.ErrInvalidFields, "a line cannot move to another part")
		v, err = apply(shop, syncOp(owner, team, syncapply.Patch, line, 1, `{"qty_centi":900,"note":null}`))
		require.NoError(t, err)
		require.Equal(t, int32(2), v)
		m, found := read(shop, "pilot", line)
		require.True(t, found)
		require.EqualValues(t, 900, m["qty_centi"])
		require.Nil(t, m["note"])

		_, err = apply(shop, syncOp(owner, team, syncapply.Delete, line, 1, `{}`))
		require.ErrorIs(t, err, syncapply.ErrStale)
		_, err = apply(shop, syncOp(owner, team, syncapply.Delete, line, 2, `{}`))
		require.NoError(t, err)
		_, found = read(shop, "owner", line)
		require.False(t, found)
	})

	t.Run("a visible line needs a positive quantity", func(t *testing.T) {
		part := newPart(0) // not below its minimum: no suggestion
		_, err := apply(shop, syncOp(owner, team, syncapply.Create, uuid.Must(uuid.NewV7()), 0, fmt.Sprintf(`{"part_id":"%s"}`, part)))
		require.ErrorIs(t, err, syncapply.ErrInvalidFields)
		_, err = apply(shop, syncOp(owner, team, syncapply.Create, uuid.Must(uuid.NewV7()), 0, fmt.Sprintf(`{"part_id":"%s","qty_centi":-5}`, part)))
		require.ErrorIs(t, err, syncapply.ErrInvalidFields)
		_, err = apply(shop, syncOp(owner, team, syncapply.Create, uuid.Must(uuid.NewV7()), 0, fmt.Sprintf(`{"part_id":"%s"}`, uuid.New())))
		require.ErrorIs(t, err, syncapply.ErrInvalidRef)
	})

	t.Run("another team cannot see or touch a line", func(t *testing.T) {
		part := newPart(100)
		line := uuid.Must(uuid.NewV7())
		_, err := apply(shop, syncOp(owner, team, syncapply.Create, line, 0, fmt.Sprintf(`{"part_id":"%s","qty_centi":100}`, part)))
		require.NoError(t, err)
		_, err = applyTo(shop, otherTeam, other, syncOp(other, otherTeam, syncapply.Patch, line, 1, `{"qty_centi":1}`))
		require.ErrorIs(t, err, syncapply.ErrNotFound)
		_, err = applyTo(shop, otherTeam, other, syncOp(other, otherTeam, syncapply.Delete, line, 1, `{}`))
		require.Error(t, err)
	})
}
