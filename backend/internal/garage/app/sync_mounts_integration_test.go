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

// TestMountAndInstallAppliers covers engine mounts and part installs: the device id and times
// are kept inside a sane window, the rules of the normal use cases hold, a repeated close is
// harmless, an overlap is refused, and another team's assets cannot be reached.
func TestMountAndInstallAppliers(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	team := h.team(t, owner, "owner")
	other := h.user(t, "other")
	otherTeam := h.team(t, other, "owner")

	mounts := garage.NewEngineMountApplier()
	installs := garage.NewPartInstallApplier()

	apply := func(a syncapply.Applier, o syncapply.Op) (int32, error) {
		var v int32
		err := h.syncTx(t, owner, team, func(tx db.DBTX) (err error) { v, err = a.Apply(h.ctx, tx, o); return err })
		return v, err
	}
	read := func(a syncapply.Applier, id uuid.UUID) (map[string]any, bool) {
		var raw json.RawMessage
		var found bool
		require.NoError(t, h.syncTx(t, owner, team, func(tx db.DBTX) (err error) {
			raw, found, err = a.Read(h.ctx, tx, tid(team), "pilot", id)
			return err
		}))
		if !found {
			return nil, false
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal(raw, &m))
		return m, true
	}
	newCar := func(team uuid.UUID) uuid.UUID {
		id := uuid.Must(uuid.NewV7())
		h.pg.Exec(t, fmt.Sprintf(`INSERT INTO garage.car (id, team_id, make, model) VALUES ('%s', '%s', 'M', 'X')`, id, team))
		return id
	}
	newEngine := func(team uuid.UUID) uuid.UUID {
		id := uuid.Must(uuid.NewV7())
		h.pg.Exec(t, fmt.Sprintf(`INSERT INTO garage.engine (id, team_id, make, model) VALUES ('%s', '%s', 'E', 'Y')`, id, team))
		return id
	}
	newPart := func(service bool) uuid.UUID {
		id := uuid.Must(uuid.NewV7())
		h.pg.Exec(t, fmt.Sprintf(`INSERT INTO garage.part (id, team_id, kind, description, has_service_life) VALUES ('%s', '%s', 'glow_plug', 'Plug', %t)`, id, team, service))
		return id
	}
	ago := func(d time.Duration) string { return time.Now().Add(-d).UTC().Format(time.RFC3339Nano) }
	create := func(a syncapply.Applier, fields string) (uuid.UUID, error) {
		id := uuid.Must(uuid.NewV7())
		_, err := apply(a, syncOp(owner, team, syncapply.Create, id, 0, fields))
		return id, err
	}

	t.Run("mount, move on, close twice", func(t *testing.T) {
		engine, car1, car2 := newEngine(team), newCar(team), newCar(team)
		first, err := create(mounts, fmt.Sprintf(`{"engine_id":"%s","car_id":"%s","mounted_at":"%s"}`, engine, car1, ago(48*time.Hour)))
		require.NoError(t, err)
		m, found := read(mounts, first)
		require.True(t, found)
		require.Nil(t, m["unmounted_at"])

		// A second open mount of the same engine, or of the same car, is refused.
		_, err = create(mounts, fmt.Sprintf(`{"engine_id":"%s","car_id":"%s"}`, engine, car2))
		require.ErrorIs(t, err, syncapply.ErrInvalidRef)
		_, err = create(mounts, fmt.Sprintf(`{"engine_id":"%s","car_id":"%s"}`, newEngine(team), car1))
		require.ErrorIs(t, err, syncapply.ErrInvalidRef)

		v, err := apply(mounts, syncOp(owner, team, syncapply.Patch, first, 1, fmt.Sprintf(`{"unmounted_at":"%s"}`, ago(24*time.Hour))))
		require.NoError(t, err)
		require.Equal(t, int32(1), v)
		_, err = apply(mounts, syncOp(owner, team, syncapply.Patch, first, 1, fmt.Sprintf(`{"unmounted_at":"%s"}`, ago(time.Hour))))
		require.NoError(t, err, "closing a closed mount is harmless")
		require.Equal(t, "t", h.pg.Exec(t, fmt.Sprintf(`SELECT unmounted_at < now() - interval '23 hours' FROM garage.engine_mount WHERE id = '%s'`, first)), "the first close stands")

		_, err = create(mounts, fmt.Sprintf(`{"engine_id":"%s","car_id":"%s"}`, engine, car2))
		require.NoError(t, err)
	})

	t.Run("an overlap with history is refused", func(t *testing.T) {
		engine, car := newEngine(team), newCar(team)
		first, err := create(mounts, fmt.Sprintf(`{"engine_id":"%s","car_id":"%s","mounted_at":"%s"}`, engine, car, ago(10*time.Hour)))
		require.NoError(t, err)
		_, err = apply(mounts, syncOp(owner, team, syncapply.Patch, first, 1, fmt.Sprintf(`{"unmounted_at":"%s"}`, ago(5*time.Hour))))
		require.NoError(t, err)
		_, err = create(mounts, fmt.Sprintf(`{"engine_id":"%s","car_id":"%s","mounted_at":"%s"}`, engine, newCar(team), ago(8*time.Hour)))
		require.ErrorIs(t, err, syncapply.ErrInvalidRef)
	})

	t.Run("mount rules", func(t *testing.T) {
		engine, car := newEngine(team), newCar(team)
		for name, fields := range map[string]string{
			"unknown field": fmt.Sprintf(`{"engine_id":"%s","car_id":"%s","team_id":"x"}`, engine, car),
			"not a uuid":    fmt.Sprintf(`{"engine_id":"nope","car_id":"%s"}`, car),
			"bad time":      fmt.Sprintf(`{"engine_id":"%s","car_id":"%s","mounted_at":"soon"}`, engine, car),
			"missing car":   fmt.Sprintf(`{"engine_id":"%s"}`, engine),
		} {
			_, err := create(mounts, fields)
			require.ErrorIs(t, err, syncapply.ErrInvalidFields, name)
		}
		_, err := create(mounts, fmt.Sprintf(`{"engine_id":"%s","car_id":"%s"}`, uuid.New(), car))
		require.ErrorIs(t, err, syncapply.ErrInvalidRef)
		h.pg.Exec(t, fmt.Sprintf(`UPDATE garage.engine SET status = 'retired' WHERE id = '%s'`, engine))
		_, err = create(mounts, fmt.Sprintf(`{"engine_id":"%s","car_id":"%s"}`, engine, car))
		require.ErrorIs(t, err, syncapply.ErrInvalidRef)

		// A future time is replaced by the server's clock.
		fresh, err := create(mounts, fmt.Sprintf(`{"engine_id":"%s","car_id":"%s","mounted_at":"%s"}`, newEngine(team), newCar(team), time.Now().Add(72*time.Hour).UTC().Format(time.RFC3339)))
		require.NoError(t, err)
		require.Equal(t, "t", h.pg.Exec(t, fmt.Sprintf(`SELECT mounted_at < now() + interval '1 minute' FROM garage.engine_mount WHERE id = '%s'`, fresh)))
		// Deleting is not offered.
		_, err = apply(mounts, syncOp(owner, team, syncapply.Delete, fresh, 1, `{}`))
		require.ErrorIs(t, err, syncapply.ErrInvalidFields)
		// A close that ends before the mount started is replaced by now.
		_, err = apply(mounts, syncOp(owner, team, syncapply.Patch, fresh, 1, fmt.Sprintf(`{"unmounted_at":"%s"}`, ago(240*time.Hour))))
		require.NoError(t, err)
		require.Equal(t, "t", h.pg.Exec(t, fmt.Sprintf(`SELECT unmounted_at > mounted_at FROM garage.engine_mount WHERE id = '%s'`, fresh)))
	})

	t.Run("another team's engine, car or mount is out of reach", func(t *testing.T) {
		foreignEngine, foreignCar := newEngine(otherTeam), newCar(otherTeam)
		_, err := create(mounts, fmt.Sprintf(`{"engine_id":"%s","car_id":"%s"}`, foreignEngine, foreignCar))
		require.ErrorIs(t, err, syncapply.ErrInvalidRef)
		mine, err := create(mounts, fmt.Sprintf(`{"engine_id":"%s","car_id":"%s"}`, newEngine(team), newCar(team)))
		require.NoError(t, err)
		err = h.syncTx(t, other, otherTeam, func(tx db.DBTX) error {
			_, err := mounts.Apply(h.ctx, tx, syncOp(other, otherTeam, syncapply.Patch, mine, 1, fmt.Sprintf(`{"unmounted_at":"%s"}`, ago(time.Minute))))
			return err
		})
		require.ErrorIs(t, err, syncapply.ErrNotFound)
	})

	t.Run("install and remove a part", func(t *testing.T) {
		part, car := newPart(true), newCar(team)
		id, err := create(installs, fmt.Sprintf(`{"part_id":"%s","car_id":"%s","installed_at":"%s"}`, part, car, ago(30*time.Hour)))
		require.NoError(t, err)
		require.Equal(t, "0", h.pg.Exec(t, fmt.Sprintf(`SELECT count(*) FROM garage.stock_movement WHERE part_id = '%s'`, part)), "the unit leaves the stock with its own movement")
		i, found := read(installs, id)
		require.True(t, found)
		require.Nil(t, i["removed_at"])
		require.EqualValues(t, 1, i["version"])

		_, err = apply(installs, syncOp(owner, team, syncapply.Patch, id, 1, `{"removal_destination":"scrap"}`))
		require.ErrorIs(t, err, syncapply.ErrInvalidFields, "scrap needs a reason")
		_, err = apply(installs, syncOp(owner, team, syncapply.Patch, id, 1, `{"removal_destination":"stock","removal_reason":"worn"}`))
		require.ErrorIs(t, err, syncapply.ErrInvalidFields, "stock takes no reason")
		_, err = apply(installs, syncOp(owner, team, syncapply.Patch, id, 1, fmt.Sprintf(`{"removal_destination":"scrap","removal_reason":"worn","removed_at":"%s"}`, ago(2*time.Hour))))
		require.NoError(t, err)
		i, _ = read(installs, id)
		require.Equal(t, "scrap", i["removal_destination"])
		require.Equal(t, "worn", i["removal_reason"])
		require.NotNil(t, i["removed_at"])

		// Removing again from another device (a stale base) is harmless and changes nothing.
		_, err = apply(installs, syncOp(owner, team, syncapply.Patch, id, 1, `{"removal_destination":"spare"}`))
		require.NoError(t, err)
		i, _ = read(installs, id)
		require.Equal(t, "scrap", i["removal_destination"])
	})

	t.Run("install rules", func(t *testing.T) {
		plain, withLife, car, engine := newPart(false), newPart(true), newCar(team), newEngine(team)
		for name, fields := range map[string]string{
			"no service life": fmt.Sprintf(`{"part_id":"%s","car_id":"%s"}`, plain, car),
			"both targets":    fmt.Sprintf(`{"part_id":"%s","car_id":"%s","engine_id":"%s"}`, withLife, car, engine),
			"no target":       fmt.Sprintf(`{"part_id":"%s"}`, withLife),
			"unknown part":    fmt.Sprintf(`{"part_id":"%s","car_id":"%s"}`, uuid.New(), car),
			"unknown car":     fmt.Sprintf(`{"part_id":"%s","car_id":"%s"}`, withLife, uuid.New()),
			"foreign car":     fmt.Sprintf(`{"part_id":"%s","car_id":"%s"}`, withLife, newCar(otherTeam)),
		} {
			_, err := create(installs, fields)
			require.Error(t, err, name)
			require.NotContains(t, err.Error(), "SQLSTATE", name)
		}
		h.pg.Exec(t, fmt.Sprintf(`UPDATE garage.car SET status = 'retired' WHERE id = '%s'`, car))
		_, err := create(installs, fmt.Sprintf(`{"part_id":"%s","car_id":"%s"}`, withLife, car))
		require.ErrorIs(t, err, syncapply.ErrInvalidRef, "a retired car")
		_, err = create(installs, fmt.Sprintf(`{"part_id":"%s","engine_id":"%s","bogus":1}`, withLife, engine))
		require.ErrorIs(t, err, syncapply.ErrInvalidFields)
		id, err := create(installs, fmt.Sprintf(`{"part_id":"%s","engine_id":"%s"}`, withLife, engine))
		require.NoError(t, err)
		_, err = apply(installs, syncOp(owner, team, syncapply.Create, id, 0, fmt.Sprintf(`{"part_id":"%s","engine_id":"%s"}`, withLife, engine)))
		require.ErrorIs(t, err, syncapply.ErrInvalidFields, "the same id twice is refused")
	})
}
