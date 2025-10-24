package txmanager

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Manager provides scoped transaction helpers for service layers.
type Manager interface {
	WithinTx(ctx context.Context, opts TxOptions, fn func(context.Context, Session) error) error
	WithinReadOnlyTx(ctx context.Context, opts TxOptions, fn func(context.Context, Session) error) error
}

type managerImpl struct {
	pool    *pgxpool.Pool
	cfg     Config
	presets TxOptionPreset
	opts    managerOptions
	metrics *telemetry
	helper  *log.Helper
	tracer  trace.Tracer
}

// NewManager constructs a transaction manager backed by the provided pgx pool.
func NewManager(pool *pgxpool.Pool, cfg Config, options ...Option) (Manager, error) {
	if pool == nil {
		return nil, errors.New("txmanager: pool is required")
	}

	cfg = cfg.sanitized()
	mgrOpts := defaultManagerOptions()
	for _, opt := range options {
		opt(&mgrOpts)
	}

	if mgrOpts.meter == nil {
		mgrOpts.meter = otel.GetMeterProvider().Meter(cfg.MeterName)
	}
	if mgrOpts.tracer == nil {
		mgrOpts.tracer = otel.Tracer(cfg.MeterName)
	}

	helper := log.NewHelper(mgrOpts.logger)

	metricsEnabled := cfg.MetricsEnabled
	if mgrOpts.metricsEnabledOverride != nil {
		metricsEnabled = *mgrOpts.metricsEnabledOverride
	}
	telemetry := newTelemetry(mgrOpts.meter, helper, metricsEnabled)

	m := &managerImpl{
		pool:    pool,
		cfg:     cfg,
		presets: cfg.BuildPresets(),
		opts:    mgrOpts,
		metrics: telemetry,
		helper:  helper,
		tracer:  mgrOpts.tracer,
	}
	return m, nil
}

func (m *managerImpl) WithinTx(ctx context.Context, override TxOptions, fn func(context.Context, Session) error) error {
	base := m.presets.Default
	opts := mergeTxOptions(base, override)
	return m.exec(ctx, opts, fn, "read_write")
}

func (m *managerImpl) WithinReadOnlyTx(ctx context.Context, override TxOptions, fn func(context.Context, Session) error) error {
	base := m.presets.ReadOnly
	opts := mergeTxOptions(base, override)
	opts.AccessMode = ReadOnly
	return m.exec(ctx, opts, fn, "read_only")
}

func (m *managerImpl) exec(ctx context.Context, opts TxOptions, fn func(context.Context, Session) error, method string) (err error) {
	if ctx == nil {
		ctx = context.Background()
	}

	ctx, cancel := applyTimeout(ctx, opts.Timeout)
	defer cancel()

	spanName := opts.TraceName
	if spanName == "" {
		spanName = "db.tx." + method
	}
	ctx, span := m.tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	isolation := isoString(opts.Isolation)
	span.SetAttributes(attribute.String("db.system", "postgresql"), attribute.String("db.tx.isolation", isolation), attribute.String("db.tx.method", method))

	start := m.opts.clock()
	m.metrics.recordStart(ctx, method, isolation)

	var tx pgx.Tx
	tx, err = m.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: opts.Isolation, AccessMode: opts.AccessMode})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "begin")
		m.helper.Errorf("txmanager: begin failed method=%s isolation=%s err=%v", method, isolation, err)
		m.metrics.recordEnd(ctx, method, isolation, false, err, m.elapsedSince(start))
		return err
	}

	committed := false
	defer func() {
		if !committed {
			if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
				m.helper.Warnf("txmanager: rollback failed method=%s isolation=%s err=%v", method, isolation, rbErr)
			}
		}
	}()

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("txmanager: panic recovered: %v", r)
			span.RecordError(err)
			span.SetStatus(codes.Error, "panic")
			m.helper.Errorf("txmanager: panic method=%s isolation=%s err=%v", method, isolation, err)
			m.metrics.recordEnd(ctx, method, isolation, false, err, m.elapsedSince(start))
			panic(r)
		}
	}()

	if opts.LockTimeout > 0 {
		ms := opts.LockTimeout / time.Millisecond
		if ms > 0 {
			stmt := fmt.Sprintf("set local lock_timeout = '%dms'", ms)
			if _, execErr := tx.Exec(ctx, stmt); execErr != nil {
				err = fmt.Errorf("set lock_timeout: %w", execErr)
				span.RecordError(err)
				span.SetStatus(codes.Error, "lock_timeout")
				m.helper.Errorf("txmanager: lock_timeout failed method=%s isolation=%s err=%v", method, isolation, err)
				m.metrics.recordEnd(ctx, method, isolation, false, err, m.elapsedSince(start))
				return err
			}
		}
	}

	session := newSession(ctx, tx)
	err = fn(ctx, session)
	if err != nil {
		retryable, sqlState := classifyPgError(err)
		if retryable {
			err = wrapRetryable(err)
		}
		if sqlState != "" {
			span.SetAttributes(attribute.String("db.sql_state", sqlState))
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "exec")
		m.helper.Warnf("txmanager: fn error method=%s isolation=%s retryable=%t err=%v", method, isolation, retryable, err)
		m.metrics.recordEnd(ctx, method, isolation, retryable, err, m.elapsedSince(start))
		return err
	}

	if commitErr := tx.Commit(ctx); commitErr != nil {
		retryable, sqlState := classifyPgError(commitErr)
		if retryable {
			commitErr = wrapRetryable(commitErr)
		}
		if sqlState != "" {
			span.SetAttributes(attribute.String("db.sql_state", sqlState))
		}
		span.RecordError(commitErr)
		span.SetStatus(codes.Error, "commit")
		err = fmt.Errorf("commit: %w", commitErr)
		m.helper.Errorf("txmanager: commit failed method=%s isolation=%s retryable=%t err=%v", method, isolation, retryable, err)
		m.metrics.recordEnd(ctx, method, isolation, retryable, err, m.elapsedSince(start))
		return err
	}

	committed = true
	m.metrics.recordEnd(ctx, method, isolation, false, nil, m.elapsedSince(start))
	span.SetStatus(codes.Ok, "committed")
	return nil
}

func applyTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return ctx, func() {}
	}
	if deadline, ok := ctx.Deadline(); ok {
		if time.Until(deadline) <= timeout {
			return ctx, func() {}
		}
	}
	return context.WithTimeout(ctx, timeout)
}

func isoString(level pgx.TxIsoLevel) string {
	switch level {
	case pgx.Serializable:
		return "serializable"
	case pgx.RepeatableRead:
		return "repeatable_read"
	case pgx.ReadUncommitted:
		return "read_uncommitted"
	default:
		return "read_committed"
	}
}

func (m *managerImpl) elapsedSince(start time.Time) time.Duration {
	now := m.opts.clock()
	return now.Sub(start)
}
