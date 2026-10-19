package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/authz"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/outbox/sqlcgen"
)

// The generated DBTX is the narrow surface; db.DBTX is the wider one the use
// cases receive and must satisfy it (F1-05b requirement).
var _ sqlcgen.DBTX = (db.DBTX)(nil)

type capturedExec struct {
	sql  string
	args []any
}

type fakeDB struct {
	exec  *capturedExec
	query bool
}

func (f *fakeDB) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.exec = &capturedExec{sql: sql, args: args}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (f *fakeDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	f.query = true
	return nil, errors.New("unexpected query")
}

func (f *fakeDB) QueryRow(context.Context, string, ...any) pgx.Row {
	f.query = true
	return failingRow{}
}

func (f *fakeDB) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	f.query = true
	return nil
}

func (f *fakeDB) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	f.query = true
	return 0, errors.New("unexpected copy")
}

type failingRow struct{}

func (failingRow) Scan(...any) error { return errors.New("unexpected query") }

var _ sqlcgen.DBTX = (*fakeDB)(nil)

func TestRecordWritesTheMembershipEvent(t *testing.T) {
	t.Parallel()
	team := uuid.Must(uuid.NewV7())
	user := uuid.Must(uuid.NewV7())
	role := "mechanic"
	e := MembershipChanged(team, user, &role)

	f := &fakeDB{}
	require.NoError(t, Record(context.Background(), f, e))
	require.NotNil(t, f.exec)
	require.False(t, f.query, "Record must not read the table")

	args := f.exec.args
	require.Len(t, args, 6)
	require.Equal(t, pgtype.UUID{Bytes: team, Valid: true}, args[0])
	require.Equal(t, AggregateMembership, args[1])
	require.Equal(t, pgtype.UUID{Bytes: user, Valid: true}, args[2])
	require.Equal(t, EventMembershipChanged, args[3])

	var payload membershipPayload
	require.NoError(t, json.Unmarshal(args[4].([]byte), &payload))
	require.Equal(t, team, payload.TeamID)
	require.Equal(t, user, payload.UserID)
	require.Equal(t, &role, payload.RoleAfter)
	require.Equal(t, pgtype.Text{}, args[5], "no span means no traceparent")
}

func TestRecordWritesTheTraceParent(t *testing.T) {
	t.Parallel()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	ctx, span := tp.Tracer("test").Start(context.Background(), "request")
	defer span.End()

	f := &fakeDB{}
	require.NoError(t, Record(ctx, f, MembershipChanged(uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), nil)))

	trace, ok := f.exec.args[5].(pgtype.Text)
	require.True(t, ok)
	require.True(t, trace.Valid)
	require.Regexp(t, regexp.MustCompile(`^00-[0-9a-f]{32}-[0-9a-f]{16}-0[01]$`), trace.String)
}

func TestRecordRejectsInvalidEvents(t *testing.T) {
	t.Parallel()
	team := uuid.Must(uuid.NewV7())
	user := uuid.Must(uuid.NewV7())

	tests := []struct {
		name  string
		event Event
	}{
		{"empty aggregate", Event{TeamID: team, AggregateID: user, Type: "membership.changed", Payload: struct{}{}}},
		{"aggregate without a dot", Event{TeamID: team, Aggregate: "membership", AggregateID: user, Type: "membership.changed", Payload: struct{}{}}},
		{"empty type", Event{TeamID: team, Aggregate: "identity.membership", AggregateID: user, Payload: struct{}{}}},
		{"type without a dot", Event{TeamID: team, Aggregate: "identity.membership", AggregateID: user, Type: "changed", Payload: struct{}{}}},
		{"nil aggregate id", Event{TeamID: team, Aggregate: "identity.membership", Type: "membership.changed", Payload: struct{}{}}},
		{"oversized payload", Event{TeamID: team, Aggregate: "identity.membership", AggregateID: user, Type: "membership.changed", Payload: strings.Repeat("x", maxJSONBytes)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := &fakeDB{}
			require.ErrorIs(t, Record(context.Background(), f, tc.event), ErrInvalidEvent)
			require.Nil(t, f.exec)
		})
	}
}

func TestReasonMapsAnInvalidRoleToInvalidPayload(t *testing.T) {
	t.Parallel()
	require.Equal(t, "invalid payload", reason(authz.ErrInvalidRole))
}

func TestMembershipChangedCarriesState(t *testing.T) {
	t.Parallel()
	team := uuid.Must(uuid.NewV7())
	user := uuid.Must(uuid.NewV7())

	e := MembershipChanged(team, user, nil)
	require.Equal(t, AggregateMembership, e.Aggregate)
	require.Equal(t, EventMembershipChanged, e.Type)
	require.Equal(t, user, e.AggregateID)

	raw, err := json.Marshal(e.Payload)
	require.NoError(t, err)
	require.JSONEq(t, `{"team_id":"`+team.String()+`","user_id":"`+user.String()+`","role_after":null}`, string(raw))
}

func TestBackoffCapsAtTheMaximum(t *testing.T) {
	t.Parallel()
	p := &Processor{opts: Options{BaseBackoff: 2 * time.Second, MaxBackoff: 5 * time.Minute}}
	tests := []struct {
		attempts int16
		want     time.Duration
	}{
		{0, 4 * time.Second},
		{1, 4 * time.Second},
		{2, 8 * time.Second},
		{3, 16 * time.Second},
		{7, 256 * time.Second},
		{8, 5 * time.Minute},
		{100, 5 * time.Minute},
	}
	for _, tc := range tests {
		require.Equal(t, tc.want, p.backoff(tc.attempts), "attempts %d", tc.attempts)
	}
}

func TestNewProcessorValidatesOptions(t *testing.T) {
	t.Parallel()
	log := testLogger()
	opts := DefaultOptions()
	opts.BatchSize = 0
	_, err := NewProcessor(&fakeDB{}, fakeRoles{}, opts, log, nil, nil)
	require.Error(t, err)

	opts = DefaultOptions()
	opts.Lease = 0
	_, err = NewProcessor(&fakeDB{}, fakeRoles{}, opts, log, nil, nil)
	require.Error(t, err)

	// The lease must cover a whole batch of SetTeamRole calls (ADR 0015).
	opts = DefaultOptions()
	opts.BatchSize = 200
	opts.Lease = time.Second
	opts.RoleTimeout = 200 * time.Millisecond
	_, err = NewProcessor(&fakeDB{}, fakeRoles{}, opts, log, nil, nil)
	require.ErrorContains(t, err, "lease")

	_, err = NewProcessor(nil, fakeRoles{}, DefaultOptions(), log, nil, nil)
	require.Error(t, err)
}

type fakeRoles struct{}

func (fakeRoles) SetTeamRole(context.Context, string, string, *string) error { return nil }

func testLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}
