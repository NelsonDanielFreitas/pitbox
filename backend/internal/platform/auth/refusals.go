package auth

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// RefusalCounter counts the callbacks refused before their state was validated, by reason.
// It is the only trace of that traffic besides the log line: nothing unauthenticated writes to
// the database.
type RefusalCounter interface {
	Count(ctx context.Context, reason string)
}

type otelRefusals struct{ c metric.Int64Counter }

// NewRefusalCounter creates the counter pitbox.auth.refused_logins{reason} on the meter. The
// instrument is a no-op until a MeterProvider is installed (F5-07).
func NewRefusalCounter(m metric.Meter) (RefusalCounter, error) {
	c, err := m.Int64Counter("pitbox.auth.refused_logins",
		metric.WithDescription("Login callbacks refused before their state was validated, by reason"))
	if err != nil {
		return nil, fmt.Errorf("create refusal counter: %w", err)
	}
	return otelRefusals{c}, nil
}

func (o otelRefusals) Count(ctx context.Context, reason string) {
	o.c.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
}
