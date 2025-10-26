package outbox

import (
	"context"
	"fmt"
	"strings"

	"github.com/bionicotaku/lingo-utils/outbox/store"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RepositoryOptions 控制共享仓储的初始化行为。
type RepositoryOptions struct {
	Schema string
}

// NewRepository 构造共享 Outbox/Inbox 仓储，并验证连接 search_path 是否包含目标 schema。
func NewRepository(pool *pgxpool.Pool, logger log.Logger, opts RepositoryOptions) (*store.Repository, error) {
	if pool == nil {
		return nil, fmt.Errorf("outbox: pgx pool is required")
	}
	repo := store.NewRepository(pool, logger)

	if schema := strings.TrimSpace(opts.Schema); schema != "" {
		if err := ensureSearchPath(pool, schema); err != nil {
			return nil, err
		}
	}
	return repo, nil
}

func ensureSearchPath(pool *pgxpool.Pool, schema string) error {
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("outbox: acquire connection for schema validation: %w", err)
	}
	defer conn.Release()

	var searchPath string
	if err := conn.QueryRow(ctx, "select current_schema()").Scan(&searchPath); err != nil {
		return fmt.Errorf("outbox: query current_schema: %w", err)
	}
	if !strings.EqualFold(searchPath, schema) {
		return fmt.Errorf("outbox: current_schema=%s mismatch expected=%s, please configure pgx search_path", searchPath, schema)
	}
	return nil
}
