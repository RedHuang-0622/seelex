# 代码变更摘要

## Session 存储层模块化（阶段 1-2，2026-08-05）

依据 `docs/2026-08-05-session-storage-modularization/`（plan.md 阶段 1-2）
对会话存储层实施改动；不改变既有数据布局的写入语义。

### 新增/修改/删除文件

| 文件 | 类型 | 说明 | 设计模式 |
|------|------|------|---------|
| `sessionstore/event_range.go` | 新增 | EventSeq 范围读取（selectEventRange 过滤器 + JSON/SQLite/Redis 实现） | 共享过滤 helper |
| `sessionstore/conversation.go` | 新增 | Conversation 模块 DTO + 部分解码（只解析 conversation 子树）+ 三后端实现 | 模块冷读 |
| `sessionstore/sessionstore.go` | 修改 | framework events 独立 append-only 模块（P0）；Repository 新增 ReadEventRange/ReadConversationRange；Event.MessageID | 模块化 manifest 过渡 |
| `sessionstore/session_context.go` | 修改 | CompactFrame 增加 round/event/message 范围与 revision 字段；PushCompact 范围校验 | DDD 值对象扩展 |
| `session/manager.go` | 修改 | 透传 LoadConversationRangeByWorkspace / LoadEventRangeByWorkspace | 薄包装 |
| `application_adapters.go` | 修改 | LoadConversationRangeWorkspace 改走模块冷读；MessageID 适配透传 | 适配器 |
| `application/model/context.go` | 修改 | TranscriptEvent.MessageID | 契约 DTO |
| `application/core/session_archive.go` | 修改 | enrichTranscriptMessageIDs（persist 时 event-to-message 关联）；recordConversationTranscript 携带 MessageID | 内容寻址配对 |
| `sessionstore/event_store_test.go` | 修改 | framework events rollover/迁移契约测试 | 契约测试 |
| `sessionstore/conversation_test.go` | 新增 | conversation 模块跨后端契约测试 | 契约测试 |
| `sessionstore/event_range_test.go` | 新增 | event range 跨后端契约测试 | 契约测试 |
| `sessionstore/session_context_test.go` | 修改 | CompactFrame 范围字段往返 + 校验测试 | 单元测试 |
| `sessionstore/state_test.go` | 修改 | Event.MessageID 持久化往返断言 | 单元测试 |
| `application/core/session_archive_test.go` | 修改 | enrichTranscriptMessageIDs 配对测试 | 单元测试 |

### 关键变更

1. **P0 修复（generation rollover 后 framework facts 丢失）**：JSON 后端
   执行事实事件库从 generation 内 `events.json` 迁到 session 根目录
   `framework-events.json`（独立 append-only 模块）；首次追加时扫描全部旧
   generation 按 Seq 幂等合并迁移；读取保留 v1 回退。契约测试覆盖连续两次
   Commit 后旧 facts 仍可读、重复 Seq 不重复落库。
2. **Conversation 模块冷读**：`Repository.ReadConversationRange` 只解析
   state blob 的 conversation 子树（不反序列化 Plan/Execution/Projection），
   兼容 v1 archive 与 v2/v3 record；`LoadConversationRangeWorkspace` 改走
   模块读，长会话向上翻页不再解析完整执行状态。损坏/版本不兼容显式报错。
3. **Event 模块范围读取**：`ReadEventRange` 按 EventSeq 含端点读取，跨 shard
   连续，倒置范围显式报错，缺失会话返回 not-found（不静默为空）。
4. **Event.MessageID 稳定定位键**：TranscriptEvent/Event 增加 `message_id`；
   persist 时按 CallID（tool/tool_result 分表）+ 角色内容建立 event-to-message
   关联，不按数组位置推导；resume 回退路径直接携带 message ID。
5. **CompactFrame 正式定位字段**：新增 RoundFrom/RoundTo、EventFrom/EventTo、
   MessageFrom/MessageTo、EventRevision/ConversationRevision（architecture.md
   §3.3），保留 From/To 兼容；半填充/倒置范围拒绝。

### API 变更

| API | 变更 | 兼容性 |
|-----|------|--------|
| `Repository.ReadEventRange(ctx, key, fromSeq, toSeq)` | 新增 | 向后兼容（三后端实现补齐） |
| `Repository.ReadConversationRange(ctx, key, offset, limit)` | 新增 | 同上 |
| `Router.LoadEventRangeWorkspace` / `LoadConversationRangeWorkspace` | 新增 | 新增方法 |
| `Manager.LoadEventRangeByWorkspace` / `LoadConversationRangeByWorkspace` | 新增 | 新增方法 |
| `sessionstore.Event.MessageID` / `application.TranscriptEvent.MessageID` | 新增字段 | 旧数据缺省为空，读兼容 |
| `sessionstore.CompactFrame` | 新增 8 字段 | From/To 保留，旧 JSON 兼容 |
| JSON framework events 存储位置 | generation 内 → 根 `framework-events.json` | 首次追加自动迁移；读回退旧布局 |

### 循环依赖检查

- [x] 确认无新增（sessionstore 不依赖 application；DTO 在存储层定义）

### 验证

- `go build ./...` ✅ / `go vet ./...` ✅ / `gofmt` ✅
- 新增契约测试 10+ 用例全部通过；sessionstore / application/core / session 全量通过
- `seelebridge` 存在 1 个既有环境性失败（`TestRuntimeProjectScopedToolsUseBoundProject`，
  Windows 下 bash/PowerShell `&&` 语法不兼容；git stash 后复现，与本次改动无关）
- code-impl Go 专项 grep（`return nil, nil` / 包级可变 / 硬编码密钥）✅

### Commit 建议（待确认）

1. `fix(storage): keep framework facts across generation rollover`（G1）
2. `feat(storage): modular session readers and stable event keys`（G2-G5）

## Session 存储层模块化阶段 3：context 四栈独立持久化并接入 Resume（2026-08-06）

依据 `docs/2026-08-05-session-storage-modularization/`（plan.md 阶段 3）。

### 关键变更

1. **context 独立存储通道（S1）**：`Repository` 新增 `WriteContextState/ReadContextState`
   （JSON: `context.json` 独立文件；SQL: `seelex_session_context` 表；Redis: 独立
   `:context` key），与 SessionRecord 的 state blob 物理隔离。`SessionContextStore`
   改走独立通道——此前它与 SessionRecord 共用 `state.json`，两者 schema 完全不同
   （`schema_version=1` vs `version=3`），生产 attach 会互相破坏。契约测试覆盖
   JSON/SQLite 双后端共存、互不覆盖、删除级联。
2. **生产接线（S2）**：`sessionPort` 实现 `AttachSessionContext/DetachSessionContext`
   （创建按 sessionID 的 SessionContextStore + Load + `runtime.AttachSessionContextStore`）；
   `resumeSession` 恢复后挂接四栈（损坏显式失败），`BeginNewSession`/draft 实体化时
   解绑（防跨会话串栈）；`session.Manager` 暴露 `Router()`。
3. **SessionID 注入（S3）**：`seelexctx.ControllerOptions.SessionID` 改为动态
   `SessionIDProvider`；Runtime 跟踪当前主会话 ID（`MainSessionID`，newMainSession
   时更新），压缩帧 `SegmentID` 溯源为 `compact-<sessionID>-<ms>`，会话切换后
   不再漂移。
4. `isSessionNotFound` 统一识别 `sql.ErrNoRows`（SQL 后端"会话不存在"语义）。

### 文件

| 文件 | 类型 | 说明 |
|------|------|------|
| `sessionstore/context_state.go` | 新增 | context 独立通道三后端实现 |
| `sessionstore/context_state_test.go` | 新增 | 通道隔离/共存/删除级联契约测试 |
| `sessionstore/sessionstore.go` | 修改 | Repository 接口 + SQL 建表 + Delete 级联 |
| `sessionstore/session_context.go` | 修改 | SessionContextStore 改走独立通道 |
| `sessionstore/durable_history.go` | 修改 | isSessionNotFound 识别 sql.ErrNoRows |
| `session/manager.go` | 修改 | Router() 暴露 + context 透传 |
| `application/core/session_archive.go` | 修改 | sessionContextPort 端口定义 |
| `application/core/session_history.go` | 修改 | resume 后挂接 context |
| `application/core/session_draft.go` | 修改 | 新建/实体化时解绑 |
| `application_adapters.go` | 修改 | sessionPort 实现 attach/detach |
| `main.go` | 修改 | sessionPort 注入 runtime |
| `seelebridge/runtime.go` | 修改 | MainSessionID 跟踪 |
| `seelebridge/context_components.go` | 修改 | SessionIDProvider 注入 |
| `seelexctx/controller.go` | 修改 | ControllerOptions.SessionIDProvider |

### 验证

- `go build ./...` / `go vet ./...` / `gofmt` 全绿
- 新增测试：context 通道契约（JSON/SQLite）、resume attach/detach 行为、
  损坏 context 显式失败、MainSessionID 跟踪、segmentID 前缀断言
- sessionstore / session / application / seelexctx / seelebridge 全量通过

## MCP 工具冷启动（懒加载）改造（2026-08-06）

### 诊断补充（端到端验证发现，最终根因）

冷启动改造后 mcp_load 首次加载仍 30-90s 超时，逐层诊断（排除法）：

| 层级 | 实测 | 结论 |
|---|---|---|
| 启动路径（含 MCP 登记） | 46ms（原 3.5s） | 冷启动改造生效 |
| 裸 npx 冷启动（bash pipe） | 3.3s | npx 层有固定开销 |
| 直接 node + cli.js 握手 | **0.56s** | server 本身极快 |
| 测试内直接 LoadMCP（不经 ChatStream） | **483ms 成功 24 工具** | LoadMCP/AttachMCP 本身无问题 |
| backend ChatStream 工具调用 | 60s+ 超时，**从未 spawn 进程** | 工具调用在审批层挂起 |
| `--permission full_access` 对照 | **3.9s 成功** | 实锤：manual 审批挂起 |

最终根因（三层叠加）：
1. **seele.yaml 的 permission.rules 覆盖内置白名单**（兜底 `*: ask`），mcp_load
   不在其中 → manual 模式每次调用触发审批；backend 无审批人 → 挂起到超时。
   修复：seele.yaml 加 `mcp_load: allow` + 内置白名单同步补充。
2. **npx 层 stdio 代理不透明**（`@latest` 每次解析 registry ~5.4s + 握手延迟
   3s~90s+ 不稳定）：accounts.yaml 改为 node + 全局安装 cli.js 直连（0.56s），
   并全局安装 `@playwright/mcp@0.0.79`（与 npx 缓存版本一致）。
3. 冷启动改造本身（RegisterLazyMCP 只登记不连接）有效：启动 46ms。

修复后 manual 模式 mcp_load **2.9s** 加载 playwright 24 工具，启动 46ms。

## MCP 工具冷启动（懒加载）改造（2026-08-06）

| 文件 | 类型 | 说明 |
|------|------|------|
| `seelebridge/mcp.go` | 修改 | 新增 `RegisterLazyMCP`（只存配置不连接）/ `LoadMCP`（幂等按名 attach）/ `LazyMCPServerNames` |
| `seelebridge/runtime.go` | 修改 | Runtime 增加 lazy MCP 注册表字段 |
| `mcpconfig.go` | 修改 | `registerMCPServers` 从启动时同步 attach 改为登记（冷启动） |
| `main.go` | 修改 | 新增内置 `mcp_load` 工具（按名加载，30s 超时保护） |
| `seelebridge/mcp_provider_test.go` | 修改 | 冷启动契约测试（登记不连接 / 失败不破坏登记 / 校验） |

背景：accounts.yaml 配置了 Playwright MCP（npx stdio），启动路径同步
spawn + initialize + tools/list 实测阻塞 3.4s（无 MCP 对照仅 31ms），且
GUI 子系统 spawn 控制台子进程弹终端窗口。改为冷启动后启动回到 46ms。

## Windows bash 工具 shell 探测修复（2026-08-05）

| 文件 | 类型 | 说明 |
|------|------|------|
| `seelebridge/scoped_tools.go` | 修改 | `scopedBashCommand` 在固定 Git 路径探测失败后回退 `exec.LookPath("bash")`，覆盖自定义安装路径（scoop/chocolatey/便携版），避免直接跳到 PowerShell 拒绝 `&&` 等 bash 语法 |
| `seelebridge/sandbox.go` | 修改 | `commandShell` 同步补充固定 Git 路径 + PATH 探测；`commandShellArgs` 按 basename 识别完整路径形式的 `bash.exe`（否则会误走 cmd 参数） |

根因：本机 Git Bash 装在非标准路径（`G:\Tools\Git\Git\usr\bin\bash.exe`），
`scopedBashCommand` 只探测三个标准路径，回退 PowerShell 5.1 → `pwd && ls -la`
报 ParserError，导致 `TestRuntimeProjectScopedToolsUseBoundProject` 失败。
修复后该测试通过，`go test ./seelebridge/` 全量通过。

## 本次账号池与测试增量（2026-07-27）

| 文件 | 类型 | 说明 | 设计模式 |
|------|------|------|---------|
| `config/accounts.example.yaml` | 修改 | 配置模板改为 `roles.subagent/agent/goalplan` 分组，匹配当前角色路由策略 | Configuration Template / Strategy |
| `scripts/sync-claudecode-account.ps1` | 修改 | 本地同步脚本输出角色分组格式，并默认写入运行时读取的 `config/accounts.yaml` | Adapter |
| `seelebridge/config.go` | 修改 | 明确角色分组解析、角色回退和空号池校验 | Strategy / Fallback |
| `seelebridge/runtime_test.go` | 修改 | 覆盖角色分组、旧 accounts 列表拒绝和空配置边界 | Contract Test |
| `e2e/scenario/harness.go`、`scripted_engine.go` | 修改 | 补齐 application 新增接口桩，恢复 e2e 场景编译 | Test Double |

本地生成的 `config/accounts.yaml` 已按模板更新，文件被 `.gitignore` 忽略，不进入提交。

## 新增/修改/删除文件

| 文件 | 类型 | 说明 | 设计模式 |
|------|------|------|---------|
| `gui/bridge.go` | 新增 | Application Core 到桌面 GUI 的窄接口和事件桥 | Adapter / Observer |
| `gui/run_wails.go` | 新增 | `gui` build tag 下启动 Wails 窗口 | Factory |
| `gui/run_stub.go` | 新增 | 默认构建下提供明确的 GUI 未启用错误 | Null Object / Build Variant |
| `gui/assets.go`、`gui/frontend/dist/*` | 新增 | Codex 风格三栏界面；Runtime 弹窗、指令模式与原生 SVG 图标控件 | Embed / Component UI |
| `gui/frontend/dist/components.js` | 新增 | 消息/图标/资料源组件；工具 `IN`/`OUT` 双框；真实 ChatState 驱动运行指示和队列卡片 | Presentation Component / Lazy Expansion |
| `gui/frontend/dist/app.js` | 新增 | 运行时保持发送可用，后续输入进入后端队列；同步短状态、占位提示和活动组件 | State Projection |
| `gui/frontend/dist/markdown.js` | 新增 | 无外部依赖的安全 Markdown 渲染组件；支持常用语法与块级 `<think>` 折叠思考过程 | Pure Function / Sanitizing Renderer |
| `gui/frontend/dist/markdown.test.mjs`、`components.test.mjs` | 新增 | Markdown/思考块、状态动效、队列、安全转义和危险链接边界测试 | Contract Test |
| `gui/bridge_test.go` | 新增 | Bridge 转发、事件和资源契约测试 | Fake / Contract Test |
| `application/app.go`、`application/state.go` | 修改 | Snapshot 暴露持久化会话元数据，供 TUI/GUI 共用 | Snapshot / DTO |
| `application/ports.go`、`application_adapters.go` | 修改 | 新增会话历史替换/新建窄接口，完整保留 reasoning 和工具调用上下文 | Adapter / Transactional Replace |
| `application/chat.go`、`application/command.go` | 修改 | 恢复后按选中会话 ID 继续保存，新建会话切换独立 ID | Session Routing |
| `application/chat.go` | 修改 | 队列接续时不再额外插入临时用户消息，避免合并输入重复显示 | Single Source of Truth |
| `main.go` | 修改 | 增加 `-frontend`、`-version`，默认 manual 权限并校验输入 | Strategy |
| `version.go` | 修改 | 可由 linker 注入的版本和默认前端 | Single Source |
| `config/accounts.example.yaml` | 新增 | 唯一公开账户配置模板 | Configuration Template |
| `config/account-pool.yaml` | 删除 | 移除被 Git 跟踪、可能被误填密钥的真实配置文件名 | Security Hardening |
| 构建脚本 / Makefile | 修改/新增 | 配置白名单、正确版本注入、CLI/GUI 归档、附带许可证文档 | Allowlist Packaging |
| `.github/workflows/ci.yml` | 修改 | gofmt、配置白名单、发布安全、漏洞扫描 | Quality Gate |
| `.github/workflows/release.yml` | 新增 | tag 驱动 CLI/Windows GUI 构建、校验和和预发行 | Release Pipeline |
| `LICENSE`、`CHANGELOG.md` | 新增 | MIT 授权与 Alpha 变更记录 | Release Metadata |
| `README.md`、`docs/feature-instrumentation.md` | 修改 | 同步 GUI、权限、版本和已知限制 | Documentation |
| `smoke_test.go` | 修改 | 外部 LLM 测试改为显式 opt-in，缺配置时不再伪装通过 | Explicit Integration Gate |
| `go.mod`、`go.sum` | 修改 | 引入 Wails v2.13.0；升级 gRPC 到安全版本 v1.79.3 | Dependency Management |
| 多个现有 `.go` 文件 | 格式化 | 统一 gofmt，满足新 CI 门禁 | Mechanical Formatting |

## API 变更

| API | 变更 | 兼容性 |
|-----|------|-------|
| CLI `-permission` | 默认 `full_access` → `manual` | 有意的安全行为变化；可显式选择 `full_access` |
| CLI `-frontend` | 新增 `tui` / `gui` | 向后兼容，默认仍为 TUI |
| CLI `-version` | 新增 | 向后兼容 |
| `main.Version` | 常量改为 linker 可注入变量 | 构建脚本已同步 |
| `main.DefaultFrontend` | 新增 linker 可注入变量 | Windows GUI 包默认启动 GUI |
| `gui.Application` | 新增调用方窄接口 | `*application.Service` 直接满足 |
| `application.Snapshot.Sessions` | 新增持久化会话列表 | 向后兼容，新 JSON 字段 |
| `application.ChatEngine.ReplaceHistory` | 新增原子历史替换 | 内部窄接口，启用 TUI/GUI 会话恢复 |
| `application.ChatEngine.StartSession` | 新增独立会话 ID 生成 | 内部窄接口 |
| `gui.AppInfo.Project` | 新增工作区名称、根路径与资料源 | 向后兼容，本地 GUI 元数据 |
| `markdown(value)` | 从内联简易格式化升级为独立安全渲染器的兼容别名 | 保持原导出名与调用方式 |
| `renderMarkdown(value)` | 新增纯函数渲染接口 | 前端内部 API，不影响 Go/Wails Bridge |
| `renderConversationComponent(messages, chat)` | 可选接收 ChatState 并附加运行/队列组件 | 向后兼容，省略第二参数时行为不变 |
| `renderChatActivity(chat)` | 新增纯展示接口 | 只消费 `running/input_queue`，不维护前端队列副本 |

## 设计模式使用

| 模式 | 文件 | 效果 |
|------|------|------|
| Adapter | `gui/bridge.go` | GUI 不依赖 Seele/Engine 深层类型 |
| Observer | `gui/bridge.go` | Application Event 转发为桌面事件 |
| Strategy | `main.go` | 一次装配后选择 TUI 或 GUI |
| Build Variant | `gui/run_*.go` | 默认 CLI 不编译桌面容器，`-tags "gui,desktop,production"` 显式启用 |
| Allowlist | 构建脚本 | 发行包只复制公开模板，阻止本机账户文件泄露 |
| Sanitizing Renderer | `gui/frontend/dist/markdown.js` | 原始 HTML 先转义、危险 URL 协议拒绝，再生成受控标签 |
| State Projection | `gui/frontend/dist/components.js` | 将后端 ChatState 投影为加载动效和可见队列，不复制业务状态 |

## 接口抽象

| 接口 | 实现方 | 使用方 |
|------|-------|-------|
| `gui.Application` | `application.Service` | `gui.Bridge` |
| `emitter` | Wails runtime adapter / test fake | `gui.Bridge` 事件循环 |
| `renderMarkdown(value)` | `gui/frontend/dist/markdown.js` | GUI 消息组件 |
| `renderChatActivity(chat)` | `gui/frontend/dist/components.js` | GUI 会话尾部状态区 |

## 循环依赖检查

- [x] `application` 不依赖 `tui` 或 `gui`
- [x] `tui` 与 `gui` 互不依赖
- [x] `components.js` 单向依赖纯函数 `markdown.js`
- [x] 队列状态由 `application.ChatState` 单向流入 GUI，无前端双写状态
- [x] `go build ./...` 和 `go build -tags "gui,desktop,production" ./...` 均通过

## Commit 建议

未自动提交，建议消息：

```text
feat(gui): add safe alpha desktop frontend

- add Wails GUI bridge and embedded desktop interface
- render conversation Markdown with safe links and escaped raw HTML
- default permissions to manual and validate frontend modes
- harden release packaging against local account leakage
- add versioned prerelease CI and security scanning

Refs: release P0, GUI alpha
```

## Application modularization (2026-07-28)

| Directory | Responsibility |
|---|---|
| `application/model` | Versioned DTOs and safe snapshot copies |
| `application/event` | Event envelope and subscription hub |
| `application/approval` | Approval request lifecycle |
| `application/contract` | Application-owned external ports |
| `application/prompt` | Prompt stack and effort policy |
| `application/search` | Tavily web-search provider |
| `application/core` | Service use-case orchestration |

`application/application.go` preserves the existing public API as a facade, so
callers in the composition root, TUI, GUI, and adapters do not need import-path
changes. `application/README.md` documents the dependency direction.

Verification: `go test ./application/...` and `go test ./...`.

## PlanAct authority follow-up (2026-07-29)

| File | Type | Description | Design |
|---|---|---|---|
| `seelebridge/plan_authority.go` | Added | Request-ID-bound atomic PlanAct scope with preflight and authority phases. | State machine / Capability |
| `seelebridge/plan_preflight.go` | Modified | Canonical object DAG is preferred; supported array forms are compatibility input. | Contract normalization |
| `application/core/chat.go` | Modified | Acquires/releases authority for exactly one ChatStream request. | Scoped lifecycle |
| `seelebridge/runtime_test.go` | Modified | Covers concurrent acquisition and a stale `plan_load` handler. | Defense in depth test |
| `manual_smoke_test.go` | Modified | Reports bounded Plan tool errors when an opt-in live High preflight is rejected. | Observable acceptance test |

The existing real-API High samples remain systems-acceptance evidence rather
than a statistically significant quality claim; the documented follow-up
protocol requires fresh sessions and at least ten samples per effort.

## Plan-load tool contract

| File | Change | Purpose |
|---|---|---|
| `seelebridge/plan_tool_provider.go` | Added | Decorates the framework `plan_load` registration with strict top-level, node, and edge contracts; includes array/object counterexamples, a valid adjacency-map DAG example, and schemas for node and edge map values. |
| `seelebridge/runtime.go` | Updated | Registers the decorated provider while preserving the framework's existing WorkPlan handlers. |
| `seelebridge/runtime_test.go` | Updated | Adds schema regression coverage, a smoke test that dispatches a valid DAG through the real handler, and a benchmark for repeated plan loads. |

API compatibility: unchanged. Verification: `go test ./seelebridge -count=1`, `go test ./... -count=1 -timeout=120s`, `go test ./seelebridge -run TestPlanLoadSmoke -count=1`, `go test ./seelebridge -run '^$' -bench BenchmarkPlanLoadSmoke`, and `go vet ./...`.

## Effort plan policy and default Plan Skill

| File | Change | Purpose |
|---|---|---|
| `application/prompt/effort.go` | Updated | Maps Lite/Medium/High/Max to explicit PlanPolicy constraints and prompt instructions. |
| `seelebridge/plan_policy.go` | Added | Validates Medium's four-node serial DAG and applies per-plan fork concurrency. |
| `seelebridge/plan_tool_provider.go` | Updated | Applies the runtime policy before delegating to Seele's `plan_load` handler. |
| `seelebridge/plan_preflight.go` | Added | Makes an isolated provider request that forces `plan_load`, then dispatches the validated plan before normal ReAct execution. |
| `application/core/chat.go` | Updated | Runs the policy-selected preflight before forwarding a normal user request to the chat engine. |
| `plugins/default/plan/SKILL.md` | Updated | Makes `#plan` the default Plugin's WorkPlan prompt, including valid JSON and effort rules. |
| `plugins/plan/` | Removed | Removes the standalone Plan Plugin; Plan tools are registered during Runtime startup. |

Policy: Lite leaves Plan optional; Medium allows at most four serial nodes with concurrency one; High allows DAG branches with concurrency three; Max permits every currently runnable node in a loaded plan to run concurrently.

Manual live verification: `manual_smoke_test.go` is excluded unless built with
the `manualsmoke` tag and an explicit `SEELEX_SMOKE_ACCOUNTS` path. It copies
the configured account file into a test temporary directory and verifies that a
real model request produces a successful forced `plan_load` call before normal
ReAct execution.

## Explicit recovery replan

| File | Change | Purpose |
|---|---|---|
| `seelebridge/plan_preflight.go` / `replan_guard.go` | Updated / Added | Adds the bounded `PrepareReplan` recovery request plus process-wide concurrency, operation-rate and provider-request budgets. A schema/policy rejection may make one corrective request only before any WorkPlan replacement; all other failures are not retried. |
| `application/core/chat.go` / `service.go` | Updated | Adds a user-selected replan branch after `plan_run` failure; it loads a replacement DAG but never automatically runs it. Interaction ID de-duplicates in-flight UI actions and each Plan chain accepts at most two successful recovery replacements. |
| `application/model/state.go` | Updated | Exposes a non-secret recovery monitor for in-flight work, budgets, successful/failed/rejected requests and provider-call cost proxy. |
| `seelebridge/replan_guard_test.go` / `runtime_test.go` | Added / Updated | Covers duplicate in-flight operations, global concurrency/rate/provider budgets, one safe corrective retry and its request accounting. |
| `docs/2026-07-29-replan/plan.md` | Added | Records the recovery path, bounded context and side-effect boundary. |
| `manual_smoke_test.go` | Updated | Extends the opt-in real API smoke to make a forced live replan request. |

Verification: core recovery tests, runtime forced-tool tests, guard repeat tests, `BenchmarkPlanLoadSmoke`, full repository test suite, and the opt-in real-account A/B smoke test. The live A/B observed a voluntary Lite `plan_load` control and a forced recovery `plan_load` treatment; the treatment uses one provider request when its first response is valid and two when the bounded corrective retry is needed.

## Plan DAG input adapter

| File | Change | Purpose |
|---|---|---|
| `seelebridge/plan_input_adapter.go` | Added | Normalizes LLM-friendly node and edge arrays, plus object target entries, into Seele's canonical object-keyed WorkPlan JSON without guessing ambiguous edges. |
| `seelebridge/plan_tool_provider.go` / `plan_preflight.go` | Updated | Applies normalization before policy validation and forced preflight dispatch; exposes both accepted forms in the function schema and prompt. |
| `application/core/chat.go` | Updated | Projects normalized `plan_load` arguments to the Application snapshot so UI PlanState matches the WorkPlan that ran. |
| `seelebridge/runtime_test.go` / `application/core/service_test.go` | Updated | Covers array adaptation, nested targets, missing edge sources, direct dispatch, tool schema, and UI argument projection. |

Canonical compatibility is preserved: existing object-keyed DAG JSON continues to be accepted. The adapter accepts `nodes[]` entries with `id` or `key` and `edges[]` entries with source/from plus target/to; it rejects missing or dangling IDs before execution so preflight may make one safe corrective request.

Post-adapter live A/B: three Lite array-DAG requests produced 3/3 valid, 6/6 structural-score Plans; High produced 2/3 within a 50-second per-sample limit, also 6/6. The High success path made two `plan_load` calls per sample (forced preflight plus normal ReAct), recorded as a follow-up cost/deduplication issue rather than a quality success claim.

## Authoritative preflight WorkPlan

| File | Change | Purpose |
|---|---|---|
| `application/core/chat.go` | Updated | Adds a per-turn, non-persistent seelex:plan-context:v1 authority envelope containing the canonical DAG and original request, then restores the original request after ChatStream. |
| `application/contract/ports.go` / `application_adapters.go` / `e2e/scenario/harness.go` | Updated | Adds the application-owned RuntimePort authority operation and adapters/stubs. |
| `seelebridge/runtime.go` / `plan_tool_provider.go` | Updated | Temporarily removes plan_load/plan_clear from the normal ReAct tool snapshot and retains a synchronized guard while the preflight WorkPlan is authoritative. |
| `application/prompt/effort.go` / `plugins/default/plan/SKILL.md` | Updated | Documents the preflight exception to the normal first-action `plan_load` rule. |
| `application/core/command_test.go` / `seelebridge/runtime_test.go` / `manual_smoke_test.go` | Updated | Covers envelope lifecycle, history cleanup, tool visibility restoration, and opt-in live acceptance of one actual High preflight load. |

The authority guard is scoped to the normal ChatStream only. It prevents
untrusted ReAct output from replacing the validated plan, then unlocks before
the user can choose the existing bounded explicit replan interaction.

## Durable context rehydration (2026-08-05)

| File | Type | Description | Design |
|---|---|---|---|
| `sessionstore/sessionstore.go` | Modified | The event-tail loader keeps a final unanswered user request as a valid standalone unit. | Durable tail / bounded protocol recovery |
| `application/core/task_context_state.go` | Modified | Application-side transcript reconstruction mirrors the durable-tail rule. | Single reconstruction contract |
| `sessionstore/state_test.go` | Added | Verifies the storage tail does not drop the final user request. | Storage contract test |
| `application/core/context_controller_test.go` | Added | Verifies provider history reconstruction retains the final user request. | Context unit test |
| `application/core/session_archive_test.go` | Added | Verifies resume then submit passes the trailing durable request into the provider history. | Resume integration test |

The fix keeps framework working history disposable while ensuring the durable
conversation rendered by the UI and the next cold-loaded provider context do
not diverge at an interrupted/cancelled turn boundary.

The follow-up recovery guard treats a checkpoint containing only version,
event-range, and timestamp metadata as non-context. Resume filters internal
checkpoint/control messages from both the visible conversation and transcript
tail, and falls back to a bounded protocol tail reconstructed from the durable
`SessionRecord.Conversation` when the transcript is empty or misses the latest
visible user turn. This prevents an empty checkpoint from replacing the actual
conversation after working history has been released from memory.

The rehydrated record tail is also copied back into Application transcript
state. Subsequent `prepareExecutionContext` calls therefore keep the same
durable user/assistant history instead of rebuilding from an empty in-memory
tail. Metadata-only checkpoints are no longer emitted into provider history.
The long-context smoke test re-asks `我的名字是什么？` after twelve large
turns and verifies that the durable `user_name=hzr` checkpoint and opening
question reach the next provider request.

The authoritative context uses the dedicated
seelex:plan-context:v1 authority=preflight-loaded envelope, parallel to the
Skill context envelope. Live verification completed in 49.06 s: High issued
one actual role=tool plan_load call and no plan_run; explicit replan used one
additional provider request. The earlier apparent second High plan_load was
the UI tool_result lifecycle record for the same invocation, not a provider
or tool call.

## Atomic PlanAct scope follow-up (2026-07-29)

| File | Type | Description | Design |
|---|---|---|---|
| `seelebridge/plan_authority.go` | Modified | Replaces load-then-lease with atomic preflight/authority scope; private context is the only preflight mutation capability. | State machine / Capability |
| `seelebridge/plan_tool_provider.go` | Modified | Guards stale `plan_clear` as well as `plan_load`; documents invalid top-level node IDs. | Defense in depth |
| `manual_smoke_test.go` | Modified | Uses independent phase budgets and asserts High DAG structure; Lite is an observational A/B control. | Resilient smoke harness |

## Prompt assets and evidence-aware PlanAct (2026-07-29)

| File | Type | Description | Design |
|---|---|---|---|
| `internal/promptassets/assets/` | Added | Versioned Markdown sources for Seelex identity, system behavior, effort levels, and PlanAct preflight/replan prompts. | Embedded resource / Template |
| `internal/promptassets/promptassets.go` | Added | Build-time asset embedding and rendering of runtime policy facts without exposing user input or credentials. | Template method |
| `application/prompt/effort.go` / `application/core/service.go` | Updated | Load system and effort prompt layers from assets instead of Go string literals. | Composition |
| `seelebridge/plan_preflight.go` | Updated | Renders Plan policy limits into the preflight/recovery templates. | Policy projection |
| `gui/frontend/dist/read-sources.*` | Added | Projects successful `read_file` calls into the GUI's Agent-read-files sidebar. | Pure projection |

The Plan templates require code-review plans to use `inspect -> verify -> report`
with evidence-backed findings, distinguish confirmed facts from hypotheses, and
use explicit node-ID anti-examples to prevent entry/node mismatches.

## CI formatting and session-catalog race fix (2026-08-05)

| File | Change | Purpose |
|---|---|---|
| `application/core/service_test.go` | Replaces test constructors with `testing.TB`-aware fixtures and adds a catalog-worker cleanup regression. | Makes dependencies immutable after construction and guarantees `Shutdown` through `t.Cleanup`. |
| `application/core/*_test.go` | Moves Session/Runtime/Engine customizations into construction-time options; wraps bespoke `Dependencies` fixtures too. | Removes all post-construction Service dependency writes. |
| `application/core/compressed_turn.go` | Copies session/workspace IDs under `service.mu.RLock`, then performs storage I/O after unlock. | Protects Application snapshot reads without extending the lock around external I/O. |
| Five Go files listed by CI | Applied `gofmt`. | Restores formatting gate compliance. |
| `.github/workflows/ci.yml` | Rejects test writes to `service.deps.*`. | Prevents this race pattern from being reintroduced. |

Verification: `go test ./application/core -run '^(TestPersistenceFailureDoesNotClaimProgressWasSaved|TestReadCompressedTurnHandlerReadsOriginal|TestSessionArchivePreservesVisibleHistoryPlanAndReadCache|TestPersistSessionRecordMergesBoundedProjectionWithFullHistory|TestResumeSessionUsesRecordWhenProviderHistoryIsUnavailable)$' -count=20`, `go test ./... -count=1 -timeout=120s`, `go build ./...`, GUI-tagged build, `go vet ./...`, and `gofmt -l .` all pass. Windows local `-race` cannot run with `CGO_ENABLED=0`; Linux CI remains the race-validation authority.
