// Package audit writes the audit trail (audit.audit_log).
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/auth"
)

// Execer is what the writer needs from a pool or a transaction.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Writer records the events of the login flow as platform events (team_id NULL). The
// use cases of the modules write their own entries inside their transaction (F1-04+).
type Writer struct{ db Execer }

// NewWriter takes the pool of the application role.
func NewWriter(db Execer) *Writer { return &Writer{db: db} }

const insert = `
INSERT INTO audit.audit_log (team_id, actor_kind, actor_id, action, object_type, object_id, after, trace_id, ip, user_agent)
VALUES (NULL, $1, $2, $3, 'session', $4, $5, $6, $7, $8)`

// Record implements auth.Auditor. The statement has no RETURNING: the table's read policy
// only shows a team's own rows, and the append-only trail is not read back by the application.
func (w *Writer) Record(ctx context.Context, e auth.AuditEvent) error {
	var after []byte
	if e.Reason != "" {
		var err error
		if after, err = json.Marshal(map[string]string{"reason": e.Reason}); err != nil {
			return fmt.Errorf("marshal audit detail: %w", err)
		}
	}
	var ip *netip.Addr
	if e.Meta.IP.IsValid() {
		ip = &e.Meta.IP
	}
	if _, err := w.db.Exec(ctx, insert,
		string(e.ActorKind), e.ActorID, e.Action, nullIfEmpty(e.SessionRef), after,
		nullIfEmpty(e.Meta.TraceID), ip, nullIfEmpty(e.Meta.UserAgent),
	); err != nil {
		return fmt.Errorf("write audit entry: %w", err)
	}
	return nil
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
