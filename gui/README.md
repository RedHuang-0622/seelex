# GUI Backend

## 模块定位

`gui` 是 Wails/WebView 桌面适配器。它复用 `application.Service`，负责 Go/JavaScript 边界、事件转发、目录选择和关闭协调，不拥有聊天、session、project 或 Plan 业务状态。

## 文件结构

| 文件 | 职责 |
|---|---|
| `bridge.go` | Wails 暴露方法、Application interface 和事件 relay。 |
| `assets.go` | `//go:embed frontend/dist`。 |
| `run_wails.go` / `run_stub.go` | build tags 下的真实 GUI 与不可用 stub。 |
| `dialogs_gui.go` / `dialogs_stub.go` | 平台目录选择适配。 |
| `shutdown.go` | 等待任一会话（含后台）运行完成的 graceful close；超时取消全部 running sid 并等待收尾。 |
| `fork_live_probe_test.go` | 真实 API headless fork 探针（env 门控，opt-in；顶层 Submit → fork_subagents 链路 1/10/100 并发对照）。 |
| `role_live_probe_test.go` | R2/R4 真实 API headless 冒烟（env 门控）：真实 Submit 物化主会话 → `role.*` 建 TL/写 draft/sync/floor/角色 wire → `schedule.*` → goroutine/mutex/block pprof 现场。 |
| `headless_team.go` | AgentTeam 角色管理 RPC（`team.*`）：preset 清单、装配、成员表、角色配置 CRUD、工作顺序设置。 |
| `team_live_probe_test.go` | AgentTeam 工厂真实 API headless 冒烟（env 门控）：真实 Submit 物化主会话 → `team.materialize`（goal/review preset）→ 角色 CRUD + `order_roles` → 设计稿不变量核对 → pprof 现场。 |
| `seelebridge/custom_role_live_probe_test.go` | provider role 能力实验（env 门控）：把非标准逻辑角色 `tl` 放入真实请求历史，确认 provider 是否接受自定义 role 名。 |
| [`frontend/`](frontend/README.md) | 原生 HTML/CSS/ES modules 前端。 |

## Bridge 契约

`Application` 是 GUI 需要的最小端口。`Bridge` 暴露 Snapshot、Submit、BeginNewSession、`SaveComposerDraft`（草稿未发送输入防抖落盘，跨重启恢复）、ResumeSession、`ForkSessionLatest`（从会话最新完整轮次分支出新会话并切换，返回子会话 ID）、Cancel、Interaction、Plugin/Account/Effort/Full Access、history pagination、workspace、session storage settings、`UpdateWorkItemStatus`（工作表格 todo 三态）等方法，并把 Application Event 统一转发为 `seelex:event`。`BeginNewSession` 只进入 Application draft（早分配真实 SID，不建引擎 bundle），GUI 不通过 `/new` 字符串命令抢先创建 Session。

会改变会话目录的命令（`BeginNewSession`/`DeleteSession`/`ArchiveSession`/`ForkSessionLatest`/`SetSessionMeta`）在返回前调用 `settleCatalog()`：等 `Application.WaitCatalogRefresh` 的目录刷新回执（预算 `sessionCatalogSettleTimeout`），使 renderer 紧接着重拉的 `Snapshot()` 已携带权威列表。等待超时不算失败也不向上报错——命令本身已成功，未收敛的列表由 worker 发布的 `snapshot.changed` 补齐，前端不得为此回填旧列表。

`ArchiveSession` 的目录收敛按目标会话所在项目做范围刷新：归档行从该项目格子
过滤（record 状态 archived 是唯一标记，不存在全局数组上的归档位），其它项目
列表与数据五片不受影响。

会话级冷读宿主面（C1）：`Bridge.ListSessions()` 返回权威会话目录（与快照目录
同源）；`Bridge.SnapshotOf(sessionID)` 对未驻留会话返回 `Resident=false` 的
record 只读基线；`Bridge.GetSessionTranscript(sessionID, fromSeq, toSeq)` 按
Seq 区间读事件日志（`(0,0)` = 全量）。三者均不要求目标会话加载引擎。

子代理详情新鲜度（G7）：`seelex:subagent_live` 除 stage/tool 外新增
`assistant` 正文增量事件（节点 Session `ChatStream` 的流式文本分片）；前端
详情弹窗会话记录由该事件驱动增量渲染，`nodeDetailPollTimer` 2s 轮询已删除
（`TestEmbeddedFrontendExists` 的禁轮询断言相应启用）。

relay 只订阅一次 `SubscribeSession("")`（跟随当前视图会话）：会话归属由
application 在事件投递端判定，Bridge 不保存 `currentSessionID` 副本，渲染层收不到
别会话的事件，因此切换/新建/分支都只是应用层命令，不需要重建订阅。宿主应用不支持
会话级订阅时退回全局订阅（此时应用自身也没有多会话状态可污染）。

事件投递回执（C4）：`EventEmitter` 没有返回值，Go→WebView 这条腿丢了什么 Bridge
无从得知。因此 relay 优先申请**带重放窗口的订阅**（可选端口
`SubscribeSessionWithReplay`），渲染层每应用一批事件回报水位（`Bridge.AckEvents`，
150ms 合并），并可在 `delivery_seq` 缺口处主动 `Bridge.ReplayEvents` 增量补取。
水位落后时 `catchUpRenderer` 把窗口内未确认的事件重推（重复对渲染层是幂等的，
按 `delivery_seq` 去重），重推封顶 `eventResendMaxTries` 次；窗口已淘汰则改投一条带
当前水位的 `resync.required`，让渲染层整份重拉并把水位抬到该处。运行期因此不再
需要"每秒轮询 Snapshot"的兜底对账。

子代理 `subagent.changed`、`subagent.tool.started`、`subagent.tool.completed` 与其他 Application Event 使用同一 relay，不在 Bridge 内改写 payload。`tool_full_chain_test.go` 从 `Bridge.Submit` 进入，覆盖 ToolHookBridge → Application/EventHub → Bridge emitter → `seelex:event`，并验证 `Bridge.SetFullAccess(true)` 会释放既有审批、投影 Snapshot、relay `runtime.changed`。

工作表格增量 `worktable.changed` 使用同一 relay（payload 只带表格投影，不整份
runtime）；`Bridge.UpdateWorkItemStatus(id, status)` 只做参数透传，业务校验在
application 层（v1 仅支持 `todo:<index>` 的 pending/doing/done）。

启动配置容错：`Options.StartupWarning` 非空时，窗口就绪（`OnDomReady`）后
弹出原生错误对话框展示启动期配置警告（如 `accounts.yaml` 解析失败）。警告同时
以系统通知进入会话可见区（`application.Service.AddNotice`），应用照常启动，
不闪退。

task 体系增量 `task.changed`（逐任务状态/打点/retry）同样经 relay；主动
`taskadd` 是模型可调用的 harness 工具（注册表幂等去重），不经 Bridge。

A2A 角色管理面（右侧栏「状态 → Agent Team」子页数据源）：
`Bridge.AgentTeamPresets` / `AgentTeamView` / `AgentTeamMaterialize` /
`AgentTeamPutRole` / `AgentTeamDeleteRole` / `AgentTeamSetOrder`，全部走
`application/contract` 纯 DTO（S27 收口；`agentTeamApplication` 是可选能力接口，
宿主未装配时返回可展示错误而不是空视图）。`sessionID` 传空 = 当前视图会话，
Bridge 不保存 `currentSessionID` 副本。顺序的唯一事实是会话
`lifecycle.order_policy`/`order_roles` 与角色注册表：前端只提交用户改动后的完整
顺序表（上移/下移/摘除/恢复）或单个角色，Bridge 不缓存也不推导第二份顺序；
定时任务 agent 单独分区、永不进入 `order_roles`（设计稿 §7.1）。前端只读渲染 +
动作转发在 `frontend/dist/agent-team-view.js`（纯函数，含单元测试），面板 DOM 挂在
状态子页的 `#team-section`。

Bridge 方法只做参数转换和调用，不维护镜像业务状态。DSN 等敏感配置必须由 backend redaction 后再返回 renderer。

## 关闭语义

Wails `BeforeClose` 首次触发时调用 `BeginGracefulShutdown`，后台等待
`WaitForIdle`。空闲判定是**进程级**的（生产 Application 经可选端口
`AnyChatRunning` 报告）：视图会话空闲而后台会话仍在跑时同样进入 drain，
全部会话与已接受队列完成后再允许窗口退出。重复关闭不得启动多个 waiter。

等待超时（默认 5s）表示工具、审批或 Provider 未收敛：此时调用
`CancelAllChats` 取消**全部**运行中会话（不只视图会话——后台会话同样占用
引擎与持久化通道），随后再等一个短预算让每个 runChat 走正常收尾（逐会话
flush 完成才标记 idle），到点仍未收敛才按最佳努力退出。

## Build tags

- 普通构建：GUI stub，默认 TUI 不依赖 WebView。
- Wails GUI：`-tags "gui,desktop,production"`，启用 Wails runtime。这里的 `production` 是 Wails 构建约定的一部分，只区分真实桌面实现与 stub，不表示 Seelex 已达到 production-ready；项目成熟度以根 README 的 Developer Alpha 声明为准。
- pprof 采样：`-tags pprof`，Go 侧启动 `127.0.0.1:6060`（`SEELEX_PPROF_ADDR` 可覆盖）的 `/debug/pprof/*` 端口。用于排除法对照——GUI 内存大头在 WebView2 渲染进程，Go 侧 pprof 只能确认主进程小，救不了渲染进程。
- 前端文件编译时嵌入，修改 `frontend/dist` 后必须重建二进制。

## 渲染内存治理（根因链）

WebView2（Chromium）渲染进程吃内存的根因是：每条工具输出的全文都进可见会话快照并被 `markdown()` 渲染进 DOM。治理分三层（对应 `config/seelex.yaml` limits 段与前端渲染）：

1. **快照截断（治本）**：`snapshot_tool_output_chars`（默认 8000）——超过该值的工具输出只把预览放进快照 `message.content` / `tool.result`，完整内容归档为 `result_ref`，前端"加载完整输出"经 `Bridge.ToolResultContent`（复用 `read_tool_result` 通道）按需分页读回。provider 侧历史不受此值影响（仍由 `max_tool_result_chars` 约束）。
2. **前端折叠（立竿见影）**：`components.js` 对工具输出渲染预算压到 4KB/40 行，超预算默认折叠成 `<details class="io-collapse">`；`conversation-view.js` 处理 `data-load-ref` 点击按 `result_ref` 异步拉全文。
3. **性能追踪钩子**：`perf-hooks.js`（`window.__seelexPerf`）每 10s 轮询 `Bridge.PerfStats`（无内容指标：快照 JSON 体积/会话字符数/单条最大/截断数/归档体积），与渲染进程侧 DOM 节点数、JS heap、渲染耗时对照，顶部状态区徽标展示；点击徽标在 console 打印最近 60 个样本。对照涨跌即可定位"谁在吃内存"，无需再靠任务管理器。

## Review 指南

- Bridge 是否仍为薄适配器，业务分支应进入 Application。
- 事件订阅 goroutine、Wails context 和关闭 channel 是否可退出。
- renderer 可见数据是否已脱敏。
- build-tag 两套实现是否保持相同导出 API。
- 新 Bridge 方法是否有 fakeApplication contract test。
- 快照截断是否保持"可见会话预览 + 完整内容可读回"两条通道一致（`ToolResultContent` 与 `read_tool_result` 共用 `toolResultContent`）。

## 测试

```text
go test ./gui -count=1
go build -tags "gui,desktop,production" ./...
go build -tags pprof .
node --test gui/frontend/dist/*.test.mjs
```

真实 API fork 并发探针（默认跳过；需要已构建的 headless 目标二进制）：

```powershell
$env:SMOKE_FORK_LIVE='1'
$env:SMOKE_FORK_LIVE_N='10'      # 子代理个数；SMOKE_FORK_LIVE_GOAL 可换题
go test ./gui -run TestRealAPIForkLiveProbe -v -count=1 -timeout 20m
```

真实 API R2/R4 群聊角色探针（默认跳过；目标二进制需包含 `role.*` 接口）：

```powershell
go build -tags pprof -o tmp/headless-smoke/seelex-pprof.exe .
$env:SMOKE_ROLE_LIVE='1'
$env:SMOKE_ROLE_LIVE_PPROF='1'
go test ./gui -run TestRealAPIRoleSessionLiveProbe -v -count=1 -timeout 20m
```

观测点：`role.snapshot` / `role.wire` 返回 `design_warnings` 与
`unassigned_role_rows`；报告落 `tmp/headless-smoke/reports/role-{mutex,block}-*.txt`
及 `fork-live-goroutine-role-live-*.txt`。race 版目标：
`go build -race -tags pprof -o tmp/headless-smoke/seelex-pprof-race.exe .`，
运行时设 `GORACE=halt_on_error=1`。

AgentTeam 角色管理 `team.*` RPC（headless 前门禁；契约测试
[`headless_team_test.go`](headless_team_test.go)）：

| 方法 | 参数 | 语义 |
|---|---|---|
| `team.presets` | — | 列出内置团队实例（`goal-a2a`/`review-team`/`research-team`） |
| `team.materialize` | `main_session_id`、`team_kind` 或 `spec`、`join_seq_id` | 按 preset/自定义 `TeamSpec` 装配：幂等建角色会话 + 写注册表 + 写顺序策略 |
| `team.view` | `main_session_id` | 成员表 + 工作顺序 + 定时分区 + 设计偏差提示 |
| `team.put_role` | `main_session_id`、`role` | 新增/覆盖角色配置（`role_name` 幂等） |
| `team.delete_role` | `main_session_id`、`role_name` | 删除角色配置并同步摘除 `order_roles` |
| `team.set_order` | `main_session_id`、`order_policy`、`order_roles` | 设置工作顺序（定时角色不得入列，未注册角色拒绝） |

真实 API 冒烟（默认跳过）：

```powershell
go build -tags pprof -o tmp/headless-smoke/seelex-pprof-team.exe .
$env:SMOKE_TEAM_LIVE='1'; $env:SMOKE_TEAM_LIVE_PPROF='1'
go test ./gui -run TestRealAPIAgentTeamLiveProbe -v -count=1 -timeout 20m
```

报告落 `tmp/headless-smoke/reports/team-live-*.json`，并抓 goroutine/mutex/block pprof。

权威设计文档位于 [`docs/gui`](../docs/gui/README.md)。

## 事件与关闭生命周期

桌面宿主在启动时调用 `Bridge.Start`，在关闭时调用 `Bridge.Stop`；前者先发送 `seelex:ready` 快照，再将 Application Event 原样转发为 `seelex:event`。关闭运行中的会话会先进入 graceful drain，最长等待 5 秒；超时表示工具、审批或 Provider 未收敛，关闭协调器会取消全部运行中会话并等待取消收尾后退出，避免窗口无限处于拒绝关闭状态、也避免在后台会话数据落盘前强行退出。
