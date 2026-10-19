// Package config loads and validates the process configuration from environment
// variables. Loading fails closed: an invalid or ambiguous value is an error at
// startup, never a silent default.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

// Environments is the closed set of valid PITBOX_ENV values.
const (
	EnvLocal      = "local"
	EnvPreview    = "preview"
	EnvStaging    = "staging"
	EnvProduction = "production"
)

const (
	// TelemetryFlushTimeout is the time reserved at the end of the shutdown to
	// flush pending spans. It is a constant, not configuration, because it is
	// part of the shutdown budget below.
	TelemetryFlushTimeout = 2 * time.Second

	// MaxShutdownBudget is the longest the whole shutdown (drain + HTTP shutdown +
	// telemetry flush) may take. Cloud Run sends SIGKILL 10 s after SIGTERM, so the
	// budget has to fit inside that window.
	MaxShutdownBudget = 10 * time.Second

	// MaxRPCTimeout is the architecture's ceiling for a unary RPC (§4.4).
	MaxRPCTimeout = 30 * time.Second
	// MaxRPCStreamDuration is the longest a streaming RPC may be configured to run: the
	// Cloud Run request timeout is at most 60 min (to confirm in F5-04).
	MaxRPCStreamDuration = 60 * time.Minute

	// MaxSessionAbsolute is the longest a web session may live. It matches the
	// absolute refresh token expiration configured in Zitadel (720 h, ADR 0013): a
	// longer session could not be renewed anyway.
	MaxSessionAbsolute = 720 * time.Hour

	// MaxRateLimitRPS is the ceiling for any rate limit. The GCRA script in Valkey
	// uses millisecond precision; above 1000 RPS the increment rounds to zero.
	MaxRateLimitRPS = 1000
)

// Config is the configuration shared by pitbox-api and pitbox-worker.
//
// Go note: the `env` struct tags are read by reflection at runtime. A typo in a
// tag compiles fine, which is why Validate and the tests in this package cover
// every field.
type Config struct {
	Env      string `env:"PITBOX_ENV" envDefault:"local"`
	HTTPAddr string `env:"PITBOX_HTTP_ADDR" envDefault:"127.0.0.1:8081"`
	LogLevel string `env:"PITBOX_LOG_LEVEL" envDefault:"info"`

	// ShutdownDrain is how long the server keeps serving, already reporting "not
	// ready", before it stops accepting connections. 0 locally; a few seconds on
	// Kubernetes so endpoints stop receiving traffic first.
	ShutdownDrain time.Duration `env:"PITBOX_SHUTDOWN_DRAIN" envDefault:"0s"`
	// ShutdownTimeout bounds http.Server.Shutdown.
	ShutdownTimeout time.Duration `env:"PITBOX_SHUTDOWN_TIMEOUT" envDefault:"6s"`

	// RPCMaxTimeout is both the default deadline of a unary RPC (when the client sends
	// none) and its ceiling (a longer client deadline is cut to it). Arquitetura §4.4.
	RPCMaxTimeout time.Duration `env:"PITBOX_RPC_MAX_TIMEOUT" envDefault:"30s"`
	// RPCStreamMaxDuration is the ceiling for streaming RPCs. It must stay below the
	// platform's request timeout (Cloud Run: at most 60 min), so that this limit, not
	// the platform, is the one that acts first.
	RPCStreamMaxDuration time.Duration `env:"PITBOX_RPC_STREAM_MAX_DURATION" envDefault:"55m"`

	// ClientIPMode says where the client address recorded in the audit metadata comes
	// from: "peer" (the TCP peer; the default) or "header:<Name>" (for example
	// "header:CF-Connecting-IP"). A header is client-controlled data unless every
	// request comes through a proxy that sets it, so header mode needs
	// OriginCloudflareOnly (see Validate).
	ClientIPMode string `env:"PITBOX_CLIENT_IP_MODE" envDefault:"peer"`
	// OriginCloudflareOnly declares that the origin only accepts traffic from the
	// Cloudflare edge (F5-05). It must stay false until that is true.
	OriginCloudflareOnly bool `env:"PITBOX_ORIGIN_CLOUDFLARE_ONLY" envDefault:"false"`

	// Authentication (F1-03, ADR 0013). Nothing here has a safe default outside local
	// development: Validate requires all of it in staging, preview and production.

	// PublicURL is the origin the browser sees for pitbox-api (scheme and host, no path).
	// The OIDC redirect URI is PublicURL + "/auth/callback".
	PublicURL string `env:"PITBOX_PUBLIC_URL" envDefault:"http://localhost:8081"`
	// OIDCIssuer is the Zitadel issuer, compared exactly with the "iss" of the ID token.
	OIDCIssuer       string `env:"PITBOX_OIDC_ISSUER" envDefault:"http://localhost:8080"`
	OIDCClientID     string `env:"PITBOX_OIDC_CLIENT_ID"`
	OIDCClientSecret string `env:"PITBOX_OIDC_CLIENT_SECRET"`
	// OIDCAllowedAlgs are the ID token signature algorithms accepted (cryptographic
	// agility: change the list, not the code). "none" and HS* are never valid.
	OIDCAllowedAlgs []string `env:"PITBOX_OIDC_ALLOWED_ALGS" envSeparator:"," envDefault:"ES256"`

	ValkeyAddr     string `env:"PITBOX_VALKEY_ADDR" envDefault:"127.0.0.1:6379"`
	ValkeyUsername string `env:"PITBOX_VALKEY_USERNAME" envDefault:"pitbox_api"`
	ValkeyPassword string `env:"PITBOX_VALKEY_PASSWORD"`
	ValkeyTLS      bool   `env:"PITBOX_VALKEY_TLS" envDefault:"false"`

	// DatabaseURL is the PostgreSQL connection string of the application role (pitbox_api,
	// never the owner or the migrator). It carries the password: a secret.
	DatabaseURL string `env:"PITBOX_DATABASE_URL"`

	// AuthKeys are the master keys for sealing cookies and refresh tokens, as
	// "kid:base64,kid:base64". The first one seals; the others only open (rotation).
	// Read them with AuthKeyring.
	AuthKeys string `env:"PITBOX_AUTH_KEYS"`

	// SessionIdleTimeout and SessionAbsoluteTimeout bound a web session (ADR 0013).
	SessionIdleTimeout     time.Duration `env:"PITBOX_SESSION_IDLE_TIMEOUT" envDefault:"168h"`
	SessionAbsoluteTimeout time.Duration `env:"PITBOX_SESSION_ABSOLUTE_TIMEOUT" envDefault:"720h"`

	// Rate limiting (F1-04b, ADR 0014). Local limits are higher than the distributed
	// Valkey limits so the local bucket does not reject requests that Valkey would allow.
	RateLimitUserReadRPS    int           `env:"PITBOX_RL_USER_READ_RPS"    envDefault:"20"`
	RateLimitUserReadBurst  int           `env:"PITBOX_RL_USER_READ_BURST"  envDefault:"30"`
	RateLimitUserWriteRPS   int           `env:"PITBOX_RL_USER_WRITE_RPS"   envDefault:"5"`
	RateLimitUserWriteBurst int           `env:"PITBOX_RL_USER_WRITE_BURST" envDefault:"10"`
	RateLimitTeamReadRPS    int           `env:"PITBOX_RL_TEAM_READ_RPS"    envDefault:"100"`
	RateLimitTeamReadBurst  int           `env:"PITBOX_RL_TEAM_READ_BURST"  envDefault:"150"`
	RateLimitTeamWriteRPS   int           `env:"PITBOX_RL_TEAM_WRITE_RPS"   envDefault:"20"`
	RateLimitTeamWriteBurst int           `env:"PITBOX_RL_TEAM_WRITE_BURST" envDefault:"40"`
	RateLimitPublicRPS      int           `env:"PITBOX_RL_PUBLIC_RPS"       envDefault:"10"`
	RateLimitPublicBurst    int           `env:"PITBOX_RL_PUBLIC_BURST"     envDefault:"20"`
	RateLimitValkeyTimeout  time.Duration `env:"PITBOX_RL_VALKEY_TIMEOUT"   envDefault:"50ms"`

	// Authorization (F1-05, ADR 0005). OpenFGA is the primary authorization decision point
	// before RLS enforces isolation in Postgres. Outside local development, TLS and a
	// pre-shared key are mandatory; the store and model IDs should be explicit.
	OpenFGAAddr         string        `env:"PITBOX_OPENFGA_ADDR"          envDefault:"127.0.0.1:8091"`
	OpenFGAPresharedKey string        `env:"PITBOX_OPENFGA_PRESHARED_KEY"`
	OpenFGATLSEnabled   bool          `env:"PITBOX_OPENFGA_TLS"           envDefault:"false"`
	OpenFGATimeout      time.Duration `env:"PITBOX_OPENFGA_TIMEOUT"       envDefault:"200ms"`
	OpenFGAStoreID      string        `env:"PITBOX_OPENFGA_STORE_ID"`
	OpenFGAModelID      string        `env:"PITBOX_OPENFGA_MODEL_ID"`

	// Outbox worker (F1-05b, ADR 0015). The lease bounds how long an event is
	// hidden while one instance applies it; the backoff keeps a failing event (and
	// the following ones of its key) waiting. The alert is a log/metric threshold.
	OutboxPollInterval  time.Duration `env:"PITBOX_OUTBOX_POLL_INTERVAL"  envDefault:"1s"`
	OutboxBatchSize     int           `env:"PITBOX_OUTBOX_BATCH_SIZE"     envDefault:"20"`
	OutboxLease         time.Duration `env:"PITBOX_OUTBOX_LEASE"          envDefault:"30s"`
	OutboxMaxBackoff    time.Duration `env:"PITBOX_OUTBOX_MAX_BACKOFF"    envDefault:"5m"`
	OutboxAlertAttempts int           `env:"PITBOX_OUTBOX_ALERT_ATTEMPTS" envDefault:"5"`

	// The standard OpenTelemetry variables are read here only to validate them:
	// the SDK reads them again itself. Tracing is off when neither is set.
	OTLPEndpoint       string `env:"OTEL_EXPORTER_OTLP_ENDPOINT"`
	OTLPTracesEndpoint string `env:"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"`
}

// Load reads the configuration from the process environment.
func Load() (Config, error) { return LoadFrom(nil) }

// LoadFrom reads the configuration from the given variables (nil means the
// process environment). Tests use it to avoid touching the real environment.
func LoadFrom(environment map[string]string) (Config, error) {
	var c Config
	if err := env.ParseWithOptions(&c, env.Options{Environment: environment}); err != nil {
		return Config{}, fmt.Errorf("parse environment: %w", err)
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

// TracingEnabled reports whether an OTLP endpoint is configured.
func (c Config) TracingEnabled() bool {
	return c.OTLPEndpoint != "" || c.OTLPTracesEndpoint != ""
}

// SlogLevel returns the parsed log level. Validate guarantees it is valid.
func (c Config) SlogLevel() slog.Level {
	var l slog.Level
	_ = l.UnmarshalText([]byte(c.LogLevel)) // validated in Validate
	return l
}

// Validate checks the semantic rules the env library cannot express.
func (c Config) Validate() error {
	var errs []error

	switch c.Env {
	case EnvLocal, EnvPreview, EnvStaging, EnvProduction:
	default:
		errs = append(errs, fmt.Errorf("PITBOX_ENV: %q is not one of local, preview, staging, production", c.Env))
	}

	if err := validateAddr(c.HTTPAddr); err != nil {
		errs = append(errs, fmt.Errorf("PITBOX_HTTP_ADDR: %w", err))
	}

	var l slog.Level
	if err := l.UnmarshalText([]byte(c.LogLevel)); err != nil {
		errs = append(errs, fmt.Errorf("PITBOX_LOG_LEVEL: %q is not one of debug, info, warn, error", c.LogLevel))
	}

	if c.ShutdownDrain < 0 {
		errs = append(errs, errors.New("PITBOX_SHUTDOWN_DRAIN: must not be negative"))
	}
	if c.ShutdownTimeout <= 0 {
		errs = append(errs, errors.New("PITBOX_SHUTDOWN_TIMEOUT: must be positive"))
	}
	if total := c.ShutdownDrain + c.ShutdownTimeout + TelemetryFlushTimeout; total > MaxShutdownBudget {
		errs = append(errs, fmt.Errorf("shutdown budget %s (drain + timeout + %s telemetry flush) exceeds %s",
			total, TelemetryFlushTimeout, MaxShutdownBudget))
	}

	if c.RPCMaxTimeout < time.Second || c.RPCMaxTimeout > MaxRPCTimeout {
		errs = append(errs, fmt.Errorf("PITBOX_RPC_MAX_TIMEOUT: %s is outside 1s..%s", c.RPCMaxTimeout, MaxRPCTimeout))
	}
	if c.RPCStreamMaxDuration <= 0 || c.RPCStreamMaxDuration > MaxRPCStreamDuration {
		errs = append(errs, fmt.Errorf("PITBOX_RPC_STREAM_MAX_DURATION: %s is outside (0, %s]", c.RPCStreamMaxDuration, MaxRPCStreamDuration))
	}
	if err := c.validateClientIPMode(); err != nil {
		errs = append(errs, err)
	}
	errs = append(errs, c.validateAuth()...)
	errs = append(errs, c.validateRateLimit()...)
	errs = append(errs, c.validateOpenFGA()...)
	errs = append(errs, c.validateOutbox()...)

	// Telemetry must not travel in clear text outside local development.
	if c.Env != EnvLocal {
		for _, e := range []struct{ name, value string }{
			{"OTEL_EXPORTER_OTLP_ENDPOINT", c.OTLPEndpoint},
			{"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", c.OTLPTracesEndpoint},
		} {
			if strings.HasPrefix(strings.ToLower(e.value), "http://") {
				errs = append(errs, fmt.Errorf("%s: plain http is only allowed when PITBOX_ENV=local", e.name))
			}
		}
	}

	return errors.Join(errs...)
}

var headerName = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

// ClientIPHeader returns the header the client address is read from, or "" when
// the TCP peer is used.
func (c Config) ClientIPHeader() string {
	name, _ := strings.CutPrefix(c.ClientIPMode, "header:")
	if name == c.ClientIPMode { // not in header mode
		return ""
	}
	return name
}

func (c Config) validateClientIPMode() error {
	if c.ClientIPMode == "peer" {
		return nil
	}
	name, ok := strings.CutPrefix(c.ClientIPMode, "header:")
	if !ok || !headerName.MatchString(name) {
		return fmt.Errorf("PITBOX_CLIENT_IP_MODE: %q is neither \"peer\" nor \"header:<Name>\"", c.ClientIPMode)
	}
	if c.Env == EnvLocal || !c.OriginCloudflareOnly {
		return errors.New("PITBOX_CLIENT_IP_MODE: header mode needs PITBOX_ORIGIN_CLOUDFLARE_ONLY=true outside local (a client-controlled header must not be trusted otherwise)")
	}
	return nil
}

func (c Config) validateRateLimit() []error {
	var errs []error
	for _, r := range []struct {
		name, kind string
		rps, burst int
	}{
		{"PITBOX_RL_USER_READ", "user read", c.RateLimitUserReadRPS, c.RateLimitUserReadBurst},
		{"PITBOX_RL_USER_WRITE", "user write", c.RateLimitUserWriteRPS, c.RateLimitUserWriteBurst},
		{"PITBOX_RL_TEAM_READ", "team read", c.RateLimitTeamReadRPS, c.RateLimitTeamReadBurst},
		{"PITBOX_RL_TEAM_WRITE", "team write", c.RateLimitTeamWriteRPS, c.RateLimitTeamWriteBurst},
		{"PITBOX_RL_PUBLIC", "public", c.RateLimitPublicRPS, c.RateLimitPublicBurst},
	} {
		if r.rps <= 0 || r.rps > MaxRateLimitRPS {
			errs = append(errs, fmt.Errorf("%s_RPS: %s rate must be between 1 and %d", r.name, r.kind, MaxRateLimitRPS))
		}
		if r.burst <= 0 {
			errs = append(errs, fmt.Errorf("%s_BURST: %s burst must be positive", r.name, r.kind))
		}
		if r.burst < r.rps {
			errs = append(errs, fmt.Errorf("%s_BURST: %s burst (%d) must be at least the rate (%d)", r.name, r.kind, r.burst, r.rps))
		}
	}
	if c.RateLimitValkeyTimeout <= 0 || c.RateLimitValkeyTimeout > 200*time.Millisecond {
		errs = append(errs, fmt.Errorf("PITBOX_RL_VALKEY_TIMEOUT: %s is outside (0, 200ms]", c.RateLimitValkeyTimeout))
	}
	return errs
}

func (c Config) validateOpenFGA() []error {
	var errs []error
	if err := validateAddr(c.OpenFGAAddr); err != nil {
		errs = append(errs, fmt.Errorf("PITBOX_OPENFGA_ADDR: %w", err))
	}
	if c.OpenFGATimeout < 50*time.Millisecond || c.OpenFGATimeout > time.Second {
		errs = append(errs, fmt.Errorf("PITBOX_OPENFGA_TIMEOUT: %s is outside 50ms..1s", c.OpenFGATimeout))
	}
	if c.Env != EnvLocal {
		if !c.OpenFGATLSEnabled {
			errs = append(errs, errors.New("PITBOX_OPENFGA_TLS: required outside local development"))
		}
		if c.OpenFGAPresharedKey == "" {
			errs = append(errs, errors.New("PITBOX_OPENFGA_PRESHARED_KEY: required outside local development"))
		}
		if c.OpenFGAStoreID == "" {
			errs = append(errs, errors.New("PITBOX_OPENFGA_STORE_ID: required outside local development"))
		}
	}
	return errs
}

func (c Config) validateOutbox() []error {
	var errs []error
	if c.OutboxPollInterval < 100*time.Millisecond || c.OutboxPollInterval > time.Minute {
		errs = append(errs, fmt.Errorf("PITBOX_OUTBOX_POLL_INTERVAL: %s is outside 100ms..1m", c.OutboxPollInterval))
	}
	if c.OutboxBatchSize < 1 || c.OutboxBatchSize > 200 {
		errs = append(errs, fmt.Errorf("PITBOX_OUTBOX_BATCH_SIZE: %d is outside 1..200", c.OutboxBatchSize))
	}
	if c.OutboxLease < time.Second || c.OutboxLease > 5*time.Minute {
		errs = append(errs, fmt.Errorf("PITBOX_OUTBOX_LEASE: %s is outside 1s..5m", c.OutboxLease))
	}
	if c.OutboxMaxBackoff < 30*time.Second || c.OutboxMaxBackoff > time.Hour {
		errs = append(errs, fmt.Errorf("PITBOX_OUTBOX_MAX_BACKOFF: %s is outside 30s..1h", c.OutboxMaxBackoff))
	}
	if c.OutboxAlertAttempts < 1 || c.OutboxAlertAttempts > 100 {
		errs = append(errs, fmt.Errorf("PITBOX_OUTBOX_ALERT_ATTEMPTS: %d is outside 1..100", c.OutboxAlertAttempts))
	}
	return errs
}

func validateAddr(addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%q is not host:port: %w", addr, err)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 0 || n > 65535 {
		return fmt.Errorf("%q has an invalid port", addr)
	}
	return nil
}

// LogValue implements slog.LogValuer with an allow-list: only the fields listed
// here are ever logged. A new field (for example a secret added by the login
// card) is hidden by default and has to be added here on purpose.
//
// The OTLP endpoints are deliberately not logged: a URL can carry credentials.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("env", c.Env),
		slog.String("http_addr", c.HTTPAddr),
		slog.String("log_level", c.LogLevel),
		slog.Duration("shutdown_drain", c.ShutdownDrain),
		slog.Duration("shutdown_timeout", c.ShutdownTimeout),
		slog.Duration("rpc_max_timeout", c.RPCMaxTimeout),
		slog.Duration("rpc_stream_max_duration", c.RPCStreamMaxDuration),
		slog.String("client_ip_mode", c.ClientIPMode),
		slog.Bool("auth_configured", c.AuthConfigured()),
		slog.String("public_url", c.PublicURL),
		slog.String("oidc_issuer", c.OIDCIssuer),
		slog.Duration("session_idle_timeout", c.SessionIdleTimeout),
		slog.Duration("session_absolute_timeout", c.SessionAbsoluteTimeout),
		slog.Int("rl_user_read_rps", c.RateLimitUserReadRPS),
		slog.Int("rl_user_read_burst", c.RateLimitUserReadBurst),
		slog.Int("rl_user_write_rps", c.RateLimitUserWriteRPS),
		slog.Int("rl_user_write_burst", c.RateLimitUserWriteBurst),
		slog.Int("rl_team_read_rps", c.RateLimitTeamReadRPS),
		slog.Int("rl_team_read_burst", c.RateLimitTeamReadBurst),
		slog.Int("rl_team_write_rps", c.RateLimitTeamWriteRPS),
		slog.Int("rl_team_write_burst", c.RateLimitTeamWriteBurst),
		slog.Int("rl_public_rps", c.RateLimitPublicRPS),
		slog.Int("rl_public_burst", c.RateLimitPublicBurst),
		slog.Duration("rl_valkey_timeout", c.RateLimitValkeyTimeout),
		slog.String("openfga_addr", c.OpenFGAAddr),
		slog.Bool("openfga_tls", c.OpenFGATLSEnabled),
		slog.Duration("openfga_timeout", c.OpenFGATimeout),
		slog.Bool("openfga_store_id_set", c.OpenFGAStoreID != ""),
		slog.Bool("openfga_model_id_set", c.OpenFGAModelID != ""),
		slog.Duration("outbox_poll_interval", c.OutboxPollInterval),
		slog.Int("outbox_batch_size", c.OutboxBatchSize),
		slog.Duration("outbox_lease", c.OutboxLease),
		slog.Duration("outbox_max_backoff", c.OutboxMaxBackoff),
		slog.Int("outbox_alert_attempts", c.OutboxAlertAttempts),
		slog.Bool("tracing_enabled", c.TracingEnabled()),
	)
}

// String and GoString keep the secrets out of fmt verbs (%v, %+v, %#v) and of any
// code that prints the configuration by accident: Config holds the OIDC client
// secret, the Valkey password and the master keys.
func (Config) String() string   { return "config.Config{<redacted>}" }
func (Config) GoString() string { return "config.Config{<redacted>}" }
