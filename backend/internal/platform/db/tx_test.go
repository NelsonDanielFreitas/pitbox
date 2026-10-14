package db_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/apperr"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/testutil"
)

// Fixed ids so that the failures name them.
const (
	idUserA     = "01900000-0000-7000-8000-00000000a0a0" // owner of team A
	idUserB     = "01900000-0000-7000-8000-00000000b0b0" // mechanic of team B
	idUserC     = "01900000-0000-7000-8000-00000000c0c0" // no team
	idUserD     = "01900000-0000-7000-8000-00000000d0d0" // member of team A, account deleted
	idUserE     = "01900000-0000-7000-8000-00000000e0e0" // member of a deleted team
	idTeamA     = "01900000-0000-7000-8000-0000000000a1"
	idTeamB     = "01900000-0000-7000-8000-0000000000b1"
	idTeamGone  = "01900000-0000-7000-8000-0000000000e1" // deleted
	idTeamNone  = "01900000-0000-7000-8000-0000000000f1" // never existed
	idCarA      = "01900000-0000-7000-8000-0000000000ca"
	idCarB      = "01900000-0000-7000-8000-0000000000cb"
	sqlStateRLS = "42501"
	sqlReadOnly = "25006"
)

const fixtures = `
INSERT INTO identity.user_account (id, idp_subject, email, display_name, deleted_at) VALUES
  ('` + idUserA + `', 'sub-a', 'a@x.pt', 'A', NULL),
  ('` + idUserB + `', 'sub-b', 'b@x.pt', 'B', NULL),
  ('` + idUserC + `', 'sub-c', 'c@x.pt', 'C', NULL),
  ('` + idUserD + `', 'sub-d', 'd@x.pt', 'D', now()),
  ('` + idUserE + `', 'sub-e', 'e@x.pt', 'E', NULL);
INSERT INTO identity.team (id, name, created_by, deleted_at) VALUES
  ('` + idTeamA + `', 'Equipa A', '` + idUserA + `', NULL),
  ('` + idTeamB + `', 'Equipa B', '` + idUserB + `', NULL),
  ('` + idTeamGone + `', 'Equipa apagada', '` + idUserE + `', now());
INSERT INTO identity.membership (team_id, user_id, role) VALUES
  ('` + idTeamA + `', '` + idUserA + `', 'owner'),
  ('` + idTeamA + `', '` + idUserD + `', 'mechanic'),
  ('` + idTeamB + `', '` + idUserB + `', 'mechanic'),
  ('` + idTeamGone + `', '` + idUserE + `', 'owner');
INSERT INTO garage.car (id, team_id, make, model, transponder_number) VALUES
  ('` + idCarA + `', '` + idTeamA + `', 'Mugen', 'MBX8R', '1111111'),
  ('` + idCarB + `', '` + idTeamB + `', 'Kyosho', 'MP10', '2222222');`

func user(t *testing.T, s string) db.UserID {
	t.Helper()
	u, err := db.ParseUserID(s)
	if err != nil {
		t.Fatalf("ParseUserID(%q): %v", s, err)
	}
	return u
}

func team(t *testing.T, s string) db.TeamID {
	t.Helper()
	v, err := db.ParseTeamID(s)
	if err != nil {
		t.Fatalf("ParseTeamID(%q): %v", s, err)
	}
	return v
}

func count(t *testing.T, tx db.DBTX, query string, args ...any) int {
	t.Helper()
	var n int
	if err := tx.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func sqlState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func setup(t *testing.T, opts ...db.Option) (testutil.Postgres, *pgxpool.Pool, *db.DB) {
	t.Helper()
	pg := testutil.StartPostgres(t)
	pg.Exec(t, fixtures)
	pool, err := db.Open(context.Background(), pg.URL, opts...)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(pool.Close)
	return pg, pool, db.New(pool)
}

// The "pronto quando" of F1-04: the user of team B does not read team A's data. One real
// PostgreSQL (the compose image, schema.sql, the application role) for all the subtests.
func TestTeamIsolation(t *testing.T) {
	t.Parallel()
	pg, pool, d := setup(t)
	ctx := context.Background()
	uA, uB, tA, tB := user(t, idUserA), user(t, idUserB), team(t, idTeamA), team(t, idTeamB)

	t.Run("a member sees the team's rows and learns the role", func(t *testing.T) {
		err := d.WithTeamReadTx(ctx, uA, tA, func(ctx context.Context, tx db.DBTX, m db.Membership) error {
			if m.Role != "owner" {
				t.Errorf("role = %q", m.Role)
			}
			var id string
			if err := tx.QueryRow(ctx, `SELECT id::text FROM garage.car`).Scan(&id); err != nil || id != idCarA {
				t.Errorf("cars of team A: %q, %v", id, err)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WithTeamReadTx: %v", err)
		}
	})

	t.Run("the user of team B does not read team A's data", func(t *testing.T) {
		err := d.WithTeamReadTx(ctx, uB, tB, func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
			if n := count(t, tx, `SELECT count(*) FROM garage.car`); n != 1 {
				t.Errorf("team B sees %d cars, want its own 1", n)
			}
			if n := count(t, tx, `SELECT count(*) FROM garage.car WHERE id = $1`, idCarA); n != 0 {
				t.Errorf("team B read team A's car by id")
			}
			if n := count(t, tx, `SELECT count(*) FROM garage.car WHERE team_id = $1`, idTeamA); n != 0 {
				t.Errorf("team B read team A's cars by team_id")
			}
			if n := count(t, tx, `SELECT count(*) FROM identity.team WHERE id = $1`, idTeamA); n != 0 {
				t.Errorf("team B sees team A")
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WithTeamReadTx: %v", err)
		}
	})

	t.Run("not a member: the same error whatever the reason, and fn never runs", func(t *testing.T) {
		cases := map[string]struct {
			user db.UserID
			team db.TeamID
		}{
			"a member of another team":          {uB, tA},
			"a team that never existed":         {uA, team(t, idTeamNone)},
			"a deleted team":                    {user(t, idUserE), team(t, idTeamGone)},
			"a deleted user of a team":          {user(t, idUserD), tA},
			"a user without any team":           {user(t, idUserC), tA},
			"a user that does not exist at all": {user(t, "01900000-0000-7000-8000-00000000ffff"), tA},
		}
		var first string
		for name, c := range cases {
			called := false
			err := d.WithTeamTx(ctx, c.user, c.team, func(context.Context, db.DBTX, db.Membership) error { called = true; return nil })
			if !errors.Is(err, db.ErrNotMember) || !errors.Is(err, apperr.ErrNotFound) || called {
				t.Errorf("%s: err = %v, fn called = %v", name, err, called)
			}
			if first == "" {
				first = err.Error()
			} else if err.Error() != first {
				t.Errorf("%s: the error text differs (%q vs %q): it would let a caller tell the cases apart", name, err, first)
			}
		}
	})

	t.Run("writes with another team's id are refused by the database", func(t *testing.T) {
		// A refused statement aborts the PostgreSQL transaction, so fn returns the error and the whole
		// transaction rolls back (returning nil would make the commit itself fail: also fail closed).
		err := d.WithTeamTx(ctx, uB, tB, func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
			_, err := tx.Exec(ctx, `INSERT INTO garage.car (team_id, make, model, transponder_number) VALUES ($1, 'X', 'Y', '9999999')`, idTeamA)
			return err
		})
		if sqlState(err) != sqlStateRLS {
			t.Fatalf("insert into team A: %v, want SQLSTATE %s", err, sqlStateRLS)
		}
		err = d.WithTeamTx(ctx, uB, tB, func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
			for _, q := range []string{`UPDATE garage.car SET make = 'hacked' WHERE id = $1`, `DELETE FROM garage.car WHERE id = $1`} {
				tag, err := tx.Exec(ctx, q, idCarA)
				if err != nil || tag.RowsAffected() != 0 {
					t.Errorf("%s: %v rows %d, want 0", q, err, tag.RowsAffected())
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WithTeamTx: %v", err)
		}
		if got := pg.Exec(t, `SELECT make FROM garage.car WHERE id = '`+idCarA+`'`); got != "Mugen" {
			t.Errorf("team A's car was changed: %s", got)
		}
	})

	t.Run("without WithTeamTx nothing is visible", func(t *testing.T) {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM garage.car`).Scan(&n); err != nil || n != 0 {
			t.Errorf("direct pool read: %d rows, %v", n, err)
		}
	})

	t.Run("commit, rollback and panic", func(t *testing.T) {
		insert := func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `INSERT INTO garage.car (team_id, make, model, transponder_number) VALUES (app.current_team_id(), 'T', 'T', '7777777')`)
			return err
		}
		present := func() bool {
			return pg.Exec(t, `SELECT count(*) FROM garage.car WHERE transponder_number = '7777777'`) == "1"
		}

		boom := errors.New("boom")
		err := d.WithTeamTx(ctx, uA, tA, func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
			if err := insert(ctx, tx); err != nil {
				return err
			}
			return boom
		})
		if !errors.Is(err, boom) || present() {
			t.Fatalf("an error in fn must roll back: err %v, present %v", err, present())
		}

		func() {
			defer func() {
				if r := recover(); r != "kaboom" {
					t.Errorf("the panic must reach the caller (the recovery interceptor), got %v", r)
				}
			}()
			_ = d.WithTeamTx(ctx, uA, tA, func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
				_ = insert(ctx, tx)
				panic("kaboom")
			})
		}()
		if present() {
			t.Fatal("a panic in fn must roll back")
		}

		if err := d.WithTeamTx(ctx, uA, tA, func(ctx context.Context, tx db.DBTX, _ db.Membership) error { return insert(ctx, tx) }); err != nil || !present() {
			t.Fatalf("a nil return must commit: %v, present %v", err, present())
		}
		pg.Exec(t, `DELETE FROM garage.car WHERE transponder_number = '7777777'`)
	})

	t.Run("a read transaction refuses writes", func(t *testing.T) {
		err := d.WithTeamReadTx(ctx, uA, tA, func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
			_, err := tx.Exec(ctx, `INSERT INTO garage.car (team_id, make, model, transponder_number) VALUES (app.current_team_id(), 'R', 'R', '5555555')`)
			if sqlState(err) != sqlReadOnly {
				t.Errorf("insert in a read transaction: %v, want SQLSTATE %s", err, sqlReadOnly)
			}
			return err
		})
		if err == nil {
			t.Fatal("the error must reach the caller")
		}
	})

	t.Run("the user context sees the user's teams and memberships only", func(t *testing.T) {
		err := d.WithUserTx(ctx, uA, func(ctx context.Context, tx db.DBTX) error {
			if n := count(t, tx, `SELECT count(*) FROM identity.team`); n != 1 {
				t.Errorf("user A sees %d teams, want 1", n)
			}
			if n := count(t, tx, `SELECT count(*) FROM identity.team WHERE id = $1`, idTeamB); n != 0 {
				t.Error("user A sees team B")
			}
			if n := count(t, tx, `SELECT count(*) FROM identity.membership`); n != 1 {
				t.Errorf("user A sees %d memberships, want its own 1 (team A has another member)", n)
			}
			if n := count(t, tx, `SELECT count(*) FROM garage.car`); n != 0 {
				t.Errorf("the user context (no team) reads %d cars", n)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WithUserTx: %v", err)
		}
	})

	t.Run("the user context cannot create a team (that is F1-06's variant)", func(t *testing.T) {
		err := d.WithUserTx(ctx, uA, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `INSERT INTO identity.team (name, created_by) VALUES ('Nova', $1)`, idUserA)
			return err
		})
		if sqlState(err) != sqlStateRLS {
			t.Fatalf("insert team in a user context: %v, want SQLSTATE %s", err, sqlStateRLS)
		}
	})
}

func TestZeroAndMalformedIdsNeverReachTheDatabase(t *testing.T) {
	t.Parallel()

	for name, s := range map[string]string{
		"empty":        "",
		"garbage":      "abc",
		"nil uuid":     "00000000-0000-0000-0000-000000000000",
		"braces":       "{01900000-0000-7000-8000-0000000000a1}",
		"urn":          "urn:uuid:01900000-0000-7000-8000-0000000000a1",
		"no hyphens":   "019000000000700080000000000000a1",
		"too long":     idTeamA + "0",
		"sql":          "'; DROP TABLE garage.car; --",
		"newline":      idTeamA[:35] + "\n",
		"non-hex char": "0190000g-0000-7000-8000-0000000000a1",
	} {
		// A team id comes from the client: its error is the client's (INVALID_ARGUMENT). A user id
		// comes from our own session: its error is ours (internal) and must not become the client's.
		if _, err := db.ParseTeamID(s); !errors.Is(err, db.ErrInvalidScope) || !errors.Is(err, apperr.ErrInvalidArgument) {
			t.Errorf("%s: ParseTeamID err = %v", name, err)
		}
		if _, err := db.ParseUserID(s); !errors.Is(err, db.ErrInvalidScope) {
			t.Errorf("%s: ParseUserID err = %v", name, err)
		}
	}
	if _, err := db.ParseTeamID(strings.ToUpper(idTeamA)); err != nil {
		t.Errorf("upper case hex is still a canonical UUID: %v", err)
	}

	// The zero value of the types (nil UUID) is refused by the transaction itself; no pool needed.
	d := db.New(nil)
	called := false
	fn := func(context.Context, db.DBTX, db.Membership) error { called = true; return nil }
	for name, err := range map[string]error{
		"zero user":       d.WithTeamTx(context.Background(), db.UserID{}, team(t, idTeamA), fn),
		"zero team":       d.WithTeamTx(context.Background(), user(t, idUserA), db.TeamID{}, fn),
		"zero user, read": d.WithTeamReadTx(context.Background(), db.UserID{}, team(t, idTeamA), fn),
		"zero user alone": d.WithUserTx(context.Background(), db.UserID{}, func(context.Context, db.DBTX) error { called = true; return nil }),
		"nil function":    d.WithTeamTx(context.Background(), user(t, idUserA), team(t, idTeamA), nil),
	} {
		if !errors.Is(err, db.ErrInvalidScope) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if called {
		t.Error("fn ran with an invalid scope")
	}
}

// The context must never outlive its transaction. With a pool of ONE connection every
// request reuses the same one, which is the worst case for a leak.
func TestTheContextDoesNotLeakToTheNextUseOfTheConnection(t *testing.T) {
	t.Parallel()
	_, pool, d := setup(t, db.MaxConns(1))
	ctx := context.Background()
	uA, tA := user(t, idUserA), team(t, idTeamA)

	clean := func(after string) {
		t.Helper()
		var cars int
		var team, usr string
		err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM garage.car), coalesce(current_setting('app.team_id', true), ''), coalesce(current_setting('app.user_id', true), '')`).Scan(&cars, &team, &usr)
		if err != nil {
			t.Fatalf("after %s: %v", after, err)
		}
		if cars != 0 || team != "" || usr != "" {
			t.Errorf("after %s the connection still has a context: cars %d, team %q, user %q", after, cars, team, usr)
		}
	}
	ok := func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
		if n := count(t, tx, `SELECT count(*) FROM garage.car`); n != 1 {
			t.Errorf("inside, %d cars", n)
		}
		return nil
	}

	if err := d.WithTeamTx(ctx, uA, tA, ok); err != nil {
		t.Fatalf("commit: %v", err)
	}
	clean("a commit")

	_ = d.WithTeamTx(ctx, uA, tA, func(ctx context.Context, tx db.DBTX, m db.Membership) error {
		_ = ok(ctx, tx, m)
		return errors.New("boom")
	})
	clean("a rollback")

	func() {
		defer func() { _ = recover() }()
		_ = d.WithTeamTx(ctx, uA, tA, func(ctx context.Context, tx db.DBTX, m db.Membership) error { _ = ok(ctx, tx, m); panic("x") })
	}()
	clean("a panic")

	cctx, cancel := context.WithCancel(ctx)
	err := d.WithTeamTx(cctx, uA, tA, func(ctx context.Context, tx db.DBTX, m db.Membership) error {
		_ = ok(ctx, tx, m)
		cancel() // the caller gives up in the middle
		return ctx.Err()
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
	clean("a cancellation")

	_ = d.WithTeamTx(ctx, uA, team(t, idTeamB), ok) // refused: not a member
	clean("a refused membership")

	if err := d.WithUserTx(ctx, uA, func(context.Context, db.DBTX) error { return nil }); err != nil {
		t.Fatalf("WithUserTx: %v", err)
	}
	clean("a user transaction")
}

// Many requests of different teams at the same time over few connections: each only ever
// sees its own team's rows (run with -race).
func TestConcurrentTeamsNeverSeeEachOther(t *testing.T) {
	t.Parallel()
	_, _, d := setup(t, db.MaxConns(4))
	uA, uB, tA, tB := user(t, idUserA), user(t, idUserB), team(t, idTeamA), team(t, idTeamB)

	var wg sync.WaitGroup
	for i := range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			u, tm, want := uA, tA, idCarA
			if i%2 == 1 {
				u, tm, want = uB, tB, idCarB
			}
			for range 15 {
				err := d.WithTeamReadTx(context.Background(), u, tm, func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
					rows, err := tx.Query(ctx, `SELECT id::text FROM garage.car`)
					if err != nil {
						return err
					}
					defer rows.Close()
					var got []string
					for rows.Next() {
						var id string
						if err := rows.Scan(&id); err != nil {
							return err
						}
						got = append(got, id)
					}
					if len(got) != 1 || got[0] != want {
						return fmt.Errorf("worker %d saw %v, want only %s", i, got, want)
					}
					return rows.Err()
				})
				if err != nil {
					t.Errorf("%v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestTheFunctionCannotCommitOrKeepTheTransaction(t *testing.T) {
	t.Parallel()
	_, _, d := setup(t)
	ctx := context.Background()
	var kept db.DBTX

	err := d.WithTeamTx(ctx, user(t, idUserA), team(t, idTeamA), func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
		kept = tx
		if _, ok := tx.(pgx.Tx); ok {
			t.Error("fn can reach the pgx.Tx")
		}
		if _, ok := tx.(interface{ Commit(context.Context) error }); ok {
			t.Error("fn can commit")
		}
		if _, ok := tx.(interface{ Rollback(context.Context) error }); ok {
			t.Error("fn can roll back")
		}
		if _, ok := tx.(interface {
			Begin(context.Context) (pgx.Tx, error)
		}); ok {
			t.Error("fn can open a nested transaction")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTeamTx: %v", err)
	}
	// After the function returned the handle is dead: a goroutine that outlived it cannot run
	// statements outside the transaction.
	if _, err := kept.Exec(ctx, `SELECT 1`); !errors.Is(err, db.ErrTxClosed) {
		t.Errorf("Exec after the end: %v", err)
	}
	if _, err := kept.Query(ctx, `SELECT 1`); !errors.Is(err, db.ErrTxClosed) {
		t.Errorf("Query after the end: %v", err)
	}
	if err := kept.QueryRow(ctx, `SELECT 1`).Scan(new(int)); !errors.Is(err, db.ErrTxClosed) {
		t.Errorf("QueryRow after the end: %v", err)
	}
}

// The pool refuses, at connection time, every role that would switch the isolation off.
func TestThePoolRefusesRolesThatBypassRLS(t *testing.T) {
	t.Parallel()
	pg := testutil.StartPostgres(t)
	ctx := context.Background()

	for name, url := range map[string]string{
		"the migrator (a member of pitbox_owner, sessions run as the owner)": pg.MigratorURL,
		"the superuser": pg.SuperURL,
	} {
		pool, err := db.Open(ctx, url)
		if err != nil {
			t.Fatalf("%s: Open must not connect: %v", name, err)
		}
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		err = pool.Ping(cctx)
		cancel()
		pool.Close()
		if err == nil {
			t.Errorf("%s: the pool accepted the role", name)
			continue
		}
		for _, secret := range []string{"postgres://", "password", pg.URL} {
			if strings.Contains(strings.ToLower(err.Error()), strings.ToLower(secret)) {
				t.Errorf("%s: the error leaks %q: %v", name, secret, err)
			}
		}
	}

	pool, err := db.Open(ctx, pg.URL)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Errorf("the application role must work: %v", err)
	}
}

// Documents the boundary of the design, so nobody is surprised by it: the context is a plain
// session variable, and any SQL that runs as the application role can rewrite it. What stops
// that is that no SQL of ours comes from outside data (parameters and sqlc) and that the
// architecture test refuses these strings outside this package. If this test ever starts
// failing, the database was hardened (for example, a signed context): update the ADR 0005.
func TestKnownLimitTheContextCanBeRewrittenBySQLRunningAsTheApplicationRole(t *testing.T) {
	t.Parallel()
	_, _, d := setup(t)
	err := d.WithTeamReadTx(context.Background(), user(t, idUserA), team(t, idTeamA), func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('app.team_id', $1, true)`, idTeamB); err != nil {
			t.Fatalf("set_config: %v", err)
		}
		if n := count(t, tx, `SELECT count(*) FROM garage.car WHERE id = $1`, idCarB); n != 1 {
			t.Errorf("the known limit no longer holds (%d): update ADR 0005 and the threat model", n)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTeamReadTx: %v", err)
	}
}
