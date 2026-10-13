package store_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/store"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/testutil"
)

// One real PostgreSQL (the compose image, schema.sql applied, application role pitbox_api)
// for all the subtests: starting a database is the expensive part.
func TestResolveLoginAgainstPostgres(t *testing.T) {
	t.Parallel()
	pg := testutil.StartPostgres(t)
	pool, err := db.Open(context.Background(), pg.URL)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(pool.Close)
	logins := store.NewLogins(pool)
	ctx := context.Background()

	t.Run("creates the user and is idempotent", func(t *testing.T) {
		a, err := logins.ResolveLogin(ctx, "sub-ana", "Ana@Exemplo.pt", "Ana")
		if err != nil || len(a) != 36 {
			t.Fatalf("first login: %q, %v", a, err)
		}
		b, err := logins.ResolveLogin(ctx, "sub-ana", "ana@exemplo.pt", "Ana")
		if err != nil || b != a {
			t.Fatalf("second login: %q, %v; want %q", b, err, a)
		}
		if got := pg.Exec(t, `SELECT count(*) FROM identity.user_account WHERE idp_subject = 'sub-ana'`); got != "1" {
			t.Errorf("rows = %s", got)
		}
	})

	t.Run("updates email and name", func(t *testing.T) {
		id, _ := logins.ResolveLogin(ctx, "sub-bia", "bia@exemplo.pt", "Bia")
		again, err := logins.ResolveLogin(ctx, "sub-bia", "bia.nova@exemplo.pt", "Bia Nova")
		if err != nil || again != id {
			t.Fatalf("%q, %v", again, err)
		}
		if got := pg.Exec(t, `SELECT email || '|' || display_name FROM identity.user_account WHERE id = '`+id+`'`); got != "bia.nova@exemplo.pt|Bia Nova" {
			t.Errorf("row = %s", got)
		}
	})

	t.Run("an email of another account is refused, never linked", func(t *testing.T) {
		first, _ := logins.ResolveLogin(ctx, "sub-carlos", "carlos@exemplo.pt", "Carlos")
		_, err := logins.ResolveLogin(ctx, "sub-intruso", "CARLOS@exemplo.pt", "Intruso")
		if !errors.Is(err, domain.ErrEmailInUse) {
			t.Fatalf("err = %v, want ErrEmailInUse", err)
		}
		if got := pg.Exec(t, `SELECT count(*) FROM identity.user_account WHERE idp_subject = 'sub-intruso'`); got != "0" {
			t.Errorf("the refused login created %s rows", got)
		}
		if again, _ := logins.ResolveLogin(ctx, "sub-carlos", "carlos@exemplo.pt", "Carlos"); again != first {
			t.Error("the original account changed")
		}
	})

	t.Run("a deleted account cannot sign in", func(t *testing.T) {
		id, _ := logins.ResolveLogin(ctx, "sub-dino", "dino@exemplo.pt", "Dino")
		pg.Exec(t, `UPDATE identity.user_account SET deleted_at = now() WHERE id = '`+id+`'`)
		if _, err := logins.ResolveLogin(ctx, "sub-dino", "dino@exemplo.pt", "Dino"); !errors.Is(err, domain.ErrAccountDeleted) {
			t.Fatalf("err = %v, want ErrAccountDeleted", err)
		}
	})

	t.Run("invalid input is not mistaken for a refusal", func(t *testing.T) {
		for _, c := range []struct{ sub, email string }{{"", "x@y.pt"}, {"sub-x", ""}, {"sub-x", "no-at-sign"}} {
			if _, err := logins.ResolveLogin(ctx, c.sub, c.email, "X"); !errors.Is(err, domain.ErrInvalidLogin) {
				t.Errorf("%+v: err = %v, want ErrInvalidLogin", c, err)
			}
		}
	})

	t.Run("the SQL cannot be injected through the arguments", func(t *testing.T) {
		evil := "x'); DROP TABLE identity.user_account; --"
		if _, err := logins.ResolveLogin(ctx, evil, "evil@exemplo.pt", evil); err != nil {
			t.Fatalf("ResolveLogin: %v", err)
		}
		if got := pg.Exec(t, `SELECT count(*) FROM identity.user_account WHERE idp_subject = '`+strings.ReplaceAll(evil, "'", "''")+`'`); got != "1" {
			t.Errorf("the value was not stored literally: %s", got)
		}
	})

	t.Run("the application role still cannot read accounts", func(t *testing.T) {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM identity.user_account`).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		if n != 0 {
			t.Errorf("pitbox_api without context sees %d accounts (RLS must hide them)", n)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO identity.user_account (idp_subject, email, display_name) VALUES ('direct', 'direct@x.pt', 'Direct')`); err == nil {
			t.Error("the application role inserted a user directly, bypassing resolve_login")
		}
	})

	// The first logins of one person can arrive together (two tabs): they must end with one
	// user, and every call must return that user's id.
	t.Run("simultaneous first logins of the same subject make one user", func(t *testing.T) {
		const n = 20
		ids := make([]string, n)
		var wg sync.WaitGroup
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				id, err := logins.ResolveLogin(ctx, "sub-race", "race@exemplo.pt", "Race")
				if err != nil {
					t.Errorf("call %d: %v", i, err)
				}
				ids[i] = id
			}()
		}
		wg.Wait()
		for _, id := range ids {
			if id == "" || id != ids[0] {
				t.Fatalf("ids differ: %v", ids)
			}
		}
		if got := pg.Exec(t, `SELECT count(*) FROM identity.user_account WHERE idp_subject = 'sub-race'`); got != "1" {
			t.Errorf("rows = %s", got)
		}
	})

	t.Run("simultaneous logins with one email: one account wins", func(t *testing.T) {
		const n = 12
		var wg sync.WaitGroup
		var mu sync.Mutex
		var ok, refused int
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := logins.ResolveLogin(ctx, fmt.Sprintf("sub-dup-%d", i), "dup@exemplo.pt", "Dup")
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
					ok++
				case errors.Is(err, domain.ErrEmailInUse):
					refused++
				default:
					t.Errorf("call %d: %v", i, err)
				}
			}()
		}
		wg.Wait()
		if ok != 1 || refused != n-1 {
			t.Errorf("ok = %d, refused = %d; want 1 and %d", ok, refused, n-1)
		}
		if got := pg.Exec(t, `SELECT count(*) FROM identity.user_account WHERE email = 'dup@exemplo.pt'`); got != "1" {
			t.Errorf("rows = %s", got)
		}
	})
}
