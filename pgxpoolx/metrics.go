package pgxpoolx

import (
	"context"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type poolTelemetry struct {
	helper        *log.Helper
	acquire       metric.Float64Histogram
	healthLatency metric.Float64Histogram
	healthFail    metric.Int64Counter
	registration  metric.Registration
	enabled       bool
}

func newPoolTelemetry(meter metric.Meter, helper *log.Helper, pool *pgxpool.Pool) *poolTelemetry {
	t := &poolTelemetry{helper: helper}
	if meter == nil || helper == nil || pool == nil {
		return t
	}

	var err error

	t.acquire, err = meter.Float64Histogram("db.pool.acquire_duration", metric.WithUnit("ms"))
	if err != nil {
		helper.Warnf("pgxpoolx: acquire histogram error: %v", err)
	}

	t.healthLatency, err = meter.Float64Histogram("db.pool.health_check.duration", metric.WithUnit("ms"))
	if err != nil {
		helper.Warnf("pgxpoolx: health check histogram error: %v", err)
	}

	t.healthFail, err = meter.Int64Counter("db.pool.health_check.failures")
	if err != nil {
		helper.Warnf("pgxpoolx: health check counter error: %v", err)
	}

	connectionsGauge, err := meter.Int64ObservableGauge("db.pool.connections")
	if err != nil {
		helper.Warnf("pgxpoolx: connections gauge error: %v", err)
	} else {
		reg, regErr := meter.RegisterCallback(func(ctx context.Context, observer metric.Observer) error {
			stats := pool.Stat()
			observer.ObserveInt64(connectionsGauge, int64(stats.AcquiredConns()),
				metric.WithAttributes(attribute.String("state", "active")))
			observer.ObserveInt64(connectionsGauge, int64(stats.IdleConns()),
				metric.WithAttributes(attribute.String("state", "idle")))
			observer.ObserveInt64(connectionsGauge, int64(stats.TotalConns()),
				metric.WithAttributes(attribute.String("state", "total")))
			return nil
		}, connectionsGauge)
		if regErr != nil {
			helper.Warnf("pgxpoolx: register connections callback: %v", regErr)
		} else {
			t.registration = reg
		}
	}

	t.enabled = true
	return t
}

func (t *poolTelemetry) recordAcquire(ctx context.Context, elapsed time.Duration) {
	if t == nil || !t.enabled || t.acquire == nil {
		return
	}
	t.acquire.Record(ctx, float64(elapsed.Milliseconds()))
}

func (t *poolTelemetry) recordHealthCheck(ctx context.Context, elapsed time.Duration, err error) {
	if t == nil || !t.enabled {
		return
	}
	if t.healthLatency != nil {
		t.healthLatency.Record(ctx, float64(elapsed.Milliseconds()))
	}
	if err != nil && t.healthFail != nil {
		t.healthFail.Add(ctx, 1)
	}
}

func (t *poolTelemetry) shutdown(ctx context.Context) {
	if t == nil || !t.enabled {
		return
	}
	if t.registration != nil {
		if unregisterErr := t.registration.Unregister(); unregisterErr != nil {
			t.helper.Warnf("pgxpoolx: unregister connections callback: %v", unregisterErr)
		}
	}
}
