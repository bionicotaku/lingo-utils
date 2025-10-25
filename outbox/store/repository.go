package store

import (
	"github.com/bionicotaku/lingo-utils/outbox/sqlc"
	"github.com/bionicotaku/lingo-utils/txmanager"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository 聚合 Outbox/InBox 数据访问能力。
type Repository struct {
	base *outboxsql.Queries
	log  *log.Helper
}

// NewRepository 构造仓储实例，要求 pool 已配置 search_path 指向目标 schema。
func NewRepository(pool *pgxpool.Pool, logger log.Logger) *Repository {
	return &Repository{
		base: outboxsql.New(pool),
		log:  log.NewHelper(logger),
	}
}

func (r *Repository) queries(sess txmanager.Session) *outboxsql.Queries {
	if sess == nil {
		return r.base
	}
	return r.base.WithTx(sess.Tx())
}
