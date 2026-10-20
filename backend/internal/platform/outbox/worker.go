package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/authz"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/outbox/sqlcgen"
)

// The event this worker consumes. Identity membership is the only producer in F1.
const (
	AggregateMembership    = "identity.membership"
	EventMembershipChanged = "membership.changed"
)

var (
	errUnknownEvent  = errors.New("outbox: unknown event")
	errPayloadBroken = errors.New("outbox: event payload does not match its columns")
)

// claimedRow is the shape shared by the batch claim and the per-key drain.
type claimedRow struct {
	ID          int64
	TeamID      pgtype.UUID
	Aggregate   string
	AggregateID pgtype.UUID
	EventType   string
	Payload     []byte
	TraceParent pgtype.Text
	Attempts    int16
}

// Querier is the narrow database surface the worker needs (like store/login.go).
// The pool of the application role satisfies it; the worker opens no transaction.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

var _ sqlcgen.DBTX = (Querier)(nil)

// RoleWriter keeps one user's team relation in sync with the membership.
type RoleWriter interface {
	SetTeamRole(ctx context.Context, user, team string, role *string) error
}

// Options tunes the poller. Defaults match ADR 0015.
type Options struct {
	BatchSize    int
	Lease        time.Duration
	BaseBackoff  time.Duration
	MaxBackoff   time.Duration
	PollInterval time.Duration
	AlertAfter   int
	// RoleTimeout bounds one SetTeamRole call. The lease must cover a whole batch
	// with it, otherwise another instance could claim an event while this one is
	// still applying the batch and reorder a key (see NewProcessor).
	RoleTimeout time.Duration
}

// DefaultOptions returns the values of ADR 0015.
func DefaultOptions() Options {
	return Options{
		BatchSize:    20,
		Lease:        30 * time.Second,
		BaseBackoff:  2 * time.Second,
		MaxBackoff:   5 * time.Minute,
		PollInterval: time.Second,
		AlertAfter:   5,
		RoleTimeout:  200 * time.Millisecond,
	}
}

// Processor claims events and applies them to OpenFGA. It is safe to run more than
// one instance: the claim uses a lease and FOR UPDATE SKIP LOCKED.
type Processor struct {
	db      Querier
	roles   RoleWriter
	log     *slog.Logger
	tracer  trace.Tracer
	opts    Options
	claimed metric.Int64Counter
	applied metric.Int64Counter
	failed  metric.Int64Counter
}

// NewProcessor builds a Processor. meter and tracer may come from the global
// providers; nil is accepted (no-op metrics/traces).
func NewProcessor(db Querier, roles RoleWriter, opts Options, log *slog.Logger, mp metric.MeterProvider, tp trace.TracerProvider) (*Processor, error) {
	if db == nil || roles == nil || log == nil {
		return nil, errors.New("outbox: processor needs a database, a role writer and a logger")
	}
	if opts.BatchSize < 1 || opts.BatchSize > 200 {
		return nil, errors.New("outbox: batch size must be between 1 and 200")
	}
	if opts.Lease < time.Second || opts.Lease > 5*time.Minute {
		return nil, errors.New("outbox: lease must be between 1s and 5m")
	}
	if opts.BaseBackoff <= 0 || opts.MaxBackoff < opts.BaseBackoff {
		return nil, errors.New("outbox: invalid backoff")
	}
	if opts.PollInterval <= 0 {
		return nil, errors.New("outbox: poll interval must be positive")
	}
	if opts.AlertAfter < 1 {
		return nil, errors.New("outbox: alert threshold must be positive")
	}
	if opts.RoleTimeout <= 0 {
		return nil, errors.New("outbox: OpenFGA role timeout must be positive")
	}
	// The whole claimed batch is applied in series. If the lease expired mid-batch,
	// another instance could claim an event and apply a later event of the same key
	// first; the old event would then be applied at the end and could restore access.
	if opts.Lease < 2*time.Duration(opts.BatchSize)*opts.RoleTimeout {
		return nil, fmt.Errorf("outbox: lease %s must be at least 2 × batch (%d) × OpenFGA timeout (%s)", opts.Lease, opts.BatchSize, opts.RoleTimeout)
	}
	if mp == nil {
		mp = otel.GetMeterProvider()
	}
	if tp == nil {
		tp = otel.GetTracerProvider()
	}
	meter := mp.Meter("pitbox/outbox")
	claimed, err := meter.Int64Counter("outbox.claimed")
	if err != nil {
		return nil, fmt.Errorf("outbox: create metrics: %w", err)
	}
	applied, err := meter.Int64Counter("outbox.applied")
	if err != nil {
		return nil, fmt.Errorf("outbox: create metrics: %w", err)
	}
	failed, err := meter.Int64Counter("outbox.failed")
	if err != nil {
		return nil, fmt.Errorf("outbox: create metrics: %w", err)
	}
	return &Processor{
		db: db, roles: roles, log: log, tracer: tp.Tracer("pitbox/outbox"), opts: opts,
		claimed: claimed, applied: applied, failed: failed,
	}, nil
}

// Run processes batches until ctx is cancelled. It finishes the event in progress
// and does not mark the ones it did not apply; those return after their lease.
func (p *Processor) Run(ctx context.Context) error {
	ticker := time.NewTicker(p.opts.PollInterval)
	defer ticker.Stop()
	for {
		if err := p.ProcessBatch(ctx); err != nil && ctx.Err() == nil {
			// The batch could not even be claimed or marked: log and try again.
			p.log.ErrorContext(ctx, "outbox batch failed", slog.String("error", "database"))
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// ProcessBatch claims at most one event per key and applies it. A failure in one
// key does not stop the others; the failed event gets an exponential backoff and
// keeps blocking the following events of its own key (ADR 0015).
func (p *Processor) ProcessBatch(ctx context.Context) error {
	rows, err := sqlcgen.New(p.db).ClaimOutbox(ctx, sqlcgen.ClaimOutboxParams{
		LeaseSeconds: p.opts.Lease.Seconds(),
		BatchSize:    int32(p.opts.BatchSize), //nolint:gosec // NewProcessor bounds BatchSize to 1..200
	})
	if err != nil {
		return fmt.Errorf("claim outbox events: %w", err)
	}
	p.claimed.Add(ctx, int64(len(rows)))

	applied := make([]int64, 0, len(rows))
	for _, row := range rows {
		if ctx.Err() != nil {
			// Graceful shutdown: stop before starting another event. Applied ones
			// are still marked below, with a context that survives the cancellation.
			break
		}
		r := claimedRow{
			ID: row.ID, TeamID: row.TeamID, Aggregate: row.Aggregate, AggregateID: row.AggregateID,
			EventType: row.EventType, Payload: row.Payload, TraceParent: row.TraceParent, Attempts: row.Attempts,
		}
		if err := p.apply(ctx, r); err != nil {
			p.failed.Add(ctx, 1)
			p.retry(ctx, r, err)
			continue
		}
		applied = append(applied, r.ID)
		p.applied.Add(ctx, 1)
	}
	if len(applied) == 0 {
		return nil
	}

	// The mark must survive a cancellation: the events were applied and repeating
	// them would be safe but noisy.
	markCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := sqlcgen.New(p.db).MarkOutboxPublished(markCtx, applied); err != nil {
		return fmt.Errorf("mark outbox events: %w", err)
	}
	return nil
}

// DrainKey applies the unpublished events of one membership key in order. It is
// the same claim/apply/mark path as the worker, restricted to the key, so a
// delayed post-commit call cannot write a role over a newer event: if the
// worker already holds the oldest event, the drain does nothing.
func (p *Processor) DrainKey(ctx context.Context, teamID, aggregateID uuid.UUID) error {
	for {
		rows, err := sqlcgen.New(p.db).ClaimOutboxKey(ctx, sqlcgen.ClaimOutboxKeyParams{
			LeaseSeconds: p.opts.Lease.Seconds(),
			TeamID:       pgtype.UUID{Bytes: teamID, Valid: true},
			Aggregate:    AggregateMembership,
			AggregateID:  pgtype.UUID{Bytes: aggregateID, Valid: true},
			BatchSize:    int32(p.opts.BatchSize), //nolint:gosec // NewProcessor bounds BatchSize to 1..200
		})
		if err != nil {
			return fmt.Errorf("claim outbox key: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}
		p.claimed.Add(ctx, int64(len(rows)))
		applied := make([]int64, 0, len(rows))
		for _, row := range rows {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			r := claimedRow{
				ID: row.ID, TeamID: row.TeamID, Aggregate: row.Aggregate, AggregateID: row.AggregateID,
				EventType: row.EventType, Payload: row.Payload, TraceParent: row.TraceParent, Attempts: row.Attempts,
			}
			if err := p.apply(ctx, r); err != nil {
				p.failed.Add(ctx, 1)
				p.retry(ctx, r, err)
				return err
			}
			applied = append(applied, r.ID)
			p.applied.Add(ctx, 1)
		}
		markCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err = sqlcgen.New(p.db).MarkOutboxPublished(markCtx, applied)
		cancel()
		if err != nil {
			return fmt.Errorf("mark outbox key: %w", err)
		}
	}
}

func (p *Processor) apply(ctx context.Context, row claimedRow) error {
	if row.Aggregate != AggregateMembership || row.EventType != EventMembershipChanged {
		return errUnknownEvent
	}
	if !row.TeamID.Valid || !row.AggregateID.Valid {
		return errPayloadBroken
	}
	var payload membershipPayload
	if err := json.Unmarshal(row.Payload, &payload); err != nil {
		return errPayloadBroken
	}
	team := uuid.UUID(row.TeamID.Bytes)
	user := uuid.UUID(row.AggregateID.Bytes)
	if payload.TeamID != team || payload.UserID != user {
		return errPayloadBroken
	}

	ctx, span := p.tracer.Start(extractTrace(ctx, row.TraceParent.String), "outbox.publish")
	defer span.End()

	if err := p.roles.SetTeamRole(ctx, "user:"+user.String(), "team:"+team.String(), payload.RoleAfter); err != nil {
		return err
	}
	return nil
}

// retry replaces the lease with the backoff of the failed event. The event stays
// unpublished, so the NOT EXISTS in the claim keeps the following events of the
// same key waiting.
func (p *Processor) retry(ctx context.Context, row claimedRow, cause error) {
	backoff := p.backoff(row.Attempts)
	params := sqlcgen.RescheduleOutboxParams{BackoffSeconds: backoff.Seconds(), ID: row.ID}
	// The request context may be cancelled (a shutdown, a timeout): the backoff
	// must still replace the lease, or the event stays hidden for the whole lease.
	rescheduleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := sqlcgen.New(p.db).RescheduleOutbox(rescheduleCtx, params); err != nil {
		// The lease will expire on its own; the event is not lost.
		p.log.WarnContext(ctx, "outbox event could not be rescheduled",
			slog.Int64("outbox_id", row.ID), slog.String("error", "database"))
	}
	level := slog.LevelWarn
	if int(row.Attempts) < p.opts.AlertAfter {
		level = slog.LevelInfo
	}
	p.log.LogAttrs(ctx, level, "outbox event failed",
		slog.Int64("outbox_id", row.ID),
		slog.String("aggregate", row.Aggregate),
		slog.String("event_type", row.EventType),
		slog.Int("attempts", int(row.Attempts)),
		slog.String("reason", reason(cause)),
	)
}

// backoff is min(2^attempts × BaseBackoff, MaxBackoff). attempts is the value
// after the claim incremented it (1 on the first failure).
func (p *Processor) backoff(attempts int16) time.Duration {
	n := int(attempts)
	if n < 1 {
		n = 1
	}
	if n > 8 {
		n = 8
	}
	d := p.opts.BaseBackoff * time.Duration(1<<n)
	if d <= 0 || d > p.opts.MaxBackoff {
		return p.opts.MaxBackoff
	}
	return d
}

func extractTrace(ctx context.Context, traceParent string) context.Context {
	if traceParent == "" {
		return ctx
	}
	return propagation.TraceContext{}.Extract(ctx, propagation.MapCarrier{"traceparent": traceParent})
}

// reason keeps infrastructure details (which may echo the request) out of the log.
func reason(err error) string {
	switch {
	case errors.Is(err, errUnknownEvent):
		return "unknown event"
	case errors.Is(err, errPayloadBroken), errors.Is(err, authz.ErrInvalidRole):
		return "invalid payload"
	default:
		return "apply failed"
	}
}

type membershipPayload struct {
	TeamID    uuid.UUID `json:"team_id"`
	UserID    uuid.UUID `json:"user_id"`
	RoleAfter *string   `json:"role_after"`
}

// MembershipChanged builds the event of a membership state change. roleAfter is
// nil when the member was removed. Keeping the builder here makes the payload
// shape one thing, shared by the producer and this worker.
func MembershipChanged(team, user uuid.UUID, roleAfter *string) Event {
	return Event{
		TeamID:      team,
		Aggregate:   AggregateMembership,
		AggregateID: user,
		Type:        EventMembershipChanged,
		Payload: membershipPayload{
			TeamID:    team,
			UserID:    user,
			RoleAfter: roleAfter,
		},
	}
}
