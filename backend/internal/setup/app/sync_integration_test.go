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
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/app"
)

func op(user, team uuid.UUID, kind syncapply.Kind, id uuid.UUID, expected int32, fields string) syncapply.Op {
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(fields), &m); err != nil {
		panic(err)
	}
	return syncapply.Op{
		OpID: uuid.New(), TeamID: tid(team), UserID: uid(user), Role: "mechanic",
		Kind: kind, EntityID: id, Fields: m, ExpectedVersion: expected,
	}
}

// inTx runs fn in a team transaction, as the sync module does for each operation.
func (h *harness) inTx(t *testing.T, user, team uuid.UUID, fn func(tx db.DBTX) error) error {
	t.Helper()
	return db.New(h.pool).WithTeamTx(h.ctx, uid(user), tid(team), func(_ context.Context, tx db.DBTX, _ db.Membership) error {
		return fn(tx)
	})
}

func TestSetupAppliers(t *testing.T) {
	h := newHarnessWithGarage(t, garage.NewDirectory())
	owner := h.user(t, "owner")
	team := h.team(t, owner, "owner")
	car := h.car(t, team)
	setupApplier, versionApplier := app.NewSetupApplier(h.svc), app.NewVersionApplier(h.svc)
	id := uuid.Must(uuid.NewV7())

	apply := func(a syncapply.Applier, o syncapply.Op) (int32, error) {
		var v int32
		err := h.inTx(t, owner, team, func(tx db.DBTX) (err error) { v, err = a.Apply(h.ctx, tx, o); return err })
		return v, err
	}
	current := func(a syncapply.Applier, entity uuid.UUID) (int32, error) {
		var v int32
		err := h.inTx(t, owner, team, func(tx db.DBTX) (err error) { v, err = a.CurrentVersion(h.ctx, tx, tid(team), entity); return err })
		return v, err
	}

	t.Run("create keeps the device id and writes version 1", func(t *testing.T) {
		_, err := apply(setupApplier, op(owner, team, syncapply.Create, id, 0, fmt.Sprintf(`{"car_id":%q,"name":"  Dia seco ","notes":"n"}`, car)))
		require.NoError(t, err)
		require.Equal(t, "Dia seco|1|"+owner.String(), h.pg.Exec(t, fmt.Sprintf(
			`SELECT s.name||'|'||s.current_version||'|'||s.created_by FROM setup.setup s WHERE s.id = '%s'`, id)))
		require.Equal(t, "1", h.pg.Exec(t, fmt.Sprintf(`SELECT count(*) FROM setup.setup_version WHERE setup_id = '%s' AND version = 1`, id)))
	})

	t.Run("setup rejects what it does not support or cannot trust", func(t *testing.T) {
		for name, o := range map[string]syncapply.Op{
			"patch":            op(owner, team, syncapply.Patch, id, 1, `{"name":"x"}`),
			"delete":           op(owner, team, syncapply.Delete, id, 1, `{}`),
			"unknown field":    op(owner, team, syncapply.Create, uuid.New(), 0, fmt.Sprintf(`{"car_id":%q,"name":"a","current_version":9}`, car)),
			"no name":          op(owner, team, syncapply.Create, uuid.New(), 0, fmt.Sprintf(`{"car_id":%q}`, car)),
			"name too long":    op(owner, team, syncapply.Create, uuid.New(), 0, fmt.Sprintf(`{"car_id":%q,"name":%q}`, car, strings.Repeat("x", 500))),
			"bad car id":       op(owner, team, syncapply.Create, uuid.New(), 0, `{"car_id":"nope","name":"a"}`),
			"version no setup": op(owner, team, syncapply.Create, uuid.New(), 0, fmt.Sprintf(`{"car_id":%q,"name":"a","source_version":1}`, car)),
		} {
			_, err := apply(setupApplier, o)
			require.ErrorIs(t, err, syncapply.ErrInvalidFields, name)
		}
		_, err := apply(setupApplier, op(owner, team, syncapply.Create, uuid.New(), 0, fmt.Sprintf(`{"car_id":%q,"name":"a"}`, uuid.New())))
		require.ErrorIs(t, err, syncapply.ErrInvalidRef, "a car that does not exist")

		otherOwner := h.user(t, "other")
		otherTeam := h.team(t, otherOwner, "owner")
		_, err = apply(setupApplier, op(owner, team, syncapply.Create, uuid.New(), 0, fmt.Sprintf(`{"car_id":%q,"name":"a"}`, h.car(t, otherTeam))))
		require.ErrorIs(t, err, syncapply.ErrInvalidRef, "another team's car")
		_, err = apply(setupApplier, op(owner, team, syncapply.Create, id, 0, fmt.Sprintf(`{"car_id":%q,"name":"again"}`, car)))
		require.Error(t, err, "an existing id is not overwritten")
	})

	t.Run("a new version is based on the current one", func(t *testing.T) {
		v, err := apply(versionApplier, op(owner, team, syncapply.Patch, id, 1,
			`{"data":{"diffs":{"front_cst":500}},"change_summary":"óleo"}`))
		require.NoError(t, err)
		require.EqualValues(t, 2, v)
		cur, err := current(versionApplier, id)
		require.NoError(t, err)
		require.EqualValues(t, 2, cur)

		// A device still on version 1 is stale: the sync module turns this into a conflict.
		_, err = apply(versionApplier, op(owner, team, syncapply.Patch, id, 1, `{"data":{"diffs":{"front_cst":900}}}`))
		require.ErrorIs(t, err, syncapply.ErrStale)
		require.Equal(t, "2", h.pg.Exec(t, fmt.Sprintf(`SELECT current_version FROM setup.setup WHERE id = '%s'`, id)))
	})

	t.Run("versions are validated and immutable", func(t *testing.T) {
		for name, fields := range map[string]string{
			"unknown field":   `{"data":{},"version":7}`,
			"no data":         `{"change_summary":"x"}`,
			"null data":       `{"data":null}`,
			"data not object": `{"data":[1]}`,
			"schema":          `{"data":{"diffs":{"front_cst":"thick"}}}`,
			"unknown group":   `{"data":{"nitro_boost":{"x":1}}}`,
		} {
			_, err := apply(versionApplier, op(owner, team, syncapply.Patch, id, 2, fields))
			require.ErrorIs(t, err, syncapply.ErrInvalidFields, name)
		}
		for _, kind := range []syncapply.Kind{syncapply.Create, syncapply.Delete, syncapply.Append} {
			_, err := apply(versionApplier, op(owner, team, kind, id, 2, `{"data":{}}`))
			require.ErrorIs(t, err, syncapply.ErrInvalidFields)
		}
		_, err := apply(versionApplier, op(owner, team, syncapply.Patch, uuid.New(), 1, `{"data":{}}`))
		require.ErrorIs(t, err, syncapply.ErrNotFound)
		require.Equal(t, "2", h.pg.Exec(t, fmt.Sprintf(`SELECT count(*) FROM setup.setup_version WHERE setup_id = '%s'`, id)))
	})

	t.Run("a reference to another team's engine is refused", func(t *testing.T) {
		otherOwner := h.user(t, "other2")
		otherTeam := h.team(t, otherOwner, "owner")
		foreign := h.engine(t, otherTeam)
		// fakeGarage always says yes; the real directory is team-scoped.
		_, err := apply(versionApplier, op(owner, team, syncapply.Patch, id, 2,
			fmt.Sprintf(`{"data":{"engine":{"engine_id":%q}}}`, foreign)))
		require.ErrorIs(t, err, syncapply.ErrInvalidRef)
	})

	t.Run("reads return the setup and its versions, and nothing for another team", func(t *testing.T) {
		var setupRaw, versionsRaw json.RawMessage
		var found1, found2 bool
		require.NoError(t, h.inTx(t, owner, team, func(tx db.DBTX) (err error) {
			setupRaw, found1, err = setupApplier.Read(h.ctx, tx, tid(team), "pilot", id)
			if err != nil {
				return err
			}
			versionsRaw, found2, err = versionApplier.Read(h.ctx, tx, tid(team), "pilot", id)
			return err
		}))
		require.True(t, found1)
		require.True(t, found2)
		var s map[string]any
		require.NoError(t, json.Unmarshal(setupRaw, &s))
		require.Equal(t, id.String(), s["id"])
		require.EqualValues(t, 2, s["current_version"])
		var v struct {
			SetupID  string `json:"setup_id"`
			Versions []struct {
				Version int32          `json:"version"`
				Data    map[string]any `json:"data"`
			} `json:"versions"`
		}
		require.NoError(t, json.Unmarshal(versionsRaw, &v))
		require.Len(t, v.Versions, 2)
		require.EqualValues(t, 2, v.Versions[0].Version, "newest first")

		otherOwner := h.user(t, "other3")
		otherTeam := h.team(t, otherOwner, "owner")
		require.NoError(t, h.inTx(t, otherOwner, otherTeam, func(tx db.DBTX) error {
			_, ok, err := setupApplier.Read(h.ctx, tx, tid(otherTeam), "owner", id)
			require.NoError(t, err)
			require.False(t, ok)
			_, ok, err = versionApplier.Read(h.ctx, tx, tid(otherTeam), "owner", id)
			require.NoError(t, err)
			require.False(t, ok)
			return nil
		}))
	})
}
