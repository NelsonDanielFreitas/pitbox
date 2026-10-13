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
)

// Config is the configuration of pitbox-api.
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
		slog.Bool("tracing_enabled", c.TracingEnabled()),
	)
}
