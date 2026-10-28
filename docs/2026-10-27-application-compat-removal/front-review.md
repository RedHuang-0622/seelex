# Application 兼容层退场前置审查

日期：2026-10-27

基线提交：`39e5afe0 refactor(application): retire core package`

## 需求摘要

彻底删除 `application` 的兼容门面：以 `application/compat`、`application/faces_api.go`、`assembly.Frontend`、`FrontendPorts` 和 test-only 转发混入层为主，把 GUI、TUI、headless、console、wiring 与工具注册改为直接消费窄能力组件；保留业务状态所有者，不把 194 个转发方法整体搬到另一个包。

## A/B 基线

| 项目 | A 工作树（删除前） | B 工作树（删除后） |
|---|---|---|
| 路径 | `C:\Users\redre\.codex\worktrees\application-compat-removal\seelex` | `C:\Users\redre\.codex\worktrees\application-compat-removal-b\seelex` |
| 起点 | `39e5afe0`，干净 | `39e5afe0`，干净 |
| 作用 | 只读基线、回归对照 | 唯一允许编辑的实现臂 |

A 已执行并通过：

```text
go test ./application/... -count=1 -timeout=120s
go test ./gui ./tui ./headless ./wiring/... -count=1 -timeout=120s
```

删除后的 B 必须重跑同一组命令，并增加 console、e2e、仓库级 test、build、vet 与结构门禁；A/B 结果不一致即视为回归。

## 当前债务事实

| 位置 | 事实 |
|---|---|
| `application/compat/frontend.go` | 1089 行、194 个函数；其中大量方法只转发到 `assembly.FrontendPorts` |
| `application/compat/frontend_test.go` | 用反射和 AST 证明兼容方法存在、且只转发 |
| `application/assembly/frontend_ports.go` | 49 个扁平字段与多处回调，是前端兼容面的装配快照 |
| `application/assembly/service.go:66` | `type Frontend struct{ *Service }` 是 test-only 兼容入口的类型依赖 |
| `application/assembly/frontend_migration_test.go` | 403 行、128 个 test-only 转发方法 |
| `application/assembly/frontend_shell_legacy_test.go` | 10 个 test-only `Service` 转发方法 |
| `application/assembly/*_test_aliases*.go` | 15 个文件，把已迁出能力重新投影给旧测试名 |
| `application/faces_api.go` | 旧 `ViewFace/CommandFace/PolicyFace/QueueFace` 与多组宽能力接口的事实源 |
| `application/frontend_assertions.go` | 把兼容 Frontend 与旧宽接口做编译期绑定 |
| `wiring/constructors.go:48` | 生产构造仍返回 `application.Frontend` |
| `wiring/registry/registry.go:46,126,195,283,333,402` | 工具注册反复构造兼容 Frontend |
| `gui/bridge.go:24,300,353` | GUI Bridge 依赖一个 100+ 方法的宽接口，且用 optional interface 做能力探测 |
| `tui/ui.go:17,48,86` | TUI 直接绑定兼容 Frontend |
| `headless/headless.go:44` 起 | headless 直接绑定兼容 Frontend，并用 optional interface 探测扩展能力 |

## 代码归属与影响文件

| 文件/目录 | 修改类型 | 具体位置 | 归属结论 |
|---|---|---|---|
| `application/compat/` | 删除 | 整个目录 | 退场；不得搬迁或改名复制 |
| `application/application.go` | 修改 | `applicationcompat` import、`Frontend` alias | 删除兼容 alias；只保留稳定类型/常量/错误 |
| `application/faces_api.go` | 删除或拆散 | 整个文件；仅 `ContextCompactionResult` alias 移回 `application.go` | 禁止保留宽入站兼容接口 |
| `application/frontend_assertions.go` | 删除 | 整个文件 | 无 Frontend 后失去事实意义 |
| `application/assembly/frontend_ports.go` | 重写 | 49 个字段与 `NewFrontendPorts` | 改为 assembly 内部/组合根使用的数据型 `Components`；不得含 UI 方法 |
| `application/assembly/service.go` | 修改 | `type Frontend` | 删除 test-only 兼容类型 |
| `application/assembly/frontend_migration_test.go` | 删除 | 整个文件 | 128 个 test-only 转发方法 |
| `application/assembly/frontend_shell_legacy_test.go` | 删除 | 整个文件 | 10 个 test-only `Service` 转发方法 |
| `application/assembly/*_legacy_test.go` | 删除 | shim 文件 | 行为测试若仍有效，迁到 owner 包或用 owner API 重写 |
| `application/assembly/*_test_aliases*.go` | 删除 | 15 个文件 | 测试直接调用 owner，不再维持旧名字 |
| `wiring/constructors.go` | 修改 | `NewFrontend` | 删除；构造按 consumer dependencies 在 bootstrap 完成 |
| `wiring/bootstrap/bootstrap.go` | 修改 | 启动通知、project board sink、lifecycle、scheduler observer | 直接使用组件/owner |
| `wiring/bootstrap/application.go` | 修改 | role permission provider | 直接使用 RoleApplication |
| `wiring/registry/registry.go` | 修改 | 6 处 `applicationcompat.New(app)` | 按函数只传所需 owner |
| `gui/bridge.go` | 拆分修改 | `Application` 宽接口、optional interfaces、约 120 个转发调用点 | Bridge 持有按领域拆分的依赖，不导入兼容门面 |
| `gui/shutdown.go` | 修改 | `app Application` | 拆分 state/lifecycle 依赖 |
| `gui/terminal_bridge.go` | 修改 | Snapshot 读取 | 直接使用 snapshot owner |
| `tui/*.go` | 修改 | `AppController`、`NewModel`、stream/dialog/panel | 使用 TUI 自有的窄依赖集合 |
| `headless/*.go` | 修改 | `Application` 与 role/goal/subagent/schedule 扩展接口 | 使用 headless 自有 dependencies；删除 optional interface 探测 |
| `console/*.go` | 修改 | backend console 的 app 参数 | 使用 console 自有 dependencies |
| `wiring/bootstrap/frontend*.go` | 修改 | StartFrontend/InitTUI/startGUI | 从组件构造各前端依赖 |
| `application/query_faces_test.go` | 删除 | 整个文件 | 测的是兼容 Frontend 满足旧查询面 |
| `application/api_partition_test.go` | 删除 | 整个文件 | 旧宽 API 分组失去意义 |
| `application/api_contract_test.go` | 修改 | 文件清单 | 只校验仍存在的稳定 application 契约面 |
| `application/architecture_budget_test.go` | 修改 | compatibility ledger 字段与断言 | 兼容层必须退场；不再维护永久兼容台账 |
| `application/testdata/application_budget.json` | 修改 | `compatibility_ledger*` 字段 | 删除兼容台账输入 |
| `e2e/facade_boundary_test.go` | 修改 | 依赖白名单、类型身份、构造器门禁 | 删除 `application/compat` 白名单和 Frontend 断言 |
| `contract/runtime_migration_test.go` | 修改 | `applicationcompat.New(app)` 字符串断言 | 改为断言直接 owner 注册 |
| `application/README.md`、`application/assembly/README*.md` | 修改 | 所有 compat/兼容门面段落 | 只描述终态 |
| 相关子模块 README | 修改 | 首次调用方与数据流 | 从 compat 改为 owner 组件 |

## 方法归属

194 个兼容函数按 owner 收回，不在新类型上重写：

| owner | 兼容入口组 |
|---|---|
| `admin.AccountApplication` | AddAccount、AccountDetails、UpdateAccount、DeleteAccount、AccountBalance、SetAccountWindow、SelectAccount |
| `input.Application` | Suggestions |
| `policy.SwitchApplication` | ActivateSkill、SwitchEffort、SwitchPlugin、SetFullAccess、SetPermissionTier、SetLoopLimit、SetReasoningEffort、SessionReleaseWindow、SetSessionReleaseWindow |
| `policy.RoleApplication` | RoleDefaultVector、RoleAccount、Inheritance、SSH user spaces、permission vector 的读写 |
| `contract.RoleSessionsPort` | Role session CRUD、draft/rows、lifecycle、schedule register/cancel/fire |
| `session/query.Application` | ListSessions、WaitCatalogRefresh、GetSessionTranscript、SessionInputIndex、SnapshotOf |
| `session.MetaApplication` | SessionMeta、SetSessionMeta |
| `session.Archive/Delete/DraftEntry/Lifecycle/Resume/SSH` | 对应 archive/delete/begin/unload/resume/SSH 操作 |
| `conversation.UnifiedApplication` | ConversationView、ConversationChat、RecoverConversation |
| `conversation.QueueApplication` | ReorderQueuedInput、RecallQueuedInput |
| `input.SubmissionApplication` | Submit |
| `session.SubmitApplication` | SubmitToSession |
| `session.Interaction/ForkApplication` | ResolveInteraction、ForkSession、ForkSessionLatest |
| `conversation/context.Application` | CompactContextNow、CompactContextHandler |
| `reference.Application` | ToolResultContent、read_* handlers |
| `execution.WorkItemApplication` | WorkItemExecutionFor、Teammate read、UpdateWorkItemStatus |
| `execution.ProjectBoardApplication` | ProjectRootFor、WorkTableRowsFor |
| `execution.SubagentApplication` | recovery/fork/resume/detail/clear |
| `goal.Application` | goal begin/update/done/status/governance |
| `review.Application` | RequestFileReview |
| `scheduler` 三个 Application | schedule/run/session |
| `lifecycle.Application` | any-running、wait-idle、drain/cancel/shutdown |
| `viewstate.Coordinator` / `SnapshotApplication` | subscribe、subscribe session、snapshot |
| `task.Coordinator` / TerminalHandlerApplication | GoalSkillActive、terminal handler |
| `toolrecord.Application` | enable/apply/flush/store/board/command outcome；role close 应增加 owner 方法而不是闭包 |
| `workspace.Application` / `workspace/switch.UseCase` | environments、git、file、bind/create/tree/write |

`PrepareProviderHistory` 直接走 `conversation/context.EngineHistoryCoordinator`；`PromptLayers` 直接走 `prompt.PromptStack`。

## 依赖分析

- 上游：`application/assembly` 继续拥有跨组件状态、锁、生命周期和装配顺序。
- 直接消费方：`gui`、`tui`、`headless`、`console`、`wiring/bootstrap`、`wiring/registry`、`e2e/scenario`、`test`。
- 下游：各 `application/<domain>` owner 的结构接口不变，不新增业务状态机。
- 禁止方向：领域包 import GUI/TUI/wiring；frontend import backend/store。
- 循环风险：`application` 仍 alias `assembly.Service`。因此 `assembly` 不能 import 根 `application`；前端依赖接口必须声明在 consumer 包或只由 wiring 装配，不能把根门面类型塞回 assembly。

## 风险

| 风险 | 概率 | 严重度 | 缓解 |
|---|---|---|---|
| 把 194 个方法整体搬到根包或新“frontend”包 | 高 | 极高 | 结构门禁禁止任意 Frontend 方法；按 owner 改调用点 |
| GUI 行为回归 | 高 | 高 | 分领域小批迁移；A/B 同命令比较；保留 Bridge 行为测试 |
| test-only shim 继续掩盖生产缺口 | 高 | 高 | 结构扫描必须包含 `_test.go`；删除 alias/legacy 文件 |
| `SelectAccount` / `SetAccountWindow` 的 `context.Background()` 隐式语义被无意改变 | 中 | 中 | parity 批保持现状；后续作为独立 bugfix 决策 |
| 前端依赖接口重新长成 God interface | 中 | 高 | 单接口 ≤20 方法；按页面/用例拆；编译期只绑定实际使用面 |
| 装配快照变成新的万能 bag | 中 | 高 | 组件快照只含字段，不含方法；按 Conversation/Session/Execution/Workspace/Policy/Admin/Runtime 分组 |
| 删除测试时误删唯一行为覆盖 | 中 | 高 | 先分类行为测试与转发测试；行为测试迁 owner，只有纯转发/旧名字断言可删 |

## 建议方案

采用“消费方依赖 + assembly 数据型组件快照”的终态：

1. `assembly` 保留 `Service` 的状态协调职责，另提供只含 concrete component 指针的分组快照，不实现 UI 方法。
2. `gui`、`tui`、`headless`、`console` 各自声明本包真正消费的窄接口，并组合成各自的 dependencies 值。
3. `wiring/bootstrap` 是唯一把 `assembly` 组件装配成各前端 dependencies 的 composition root。
4. 工具注册按函数接收需要的 owner，不再构造统一 Frontend。
5. 所有调用方归零后删除 `application/compat`、旧宽接口、assembly 兼容类型和 test-only shim。

该方案比“将 compat 改名为 frontend”多改消费者，但不会把现有 God facade 原样挪到新目录。
