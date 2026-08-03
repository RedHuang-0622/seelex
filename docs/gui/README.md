# Seelex GUI / Agent Workbench 设计包

本目录是 GUI、Agent Workbench、跨模块 JSON 契约和未来 HTTP adapter 的权威设计入口。日期型 `docs/2026-*` 记录研发过程，`docs/arch/agent-workbench-architecture.md` 保留方案推演；发生冲突时以本目录 Schema、`architecture.md` 和模块登记为准。

## 交付结构

```text
docs/gui/
├── architecture.md          # 总体架构、数据流、并发与 generation 发布模型
├── module_dotting.json      # 机器可读模块登记与依赖 DAG
├── modules/                 # 每个模块的详细设计
├── schemas/                 # 对外/跨模块 JSON Schema（必需契约）
├── api/                     # HTTP、安全、分页、错误、快照语义
├── examples/                # 必须通过对应 Schema 的 payload
├── recipes/                 # 提交、回滚、重建和故障恢复
├── CHANGELOG.md             # 重要设计变更
├── decisions.md             # ADR 记录
├── ci-and-testing.md        # 测试和 CI
└── code-review.md           # 实现追溯与审查
```

## 当前与目标状态

| 能力 | 状态 | 说明 |
|---|---|---|
| 单 Application Core、Snapshot/Event v1 | 已实现 | TUI/GUI 共用，单 Engine、单 active session |
| Wails Bridge、client reducer、安全 Markdown、Effort | 已实现 | 当前桌面 alpha 主链 |
| DSL Card、Workspace sandbox | 规划 | 契约与详设已建立，尚未作为当前能力宣传 |
| 多 SessionActor 并行 | 规划 | 当前 session 列表仍是破坏式 history 切换 |
| immutable generation repository | 规划 | 本轮冻结发布与恢复模型，当前 store 尚未实现该协议 |
| HTTP API v2 | 规划 | 当前没有网络服务；文档用于先冻结 adapter 契约 |
| 证据门禁需求到 Dev 自迭代 | 规划 | RAG evidence gate、人工队列、分层 baseline、Dev/E2E 反馈重开 |

完整状态、接口和实现路径只从 [`module_dotting.json`](module_dotting.json) 读取。

## 推荐阅读顺序

1. [`architecture.md`](architecture.md)：系统边界、数据流、并发、generation 和通用结构。
2. [`module_dotting.json`](module_dotting.json)：模块职责、状态、接口、输入输出和依赖。
3. [`modules/`](modules/)：实现或规划模块的详细设计。
4. [`schemas/`](schemas/) 与 [`examples/`](examples/)：实际 JSON 契约及合法 payload。
5. [`api/`](api/)：规划 HTTP adapter 的 transport 语义。
6. [`recipes/`](recipes/)：generation 运维和恢复流程。
7. [`architecture-review.md`](architecture-review.md)：并发、循环依赖、边界与解耦审查。

## 模块索引

| 模块 | 状态 | 详细设计 |
|---|---|---|
| Application protocol | 已实现 | [`application-protocol.md`](modules/application-protocol.md) |
| Desktop Bridge | 已实现 | [`desktop-bridge.md`](modules/desktop-bridge.md) |
| Client state | 已实现 | [`client-state.md`](modules/client-state.md) |
| Conversation rendering | 已实现 | [`conversation-rendering.md`](modules/conversation-rendering.md) |
| Effort control | 已实现 | [`effort-control.md`](modules/effort-control.md) |
| Shell/interactions | 已实现 | [`shell-and-interactions.md`](modules/shell-and-interactions.md) |
| JSON DSL Card | 规划 | [`dsl-card-runtime.md`](modules/dsl-card-runtime.md) |
| Workspace sandbox | 规划 | [`workspace-sandbox.md`](modules/workspace-sandbox.md) |
| Generation repository | 规划 | [`generation-store.md`](modules/generation-store.md) |
| Multi-session workbench | 规划 | [`multi-session-pages.md`](modules/multi-session-pages.md) |
| HTTP API adapter | 规划 | [`http-api-adapter.md`](modules/http-api-adapter.md) |
| Evidence-gated Dev loop | 规划 | [`evidence-gated-dev-loop.md`](modules/evidence-gated-dev-loop.md) |
| Agent E2E | 规划 | [`agent-e2e-interaction.md`](modules/agent-e2e-interaction.md) |

## 契约索引

| Schema | 示例 | 用途 |
|---|---|---|
| [`snapshot`](schemas/snapshot.schema.json) | [`snapshot.json`](examples/snapshot.json) | Workbench 权威快照 |
| [`session snapshot`](schemas/session-snapshot.schema.json) | [`session-snapshot.json`](examples/session-snapshot.json) | 单 session 权威快照与历史窗口 |
| [`event`](schemas/event.schema.json) | [`event.json`](examples/event.json) | scoped 有序增量 |
| [`page`](schemas/page.schema.json) | [`workspace-page.json`](examples/workspace-page.json) | 大结果 cursor 分页 |
| [`error`](schemas/error.schema.json) | [`error.json`](examples/error.json) | typed problem response |
| [`card`](schemas/card.schema.json) | [`card.json`](examples/card.json) | 安全对话卡片 |
| [`generation manifest`](schemas/generation-manifest.schema.json) | [`generation-manifest.json`](examples/generation-manifest.json) | 不可变 checkpoint 清单 |
| [`generation operation`](schemas/generation-operation.schema.json) | [`generation-operation.json`](examples/generation-operation.json) | checkpoint/rollback mutation |
| [`turn request`](schemas/turn-request.schema.json) | [`turn-request.json`](examples/turn-request.json) | 提交 session turn |
| [`turn accepted`](schemas/turn-accepted.schema.json) | [`turn-accepted.json`](examples/turn-accepted.json) | 异步接受结果 |
| [`interaction resolution`](schemas/interaction-resolution.schema.json) | [`interaction-resolution.json`](examples/interaction-resolution.json) | session-scoped 审批决议 |
| [`evidence assessment`](schemas/evidence-assessment.schema.json) | [`evidence-assessment.json`](examples/evidence-assessment.json) | 原子主张、证据绑定与门禁结果 |
| [`dev iteration`](schemas/dev-iteration.schema.json) | [`dev-iteration.json`](examples/dev-iteration.json) | 需求到 Dev/E2E 自迭代运行状态 |
| [`module registry`](schemas/module-dotting.schema.json) | [`module_dotting.json`](module_dotting.json) | 模块登记与依赖 |

`common.schema.json` 只提供共享 `$defs`，不对应独立 payload。

## 维护规则

1. Schema 是契约事实源；字段变化先改 Schema，再改示例、API、模块文档和实现。
2. 新增模块必须登记 `status`、职责、接口、输入输出、计划路径和 `depends_on`；依赖不得成环。
3. 规划能力必须标为“规划”，不能因存在设计或示例就标为已实现。
4. Snapshot/Event 变化必须同步 reducer、gap/resync、revision-floor 和契约测试。
5. generation 变化必须同步 manifest Schema、四个 recipes 和 `CHANGELOG.md`。
6. HTTP mutation 必须说明认证 scope、幂等、precondition、错误原子性和取消语义。
7. `go test ./...` 会编译 Schema、验证所有示例并检查模块依赖图和文档存在性。
8. 自动生成资格只能来自版本化 evidence/gate 或明确人工批准；低证据条目不得删除或静默忽略。
