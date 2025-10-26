# Outbox/Inbox 共享模板与工具使用指南

此目录承载 Outbox/Inbox 的通用化基础设施：表结构模板、sqlc schema 模板，以及自动渲染工具。目标是让每个微服务在自己的 schema 下快速生成一致的事件表结构与 sqlc 定义，避免重复维护。

## 目录结构

```
outbox/
├── README.md
├── cmd/render-sql/            # 迁移/SQLC 模板渲染 CLI
├── config/                    # 强类型配置定义与校验
├── inbox/                     # StreamingPull Runner + Handler 接口
├── publisher/                 # Outbox 发布 Runner
├── schema/
│   └── tmpl/
│       ├── outbox_inbox_ddl.sql.tmpl        # 迁移脚本模板
│       └── outbox_inbox_sqlc_schema.sql.tmpl# sqlc schema 模板
├── store/                     # 通用仓储（Outbox/Inbox）
└── repository.go              # 面向服务的仓储构造辅助
```

## 快速开始

1. **进入 `lingo-utils` 根目录：**
   ```sh
   cd /path/to/learning-app/back-end/lingo-utils
   ```

2. **运行渲染工具：**
   ```sh
   go run ./outbox/cmd/render-sql \
     -schema catalog \
     -ddl-out ../kratos-template/migrations/generated/catalog_events.sql \
     -sqlc-out ../kratos-template/sqlc/schema/generated_catalog_events.sql
   ```

   参数说明：
   - `-schema`：目标 PostgreSQL schema 名称（必填）。
   - `-ddl-out`：迁移 SQL 的输出路径（必填）。
   - `-sqlc-out`：sqlc schema 的输出路径（必填）。
   - `-template-dir`：可选，模板目录（默认使用当前仓库下 `outbox/schema/tmpl`）。

3. **在目标服务中使用：**
   - 将生成的 DDL 迁移合并或替换现有事件表迁移脚本。
   - 将 sqlc schema 文件替换 `sqlc/schema/` 对应内容后，执行 `sqlc generate` 重新生成仓储模型。

## 模板要点

- `outbox_events` 与 `inbox_events` 字段、索引、注释保持一致，payload 采用 `BYTEA` 存储 Protobuf 二进制，headers 保持 JSONB。
- 模板包含 `CREATE SCHEMA IF NOT EXISTS`，可安全用于首次初始化。
- sqlc 模板与 DDL 完全同步，确保生成的 Go 代码字段类型一致。

## 运行时代码复用

除了迁移模板，本目录还提供运行时封装，帮助服务端仅通过少量代码完成 Outbox/Inbox 集成：

- `config.Config`：聚合 schema、发布器与消费者配置，带 Normalize/Validate。
- `outbox.NewRepository`：基于 pgxpool 和 schema 构造共享仓储，自动校验 `search_path`。
- `publisher.Runner`：封装 Outbox 扫描、租约、退避与指标；调用 `Run(ctx)` 即可常驻。
- `inbox.Runner[T]`：泛型 StreamingPull 消费者，组合自定义 Decoder/Handler 即可落地投影。

### 最小示例

```go
repo, _ := outbox.NewRepository(pool, logger, outbox.RepositoryOptions{Schema: "catalog"})
runner, _ := publisher.NewRunner(publisher.RunnerParams{
    Store:     repo,
    Publisher: pub,
    Config:    cfg.Publisher,
    Logger:    logger,
})
go runner.Run(ctx)

consumer, _ := inbox.NewRunner(inbox.RunnerParams[v1.Event]{
    Store:      repo,
    Subscriber: sub,
    TxManager:  tx,
    Decoder:    dec,
    Handler:    handler,
    Config:     cfg.Inbox,
    Logger:     logger,
})
go consumer.Run(ctx)
```

> ⚠️ 建议在仓储与 Runner 创建前调用 `cfg := cfg.Normalize(); cfg.Validate()`，确保所有字段满足约束。

## 集成建议

- 在服务的 Makefile 或脚本中添加步骤，在 sqlc/迁移生成前调用 `render-sql`，保证任何 schema 变更都从模板统一下发。
- 若某个服务需要扩充额外字段，可在渲染后追加，但请同步回 `tmpl` 以便共享。
- 结合本文的 Runner 封装，可实现 Outbox/Inbox 从迁移到运行态的全栈复用。

## 后续计划

- 下沉共享仓储接口、Outbox Publisher 任务、Inbox Worker。
- 提供跨服务的集成测试基线与配置校验。
- 逐步将现有服务迁移到共享实现，保持单处维护。

如需扩展模板或渲染逻辑，欢迎在文档中补充或提出 Issue。***
