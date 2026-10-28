> 历史记录，不描述当前实现。本文记录退场执行台账。

# Application 兼容层退场 执行台账

事实源：`front-review.md`（债务与归属）、`plan.md`（方案 C 与里程碑）、`deletion-list.md`（删除 ratchet）。
本文件只记录**执行进度与证据**，不重复设计论证。

工作树：A = `C:\Users\redre\.codex\worktrees\application-compat-removal\seelex`（只读基线 `39e5afe0`，干净）；
B = `C:\Users\redre\.codex\worktrees\application-compat-removal-b\seelex`（唯一实现臂）。

## 1. 里程碑状态

| 里程碑 | 状态 | 证据 / 缺口 |
|---|---|---|
| M0 A/B 脚本 + 结构红测 | 已交付 | B：`scripts/application-compat-ab.ps1`（78 行，三组命令双臂采集）、`application/compat_retirement_test.go`（174 行，AST 扫描含 `_test.go`）。compat 存在时两个用例必红 —— 见 §3 首轮红测记录 |
| M1 组合面替换 | 已交付（预算待挤行） | `assembly/frontend_ports.go`（159 行、49 字段）已删 → `assembly/components.go`（198 行、7 组 47 字段、0 func 字段，纯数据）；`FrontendPorts`/`NewFrontendPorts` 全仓生产引用 = 0；bootstrap/registry 已改直连 owner（`registry.go` 的 `applicationcompat.New` = 0）；`go build ./...`、`go vet` 全绿。**唯一残留**：`frontend_gui.go:28` 仍经 `wiring.NewFrontend` 取门面 → 属 M3。行数 7123 > 预算 7092 已裁决「先挤 assembly 再谈基线」（§5.4） |
| M2 TUI 替换 | 已交付 | `tui/dependencies.go`（10 个 consumer 端口 + `Dependencies` 装配 struct）；`NewModel(deps Dependencies)`，`model.app` 引用 = 0，`AppController`/`queueController`/`toolRecordReader` 三个宽面/探测面已删；装配表 `wiring/bootstrap/frontend_tui.go:tuiDependencies(assembly.ComponentsFor(service))`。详见 §2.2 |
| M2 剩余：headless + console | 已交付 | headless 17 个 per-owner 端口 + 17 个骨架字段、9 处运行时断言 → 0；console 5 个端口、死字段 `View`/`Workspace` 已删；装配表 `wiring/bootstrap/frontend_ports.go`（17 行 + 3 行）；23 文件 +494/−528（净 −34）。详见 §2.3 |
| M3 GUI 替换 | 未开始（口径已定） | `gui/bridge.go:24 type Application`（宽接口）+ `:353 NewBridge` 未拆，`:251-257,864` 8 个编译期断言仍绑 `*application.Frontend`，`:1758` 「工具记录未装配」判空未收。ctx 口径已裁决为「端口带 ctx，随门面消失」（§5.2） |
| M4 兼容面物理删除 | 仍是缺口 | `application/compat/frontend.go` 1160 行 / 192 个 `*Frontend` 方法仍在；`application.go:21` alias、`assembly/service.go:67` `type Frontend struct{ *Service }`、15 个 alias 补丁、`frontend_migration_test.go` 全在；compat 真实 importer 5 个文件（见 §4 计数） |
| M5 终验与 A/B 报告 | 部分（首轮已跑） | 首轮全仓：`build-exit=0`、`vet-exit=0`、`go test ./... -count=1` exit=1，红面 = `application` 三个故意的结构门禁 + `backend/engine/account` 一个时钟抖动用例（隔离 3/3 绿，非回归）。见 §4 |

## 2. 各轮改动清单

### 2.1 M1 收尾（组合面替换）

| 项 | 结果 | 证据 |
|---|---|---|
| registry 4 个 read/search handler 形状不匹配 | 已修 | 判据只在一处：`wiring/registry/registry.go` 新增 `argsOnly`，把 owner 的 `func(argsJSON string)` 提升到 `RegisterTool` 要求的 `func(context.Context, string)`。owner 侧不新增无人读取的 ctx 参数（旧兼容门面同样丢弃 ctx） |
| `Components` 扁平 49 字段 → 7 组 | 已修 | `application/assembly/components.go`：`Admin/Conversation/Session/Execution/Workspace/Policy/Runtime` 七个 struct，47 个 concrete owner 指针字段，0 个 func 字段；文件头注释登记「字段里不许出现 func」的理由与判据回归 owner 的去向 |
| `AddNotice func(string)` | 已修 | 终态 `components.Session.View.AddNotice`（`view_state.Coordinator` 的方法）；bootstrap 三处调用点直接走 owner |
| `PublishRuntime func()` | 已修 | `assembly.PublishInitialRuntimeProjections(service)`（`runtime_projection.go`，+9 行）显式触发；对照 A 的 `frontendApp.PublishRuntimeProjections()` 同位同序，行为不变 |
| `SetProjectBoardSink func(...)` | 已修 | `projectBoardPort` → 导出 `ProjectBoardSinkPort`（带 `Store` 方法），快照持指针而非闭包；bootstrap 直接 `components.Execution.ProjectBoardSink.Store(...)` |
| `CloseRoleToolRecord func(...)` | 已修 | 判据入 owner：`toolrecord.Application.CloseRoleUnit(main, role)`；`RoleUnitID` 拼接不再出现在装配根 |
| `bootstrap.go` `frontendApp` 残留 | 已修 | `frontendApp := wiring.NewFrontend(app)` → `components := assembly.ComponentsFor(app)`；逐项改 owner 读数（`Runtime.Lifecycle.Shutdown`、`Policy.Role.RoleSSHUserSpaces`、`Conversation.ContextHistory.PrepareProviderHistoryFor`、`Execution.RoleActivity.Handle`、`Runtime.Runtime.Refresh` 等） |
| `toolCommandOutcomeObserver(next, *application.Frontend)` | 已修 | 改持 `*toolrecord.Application`（consumer 侧只用到 `RecordCommandOutcome`） |
| console 的 Snapshot + 工作区两面 | 已修 | `console.BindProject(components.Session.Snapshot, components.Workspace.Switch, …)`：按 consumer-owned 依赖入参，不再传宽门面 |
| assembly 死代码 | 已删（−18 行） | `skill_context.go` 的 `writeSkillItem`（含随之失效的 `fmt` import，−10）与 `service_scheduler.go` 的 `withScheduledRunDefaults`、`(*Service).validateScheduledRunParams`（−8）。判据：全仓 `.go` 引用 = 0，owner `scheduler.WithRunDefaults`/`ValidateRunParams` 仍有包内调用方，未产生新死代码 |
| README 与实现同步 | 已修 | `application/compat/README.md`（过时类型名）、`wiring/registry/README.md`（4 处：文件表、核心实现、「做」、数据流）；`gen_core_readme_index.py --check` 现报「检查 21 个 README，无漂移」 |
| 仓库根临时脚本 | 已清 | 8 个 `_*.py`/`_*.tsv` 分析脚本移出仓库（AGENTS.md 禁止仓库根散落临时件），分析结论落本台账 |

### 2.2 M2 TUI（consumer-owned 端口替换）

| 项 | 结果 | 证据 |
|---|---|---|
| TUI 宽门面 `AppController`（11 方法） | 已删 | `tui/dependencies.go`：10 个端口 + `Dependencies` struct 替换。端口按 **owner** 切（不按 UI 关切切，否则需要门面把两个 owner 再缝一次），方法名逐字沿用 owner，owner 指针**结构上满足**端口 —— 无适配器、无 `_ tui.X = (*owner)(nil)` 断言行 |
| 10 个端口的调用点依据 | 已核 | `SnapshotReader`←`deps.Snapshot.Snapshot()`；`EventReader`←`Subscribe`/`SubscribeSession`；`Submitter`←`submitInput`；`SuggestReader`←`currentSuggestions`（`tui/view.go:35,95` 活调用）；`InteractionWriter`←`resolveInteraction`（3 处）；`TurnStopper`←`CancelChat`；`EffortSwitcher`←`SwitchEffort`（owner 不收 ctx，端口随之不收）；`HistoryPager`←`loadMoreHistory`/`loadLatestHistory`；`QueueEditor`/`ToolRecordReader` = 可选能力（装配可为 nil） |
| `SelectAccount`/`SwitchPlugin` | 已删 | TUI 生产代码零调用点 → plan 秩序 4「没有真实调用点的接口不创建」，两个 fake 方法随之删除 |
| 运行时类型断言探测 | 已删 | `model.app.(queueController)`、`model.app.(toolRecordReader)` 两处探测消失；能力改由装配决定（`Dependencies.Queue == nil`、`Dependencies.ToolRecord == nil`）；「当前前端不支持队列编辑」判据收进唯一一处 `useQueueEditor()`（tui.go:335，调用点 350/366 共用） |
| `model.app` 单点依赖 | 归零 | `tui` 包内 `grep "model\.app"` = 0；`NewModel(deps Dependencies)`（tui.go:50），快照初值 `deps.Snapshot.Snapshot()`（:64） |
| 装配表 | 已建 | `wiring/bootstrap/frontend_tui.go:tuiDependencies(assembly.ComponentsFor(service))` 一行一端口、右值 = 拥有该用例的组件指针，绑定语句本身即编译期结构断言；`InitTUI/startTUI` 形参改 `*application.Service`，`frontend.go` 的 `case "tui"` 随之传 `app`，`frontend_gui.go` stub 同步签名 |
| 测试假实现 | 已改 | `fakeApp`/`queueFakeApp`/`toolRecordFakeApp` 各加 `deps()` 显式列出自己真提供的端口（Queue/ToolRecord 在缺能力的假实现上保持 nil，与迁移前「断言失败即退场」形状一致）；`ResolveInteraction`→`Resolve`、`SwitchEffort(ctx,…)`→`SwitchEffort(…)`、`ToolRecordBoard()`→`Board()`；42 个 `NewModel(...)` 调用点全改造（剩余未改造 = 0） |
| 死测试夹具 | 已删 | `tui/goalteam_test.go` 的 `teamFakeApp`（`AgentTeamView` 读面无生产调用方、字段无读者）与 `teamViewFixture()`；随之修正该文件验收口径第 2 条：Alt+T 自 2026-10-12 J-1a 起只读快照投影，不排异步命令 |
| `tui/stream.go` 的 `submitInput`/`loadMoreHistory`/`loadLatestHistory`、`dialog.go` 的 `resolveInteraction`、`types.go` 的 `currentSuggestions`、`tool_record_panel.go` 的 `toolRecordLines` | 已改 | 形参/取值从宽门面换成对应窄端口（`Submitter`/`HistoryPager`/`InteractionWriter`/`SuggestReader`/`ToolRecordReader`），无逻辑改动 |

### 2.3 M2 剩余（headless + console 迁到 consumer-owned 端口）

| 项 | 结果 | 证据 |
|---|---|---|
| headless 宽门面 `Application` | 已拆 | `headless/dependencies.go`：**17 个端口 / 17 个 `Dependencies` 字段**，一字段一 owner（Events/Snapshot/Turn/Submission/Draft/Resume/Fork/Interaction/History/Catalog/Workspace/Policy/Perf/ToolRecords/Roles/Goal/Subagents），端口方法名逐字 = owner 方法名，owner 指针结构满足端口 → 无适配器、无 `_ X = (*impl)(nil)` 断言 |
| `server.app` 单点依赖 | 归零 | `headless` 包内 `grep "server\.app"` = 0（仅文档叙述保留字面）；旧形态 9 处运行时类型断言探测 5 个可选面全部消失，能力有无改由装配表说话 |
| 「未装配」错误面 | 按不可达删除 | 生产路径上 `Application` 唯一实现者是 compat 门面，它实现全部 5 个可选面（`application/compat/frontend.go:750,775` 等），断言恒为真；false 分支只被测试里刻意只实现一部分面的假实现触达，且**无任何用例断言**那条「未装配」错误 → 判为不可达错误面删除，不复制成新端口。**注**：本行初稿写作「实测这 5 处断言从未判为无」，实测口径错误（9 处断言 / 5 个面，false 分支测试可达），已按测量结果改写并同步 `headless/README.md:33-36` |
| console 端口 | 已建 | 5 个端口，`Dependencies{Submission,Turn,Events}`；死字段 `View`/`Workspace` 删除（无任何调用点读取），项目选择改为 `BindProject(view ViewSnapshot, commands WorkspaceBinder, rootPath string)` 显式入参 |
| 装配表 | 已建 | `wiring/bootstrap/frontend_ports.go`：headless 17 行 + console 3 行，右值与门面转发目标逐字同构；绑定语句即编译期结构断言 |
| 测试假实现 | 已按 owner 拆分 | per-owner fake 落在 `gui/headless_ports_test.go`（+ 另 5 个 gui/headless 文件）；一个端口一个 fake，无驱动用例的端口不建 fake |
| RPC 对外契约 | 未变 | `/rpc` 方法名字符串零改动（Go 侧落到哪个 owner 不改变线上名字），外部驱动无需改动 |
| 规模与守恒 | 已核 | `git diff --stat -- headless console gui wiring contract` = 23 文件、+494/−528（净 **−34**）；`^func Test` 计数 A = B（gui 93；console+headless+bootstrap 7）；`grep -c "^func Test" tui/*_test.go` A = B = 36 |

### 2.4 四条裁决落地（2026-10-27 本轮）

| 裁决 | 状态 | 实测证据 |
|---|---|---|
| **§5.1 删 11 处守卫，只留 owner 一处** | 已交付 | `grep -c "if service.toolRecords != nil" application/assembly/` **11 → 0**；分布：`service_assembler.go` 3、`service_assembler_applications.go` 2、`service_assembler_session.go` 2、`service_assembler_conversation_turn.go` 1、`service_assembler_scheduler_lifecycle.go` 1、`service_assembler_session_draft.go` 1、`tool_hooks.go` 1。删除安全性由「守卫恒不成立」证明：`service.toolRecords` 全仓**唯一**赋值点 `application/assembly/service_assembler.go:173` 是装配函数体内无条件直线语句（不在任何 `if` 内，前置无 `return`），且 `grep -rn "toolRecords:\s*nil\|toolRecords = nil" application/` = **0** —— 生产与测试都不存在 nil 路径，守卫是死代码而非防御。owner 侧唯一判据保留：`application/execution/toolrecord/application.go:131,134,138`（`writable`/`store`/`accumulator` 三个可选依赖），`host.go:6,15,29,34` 四个方法体无 nil 判据（接收者恒非 nil，无需第二判据） |
| **§5.4 先挤 assembly，再谈基线** | 进行中（第一笔已进账） | 行数 **7123 → 7101**（−22 = 11 守卫 × 2 行），预算 7092 → **仍超 9 行**。门禁仍红且红因是规模不是行为：`go test ./application -run TestApplicationArchitectureBudget` exit=1 → `architecture_budget_test.go:68: application/assembly 生产代码 = 7101 行，预算 = 7092`。`application/testdata/application_budget.json` 未改动（纪律：只允许下调，抬高须用户裁定）。剩余 9 行由有界扫描子代理给出候选清单后再删，候选仅限「死代码 + 同一判据多份实现」，禁止新增抽象层、禁止把文件挪出包躲计数器 |
| **§5.2 端口带 ctx，随门面消失** | 口径已定，本轮零代码改动 | GUI 端口方法签名从 owner 逐字继承（含首参 `context.Context`），不新增无 ctx 变体。两处 `context.Background()` 替换点 `application/compat/frontend.go:835`（`SetAccountWindow`）、`:845`（`SelectAccount`）随 D1 删除门面一并消失，owner 侧 `application/admin/application.go:113,140` 与 `backend/engine/assemble/runtime_account.go:192,240` 不动 → 本轮行为零变化。端到端 ctx 贯通是**新独立议题**（须走 AGENTS.md §9 外部依赖批次，跨 `contract/ports.go:120,582` → `backend/engine/assemble` → `application/admin` → GUI 四层），不在本工作包内 |
| **§5.5 加过期标记 + 白名单豁免** | 子代理在飞 | 规格已下发：`docs/2026-10-25-application-modularization/{implementation,remaining-work}.md` 首行加含 `历史记录，不描述当前实现` 的标记；门禁（`application/compat_retirement_test.go:14` 起，现为 `WalkDir` 只扫 `application/`）扩扫全仓 `*.md`，豁免条件 = 前 10 行含该标记字面，**不做文件名硬编码白名单**，且反向断言两份历史文档确实带标记（删标记即变红并指名文件）。要求交付含非空转证据：摘掉标记 → 变红 → 恢复 → 变绿 |

**本轮 §5.1 验证记录**（全部落盘后 grep，未用 `| tail`）：

```text
gofmt -l <7 files>                     → 空（A 臂同包基线亦空，证明非既有脏格式）
go build ./...                         → exit=0，日志 0 行
go test ./application/assembly/... ./application/execution/toolrecord/... -count=1
                                       → exit=0，ok 包 2，FAIL/panic 0（/g/tmp/s51-test.log）
```

### 2.5 M3 GUI 调用点实测清单（只读子代理，2026-10-27）

宽接口与注入路径（实测）：

| 项 | 实测 |
|---|---|
| 宽消费接口 | `gui/bridge.go:24 type Application interface` = **55 个方法** → 违反 plan 秩序 4（单接口 ≤20），必须拆 |
| 注入链 | `gui/run_wails.go:18 NewBridge(app, …)` ← `wiring/bootstrap/frontend_gui.go:28 gui.Run(wiringroot.NewFrontend(service), …)` ← `wiring/constructors.go:48` 返回 `*application.Frontend`（= `application/application.go:21` = `applicationcompat.Frontend`）；`gui/*.go` 内 `applicationcompat`/`wiring.` 引用 = **0**，门面是外部塞进来的 |
| 消费字段 | `Bridge.app`（`bridge.go:301`）、`closeCoordinator.app`（`shutdown.go:30`） |
| 编译期断言 | `bridge.go:251,252,253,254,255,256,257`（`_ &lt;Iface&gt; = (*application.Frontend)(nil)` ×7）+ `bridge.go:864` = **8 处**，全部随门面退场改由装配表绑定承担 |
| 可选面探测 | `bridge.go:484,489,490,766,790,801,812,823,837,846,870,880,890,894,1111,1143,1517,1745,1756`、`shutdown.go:78,106` ≈ **20 处**；错误面含 `:768`「未装配消息编辑分叉面」、`:1109/:1141` "bridge is not assembled"、`:1113`「未装配角色设置面」、`:1145`「未装配会话 SSH 面」、`:1519`「未装配角色会话读面」、`:1758`「工具记录未装配」及 6 处 "session-scoped API is not supported / queued input editing / session input index" |
| 门面名 ≠ owner 名 | **≈24 个**（如 `BeginNewSession`→`Begin`、`ResolveInteraction`→`Resolve`、`ConversationView`→`View`、`WorkspaceTree`→`Tree`、`SubagentSessionDetail`→`Detail`、`ToolRecordBoard`→`Board`）。秩序 4/plan:159 要求端口逐字采用 **owner 名**，改名吸收在 Bridge 方法层，禁止在门面与端口之间再造第三层 |

端口分组（按 owner 聚合，全部 ≤12 方法，满足 ≤20 上限）：View/事件订阅、会话提交与排队、会话生命周期与目录、composer 策略、admin 账号、权限档位、角色权限设置（12）、看板与 teamwork、subagent 单元、workspace 文件与 git、项目环境与会话 SSH、scheduler、历史检索/工具记录/读面 —— 共 **13 组**。

两个附带发现（不在 M3 主线，须单独裁决）：

1. `gui/bridge.go:16` import `middle/protocol/model` → 与 plan 秩序 6「前端只 import `application`/`contract` 类型与自己的子包」冲突，属既有违规，不在本包删除范围内时不应顺手改；
2. `ToolRecordEnabled()` 在 `bridge.go:1736` 的 `toolRecordBoardProvider` 声明，GUI 侧**从未调用** → 按死字段规则应删除，不迁移。

### 2.6 M3 分批口径（切片顺序与过渡形态）

`bridge.go` 是单车道 God 文件（AGENTS.md §11 规则 3），M3 只能串行切分，每片满足「可编译 + 受影响测试绿 + 宽接口只减不增」：

- 过渡形态：`gui` 新增 `Dependencies`（consumer-owned 端口字段），`frontend_gui.go` 装配表逐字段绑定 owner；已迁移组改走 `deps.X`，未迁移组仍走 `Bridge.app` 宽接口。**宽接口逐片收缩、最后一片删除**，不许为省事把 `Application` 保留成第二个入口。
- 每片交付须含：删除的断言编号、消失的探测点数、迁移调用点数、owner 名采用清单、对应 fake（一个端口一个 fake，无驱动用例不建 fake）。

切片计划（串行，按 owner 聚合；方法数 = 门面面，调用点数 = `bridge.app.X()`/`coordinator.app.X()` 实测点）：

| 片 | 组（owner） | 门面方法 | 调用点 | 附带消失项 |
|---|---|---|---|---|
| S1 | admin 账号（`application/admin.AccountApplication`）7 + 权限档位（`policy.SwitchApplication`）2 | 9 | 9 | — |
| S2 | 角色权限设置（`policy.RoleApplication`）12 + composer 策略（`policy.SwitchApplication`/`input.Application`/`history.Application`）9 | 21 | 21 | `bridge.go:1111,1113`「未装配角色设置面」探测 |
| S3 | View 快照与事件（`session/viewstate`）4 + 会话生命周期与目录（`application/session` + `session/query.Application`）11 | 15 | 19 | `:484,:489,:490,:890,:894` sessionAware/replayAware 探测 |
| S4 | 会话提交与排队（`input.SubmissionApplication`/`session.SubmitApplication`/`InteractionApplication`/`lifecycle.Application`/`conversation.QueueApplication`/`conversation/edit.EditApplication`）11 | 11 | 13 | `:766,:768`「未装配消息编辑分叉面」、`:790,:801,:812,:823,:837,:846,:870,:880` 共 8 处、`shutdown.go:78,:106` sessionActivity |
| S5 | 看板与 teamwork（`execution.WorkItemApplication`/`conversation.UnifiedApplication`/`contract.RoleSessionsPort`）9 + subagent 单元（`execution.SubagentApplication`）2 + 读面（`observability`/`prompt`/`reference`/`ports.Execution.ToolRecords`）6 | 17 | 17 | `:1517,:1519`「未装配角色会话读面」、`:1745,:1756,:1758`「工具记录未装配」；死方法 `ToolRecordEnabled()`（`:1736` 声明、零调用）直接删除不迁移 |
| S6 | workspace 文件与 git（`application/workspace.Application` + `workspace/switch.UseCase`）13 + 项目环境与会话 SSH（`workspace.Application`/`session.SSHApplication`）7 | 20 | 20 | `:1143,:1145`「未装配会话 SSH 面」 |
| S7 | scheduler（`application/admin/scheduler.SchedulerApplication`）3 | 3 | 3 | 收尾：删 `Bridge.app` 字段与 `closeCoordinator.app` 字段、删宽接口 `gui/bridge.go:24`、`frontend_gui.go:28` 改传 `gui.Dependencies`（不再经 `wiringroot.NewFrontend`） |

**M3 终态判据（缺一即未结束）**：

1. `grep -c "func " gui/bridge.go` 内宽接口 `Application` 声明 = 0 处；`gui` 包内 `bridge\.app\.\|coordinator\.app\.` = **0**；
2. 8 处编译期断言（`bridge.go:251-257,:864`）全删，结构断言由 `frontend_gui.go` 装配表逐字段绑定承担；
3. ≈20 处可选面探测与 12 条「未装配 / not supported / not assembled」错误面全消失，能力有无只由装配表说话；
4. `wiring/bootstrap/frontend_gui.go:28` 不再引用 `wiringroot.NewFrontend`；`gui/*.go` 仍保持 `applicationcompat`/`wiring.` 引用 = 0；
5. 每个端口方法名逐字 = owner 方法名（≈24 处改名吸收在 Bridge 方法层），Wails 暴露给 JS 的 Go 方法名与 `/rpc` 线名零改动；
6. 每片：`gofmt -l` 空、`go build -tags "gui,desktop,production" ./...` exit=0、`go test ./gui/... ./wiring/...` exit=0、`^func Test` 数不降。

### 2.7 门面专用测试与夹具分类（只读子代理实测 + 我逐条复核）

**门禁自身的盲区（须修，且属于「红灯先行」范畴）**：`TestAssemblyHasNoTestOnlyForwardingFixtures`（`application/compat_retirement_test.go:140`）四条判据为 `:152` 文件名含 `legacy`、`:153` 含 `test_alias`、`:154` == `frontend_migration_test.go`、`:164-165` 声明**带接收者**的方法 `frontendOfTestService`。第 4 条命中 **0 个文件** —— 真正的夹具是 `application/assembly/service_helpers_test.go:23` 的**自由函数**，而它是包内使用最广的转发生存根（**150** 个调用文件，实测）。即：门禁报出的 25 = 9 `legacy` + 15 `test_alias` + 1 `frontend_migration`，全部来自前 3 条，最危险的那一个它看不见。

**25 个夹具分类**（断言记号 `t.Fatal*/Error*/Run/Cleanup/testing.T` 全量计数 = **0**，即夹具自身不含任何断言）：**24 FORWARDING-ONLY + 1 MIXED**。

唯一 MIXED：`application/assembly/session_history_test_aliases_test.go`。它不是转发，是**同一判据的第二份弱实现**（AGENTS.md §8 规则 3 + §10.3「仿真实生产假象」红线），我已逐行复核：

| 判据 | 夹具（assembly 测试） | owner（生产唯一实现） | 夹具缺失的语义 |
|---|---|---|---|
| 可见性 | `:27 isVisibleHistoryMessage(msg)` = `role != "system"`，1 参 | `application/session/history/history_runtime.go:520`，2 参：system 隐藏 / 非 user 可见 / **user 且 `display(content)==""` 隐藏** | user 空投影行仍判可见 |
| 消息投影 | `:12/:19`（2 参）：仅 assistant\|tool 剥思考块 + 复制 Role/Content/ReasoningContent | `history_runtime.go:501`（3 参）：user 走 `display`、`IsProviderOnlyHistoryContent` → 置空、`ToolCalls` → `message.Tool` 投影 | display 还原、provider-only 遮蔽、工具调用投影 |
| 一行转发 | `:8 pointViewAtRestoringShell` | `components.sessionHistoryRuntime.PointViewAtRestoringShell` | — （纯转发，直接重定向调用方后删除） |

owner 侧覆盖缺口（删除夹具前必须先补，红测先行）：`application/session/history/runtime_test.go:10-23` 只测了 `strings.TrimSpace` + system 隐藏，**未**覆盖 skill 信封还原、provider-only 置空、ToolCalls 投影、user 空投影不可见；而 `application/assembly/skill_context_test.go:41`、`:50` 恰好是拿夹具的简化判据在断言。→ 已派写代理做「同一输入跑两遍」的一致性红证 + 把场景搬到 owner + 重定向 assembly 用例 + 删夹具副本。

**删除成本实测（非夹具测试文件对转发符号的引用数）**：`frontendOfTestService` **150**、`service.Snapshot()` 97、`service.Submit(` 57、`SubmitToSession` 16、`SubscribeSession` 12、`appendDelta` 6。另：`frontend_migration_test.go` 的 128 个带接收者方法里 **126 个**与 `application/compat/frontend.go` 的 192 个方法**同名**；`assembly.Frontend`（`service.go:67`）是**零生产方法**的生产类型，其方法唯一来源就是这个测试文件 —— 测试构建面严格大于发布面。

**门面引用测试分类**（模式：`applicationcompat` / `compat.New(` / `application.Frontend` / `assembly.Frontend` / `FrontendPorts` / `wiring.NewFrontend`；11 文件 / 25 个测试函数）：FORWARDING-ONLY **4**（`application/compat/frontend_test.go:11,:52` —— 后者断言方法体只有一条语句，本身就是转发测试；`application/query_faces_test.go:11,:27`）、BEHAVIOUR **10**（`query_faces_test.go:34`、`test/project_board_i7_test.go:110,151`、`test/tool_full_chain_test.go:59,133`、`test/teamwork_headless_smoke_test.go:683`、`test/repro_hot_attach_background_tool_order_test.go:280`、`test/session_chain_bench_test.go:47`、`gui/tool_full_chain_test.go:261,319`，断言密度 13–46/文件，只是**经门面构造**，禁止删除，须重指向 `wiring` 注入的 owner 窄端口）、结构性门禁 **11**（`compat_retirement_test.go:18,140`、`contract/runtime_migration_test.go:72,91,110`、`e2e/facade_boundary_test.go:415,441,464,490,507,559`）。`FrontendPorts` 现仅存在于门禁的禁用名列表中，已退场。

**对 M4 的含义**：D5「删除测试专用转发夹具」的真实体量由 150 个调用文件决定，不是一次提交能完成的；须按 owner 分组切成多批、每批一组互斥文件集并行重定向，且第 4 条判据要先修好，否则重定向完仍有一个夹具对门禁隐形。

### 2.8 重新激活后的基线与车道（2026-10-27 第二轮）

**B 臂实测基线（本次开工时，命令落盘 `/g/tmp/audit-budget.log`）**

| 指标 | 实测 | 命令 |
|---|---|---|
| `go build ./...` | exit=0，日志 0 行 | `go build ./... > /g/tmp/goal-build.log` |
| assembly 生产行数 | **7101**，预算 7092（红，缺 9 行） | `go test ./application -run TestApplicationArchitectureBudget` exit=1 |
| 宽接口方法数 | **55**（此前记 56 系计数口径错，以本条为准） | `awk '/^type Application interface \{/,/^\}/' gui/bridge.go \| grep -c $'\t[A-Z]'` |
| 非测试 `.app.` 引用 | **96** = bridge 88 / shutdown 7 / terminal_bridge 1 | `grep -ho "\.app\.[A-Za-z]*" $(ls gui/*.go \| grep -v _test.go) \| wc -l` |
| 测试文件 `.app.` 引用 | **0** | 同上对 `gui/*_test.go` |
| `(*application.Frontend)(nil)` 断言 | **8** | `grep -c "= (\*application.Frontend)(nil)" gui/bridge.go` |
| gui 测试用例数 | **93** | `grep -c "^func Test" gui/*_test.go` 求和 |
| `frontendOfTestService` 调用文件 | **151** | `grep -rl "frontendOfTestService" application/assembly \| wc -l` |

**车道划分（文件互斥，`gui/bridge.go` 单车道串行）**

| 车道 | 范围 | 对应剩余项 |
|---|---|---|
| A 挤行数 | `application/assembly/*.go` 非测试，排除 `service.go` 与任何 `Frontend` 符号 | §5.4（7101→≤7092） |
| B 门禁 | `application/compat_retirement_test.go` + 两份 2026-10-25 历史文档 | §5.5 + 第 3 项盲区 |
| C 判据去重 | `application/session/history/*`、`session_history_test_aliases_test.go`、`skill_context_test.go` | 第 4 项 |
| D M3 | `gui/**`、`gui/dependencies.go`、`wiring/bootstrap/frontend_gui.go` | 第 5 项 S1–S4（S5–S7 待 S1–S4 冻结验收后开） |

**已核事实**：`application/assembly/components.go:37` 的 `Components` 已含 S1–S7 所需全部 owner 指针
（`Admin.Account`、`Policy.Switch/Role/RoleSessions`、`Conversation.*`、`Session.*`、`Execution.*`、
`Workspace.Projects/Switch`、`Runtime.Scheduler/Lifecycle`），因此 M3 各片都能绑真实 owner，不需要绑门面充数。

**本轮开工时仍未落盘**：`gui/dependencies.go` 不存在、`session_history_test_aliases_test.go` 仍存在、
`frontendOfTestService` 仍在 `service_helpers_test.go:26`（上表 151 个调用文件）、预算仍 7101。

## 3. 首轮红测记录（M0 交付证据）

```text
$ go test ./application -run 'TestApplicationCompatibilitySurfaceIsRetired|TestAssemblyHasNoTestOnlyForwardingFixtures' -count=1
FAIL ... application/compat still exists; migrate callers to their owning components
FAIL ... assembly still has test-only compatibility forwarding fixtures:
  frontend_migration_test.go, *_legacy_test.go, *_test_aliases*.go, frontendOfTestService
```

B 首次 build（M1 中途）失败样本：

```text
wiring/registry/registry.go:64:157: cannot use components.Reference.ReadToolResultHandler
  (value of type func(argsJSON string) (string, error)) as func("context".Context, string) (string, error)
（同型错误共 4 处：read_tool_result / read_plan / read_compressed_turn / search_history）
```

### 3.1 本轮自造红灯与修复（contract 门禁字面量重推）

首轮全仓跑出 1 个**我引入的回归**：`contract.TestBM005ReviewFilesRegistrationIsOwnedByWiring`
在 `runtime_migration_test.go:128` 断言字面量
`review.RegisterShowFilesTool(runtime, applicationcompat.New(app))`，而我 M1 把注册面改成
`appassembly.ComponentsFor(app).Admin.Review` 后该字面量消失 → 门禁红。

处置顺序（按 AGENTS.md §8 规则 7「红灯先行」与 §9 规则 5，不改测试迁就实现，而是重推门禁钉的对象）：

| 步骤 | 内容 | 结果 |
|---|---|---|
| 1 | 判明该用例锁的是「注册归 wiring、端口来自 owner 组件」，**不是**「端口来自兼容门面」 | compat 已登记 D1 退场，钉门面字面量等于把兼容层写进长期门禁 |
| 2 | 字面量重推到 owner 形状 `review.RegisterShowFilesTool(runtime, appassembly.ComponentsFor(app).Admin.Review)` | 保留原意图，钉子换到正确对象 |
| 3 | 追加禁止断言：`if strings.Contains(registry, "applicationcompat.New(app)") → t.Error` | 门禁从「要求门面」升级为「禁止门面回流」，方向与 D1 一致 |
| 4 | 其余两条断言（`domain` 必须 own `func RegisterShowFilesTool(`；compat 内部仍转发给 port）逐字未动 | 不扩大变更面 |
| 5 | 反空转证明：**不在 A 臂跑用例**（A 含旧断言，跑绿会误判「断言不咬」），改用字面量计数 | 见下表 |

| 字面量计数（`grep -c -F`） | A（`39e5afe0`） | B（本轮） | 读法 |
|---|---|---|---|
| `review.RegisterShowFilesTool(runtime, appassembly.ComponentsFor(app).Admin.Review)` | 0 | 1 | 新断言在 A 源码上**必红** → 非空转 |
| `applicationcompat.New(app)`（`wiring/registry/registry.go`） | 6 | 0 | 新禁止断言在 B 上有可验对象 |

修复后：`go test ./contract ./wiring/... -count=1` → exit=0。

## 4. 验证记录

| 命令 | 结果 |
|---|---|
| `go build ./...`（B） | 通过（exit=0） |
| `gofmt -l`（B，本轮改动文件） | 无命中 |
| `go vet ./application/... ./wiring/... ./gui ./tui ./headless ./console`（B） | 通过（exit=0） |
| `python scripts/gen_core_readme_index.py --check --only application/assembly wiring/registry application/compat` | 通过：检查 21 个 README，无漂移 |
| A 臂基线 `scripts/application-compat-ab.ps1` | exit=0，44.99s，全绿 |
| B 臂候选 `go test ./application/... ./gui ./tui ./headless ./console ./wiring/... -count=1` | exit=1；42 个包 `ok`，唯一失败包 = `github.com/RedHuang-0622/seelex/application`，失败用例恰为 3 个结构门禁：`TestApplicationArchitectureBudget`、`TestApplicationCompatibilitySurfaceIsRetired`、`TestAssemblyHasNoTestOnlyForwardingFixtures`。`application/assembly`（43.471s）、`gui`、`tui`、`console`、`wiring/adapters` 全部 `ok` —— 无行为回归 |
| M2：`go build ./...`（B） | 通过（exit=0） |
| M2：`go vet ./tui`（B） | 通过（exit=0） |
| M2：`go vet -tags "gui,desktop,production" ./wiring/bootstrap`（B） | 通过（exit=0）—— gui 构建标签下的 `startTUI` stub 与新签名一致 |
| M2：`go test ./tui -count=1` | B `ok 3.768s`；A（基线同命令）`ok 4.082s` —— 双臂全绿 |
| M2：`grep -c "^func Test" tui/*_test.go` | A = 36，B = 36 —— 一个用例都没丢（删掉的只有死夹具 `teamFakeApp`/`teamViewFixture`，不含用例） |
| M2：`gofmt -l`（B） | 初报 4 个文件漂移（本轮新建/改写文件），已格式化后复检无命中 |
| M2+：`go build ./...`（B，headless+console 迁移后） | 通过（exit=0） |
| M2+：`go test ./contract ./wiring/... -count=1`（B） | 通过（exit=0）—— §3.1 回归已闭合 |
| M2+：`python scripts/gen_core_readme_index.py --check --only headless console` | 无漂移（exit=0） |
| M5 首轮：`go build ./...`（B 全仓） | exit=0 |
| M5 首轮：`go vet ./...`（B 全仓） | exit=0 |
| M5 首轮：`go test ./... -count=1 -timeout=300s`（B 全仓，日志 `/g/tmp/m2-final.log` 189 行） | **exit=1**，失败包恰 2 个：`application`（= 3 个故意红测门禁：`TestApplicationArchitectureBudget` 7123>7092、`TestApplicationCompatibilitySurfaceIsRetired`、`TestAssemblyHasNoTestOnlyForwardingFixtures` 列 25 个转发夹具文件）与 `backend/engine/account`（1 个用例）；其余全部 `ok`，含 `application/assembly`、`gui`、`tui`、`headless`、`console`、`contract`、`wiring/adapters`、`test` |
| 异常隔离复测：`go test ./backend/engine/account -run TestSlowHealthyStreamSurvivesIdleWatchdog -count=1` ×3 | 3/3 `ok`（6.07s / 5.02s / 5.93s，exit=0）。该目录本轮零改动（`git status --porcelain` 无 `backend/`）；失败样本为 300ms 空闲看门狗在全仓并行负载下超时 → 判为时钟抖动，**不记作迁移回归**，M5 终验需单列复跑 |
| compat 剩余真实 importer（B，`grep -l 'application/compat"' --include=*.go`） | 5 个文件：`application/application.go:9`（+`:21` alias）、`application/compat_retirement_test.go:26`（门禁字符串常量）、`application/query_faces_test.go:8,29`、`e2e/facade_boundary_test.go:172`（字符串清单）、`wiring/constructors.go:13,49` |
| `applicationcompat.New(` 调用点（B） | 3 处：生产 1（`wiring/constructors.go:49`）+ 门面专属测试 1（`query_faces_test.go:29`）+ 门禁禁止字面量 1（`contract/runtime_migration_test.go:133`）。A 同口径：`wiring/registry/registry.go` 6 处 + 上述 |
| `wiring.NewFrontend(` 调用点（B） | 5 处：生产 1（`wiring/bootstrap/frontend_gui.go:28`，M3 目标）+ 测试夹具 4（`e2e/scenario/harness.go:47`、`gui/tool_full_chain_test.go:256`、`test/tool_full_chain_test.go:309`、`test/repro_hot_attach_background_tool_order_test.go:110`，M4） |
| compat 方法名集合 A/B 差（AST `grep -oE '^func \(frontend \*Frontend\) …'` 排序 comm） | A=193、B=192；只在 A = `PublishRuntimeProjections`；只在 B = **0** → 迁移过程未向门面新增任何方法（守恒证据） |

### compat 引用计数 A/B（M1 前后的验收口径）

计数命令 = §验收四条 grep 的同一正则，A 臂 = `39e5afe0`，B 臂 = 本轮候选。

| 指标（正则） | A | B | Δ | 说明 |
|---|---|---|---|---|
| `FrontendPorts`（Go 面） | 16 | 1 | −15 | 余下 1 处 = `application/compat_retirement_test.go:61` 门禁自身的标识符字面量；**生产引用 = 0** |
| `frontendOfTestService` | 862 | 864 | +2 | +2 全部来自 M0 红测的 AST 扫描字符串（`compat_retirement_test.go:165-166`）；转发夹具实际引用 = 862 → 862（**净零**，删除属 D3/D4，即 M4） |
| `^func (frontend *Frontend)`（全仓，含 `assembly/frontend_migration_test.go` 夹具） | 321 | 320 | −1 | 唯一消失的是 `application/compat/frontend.go:802 PublishRuntimeProjections` → 改为 `assembly.PublishInitialRuntimeProjections(service)`（`runtime_projection.go:76`）；`assembly/frontend_migration_test.go:352` 的同名方法仍在（M4 删除对象）。单看 `application/compat/frontend.go` = 193 → 192（网关判定用的就是这个口径） |
| `application/compat` 引用（*.go） | 20 | 15 | −5 | |
| `application/compat` 引用（*.md） | 51 | 49 | −2 | |
| compat 文件数 | 3 | 3 | 0 | 未动（M4） |
| compat 行数 | 1167 | 1160 | −7 | |

结论：**两个"没降"的计数都是门禁自己引用的字符串，不是新增债务**。逐文件 diff 证明 B 相对 A 在 `application` 内没有新增任何 compat 或转发夹具引用（`grep -c` 的 per-file diff 只有 `compat_retirement_test.go` 一行差异）。

### assembly 生产行数算术（预算冲突的成因）

| 文件 | A | B | Δ |
|---|---|---|---|
| `frontend_ports.go` | 159 | 0 | −159 |
| `components.go` | 0 | 198 | +198 |
| `runtime_projection.go` | 69 | 78 | +9 |
| `service_components.go` | 201 | 202 | +1 |
| `service_scheduler.go` | 83 | 75 | −8（本轮死代码） |
| `skill_context.go` | 85 | 75 | −10（本轮死代码） |
| 合计（54 个生产文件） | **7092** | **7123** | **+31** |

预算 `assembly_production_line_limit` = 7092，与 A 的实际行数逐字相等，即 ratchet 无余量。
`+39` 中 `components.go` 比 `frontend_ports.go` 大 39 行的全部来源是 7 个分组 struct 的声明与空行
（7 个 `type …Components struct {` + 7 个 `}` + 6 个分隔空行 ≈ 20 行）与其 doc 注释 —— plan M1
退出条件「assembly 只暴露分组组件数据」本身要求这个形状，不是可省的包装。

### 网关 vs 兼容层判定（2026-10-27 用户裁决：「是 frontend 网关层可以保留，兼容层就算了」）

判据不靠读代码印象，用 AST 分类器逐方法测量（`go/ast`：`FuncDecl` receiver = `*Frontend`，
剥离 `if frontend == nil/!= nil` 守卫后看剩余语句数与形状；同时统计体内**非 receiver 根**的调用、
守卫外分支、赋值）。工具在仓库外（`compatclass`，一次性分析件，按 §2 规范不落仓库根），
双臂各跑一次即可复现下列数字。

| 测量项 | 数值 | 读法 |
|---|---|---|
| `*Frontend` 方法数（B：`application/compat/frontend.go`） | 192（A：193） | −1 = M1 已删的 `PublishRuntimeProjections` |
| 方法体总行 | 709 / 192 | 平均每方法 3.7 行 |
| 单条转发调用（`return owner.M(…)` 或裸调用） | 147（`forward_call_raw`）+ 2（带守卫） = **149** | 出口 = 一个 owner 的一个方法 |
| 单条 owner 取用（`return frontend.ports.<组>.<字段>`） | 43（`resolve_owner_guard`） | 访问器：把分组字段取出来，判空指针 |
| **带自有行为的方法（`OWN_BEHAVIOUR`）** | **0** | 没有任何方法体不是"一条转发/一条取用" |
| 跨多个分组的一次调用（多 owner 编排） | **0** | 网关的核心特征（跨域编排）一个都没有 |
| 体内赋值语句 | **0** | 不持有、不计算、不缓存任何状态 |
| 守卫外的 if/for/switch/defer/go | **0** | 45 处 if 全是 `frontend == nil` / `!= nil` 指针守卫 |
| 体内调用非 receiver 根的外部函数 | **2** | `SetAccountWindow`、`SelectAccount` 用 `context.Background()` **替换**调用方 ctx（共 10 个 forwarder 声明 `_ context.Context`）；这是丢取消信号的兼容副作用，不是编排 |

配套三件的形状同样不含行为：`application.go:21` 是 `Frontend = applicationcompat.Frontend` 一行 **type alias**；
`assembly/service.go:67` 是 `type Frontend struct{ *Service }` —— 0 个自有方法，纯嵌入；
`wiring/constructors.go:48 NewFrontend` 是 `return applicationcompat.New(app)` 一行；
`compat.New` 只做 `&Frontend{Frontend: &assembly.Frontend{Service: service}, ports: ComponentsFor(service)}` 的字段赋值。
外部没有一处对门面指针判空（`gui/`、`headless/`、`console/`、`wiring/` 非测试代码 grep = 0），
所以 45 处守卫在生产里永不触发。

**判定**：`application/compat` 是**兼容层**（alias → 嵌入 → 转发 → 构造，四段全无可拥有行为），
按裁决删除，D1/D2/D4 清单不变。保留的"网关形"制品是这些**真有归属**的面：

| 保留 | 为什么算网关/owner 而不算兼容层 |
|---|---|
| `application/assembly.Service` | 状态、锁、生命周期顺序的唯一拥有者；`PublishInitialRuntimeProjections`、`CloseRoleUnit` 这类判据住在这里 |
| `application/assembly.Components` | 只读装配快照，0 个 func 字段（M1 已按秩序 3 落地） |
| 各 consumer 的 `Dependencies`（如 `tui/dependencies.go`） | 由真实调用点推导的窄端口集合，是 consumer 自己的请求形状，不是第三份实现 |
| `wiring/bootstrap` 装配表（`tuiDependencies` 等） | 秩序 5 指定的构造/装配/关闭唯一位置；绑定语句即编译期结构断言 |

结构后果（已裁决，见 §5.2）：门面消失后，那 10 个 `_ context.Context` forwarder 与 2 处
`context.Background()` 顶替一起退场，consumer 端口签名逐字回到 owner。实测纠正：ctx 的丢弃
**不发生在兼容层** —— owner `application/admin/application.go:113,140` 自己声明 `_ context.Context`
忽略，底层 `Runtime` 两个方法根本不收 ctx，所以那 2 处 `Background()` 是给注定被丢弃的形参换值，
不构成门面的真实行为，也不构成 D1 删除的行为风险；端到端接通取消信号另立新议题。

## 5. 用户裁决记录（2026-10-27）

四条待决已全部裁决，结论与执行义务如下。

1. **`toolrecord` 的 nil 判据归一 → 裁决「删 11 处，只留 owner 一处」**。
   实测证据：`service.toolRecords` 全仓（非测试）**只有 1 处赋值且无条件**
   （`application/assembly/service_assembler.go:173`）；`toolrecord.New(deps)` 只存闭包、
   不同步调用任何 Deps（`application/execution/toolrecord/application.go:70-79`）；owner
   `CloseRoleUnit` 已自带 `application == nil` 守卫（`:278-281`）。因此散在
   `service_assembler{,_applications,_conversation_turn,_scheduler_lifecycle,_session}.go` 的
   **11 处** `if service.toolRecords != nil` 实测永不触发，且违反 AGENTS.md §8 规则 3
   （同一判据只出现在一处）。执行：删这 11 处判空，判据只留 owner 一处，单独一个 commit；
   风险登记：若将来出现绕过 assembler 构造 `Service` 的测试，由 owner 守卫兜住而非各处判空。
   附带收获：这 11 处即 §5.4「先挤 assembly」的第一笔行款。
2. **`SelectAccount`/`SetAccountWindow` 的 ctx → 裁决「端口带 ctx，随门面消失」**。
   实测纠正了议题前提：丢弃**不发生在兼容层**。owner 层自己就把形参声明为
   `_ context.Context` 忽略（`application/admin/application.go:113,140`），底层
   `Runtime.SelectAccount(name)` / `SetAccountWindow(name, window)` 根本不收 ctx
   （`backend/engine/assemble/runtime_account.go:192,240`、`contract/ports.go:120,582`、
   `backend/limit/accountwindow/setter.go:55`）。compat 的 `context.Background()`
   （`application/compat/frontend.go:835,845`）只是给一个**注定被丢弃**的形参换了个值。
   执行：M3 GUI 端口签名逐字沿用 owner（含 ctx），D1 删除门面时那 2 处 `Background()`
   自动消失，行为零变更、零跨层改动；不做「接通」。
   **新记议题（不属本工作包）**：账号切换/窗口设置端到端拿不到取消信号，根因在
   owner 与 `Runtime` 的契约形状，需按 AGENTS.md §9 外部依赖批次另立一批
   （改 4 层签名：`contract/ports.go` → `backend/engine/assemble` → `application/admin` → GUI 端口）。

3. **GUI 十组拆分的粒度 → 裁决「沿用 plan 口径」**：State/Conversation/Session/Accounts/
   Policy/Workspace/Roles/Execution/Tools/Lifecycle 十组不变，M3 直接按它切 `Dependencies`。
4. **`assembly_production_line_limit` 超预算 31 行 → 裁决「先挤 assembly，再谈基线」**：
   先做一次**有界扫描**（只做 assembly 内死代码 + 同一判据多份实现的合并，不新增抽象层），
   实测报出能删多少行；仍不足的部分再携证据申请抬基线。实测证据 —— 这笔债**不是过渡性的**，
   删掉 compat 也还不回来：
   - 预算 7092 与 A 的实际行数逐字相等，ratchet 零余量；
   - `Components` 47 个字段里只有 **8** 个（`Completion/DraftEntry/EditFork/Perf/Projects/Scheduler/Submission/Unified`）是 compat 独占，
     且这 8 个全部仍被**有生产调用方**的 compat 出口读取（调用方分布在 `gui/bridge.go`、`tui`、`headless`、`console`、`wiring/adapters`、`application/{input,workspace,session,conversation}`）—— 现在一个都删不掉；
   - 即使 M2/M3 把前端迁完、M4 删掉 compat，`assembly` 最多回收 8×2 = 16 行 → 终态 ≈ 7107，**仍超 7092 约 15 行**；
   - 已诚实付掉的：删除 assembly 死代码 −18 行（7141 → 7123），prune 无消费字段 = 0（0 个），删除 assembly 内只转发方法 = 0（不存在）。
   - **§5.1 是本条的第一笔进账**：11 处死守卫随裁决删除后重算行数，再决定是否需要申请基线。
5. **验收门禁的 `--glob '*.md'` 范围 → 裁决「加过期标记 + 白名单豁免」**：历史工作包
   `docs/2026-10-25-application-modularization/{implementation,remaining-work}.md` 顶部统一加
   「历史记录，不描述当前实现」标记；门禁仍扫全部 `*.md`，但**只豁免带该标记的文件**，
   且豁免条件由门禁脚本校验（无标记仍红）。这样既保住 AGENTS.md §1 的历史/现状区分与
   按标识符检索能力，又不给兼容面以「文档」形式回流的空间。

   按裁决排序后的候选与否决记录：**(i) 重定基线**（7092→7107，M4 后按实测再收紧，并按
   AGENTS.md §10.2 规则 7 在兼容台账登记旧面/新面/首调用方/删除里程碑/归零命令/回退方式）
   —— 因「先挤 assembly」尚未定数，暂不启用，但它是扫描后的兜底路径；
   **(ii) 改顺序**（先 M2/M3/M4 再落分组快照）—— 否决：单批规模远超 plan 分批纪律，且 M2/M3
   需要一个还不存在的迁移面；**(iii) 认红到 M4** —— 否决：ratchet 在中间三个里程碑失去保护，
   也违背 M1「A 行为测试仍绿」的「仍」字。
   纪律不变：在「挤行实测 + 申请」获批前不动 `application/testdata/application_budget.json`
   —— 这条线是「预算只允许下调」，抬高它必须由用户裁定。

## 6. 未触碰

A 工作树、用户数据（`.seelex`、`config/*.yaml`、`*.local.yaml`、`dist`）、`store/`、会话运行时代码、main 分支的 backend-middle 模块化未提交改动。
