# pgxpoolx

`pgxpoolx` 提供遵循 lingo-utils 组件模式的 PostgreSQL 连接池构建器，封装 DSN 解析、search_path 配置、Supabase 兼容设置、健康检查以及可选的 OpenTelemetry 指标。

## Wire 集成示例

```go
import (
    "context"

    "github.com/bionicotaku/lingo-utils/pgxpoolx"
)

func initPool(ctx context.Context, cfg pgxpoolx.Config, logger log.Logger) (*pgxpoolx.Component, func(), error) {
    return pgxpoolx.ProvideComponent(ctx, cfg, logger)
}
```

## 配置要点

- `DSN`：必填，建议启用 `sslmode=require` 与 Supabase 兼容。
- `EnablePreparedStmt`：默认关闭，满足 Supabase Pooler 简单协议要求。
- `SearchPath` / `Schema`：支持自动注入 `SET search_path`。
- `MetricsEnabled`：显式开启后才会上报连接池指标，默认关闭避免噪音。
