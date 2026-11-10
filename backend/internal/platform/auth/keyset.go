package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// Limits of the bounded key set (ADR 0032, section 7).
const (
	// keySetTTL is how long a fetched key set is trusted. The identity provider serves it
	// with a 5 minute max-age; a longer TTL here only delays noticing a key that was removed.
	keySetTTL = 10 * time.Minute
	// keySetCooldown is the minimum time between two fetch attempts, successful or not: at
	// most two requests per minute reach the identity provider, whatever the callers send.
	keySetCooldown = 30 * time.Second
	// keySetNegativeTTL is how long an unknown kid is refused without looking again.
	keySetNegativeTTL = time.Minute
	// keySetMaxNegative bounds the memory spent remembering unknown kids.
	keySetMaxNegative = 256
	// keySetFetchTimeout bounds one request to the identity provider.
	keySetFetchTimeout = 5 * time.Second
	// keySetMaxBody bounds the JWKS response.
	keySetMaxBody = 256 << 10
	// keySetMaxKeys bounds how many keys of one response are kept.
	keySetMaxKeys = 32
	// keySetMaxKidLen bounds a key id taken from an untrusted token header.
	keySetMaxKidLen = 128
)

// The errors carry constant text on purpose: they end up in logs and nothing from the token
// or from the response may reach them.
var (
	errKeySetToken       = errors.New("auth: malformed or unacceptable token header")
	errKeySetUnknownKey  = errors.New("auth: no signing key for this token")
	errKeySetUnavailable = errors.New("auth: signing keys unavailable")
	errKeySetBadSig      = errors.New("auth: token signature invalid")
)

// keySetConfig configures a boundedKeySet; zero values mean the defaults above.
type keySetConfig struct {
	URI         string
	Client      *http.Client
	Algs        []jose.SignatureAlgorithm // accepted signature algorithms; never empty
	Now         func() time.Time
	TTL         time.Duration
	Cooldown    time.Duration
	NegativeTTL time.Duration
	MaxNegative int
	Timeout     time.Duration
	MaxBody     int64
}

// boundedKeySet implements oidc.KeySet over a remote JWKS with a hard limit on how often
// it may go to the network.
//
// go-oidc's own RemoteKeySet fetches again on every token whose kid it does not know, with
// no cache lifetime and no limit. Anyone who can present a token to an endpoint that
// verifies one (the public back-channel logout endpoint, and the mobile login) could make
// the server hammer the identity provider with unknown kids. Here:
//
//   - fetches are rate limited by a cooldown that also covers failed attempts;
//   - concurrent callers share one fetch (single flight);
//   - an unknown kid is remembered for a while, in a table of bounded size;
//   - the response size, the number of keys and the time of a fetch are bounded;
//   - a token without a kid is refused (the identity provider always sets one).
//
// When the key set is stale and the cooldown forbids a new fetch the token is refused
// (fail closed): stale keys are never used.
type boundedKeySet struct {
	cfg keySetConfig

	mu          sync.Mutex
	set         map[string][]jose.JSONWebKey // kid -> keys
	fetchedAt   time.Time                    // zero: never fetched
	lastAttempt time.Time                    // zero: never attempted
	unknown     map[string]time.Time         // kid -> when the refusal expires
	flight      *keyFlight
}

// keyFlight is one fetch in progress.
type keyFlight struct {
	done chan struct{}
	err  error
}

func newBoundedKeySet(cfg keySetConfig) *boundedKeySet {
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 10 * time.Second}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.TTL <= 0 {
		cfg.TTL = keySetTTL
	}
	if cfg.Cooldown <= 0 {
		cfg.Cooldown = keySetCooldown
	}
	if cfg.NegativeTTL <= 0 {
		cfg.NegativeTTL = keySetNegativeTTL
	}
	if cfg.MaxNegative <= 0 {
		cfg.MaxNegative = keySetMaxNegative
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = keySetFetchTimeout
	}
	if cfg.MaxBody <= 0 {
		cfg.MaxBody = keySetMaxBody
	}
	return &boundedKeySet{cfg: cfg, unknown: map[string]time.Time{}}
}

// VerifySignature implements oidc.KeySet: it returns the payload of a JWS whose signature
// verifies with a key of the identity provider.
func (k *boundedKeySet) VerifySignature(ctx context.Context, rawJWT string) ([]byte, error) {
	jws, err := jose.ParseSigned(rawJWT, k.cfg.Algs)
	if err != nil || len(jws.Signatures) != 1 {
		return nil, errKeySetToken
	}
	header := jws.Signatures[0].Header
	if header.KeyID == "" || len(header.KeyID) > keySetMaxKidLen {
		return nil, errKeySetToken
	}
	keys, err := k.keysFor(ctx, header.KeyID)
	if err != nil {
		return nil, err
	}
	for _, key := range keys {
		// A key that names an algorithm is only good for that one.
		if key.Algorithm != "" && key.Algorithm != header.Algorithm {
			continue
		}
		if payload, err := jws.Verify(&key); err == nil {
			return payload, nil
		}
	}
	return nil, errKeySetBadSig
}

// keysFor returns the keys with this kid, fetching the key set only when the limits allow.
func (k *boundedKeySet) keysFor(ctx context.Context, kid string) ([]jose.JSONWebKey, error) {
	// Three rounds at most: look, maybe wait for a fetch someone else started, maybe
	// fetch. A fetch started by someone else sets lastAttempt, so the next round cannot
	// start another one before the cooldown.
	for range 3 {
		k.mu.Lock()
		now := k.cfg.Now()
		fresh := !k.fetchedAt.IsZero() && now.Sub(k.fetchedAt) < k.cfg.TTL
		if fresh {
			if keys := k.set[kid]; len(keys) > 0 {
				k.mu.Unlock()
				return keys, nil
			}
			if expiry, bad := k.unknown[kid]; bad && now.Before(expiry) {
				k.mu.Unlock()
				return nil, errKeySetUnknownKey
			}
		}
		if f := k.flight; f != nil {
			k.mu.Unlock()
			select {
			case <-f.done:
				continue
			case <-ctx.Done():
				return nil, errKeySetUnavailable
			}
		}
		if !k.lastAttempt.IsZero() && now.Sub(k.lastAttempt) < k.cfg.Cooldown {
			if fresh {
				k.rememberUnknownLocked(kid, now)
				k.mu.Unlock()
				return nil, errKeySetUnknownKey
			}
			k.mu.Unlock()
			return nil, errKeySetUnavailable
		}
		f := &keyFlight{done: make(chan struct{})}
		k.flight = f
		k.lastAttempt = now
		k.mu.Unlock()

		set, err := k.fetch(ctx)

		k.mu.Lock()
		if err == nil {
			k.set, k.fetchedAt = set, k.cfg.Now()
			clear(k.unknown) // the new set may hold what was unknown
		}
		k.flight = nil
		f.err = err
		close(f.done)
		keys := k.set[kid]
		if err == nil && len(keys) == 0 {
			k.rememberUnknownLocked(kid, k.cfg.Now())
		}
		k.mu.Unlock()

		switch {
		case err != nil:
			return nil, errKeySetUnavailable
		case len(keys) == 0:
			return nil, errKeySetUnknownKey
		}
		return keys, nil
	}
	return nil, errKeySetUnavailable
}

// rememberUnknownLocked records a refused kid; the table never grows past MaxNegative, and
// when it is full of live entries the new kid is simply not remembered (the cooldown still
// bounds the fetches). k.mu must be held.
func (k *boundedKeySet) rememberUnknownLocked(kid string, now time.Time) {
	if len(k.unknown) >= k.cfg.MaxNegative {
		for id, expiry := range k.unknown {
			if !now.Before(expiry) {
				delete(k.unknown, id)
			}
		}
		if len(k.unknown) >= k.cfg.MaxNegative {
			return
		}
	}
	k.unknown[kid] = now.Add(k.cfg.NegativeTTL)
}

// fetch downloads and parses the key set. The caller's cancellation does not abort it (other
// callers wait for the same fetch), but the timeout does.
func (k *boundedKeySet) fetch(ctx context.Context) (map[string][]jose.JSONWebKey, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), k.cfg.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, k.cfg.URI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := k.cfg.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jwks request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jwks status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, k.cfg.MaxBody+1))
	if err != nil {
		return nil, fmt.Errorf("jwks body: %w", err)
	}
	if int64(len(body)) > k.cfg.MaxBody {
		return nil, errors.New("jwks body too large")
	}
	return parseKeySet(body)
}

// parseKeySet keeps the usable public signing keys of a JWKS document, indexed by kid.
// Private keys, keys for encryption, keys without a kid and invalid keys are dropped; the
// document is refused if nothing usable is left.
func parseKeySet(body []byte) (map[string][]jose.JSONWebKey, error) {
	var doc jose.JSONWebKeySet
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, errors.New("jwks: malformed document")
	}
	out := map[string][]jose.JSONWebKey{}
	n := 0
	for _, key := range doc.Keys {
		if n >= keySetMaxKeys {
			break
		}
		if key.KeyID == "" || len(key.KeyID) > keySetMaxKidLen || !key.Valid() || !key.IsPublic() ||
			(key.Use != "" && key.Use != "sig") {
			continue
		}
		out[key.KeyID] = append(out[key.KeyID], key)
		n++
	}
	if n == 0 {
		return nil, errors.New("jwks: no usable key")
	}
	return out, nil
}
