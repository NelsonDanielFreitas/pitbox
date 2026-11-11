package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/valkey-io/valkey-go"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/interceptors"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/ratelimit"
)

// RecoverPath asks for a passkey registration link by email (#158, ADR 0034).
const RecoverPath = "/auth/recover"

// Limits of the recovery request.
const (
	recoverMaxBody    = 1 << 10 // the body is one short email
	recoverMaxEmail   = 254     // RFC 5321
	recoverWorkTime   = 20 * time.Second
	recoverMaxWorkers = 8 // requests being worked on at once; more are dropped

	// Per window: a victim's inbox is not a mailbox to flood, one address is not a mail
	// cannon, and the whole service has a ceiling. Wrong recovery codes are counted apart from
	// the emails sent (ADR 0035): someone who only knows an address can fill the first count,
	// but never the second, so the owner can still get the link when they have the code.
	recoverWindow            = time.Hour
	recoverMaxSentPerEmail   = 3  // links sent to one address
	recoverMaxFailPerEmail   = 10 // failed or missing codes for one address
	recoverMaxPerIP          = 10
	recoverMaxConsumePerUser = 20 // codes tried by a signed-in user (the step-up)
	recoverMaxGlobal         = 120
	recoverThrottlePath      = "pitbox:rl:rec:"
)

// Audit reasons of auth.recovery_requested. The client never sees them.
const (
	recoverReasonSent       = "sent"
	recoverReasonSentCode   = "sent_with_code"
	recoverReasonCodeNeeded = "code_required"
	recoverReasonBadCode    = "invalid_code"
	recoverReasonNoCodeKeys = "codes_unavailable"
	recoverReasonStore      = "store_error"
	recoverReasonNoAccount  = "no_account"
	recoverReasonNoPasskey  = "no_passkey"
	recoverReasonUpstream   = "upstream_error"
	recoverReasonDisabled   = "disabled"
	recoverReasonBusy       = "dropped_busy"
	recoverReasonThrottle   = "throttled"
	recoverReasonThrottleIn = "throttle_unavailable"
)

// RecoveryThrottler counts events per key and window, for the limits that are per hour and
// not per second (the request limiter cannot express "3 per hour").
type RecoveryThrottler interface {
	// Hit counts one event for key and reports whether it is still within max for the window.
	Hit(ctx context.Context, key string, max int64, window time.Duration) (within bool, err error)
	// Count reads the events counted for key in the current window without counting one.
	Count(ctx context.Context, key string) (int64, error)
}

// ValkeyThrottler implements RecoveryThrottler on Valkey with a fixed window: the first event
// sets the expiry. The Valkey user is confined to pitbox:rl:* (ADR 0013).
type ValkeyThrottler struct{ c valkey.Client }

// NewValkeyThrottler takes the client shared with the session store.
func NewValkeyThrottler(c valkey.Client) *ValkeyThrottler { return &ValkeyThrottler{c: c} }

var hitScript = valkey.NewLuaScript(`
local n = redis.call('INCR', KEYS[1])
if n == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end
return n
`)

// Hit implements RecoveryThrottler.
func (t *ValkeyThrottler) Hit(ctx context.Context, key string, max int64, window time.Duration) (bool, error) {
	if !strings.HasPrefix(key, recoverThrottlePath) {
		return false, errors.New("auth: throttle key outside its prefix")
	}
	n, err := hitScript.Exec(ctx, t.c, []string{key}, []string{strconv.FormatInt(window.Milliseconds(), 10)}).AsInt64()
	if err != nil {
		return false, err
	}
	return n <= max, nil
}

// Count implements RecoveryThrottler.
func (t *ValkeyThrottler) Count(ctx context.Context, key string) (int64, error) {
	if !strings.HasPrefix(key, recoverThrottlePath) {
		return 0, errors.New("auth: throttle key outside its prefix")
	}
	n, err := t.c.Do(ctx, t.c.B().Get().Key(key).Build()).AsInt64()
	if valkey.IsValkeyNil(err) {
		return 0, nil
	}
	return n, err
}

// recoverer holds the state the handler needs for /auth/recover. The zero value is usable:
// the worker slots are made on first use.
type recoverer struct {
	once  sync.Once
	slots chan struct{}
}

func (r *recoverer) acquire() bool {
	r.once.Do(func() { r.slots = make(chan struct{}, recoverMaxWorkers) })
	select {
	case r.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (r *recoverer) release() { <-r.slots }

// normalizeEmail returns the lowercase address, or false if the value is not a plain address:
// no display name, no angle brackets, no comments, a domain with a dot, bounded length.
func normalizeEmail(raw string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" || len(email) > recoverMaxEmail {
		return "", false
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Name != "" || addr.Address != email {
		return "", false
	}
	at := strings.LastIndexByte(email, '@')
	if at < 1 || !strings.Contains(email[at+1:], ".") {
		return "", false
	}
	for _, r := range email {
		if r <= ' ' || r == '<' || r == '>' || r == '(' || r == ')' || r == '"' || r == ',' || r == ';' || r == '\\' {
			return "", false
		}
	}
	return email, true
}

// recoverAccount handles "I lost my passkey". The answer does not depend on whether the account
// exists: after the body is validated and the flood limit is checked it is always 202 with
// the same body, and everything that touches the account happens after the answer is sent.
// Nothing here tells the caller whether an email was sent.
func (h *Handler) recoverAccount(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	var req struct {
		Email string `json:"email"`
		// Code is a recovery code. Optional: an account without codes recovers with the
		// email alone (ADR 0035).
		Code string `json:"code"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, recoverMaxBody)
	if !decodeMobileBody(w, r, &req) {
		return
	}
	email, ok := normalizeEmail(req.Email)
	if !ok {
		mobileFail(w, http.StatusBadRequest, mobileErrInvalid)
		return
	}
	code := ""
	if req.Code != "" {
		// A code that is not even well formed is the caller's typing, whatever the account.
		if code, ok = NormalizeRecoveryCode(req.Code); !ok {
			mobileFail(w, http.StatusBadRequest, mobileErrInvalid)
			return
		}
	}
	if h.Limiter == nil {
		// Fail closed: a public endpoint without a limit is not served.
		mobileFail(w, http.StatusServiceUnavailable, mobileErrUnavailable)
		return
	}
	meta := interceptors.RequestMeta(r, h.ClientIPHeader)
	ipKey := "unknown"
	if meta.IP.IsValid() {
		ipKey = meta.IP.String()
	}
	// The flood limit per address is the only refusal that can differ between callers, and
	// it depends on the caller's address, never on the email.
	decision := h.Limiter.Allow(r.Context(), ratelimit.ScopePublic, "auth-recover:"+ipKey, true)
	if decision == ratelimit.RateLimited {
		mobileFail(w, http.StatusTooManyRequests, mobileErrRateLimited)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"status":"accepted"}` + "\n"))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	if decision != ratelimit.Allowed {
		// The limiter is down: the request is dropped, not served unlimited, and the caller
		// is told the same thing as always.
		h.Log.WarnContext(r.Context(), "recovery request dropped", slog.String("reason", recoverReasonThrottleIn))
		return
	}
	// The work outlives the request: its own deadline, the request's values (trace) only.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), recoverWorkTime)
	go func() {
		defer cancel()
		h.recoverWork(ctx, email, code, meta, ipKey)
	}()
}

// recoverWork does what the request asked, after the answer is gone. The reason of the
// outcome goes to the audit trail with a pseudonym of the email (or the subject, when the
// account was found), never the address.
func (h *Handler) recoverWork(ctx context.Context, email, code string, meta interceptors.AuditMeta, ipKey string) {
	actor := "email:" + h.pseudonym(email)
	record := func(reason string) {
		if h.Audit == nil {
			return
		}
		if err := h.Audit.Record(ctx, AuditEvent{
			Action: "auth.recovery_requested", ActorKind: ActorSystem, ActorID: actor, Reason: reason, Meta: meta,
		}); err != nil {
			h.Log.ErrorContext(ctx, "audit recovery requested", slog.String("reason", "audit"))
		}
	}

	if !h.recoverySlots.acquire() {
		h.Log.WarnContext(ctx, "recovery request dropped", slog.String("reason", recoverReasonBusy))
		record(recoverReasonBusy)
		return
	}
	defer h.recoverySlots.release()

	if h.RecoverThrottle == nil {
		h.Log.ErrorContext(ctx, "recovery request dropped", slog.String("reason", recoverReasonThrottleIn))
		return
	}
	// Every request counts against its address and the whole service; the count per email is
	// taken later and only for what the email can be blamed for.
	for _, l := range []struct {
		key string
		max int64
	}{
		{recoverThrottlePath + "ip:" + ipKey, recoverMaxPerIP},
		{recoverThrottlePath + "global", recoverMaxGlobal},
	} {
		within, err := h.RecoverThrottle.Hit(ctx, l.key, l.max, recoverWindow)
		if err != nil {
			h.Log.WarnContext(ctx, "recovery request dropped", slog.String("reason", recoverReasonThrottleIn))
			return
		}
		if !within {
			record(recoverReasonThrottle)
			return
		}
	}

	if h.Accounts == nil {
		record(recoverReasonDisabled)
		return
	}
	subject, found, err := h.Accounts.FindUserByEmail(ctx, email)
	if err != nil {
		h.Log.WarnContext(ctx, "recovery lookup failed", slog.String("reason", recoverReasonUpstream))
		record(recoverReasonUpstream)
		return
	}
	if !found {
		record(recoverReasonNoAccount)
		return
	}
	actor = "sub:" + subject
	methods, err := h.Accounts.AuthMethods(ctx, subject)
	if err != nil {
		h.Log.WarnContext(ctx, "recovery lookup failed", slog.String("reason", recoverReasonUpstream))
		record(recoverReasonUpstream)
		return
	}
	if !methods.Passkey {
		// A password account recovers through the identity provider's own password reset.
		record(recoverReasonNoPasskey)
		return
	}

	// The second factor (ADR 0035): an account with recovery codes needs one of them besides
	// the email. Without a store or a key to check it, nothing is sent.
	if h.RecoveryCodes == nil || h.RecoveryCodeHasher == nil {
		h.Log.ErrorContext(ctx, "recovery request dropped", slog.String("reason", recoverReasonNoCodeKeys))
		record(recoverReasonNoCodeKeys)
		return
	}
	pseudo := h.pseudonym(email)
	// Failures are counted per address AND per caller: someone who only knows an address can
	// fill their own count, never the one of the person who owns it.
	failKey, sentKey := recoverThrottlePath+"fail:"+pseudo+":"+ipKey, recoverThrottlePath+"sent:"+pseudo
	for _, l := range []struct {
		key string
		max int64
	}{{failKey, recoverMaxFailPerEmail}, {sentKey, recoverMaxSentPerEmail}} {
		n, err := h.RecoverThrottle.Count(ctx, l.key)
		if err != nil {
			h.Log.WarnContext(ctx, "recovery request dropped", slog.String("reason", recoverReasonThrottleIn))
			return
		}
		if n >= l.max {
			record(recoverReasonThrottle)
			return
		}
	}
	attempt, err := h.RecoveryCodes.AttemptBySubject(ctx, subject, h.RecoveryCodeHasher.Hashes(code))
	if err != nil {
		h.Log.WarnContext(ctx, "recovery code check failed", slog.String("reason", recoverReasonStore))
		record(recoverReasonStore)
		return
	}
	usedCode := false
	switch attempt {
	case AttemptUnknownUser, AttemptNoCodes:
		// No code to ask for: this account recovers with the email alone, as before.
	case AttemptConsumed:
		usedCode = true
	default: // AttemptInvalid
		_, _ = h.RecoverThrottle.Hit(ctx, failKey, recoverMaxFailPerEmail, recoverWindow)
		if code == "" {
			record(recoverReasonCodeNeeded)
		} else {
			record(recoverReasonBadCode)
		}
		return
	}

	if within, err := h.RecoverThrottle.Hit(ctx, sentKey, recoverMaxSentPerEmail, recoverWindow); err != nil || !within {
		record(recoverReasonThrottle)
		return
	}
	if err := h.Accounts.SendPasskeyRegistrationLink(ctx, subject); err != nil {
		h.Log.WarnContext(ctx, "recovery link failed", slog.String("reason", recoverReasonUpstream))
		record(recoverReasonUpstream)
		return
	}
	if !usedCode {
		record(recoverReasonSent)
		return
	}
	// A code was used: the rest of the set may have been seen, and every session of the
	// account ends, so what the person finds after the new passkey is a clean account.
	if err := h.RecoveryCodes.InvalidateBySubject(ctx, subject); err != nil {
		h.Log.ErrorContext(ctx, "recovery codes not invalidated", slog.String("reason", recoverReasonStore))
	}
	if h.SessionRevoker != nil {
		if err := h.SessionRevoker.DeleteAllBySubject(ctx, subject); err != nil {
			h.Log.ErrorContext(ctx, "sessions not ended after recovery", slog.String("reason", recoverReasonStore))
		}
	}
	record(recoverReasonSentCode)
}

// pseudonym is the audit identifier of an email; without a key it is a constant, so nothing
// about the address is recorded.
func (h *Handler) pseudonym(email string) string {
	if h.EmailPseudonyms == nil {
		return "unkeyed"
	}
	return h.EmailPseudonyms.Pseudonym(email)
}
