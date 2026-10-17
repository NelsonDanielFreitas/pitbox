package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"
)

// ErrNoSession means there is no usable session for the token: unknown, expired (idle or
// absolute) or revoked. One error, no detail.
var ErrNoSession = errors.New("auth: no session")

// ErrConflict means the session changed since it was read (someone else refreshed it).
var ErrConflict = errors.New("auth: session changed")

const (
	sessPrefix = "pitbox:sess:"
	lockPrefix = "pitbox:lock:refresh:"

	// touchEvery is how often the idle timeout is pushed forward: a session in use does
	// not cost a write on every request.
	touchEvery = time.Minute

	// RefreshLockTTL is how long a refresh may hold the lock; the lock is only there to
	// stop a crashed instance from blocking the session forever.
	RefreshLockTTL = 10 * time.Second
)

// Session is what the BFF keeps for a signed-in browser. Only identifiers and the sealed
// tokens: no email, no name, no access token (the BFF never needs it after the code
// exchange, and what is not stored cannot leak).
type Session struct {
	UserID        string // identity.user_account.id
	Subject       string // OIDC "sub"
	SessionID     string // OIDC "sid" (Zitadel's session; "" if the IdP sent none)
	AMR           []string
	AuthTime      time.Time
	CreatedAt     time.Time
	AccessExpires time.Time
	RefreshToken  string // plaintext in memory, sealed in the store
	IDToken       string // kept for id_token_hint at logout; sealed in the store
	UserAgent     string // for the user's list of sessions; personal data
	IP            string // for the user's list of sessions; personal data

	ref        string // hex sha256 of the cookie value: the key suffix
	generation uint64
}

// Ref identifies the session in logs and audit entries. It is the SHA-256 of the cookie
// value: it cannot be turned back into the cookie, and it is not accepted as one.
func (s Session) Ref() string { return s.ref }

// record is the stored form.
type record struct {
	V             int       `json:"v"`
	UserID        string    `json:"u"`
	Subject       string    `json:"sub"`
	SessionID     string    `json:"sid,omitempty"`
	AMR           []string  `json:"amr,omitempty"`
	AuthTime      time.Time `json:"at"`
	CreatedAt     time.Time `json:"ca"`
	AccessExpires time.Time `json:"ae"`
	SealedRefresh string    `json:"rt,omitempty"` // refresh token, sealed
	SealedID      string    `json:"id,omitempty"` // ID token, sealed
	UserAgent     string    `json:"ua,omitempty"`
	IP            string    `json:"ip,omitempty"`
}

// Store keeps sessions in Valkey.
type Store struct {
	c        valkey.Client
	sealer   *Sealer
	now      func() time.Time
	idle     time.Duration
	absolute time.Duration
}

// NewStore takes the sealer for PurposeSessionToken. The Valkey user only needs access to
// pitbox:sess:* and pitbox:lock:* (ADR 0013).
func NewStore(c valkey.Client, sealer *Sealer, idle, absolute time.Duration, now func() time.Time) *Store {
	return &Store{c: c, sealer: sealer, now: now, idle: idle, absolute: absolute}
}

var (
	createScript = valkey.NewLuaScript(`
if redis.call('EXISTS', KEYS[1]) == 1 then return 0 end
redis.call('HSET', KEYS[1], 'g', '1', 'd', ARGV[1])
redis.call('PEXPIRE', KEYS[1], ARGV[2])
redis.call('SADD', KEYS[2], ARGV[3])
redis.call('PEXPIRE', KEYS[2], ARGV[4], 'NX')
redis.call('PEXPIRE', KEYS[2], ARGV[4], 'GT')
if ARGV[5] == '1' then redis.call('SET', KEYS[3], ARGV[3], 'PX', ARGV[2]) end
return 1`)

	// Compare-and-set on the generation: the write only happens if nobody changed the
	// session since it was read.
	updateScript = valkey.NewLuaScript(`
if redis.call('HGET', KEYS[1], 'g') ~= ARGV[1] then return 0 end
redis.call('HSET', KEYS[1], 'd', ARGV[2], 'g', tostring(tonumber(ARGV[1]) + 1))
return 1`)

	deleteScript = valkey.NewLuaScript(`
redis.call('DEL', KEYS[1])
redis.call('SREM', KEYS[2], ARGV[1])
if redis.call('GET', KEYS[3]) == ARGV[1] then redis.call('DEL', KEYS[3]) end
return 1`)

	// Release only if the lock is still ours: after our TTL another holder may own it.
	unlockScript = valkey.NewLuaScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then return redis.call('DEL', KEYS[1]) end
return 0`)
)

func hashHex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func (s *Store) keys(ref, sub, sid string) (sess, userIdx, sidIdx string) {
	sidIdx = sessPrefix + "s:none" // placeholder: the script only writes it if there is a sid
	if sid != "" {
		sidIdx = sessPrefix + "s:" + hashHex(sid)
	}
	return sessPrefix + ref, sessPrefix + "u:" + hashHex(sub), sidIdx
}

// Create stores a new session and returns the cookie value. The value is 256 random bits;
// only its SHA-256 reaches the store, so a read of the Valkey does not yield usable cookies.
func (s *Store) Create(ctx context.Context, sess Session) (token string, err error) {
	if sess.Subject == "" || sess.UserID == "" {
		return "", errors.New("auth: a session needs a subject and a user")
	}
	token, err = randomToken()
	if err != nil {
		return "", err
	}
	ref := hashHex(token)
	now := s.now()
	sess.CreatedAt = now
	sealedRT, err := s.sealer.Seal([]byte(sess.RefreshToken), []byte(ref+":rt"))
	if err != nil {
		return "", fmt.Errorf("seal refresh token: %w", err)
	}
	sealedID, err := s.sealer.Seal([]byte(sess.IDToken), []byte(ref+":id"))
	if err != nil {
		return "", fmt.Errorf("seal id token: %w", err)
	}
	data, err := json.Marshal(record{
		V: 1, UserID: sess.UserID, Subject: sess.Subject, SessionID: sess.SessionID, AMR: sess.AMR,
		AuthTime: sess.AuthTime, CreatedAt: now, AccessExpires: sess.AccessExpires,
		SealedRefresh: sealedRT, SealedID: sealedID, UserAgent: sess.UserAgent, IP: sess.IP,
	})
	if err != nil {
		return "", fmt.Errorf("marshal session: %w", err)
	}
	k, u, sid := s.keys(ref, sess.Subject, sess.SessionID)
	hasSID := "0"
	if sess.SessionID != "" {
		hasSID = "1"
	}
	ttl := min(s.idle, s.absolute)
	res, err := createScript.Exec(ctx, s.c, []string{k, u, sid}, []string{
		string(data), ms(ttl), ref, ms(s.absolute), hasSID,
	}).AsInt64()
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	if res != 1 {
		return "", errors.New("create session: key collision")
	}
	return token, nil
}

func ms(d time.Duration) string { return strconv.FormatInt(d.Milliseconds(), 10) }

// validToken is a cheap shape check before hashing: 32 bytes as unpadded base64url.
func validToken(t string) bool {
	if len(t) != 43 {
		return false
	}
	for i := 0; i < len(t); i++ {
		if !isBase64URL(t[i]) {
			return false
		}
	}
	return true
}

func isBase64URL(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_'
}

// Get returns the session for a cookie value. It enforces the absolute timeout (the idle
// one is the key's TTL) and pushes the idle timeout forward at most once per touchEvery.
func (s *Store) Get(ctx context.Context, token string) (Session, error) {
	if !validToken(token) {
		return Session{}, ErrNoSession
	}
	return s.GetByRef(ctx, hashHex(token))
}

// GetByRef returns the session for a reference (SHA-256 of the cookie value). It is used by
// step-up, where the callback carries the session ref in the sealed login cookie but not the
// cookie value itself.
func (s *Store) GetByRef(ctx context.Context, ref string) (Session, error) {
	key := sessPrefix + ref
	res := s.c.DoMulti(ctx,
		s.c.B().Hgetall().Key(key).Build(),
		s.c.B().Pttl().Key(key).Build(),
	)
	fields, err := res[0].AsStrMap()
	if err != nil {
		return Session{}, fmt.Errorf("read session: %w", err)
	}
	if len(fields) == 0 {
		return Session{}, ErrNoSession
	}
	pttl, err := res[1].AsInt64()
	if err != nil {
		return Session{}, fmt.Errorf("read session ttl: %w", err)
	}
	gen, err := strconv.ParseUint(fields["g"], 10, 64)
	if err != nil {
		return Session{}, ErrNoSession // corrupt: treat as gone
	}
	var rec record
	if err := json.Unmarshal([]byte(fields["d"]), &rec); err != nil || rec.V != 1 {
		return Session{}, ErrNoSession
	}
	sess := Session{
		UserID: rec.UserID, Subject: rec.Subject, SessionID: rec.SessionID, AMR: rec.AMR,
		AuthTime: rec.AuthTime, CreatedAt: rec.CreatedAt, AccessExpires: rec.AccessExpires,
		UserAgent: rec.UserAgent, IP: rec.IP,
		ref: ref, generation: gen,
	}

	now := s.now()
	remaining := rec.CreatedAt.Add(s.absolute).Sub(now)
	if remaining <= 0 {
		_ = s.Delete(ctx, sess)
		return Session{}, ErrNoSession
	}

	rt, err := s.sealer.Open(rec.SealedRefresh, []byte(ref+":rt"))
	if err != nil {
		return Session{}, ErrNoSession // sealed with a key we no longer have, or tampered with
	}
	id, err := s.sealer.Open(rec.SealedID, []byte(ref+":id"))
	if err != nil {
		return Session{}, ErrNoSession
	}
	sess.RefreshToken, sess.IDToken = string(rt), string(id)

	// Sliding idle timeout: only when the last push was more than touchEvery ago.
	want := min(s.idle, remaining)
	if pttl >= 0 && time.Duration(pttl)*time.Millisecond < want-touchEvery {
		// Best effort: failing to extend only shortens the session, never lengthens it.
		_ = s.c.Do(ctx, s.c.B().Pexpire().Key(key).Milliseconds(want.Milliseconds()).Build()).Error()
	}
	return sess, nil
}

// Update writes the session back if nobody changed it since Get (compare-and-set on the
// generation). The refresh token and the access expiry are the fields that change.
func (s *Store) Update(ctx context.Context, sess Session) error {
	sealedRT, err := s.sealer.Seal([]byte(sess.RefreshToken), []byte(sess.ref+":rt"))
	if err != nil {
		return fmt.Errorf("seal refresh token: %w", err)
	}
	sealedID, err := s.sealer.Seal([]byte(sess.IDToken), []byte(sess.ref+":id"))
	if err != nil {
		return fmt.Errorf("seal id token: %w", err)
	}
	data, err := json.Marshal(record{
		V: 1, UserID: sess.UserID, Subject: sess.Subject, SessionID: sess.SessionID, AMR: sess.AMR,
		AuthTime: sess.AuthTime, CreatedAt: sess.CreatedAt, AccessExpires: sess.AccessExpires,
		SealedRefresh: sealedRT, SealedID: sealedID, UserAgent: sess.UserAgent, IP: sess.IP,
	})
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	ok, err := updateScript.Exec(ctx, s.c, []string{sessPrefix + sess.ref},
		[]string{strconv.FormatUint(sess.generation, 10), string(data)}).AsInt64()
	if err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	if ok != 1 {
		return ErrConflict
	}
	return nil
}

// Delete removes the session and its index entries. Deleting a session that is already
// gone is not an error.
func (s *Store) Delete(ctx context.Context, sess Session) error {
	k, u, sid := s.keys(sess.ref, sess.Subject, sess.SessionID)
	if err := deleteScript.Exec(ctx, s.c, []string{k, u, sid}, []string{sess.ref}).Error(); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// SessionInfo is the public view of a session for the user's session list.
type SessionInfo struct {
	Ref       string
	UserID    string
	AuthTime  time.Time
	CreatedAt time.Time
	UserAgent string
	IP        string
	Current   bool // true if this is the session that asked for the list
}

// ListBySubject returns the active sessions of a user. The excludeRef session is marked
// Current; pass "" to mark none.
func (s *Store) ListBySubject(ctx context.Context, sub, excludeRef string) ([]SessionInfo, error) {
	_, u, _ := s.keys("", sub, "")
	refs, err := s.c.Do(ctx, s.c.B().Smembers().Key(u).Build()).AsStrSlice()
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	out := make([]SessionInfo, 0, len(refs))
	for _, ref := range refs {
		sess, err := s.GetByRef(ctx, ref)
		if err != nil {
			if errors.Is(err, ErrNoSession) {
				continue // stale index entry
			}
			return nil, fmt.Errorf("read session %s: %w", ref, err)
		}
		out = append(out, SessionInfo{
			Ref:       ref,
			UserID:    sess.UserID,
			AuthTime:  sess.AuthTime,
			CreatedAt: sess.CreatedAt,
			UserAgent: sess.UserAgent,
			IP:        sess.IP,
			Current:   ref == excludeRef,
		})
	}
	// Deterministic order for tests and stable UI.
	slices.SortFunc(out, func(a, b SessionInfo) int {
		if a.Current != b.Current {
			if a.Current {
				return -1
			}
			return 1
		}
		return a.CreatedAt.Compare(b.CreatedAt)
	})
	return out, nil
}

// DeleteByRef removes a session by its reference. It reads the session first to know the
// subject and clean up the user index.
func (s *Store) DeleteByRef(ctx context.Context, ref string) error {
	sess, err := s.GetByRef(ctx, ref)
	if err != nil {
		if errors.Is(err, ErrNoSession) {
			return nil
		}
		return err
	}
	return s.Delete(ctx, sess)
}

// DeleteAllBySubject removes every session of a user. It is used after account recovery or
// by global sign-out.
func (s *Store) DeleteAllBySubject(ctx context.Context, sub string) error {
	_, u, _ := s.keys("", sub, "")
	refs, err := s.c.Do(ctx, s.c.B().Smembers().Key(u).Build()).AsStrSlice()
	if err != nil {
		return fmt.Errorf("list sessions for delete: %w", err)
	}
	for _, ref := range refs {
		if err := s.DeleteByRef(ctx, ref); err != nil {
			return err
		}
	}
	return nil
}

// RefreshLock is held by the one request that refreshes the tokens of a session.
type RefreshLock struct {
	s     *Store
	key   string
	owner string
}

// TryLockRefresh takes the refresh lock for the session without waiting: SET NX PX. It
// returns nil, nil if someone else holds it (the caller carries on with the current
// session, see ADR 0013).
func (s *Store) TryLockRefresh(ctx context.Context, sess Session) (*RefreshLock, error) {
	owner, err := randomToken()
	if err != nil {
		return nil, err
	}
	key := lockPrefix + sess.ref
	err = s.c.Do(ctx, s.c.B().Set().Key(key).Value(owner).Nx().PxMilliseconds(RefreshLockTTL.Milliseconds()).Build()).Error()
	if valkey.IsValkeyNil(err) {
		return nil, nil //nolint:nilnil // nil lock means "someone else has it", not an error
	}
	if err != nil {
		return nil, fmt.Errorf("take refresh lock: %w", err)
	}
	return &RefreshLock{s: s, key: key, owner: owner}, nil
}

// Release frees the lock only if it is still ours.
func (l *RefreshLock) Release(ctx context.Context) error {
	if err := unlockScript.Exec(ctx, l.s.c, []string{l.key}, []string{l.owner}).Error(); err != nil {
		return fmt.Errorf("release refresh lock: %w", err)
	}
	return nil
}
