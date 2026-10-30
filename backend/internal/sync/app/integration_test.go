package app_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/authz"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/clock"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/syncapply"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/testutil"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/sync/app"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/sync/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/sync/store"
)

// --- fakes -------------------------------------------------------------------

type fakeAuthz struct {
	mu    sync.Mutex
	roles map[string]string
}

func (f *fakeAuthz) set(user, team, role string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.roles[user+"|"+team] = role
}

func (f *fakeAuthz) Check(_ context.Context, user, relation, object string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	role := f.roles[strings.TrimPrefix(user, "user:")+"|"+strings.TrimPrefix(object, "team:")]
	ok := false
	switch relation {
	case "member":
		ok = role == "owner" || role == "mechanic" || role == "pilot"
	case "mechanic":
		ok = role == "owner" || role == "mechanic"
	}
	if !ok {
		return authz.ErrDenied
	}
	return nil
}

type fakeCatalog struct{}

func (fakeCatalog) Load(context.Context) (string, json.RawMessage, error) {
	return "cat-v1", json.RawMessage(`{"scales":[]}`), nil
}

// --- harness -----------------------------------------------------------------

type harness struct {
	pg    testutil.Postgres
	pool  *pgxpool.Pool
	svc   *app.Service
	authz *fakeAuthz
	ctx   context.Context
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	pg := testutil.StartPostgres(t)
	pool, err := db.Open(context.Background(), pg.URL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	az := &fakeAuthz{roles: map[string]string{}}
	svc, err := app.New(app.Deps{
		DB: db.New(pool), Store: store.NewRepo(), Authz: az, Clock: clock.Real(),
		Log: slog.New(slog.DiscardHandler), Appliers: syncapply.NewRegistry(garage.NewCarApplier(), noteApplier{}),
		PassSigner:   domain.PassSigner{KeyID: "k1", Key: priv},
		PassVerifier: domain.PassVerifier{"k1": pub}, Catalog: fakeCatalog{},
	})
	require.NoError(t, err)
	return &harness{pg: pg, pool: pool, svc: svc, authz: az, ctx: context.Background()}
}

func uid(id uuid.UUID) db.UserID { return db.UserID(id) }
func tid(id uuid.UUID) db.TeamID { return db.TeamID(id) }

func (h *harness) user(t *testing.T, label string) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	h.pg.Exec(t, fmt.Sprintf(`INSERT INTO identity.user_account (id, idp_subject, email, display_name) VALUES ('%s','%s','%s@example.pt','%s')`,
		id, label+"-"+id.String(), label, label))
	return id
}

func (h *harness) team(t *testing.T, owner uuid.UUID) uuid.UUID {
	t.Helper()
	team := uuid.Must(uuid.NewV7())
	h.pg.Exec(t, fmt.Sprintf(`INSERT INTO identity.team (id, name, created_by) VALUES ('%s','Team','%s')`, team, owner))
	h.member(t, team, owner, "owner")
	return team
}

func (h *harness) member(t *testing.T, team, user uuid.UUID, role string) {
	t.Helper()
	h.pg.Exec(t, fmt.Sprintf(`INSERT INTO identity.membership (team_id, user_id, role) VALUES ('%s','%s','%s')`, team, user, role))
	h.authz.set(user.String(), team.String(), role)
}

// device plays the app: it owns an ECDSA key, registers it and signs operations.
type device struct {
	h    *harness
	key  *ecdsa.PrivateKey
	user uuid.UUID
	team uuid.UUID
	id   uuid.UUID
	pass string
}

func p1363(t *testing.T, key *ecdsa.PrivateKey, msg []byte) []byte {
	t.Helper()
	d := sha256.Sum256(msg)
	r, s, err := ecdsa.Sign(rand.Reader, key, d[:])
	require.NoError(t, err)
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return sig
}

func (h *harness) device(t *testing.T, user, team uuid.UUID) *device {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	d := &device{h: h, key: key, user: user, team: team}
	d.id, err = h.svc.RegisterDevice(h.ctx, uid(user), tid(team), app.RegisterDeviceInput{
		Name: "phone", Platform: "web", PublicKey: der, RequestID: uuid.New(),
		Proof: p1363(t, key, domain.RegistrationMessage(team, user, der)),
	})
	require.NoError(t, err)
	d.pass, _, err = h.svc.IssueOfflinePass(h.ctx, uid(user), tid(team), d.id)
	require.NoError(t, err)
	return d
}

func (d *device) op(t *testing.T, kind domain.Kind, entityID uuid.UUID, base *int32, fields string) domain.Operation {
	t.Helper()
	return d.opFor(t, "garage.car", kind, entityID, base, fields)
}

func (d *device) opFor(t *testing.T, entity string, kind domain.Kind, entityID uuid.UUID, base *int32, fields string) domain.Operation {
	t.Helper()
	op := domain.Operation{
		OpID: uuid.Must(uuid.NewV7()), Entity: entity, EntityID: entityID, Kind: kind,
		BaseVersion: base, FieldsJSON: []byte(fields), ClientTS: time.Now().Truncate(time.Millisecond),
	}
	op.Signature = p1363(t, d.key, domain.SigningMessage(d.team, d.id, op))
	return op
}

func (d *device) push(t *testing.T, ops ...domain.Operation) []app.OpResult {
	t.Helper()
	res, err := d.h.svc.Push(d.h.ctx, uid(d.user), tid(d.team), app.PushInput{DeviceID: d.id, Pass: d.pass, Ops: ops})
	require.NoError(t, err)
	return res
}

func ver(v int32) *int32 { return &v }

// noteApplier stands in for an entity that a pilot may write (the driver's feedback): it
// accepts every append and never stores anything.
type noteApplier struct{}

func (noteApplier) Entity() string { return "test.note" }
func (noteApplier) CanWrite(role string) bool {
	return role == "owner" || role == "mechanic" || role == "pilot"
}
func (noteApplier) CurrentVersion(context.Context, db.DBTX, db.TeamID, uuid.UUID) (int32, error) {
	return 0, nil
}
func (noteApplier) Apply(context.Context, db.DBTX, syncapply.Op) (int32, error) { return 1, nil }
func (noteApplier) Read(context.Context, db.DBTX, db.TeamID, string, uuid.UUID) (json.RawMessage, bool, error) {
	return nil, false, nil
}

// --- tests -------------------------------------------------------------------

func TestPushIsIdempotent(t *testing.T) {
	h := newHarness(t)
	user := h.user(t, "mech")
	team := h.team(t, user)
	d := h.device(t, user, team)
	car := uuid.Must(uuid.NewV7())

	create := d.op(t, domain.KindCreate, car, nil, `{"make":"Kyosho","model":"MP10"}`)
	first := d.push(t, create)
	require.Equal(t, domain.StatusApplied, first[0].Status)

	// The same queue again, even several times: same answer, nothing duplicated.
	for range 3 {
		again := d.push(t, create)
		require.Equal(t, first, again)
	}
	require.Equal(t, "1", h.pg.Exec(t, fmt.Sprintf(`SELECT count(*) FROM garage.car WHERE id = '%s'`, car)))
	require.Equal(t, "1", h.pg.Exec(t, fmt.Sprintf(`SELECT count(*) FROM sync.operation WHERE op_id = '%s'`, create.OpID)))
}

func TestFieldMergeAndConflict(t *testing.T) {
	h := newHarness(t)
	user := h.user(t, "mech")
	team := h.team(t, user)
	d := h.device(t, user, team)
	car := uuid.Must(uuid.NewV7())
	require.Equal(t, domain.StatusApplied, d.push(t, d.op(t, domain.KindCreate, car, nil, `{"make":"K","model":"A"}`))[0].Status)

	// Another device changed the model from version 1.
	require.Equal(t, domain.StatusApplied, d.push(t, d.op(t, domain.KindPatch, car, ver(1), `{"model":"B"}`))[0].Status)

	// Different field from the same base: merged, both survive.
	merged := d.push(t, d.op(t, domain.KindPatch, car, ver(1), `{"nickname":"Red"}`))[0]
	require.Equal(t, domain.StatusMerged, merged.Status)

	// Same field, other value: conflict; the server value stays, nothing else is lost.
	clash := d.push(t, d.op(t, domain.KindPatch, car, ver(1), `{"model":"C","nickname":"Red"}`))[0]
	require.Equal(t, domain.StatusConflict, clash.Status)

	require.Equal(t, "B|Red", h.pg.Exec(t, fmt.Sprintf(`SELECT model, nickname FROM garage.car WHERE id = '%s'`, car)))

	conflicts, err := h.svc.ListConflicts(h.ctx, uid(user), tid(team))
	require.NoError(t, err)
	require.Len(t, conflicts, 1)
	require.Equal(t, "model", conflicts[0].Field)
	require.JSONEq(t, `"B"`, string(conflicts[0].ServerValue))
	require.JSONEq(t, `"C"`, string(conflicts[0].ClientValue))

	// The mechanic picks the device's value; resolving twice changes nothing more.
	for range 2 {
		res, err := h.svc.ResolveConflict(h.ctx, uid(user), tid(team), conflicts[0].ID, app.ResolveClient, nil)
		require.NoError(t, err)
		require.True(t, res.Resolved)
	}
	require.Equal(t, "C|5", h.pg.Exec(t, fmt.Sprintf(`SELECT model, version FROM garage.car WHERE id = '%s'`, car)),
		"create + 3 applied patches + 1 resolution, no extra write on the second resolve")
}

func TestRejections(t *testing.T) {
	h := newHarness(t)
	user := h.user(t, "mech")
	team := h.team(t, user)
	d := h.device(t, user, team)
	car := uuid.Must(uuid.NewV7())

	t.Run("tampered operation is rejected and not recorded", func(t *testing.T) {
		op := d.op(t, domain.KindCreate, car, nil, `{"make":"K"}`)
		op.FieldsJSON = []byte(`{"make":"Evil"}`)
		r := d.push(t, op)[0]
		require.Equal(t, domain.StatusRejected, r.Status)
		require.Equal(t, domain.ReasonBadSignature, r.Reason)
		require.Equal(t, "0", h.pg.Exec(t, fmt.Sprintf(`SELECT count(*) FROM sync.operation WHERE op_id = '%s'`, op.OpID)))
	})

	t.Run("unknown entity is rejected and kept for review", func(t *testing.T) {
		op := d.op(t, domain.KindCreate, car, nil, `{}`)
		op.Entity = "events.pit_stop"
		op.Signature = p1363(t, d.key, domain.SigningMessage(team, d.id, op))
		r := d.push(t, op)[0]
		require.Equal(t, domain.StatusRejected, r.Status)
		require.Equal(t, domain.ReasonUnknownEntity, r.Reason)
	})

	t.Run("invalid reference rolls the write back", func(t *testing.T) {
		r := d.push(t, d.op(t, domain.KindCreate, car, nil, `{"model":"no make"}`))[0]
		require.Equal(t, domain.StatusRejected, r.Status)
		require.Equal(t, domain.ReasonInvalidFields, r.Reason)
	})

	t.Run("operation signed for another team cannot be replayed", func(t *testing.T) {
		other := h.team(t, user)
		od := h.device(t, user, other)
		op := d.op(t, domain.KindCreate, uuid.Must(uuid.NewV7()), nil, `{"make":"K"}`) // signed for team
		res, err := h.svc.Push(h.ctx, uid(user), tid(other), app.PushInput{DeviceID: od.id, Pass: od.pass, Ops: []domain.Operation{op}})
		require.NoError(t, err)
		require.Equal(t, domain.ReasonBadSignature, res[0].Reason)
	})

	t.Run("revoked device", func(t *testing.T) {
		rd := h.device(t, user, team)
		require.NoError(t, h.svc.RevokeDevice(h.ctx, uid(user), tid(team), rd.id, uuid.New()))
		r := rd.push(t, rd.op(t, domain.KindCreate, uuid.Must(uuid.NewV7()), nil, `{"make":"K"}`))[0]
		require.Equal(t, domain.StatusRejected, r.Status)
		require.Equal(t, domain.ReasonDeviceRevoked, r.Reason)
		_, _, err := h.svc.IssueOfflinePass(h.ctx, uid(user), tid(team), rd.id)
		require.ErrorIs(t, err, domain.ErrNotFound)
	})

	t.Run("forged or foreign pass", func(t *testing.T) {
		od := h.device(t, user, team)
		od.pass = d.pass // a pass of another device
		r := od.push(t, od.op(t, domain.KindCreate, uuid.Must(uuid.NewV7()), nil, `{"make":"K"}`))[0]
		require.Equal(t, domain.ReasonPassInvalid, r.Reason)
		od.pass = "garbage"
		r = od.push(t, od.op(t, domain.KindCreate, uuid.Must(uuid.NewV7()), nil, `{"make":"K"}`))[0]
		require.Equal(t, domain.ReasonPassInvalid, r.Reason)
	})

	t.Run("operation from the future", func(t *testing.T) {
		op := d.op(t, domain.KindCreate, uuid.Must(uuid.NewV7()), nil, `{"make":"K"}`)
		op.ClientTS = time.Now().Add(time.Hour)
		op.Signature = p1363(t, d.key, domain.SigningMessage(team, d.id, op))
		require.Equal(t, domain.ReasonBadTimestamp, d.push(t, op)[0].Reason)
	})
}

func TestAuthorization(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	team := h.team(t, owner)
	d := h.device(t, owner, team)

	pilot := h.user(t, "pilot")
	h.member(t, team, pilot, "pilot")
	stranger := h.user(t, "stranger")
	h.team(t, stranger)

	op := d.op(t, domain.KindCreate, uuid.Must(uuid.NewV7()), nil, `{"make":"K"}`)

	// A pilot reads and may open a Push, but each operation is checked against its entity: a
	// car is the mechanic's, the driver's feedback is the pilot's own.
	pd := h.device(t, pilot, team)
	carOp := pd.op(t, domain.KindCreate, uuid.Must(uuid.NewV7()), nil, `{"make":"K"}`)
	noteOp := pd.opFor(t, "test.note", domain.KindAppend, uuid.Must(uuid.NewV7()), nil, `{}`)
	res := pd.push(t, carOp, noteOp)
	require.Equal(t, domain.StatusRejected, res[0].Status)
	require.Equal(t, domain.ReasonForbidden, res[0].Reason)
	require.Equal(t, domain.StatusApplied, res[1].Status)
	require.Equal(t, "0", h.pg.Exec(t, `SELECT count(*) FROM garage.car`), "the pilot's car was not created")
	// A guest is not a member: it learns nothing.
	guest := h.user(t, "guest")
	h.member(t, team, guest, "guest")
	_, err := h.svc.Push(h.ctx, uid(guest), tid(team), app.PushInput{DeviceID: d.id, Pass: d.pass, Ops: []domain.Operation{op}})
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, err = h.svc.Pull(h.ctx, uid(pilot), tid(team), app.PullInput{})
	require.NoError(t, err)

	// A user of another team learns nothing: NOT_FOUND everywhere.
	_, err = h.svc.Push(h.ctx, uid(stranger), tid(team), app.PushInput{DeviceID: d.id, Pass: d.pass, Ops: []domain.Operation{op}})
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, err = h.svc.Pull(h.ctx, uid(stranger), tid(team), app.PullInput{})
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, err = h.svc.ListConflicts(h.ctx, uid(stranger), tid(team))
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, _, err = h.svc.IssueOfflinePass(h.ctx, uid(stranger), tid(team), d.id)
	require.ErrorIs(t, err, domain.ErrNotFound)

	// Someone else's device in my own team is NOT_FOUND too.
	mech := h.user(t, "mech")
	h.member(t, team, mech, "mechanic")
	_, err = h.svc.Push(h.ctx, uid(mech), tid(team), app.PushInput{DeviceID: d.id, Pass: d.pass, Ops: []domain.Operation{op}})
	require.ErrorIs(t, err, domain.ErrNotFound)

	// Nothing was written by any of the above except the pilot's own two operations (the
	// refused car and the accepted note).
	require.Equal(t, "2", h.pg.Exec(t, `SELECT count(*) FROM sync.operation`))
}

func TestPullTombstonesAndCursor(t *testing.T) {
	h := newHarness(t)
	user := h.user(t, "mech")
	team := h.team(t, user)
	d := h.device(t, user, team)
	car := uuid.Must(uuid.NewV7())
	d.push(t, d.op(t, domain.KindCreate, car, nil, `{"make":"K","model":"A"}`))
	d.push(t, d.op(t, domain.KindPatch, car, ver(1), `{"model":"B"}`))

	first, err := h.svc.Pull(h.ctx, uid(user), tid(team), app.PullInput{CatalogVersion: "old"})
	require.NoError(t, err)
	require.Len(t, first.Changes, 1, "two writes to one entity travel as one change")
	require.False(t, first.Changes[0].Deleted)
	var got map[string]any
	require.NoError(t, json.Unmarshal(first.Changes[0].Data, &got))
	require.Equal(t, car.String(), got["id"])
	require.Equal(t, "B", got["model"])
	require.EqualValues(t, 2, got["version"])
	require.Equal(t, "cat-v1", first.CatalogVersion)
	require.NotEmpty(t, first.CatalogSnapshot)

	// Nothing new: the cursor holds and the up-to-date catalogue is not resent.
	idle, err := h.svc.Pull(h.ctx, uid(user), tid(team), app.PullInput{Cursor: first.NextCursor, CatalogVersion: "cat-v1"})
	require.NoError(t, err)
	require.Empty(t, idle.Changes)
	require.Equal(t, first.NextCursor, idle.NextCursor)
	require.Empty(t, idle.CatalogSnapshot)

	d.push(t, d.op(t, domain.KindDelete, car, ver(2), `{}`))
	after, err := h.svc.Pull(h.ctx, uid(user), tid(team), app.PullInput{Cursor: first.NextCursor})
	require.NoError(t, err)
	require.Len(t, after.Changes, 1)
	require.True(t, after.Changes[0].Deleted, "a deletion travels as a tombstone")

	_, err = h.svc.Pull(h.ctx, uid(user), tid(team), app.PullInput{Cursor: "v1.junk"})
	require.ErrorIs(t, err, domain.ErrInvalid)
}

// The bug the (xid, seq) cursor exists for: a transaction that is still open must not let
// a later change be skipped once it commits.
func TestPullDoesNotSkipLateCommits(t *testing.T) {
	h := newHarness(t)
	user := h.user(t, "mech")
	team := h.team(t, user)

	open, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock) // a failed assertion must not leave the transaction (and the pool) hanging
	slow := uuid.Must(uuid.NewV7())
	go func() {
		done <- db.New(h.pool).WithTeamTx(h.ctx, uid(user), tid(team), func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
			if _, err := garage.NewCarApplier().Apply(ctx, tx, syncapply.Op{
				TeamID: tid(team), UserID: uid(user), Kind: syncapply.Create, EntityID: slow,
				Fields: map[string]json.RawMessage{"make": json.RawMessage(`"Slow"`), "model": json.RawMessage(`"S"`)},
			}); err != nil {
				return err
			}
			close(open)
			<-release // keep the transaction open while another one commits
			return nil
		})
	}()
	select {
	case <-open:
	case err := <-done:
		require.NoError(t, err)
		t.Fatal("the slow transaction ended before the test could use it")
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for the slow transaction")
	}

	d := h.device(t, user, team)
	fast := uuid.Must(uuid.NewV7())
	d.push(t, d.op(t, domain.KindCreate, fast, nil, `{"make":"Fast","model":"F"}`)) // commits after slow's row, before slow commits

	during, err := h.svc.Pull(h.ctx, uid(user), tid(team), app.PullInput{})
	require.NoError(t, err)
	require.Empty(t, during.Changes, "nothing is handed out past a transaction that has not finished")

	unblock()
	require.NoError(t, <-done)

	got := map[uuid.UUID]bool{}
	cursor := during.NextCursor
	for range 5 {
		page, err := h.svc.Pull(h.ctx, uid(user), tid(team), app.PullInput{Cursor: cursor})
		require.NoError(t, err)
		for _, c := range page.Changes {
			got[c.EntityID] = true
		}
		cursor = page.NextCursor
		if !page.HasMore {
			break
		}
	}
	require.True(t, got[slow], "the late commit must arrive")
	require.True(t, got[fast])
}

// --- the real garage.car applier ----------------------------------------------

func TestCarApplierRules(t *testing.T) {
	h := newHarness(t)
	user := h.user(t, "mech")
	team := h.team(t, user)
	d := h.device(t, user, team)
	car := uuid.Must(uuid.NewV7())

	create := d.push(t, d.op(t, domain.KindCreate, car, nil,
		`{"make":"Kyosho","model":"MP10","model_year":2024,"nickname":"Red","status":"spare","notes":"n"}`))[0]
	require.Equal(t, domain.StatusApplied, create.Status, create.Reason)
	require.Equal(t, "Kyosho|MP10|2024|Red|spare|1/8|nitro|1", h.pg.Exec(t, fmt.Sprintf(
		`SELECT make||'|'||model||'|'||model_year||'|'||nickname||'|'||status||'|'||scale||'|'||powertrain||'|'||version FROM garage.car WHERE id = '%s'`, car)))

	// The device chose the id, and the creator is the authenticated user, not a field.
	require.Equal(t, user.String(), h.pg.Exec(t, fmt.Sprintf(`SELECT created_by FROM garage.car WHERE id = '%s'`, car)))

	rejected := map[string]string{
		"unknown field (no mass assignment)": `{"make":"K","model":"M","team_id":"` + uuid.NewString() + `"}`,
		"created_by is not a field":          `{"make":"K","model":"M","created_by":"` + uuid.NewString() + `"}`,
		"missing make":                       `{"model":"M"}`,
		"null make":                          `{"make":null,"model":"M"}`,
		"number as string":                   `{"make":"K","model":5}`,
		"year as string":                     `{"make":"K","model":"M","model_year":"2024"}`,
		"year out of range":                  `{"make":"K","model":"M","model_year":1800}`,
		"unknown status":                     `{"make":"K","model":"M","status":"flying"}`,
		"unknown catalogue":                  `{"make":"K","model":"M","scale":"1/99"}`,
		"too long":                           `{"make":"` + strings.Repeat("x", 500) + `","model":"M"}`,
	}
	for name, fields := range rejected {
		r := d.push(t, d.op(t, domain.KindCreate, uuid.Must(uuid.NewV7()), nil, fields))[0]
		require.Equal(t, domain.StatusRejected, r.Status, name)
	}
	require.Equal(t, "1", h.pg.Exec(t, `SELECT count(*) FROM garage.car`), "nothing from the rejected operations was written")

	// The same id again (a different op) is refused, not overwritten.
	dup := d.push(t, d.op(t, domain.KindCreate, car, nil, `{"make":"Other","model":"X"}`))[0]
	require.Equal(t, domain.StatusRejected, dup.Status)
	require.Equal(t, "Kyosho", h.pg.Exec(t, fmt.Sprintf(`SELECT make FROM garage.car WHERE id = '%s'`, car)))

	// null clears an optional field; a missing key leaves it alone.
	require.Equal(t, domain.StatusApplied, d.push(t, d.op(t, domain.KindPatch, car, ver(1), `{"nickname":null}`))[0].Status)
	require.Equal(t, "|MP10", h.pg.Exec(t, fmt.Sprintf(`SELECT COALESCE(nickname,'')||'|'||model FROM garage.car WHERE id = '%s'`, car)))

	// Another team's car does not exist for this device.
	other := h.team(t, user)
	od := h.device(t, user, other)
	r := od.push(t, od.op(t, domain.KindPatch, car, ver(2), `{"model":"Z"}`))[0]
	require.Equal(t, domain.StatusRejected, r.Status)
	require.Equal(t, domain.ReasonNotFound, r.Reason)
	require.Equal(t, "MP10", h.pg.Exec(t, fmt.Sprintf(`SELECT model FROM garage.car WHERE id = '%s'`, car)))

	// Delete needs the current version; a stale one is a conflict for the mechanic.
	stale := d.push(t, d.op(t, domain.KindDelete, car, ver(1), `{}`))[0]
	require.Equal(t, domain.StatusConflict, stale.Status)
	require.Equal(t, "1", h.pg.Exec(t, fmt.Sprintf(`SELECT count(*) FROM garage.car WHERE id = '%s' AND deleted_at IS NULL`, car)))
	require.Equal(t, domain.StatusApplied, d.push(t, d.op(t, domain.KindDelete, car, ver(2), `{}`))[0].Status)
	require.Equal(t, "0", h.pg.Exec(t, fmt.Sprintf(`SELECT count(*) FROM garage.car WHERE id = '%s' AND deleted_at IS NULL`, car)))
}

func TestCarApplierMountedCarCannotBeRetiredOrDeleted(t *testing.T) {
	h := newHarness(t)
	user := h.user(t, "mech")
	team := h.team(t, user)
	d := h.device(t, user, team)
	car, engine := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	require.Equal(t, domain.StatusApplied, d.push(t, d.op(t, domain.KindCreate, car, nil, `{"make":"K","model":"M"}`))[0].Status)

	h.pg.Exec(t, fmt.Sprintf(`INSERT INTO garage.engine (id, team_id, make, model) VALUES ('%s','%s','OS','21')`, engine, team))
	h.pg.Exec(t, fmt.Sprintf(`INSERT INTO garage.engine_mount (team_id, engine_id, car_id, mounted_at) VALUES ('%s','%s','%s', now())`, team, engine, car))

	retire := d.push(t, d.op(t, domain.KindPatch, car, ver(1), `{"status":"retired"}`))[0]
	require.Equal(t, domain.StatusRejected, retire.Status)
	require.Equal(t, domain.ReasonInvalidRef, retire.Reason)
	del := d.push(t, d.op(t, domain.KindDelete, car, ver(1), `{}`))[0]
	require.Equal(t, domain.StatusRejected, del.Status)
	require.Equal(t, domain.ReasonInvalidRef, del.Reason)
	require.Equal(t, "active|1", h.pg.Exec(t, fmt.Sprintf(`SELECT status||'|'||version FROM garage.car WHERE id = '%s' AND deleted_at IS NULL`, car)))
}

// Two devices patch the same car from the same base at the same time: exactly one wins the
// race, the other is merged or becomes a conflict. Neither update is lost or applied twice.
func TestConcurrentPatchesOfTheSameCar(t *testing.T) {
	h := newHarness(t)
	user := h.user(t, "mech")
	team := h.team(t, user)
	a, b := h.device(t, user, team), h.device(t, user, team)
	car := uuid.Must(uuid.NewV7())
	require.Equal(t, domain.StatusApplied, a.push(t, a.op(t, domain.KindCreate, car, nil, `{"make":"K","model":"M"}`))[0].Status)

	opA := a.op(t, domain.KindPatch, car, ver(1), `{"nickname":"A"}`)
	opB := b.op(t, domain.KindPatch, car, ver(1), `{"notes":"B"}`)
	var wg sync.WaitGroup
	results := make([][]app.OpResult, 2)
	for i, f := range []func() []app.OpResult{func() []app.OpResult { return a.push(t, opA) }, func() []app.OpResult { return b.push(t, opB) }} {
		wg.Add(1)
		go func() { defer wg.Done(); results[i] = f() }()
	}
	wg.Wait()

	for _, r := range results {
		require.Contains(t, []domain.Status{domain.StatusApplied, domain.StatusMerged}, r[0].Status, r[0].Reason)
	}
	require.Equal(t, "A|B|3", h.pg.Exec(t, fmt.Sprintf(`SELECT nickname||'|'||notes||'|'||version FROM garage.car WHERE id = '%s'`, car)),
		"both edits survive, one version each")
}
