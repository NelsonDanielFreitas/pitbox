package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/syncapply"
)

func syncOp(user, team uuid.UUID, kind syncapply.Kind, id uuid.UUID, expected int32, fields string) syncapply.Op {
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(fields), &m); err != nil {
		panic(err)
	}
	return syncapply.Op{OpID: uuid.New(), TeamID: tid(team), UserID: uid(user), Role: "mechanic",
		Kind: kind, EntityID: id, Fields: m, ExpectedVersion: expected}
}

func (h *harness) syncTx(t *testing.T, user, team uuid.UUID, fn func(tx db.DBTX) error) error {
	t.Helper()
	return db.New(h.pool).WithTeamTx(h.ctx, uid(user), tid(team), func(_ context.Context, tx db.DBTX, _ db.Membership) error {
		return fn(tx)
	})
}

// TestInventoryAppliers covers engines, tyre sets and parts: the device id is kept, rules of
// the normal use cases hold, unknown or mistyped fields are refused, a stale base is stale,
// another team's data is not reachable and a pilot never receives a purchase price.
func TestInventoryAppliers(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	team := h.team(t, owner, "owner")
	other := h.user(t, "other")
	otherTeam := h.team(t, other, "owner")

	applyTo := func(a syncapply.Applier, team, user uuid.UUID, o syncapply.Op) (int32, error) {
		var v int32
		err := h.syncTx(t, user, team, func(tx db.DBTX) (err error) { v, err = a.Apply(h.ctx, tx, o); return err })
		return v, err
	}
	apply := func(a syncapply.Applier, o syncapply.Op) (int32, error) { return applyTo(a, team, owner, o) }
	read := func(a syncapply.Applier, team, user uuid.UUID, role string, id uuid.UUID) (map[string]any, bool) {
		var raw json.RawMessage
		var found bool
		require.NoError(t, h.syncTx(t, user, team, func(tx db.DBTX) (err error) {
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

	t.Run("the registry lists them", func(t *testing.T) {
		names := map[string]bool{}
		for _, a := range garage.SyncAppliers() {
			names[a.Entity()] = true
		}
		require.Equal(t, map[string]bool{"garage.car": true, "garage.engine": true, "garage.tyre_set": true, "garage.part": true,
			"garage.stock_movement": true, "garage.shopping_item": true,
			"garage.engine_mount": true, "garage.part_install": true}, names)
	})

	t.Run("engine", func(t *testing.T) {
		a := garage.NewEngineApplier()
		id := uuid.Must(uuid.NewV7())
		v, err := apply(a, syncOp(owner, team, syncapply.Create, id, 0, `{"make":"  REDS ","model":"R7","purchased_on":"2026-03-04","status":"spare"}`))
		require.NoError(t, err)
		require.Equal(t, int32(1), v)
		require.Equal(t, "REDS|R7|2026-03-04|spare|"+owner.String(), h.pg.Exec(t, fmt.Sprintf(
			`SELECT make||'|'||model||'|'||purchased_on||'|'||status||'|'||created_by FROM garage.engine WHERE id = '%s'`, id)))

		_, err = apply(a, syncOp(owner, team, syncapply.Create, id, 0, `{"make":"a","model":"b"}`))
		require.Error(t, err, "an existing id is not overwritten")

		v, err = apply(a, syncOp(owner, team, syncapply.Patch, id, 1, `{"notes":"rodado","serial_number":null}`))
		require.NoError(t, err)
		require.Equal(t, int32(2), v)
		_, err = apply(a, syncOp(owner, team, syncapply.Patch, id, 1, `{"notes":"stale"}`))
		require.ErrorIs(t, err, syncapply.ErrStale)

		for name, o := range map[string]syncapply.Op{
			"unknown field":   syncOp(owner, team, syncapply.Create, uuid.New(), 0, `{"make":"a","model":"b","tanks_total":9}`),
			"no model":        syncOp(owner, team, syncapply.Create, uuid.New(), 0, `{"make":"a"}`),
			"bad status":      syncOp(owner, team, syncapply.Create, uuid.New(), 0, `{"make":"a","model":"b","status":"exploded"}`),
			"bad date":        syncOp(owner, team, syncapply.Create, uuid.New(), 0, `{"make":"a","model":"b","purchased_on":"31/12/2026"}`),
			"make too long":   syncOp(owner, team, syncapply.Create, uuid.New(), 0, fmt.Sprintf(`{"make":%q,"model":"b"}`, strings.Repeat("x", 500))),
			"clear make":      syncOp(owner, team, syncapply.Patch, id, 2, `{"make":null}`),
			"delete":          syncOp(owner, team, syncapply.Delete, id, 2, `{}`),
			"number as model": syncOp(owner, team, syncapply.Create, uuid.New(), 0, `{"make":"a","model":7}`),
		} {
			_, err := apply(a, o)
			require.ErrorIs(t, err, syncapply.ErrInvalidFields, name)
		}

		_, err = applyTo(a, otherTeam, other, syncOp(other, otherTeam, syncapply.Patch, id, 2, `{"notes":"x"}`))
		require.ErrorIs(t, err, syncapply.ErrNotFound, "another team's engine")
		_, found := read(a, otherTeam, other, "owner", id)
		require.False(t, found)

		m, found := read(a, team, owner, "mechanic", id)
		require.True(t, found)
		require.Equal(t, "REDS", m["make"])
		require.Equal(t, "2026-03-04", m["purchased_on"])
		require.Nil(t, m["serial_number"])
	})

	t.Run("a mounted engine cannot be retired", func(t *testing.T) {
		a := garage.NewEngineApplier()
		engine, car := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
		h.pg.Exec(t, fmt.Sprintf(`INSERT INTO garage.car (id, team_id, make, model) VALUES ('%s', '%s', 'M', 'X')`, car, team))
		_, err := apply(a, syncOp(owner, team, syncapply.Create, engine, 0, `{"make":"a","model":"b"}`))
		require.NoError(t, err)
		h.pg.Exec(t, fmt.Sprintf(`INSERT INTO garage.engine_mount (team_id, engine_id, car_id) VALUES ('%s', '%s', '%s')`, team, engine, car))
		_, err = apply(a, syncOp(owner, team, syncapply.Patch, engine, 1, `{"status":"retired"}`))
		require.ErrorIs(t, err, syncapply.ErrInvalidRef)
	})

	t.Run("tyre set", func(t *testing.T) {
		a := garage.NewTyreSetApplier()
		id := uuid.Must(uuid.NewV7())
		_, err := apply(a, syncOp(owner, team, syncapply.Create, id, 0, `{"label":"J2","make":"Pro-Line","model":"Badlands","glued_on":"2026-10-26"}`))
		require.NoError(t, err)
		require.Equal(t, "J2|new", h.pg.Exec(t, fmt.Sprintf(`SELECT label||'|'||status FROM garage.tyre_set WHERE id = '%s'`, id)))

		_, err = apply(a, syncOp(owner, team, syncapply.Create, uuid.New(), 0, `{"label":"J2","make":"a","model":"b"}`))
		require.ErrorIs(t, err, syncapply.ErrInvalidFields, "the label of an active set is unique")

		v, err := apply(a, syncOp(owner, team, syncapply.Patch, id, 1, `{"status":"worn","wear_notes":"gasto"}`))
		require.NoError(t, err)
		require.Equal(t, int32(2), v)
		_, err = apply(a, syncOp(owner, team, syncapply.Create, uuid.New(), 0, `{"label":"Z","make":"a","model":"b","tyre_part_id":"`+uuid.NewString()+`"}`))
		require.ErrorIs(t, err, syncapply.ErrInvalidFields, "glue references are not accepted here")

		_, err = applyTo(a, otherTeam, other, syncOp(other, otherTeam, syncapply.Patch, id, 2, `{"status":"used"}`))
		require.ErrorIs(t, err, syncapply.ErrNotFound)
	})

	t.Run("part", func(t *testing.T) {
		a := garage.NewPartApplier()
		id := uuid.Must(uuid.NewV7())
		_, err := apply(a, syncOp(owner, team, syncapply.Create, id, 0,
			`{"kind":"glow_plug","brand":"OS","description":"Vela OS P3","unit":"un","specs":{},"min_stock_centi":200,"unit_cost_cents":950,"has_service_life":true,"reorder_to_centi":500}`))
		require.NoError(t, err)
		require.Equal(t, "glow_plug|2.00|950|5.00|true", h.pg.Exec(t, fmt.Sprintf(
			`SELECT kind||'|'||min_stock||'|'||unit_cost_cents||'|'||reorder_to||'|'||has_service_life FROM garage.part WHERE id = '%s'`, id)))
		require.Equal(t, "0", h.pg.Exec(t, fmt.Sprintf(`SELECT count(*) FROM garage.stock_movement WHERE part_id = '%s'`, id)),
			"stock comes only from stock movements")

		v, err := apply(a, syncOp(owner, team, syncapply.Patch, id, 1, `{"min_stock_centi":300,"unit_cost_cents":null}`))
		require.NoError(t, err)
		require.Equal(t, int32(2), v)
		_, err = apply(a, syncOp(owner, team, syncapply.Patch, id, 1, `{"description":"stale"}`))
		require.ErrorIs(t, err, syncapply.ErrStale)

		for name, o := range map[string]syncapply.Op{
			"unknown kind":     syncOp(owner, team, syncapply.Create, uuid.New(), 0, `{"kind":"rocket","description":"x"}`),
			"initial qty":      syncOp(owner, team, syncapply.Create, uuid.New(), 0, `{"kind":"other","description":"x","initial_qty_centi":100}`),
			"negative min":     syncOp(owner, team, syncapply.Create, uuid.New(), 0, `{"kind":"other","description":"x","min_stock_centi":-1}`),
			"min as string":    syncOp(owner, team, syncapply.Create, uuid.New(), 0, `{"kind":"other","description":"x","min_stock_centi":"7"}`),
			"service as int":   syncOp(owner, team, syncapply.Create, uuid.New(), 0, `{"kind":"other","description":"x","has_service_life":1}`),
			"reorder below":    syncOp(owner, team, syncapply.Create, uuid.New(), 0, `{"kind":"other","description":"x","min_stock_centi":500,"reorder_to_centi":100}`),
			"specs not object": syncOp(owner, team, syncapply.Create, uuid.New(), 0, `{"kind":"other","description":"x","specs":[1]}`),
			"bad unit":         syncOp(owner, team, syncapply.Create, uuid.New(), 0, `{"kind":"other","description":"x","unit":"kg"}`),
			"clear kind":       syncOp(owner, team, syncapply.Patch, id, 2, `{"kind":null}`),
			"clear min":        syncOp(owner, team, syncapply.Patch, id, 2, `{"min_stock_centi":null}`),
		} {
			_, err := apply(a, o)
			require.ErrorIs(t, err, syncapply.ErrInvalidFields, name)
		}

		// A pilot never receives the purchase price; the mechanic does.
		h.pg.Exec(t, fmt.Sprintf(`UPDATE garage.part SET unit_cost_cents = 950 WHERE id = '%s'`, id))
		pilot, _ := read(a, team, owner, "pilot", id)
		_, leaked := pilot["unit_cost_cents"]
		require.False(t, leaked, "a pilot must not get the price")
		mechanic, _ := read(a, team, owner, "mechanic", id)
		require.EqualValues(t, 950, mechanic["unit_cost_cents"])
		require.EqualValues(t, 300, mechanic["min_stock_centi"])

		_, err = applyTo(a, otherTeam, other, syncOp(other, otherTeam, syncapply.Patch, id, 3, `{"description":"x"}`))
		require.ErrorIs(t, err, syncapply.ErrNotFound)

		// Delete archives the part and removes it from the shopping list.
		h.pg.Exec(t, fmt.Sprintf(`INSERT INTO garage.shopping_item (team_id, part_id, qty) VALUES ('%s', '%s', 1)`, team, id))
		cur := int32(0)
		require.NoError(t, h.syncTx(t, owner, team, func(tx db.DBTX) (err error) { cur, err = a.CurrentVersion(h.ctx, tx, tid(team), id); return err }))
		_, err = apply(a, syncOp(owner, team, syncapply.Delete, id, cur, `{}`))
		require.NoError(t, err)
		require.Equal(t, "0", h.pg.Exec(t, fmt.Sprintf(`SELECT count(*) FROM garage.shopping_item WHERE part_id = '%s'`, id)))
		_, found := read(a, team, owner, "mechanic", id)
		require.False(t, found, "a deleted part reads as a tombstone")
	})
}
