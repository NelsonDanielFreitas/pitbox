package audit_test

import (
	"context"
	"net/netip"
	"strings"
	"testing"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/audit"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/auth"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/interceptors"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/testutil"
)

func TestWriterAgainstPostgres(t *testing.T) {
	t.Parallel()
	pg := testutil.StartPostgres(t)
	pool, err := db.Open(context.Background(), pg.URL)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(pool.Close)
	w := audit.NewWriter(pool)
	ctx := context.Background()

	t.Run("a login", func(t *testing.T) {
		err := w.Record(ctx, auth.AuditEvent{
			Action: "auth.login", ActorKind: auth.ActorUser, ActorID: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60", SessionRef: "ref-1",
			Meta: interceptors.AuditMeta{TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", IP: netip.MustParseAddr("203.0.113.7"), UserAgent: "agent/1.0"},
		})
		if err != nil {
			t.Fatalf("Record: %v", err)
		}
		row := pg.Exec(t, `SELECT concat_ws('|', team_id, actor_kind, actor_id, action, object_type, object_id, coalesce(after::text,'-'), trace_id, host(ip), user_agent)
			FROM audit.audit_log WHERE object_id = 'ref-1'`)
		want := "user|0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60|auth.login|session|ref-1|-|4bf92f3577b34da6a3ce929d0e0e4736|203.0.113.7|agent/1.0"
		if row != want {
			t.Errorf("row = %s\nwant  %s", row, want)
		}
	})

	t.Run("a failure keeps the reason and tolerates missing metadata", func(t *testing.T) {
		if err := w.Record(ctx, auth.AuditEvent{Action: "auth.login_failed", ActorKind: auth.ActorSystem, ActorID: "anonymous", Reason: "nonce_mismatch"}); err != nil {
			t.Fatalf("Record: %v", err)
		}
		if got := pg.Exec(t, `SELECT after->>'reason' || '|' || coalesce(host(ip),'NULL') || '|' || coalesce(object_id,'NULL') || '|' || actor_kind
			FROM audit.audit_log WHERE action = 'auth.login_failed'`); got != "nonce_mismatch|NULL|NULL|system" {
			t.Errorf("row = %s", got)
		}
	})

	t.Run("a hostile user agent and reason are stored as data", func(t *testing.T) {
		evil := "x'); DELETE FROM audit.audit_log; --"
		if err := w.Record(ctx, auth.AuditEvent{Action: "auth.login_failed", ActorKind: auth.ActorSystem, ActorID: "anonymous", Reason: evil, Meta: interceptors.AuditMeta{UserAgent: evil}}); err != nil {
			t.Fatalf("Record: %v", err)
		}
		if got := pg.Exec(t, `SELECT count(*) FROM audit.audit_log`); got == "0" {
			t.Error("the audit log was emptied")
		}
	})

	t.Run("the trail is append-only for the application", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `UPDATE audit.audit_log SET action = 'x'`); err == nil {
			t.Error("the application role updated the audit log")
		}
		if _, err := pool.Exec(ctx, `DELETE FROM audit.audit_log`); err == nil {
			t.Error("the application role deleted from the audit log")
		}
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit.audit_log`).Scan(&n); err != nil || n != 0 {
			t.Errorf("without a team the application reads %d rows (err %v); platform events are not readable by it", n, err)
		}
	})

	t.Run("a closed database is an error, never a silent success", func(t *testing.T) {
		closed, _ := db.Open(ctx, pg.URL)
		closed.Close()
		err := audit.NewWriter(closed).Record(ctx, auth.AuditEvent{Action: "auth.login", ActorKind: auth.ActorUser, ActorID: "u"})
		if err == nil {
			t.Fatal("Record on a closed pool succeeded")
		}
		if strings.Contains(err.Error(), "postgres://") {
			t.Errorf("the error leaks the connection string: %v", err)
		}
	})
}
