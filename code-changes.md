# 代码变更摘要

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
