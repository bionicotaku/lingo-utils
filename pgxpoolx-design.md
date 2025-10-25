# pgxpoolx 组件设计说明

> 本文档描述计划中的 `lingo-utils/pgxpoolx` 组件，实现 PostgreSQL 连接池的标准化初始化、健康检查、日志与指标接入方案。该组件遵循 `lingo-utils` 统一的「Config → Component → ProviderSet → Typed Output」模式，为各微服务提供一致的底座能力。

---

## 1. 设计背景

- 当前 `kratos-template/internal/infrastructure/database` 内的连接池初始化只能在模板服务复用，无法直接供其他服务引用。
- 项目要求各服务独立自治，同时保持底层设施配置的一致性与可演进能力。
- 现有组件（`gclog`、`observability`、`txmanager`）已验证组件化注入模式可行，需要将数据库连接池迁移到共享层，以减少重复实现并集中治理 Supabase 相关约束。

---

## 2. 设计目标

| 目标 | 说明 |
| ---- | ---- |
| **统一初始化流程** | DSN 解析、连接池参数、Supabase 简单协议、search_path 设置一站式管理。 |
| **保持强分层** | 组件位于 `lingo-utils`，不反向依赖任何服务内部实现或 protobuf 类型。 |
| **日志标准化** | 默认接入 `gclog` 输出池创建、健康检查、关闭日志，并对 pgx 错误脱敏。 |
| **指标可选** | 通过配置开关启用连接池指标，未开启时零开销，保持“业务指标优先”前提。 |
| **易于 Wire 注入** | 提供 ProviderSet，直接暴露 `*pgxpool.Pool` 给仓储、事务管理器使用。 |

---

## 3. 组件结构

```
lingo-utils/pgxpoolx/
├── component.go      # NewComponent，实现核心初始化与健康检查
├── config.go         # Config 定义 & 默认值
├── dependencies.go   # Dependencies 定义与兜底逻辑（Logger/Meter/Tracer）
├── logging.go        # pgx QueryTracer -> slog 映射
├── metrics.go        # 可选的连接池指标上报
├── provider.go       # Component/ProviderSet/ProvidePool
└── test/...          # 健康检查、指标关闭/开启、错误路径等测试
```

- **Config**：仅包含通用参数（DSN、最大/最小连接数、生命周期、search_path、prepared statements、health check 超时、指标开关）。不耦合具体 protobuf。
- **Dependencies**：显式声明 `log.Logger`（必需）、`metric.Meter` 与 `pgx.QueryTracer`（可缺省）。缺省时 fallback 到全局 Provider 或默认 tracer。
- **Component**：聚合 `Pool`、`log.Helper` 以及内部 metrics handle，向外暴露 `*pgxpool.Pool`。

---

## 4. Config & Dependencies

```go
type Config struct {
    DSN                string
    MaxConns           int32
    MinConns           int32
    MaxConnLifetime    time.Duration
    MaxConnIdleTime    time.Duration
    HealthCheckTimeout time.Duration
    Schema             string
    SearchPath         []string
    EnablePreparedStmt *bool
    MetricsEnabled     *bool
}

type Dependencies struct {
    Logger log.Logger          // 必填，来自 gclog
    Meter  metric.Meter        // 可空，空时使用全局 Provider
    Tracer pgx.QueryTracer     // 可空，空时使用组件自带 tracer
}
```

默认值策略：
- 缺省 `MaxConns` → 使用 pgx 默认（4）或结合 Supabase 建议值。
- `EnablePreparedStmt` 缺省视作 `false`（Supabase Pooler 场景）。
- `MetricsEnabled` 缺省 `false`，避免在未接入 OTel 时误报错。
- `SearchPath` 若为空但指定 `Schema`，则自动设置为 `[schema, "public"]`。

---

## 5. 初始化流程

1. **参数校验与默认值应用**：拒绝空 DSN，补齐默认超时/连接池参数。
2. **构建 pgxpool.Config**：解析 DSN 后按配置覆写 Max/MinConns、生命周期等。
3. **Search Path**：为 Supabase schema 构造 `AfterConnect`，执行 `SET search_path`。
4. **QueryTracer 集成**：若 Dependencies.Tracer 为空，使用内部 `pgxLogger`，统一在查询失败时输出结构化日志并脱敏。
5. **创建连接池**：调用 `pgxpool.NewWithConfig`，若失败直接返回错误。
6. **健康检查**：基于 `HealthCheckTimeout` 创建子上下文，依次执行 `pool.Ping(ctx)` 和 `SELECT version()`，失败时关闭池并返回包装错误。
7. **指标初始化（可选）**：
   - 当 `MetricsEnabled` 为真时，从 `Dependencies.Meter`（或全局 Provider）获取 Meter。
   - 注册 `db.pool.connections`（UpDownCounter）、`db.pool.acquire_duration`（Histogram）、`db.pool.health_failures`（Counter）。
   - 周期性拉取 `pool.Stat()` 并上报活跃/空闲连接。
8. **返回 Component 与 cleanup**：cleanup 输出关闭日志，依次关闭 metrics 观察器与连接池。

---

## 6. 日志与指标

- **日志**：
  - 组件初始化成功时输出 `pool created`（记录脱敏 DSN、Max/MinConns、prepared statements 状态）。
  - 健康检查失败、关闭时输出 `WARN/INFO`，沿用 `log_helper` 字段规范（trace_id、span_id 等）。
  - `pgxLogger` 仅在查询失败时记录 `ERROR`，不打印 SQL，保护敏感数据。

- **指标**（可选）：
  - `db.pool.connections{state=active|idle}` UpDownCounter。
  - `db.pool.acquire_duration` Histogram，统计 `pool.Acquire` 耗时。
  - `db.pool.health_failures` Counter，记录健康检查失败次数。
  - 指标约束：默认关闭；需要时在配置中将 `metrics_enabled=true` 即可启用。

---

## 7. Wire 集成最佳实践

```go
wire.Build(
    configloader.ProviderSet,       // 提供 pgxpoolx.Config
    gclog.ProviderSet,              // log.Logger
    observability.ProviderSet,      // 注册全局 Tracer/Meter
    pgxpoolx.ProviderSet,           // 新的连接池组件
    txmanager.ProviderSet,          // 事务管理器复用 *pgxpool.Pool
    // ...
)
```

- `pgxpoolx.ProviderSet` 提供 `NewComponent` 与 `ProvidePool`。
- 依赖顺序固定：日志 → 观测 → 连接池 → 事务管理（避免缺少 Logger/Meter）。
- `txmanager` 继续从 Wire 获取 `Manager`，无需感知连接池组件迁移。

---

## 8. 配置映射（config_loader）

- 在 `loader.Bundle` 中新增 `PgxConfig pgxpoolx.Config` 字段。
- 新增 `ProvidePgxPoolConfig(b *Bundle) pgxpoolx.Config`，实现：
  - 从 `Data.Postgres` 读取 DSN、连接池参数、supabase schema。
  - 将 `Data.Postgres.transaction.metrics_enabled` 同步映射到 `Config.MetricsEnabled`（若未来需要单独开关，可提供独立字段）。
  - 保留 `EnablePreparedStatements`、`schema`、`health_check_period` 等特性。

---

## 9. 迁移步骤

1. 在 `lingo-utils` 创建 `pgxpoolx` 包并实现组件逻辑与测试。
2. 更新 `go.work` / `go.mod` 引入新包。
3. 调整 `kratos-template` Wire 文件，替换旧的 `internal/infrastructure/database` Provider 为 `pgxpoolx`.
4. 删除旧的数据库初始化包（保留迁移说明，必要时提供临时兼容层）。
5. 更新相关 README/文档，描述新的配置项与启用方式。
6. 执行 `make fmt && make lint && go test ./...` 验证改动。

---

## 10. 风险与回滚

| 风险 | 影响 | 缓解 |
| ---- | ---- | ---- |
| 组件迁移导致服务启动失败 | 业务无法连接数据库 | 保留阶段性分支，在验证完成前不要删除旧实现；新增集成测试覆盖健康检查、指标开关。 |
| 指标未启用时提示噪音 | 日志频繁警告 | 默认禁用指标，Meter 为空时不打印警告，只有显式开启指标才要求 Meter 可用。 |
| Supabase 行为变化 | 影响 search_path/协议设置 | `Config` 中保留显式字段，未来调整只需集中修改组件；在 README 标注 Supabase 特定默认值。 |

---

## 11. 下一步

- 评估业务服务对多 schema 或读写分离的需求，如有需要在 `Config` 中扩展对应字段。
- 规划连接池指标与 `txmanager` 指标的告警策略，确保底层异常能被及时发现。
- 待组件上线后，在各服务 README 中补充使用说明，确保团队成员统一接入方式。

---

## 12. 分步 TODO 清单

1. **包结构搭建**
   - [x] 在 `lingo-utils` 目录下创建 `pgxpoolx/`，建立基础 `go.mod` 引用。
   - [x] 初始化 `config.go`、`dependencies.go`、`component.go`、`logging.go`、`metrics.go`、`provider.go` 空文件。
   - [ ] 将 `go.work` 更新纳入新包（当前仓库无 go.work，后续视需要决定）。

2. **配置与依赖实现**
   - [x] 在 `config.go` 定义 `Config` 结构及 `Sanitize()` 方法，补齐默认值和字段校验。
   - [x] 在 `dependencies.go` 定义 `Dependencies` 与 `sanitizeDependencies`，默认回落到全局 OTel Provider。
   - [x] 编写 `logging.go`，从模板中迁移 `pgxLogger` 并适配新的 helper。

3. **核心组件开发**
   - [x] 在 `component.go` 实现 `NewComponent(ctx, cfg, deps)`：解析 DSN、构建 `pgxpool.Config`、`AfterConnect`、健康检查。
   - [x] 在 `component.go` 定义 `Component` 结构与 `cleanup` 逻辑。
   - [x] 在 `metrics.go` 实现可选指标注册和周期采集，遵循 `MetricsEnabled` 控制。

4. **Provider 集成**
   - [x] 在 `provider.go` 编写 `NewComponent` 封装、`ProvidePool`、`ProviderSet`。
   - [x] 添加包级 README 脚手架（可复用本文档内容）。

5. **测试与验证**
   - [x] 在 `test/` 子目录准备基于 Supabase 的集成测试脚本（依赖 `.env` 中的 `DATABASE_URL`）。
   - [x] 编写单元/集成测试覆盖配置校验、组件初始化成功/失败、指标开关与指标采集（`config_test.go`、`component_error_test.go`、`component_internal_test.go`、`integration_test.go`）。
   - [ ] 如可能，添加基准测试评估初始化耗时。

6. **模板迁移**
   - [x] 调整 `config_loader.Bundle` 增加 `PgxConfig` 字段及 `ProvidePgxPoolConfig`。
   - [x] 更新 `kratos-template/cmd/grpc/wire.go` 使用 `pgxpoolx.ProviderSet`，移除旧 `database.ProviderSet`。
   - [x] 删除 `kratos-template/internal/infrastructure/database`，或保留 stub 并标注废弃。

7. **全局验证**
   - [ ] 执行 `make fmt && make lint`，确保无静态检查问题。
   - [ ] 执行 `go test ./...`，确认模板与共享库测试通过。
   - [ ] 运行 `make run catalog`（或对应服务启动）验证实际连通性。

8. **文档与沟通**
   - [ ] 更新 `lingo-utils/component-pattern.md` 或 README 链接到 `pgxpoolx`。
   - [ ] 在相关服务 README 与 `docs/ai-context` 中说明新的数据库初始化方式。
   - [ ] 通知团队迁移计划与启用指标的配置示例。

9. **上线与回滚策略**
   - [ ] 在主干合入前保留 Feature Branch，确保可快速回滚到旧实现。
   - [ ] 合入后关注首个服务的运行日志与指标，确保无异常。
   - [ ] 如出现兼容性问题，根据风险表准备回滚（恢复旧目录或禁用新组件）。
