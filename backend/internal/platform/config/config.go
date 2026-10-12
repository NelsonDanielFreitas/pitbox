// Package config loads and validates the process configuration from environment
// variables. Loading fails closed: an invalid or ambiguous value is an error at
// startup, never a silent default.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
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
		slog.Bool("tracing_enabled", c.TracingEnabled()),
	)
}
