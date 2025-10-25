# Outbox/Inbox 通用化设计方案

> 目标：在 `lingo-utils` 下沉一套可被所有微服务复用的 Outbox/Inbox 能力，保证事件表结构、仓储接口、后台任务、配置与观测一致，避免每个服务重复实现。

## 1. 背景与现状

- **表结构重复**：各微服务在独立 schema 内维护 `outbox_events`、`inbox_events` 表，字段一致，仅 schema 不同。
- **仓储/任务复制**：`kratos-template` 中的仓储、Outbox Publisher、Metrics、退避逻辑等完全可共享，目前仍放在各服务 `internal/`。
- **配置/监控分散**：每个服务自行定义 Outbox 扫描配置与指标标签，缺乏统一的校验与文档。
- **测试脚本孤立**：端到端脚本、集成测试只覆盖单个服务，无法快速复用于其他上下文。

## 2. 拆分范围

| 模块 | 现状 | 可否抽象 | 说明 |
| --- | --- | --- | --- |
| 数据库 DDL / sqlc schema | 每服务独立 SQL 文件 | ✅ | 字段完全一致，可生成模板化 SQL。 |
| sqlc 查询与模型 | 在服务仓储目录生成 | ✅ | 查询语句一致，可在共享包生成一份，再传入 schema。 |
| Outbox 仓储实现 | 每服务 copy | ✅（需可配置 schema） | 通过注入 pgx pool + schema 名拼 SQL。 |
| Outbox Publisher 任务 | 内部实现 worker | ✅ | 除 Publish 回调外，其余逻辑统一。 |
| Inbox 处理框架 | 尚未统一 | ⚠️（可提供骨架） | 提供 Claim + handler 接口，业务消费回调。 |
| 配置结构体、默认值校验 | 服务自行定义 | ✅ | 提供统一 config struct + validator。 |
| Metrics/日志格式 | 逻辑相似 | ✅ | 在公共包注册 OTel 指标、日志字段。 |
| 测试基线 | 各自维护 | ✅ | 抽象出 shared integration test helpers。 |

## 3. 公共目录规划

建议在 `lingo-utils` 新增顶层目录 `outbox`（内含 inbox 相关能力）：

```
lingo-utils/outbox/
├── schema/                 # SQL 模板 & sqlc schema（占位符 SchemaName）
│   ├── tmpl/ddl.sql.tmpl
│   └── tmpl/schema.sqlc.tmpl
├── sqlc/                   # 统一生成的查询、模型
│   ├── queries.sql
│   ├── models.go
│   └── repo_generated.go
├── repository/             # 对外仓储接口 + 默认实现（参数：pgxpool、schema）
│   ├── repository.go
│   ├── mapper.go
│   └── message.go
├── publisher/              # Outbox 发布任务
│   ├── task.go             # worker + backoff
│   ├── metrics.go
│   └── config.go
├── inbox/
│   ├── worker.go           # 通用消费框架（Claim→Handler→Mark）
│   └── checkpoint.go
├── config/                 # 加载/校验结构体
│   └── loader.go
└── test/
    ├── postgres_fixture.go # 启动 PG，自动套用 schema
    └── publisher_suite.go  # 复用的集成测试
```

## 4. 核心设计

### 4.1 Schema 模板与生成
- 在 `schema/tmpl` 提供 DDL 模板，以 `{{ .Schema }}` 注入实际 schema 名。
- 提供 `go:generate` 或 `Makefile` 目标，在各服务内执行模板渲染得到迁移 SQL 与 `sqlc/schema`。
- 模板集中维护，字段新增/修改时所有服务自动同步。

### 4.2 Repository 抽象
- 定义 `Message`、`Event`、`Repository` 接口：
  ```go
  type Message struct {
      EventID uuid.UUID
      AggregateType string
      AggregateID uuid.UUID
      EventType string
      Payload []byte
      Headers map[string]string
      AvailableAt time.Time
  }
  type Repository interface {
      Enqueue(ctx context.Context, tx txmanager.Session, msg Message) error
      ClaimPending(ctx context.Context, before time.Time, stale time.Time, limit int, lockToken string) ([]Event, error)
      MarkPublished(ctx context.Context, tx txmanager.Session, eventID uuid.UUID, lockToken string, publishedAt time.Time) error
      Reschedule(ctx context.Context, tx txmanager.Session, eventID uuid.UUID, lockToken string, next time.Time, errMsg string) error
      CountPending(ctx context.Context) (int64, error)
  }
  ```
- 默认实现内部复用共享 sqlc 查询，通过构造 `"SET search_path TO <schema>,public"` 或在查询语句前添加 schema 前缀。

### 4.3 Outbox Publisher 组件
- Export `NewTask(cfg Config, repo Repository, publisher gcpubsub.Publisher, logger log.Logger, meter metric.Meter)`.
- 可配置项：batch size、worker 数、backoff、lock TTL、metrics 开关。
- 提供 Kratos Wire provider：`func ProvidePublisherTask(...) *PublisherTask`.
- 统一记录日志字段：`event_id`, `aggregate_id`, `attempt`, `latency_ms`, `lag_ms`.
- Metrics（建议统一命名）：
  - `outbox_publish_success_total`
  - `outbox_publish_failure_total`
  - `outbox_publish_latency_ms`
  - `outbox_pending_gauge`

### 4.4 Inbox Worker
- 允许服务注入 `func(ctx context.Context, evt Event) error` 业务处理回调。
- 负责 Claim → 幂等检查 → 调用 handler → MarkProcessed / RecordError。
- 支持最大重试次数、延迟重试间隔配置。

### 4.5 配置加载
- 在 `config` 子包定义：
  ```go
  type OutboxPublisherConfig struct {
      BatchSize int           `mapstructure:"batch_size"`
      TickInterval time.Duration `mapstructure:"tick_interval"`
      ...
      Schema string `mapstructure:"schema"`
  }
  ```
- 提供 `Validate()`，检查 `schema` 非空、`batch_size > 0` 等。
- 整合 `lingo-utils/configloader`（若已有）或提供 `MustLoad` 帮助函数。

### 4.6 测试与脚手架
- 利用 `test/postgres_fixture.go` 启动临时 PG 容器（testcontainers），自动执行模板 DDL。
- `publisher_suite.go` 包含：
  - 成功发布路径（标记 published + metrics 校验）。
  - Publish 失败重试（检查 `last_error` / `delivery_attempts`）。
  - 锁租约过期重试。
  - Headers JSON 解析失败的容错。
- 服务侧只需实现业务 handler，即可直接复用测试套件。

## 5. 引入流程建议

1. **第一阶段**（PoC）：在 `lingo-utils` 完成公共代码，先让 catalog 服务切换到 shared 实现（控制变量）。
2. **第二阶段**：将模板渲染逻辑加入 `make sqlc`，确保所有服务变化自动生效。
3. **第三阶段**：推广至其他微服务，逐个迁移现有 Outbox/Inbox 代码。
4. **第四阶段**：沉淀文档与 Runbook（排障流程、指标解读）。

## 6. 兼容性与风险

- **Breaking Change**：公共库变更可能同时影响所有服务，需在发布历史中明确标注版本（建议语义化版本，`lingo-utils/outbox` v0.x → v1.0）。
- **Schema 差异**：若个别服务需要额外字段，可通过扩展列的方式保留，但不可再复用共享 sqlc——需在文档中提醒如何“退出共享”。
- **模板渲染失败**：在 CI 安排检查，确保迁移同源。
- **配置遗漏**：统一的 `Validate/Default` 防止运行时缺参。

## 7. TODO（落地 checklist）

1. [ ] 在 `lingo-utils/outbox` 创建目录与基础 go.mod 调整。
2. [ ] 编写 DDL 模板、schema 模板，并提供渲染工具（Go program 或 Make target）。
3. [ ] 拆出共享 sqlc 查询，生成公用 repository。
4. [ ] 移植 PublisherTask 到公共包，抽象接口/配置。
5. [ ] 定义 Inbox Worker 骨架，实现幂等写入与错误记录。
6. [ ] 整理 config loader + validation。
7. [ ] 编写共享集成测试工具。
8. [ ] 更新 catalog 服务使用共享模块，验证 E2E。
9. [ ] 编写迁移指南（其它服务参考步骤）。

---

以上方案确保 Outbox/Inbox 能力在 `lingo-utils` 内达到“一处维护，多处复用”，同时保留各服务在事件内容、业务处理上的自定义空间。实现后可以显著减少重复代码、统一指标口径，并降低未来 schema 变更的维护成本。*** End Patch
