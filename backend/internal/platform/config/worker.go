package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

// WorkerConfig is the configuration of pitbox-worker. It deliberately does not parse
// the API's authentication or session settings: the worker never uses them, and a
// compromised worker must not hold the keys that seal sessions (least privilege,
// NIST SP 800-207). The worker only needs the database, OpenFGA, telemetry and its
// own polling settings.
type WorkerConfig struct {
	Env      string `env:"PITBOX_ENV" envDefault:"local"`
	LogLevel string `env:"PITBOX_LOG_LEVEL" envDefault:"info"`

	// DatabaseURL is the PostgreSQL connection string of the application role. The
	// password is a secret: no error or log repeats it.
	DatabaseURL string `env:"PITBOX_DATABASE_URL"`

	// OpenFGA is the same authorization store the API uses.
	OpenFGAAddr         string        `env:"PITBOX_OPENFGA_ADDR"          envDefault:"127.0.0.1:8091"`
	OpenFGAPresharedKey string        `env:"PITBOX_OPENFGA_PRESHARED_KEY"`
	OpenFGATLSEnabled   bool          `env:"PITBOX_OPENFGA_TLS"           envDefault:"false"`
	OpenFGATimeout      time.Duration `env:"PITBOX_OPENFGA_TIMEOUT"       envDefault:"200ms"`
	OpenFGAStoreID      string        `env:"PITBOX_OPENFGA_STORE_ID"`
	OpenFGAModelID      string        `env:"PITBOX_OPENFGA_MODEL_ID"`

	// Outbox polling (ADR 0015).
	OutboxPollInterval  time.Duration `env:"PITBOX_OUTBOX_POLL_INTERVAL"  envDefault:"1s"`
	OutboxBatchSize     int           `env:"PITBOX_OUTBOX_BATCH_SIZE"     envDefault:"20"`
	OutboxLease         time.Duration `env:"PITBOX_OUTBOX_LEASE"          envDefault:"30s"`
	OutboxMaxBackoff    time.Duration `env:"PITBOX_OUTBOX_MAX_BACKOFF"    envDefault:"5m"`
	OutboxAlertAttempts int           `env:"PITBOX_OUTBOX_ALERT_ATTEMPTS" envDefault:"5"`

	// The standard OpenTelemetry variables are read here only to validate them.
	OTLPEndpoint       string `env:"OTEL_EXPORTER_OTLP_ENDPOINT"`
	OTLPTracesEndpoint string `env:"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"`
}

// LoadWorker reads and validates the worker configuration from the process environment.
func LoadWorker() (WorkerConfig, error) { return LoadWorkerFrom(nil) }

// LoadWorkerFrom reads the worker configuration from the given variables (nil means
// the process environment). Tests use it to avoid touching the real environment.
func LoadWorkerFrom(environment map[string]string) (WorkerConfig, error) {
	var c WorkerConfig
	if err := env.ParseWithOptions(&c, env.Options{Environment: environment}); err != nil {
		return WorkerConfig{}, fmt.Errorf("parse environment: %w", err)
	}
	if err := c.Validate(); err != nil {
		return WorkerConfig{}, err
	}
	return c, nil
}

// Validate checks only what the worker uses. In particular, it does not require
// PITBOX_AUTH_KEYS, the OIDC client secret or the Valkey password: those belong to
// the API and must not be mounted into the worker.
func (c WorkerConfig) Validate() error {
	var errs []error

	switch c.Env {
	case EnvLocal, EnvPreview, EnvStaging, EnvProduction:
	default:
		errs = append(errs, fmt.Errorf("PITBOX_ENV: %q is not one of local, preview, staging, production", c.Env))
	}

	var l slog.Level
	if err := l.UnmarshalText([]byte(c.LogLevel)); err != nil {
		errs = append(errs, fmt.Errorf("PITBOX_LOG_LEVEL: %q is not one of debug, info, warn, error", c.LogLevel))
	}

	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("PITBOX_DATABASE_URL: required"))
	} else if err := validateDatabaseURL(c.Env, c.DatabaseURL); err != nil {
		errs = append(errs, err)
	}

	errs = append(errs, validateOpenFGASettings(c.Env, c.OpenFGAAddr, c.OpenFGATimeout, c.OpenFGATLSEnabled, c.OpenFGAPresharedKey, c.OpenFGAStoreID)...)
	errs = append(errs, validateOutboxSettings(c.OutboxPollInterval, c.OutboxBatchSize, c.OutboxLease, c.OutboxMaxBackoff, c.OutboxAlertAttempts)...)

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

// Environment returns the deployment environment (for the telemetry resource).
func (c WorkerConfig) Environment() string { return c.Env }

// TracingEnabled reports whether an OTLP endpoint is configured.
func (c WorkerConfig) TracingEnabled() bool {
	return c.OTLPEndpoint != "" || c.OTLPTracesEndpoint != ""
}

// SlogLevel returns the parsed log level. Validate guarantees it is valid.
func (c WorkerConfig) SlogLevel() slog.Level {
	var l slog.Level
	_ = l.UnmarshalText([]byte(c.LogLevel)) // validated in Validate
	return l
}

// LogValue implements slog.LogValuer with an allow-list of the fields the worker
// uses. Authentication, session and rate-limit settings are not part of WorkerConfig
// and therefore never appear here.
func (c WorkerConfig) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("env", c.Env),
		slog.String("log_level", c.LogLevel),
		slog.Bool("database_url_set", c.DatabaseURL != ""),
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

// String and GoString keep the database password out of fmt verbs and accidental prints.
func (WorkerConfig) String() string   { return "config.WorkerConfig{<redacted>}" }
func (WorkerConfig) GoString() string { return "config.WorkerConfig{<redacted>}" }
