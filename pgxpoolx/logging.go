package pgxpoolx

import (
	"context"
	"io"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/jackc/pgx/v5"
)

type pgxLogger struct {
	helper *log.Helper
}

func newPGXLogger(helper *log.Helper) pgx.QueryTracer {
	if helper == nil {
		helper = log.NewHelper(log.NewStdLogger(io.Discard))
	}
	return &pgxLogger{helper: helper}
}

// TraceQueryStart implements pgx.QueryTracer. Start events are ignored to keep
// query logging noise low while still satisfying the interface.
func (l *pgxLogger) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return ctx
}

// TraceQueryEnd logs failures without including SQL text to avoid leaking
// sensitive data.
func (l *pgxLogger) TraceQueryEnd(_ context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if data.Err == nil {
		return
	}
	l.helper.Errorf("pgx query failed: command_tag=%s err=%v", data.CommandTag.String(), data.Err)
}
