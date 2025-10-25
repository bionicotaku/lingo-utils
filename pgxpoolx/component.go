package pgxpoolx

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Component aggregates the constructed pgx connection pool along with helper
// facilities used during cleanup.
type Component struct {
	Pool    *pgxpool.Pool
	helper  *log.Helper
	metrics *poolTelemetry
}

// NewComponent builds and validates a PostgreSQL connection pool according to
// the provided configuration and dependencies.
func NewComponent(ctx context.Context, cfg Config, deps Dependencies) (*Component, func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}

	sanitized, err := cfg.Sanitize()
	if err != nil {
		return nil, nil, err
	}

	dep := sanitizeDependencies(deps)
	helper := log.NewHelper(dep.logger)

	poolConfig, err := pgxpool.ParseConfig(sanitized.DSN)
	if err != nil {
		return nil, nil, fmt.Errorf("pgxpoolx: parse dsn: %w", err)
	}

	if sanitized.MaxConns > 0 {
		poolConfig.MaxConns = sanitized.MaxConns
	}
	if sanitized.MinConns > 0 {
		poolConfig.MinConns = sanitized.MinConns
	}
	if sanitized.MaxConnLifetime > 0 {
		poolConfig.MaxConnLifetime = sanitized.MaxConnLifetime
	}
	if sanitized.MaxConnIdleTime > 0 {
		poolConfig.MaxConnIdleTime = sanitized.MaxConnIdleTime
	}
	if sanitized.HealthCheckPeriod > 0 {
		poolConfig.HealthCheckPeriod = sanitized.HealthCheckPeriod
	}

	poolConfig.ConnConfig.Tracer = dep.tracer
	if !sanitized.PreparedStatementsEnabled() {
		poolConfig.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	}

	if stmt, err := buildSearchPathStatement(sanitized.SearchPath); err != nil {
		return nil, nil, err
	} else if stmt != "" {
		existing := poolConfig.AfterConnect
		poolConfig.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			if existing != nil {
				if err := existing(ctx, conn); err != nil {
					return err
				}
			}
			if _, err := conn.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("set search_path: %w", err)
			}
			return nil
		}
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("pgxpoolx: create pool: %w", err)
	}

	telemetry := &poolTelemetry{}
	if sanitized.MetricsEnabledValue() {
		telemetry = newPoolTelemetry(dep.meter, helper, pool)
	}

	start := dep.clock()
	version, err := pingDatabase(ctx, pool, sanitized.HealthCheckTimeout)
	telemetry.recordHealthCheck(ctx, dep.clock().Sub(start), err)
	if err != nil {
		pool.Close()
		return nil, nil, err
	}

	helper.Infof("pgx pool created: dsn=%s max_conns=%d min_conns=%d prepared_statements=%t search_path=%s version=%s",
		sanitizeDSN(sanitized.DSN),
		poolConfig.MaxConns,
		poolConfig.MinConns,
		sanitized.PreparedStatementsEnabled(),
		strings.Join(sanitized.SearchPath, ","),
		version,
	)

	component := &Component{Pool: pool, helper: helper, metrics: telemetry}

	cleanup := func() {
		helper.Info("closing pgx pool")
		if telemetry != nil {
			telemetry.shutdown(context.Background())
		}
		pool.Close()
	}

	return component, cleanup, nil
}

func buildSearchPathStatement(searchPath []string) (string, error) {
	if len(searchPath) == 0 {
		return "", nil
	}
	parts := make([]string, 0, len(searchPath))
	for _, name := range searchPath {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		parts = append(parts, pgx.Identifier{trimmed}.Sanitize())
	}
	if len(parts) == 0 {
		return "", nil
	}
	return "set search_path to " + strings.Join(parts, ","), nil
}

func pingDatabase(ctx context.Context, pool *pgxpool.Pool, timeout time.Duration) (string, error) {
	healthCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := pool.Ping(healthCtx); err != nil {
		return "", fmt.Errorf("pgxpoolx: ping: %w", err)
	}

	var version string
	if err := pool.QueryRow(healthCtx, "select version()").Scan(&version); err != nil {
		return "", fmt.Errorf("pgxpoolx: version query: %w", err)
	}

	return truncateVersion(version), nil
}

func sanitizeDSN(dsn string) string {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}

	if parsed.User != nil {
		username := parsed.User.Username()
		if _, ok := parsed.User.Password(); ok {
			parsed.User = url.UserPassword(username, "***")
		}
	}

	return parsed.String()
}

func truncateVersion(version string) string {
	if idx := strings.Index(version, "("); idx >= 0 {
		return strings.TrimSpace(version[:idx])
	}
	if len(version) > 100 {
		return version[:100] + "..."
	}
	return version
}
