package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/valkey-io/valkey-go"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/testutil"
)

type harness struct {
	t     *testing.T
	store *Store
	app   valkey.Client // as the application user
	admin valkey.Client // as the default user, to look at what was written
	now   *time.Time
}

func newClient(t *testing.T, addr, user, password string) valkey.Client {
	t.Helper()
	c, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{addr}, Username: user, Password: password, DisableCache: true,
	})
	if err != nil {
		t.Fatalf("valkey client: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

func newHarness(t *testing.T, idle, absolute time.Duration) *harness {
	t.Helper()
	v := testutil.StartValkey(t)
	now := t0
	h := &harness{
		t: t, now: &now,
		app:   newClient(t, v.Addr, v.Username, v.Password),
		admin: newClient(t, v.Addr, "default", v.AdminPassword),
	}
	h.store = NewStore(h.app, sealer(t, PurposeSessionToken, key("k1", 1)), idle, absolute, func() time.Time { return *h.now })
	return h
}

func (h *harness) create(sub, sid string) (string, Session) {
	h.t.Helper()
	token, err := h.store.Create(context.Background(), Session{
		UserID: "u-" + sub, Subject: sub, SessionID: sid, AMR: []string{"pwd", "mfa"},
		AuthTime: t0.Add(-time.Minute), AccessExpires: t0.Add(10 * time.Minute),
		RefreshToken: "refresh-" + sub, IDToken: "idtoken-" + sub,
	})
	if err != nil {
		h.t.Fatalf("Create: %v", err)
	}
	s, err := h.store.Get(context.Background(), token)
	if err != nil {
		h.t.Fatalf("Get: %v", err)
	}
	return token, s
}

func (h *harness) adminKeys() []string {
	h.t.Helper()
	keys, err := h.admin.Do(context.Background(), h.admin.B().Keys().Pattern("*").Build()).AsStrSlice()
	if err != nil {
		h.t.Fatalf("KEYS: %v", err)
	}
	return keys
}

func TestSessionRoundTripAndNothingSensitiveInTheStore(t *testing.T) {
	t.Parallel()
	h := newHarness(t, time.Hour, 24*time.Hour)
	token, got := h.create("ana", "sid-1")

	if got.UserID != "u-ana" || got.Subject != "ana" || got.SessionID != "sid-1" || got.RefreshToken != "refresh-ana" ||
		got.IDToken != "idtoken-ana" || len(got.AMR) != 2 || !got.CreatedAt.Equal(t0) || !got.AccessExpires.Equal(t0.Add(10*time.Minute)) {
		t.Errorf("round trip changed the session: %+v", got)
	}
	if got.Ref() != hashHex(token) || got.Ref() == token {
		t.Error("Ref must be the hash of the cookie, never the cookie")
	}

	// Look at everything in the store: the cookie value, the refresh token and the ID token
	// must not appear anywhere (keys or values), only their hash / sealed forms.
	ctx := context.Background()
	for _, k := range h.adminKeys() {
		if strings.Contains(k, token) {
			t.Errorf("a key contains the cookie value: %s", k)
		}
		typ, _ := h.admin.Do(ctx, h.admin.B().Type().Key(k).Build()).ToString()
		var dump string
		switch typ {
		case "hash":
			m, _ := h.admin.Do(ctx, h.admin.B().Hgetall().Key(k).Build()).AsStrMap()
			for f, v := range m {
				dump += f + "=" + v + ";"
			}
		case "set":
			m, _ := h.admin.Do(ctx, h.admin.B().Smembers().Key(k).Build()).AsStrSlice()
			dump = strings.Join(m, ";")
		default:
			dump, _ = h.admin.Do(ctx, h.admin.B().Get().Key(k).Build()).ToString()
		}
		for _, secret := range []string{token, "refresh-ana", "idtoken-ana", `"ana@`} {
			if strings.Contains(dump, secret) {
				t.Errorf("key %s leaks %q", k, secret)
			}
		}
	}
}

func TestIndexesAreMaintained(t *testing.T) {
	t.Parallel()
	h := newHarness(t, time.Hour, 24*time.Hour)
	ctx := context.Background()
	_, s := h.create("ana", "sid-1")

	userIdx := sessPrefix + "u:" + hashHex("ana")
	sidIdx := sessPrefix + "s:" + hashHex("sid-1")
	if ok, _ := h.admin.Do(ctx, h.admin.B().Sismember().Key(userIdx).Member(s.Ref()).Build()).AsBool(); !ok {
		t.Error("the user index misses the session")
	}
	if got, _ := h.admin.Do(ctx, h.admin.B().Get().Key(sidIdx).Build()).ToString(); got != s.Ref() {
		t.Errorf("sid index = %q", got)
	}
	if err := h.store.Delete(ctx, s); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := h.store.Delete(ctx, s); err != nil {
		t.Errorf("deleting twice must not fail: %v", err)
	}
	if left := h.adminKeys(); len(left) > 1 || (len(left) == 1 && left[0] != userIdx) {
		t.Errorf("keys left after delete: %v", left)
	}
	if ok, _ := h.admin.Do(ctx, h.admin.B().Sismember().Key(userIdx).Member(s.Ref()).Build()).AsBool(); ok {
		t.Error("the user index still lists the session")
	}
}

func TestGetRefusesWhatIsNotASession(t *testing.T) {
	t.Parallel()
	h := newHarness(t, time.Hour, 24*time.Hour)
	token, _ := h.create("ana", "")
	for name, tok := range map[string]string{
		"empty":         "",
		"too short":     "abc",
		"unknown":       strings.Repeat("A", 43),
		"bad alphabet":  strings.Repeat("A", 42) + "=",
		"one char off":  token[:42] + map[bool]string{true: "A", false: "B"}[token[42] != 'A'],
		"the ref":       hashHex(token)[:43],
		"with newline":  strings.Repeat("A", 42) + "\n",
		"key injection": "pitbox:sess:" + strings.Repeat("a", 31),
	} {
		if _, err := h.store.Get(context.Background(), tok); !errors.Is(err, ErrNoSession) {
			t.Errorf("%s: Get = %v, want ErrNoSession", name, err)
		}
	}
}

func TestAbsoluteTimeout(t *testing.T) {
	t.Parallel()
	h := newHarness(t, time.Hour, 3*time.Hour)
	token, _ := h.create("ana", "sid")

	*h.now = t0.Add(3*time.Hour - time.Second)
	if _, err := h.store.Get(context.Background(), token); err != nil {
		t.Fatalf("one second before the limit: %v", err)
	}
	*h.now = t0.Add(3 * time.Hour)
	if _, err := h.store.Get(context.Background(), token); !errors.Is(err, ErrNoSession) {
		t.Fatalf("at the limit: %v", err)
	}
	for _, k := range h.adminKeys() {
		if k == sessPrefix+hashHex(token) {
			t.Error("an expired session must be deleted, not just refused")
		}
	}
}

func TestIdleTimeoutIsTheKeyTTLAndSlides(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 10*time.Minute, 24*time.Hour)
	ctx := context.Background()
	token, s := h.create("ana", "")
	key := sessPrefix + s.Ref()

	ttl := func() time.Duration {
		ms, err := h.admin.Do(ctx, h.admin.B().Pttl().Key(key).Build()).AsInt64()
		if err != nil {
			t.Fatalf("PTTL: %v", err)
		}
		return time.Duration(ms) * time.Millisecond
	}
	if d := ttl(); d < 9*time.Minute || d > 10*time.Minute {
		t.Fatalf("initial ttl = %s, want about the idle timeout", d)
	}
	// Simulate time passing in the store: 3 minutes left. A Get pushes it back to the idle timeout.
	if err := h.admin.Do(ctx, h.admin.B().Pexpire().Key(key).Milliseconds((3 * time.Minute).Milliseconds()).Build()).Error(); err != nil {
		t.Fatalf("PEXPIRE: %v", err)
	}
	if _, err := h.store.Get(ctx, token); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if d := ttl(); d < 9*time.Minute {
		t.Errorf("the idle timeout did not slide: ttl = %s", d)
	}
	// The sliding window never goes past the absolute limit.
	*h.now = t0.Add(24*time.Hour - 2*time.Minute)
	_ = h.admin.Do(ctx, h.admin.B().Pexpire().Key(key).Milliseconds((30 * time.Second).Milliseconds()).Build()).Error()
	if _, err := h.store.Get(ctx, token); err != nil {
		t.Fatalf("Get near the limit: %v", err)
	}
	if d := ttl(); d > 2*time.Minute+time.Second {
		t.Errorf("ttl %s goes past the absolute limit (2 minutes away)", d)
	}
}

func TestIdleTimeoutExpiresForReal(t *testing.T) {
	t.Parallel()
	// A real (short) idle timeout: the session works right after creation and is gone once
	// the key's TTL runs out. Polling with a generous bound, not a fixed sleep, so a slow
	// machine cannot make it flaky in either direction.
	h := newHarness(t, 4*time.Second, time.Hour)
	token, _ := h.create("ana", "")
	deadline := time.Now().Add(15 * time.Second)
	for {
		_, err := h.store.Get(context.Background(), token)
		if errors.Is(err, ErrNoSession) {
			return
		}
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("the session did not expire after the idle timeout")
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func TestUpdateIsCompareAndSet(t *testing.T) {
	t.Parallel()
	h := newHarness(t, time.Hour, 24*time.Hour)
	ctx := context.Background()
	token, first := h.create("ana", "sid")
	second, err := h.store.Get(ctx, token) // another request holding the same version
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	first.RefreshToken, first.AccessExpires = "refresh-new", t0.Add(20*time.Minute)
	if err := h.store.Update(ctx, first); err != nil {
		t.Fatalf("first Update: %v", err)
	}
	second.RefreshToken = "refresh-lost-update"
	if err := h.store.Update(ctx, second); !errors.Is(err, ErrConflict) {
		t.Fatalf("a stale Update = %v, want ErrConflict", err)
	}
	got, _ := h.store.Get(ctx, token)
	if got.RefreshToken != "refresh-new" || !got.AccessExpires.Equal(t0.Add(20*time.Minute)) || got.generation != 2 {
		t.Errorf("the winner's write was not kept: %+v", got)
	}
	// A fresh read can update again.
	got.RefreshToken = "refresh-newer"
	if err := h.store.Update(ctx, got); err != nil {
		t.Errorf("Update after a fresh read: %v", err)
	}
}

// A record copied from one session into another must not open: the sealed tokens are bound
// to the session they belong to.
func TestSealedTokensAreBoundToTheirSession(t *testing.T) {
	t.Parallel()
	h := newHarness(t, time.Hour, 24*time.Hour)
	ctx := context.Background()
	_, a := h.create("ana", "")
	tokenB, b := h.create("bruno", "")

	data, err := h.admin.Do(ctx, h.admin.B().Hget().Key(sessPrefix+a.Ref()).Field("d").Build()).ToString()
	if err != nil {
		t.Fatalf("HGET: %v", err)
	}
	if err := h.admin.Do(ctx, h.admin.B().Hset().Key(sessPrefix+b.Ref()).FieldValue().FieldValue("d", data).Build()).Error(); err != nil {
		t.Fatalf("HSET: %v", err)
	}
	if _, err := h.store.Get(ctx, tokenB); !errors.Is(err, ErrNoSession) {
		t.Fatalf("a record swapped between sessions opened: %v", err)
	}
}

func TestSessionKeyRotation(t *testing.T) {
	t.Parallel()
	h := newHarness(t, time.Hour, 24*time.Hour)
	ctx := context.Background()
	token, _ := h.create("ana", "")

	rotated := NewStore(h.app, sealer(t, PurposeSessionToken, key("k2", 2), key("k1", 1)), time.Hour, 24*time.Hour, func() time.Time { return *h.now })
	s, err := rotated.Get(ctx, token)
	if err != nil || s.RefreshToken != "refresh-ana" {
		t.Fatalf("a session sealed with the old key must still open: %v", err)
	}
	if err := rotated.Update(ctx, s); err != nil {
		t.Fatalf("Update: %v", err)
	}
	onlyNew := NewStore(h.app, sealer(t, PurposeSessionToken, key("k2", 2)), time.Hour, 24*time.Hour, func() time.Time { return *h.now })
	if _, err := onlyNew.Get(ctx, token); err != nil {
		t.Errorf("after Update the session uses the new key: %v", err)
	}

	token2, _ := h.create("bruno", "") // sealed with k1
	if _, err := onlyNew.Get(ctx, token2); !errors.Is(err, ErrNoSession) {
		t.Errorf("a session sealed with a retired key must be refused: %v", err)
	}
}

func TestRefreshLock(t *testing.T) {
	t.Parallel()
	h := newHarness(t, time.Hour, 24*time.Hour)
	ctx := context.Background()
	_, s := h.create("ana", "")

	l1, err := h.store.TryLockRefresh(ctx, s)
	if err != nil || l1 == nil {
		t.Fatalf("first lock: %v, %v", l1, err)
	}
	if l2, err := h.store.TryLockRefresh(ctx, s); err != nil || l2 != nil {
		t.Fatalf("second lock while held = %v, %v; want nil", l2, err)
	}
	// Another session is independent.
	_, other := h.create("bruno", "")
	if lo, err := h.store.TryLockRefresh(ctx, other); err != nil || lo == nil {
		t.Errorf("a different session must lock independently: %v", err)
	}

	// After our lock expired and someone else took it, releasing ours must not free theirs.
	key := lockPrefix + s.Ref()
	_ = h.admin.Do(ctx, h.admin.B().Set().Key(key).Value("someone-else").Build()).Error()
	if err := l1.Release(ctx); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if v, _ := h.admin.Do(ctx, h.admin.B().Get().Key(key).Build()).ToString(); v != "someone-else" {
		t.Error("Release deleted a lock owned by someone else")
	}
	_ = h.admin.Do(ctx, h.admin.B().Del().Key(key).Build()).Error()
	if l3, err := h.store.TryLockRefresh(ctx, s); err != nil || l3 == nil {
		t.Errorf("lock after release: %v, %v", l3, err)
	} else if err := l3.Release(ctx); err != nil {
		t.Errorf("Release: %v", err)
	}
	// The lock has a TTL, so a crashed holder cannot block a session forever.
	l4, _ := h.store.TryLockRefresh(ctx, s)
	if ttl, _ := h.admin.Do(ctx, h.admin.B().Pttl().Key(lockPrefix+s.Ref()).Build()).AsInt64(); ttl <= 0 || ttl > RefreshLockTTL.Milliseconds() {
		t.Errorf("lock ttl = %dms", ttl)
	}
	_ = l4.Release(ctx)
}

func TestOnlyOneOfFiftyGetsTheRefreshLock(t *testing.T) {
	t.Parallel()
	h := newHarness(t, time.Hour, 24*time.Hour)
	_, s := h.create("ana", "")

	var winners atomic.Int32
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l, err := h.store.TryLockRefresh(context.Background(), s)
			if err != nil {
				t.Errorf("TryLockRefresh: %v", err)
				return
			}
			if l != nil {
				winners.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := winners.Load(); got != 1 {
		t.Fatalf("%d of 50 concurrent requests got the lock, want exactly 1", got)
	}
}

// The ACL of the application user (from the compose file) is part of the design: it must
// not be able to read or write outside its prefixes, nor run dangerous commands, while
// everything the store needs works (every test above runs as that user).
func TestApplicationUserIsConfinedByTheACL(t *testing.T) {
	t.Parallel()
	h := newHarness(t, time.Hour, 24*time.Hour)
	ctx := context.Background()
	_ = h.admin.Do(ctx, h.admin.B().Set().Key("outside").Value("x").Build()).Error()
	h.create("ana", "")

	for name, cmd := range map[string]valkey.Completed{
		"read a key outside the prefixes":  h.app.B().Get().Key("outside").Build(),
		"write a key outside the prefixes": h.app.B().Set().Key("outside").Value("y").Build(),
		"KEYS":                             h.app.B().Keys().Pattern("*").Build(),
		"FLUSHALL":                         h.app.B().Flushall().Build(),
		"CONFIG GET":                       h.app.B().ConfigGet().Parameter("save").Build(),
		"ACL LIST":                         h.app.B().AclList().Build(),
		"PUBLISH":                          h.app.B().Publish().Channel("c").Message("m").Build(),
		"SCRIPT FLUSH":                     h.app.B().ScriptFlush().Build(),
	} {
		if err := h.app.Do(ctx, cmd).Error(); err == nil || !strings.Contains(err.Error(), "NOPERM") {
			t.Errorf("%s: expected NOPERM, got %v", name, err)
		}
	}
	// A script cannot reach outside either, even with a key it does not declare.
	err := h.app.Do(ctx, h.app.B().Eval().Script("return redis.call('GET','outside')").Numkeys(0).Build()).Error()
	if err == nil {
		t.Error("a script read a key outside the prefixes")
	}
	if v, _ := h.admin.Do(ctx, h.admin.B().Get().Key("outside").Build()).ToString(); v != "x" {
		t.Errorf("the outside key was modified: %q", v)
	}
}
