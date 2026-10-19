// Package outbox writes domain events in the same transaction as the change they
// describe, and (in the worker) publishes them. The event log is the transactional
// outbox of ADR 0005; the consumers are idempotent because the events carry state,
// not deltas (ADR 0015).
package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel/trace"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/outbox/sqlcgen"
)

// ErrInvalidEvent means a field of the event is not acceptable. The text never repeats it.
var ErrInvalidEvent = errors.New("outbox: invalid event")

const (
	maxFieldBytes = 200
	maxJSONBytes  = 64 << 10
)

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

// Event is a domain event to be published. TeamID may be the nil UUID for a
// platform-wide event (the column is nullable). AggregateID is never nil.
type Event struct {
	TeamID      uuid.UUID
	Aggregate   string // "identity.membership"
	AggregateID uuid.UUID
	Type        string // "membership.changed"
	Payload     any    // marshalled to JSON; keep personal data and secrets out of it
}

// Record inserts the event in the caller's transaction. It does no network I/O and
// never reads the table: the event is committed or rolled back with the change.
func Record(ctx context.Context, tx db.DBTX, e Event) error {
	if !namePattern.MatchString(e.Aggregate) || len(e.Aggregate) > maxFieldBytes ||
		!namePattern.MatchString(e.Type) || len(e.Type) > maxFieldBytes {
		return ErrInvalidEvent
	}
	if e.AggregateID == uuid.Nil {
		return ErrInvalidEvent
	}
	payload, err := json.Marshal(e.Payload)
	if err != nil || len(payload) > maxJSONBytes {
		return ErrInvalidEvent
	}

	team := pgtype.UUID{Bytes: e.TeamID, Valid: e.TeamID != uuid.Nil}
	tp := traceParent(ctx)
	var traceValue pgtype.Text
	if tp != "" {
		traceValue = pgtype.Text{String: tp, Valid: true}
	}

	if err := sqlcgen.New(tx).InsertOutbox(ctx, sqlcgen.InsertOutboxParams{
		TeamID:      team,
		Aggregate:   e.Aggregate,
		AggregateID: pgtype.UUID{Bytes: e.AggregateID, Valid: true},
		EventType:   e.Type,
		Payload:     payload,
		TraceParent: traceValue,
	}); err != nil {
		return fmt.Errorf("write outbox event: %w", err)
	}
	return nil
}

// traceParent returns the W3C traceparent of the current span, or "" without a span.
func traceParent(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	flags := byte(0)
	if sc.IsSampled() {
		flags = 1
	}
	return fmt.Sprintf("00-%s-%s-%02x", sc.TraceID(), sc.SpanID(), flags)
}
