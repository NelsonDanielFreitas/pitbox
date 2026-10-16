package ratelimit

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// metrics holds the OpenTelemetry instruments for rate limiting.
type metrics struct {
	rejections  metric.Int64Counter
	valkeyErrs  metric.Int64Counter
	breakerOpen metric.Int64ObservableGauge
}

func newMetrics(meter metric.Meter, br *breaker) (*metrics, error) {
	rejections, err := meter.Int64Counter("pitbox.ratelimit.rejected",
		metric.WithDescription("RPCs rejected by rate limiting, by scope, write/read and layer"))
	if err != nil {
		return nil, fmt.Errorf("rejection counter: %w", err)
	}

	valkeyErrs, err := meter.Int64Counter("pitbox.ratelimit.valkey.errors",
		metric.WithDescription("Valkey errors during rate limit checks"))
	if err != nil {
		return nil, fmt.Errorf("valkey error counter: %w", err)
	}

	gauge, err := meter.Int64ObservableGauge("pitbox.ratelimit.breaker.open",
		metric.WithDescription("1 when the Valkey circuit breaker is open, 0 otherwise"))
	if err != nil {
		return nil, fmt.Errorf("breaker gauge: %w", err)
	}

	m := &metrics{rejections: rejections, valkeyErrs: valkeyErrs, breakerOpen: gauge}
	_, err = meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		v := int64(0)
		if br.currentState() == breakerOpen {
			v = 1
		}
		o.ObserveInt64(gauge, v)
		return nil
	}, gauge)
	if err != nil {
		return nil, fmt.Errorf("register breaker callback: %w", err)
	}

	return m, nil
}

func (m *metrics) recordRejection(scope Scope, write bool, layer string) {
	m.rejections.Add(context.Background(), 1,
		metric.WithAttributes(
			attribute.String("scope", scope.String()),
			attribute.Bool("write", write),
			attribute.String("layer", layer),
		))
}

func (m *metrics) recordValkeyError() {
	m.valkeyErrs.Add(context.Background(), 1)
}
