package app_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/syncapply"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/app"
)

// TestFeedbackApplier covers the driver's feedback on a setup version (ADR 0026): append
// only, the device id is kept, the author is always the caller, the rules of the scales hold,
// the version must exist in this team, and every member that can pull reads it.
func TestFeedbackApplier(t *testing.T) {
	h := newHarnessWithGarage(t, garage.NewDirectory())
	owner := h.user(t, "owner")
	team := h.team(t, owner, "owner")
	car := h.car(t, team)
	pilot := h.user(t, "pilot")
	h.member(t, team, pilot, "pilot")
	feedback := app.NewFeedbackApplier(h.svc)

	setup := uuid.Must(uuid.NewV7())
	_, err := func() (int32, error) {
		var v int32
		err := h.inTx(t, owner, team, func(tx db.DBTX) (err error) {
			v, err = app.NewSetupApplier(h.svc).Apply(h.ctx, tx, op(owner, team, syncapply.Create, setup, 0, fmt.Sprintf(`{"car_id":%q,"name":"A"}`, car)))
			return err
		})
		return v, err
	}()
	require.NoError(t, err)

	as := func(user uuid.UUID, o syncapply.Op) error {
		return h.inTx(t, user, team, func(tx db.DBTX) error { _, err := feedback.Apply(h.ctx, tx, o); return err })
	}
	mk := func(user uuid.UUID, fields string) syncapply.Op {
		o := op(user, team, syncapply.Append, uuid.Must(uuid.NewV7()), 0, fields)
		o.Role = "pilot"
		return o
	}

	t.Run("who may write", func(t *testing.T) {
		require.True(t, feedback.CanWrite("pilot"))
		require.True(t, feedback.CanWrite("mechanic"))
		require.True(t, feedback.CanWrite("owner"))
		require.False(t, feedback.CanWrite("guest"))
	})

	t.Run("a pilot adds feedback; the author is the caller and everyone reads it", func(t *testing.T) {
		o := mk(pilot, fmt.Sprintf(`{"setup_id":%q,"version":1,"traction":4,"braking":2,"track_zone":"  curva 3 ","note":" ressalta "}`, setup))
		require.NoError(t, as(pilot, o))
		require.Equal(t, "4|2|curva 3|ressalta|"+pilot.String(), h.pg.Exec(t, fmt.Sprintf(
			`SELECT traction||'|'||braking||'|'||track_zone||'|'||note||'|'||author_id FROM setup.setup_feedback WHERE id = '%s'`, o.EntityID)))
		var raw json.RawMessage
		var found bool
		require.NoError(t, h.inTx(t, owner, team, func(tx db.DBTX) (err error) {
			raw, found, err = feedback.Read(h.ctx, tx, tid(team), "mechanic", o.EntityID)
			return err
		}))
		require.True(t, found)
		var m map[string]any
		require.NoError(t, json.Unmarshal(raw, &m))
		require.EqualValues(t, 4, m["traction"])
		require.Equal(t, pilot.String(), m["author_id"])
		require.Nil(t, m["steering_mid"])
	})

	t.Run("the author cannot be chosen", func(t *testing.T) {
		require.ErrorIs(t, as(pilot, mk(pilot, fmt.Sprintf(`{"setup_id":%q,"version":1,"note":"x","author_id":%q}`, setup, owner))), syncapply.ErrInvalidFields)
	})

	t.Run("rules", func(t *testing.T) {
		for name, fields := range map[string]string{
			"empty":            fmt.Sprintf(`{"setup_id":%q,"version":1}`, setup),
			"blank note only":  fmt.Sprintf(`{"setup_id":%q,"version":1,"note":"   "}`, setup),
			"scale 0":          fmt.Sprintf(`{"setup_id":%q,"version":1,"traction":0}`, setup),
			"scale 6":          fmt.Sprintf(`{"setup_id":%q,"version":1,"traction":6}`, setup),
			"scale text":       fmt.Sprintf(`{"setup_id":%q,"version":1,"traction":"3"}`, setup),
			"scale float":      fmt.Sprintf(`{"setup_id":%q,"version":1,"traction":3.5}`, setup),
			"zone too long":    fmt.Sprintf(`{"setup_id":%q,"version":1,"traction":3,"track_zone":%q}`, setup, strings.Repeat("z", 81)),
			"note too long":    fmt.Sprintf(`{"setup_id":%q,"version":1,"note":%q}`, setup, strings.Repeat("n", 2001)),
			"no setup":         `{"version":1,"traction":3}`,
			"setup not a uuid": `{"setup_id":"nope","version":1,"traction":3}`,
			"no version":       fmt.Sprintf(`{"setup_id":%q,"traction":3}`, setup),
			"unknown field":    fmt.Sprintf(`{"setup_id":%q,"version":1,"traction":3,"team_id":"x"}`, setup),
			"bad time":         fmt.Sprintf(`{"setup_id":%q,"version":1,"traction":3,"created_at":"x"}`, setup),
		} {
			require.ErrorIs(t, as(pilot, mk(pilot, fields)), syncapply.ErrInvalidFields, name)
		}
		o := mk(pilot, fmt.Sprintf(`{"setup_id":%q,"version":1,"traction":3}`, setup))
		o.Kind = syncapply.Patch
		require.ErrorIs(t, as(pilot, o), syncapply.ErrInvalidFields, "append only")
		o.Kind = syncapply.Delete
		require.ErrorIs(t, as(pilot, o), syncapply.ErrInvalidFields, "append only")
	})

	t.Run("the setup version must exist in this team", func(t *testing.T) {
		require.ErrorIs(t, as(pilot, mk(pilot, fmt.Sprintf(`{"setup_id":%q,"version":9,"traction":3}`, setup))), syncapply.ErrInvalidRef)
		require.ErrorIs(t, as(pilot, mk(pilot, fmt.Sprintf(`{"setup_id":%q,"version":1,"traction":3}`, uuid.New()))), syncapply.ErrInvalidRef)
		otherOwner := h.user(t, "other")
		otherTeam := h.team(t, otherOwner, "owner")
		// The same setup id in another team is not reachable: RLS shows that team nothing.
		err := h.inTx(t, otherOwner, otherTeam, func(tx db.DBTX) error {
			_, err := feedback.Apply(h.ctx, tx, op(otherOwner, otherTeam, syncapply.Append, uuid.Must(uuid.NewV7()), 0,
				fmt.Sprintf(`{"setup_id":%q,"version":1,"traction":3}`, setup)))
			return err
		})
		require.ErrorIs(t, err, syncapply.ErrInvalidRef)
	})

	t.Run("the same id twice is refused, and feedback is immutable", func(t *testing.T) {
		o := mk(pilot, fmt.Sprintf(`{"setup_id":%q,"version":1,"traction":3}`, setup))
		require.NoError(t, as(pilot, o))
		require.ErrorIs(t, as(pilot, o), syncapply.ErrInvalidFields)
		require.Equal(t, "1", h.pg.Exec(t, fmt.Sprintf(`SELECT count(*) FROM setup.setup_feedback WHERE id = '%s'`, o.EntityID)))
		err := h.inTx(t, owner, team, func(tx db.DBTX) error {
			_, err := tx.Exec(h.ctx, `UPDATE setup.setup_feedback SET traction = 5 WHERE id = $1`, o.EntityID)
			return err
		})
		require.Error(t, err, "the table is append only")
	})

	t.Run("a time from the future is replaced", func(t *testing.T) {
		o := mk(pilot, fmt.Sprintf(`{"setup_id":%q,"version":1,"traction":3,"created_at":%q}`, setup, time.Now().Add(72*time.Hour).UTC().Format(time.RFC3339)))
		require.NoError(t, as(pilot, o))
		require.Equal(t, "t", h.pg.Exec(t, fmt.Sprintf(`SELECT created_at < now() + interval '1 minute' FROM setup.setup_feedback WHERE id = '%s'`, o.EntityID)))
	})
}
