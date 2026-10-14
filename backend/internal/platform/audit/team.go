package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/interceptors"
)

// ErrNoContext means the statement ran outside db.WithTeamTx (or WithTeamReadTx): the
// transaction has no team and user. Nothing is written.
var ErrNoContext = errors.New("audit: the transaction has no team and user context")

// ErrInvalidEvent means a field of the event is not acceptable. The text never repeats it.
var ErrInvalidEvent = errors.New("audit: invalid event")

// TeamEvent is the part of an audit entry that a use case knows: what was done, to what, and the
// state before and after. It has no team and no author: those come from the transaction's own
// context (app.current_team_id() and app.current_user_id()), set by db.WithTeamTx, so a use case
// cannot audit in another team or in someone else's name.
type TeamEvent struct {
	Action     string // "setup.update", "member.role_changed": lower case, dot separated
	ObjectType string // "setup", "member"
	ObjectID   string // "" when there is none
	Before     any    // marshalled to JSON; keep personal data and secrets out of it
	After      any
	RequestID  string // the request's UUID (idempotency key), "" when there is none
}

const (
	maxFieldBytes = 200
	maxJSONBytes  = 64 << 10
)

var (
	actionPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)
	objectPattern = regexp.MustCompile(`^[a-z][a-z0-9_.]*$`)
)

// The SELECT with a WHERE (instead of VALUES) makes the insert write nothing when the context is
// missing, and the row count says so. The trace id, address and user agent are those that the Audit
// interceptor attached to the request, and none comes from an argument of the use case.
const insertTeamSQL = `
INSERT INTO audit.audit_log (team_id, actor_kind, actor_id, action, object_type, object_id, before, after, request_id, trace_id, ip, user_agent)
SELECT app.current_team_id(), 'user', app.current_user_id()::text, $1, $2, $3, $4::jsonb, $5::jsonb, $6::uuid, $7, $8::inet, $9
 WHERE app.current_team_id() IS NOT NULL AND app.current_user_id() IS NOT NULL`

// RecordTeam writes the entry in the caller's transaction: it is committed with the change it
// describes, and rolled back with it. The append-only table has no UPDATE or DELETE for the
// application.
func RecordTeam(ctx context.Context, tx Execer, e TeamEvent) error {
	if !actionPattern.MatchString(e.Action) || len(e.Action) > maxFieldBytes ||
		!objectPattern.MatchString(e.ObjectType) || len(e.ObjectType) > maxFieldBytes || len(e.ObjectID) > maxFieldBytes {
		return ErrInvalidEvent
	}
	var requestID *string
	if e.RequestID != "" {
		if len(e.RequestID) != 36 {
			return ErrInvalidEvent
		}
		if _, err := uuid.Parse(e.RequestID); err != nil {
			return ErrInvalidEvent
		}
		requestID = &e.RequestID
	}
	before, err := marshal(e.Before)
	if err != nil {
		return err
	}
	after, err := marshal(e.After)
	if err != nil {
		return err
	}

	meta, _ := interceptors.AuditMetaFromContext(ctx)
	var ip *string
	if meta.IP.IsValid() {
		s := meta.IP.String()
		ip = &s
	}
	tag, err := tx.Exec(ctx, insertTeamSQL,
		e.Action, e.ObjectType, nullIfEmpty(e.ObjectID), before, after, requestID,
		nullIfEmpty(meta.TraceID), ip, nullIfEmpty(meta.UserAgent))
	if err != nil {
		return fmt.Errorf("write team audit entry: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrNoContext
	}
	return nil
}

// marshal returns the JSON of v, nil for none, and refuses an oversized value.
func marshal(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil || len(b) > maxJSONBytes {
		return nil, ErrInvalidEvent
	}
	return b, nil
}
