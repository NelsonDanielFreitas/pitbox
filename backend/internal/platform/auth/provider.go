package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// ErrProviderUnavailable means the identity provider's discovery has not succeeded yet.
// /auth/login answers 503; /healthz is not affected.
var ErrProviderUnavailable = errors.New("auth: identity provider unavailable")

// ProviderConfig is what the OIDC client needs.
type ProviderConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	// AllowedAlgs are the ID token signature algorithms accepted. go-oidc rejects any other,
	// "none" and HS* included.
	AllowedAlgs []string
	HTTPClient  *http.Client
	Now         func() time.Time
}

// ready is the result of a successful discovery.
type ready struct {
	provider   *oidc.Provider // for the userinfo endpoint
	oauth      oauth2.Config
	verifier   *oidc.IDTokenVerifier
	endSession string
	revocation string
}

// Provider discovers the identity provider lazily: the process starts without it, and the
// first uses retry with a backoff (5 s doubling to 60 s), never hammering a server that
// is down.
type Provider struct {
	cfg ProviderConfig

	busy    sync.Mutex // held during a discovery; others do not wait for it
	mu      sync.Mutex
	ok      *ready
	retryAt time.Time
	fails   int
}

// NewProvider does not contact the identity provider.
func NewProvider(cfg ProviderConfig) *Provider {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Provider{cfg: cfg}
}

func (p *Provider) get() (*ready, error) {
	p.mu.Lock()
	if p.ok != nil {
		r := p.ok
		p.mu.Unlock()
		return r, nil
	}
	if p.cfg.Now().Before(p.retryAt) {
		p.mu.Unlock()
		return nil, ErrProviderUnavailable
	}
	p.mu.Unlock()

	if !p.busy.TryLock() {
		return nil, ErrProviderUnavailable // someone is already discovering
	}
	defer p.busy.Unlock()

	// A long-lived context: go-oidc keeps it to fetch the keys later, so it must not be a
	// request's. The HTTP client carries the timeout.
	ctx := oidc.ClientContext(context.Background(), p.cfg.HTTPClient)
	r, err := p.discover(ctx)
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		p.fails++
		backoff := min(5*time.Second<<min(p.fails-1, 4), time.Minute)
		p.retryAt = p.cfg.Now().Add(backoff)
		return nil, fmt.Errorf("%w: %w", ErrProviderUnavailable, err)
	}
	p.ok, p.fails = r, 0
	return r, nil
}

func (p *Provider) discover(ctx context.Context) (*ready, error) {
	prov, err := oidc.NewProvider(ctx, p.cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("discovery: %w", err)
	}
	var extra struct {
		EndSession string `json:"end_session_endpoint"`
		Revocation string `json:"revocation_endpoint"`
	}
	if err := prov.Claims(&extra); err != nil {
		return nil, fmt.Errorf("discovery claims: %w", err)
	}
	for _, e := range []string{extra.EndSession, extra.Revocation} {
		if !sameHost(p.cfg.Issuer, e) {
			return nil, errors.New("discovery: an endpoint is not on the issuer's host")
		}
	}
	endpoint := prov.Endpoint()
	endpoint.AuthStyle = oauth2.AuthStyleInHeader // client_secret_basic
	return &ready{
		provider: prov,
		oauth: oauth2.Config{
			ClientID: p.cfg.ClientID, ClientSecret: p.cfg.ClientSecret, Endpoint: endpoint,
			RedirectURL: p.cfg.RedirectURL, Scopes: []string{oidc.ScopeOpenID, "profile", "email", oidc.ScopeOfflineAccess},
		},
		verifier: prov.VerifierContext(ctx, &oidc.Config{
			ClientID: p.cfg.ClientID, SupportedSigningAlgs: p.cfg.AllowedAlgs, Now: p.cfg.Now,
		}),
		endSession: extra.EndSession,
		revocation: extra.Revocation,
	}, nil
}

// sameHost reports whether endpoint is an absolute URL on the same scheme and host as issuer.
func sameHost(issuer, endpoint string) bool {
	a, err1 := url.Parse(issuer)
	b, err2 := url.Parse(endpoint)
	return err1 == nil && err2 == nil && a.Scheme == b.Scheme && a.Host == b.Host
}

// withHTTP returns ctx carrying the HTTP client the oauth2 package uses.
func (p *Provider) withHTTP(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, p.cfg.HTTPClient)
}
