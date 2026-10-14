package auth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
)

type authHarness struct {
	*flowHarness
	a *Authenticator
}

func newAuthHarness(t *testing.T) *authHarness {
	t.Helper()
	f := newFlowHarness(t)
	if _, err := f.h.Provider.get(); err != nil {
		t.Fatalf("provider: %v", err)
	}
	return &authHarness{flowHarness: f, a: &Authenticator{
		Provider: f.h.Provider, Store: f.store, Audit: f.audit, Log: f.h.Log, Now: f.h.Now,
	}}
}

// signedIn returns a session for "ana" created by a real login through the handlers.
func (h *authHarness) signedIn() (token string, sess Session) {
	h.t.Helper()
	ck := h.signIn("ana")
	s, err := h.store.Get(context.Background(), ck.Value)
	if err != nil {
		h.t.Fatalf("Get: %v", err)
	}
	return ck.Value, s
}

func TestAuthenticateFreshSessionDoesNotRefresh(t *testing.T) {
	t.Parallel()
	h := newAuthHarness(t)
	token, sess := h.signedIn()
	base := h.idp.tokenN.Load()

	p, err := h.a.Authenticate(context.Background(), token)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if p.UserID != "user-ana" || p.Subject != "ana" || p.SessionRef != sess.Ref() || len(p.AMR) != 2 || !p.AuthTime.Equal(sess.AuthTime) {
		t.Errorf("principal = %+v", p)
	}
	if h.idp.tokenN.Load() != base {
		t.Error("a valid access token must not trigger a call to the identity provider")
	}
}

func TestAuthenticateRefusesWhatIsNotASession(t *testing.T) {
	t.Parallel()
	h := newAuthHarness(t)
	for name, tok := range map[string]string{"empty": "", "garbage": "garbage", "unknown": strings.Repeat("A", 43)} {
		if _, err := h.a.Authenticate(context.Background(), tok); !errors.Is(err, ErrUnauthenticated) {
			t.Errorf("%s: %v, want ErrUnauthenticated", name, err)
		}
	}
}

func TestAuthenticateRefreshesAnExpiredAccessToken(t *testing.T) {
	t.Parallel()
	h := newAuthHarness(t)
	token, before := h.signedIn()

	*h.now = h.now.Add(11 * time.Minute) // the 10-minute access token expired
	p, err := h.a.Authenticate(context.Background(), token)
	if err != nil || p.UserID != "user-ana" {
		t.Fatalf("Authenticate: %+v, %v", p, err)
	}
	if got := h.idp.refreshN.Load(); got != 1 {
		t.Fatalf("refresh grants = %d, want 1", got)
	}
	after, err := h.store.Get(context.Background(), token)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.RefreshToken == before.RefreshToken || !strings.HasPrefix(after.RefreshToken, "rt-") {
		t.Error("the rotated refresh token was not stored")
	}
	if !after.AccessExpires.Equal(h.now.Add(10 * time.Minute)) {
		t.Errorf("AccessExpires = %s", after.AccessExpires)
	}
	if after.generation != before.generation+1 {
		t.Errorf("generation %d -> %d", before.generation, after.generation)
	}
	// The next request uses the new token: no second refresh.
	if _, err := h.a.Authenticate(context.Background(), token); err != nil || h.idp.refreshN.Load() != 1 {
		t.Errorf("second request: %v, refreshes = %d", err, h.idp.refreshN.Load())
	}
	// And the lock was released.
	if l, _ := h.store.TryLockRefresh(context.Background(), after); l == nil {
		t.Error("the refresh lock was left held")
	}
}

// The reason for the lock: fifty simultaneous requests with an expired access token must
// make one refresh, not fifty. A second use of the same (rotated) refresh token would be
// rejected by the provider and sign the user out for nothing.
func TestFiftyConcurrentRequestsMakeOneRefresh(t *testing.T) {
	t.Parallel()
	h := newAuthHarness(t)
	token, _ := h.signedIn()
	*h.now = h.now.Add(11 * time.Minute)
	h.idp.refreshDelay = 400 * time.Millisecond // the winner holds the lock while the others arrive

	var ok, refused atomic.Int32
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := h.a.Authenticate(context.Background(), token); err != nil {
				refused.Add(1)
				t.Errorf("Authenticate: %v", err)
				return
			}
			ok.Add(1)
		}()
	}
	wg.Wait()
	if got := h.idp.refreshN.Load(); got != 1 {
		t.Fatalf("refresh grants = %d, want exactly 1", got)
	}
	if ok.Load() != 50 || refused.Load() != 0 {
		t.Fatalf("ok = %d, refused = %d: nobody may be signed out", ok.Load(), refused.Load())
	}
	if _, err := h.store.Get(context.Background(), token); err != nil {
		t.Errorf("the session must survive: %v", err)
	}
}

func TestAuthenticateEndsTheSessionWhenTheProviderRefusesIt(t *testing.T) {
	t.Parallel()
	h := newAuthHarness(t)
	token, sess := h.signedIn()
	*h.now = h.now.Add(11 * time.Minute)
	h.idp.refreshErr = "invalid_grant" // a disabled user, a signed-out session or an expired token

	if _, err := h.a.Authenticate(context.Background(), token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("Authenticate = %v, want ErrUnauthenticated", err)
	}
	if _, err := h.store.Get(context.Background(), token); !errors.Is(err, ErrNoSession) {
		t.Errorf("the session must be deleted: %v", err)
	}
	got := h.audit.actions()
	if last := got[len(got)-1]; last != "auth.session_revoked:invalid_grant" {
		t.Errorf("audit = %v", got)
	}
	if e := h.audit.events[len(h.audit.events)-1]; e.SessionRef != sess.Ref() || e.ActorID != "user-ana" {
		t.Errorf("audit event = %+v", e)
	}
	if _, err := h.a.Authenticate(context.Background(), token); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("after the revocation: %v", err)
	}
}

// What the real Zitadel answers for a deactivated user (seen in scripts/auth-smoke.py) ends the
// session; ambiguous or operational errors do not.
func TestWhichRefusalsEndTheSession(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, code, desc string
		status           int
		wantEnded        string // audit reason; "" = the session survives
	}{
		{"invalid_grant", "invalid_grant", "", 400, "invalid_grant"},
		{"user not active (Zitadel)", "invalid_request", "Errors.User.NotActive", 400, "user_not_active"},
		{"user locked (Zitadel)", "invalid_request", "Errors.User.Locked", 400, "user_not_active"},
		{"a malformed request of ours must not sign anybody out", "invalid_request", "missing parameter", 400, ""},
		{"invalid_request without description", "invalid_request", "", 400, ""},
		{"our client secret was refused", "invalid_client", "", 401, ""},
		{"unsupported grant", "unsupported_grant_type", "", 400, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newAuthHarness(t)
			token, _ := h.signedIn()
			*h.now = h.now.Add(11 * time.Minute)
			h.idp.refreshErr, h.idp.refreshErrDesc = tc.code, tc.desc

			_, err := h.a.Authenticate(context.Background(), token)
			_, getErr := h.store.Get(context.Background(), token)
			if tc.wantEnded != "" {
				if !errors.Is(err, ErrUnauthenticated) || !errors.Is(getErr, ErrNoSession) {
					t.Fatalf("err %v, store %v: the session must end", err, getErr)
				}
				got := h.audit.actions()
				if last := got[len(got)-1]; last != "auth.session_revoked:"+tc.wantEnded {
					t.Errorf("audit = %v", got)
				}
				return
			}
			if getErr != nil {
				t.Fatalf("the session must survive: %v", getErr)
			}
			// Inside the grace period the request still goes through; the log says why.
			if err != nil {
				t.Errorf("inside the grace period: %v", err)
			}
			if tc.code == "invalid_client" && !strings.Contains(h.logs.String(), `"level":"ERROR"`) {
				t.Error("a refused client must be logged as an error")
			}
			if strings.Contains(h.logs.String(), tc.desc) && tc.desc != "" {
				t.Error("the provider's description must not be logged")
			}
		})
	}
}

func TestProviderOutageGivesAGracePeriodThenUnavailable(t *testing.T) {
	t.Parallel()
	h := newAuthHarness(t)
	token, sess := h.signedIn()
	h.idp.refreshStatus = http.StatusServiceUnavailable

	// Expired 30 s ago: inside the grace period the request goes through on the current session.
	*h.now = sess.AccessExpires.Add(30 * time.Second)
	if _, err := h.a.Authenticate(context.Background(), token); err != nil {
		t.Fatalf("inside the grace period: %v", err)
	}
	// Past it: refused as unavailable, not as signed out, and the session is kept.
	*h.now = sess.AccessExpires.Add(RefreshGrace + time.Second)
	if _, err := h.a.Authenticate(context.Background(), token); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("after the grace period: %v, want ErrUnavailable", err)
	}
	if _, err := h.store.Get(context.Background(), token); err != nil {
		t.Fatalf("an outage must not delete the session: %v", err)
	}
	// The provider recovers: the next request refreshes and carries on.
	h.idp.refreshStatus = 0
	if _, err := h.a.Authenticate(context.Background(), token); err != nil {
		t.Fatalf("after recovery: %v", err)
	}
}

func TestAnotherRequestHoldingTheLockGivesGraceThenWaitsAndGivesUpAsUnavailable(t *testing.T) {
	t.Parallel()
	h := newAuthHarness(t)
	h.a.WaitTimeout = 400 * time.Millisecond
	token, sess := h.signedIn()
	other, _ := h.store.TryLockRefresh(context.Background(), sess) // "another request" that never ends
	if other == nil {
		t.Fatal("could not take the lock")
	}

	// Inside the grace period: straight through, no waiting.
	*h.now = sess.AccessExpires.Add(time.Minute)
	start := time.Now()
	if _, err := h.a.Authenticate(context.Background(), token); err != nil {
		t.Fatalf("inside the grace period: %v", err)
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Error("inside the grace period the request must not wait")
	}
	// Past it: waits for the winner and, when nothing happens, gives up as unavailable,
	// never as unauthenticated, without calling the provider.
	*h.now = sess.AccessExpires.Add(RefreshGrace + time.Second)
	start = time.Now()
	_, err := h.a.Authenticate(context.Background(), token)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("after the grace period: %v, want ErrUnavailable", err)
	}
	if d := time.Since(start); d < 300*time.Millisecond || d > 2*time.Second {
		t.Errorf("waited %s, want about the wait timeout", d)
	}
	if h.idp.refreshN.Load() != 0 {
		t.Error("a request that lost the lock must not call the provider")
	}
	if _, err := h.store.Get(context.Background(), token); err != nil {
		t.Errorf("giving up must not touch the session: %v", err)
	}
}

// The reported bug: a user who was away for longer than the grace period comes back and the
// app sends several requests at once. One refreshes, the others wait for it; nobody is refused.
func TestFiftyRequestsAfterALongAbsenceMakeOneRefreshAndAreAllAccepted(t *testing.T) {
	t.Parallel()
	h := newAuthHarness(t)
	token, _ := h.signedIn()
	*h.now = h.now.Add(30 * time.Minute) // far past the access token and the grace period
	h.idp.refreshDelay = 400 * time.Millisecond

	var ok, refused atomic.Int32
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := h.a.Authenticate(context.Background(), token); err != nil {
				refused.Add(1)
				t.Errorf("Authenticate: %v", err)
				return
			}
			ok.Add(1)
		}()
	}
	wg.Wait()
	if got := h.idp.refreshN.Load(); got != 1 {
		t.Fatalf("refresh grants = %d, want exactly 1", got)
	}
	if ok.Load() != 50 || refused.Load() != 0 {
		t.Fatalf("accepted %d, refused %d; want 50 and 0", ok.Load(), refused.Load())
	}
}

// If the winner finds the session ended (the provider refused it), the ones waiting learn it
// is gone: that is the one case where waiting ends in ErrUnauthenticated.
func TestWaitersLearnThatTheWinnerEndedTheSession(t *testing.T) {
	t.Parallel()
	h := newAuthHarness(t)
	token, _ := h.signedIn()
	*h.now = h.now.Add(30 * time.Minute)
	h.idp.refreshErr = "invalid_grant"
	h.idp.refreshDelay = 300 * time.Millisecond

	errs := make(chan error, 10)
	for range 10 {
		go func() { _, err := h.a.Authenticate(context.Background(), token); errs <- err }()
	}
	for range 10 {
		if err := <-errs; !errors.Is(err, ErrUnauthenticated) {
			t.Errorf("Authenticate = %v, want ErrUnauthenticated", err)
		}
	}
	if got := h.idp.refreshN.Load(); got != 1 {
		t.Errorf("refresh grants = %d, want 1", got)
	}
}

func TestWaitingStopsWhenTheCallerGivesUp(t *testing.T) {
	t.Parallel()
	h := newAuthHarness(t)
	token, sess := h.signedIn()
	if l, _ := h.store.TryLockRefresh(context.Background(), sess); l == nil {
		t.Fatal("could not take the lock")
	}
	*h.now = sess.AccessExpires.Add(time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := h.a.Authenticate(ctx, token); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Authenticate = %v, want ErrUnavailable", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Error("the wait ignored the caller's context")
	}
}

func TestALogoutDuringARefreshIsNotUndone(t *testing.T) {
	t.Parallel()
	h := newAuthHarness(t)
	token, sess := h.signedIn()
	*h.now = h.now.Add(11 * time.Minute)
	h.idp.refreshDelay = 400 * time.Millisecond

	done := make(chan error, 1)
	go func() { _, err := h.a.Authenticate(context.Background(), token); done <- err }()
	time.Sleep(150 * time.Millisecond) // the refresh is in flight
	if err := h.store.Delete(context.Background(), sess); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := <-done; !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("Authenticate = %v, want ErrUnauthenticated", err)
	}
	if _, err := h.store.Get(context.Background(), token); !errors.Is(err, ErrNoSession) {
		t.Errorf("the refresh resurrected a logged-out session: %v", err)
	}
}

func TestAStoreOutageIsUnavailableNotUnauthenticated(t *testing.T) {
	t.Parallel()
	h := newAuthHarness(t)
	token, _ := h.signedIn()
	h.store.c.Close() // the Valkey connection is gone

	if _, err := h.a.Authenticate(context.Background(), token); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Authenticate = %v, want ErrUnavailable", err)
	}
}

// --- the interceptor ---

type nopStream struct{}

func (nopStream) Receive(any) error  { return io.EOF }
func (nopStream) SendHeaders() error { return nil }
func (nopStream) Send(any) error     { return nil }

const privateProc = "/pitbox.test.v1.TestService/Private"
const publicProc = "/pitbox.test.v1.TestService/Public"

// callRPC runs one RPC through a server with only the auth interceptor and reports what the
// handler saw.
func (h *authHarness) callRPC(procedure string, hdr map[string][]string) (principal *Principal, called bool, err error) {
	h.t.Helper()
	server := connect.NewServer(Interceptor(h.a, publicURL, publicProc))
	spec := connect.Spec{StreamType: connect.StreamTypeUnary, Procedure: procedure}
	server.Register(connect.Method{Spec: spec, Handler: func(ctx context.Context, _ connect.Spec, _ connect.ServerStream) error {
		called = true
		if p, ok := PrincipalFrom(ctx); ok {
			principal = &p
		}
		return nil
	}})
	info := &connect.CallInfo{}
	for k, vs := range hdr {
		info.RequestHeader().SetValues(k, vs)
	}
	err = server.Call(context.Background(), procedure, info, nopStream{})
	return principal, called, err
}

func codeOf(t *testing.T, err error) (connect.Code, string) {
	t.Helper()
	var ce *connect.Error
	if !errors.As(err, &ce) {
		t.Fatalf("error %v is not a *connect.Error", err)
	}
	return ce.Code(), ce.Message()
}

func TestInterceptorAuthenticatesFromTheCookie(t *testing.T) {
	t.Parallel()
	h := newAuthHarness(t)
	token, sess := h.signedIn()
	cookie := []string{SessionCookieName + "=" + token}

	p, called, err := h.callRPC(privateProc, map[string][]string{"Cookie": cookie, "Origin": {publicURL}, "Sec-Fetch-Site": {"same-origin"}})
	if err != nil || !called || p == nil || p.UserID != "user-ana" || p.SessionRef != sess.Ref() {
		t.Fatalf("principal %+v, called %v, err %v", p, called, err)
	}
	// No Origin and no Fetch Metadata (a non-browser client) is fine.
	if _, called, err := h.callRPC(privateProc, map[string][]string{"Cookie": cookie}); err != nil || !called {
		t.Errorf("without browser headers: called %v, err %v", called, err)
	}
	if _, called, err := h.callRPC(privateProc, map[string][]string{"Cookie": cookie, "Sec-Fetch-Site": {"none"}}); err != nil || !called {
		t.Errorf("Sec-Fetch-Site none: called %v, err %v", called, err)
	}
}

func TestInterceptorIsDefaultDeny(t *testing.T) {
	t.Parallel()
	h := newAuthHarness(t)
	token, _ := h.signedIn()

	for name, hdr := range map[string]map[string][]string{
		"no cookie":                       nil,
		"unknown session":                 {"Cookie": {SessionCookieName + "=" + strings.Repeat("A", 43)}},
		"another cookie":                  {"Cookie": {"other=" + token}},
		"two session cookies (ambiguous)": {"Cookie": {SessionCookieName + "=" + token, SessionCookieName + "=" + token}},
		"bearer is not accepted here":     {"Authorization": {"Bearer " + token}},
	} {
		_, called, err := h.callRPC(privateProc, hdr)
		code, msg := codeOf(t, err)
		if called || code != connect.CodeUnauthenticated || msg != msgUnauthenticated {
			t.Errorf("%s: called %v, code %v, message %q", name, called, code, msg)
		}
	}
	// An RPC listed as public needs nothing.
	if p, called, err := h.callRPC(publicProc, nil); err != nil || !called || p != nil {
		t.Errorf("public RPC: principal %v, called %v, err %v", p, called, err)
	}
}

func TestInterceptorRefusesCrossOriginRequestsBeforeLookingAtTheSession(t *testing.T) {
	t.Parallel()
	h := newAuthHarness(t)
	token, _ := h.signedIn()
	cookie := []string{SessionCookieName + "=" + token}

	for name, hdr := range map[string]map[string][]string{
		"foreign origin":     {"Cookie": cookie, "Origin": {"https://evil.example"}},
		"null origin":        {"Cookie": cookie, "Origin": {"null"}},
		"cross-site":         {"Cookie": cookie, "Sec-Fetch-Site": {"cross-site"}},
		"same-site sibling":  {"Cookie": cookie, "Sec-Fetch-Site": {"same-site"}},
		"origin ok, site ko": {"Cookie": cookie, "Origin": {publicURL}, "Sec-Fetch-Site": {"cross-site"}},
	} {
		_, called, err := h.callRPC(privateProc, hdr)
		code, msg := codeOf(t, err)
		if called || code != connect.CodePermissionDenied || msg != msgCrossOrigin {
			t.Errorf("%s: called %v, code %v, message %q", name, called, code, msg)
		}
	}
}

func TestInterceptorMapsOutagesToUnavailableWithoutLeakingTheCause(t *testing.T) {
	t.Parallel()
	h := newAuthHarness(t)
	token, _ := h.signedIn()
	h.store.c.Close()

	_, called, err := h.callRPC(privateProc, map[string][]string{"Cookie": {SessionCookieName + "=" + token}})
	code, msg := codeOf(t, err)
	if called || code != connect.CodeUnavailable || msg != msgAuthUnavailable {
		t.Errorf("called %v, code %v, message %q", called, code, msg)
	}
}

func TestPrincipalContext(t *testing.T) {
	t.Parallel()
	if _, ok := PrincipalFrom(context.Background()); ok {
		t.Error("an empty context has no principal")
	}
	ctx := WithPrincipal(context.Background(), Principal{UserID: "u"})
	if p, ok := PrincipalFrom(ctx); !ok || p.UserID != "u" {
		t.Errorf("PrincipalFrom = %+v, %v", p, ok)
	}
}

func TestDenyAllRefusesEverythingAndNeverCallsTheHandler(t *testing.T) {
	t.Parallel()
	server := connect.NewServer(DenyAll())
	spec := connect.Spec{StreamType: connect.StreamTypeUnary, Procedure: privateProc}
	called := false
	server.Register(connect.Method{Spec: spec, Handler: func(context.Context, connect.Spec, connect.ServerStream) error {
		called = true
		return nil
	}})
	err := server.Call(context.Background(), privateProc, &connect.CallInfo{}, nopStream{})
	code, msg := codeOf(t, err)
	if called || code != connect.CodeUnauthenticated || msg != msgUnauthenticated {
		t.Errorf("called %v, code %v, message %q", called, code, msg)
	}
}
