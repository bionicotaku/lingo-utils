package txmanager

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Session represents an in-flight transaction context exposed to repositories.
type Session interface {
	Tx() pgx.Tx
	Context() context.Context
}

type session struct {
	ctx context.Context
	tx  pgx.Tx
}

func (s *session) Tx() pgx.Tx {
	return s.tx
}

func (s *session) Context() context.Context {
	return s.ctx
}

func newSession(ctx context.Context, tx pgx.Tx) Session {
	return &session{ctx: ctx, tx: tx}
}
