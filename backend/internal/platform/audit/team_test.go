package audit_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/audit"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/testutil"
)

const (
	userA = "01900000-0000-7000-8000-00000000a0a0"
	userB = "01900000-0000-7000-8000-00000000b0b0"
	teamA = "01900000-0000-7000-8000-0000000000a1"
	teamB = "01900000-0000-7000-8000-0000000000b1"
	reqID = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60"
)

// The team audit entry takes the team and the author from the transaction, so a use case cannot
// write it in another team or in someone else's name; and it commits and rolls back with the change.
func TestRecordTeamAgainstPostgres(t *testing.T) {
	t.Parallel()
	pg := testutil.StartPostgres(t)
	pg.Exec(t, `
INSERT INTO identity.user_account (id, idp_subject, email, display_name) VALUES
  ('`+userA+`', 'sub-a', 'a@x.pt', 'A'), ('`+userB+`', 'sub-b', 'b@x.pt', 'B');
INSERT INTO identity.team (id, name, created_by) VALUES ('`+teamA+`', 'A', '`+userA+`'), ('`+teamB+`', 'B', '`+userB+`');
INSERT INTO identity.membership (team_id, user_id, role) VALUES ('`+teamA+`', '`+userA+`', 'owner'), ('`+teamB+`', '`+userB+`', 'owner');`)
	pool, err := db.Open(context.Background(), pg.URL)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(pool.Close)
	d := db.New(pool)
	ctx := context.Background()
	uA, uB, tA, tB := mustUser(t, userA), mustUser(t, userB), mustTeam(t, teamA), mustTeam(t, teamB)
	rows := func(where string) string {
		return pg.Exec(t, `SELECT count(*) FROM audit.audit_log WHERE `+where)
	}

	t.Run("the team and the author come from the transaction", func(t *testing.T) {
		err := d.WithTeamTx(ctx, uA, tA, func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
			return audit.RecordTeam(ctx, tx, audit.TeamEvent{
				Action: "setup.update", ObjectType: "setup", ObjectID: "s-1",
				Before: map[string]int{"camber": -1}, After: map[string]int{"camber": -2}, RequestID: reqID,
			})
		})
		if err != nil {
			t.Fatalf("RecordTeam: %v", err)
		}
		got := pg.Exec(t, `SELECT concat_ws('|', team_id, actor_kind, actor_id, action, object_type, object_id, before, after, request_id) FROM audit.audit_log WHERE action = 'setup.update'`)
		want := teamA + `|user|` + userA + `|setup.update|setup|s-1|{"camber": -1}|{"camber": -2}|` + reqID
		if got != want {
			t.Errorf("row = %s\nwant  %s", got, want)
		}
	})

	t.Run("it is the team's own: another team's transaction does not see it", func(t *testing.T) {
		err := d.WithTeamReadTx(ctx, uB, tB, func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM audit.audit_log`).Scan(&n); err != nil || n != 0 {
				t.Errorf("team B reads %d audit entries of team A (err %v)", n, err)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WithTeamReadTx: %v", err)
		}
	})

	t.Run("rolled back together with the change it describes", func(t *testing.T) {
		boom := errors.New("boom")
		err := d.WithTeamTx(ctx, uA, tA, func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
			if err := audit.RecordTeam(ctx, tx, audit.TeamEvent{Action: "car.create", ObjectType: "car"}); err != nil {
				return err
			}
			return boom
		})
		if !errors.Is(err, boom) || rows(`action = 'car.create'`) != "0" {
			t.Errorf("err %v, rows %s: the entry must not survive a rollback", err, rows(`action = 'car.create'`))
		}
	})

	t.Run("without the context nothing is written", func(t *testing.T) {
		err := audit.RecordTeam(ctx, pool, audit.TeamEvent{Action: "car.create", ObjectType: "car"})
		if !errors.Is(err, audit.ErrNoContext) {
			t.Fatalf("err = %v, want ErrNoContext", err)
		}
		err = d.WithUserTx(ctx, uA, func(ctx context.Context, tx db.DBTX) error {
			return audit.RecordTeam(ctx, tx, audit.TeamEvent{Action: "car.create", ObjectType: "car"})
		})
		if !errors.Is(err, audit.ErrNoContext) {
			t.Fatalf("a user context has no team: err = %v, want ErrNoContext", err)
		}
		if rows(`action = 'car.create'`) != "0" {
			t.Error("an entry was written without a team")
		}
	})

	t.Run("invalid events are refused before the database", func(t *testing.T) {
		big := strings.Repeat("x", 70<<10)
		for name, e := range map[string]audit.TeamEvent{
			"empty action":       {ObjectType: "car"},
			"upper case action":  {Action: "Car.Create", ObjectType: "car"},
			"no dot in action":   {Action: "create", ObjectType: "car"},
			"sql in the action":  {Action: "x'); DROP TABLE audit.audit_log; --", ObjectType: "car"},
			"bad object type":    {Action: "car.create", ObjectType: "Car; --"},
			"long object id":     {Action: "car.create", ObjectType: "car", ObjectID: strings.Repeat("x", 201)},
			"bad request id":     {Action: "car.create", ObjectType: "car", RequestID: "not-a-uuid"},
			"oversized before":   {Action: "car.create", ObjectType: "car", Before: big},
			"unmarshalable data": {Action: "car.create", ObjectType: "car", After: make(chan int)},
		} {
			err := d.WithTeamTx(ctx, uA, tA, func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
				return audit.RecordTeam(ctx, tx, e)
			})
			if !errors.Is(err, audit.ErrInvalidEvent) {
				t.Errorf("%s: err = %v, want ErrInvalidEvent", name, err)
			}
		}
		if rows(`object_type = 'car'`) != "0" {
			t.Error("an invalid event was written")
		}
	})

	t.Run("hostile values are stored as data", func(t *testing.T) {
		evil := "x'); DELETE FROM audit.audit_log; --"
		err := d.WithTeamTx(ctx, uA, tA, func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
			return audit.RecordTeam(ctx, tx, audit.TeamEvent{Action: "car.rename", ObjectType: "car", ObjectID: evil, After: map[string]string{"name": evil}})
		})
		if err != nil {
			t.Fatalf("RecordTeam: %v", err)
		}
		if rows(`action = 'setup.update'`) != "1" {
			t.Error("the audit log was emptied")
		}
	})

	t.Run("append-only for the application", func(t *testing.T) {
		err := d.WithTeamTx(ctx, uA, tA, func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
			_, err := tx.Exec(ctx, `UPDATE audit.audit_log SET action = 'x.y'`)
			return err
		})
		if err == nil {
			t.Error("the application updated the audit log")
		}
		err = d.WithTeamTx(ctx, uA, tA, func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
			_, err := tx.Exec(ctx, `DELETE FROM audit.audit_log`)
			return err
		})
		if err == nil {
			t.Error("the application deleted from the audit log")
		}
	})
}

func mustUser(t *testing.T, s string) db.UserID {
	t.Helper()
	u, err := db.ParseUserID(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func mustTeam(t *testing.T, s string) db.TeamID {
	t.Helper()
	v, err := db.ParseTeamID(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
