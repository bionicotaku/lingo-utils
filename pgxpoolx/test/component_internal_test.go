package pgxpoolx_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/bionicotaku/lingo-utils/pgxpoolx"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestNewComponentConfigMatrix(t *testing.T) {
	dsn := databaseURL(t)
	type testCase struct {
		name   string
		config pgxpoolx.Config
	}
	cases := []testCase{
		{
			name:   "default params",
			config: pgxpoolx.Config{DSN: dsn},
		},
		{
			name:   "custom pool sizing",
			config: pgxpoolx.Config{DSN: dsn, MaxConns: 8, MinConns: 2},
		},
		{
			name:   "prepared statements enabled",
			config: pgxpoolx.Config{DSN: dsn, EnablePreparedStmt: boolPtr(true)},
		},
		{
			name:   "health check period",
			config: pgxpoolx.Config{DSN: dsn, HealthCheckPeriod: 30 * time.Second},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps := pgxpoolx.Dependencies{Logger: log.NewStdLogger(io.Discard)}
			comp, cleanup, err := pgxpoolx.NewComponent(context.Background(), tc.config, deps)
			require.NoError(t, err)
			defer cleanup()

			assert.NotNil(t, comp.Pool)
			cfg := comp.Pool.Config()
			if tc.config.MaxConns > 0 {
				assert.Equal(t, tc.config.MaxConns, cfg.MaxConns)
			}
			if tc.config.MinConns > 0 {
				assert.Equal(t, tc.config.MinConns, cfg.MinConns)
			}
			if tc.config.HealthCheckPeriod > 0 {
				assert.Equal(t, tc.config.HealthCheckPeriod, cfg.HealthCheckPeriod)
			}
		})
	}
}

func TestHealthCheckTimeout(t *testing.T) {
	cfg := pgxpoolx.Config{
		DSN:                "postgres://user:pass@203.0.113.1:6543/postgres",
		HealthCheckTimeout: 5 * time.Millisecond,
	}
	deps := pgxpoolx.Dependencies{Logger: log.NewStdLogger(io.Discard)}
	comp, cleanup, err := pgxpoolx.NewComponent(context.Background(), cfg, deps)
	require.Error(t, err)
	require.Nil(t, comp)
	require.Nil(t, cleanup)
}

func TestMetricsRecording(t *testing.T) {
	dsn := databaseURL(t)
	enable := true
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	cfg := pgxpoolx.Config{DSN: dsn, MetricsEnabled: &enable}
	deps := pgxpoolx.Dependencies{Logger: log.NewStdLogger(io.Discard), Meter: provider.Meter("pgxpoolx-test")}

	comp, cleanup, err := pgxpoolx.NewComponent(context.Background(), cfg, deps)
	require.NoError(t, err)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = comp.Pool.AcquireFunc(ctx, func(conn *pgxpool.Conn) error {
		_, execErr := conn.Exec(ctx, "SELECT 1")
		return execErr
	})
	require.NoError(t, err)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	foundHealth := false
	foundGauge := false

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Histogram[float64]:
				if m.Name == "db.pool.health_check.duration" && len(data.DataPoints) > 0 {
					foundHealth = true
				}
			case metricdata.Gauge[int64]:
				if m.Name == "db.pool.connections" && len(data.DataPoints) > 0 {
					foundGauge = true
				}
			}
		}
	}

	require.True(t, foundHealth, "expected health check histogram data point")
	require.True(t, foundGauge, "expected connections gauge data point")
}

func boolPtr(v bool) *bool { return &v }
