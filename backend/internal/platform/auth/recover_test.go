package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/auth/principal"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/config"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/ratelimit"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/testutil"
)

// fakeAccounts is an in-memory identity provider account directory.
type fakeAccounts struct {
	mu      sync.Mutex
	users   map[string]string      // email -> subject
	methods map[string]AuthMethods // subject -> credentials
	findErr error
	sendErr error
	sent    []string // subjects that got a registration link
	finds   int
}

func (a *fakeAccounts) FindUserByEmail(_ context.Context, email string) (string, bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.finds++
	if a.findErr != nil {
		return "", false, a.findErr
	}
	s, ok := a.users[email]
	return s, ok, nil
}

func (a *fakeAccounts) AuthMethods(_ context.Context, subject string) (AuthMethods, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.methods[subject], nil
}

func (a *fakeAccounts) SendPasskeyRegistrationLink(_ context.Context, subject string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sendErr != nil {
		return a.sendErr
	}
	a.sent = append(a.sent, subject)
	return nil
}

func (a *fakeAccounts) sentCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.sent)
}

// fakeThrottle counts in memory; err makes every Hit fail.
type fakeThrottle struct {
	mu     sync.Mutex
	counts map[string]int64
	err    error
}

func (t *fakeThrottle) Hit(_ context.Context, key string, max int64, _ time.Duration) (bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.err != nil {
		return false, t.err
	}
	if t.counts == nil {
		t.counts = map[string]int64{}
	}
	t.counts[key]++
	return t.counts[key] <= max, nil
}

type recoverRig struct {
	h        *Handler
	accounts *fakeAccounts
	throttle *fakeThrottle
	audit    *fakeAudit
	logs     *syncLog
}

const (
	passkeyEmail  = "ana@example.com"
	passwordEmail = "rui@example.com"
	unknownEmail  = "ninguem@example.com"
)

func newRecoverRig(t *testing.T) *recoverRig {
	t.Helper()
	keyring, err := NewKeyring([]config.AuthKey{{ID: "k1", Secret: []byte(strings.Repeat("k", 32))}})
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}
	pseudo, err := keyring.EmailPseudonymizer()
	if err != nil {
		t.Fatalf("pseudonymizer: %v", err)
	}
	rig := &recoverRig{
		accounts: &fakeAccounts{
			users:   map[string]string{passkeyEmail: "1001", passwordEmail: "1002"},
			methods: map[string]AuthMethods{"1001": {Passkey: true}, "1002": {Password: true}},
		},
		throttle: &fakeThrottle{},
		audit:    &fakeAudit{},
		logs:     &syncLog{},
	}
	rig.h = &Handler{
		Accounts: rig.accounts, RecoverThrottle: rig.throttle, EmailPseudonyms: pseudo,
		Audit: rig.audit, Log: slog.New(slog.NewJSONHandler(rig.logs, nil)),
		Limiter: fixedLimiter{ratelimit.Allowed}, Now: time.Now,
	}
	return rig
}

func (rig *recoverRig) post(body string) *httptest.ResponseRecorder {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, publicURL+RecoverPath, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = "203.0.113.7:40000"
	w := httptest.NewRecorder()
	rig.h.recoverAccount(w, r)
	return w
}

func (rig *recoverRig) email(addr string) *httptest.ResponseRecorder {
	return rig.post(`{"email":"` + addr + `"}`)
}

// waitFor polls: the work happens after the answer, in the background.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// The answer must not depend on the account: same status, same body, same headers.
func TestRecoverAnswersTheSameWhateverTheAccount(t *testing.T) {
	t.Parallel()
	type result struct {
		code    int
		body    string
		headers http.Header
	}
	get := func(rig *recoverRig, addr string) result {
		w := rig.email(addr)
		return result{w.Code, w.Body.String(), w.Result().Header.Clone()}
	}
	scenarios := map[string]func(*recoverRig) string{
		"account with a passkey":       func(*recoverRig) string { return passkeyEmail },
		"account with only a password": func(*recoverRig) string { return passwordEmail },
		"unknown email":                func(*recoverRig) string { return unknownEmail },
		"identity provider down": func(r *recoverRig) string {
			r.accounts.findErr = errAccountsUnavailable
			return passkeyEmail
		},
		"link refused by the provider": func(r *recoverRig) string {
			r.accounts.sendErr = errAccountsUnavailable
			return passkeyEmail
		},
		"throttle down": func(r *recoverRig) string {
			r.throttle.err = errors.New("valkey down")
			return passkeyEmail
		},
		"recovery off": func(r *recoverRig) string {
			r.h.Accounts = nil
			return passkeyEmail
		},
		"limiter down": func(r *recoverRig) string {
			r.h.Limiter = fixedLimiter{ratelimit.Unavailable}
			return passkeyEmail
		},
	}
	var want *result
	for name, setup := range scenarios {
		rig := newRecoverRig(t)
		got := get(rig, setup(rig))
		if got.code != http.StatusAccepted || got.body != `{"status":"accepted"}`+"\n" {
			t.Errorf("%s: %d %q", name, got.code, got.body)
		}
		if want == nil {
			want = &got
			continue
		}
		if got.code != want.code || got.body != want.body {
			t.Errorf("%s: answer differs: %d %q vs %d %q", name, got.code, got.body, want.code, want.body)
		}
		for k, v := range want.headers {
			if strings.Join(got.headers[k], ",") != strings.Join(v, ",") {
				t.Errorf("%s: header %s differs: %v vs %v", name, k, got.headers[k], v)
			}
		}
		for k := range got.headers {
			if _, ok := want.headers[k]; !ok {
				t.Errorf("%s: extra header %s", name, k)
			}
		}
	}
}

func TestRecoverSendsOnlyToAnAccountWithAPasskey(t *testing.T) {
	t.Parallel()
	rig := newRecoverRig(t)
	rig.email(passwordEmail)
	rig.email(unknownEmail)
	rig.email(passkeyEmail)
	waitFor(t, "the link", func() bool { return rig.accounts.sentCount() == 1 })
	waitFor(t, "three audit rows", func() bool { return len(rig.audit.actions()) == 3 })
	if rig.accounts.sent[0] != "1001" {
		t.Fatalf("sent to %v, want only 1001", rig.accounts.sent)
	}
	got := strings.Join(rig.audit.actions(), ",")
	for _, reason := range []string{"auth.recovery_requested:sent", "auth.recovery_requested:no_passkey", "auth.recovery_requested:no_account"} {
		if !strings.Contains(got, reason) {
			t.Errorf("audit %q lacks %q", got, reason)
		}
	}
}

func TestRecoverNeverRecordsTheEmail(t *testing.T) {
	t.Parallel()
	rig := newRecoverRig(t)
	rig.accounts.findErr = errAccountsUnavailable
	for _, addr := range []string{passkeyEmail, unknownEmail} {
		rig.email(addr)
	}
	waitFor(t, "audit rows", func() bool { return len(rig.audit.actions()) == 2 })
	rig.audit.mu.Lock()
	defer rig.audit.mu.Unlock()
	for _, e := range rig.audit.events {
		if e.ActorKind != ActorSystem || !strings.HasPrefix(e.ActorID, "email:") || len(e.ActorID) != len("email:")+32 {
			t.Errorf("actor %q is not a pseudonym", e.ActorID)
		}
		if strings.Contains(e.ActorID+e.Reason, "@") || strings.Contains(e.ActorID, "example") {
			t.Errorf("the address leaked into the audit trail: %+v", e)
		}
	}
	if rig.audit.events[0].ActorID == rig.audit.events[1].ActorID {
		t.Error("two addresses share a pseudonym")
	}
	if logs := rig.logs.String(); strings.Contains(logs, "@") || strings.Contains(logs, "example.com") {
		t.Errorf("the address leaked into the log: %s", logs)
	}
}

func TestRecoverThrottlesPerEmailPerIPAndGlobally(t *testing.T) {
	t.Parallel()
	t.Run("per email", func(t *testing.T) {
		t.Parallel()
		rig := newRecoverRig(t)
		for i := 0; i < recoverMaxPerEmail+3; i++ {
			if w := rig.email(passkeyEmail); w.Code != http.StatusAccepted {
				t.Fatalf("status %d", w.Code)
			}
		}
		waitFor(t, "all requests worked", func() bool { return len(rig.audit.actions()) == recoverMaxPerEmail+3 })
		if got := rig.accounts.sentCount(); got != recoverMaxPerEmail {
			t.Fatalf("sent %d links, want %d: the inbox of one address is not a mail cannon", got, recoverMaxPerEmail)
		}
		// Another address is not affected.
		rig.email(passwordEmail)
		waitFor(t, "other address", func() bool { return len(rig.audit.actions()) == recoverMaxPerEmail+4 })
	})
	t.Run("per IP", func(t *testing.T) {
		t.Parallel()
		rig := newRecoverRig(t)
		for i := 0; i < recoverMaxPerIP+2; i++ {
			rig.email("u" + strings.Repeat("x", i+1) + "@example.com") // always a new address
		}
		waitFor(t, "all requests worked", func() bool { return len(rig.audit.actions()) == recoverMaxPerIP+2 })
		rig.accounts.mu.Lock()
		finds := rig.accounts.finds
		rig.accounts.mu.Unlock()
		if finds != recoverMaxPerIP {
			t.Fatalf("looked up %d accounts, want %d", finds, recoverMaxPerIP)
		}
	})
	t.Run("global", func(t *testing.T) {
		t.Parallel()
		rig := newRecoverRig(t)
		rig.throttle.mu.Lock()
		rig.throttle.counts = map[string]int64{recoverThrottlePath + "global": recoverMaxGlobal}
		rig.throttle.mu.Unlock()
		rig.email(passkeyEmail)
		waitFor(t, "the request worked", func() bool { return len(rig.audit.actions()) == 1 })
		if rig.accounts.sentCount() != 0 {
			t.Fatal("a link was sent over the global ceiling")
		}
	})
}

func TestRecoverFailsSafely(t *testing.T) {
	t.Parallel()
	t.Run("throttle down: nothing is sent", func(t *testing.T) {
		t.Parallel()
		rig := newRecoverRig(t)
		rig.throttle.err = errors.New("valkey down")
		rig.email(passkeyEmail)
		waitFor(t, "the warning", func() bool { return strings.Contains(rig.logs.String(), "throttle_unavailable") })
		if rig.accounts.sentCount() != 0 || rig.accounts.finds != 0 {
			t.Fatal("the account was touched without a working throttle")
		}
	})
	t.Run("limiter down: dropped", func(t *testing.T) {
		t.Parallel()
		rig := newRecoverRig(t)
		rig.h.Limiter = fixedLimiter{ratelimit.Unavailable}
		rig.email(passkeyEmail)
		time.Sleep(50 * time.Millisecond)
		if rig.accounts.finds != 0 || len(rig.audit.actions()) != 0 {
			t.Fatal("a request went on although the limiter is down")
		}
	})
	t.Run("flood from one address: 429", func(t *testing.T) {
		t.Parallel()
		rig := newRecoverRig(t)
		rig.h.Limiter = fixedLimiter{ratelimit.RateLimited}
		if w := rig.email(passkeyEmail); w.Code != http.StatusTooManyRequests {
			t.Fatalf("status %d", w.Code)
		}
	})
	t.Run("no limiter: not served", func(t *testing.T) {
		t.Parallel()
		rig := newRecoverRig(t)
		rig.h.Limiter = nil
		if w := rig.email(passkeyEmail); w.Code != http.StatusServiceUnavailable {
			t.Fatalf("status %d", w.Code)
		}
	})
	t.Run("recovery off: audited, nothing sent", func(t *testing.T) {
		t.Parallel()
		rig := newRecoverRig(t)
		rig.h.Accounts = nil
		rig.email(passkeyEmail)
		waitFor(t, "audit", func() bool { return strings.Contains(strings.Join(rig.audit.actions(), ","), "disabled") })
	})
	t.Run("audit down does not stop the answer", func(t *testing.T) {
		t.Parallel()
		rig := newRecoverRig(t)
		rig.audit.err = errors.New("db down")
		if w := rig.email(passkeyEmail); w.Code != http.StatusAccepted {
			t.Fatalf("status %d", w.Code)
		}
		waitFor(t, "the link", func() bool { return rig.accounts.sentCount() == 1 })
	})
}

// Work in the background is bounded: with every slot taken, a request is dropped, not queued.
func TestRecoverDropsWhenBusy(t *testing.T) {
	t.Parallel()
	rig := newRecoverRig(t)
	for i := 0; i < recoverMaxWorkers; i++ {
		if !rig.h.recoverySlots.acquire() {
			t.Fatalf("slot %d refused too early", i)
		}
	}
	rig.email(passkeyEmail)
	waitFor(t, "the drop", func() bool { return strings.Contains(strings.Join(rig.audit.actions(), ","), "dropped_busy") })
	if rig.accounts.finds != 0 {
		t.Fatal("a request worked with no free slot")
	}
}

func TestRecoverRejectsMalformedRequests(t *testing.T) {
	t.Parallel()
	rig := newRecoverRig(t)
	tests := map[string]string{
		"not json":         `ana@example.com`,
		"unknown field":    `{"email":"ana@example.com","admin":true}`,
		"two objects":      `{"email":"ana@example.com"}{"email":"b@example.com"}`,
		"empty":            `{"email":""}`,
		"no at":            `{"email":"ana.example.com"}`,
		"display name":     `{"email":"Ana <ana@example.com>"}`,
		"no dot in domain": `{"email":"ana@localhost"}`,
		"comment":          `{"email":"ana(x)@example.com"}`,
		"two addresses":    `{"email":"a@example.com,b@example.com"}`,
		"space inside":     `{"email":"a b@example.com"}`,
		"too long":         `{"email":"` + strings.Repeat("a", 250) + `@example.com"}`,
		"body too large":   `{"email":"` + strings.Repeat("a", 2000) + `"}`,
		"number":           `{"email":1}`,
	}
	for name, body := range tests {
		if w := rig.post(body); w.Code != http.StatusBadRequest && w.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("%s: status %d, want 400", name, w.Code)
		}
	}
	// Not JSON by content type.
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, publicURL+RecoverPath, strings.NewReader(`{"email":"ana@example.com"}`))
	r.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	rig.h.recoverAccount(w, r)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain: status %d", w.Code)
	}
	time.Sleep(30 * time.Millisecond)
	if rig.accounts.finds != 0 {
		t.Fatal("a malformed request reached the account lookup")
	}
}

func TestNormalizeEmail(t *testing.T) {
	t.Parallel()
	if got, ok := normalizeEmail("  Ana@Example.COM "); !ok || got != "ana@example.com" {
		t.Fatalf("got %q %v", got, ok)
	}
}

func FuzzNormalizeEmail(f *testing.F) {
	for _, s := range []string{"ana@example.com", "", "a@b", "<a@b.c>", "a@b.c, d@e.f", "\"a b\"@c.d", "a@[1.2.3.4]", strings.Repeat("a", 300) + "@b.c"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out, ok := normalizeEmail(in)
		if !ok {
			return
		}
		// What is accepted is a single lowercase plain address of bounded size.
		if len(out) > recoverMaxEmail || out != strings.ToLower(out) || strings.ContainsAny(out, " <>(),;\"\\\r\n\t") || strings.Count(out, "@") != 1 {
			t.Fatalf("accepted %q as %q", in, out)
		}
		if again, ok := normalizeEmail(out); !ok || again != out {
			t.Fatalf("not idempotent: %q -> %q -> %q", in, out, again)
		}
	})
}

// recovery_ready is false only for a passkey-only account without recovery codes.
func TestRecoveryReady(t *testing.T) {
	t.Parallel()
	p := principal.Principal{UserID: "u1", Subject: "1001"}
	tests := []struct {
		name    string
		methods AuthMethods
		codes   int
		want    *bool
	}{
		{"passkey only, no codes", AuthMethods{Passkey: true}, 0, ptr(false)},
		{"passkey only, with codes", AuthMethods{Passkey: true}, 4, ptr(true)},
		{"passkey and password", AuthMethods{Passkey: true, Password: true}, 0, ptr(true)},
		{"password only", AuthMethods{Password: true}, 0, ptr(true)},
		{"nothing yet", AuthMethods{}, 0, ptr(true)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rig := newRecoverRig(t)
			rig.accounts.methods["1001"] = tt.methods
			rig.h.RecoveryCodes = &fakeRecoveryStore{}
			if tt.codes > 0 {
				hashes := make([][]byte, tt.codes)
				for i := range hashes {
					hashes[i] = []byte{byte(i)}
				}
				_ = rig.h.RecoveryCodes.Replace(context.Background(), "u1", "k1", hashes)
			}
			got := rig.h.recoveryReady(context.Background(), p)
			if got == nil || *got != *tt.want {
				t.Fatalf("recovery_ready = %v, want %v", deref(got), *tt.want)
			}
		})
	}
	t.Run("unknown when the provider fails", func(t *testing.T) {
		t.Parallel()
		rig := newRecoverRig(t)
		rig.h.RecoveryCodes = &fakeRecoveryStore{}
		rig.h.Accounts = failingAccounts{rig.accounts}
		if got := rig.h.recoveryReady(context.Background(), p); got != nil {
			t.Fatalf("got %v, want it left out", *got)
		}
	})
	t.Run("unknown when the feature is off", func(t *testing.T) {
		t.Parallel()
		rig := newRecoverRig(t)
		rig.h.Accounts = nil
		if got := rig.h.recoveryReady(context.Background(), p); got != nil {
			t.Fatal("answered with the feature off")
		}
	})
	t.Run("the answer is cached and generating codes clears it", func(t *testing.T) {
		t.Parallel()
		rig := newRecoverRig(t)
		rig.accounts.methods["1001"] = AuthMethods{Passkey: true}
		rig.h.RecoveryCodes = &fakeRecoveryStore{}
		if got := rig.h.recoveryReady(context.Background(), p); got == nil || *got {
			t.Fatal("expected false")
		}
		_ = rig.h.RecoveryCodes.Replace(context.Background(), "u1", "k1", [][]byte{{1}})
		if got := rig.h.recoveryReady(context.Background(), p); got == nil || *got {
			t.Fatal("expected the cached false")
		}
		rig.h.readiness.forget("u1")
		if got := rig.h.recoveryReady(context.Background(), p); got == nil || !*got {
			t.Fatal("expected true after the cache was cleared")
		}
	})
}

type failingAccounts struct{ *fakeAccounts }

func (failingAccounts) AuthMethods(context.Context, string) (AuthMethods, error) {
	return AuthMethods{}, errAccountsUnavailable
}

func ptr(b bool) *bool { return &b }

func deref(b *bool) any {
	if b == nil {
		return nil
	}
	return *b
}

func TestEmailPseudonymizer(t *testing.T) {
	t.Parallel()
	mk := func(secret string) *EmailPseudonymizer {
		k, err := NewKeyring([]config.AuthKey{{ID: "k1", Secret: []byte(strings.Repeat(secret, 32))}})
		if err != nil {
			t.Fatal(err)
		}
		p, err := k.EmailPseudonymizer()
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	a, b := mk("a"), mk("b")
	first, again := a.Pseudonym("x@y.z"), a.Pseudonym("x@y.z")
	if first != again {
		t.Fatal("not deterministic")
	}
	if a.Pseudonym("x@y.z") == a.Pseudonym("x2@y.z") {
		t.Fatal("collision between two addresses")
	}
	if a.Pseudonym("x@y.z") == b.Pseudonym("x@y.z") {
		t.Fatal("the pseudonym does not depend on the key")
	}
	if got := a.Pseudonym("x@y.z"); len(got) != 32 || strings.Contains(got, "x") && strings.Contains(got, "@") {
		t.Fatalf("unexpected shape %q", got)
	}
}

// The answer goes out before the account is touched: a slow identity provider changes nothing
// the caller can measure (no timing difference between a known and an unknown address).
func TestRecoverAnswersBeforeTheWork(t *testing.T) {
	t.Parallel()
	rig := newRecoverRig(t)
	release := make(chan struct{})
	rig.h.Accounts = blockingAccounts{fakeAccounts: rig.accounts, release: release}
	start := time.Now()
	w := rig.email(passkeyEmail)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("the answer waited %v for the identity provider", elapsed)
	}
	if w.Code != http.StatusAccepted {
		t.Fatalf("status %d", w.Code)
	}
	close(release)
	waitFor(t, "the link", func() bool { return rig.accounts.sentCount() == 1 })
}

type blockingAccounts struct {
	*fakeAccounts
	release chan struct{}
}

func (b blockingAccounts) FindUserByEmail(ctx context.Context, email string) (string, bool, error) {
	<-b.release
	return b.fakeAccounts.FindUserByEmail(ctx, email)
}

func FuzzRecoverBody(f *testing.F) {
	for _, s := range []string{`{"email":"ana@example.com"}`, `{}`, `[]`, `null`, `{"email":null}`, "\x00", `{"email":"` + strings.Repeat("é", 300) + `"}`} {
		f.Add(s)
	}
	rig := newRecoverRigForFuzz()
	f.Fuzz(func(t *testing.T, body string) {
		w := rig.post(body)
		switch w.Code {
		case http.StatusAccepted, http.StatusBadRequest, http.StatusRequestEntityTooLarge:
		default:
			t.Fatalf("unexpected status %d", w.Code)
		}
		if strings.Contains(w.Body.String(), "@") {
			t.Fatalf("the answer echoes an address: %q", w.Body.String())
		}
	})
}

func newRecoverRigForFuzz() *recoverRig {
	rig := &recoverRig{accounts: &fakeAccounts{}, throttle: &fakeThrottle{}, audit: &fakeAudit{}, logs: &syncLog{}}
	rig.h = &Handler{
		Accounts: rig.accounts, RecoverThrottle: rig.throttle, Audit: rig.audit,
		Log: slog.New(slog.NewJSONHandler(rig.logs, nil)), Limiter: fixedLimiter{ratelimit.Allowed}, Now: time.Now,
	}
	return rig
}

func TestValkeyThrottler(t *testing.T) {
	t.Parallel()
	v := testutil.StartValkey(t)
	th := NewValkeyThrottler(newClient(t, v.Addr, v.Username, v.Password))
	ctx := context.Background()
	const key = recoverThrottlePath + "test:a"
	for i := 1; i <= 3; i++ {
		if within, err := th.Hit(ctx, key, 3, time.Hour); err != nil || !within {
			t.Fatalf("hit %d: within=%v err=%v", i, within, err)
		}
	}
	if within, err := th.Hit(ctx, key, 3, time.Hour); err != nil || within {
		t.Fatalf("4th hit: within=%v err=%v, want refused", within, err)
	}
	if within, err := th.Hit(ctx, recoverThrottlePath+"test:b", 3, time.Hour); err != nil || !within {
		t.Fatalf("another key shares the count: within=%v err=%v", within, err)
	}
	// The window ends: the count starts again.
	const short = recoverThrottlePath + "test:short"
	if within, _ := th.Hit(ctx, short, 1, 150*time.Millisecond); !within {
		t.Fatal("first hit refused")
	}
	if within, _ := th.Hit(ctx, short, 1, 150*time.Millisecond); within {
		t.Fatal("second hit inside the window accepted")
	}
	time.Sleep(300 * time.Millisecond)
	if within, _ := th.Hit(ctx, short, 1, 150*time.Millisecond); !within {
		t.Fatal("hit after the window refused")
	}
	// A key outside pitbox:rl:rec: is never written (the Valkey ACL is wider than this feature).
	if _, err := th.Hit(ctx, "pitbox:sess:x", 3, time.Hour); err == nil {
		t.Fatal("a key outside the prefix was accepted")
	}
}

func TestSessionReportsRecoveryReady(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	f.h.Accounts = &fakeAccounts{methods: map[string]AuthMethods{"1001": {Passkey: true}}}
	cookie := f.signIn("1001")
	get := func() map[string]any {
		w := f.do(http.MethodGet, SessionPath, []*http.Cookie{cookie}, map[string]string{"Sec-Fetch-Site": "same-origin"})
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("json: %v", err)
		}
		return out
	}
	if got := get()["recovery_ready"]; got != false {
		t.Fatalf("recovery_ready = %v, want false for a passkey-only account without codes", got)
	}
	f.h.Accounts = nil
	f.h.readiness.forget(get()["user_id"].(string))
	if _, present := get()["recovery_ready"]; present {
		t.Fatal("recovery_ready is present with the feature off")
	}
}
