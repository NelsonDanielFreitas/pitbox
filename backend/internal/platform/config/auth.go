package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

// AuthKey is one master key for sealing cookies and refresh tokens (ADR 0013).
type AuthKey struct {
	ID     string
	Secret []byte
}

// minAuthKeyBytes is the smallest master key accepted: it is the input of an HKDF-SHA256
// that derives one AES-256 key per purpose, so less than 256 bits would weaken them all.
const minAuthKeyBytes = 32

var keyID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,15}$`)

// allowedAlgs are the signature algorithms an ID token may use. "none" and the HMAC
// family (HS256..) are absent on purpose: with an HMAC the client secret would verify
// the token, so anyone holding it could forge one.
var allowedAlgs = []string{"ES256", "ES384", "ES512", "RS256", "EdDSA"}

// AuthConfigured reports whether authentication is configured. When it is not (only
// possible in local development), /auth/* answers 503 and every RPC is refused:
// the service fails closed, it never serves unauthenticated.
func (c Config) AuthConfigured() bool {
	return c.OIDCClientID != "" && c.OIDCClientSecret != "" && c.AuthKeys != "" && c.ValkeyPassword != ""
}

// AuthKeyring parses AuthKeys. The first key seals; all of them open.
func (c Config) AuthKeyring() ([]AuthKey, error) {
	return parseAuthKeys(c.AuthKeys)
}

// parseAuthKeys never puts key material in an error: the value is a secret.
func parseAuthKeys(raw string) ([]AuthKey, error) {
	if raw == "" {
		return nil, nil
	}
	var keys []AuthKey
	for i, entry := range strings.Split(raw, ",") {
		id, b64, ok := strings.Cut(entry, ":")
		if !ok || !keyID.MatchString(id) {
			return nil, fmt.Errorf("PITBOX_AUTH_KEYS: entry %d is not <kid>:<base64> (kid: 1-16 chars of a-z, 0-9, -)", i+1)
		}
		secret, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, fmt.Errorf("PITBOX_AUTH_KEYS: entry %d is not standard base64", i+1)
		}
		if len(secret) < minAuthKeyBytes {
			return nil, fmt.Errorf("PITBOX_AUTH_KEYS: entry %d is shorter than %d bytes", i+1, minAuthKeyBytes)
		}
		if slices.ContainsFunc(keys, func(k AuthKey) bool { return k.ID == id }) {
			return nil, fmt.Errorf("PITBOX_AUTH_KEYS: entry %d repeats the kid of an earlier entry", i+1)
		}
		keys = append(keys, AuthKey{ID: id, Secret: secret})
	}
	return keys, nil
}

func (c Config) validateAuth() []error {
	var errs []error

	if err := c.validateOrigin("PITBOX_PUBLIC_URL", c.PublicURL, false); err != nil {
		errs = append(errs, err)
	}
	if err := c.validateOrigin("PITBOX_OIDC_ISSUER", c.OIDCIssuer, true); err != nil {
		errs = append(errs, err)
	}

	if len(c.OIDCAllowedAlgs) == 0 {
		errs = append(errs, errors.New("PITBOX_OIDC_ALLOWED_ALGS: at least one algorithm is required"))
	}
	for i, alg := range c.OIDCAllowedAlgs {
		if !slices.Contains(allowedAlgs, alg) {
			errs = append(errs, fmt.Errorf("PITBOX_OIDC_ALLOWED_ALGS: %q is not one of %s", alg, strings.Join(allowedAlgs, ", ")))
		}
		if slices.Contains(c.OIDCAllowedAlgs[:i], alg) {
			errs = append(errs, fmt.Errorf("PITBOX_OIDC_ALLOWED_ALGS: %q is listed twice", alg))
		}
	}

	if err := validateAddr(c.ValkeyAddr); err != nil {
		errs = append(errs, fmt.Errorf("PITBOX_VALKEY_ADDR: %w", err))
	}
	if c.ValkeyUsername == "" {
		errs = append(errs, errors.New("PITBOX_VALKEY_USERNAME: must not be empty (the API never uses the default user)"))
	}
	if c.Env != EnvLocal && !c.ValkeyTLS {
		errs = append(errs, errors.New("PITBOX_VALKEY_TLS: the session store holds secrets and needs TLS outside local"))
	}

	if _, err := parseAuthKeys(c.AuthKeys); err != nil {
		errs = append(errs, err)
	}

	if c.SessionIdleTimeout < time.Minute {
		errs = append(errs, errors.New("PITBOX_SESSION_IDLE_TIMEOUT: must be at least 1m"))
	}
	if c.SessionAbsoluteTimeout > MaxSessionAbsolute {
		errs = append(errs, fmt.Errorf("PITBOX_SESSION_ABSOLUTE_TIMEOUT: %s is above %s (the refresh token limit in Zitadel)", c.SessionAbsoluteTimeout, MaxSessionAbsolute))
	}
	if c.SessionIdleTimeout > c.SessionAbsoluteTimeout {
		errs = append(errs, errors.New("PITBOX_SESSION_IDLE_TIMEOUT: must not exceed PITBOX_SESSION_ABSOLUTE_TIMEOUT"))
	}

	// All or nothing. Outside local, authentication is mandatory; in local it may be
	// absent, but a half-configured setup is a mistake worth failing on.
	missing := missingAuthSettings(c)
	if c.Env != EnvLocal && len(missing) > 0 {
		errs = append(errs, fmt.Errorf("authentication is mandatory outside local; missing: %s", strings.Join(missing, ", ")))
	} else if c.Env == EnvLocal && len(missing) > 0 && len(missing) < 4 {
		errs = append(errs, fmt.Errorf("authentication is partly configured; missing: %s", strings.Join(missing, ", ")))
	}
	return errs
}

func missingAuthSettings(c Config) []string {
	var missing []string
	for _, s := range []struct{ name, value string }{
		{"PITBOX_OIDC_CLIENT_ID", c.OIDCClientID},
		{"PITBOX_OIDC_CLIENT_SECRET", c.OIDCClientSecret},
		{"PITBOX_AUTH_KEYS", c.AuthKeys},
		{"PITBOX_VALKEY_PASSWORD", c.ValkeyPassword},
	} {
		if s.value == "" {
			missing = append(missing, s.name)
		}
	}
	return missing
}

// validateOrigin checks a URL that identifies a server. Outside local it must be https;
// in local, http is accepted only for the loopback host. It never carries credentials, a
// query or a fragment. The issuer may have a path; the public origin may not.
func (c Config) validateOrigin(name, raw string, allowPath bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return fmt.Errorf("%s: not a valid absolute URL", name)
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return fmt.Errorf("%s: must not contain credentials, a query or a fragment", name)
	}
	if !allowPath && u.Path != "" && u.Path != "/" {
		return fmt.Errorf("%s: must be an origin without a path", name)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if c.Env != EnvLocal {
			return fmt.Errorf("%s: plain http is only allowed when PITBOX_ENV=local", name)
		}
		switch u.Hostname() {
		case "localhost", "127.0.0.1", "::1":
		default:
			return fmt.Errorf("%s: plain http is only allowed for localhost", name)
		}
	default:
		return fmt.Errorf("%s: scheme must be https (or http for localhost)", name)
	}
	return nil
}
