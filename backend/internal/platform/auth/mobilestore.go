package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/valkey-io/valkey-go"
)

// Mobile login and Bearer storage (ADR 0032). The Valkey user needs access to pitbox:mob:*.
const (
	// bearerPrefix marks our Bearer. Anything else in the Authorization header (a JWT, an ID
	// token) is refused by shape, before the store is read.
	bearerPrefix = "pbm1_"
	// bearerKeyPrefix + sha256(secret part) -> session ref, for the life of the Bearer.
	bearerKeyPrefix = "pitbox:mob:at:" //nolint:gosec // a key prefix, not a credential
	// BearerTTL is how long a Bearer lives. It matches the access token of the identity
	// provider: a stolen Bearer is good for at most this long, and cannot be renewed.
	BearerTTL = 10 * time.Minute

	pendingPrefix = "pitbox:mob:pend:"
	pendingIndex  = "pitbox:mob:pendidx"
	// PendingTTL is how long a started login may take (the person authenticates in a browser).
	PendingTTL = 5 * time.Minute
	// maxPending bounds the logins waiting for their code: whoever calls /start (it is public)
	// cannot fill the store. Over it, /start refuses (fail closed).
	maxPending = 2000

	// maxCounter keeps the counter far from Lua's exact-integer limit (2^53).
	maxCounter = 1 << 40
	// counterGap is how far ahead of the stored value a counter may be. The app counts every
	// attempt before it sends it, so a lost response leaves a gap, never a repeat.
	counterGap = 16
)

// MobilePending is what /auth/mobile/start remembers until /auth/mobile/token. Nothing in it
// is a secret: the challenge is a hash, the key is public.
type MobilePending struct {
	Challenge string `json:"c"`  // base64url SHA-256 of the PKCE verifier
	Nonce     string `json:"n"`  // sent to the identity provider; must come back in the ID token
	DevicePub []byte `json:"dp"` // SPKI DER of the session key
	// StepUpRef and StepUpSub are set when the login is a reauthentication of a session the
	// phone already has: the session it replaces and the account it must stay (F2-06b).
	StepUpRef string `json:"sr,omitempty"`
	StepUpSub string `json:"ss,omitempty"`
}

var (
	// ErrNoPending: unknown, used or expired state.
	ErrNoPending = errors.New("auth: no pending mobile login")
	// ErrPendingFull: too many logins waiting.
	ErrPendingFull = errors.New("auth: too many pending mobile logins")
)

var (
	putPendingScript = valkey.NewLuaScript(`
redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', ARGV[3])
if redis.call('ZCARD', KEYS[2]) >= tonumber(ARGV[4]) then return 0 end
if not redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2], 'NX') then return -1 end
redis.call('ZADD', KEYS[2], ARGV[5], KEYS[1])
redis.call('PEXPIRE', KEYS[2], ARGV[2])
return 1`)

	// Read and delete in one step: a state is used once, even by two simultaneous requests.
	takePendingScript = valkey.NewLuaScript(`
local v = redis.call('GETDEL', KEYS[1])
if v then redis.call('ZREM', KEYS[2], KEYS[1]) end
return v`)

	// Spend a counter: only a value above the stored one, and not absurdly above it.
	//   1  spent        -1  not above the stored value (a repeat: reuse)
	//  -2  too far ahead    -3  no such session
	consumeCounterScript = valkey.NewLuaScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then return -3 end
local c = tonumber(redis.call('HGET', KEYS[1], 'c') or '0')
local n = tonumber(ARGV[1])
if n <= c then return -1 end
if n > c + tonumber(ARGV[2]) then return -2 end
redis.call('HSET', KEYS[1], 'c', ARGV[1])
return 1`)

	// Replace the session's Bearer: the previous one stops working at once.
	rotateBearerScript = valkey.NewLuaScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then return 0 end
local old = redis.call('HGET', KEYS[1], 'b')
if old then redis.call('DEL', ARGV[4] .. old) end
redis.call('HSET', KEYS[1], 'b', ARGV[1])
redis.call('SET', KEYS[2], ARGV[3], 'PX', ARGV[2])
return 1`)
)

// CounterResult is the outcome of spending a refresh counter.
type CounterResult int

const (
	CounterSpent CounterResult = iota
	// CounterReused: the value was not above the stored one. A signed request that arrives a
	// second time is a copy of an earlier one: someone other than the app has it.
	CounterReused
	// CounterTooFar: the value jumps further than a lost response explains.
	CounterTooFar
	// CounterNoSession: the session is gone.
	CounterNoSession
)

// newBearer makes a Bearer: the prefix and 256 random bits.
func newBearer() (string, error) {
	t, err := randomToken()
	if err != nil {
		return "", err
	}
	return bearerPrefix + t, nil
}

// bearerSecret returns the random part of a Bearer, or "" if it is not shaped like one.
func bearerSecret(bearer string) string {
	secret, ok := strings.CutPrefix(bearer, bearerPrefix)
	if !ok || !validToken(secret) {
		return ""
	}
	return secret
}

// BearerFromHeader reads "Authorization: Bearer <token>". present reports whether the header
// exists at all (so a request that carries both a cookie and an Authorization header can be
// refused); token is "" if it is present but not a well-formed Bearer.
func BearerFromHeader(values []string) (token string, present bool) {
	if len(values) == 0 {
		return "", false
	}
	if len(values) != 1 {
		return "", true // two Authorization headers: ambiguous, refused
	}
	scheme, rest, ok := strings.Cut(values[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || bearerSecret(rest) == "" {
		return "", true
	}
	return rest, true
}

// CreateMobile stores a new mobile session and returns its reference. The caller then issues
// the first Bearer with RotateBearer. devicePub is the SPKI DER of the phone's session key.
func (s *Store) CreateMobile(ctx context.Context, sess Session, devicePub []byte) (ref string, err error) {
	if len(devicePub) == 0 {
		return "", errors.New("auth: a mobile session needs the device key")
	}
	sess.Kind, sess.DevicePub = KindMobile, devicePub
	token, err := s.create(ctx, sess)
	if err != nil {
		return "", err
	}
	return hashHex(token), nil // the token itself is dropped: nobody can present it
}

// RotateBearer issues a new Bearer for the session and invalidates the previous one.
func (s *Store) RotateBearer(ctx context.Context, ref string) (bearer string, err error) {
	bearer, err = newBearer()
	if err != nil {
		return "", err
	}
	hash := hashHex(bearerSecret(bearer))
	ok, err := rotateBearerScript.Exec(ctx, s.c,
		[]string{sessPrefix + ref, bearerKeyPrefix + hash},
		[]string{hash, ms(BearerTTL), ref, bearerKeyPrefix}).AsInt64()
	if err != nil {
		return "", fmt.Errorf("rotate bearer: %w", err)
	}
	if ok != 1 {
		return "", ErrNoSession
	}
	return bearer, nil
}

// GetByBearer returns the mobile session a Bearer belongs to.
func (s *Store) GetByBearer(ctx context.Context, bearer string) (Session, error) {
	secret := bearerSecret(bearer)
	if secret == "" {
		return Session{}, ErrNoSession
	}
	hash := hashHex(secret)
	ref, err := s.c.Do(ctx, s.c.B().Get().Key(bearerKeyPrefix+hash).Build()).ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return Session{}, ErrNoSession
		}
		return Session{}, fmt.Errorf("read bearer: %w", err)
	}
	sess, err := s.GetByRef(ctx, ref)
	if err != nil {
		return Session{}, err
	}
	if sess.Kind != KindMobile {
		return Session{}, ErrNoSession
	}
	// The session must still name this very Bearer (a rotation deletes the old key, this is
	// the second lock on the same door).
	cur, err := s.c.Do(ctx, s.c.B().Hget().Key(sessPrefix+ref).Field("b").Build()).ToString()
	if err != nil || subtle.ConstantTimeCompare([]byte(cur), []byte(hash)) != 1 {
		return Session{}, ErrNoSession
	}
	return sess, nil
}

// ConsumeCounter spends a refresh counter (see the scripts for the rules).
func (s *Store) ConsumeCounter(ctx context.Context, ref string, counter uint64) (CounterResult, error) {
	if counter == 0 || counter > maxCounter {
		return CounterReused, nil // not a value the app ever sends
	}
	res, err := consumeCounterScript.Exec(ctx, s.c, []string{sessPrefix + ref},
		[]string{strconv.FormatUint(counter, 10), strconv.Itoa(counterGap)}).AsInt64()
	if err != nil {
		return 0, fmt.Errorf("spend counter: %w", err)
	}
	switch res {
	case 1:
		return CounterSpent, nil
	case -2:
		return CounterTooFar, nil
	case -3:
		return CounterNoSession, nil
	default:
		return CounterReused, nil
	}
}

// PutPending remembers a started login under its state. The state is random and public to
// the browser; only its hash is the key.
func (s *Store) PutPending(ctx context.Context, state string, p MobilePending) error {
	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal pending login: %w", err)
	}
	now := s.now()
	key := pendingPrefix + hashHex(state)
	res, err := putPendingScript.Exec(ctx, s.c, []string{key, pendingIndex}, []string{
		string(data), ms(PendingTTL), strconv.FormatInt(now.UnixMilli(), 10), strconv.Itoa(maxPending),
		strconv.FormatInt(now.Add(PendingTTL).UnixMilli(), 10),
	}).AsInt64()
	if err != nil {
		return fmt.Errorf("store pending login: %w", err)
	}
	switch res {
	case 1:
		return nil
	case 0:
		return ErrPendingFull
	default:
		return errors.New("store pending login: key collision")
	}
}

// TakePending returns and removes the login started with this state, once.
func (s *Store) TakePending(ctx context.Context, state string) (MobilePending, error) {
	if !validToken(state) {
		return MobilePending{}, ErrNoPending
	}
	raw, err := takePendingScript.Exec(ctx, s.c, []string{pendingPrefix + hashHex(state), pendingIndex}, nil).ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return MobilePending{}, ErrNoPending
		}
		return MobilePending{}, fmt.Errorf("take pending login: %w", err)
	}
	var p MobilePending
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return MobilePending{}, ErrNoPending
	}
	return p, nil
}
