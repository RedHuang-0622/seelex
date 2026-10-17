# M1 执行记录：契约先行与卫生

日期：2026-10-16
状态：进行中
依据：[plan.md](plan.md) §4「M1 契约先行与卫生」、§5「给长程任务的约束」；`AGENTS.md` §8；根 `MEMORY.md`

本文件是 M1 的**实现记录与验收证据**（一次性工作包文档），不冒充长期事实来源；
长期结论落地后按 `AGENTS.md` §2 迁入 `docs/arch/`。

## 0. 基线与验收口径（改动前）

改动前 HEAD = `96fc163b`，全量门禁结果作为后续每一步「不新增红灯」的比较基准：

| 命令 | 结果 |
|---|---|
| `go build ./...` | 通过 |
| `go build -tags "gui,desktop,production" ./...` | 通过 |
| `go test ./... -count=1 -timeout=900s` | 仅 `application/core` 3 例失败，其余全绿 |

既有红（改动前即可稳定复现，非偶发；`-run` 单跑同样红）：

| 用例 | 位置 | 失败断言 |
|---|---|---|
| `TestContextBudgetSkipsCompactionWithoutMargin` | `application/core/context_budget_margin_idempotency_test.go:74` | 回合 3 又落记录（无余量压缩未做幂等校验） |
| `TestRunningSessionAccumulatesAllRoundsInOrderControl` | `application/core/context_restore_prefix_order_test.go:33` | running 态上下文顺序 = `[user-11 answer-11]`，期望原始顺序 |
| `TestColdRestoreTailFirstRequestKeepsTranscriptOrder` | `application/core/context_restore_prefix_order_test.go:64` | 冷恢复后上下文顺序同上 |

验收口径：每一步都按「同输入同结果 + 相对基线不新增红灯」判定，而不是「测试变绿」；
没有测试文件的包（`application/contract`、`application`）以 `go build` / `go vet` 与编译期、
结构性断言为准。

## 1. 阶段拆分

| 步骤 | 内容 | 状态 |
|---|---|---|
| M1-A | `application/contract`（含 `dto/`）→ 顶层 `contract/`；包名不变；全局改 import 与**现行**文档引用 | **已完成**（证据见 §3.1、§4.1） |
| M1-A2 | 卫生项：仓库在 HEAD 上就有 6 个 `.go` 未 gofmt 干净（CI「格式检查」步骤 HEAD 即红）→ 只做 `gofmt -w`，不夹带搬迁 | **已完成**（证据见 §3.2、§4.2） |
| M1-B | 门面构造函数（`New` / `NewEventHub` / `NewApprovalBroker` / `NewToolHookBridge` / `NewPromptStack` / `NewEffortManager` / `WebSearch`）迁出到 `wiring/` | **已完成**（证据见 §3.3、§4.3） |
| M1-C | `gui/bridge.go` 的 4 条越层 import（`sessionstore` / `seelexctx` / `seelexctx/search` / `seelebridge`）改经门面 | **已完成**（证据见 §3.4、§4.4） |
| M1-D | I1/I2/I3/I4 机械断言 + 允许清单（ratchet，只许收缩） | **已完成**（证据见 §3.5、§4.5） |

## 2. 偏差登记（plan 退出条件本轮暂不可达项）

| ID | plan 退出条件 | 现状 | 依据与处置 |
|---|---|---|---|
| D-M1-1 | 契约层依赖闭包 = stdlib | 暂不可达：`contract/ports.go` 仍依赖 `Seele/types`、`application/approval`、`application/event`、`application/model`、`seelexctx/search`、`seelexctx/snapshot` | [global-review.md](../2026-10-15-product-architecture-pre-design/global-review.md) 的 M1「契约提取」前置含「状态 owner、产品 A/B 模型与 scope 决定」；plan §5.12 禁止实现方自行裁决归属与语义。本轮以 allowlist ratchet 钉住现状、只许收缩，待裁决后收口 |
| D-M1-2 | 前端依赖闭包 ⊆ {门面, 前端子包} | 部分不可达：`gui/bridge.go` 除 plan 点名的 4 条越层边外，还直接 import `application/contract/dto` 与 `application/model` | plan §3.3 只点名 4 条越层边；`model`/`dto` 是纯数据形状，按 plan 口径暂列允许项并写入断言允许清单，M5 门面收窄时统一裁决 |
| D-M1-3 | 「`gofmt -l .` 为空」 | 基线即不空：`application/core/completion.go`、`application/core/project_board_test.go`、`application/core/service_fakes_test.go`、`gui/tool_full_chain_test.go`、`seelebridge/internal/stream/stream.go`、`tui/goalteam_test.go` 在 HEAD 上就未 gofmt 干净（已用 `git show HEAD:<file> \| gofmt -l` 逐个复核）。M1-A 只对因 import 排序而必须格式化的文件执行 `gofmt -w`，并把上述 6 个文件还原为「HEAD 原文 + 仅改 import 路径」，不借搬迁夹带无关格式化 | 本里程碑的 `gofmt -l` 结论按「与基线同名同数」判定，不由 M1-A 顺手修；建议单独立项清理 |
| D-M1-4 | `rg 'application\.New'` 全仓为空 | 部分保留：`docs/devlog/`（4 文件 6 处）与 `docs/2026-09-02-session-subsystem-remediation/architecture-map.md:111` 仍写着迁移前的名字 | 与 M1-A 同款判据——日期工作包与 devlog 是**历史记录**，改写等于伪造当时状态；现行事实文档（`application/README.md`、`wiring/README.md`、代码注释）已全部对齐。`wiring/README.md` 里 1 处是有意的 review 提问（「是否仍有残留」） |
| D-M1-5 | 「`gofmt -l .` 为空」的第二层噪声 | `gofmt -l .` 还会命中 `vendor/`（上游未格式化文件）与 `tmp/reshape/_headcheck/`（M1-A 自建 318 文件的影子副本） | 与 plan P0「清影子树并防止再生」冲突：`tmp/` 虽被 `.gitignore` 覆盖，但仍会污染全局搜索与批量改名。**已在 root 复核时处理**（见 §8：318 个副本改名为 `.go.txt`；本环境的命令策略拒绝递归删除，故目录保留在 gitignored 的 `tmp/` 内）。`vendor/` 命中是既有现象且 CI 无 vendor，不作为本里程碑判据 |
| D-M1-6 | 「前端依赖闭包 ⊆ {门面, 前端子包}」 | **第 5 条越层边**：`gui/headless.go:30` 直接 import `application/core/goal`（`goaldomain.*` 出现在 4 个 RPC 请求/响应签名里）。plan §3.3 只点名 4 条越层边，未点名它 | 该文件按 plan §2.1 计划搬到独立 `headless/` 包（标注「可选，见 D4」）。goal 域类型按 plan §2.7 终局归 `backend/multiagent/goal/`，届时由 contract DTO 承接；本轮**不动**，写入 I2 允许清单（ratchet），M5 门面收窄/headless 拆分时统一裁决 |
| D-M1-7 | 同上 | `application/console`（plan §2.1 列为前端、计划搬 `console/`）直接 import `seelebridge`；`tui` 已干净（`application` + `contract/dto` + `tui/splash`） | plan §3.3 的越层边清单同样未点名 console；本轮不动，写入 I2 允许清单，随 console 搬迁（M5）一并收口 |
| D-M1-8 | 「不新增红灯」 | `seelebridge.TestForkSubagentsBestEffortKeepsSiblingAlive` 在 M1-D 的第一次全量跑中出现 **1 次**失败（`fork_besteffort_test.go:60`），同一代码在 M1-B / M1-C 全量跑与 M1-D 重跑中均 `ok`，单跑 `-count=5` 连续通过 | 失败正文是 `upstream LLM stream failed: context deadline exceeded ... while reading body`——全量并行压满机器时的超时，非本里程碑改动可达的路径（M1-B/C/D 只碰 `application` 门面、`gui/bridge.go`、`wiring/`、`e2e/`，未被该用例引用）。登记为**既有偶发红灯**，不并入 M1 的判据；若再复现应单独立项 |

## 3. 删除清单（原位置 → 唯一位置）

### 3.1 M1-A：契约层物理归位

整组搬迁，无「一处实现两处落点」残留；`application/contract` 目录已不存在。

| 原位置 | 新唯一位置 | 规模 |
|---|---|---|
| `application/contract/ports.go` | `contract/ports.go` | 1 文件 |
| `application/contract/workspace_file.go` | `contract/workspace_file.go` | 1 文件 |
| `application/contract/workspace_tree.go` | `contract/workspace_tree.go` | 1 文件 |
| `application/contract/README.md` | `contract/README.md` | 1 文件 |
| `application/contract/dto/`（41 个 `.go`，含 4 个 `_test.go`）＋ `dto/README.md` | `contract/dto/` | 42 文件 |

同一步内的引用改写（不是新增实现，旧路径无残留）：

1. **import 路径**：309 个 `.go` 文件原地改 `github.com/RedHuang-0622/seelex/application/contract`
   → `github.com/RedHuang-0622/seelex/contract`（44 个随目录搬移的 `.go` 文件无需重写自身 import）。
2. **注释与字符串里的路径字样**：Go 注释约 40 处、`e2e/layout_test.go` 的仓库模块清单
   （`"application/contract"` → `"contract"`）一并对齐。
3. **段式路径**：`e2e/dto_boundary_test.go` 的 `filepath.Join("application", "contract", …)`
   → `filepath.Join("contract", …)`（6 处）。这是搬迁引入的**新红**（2 个用例），由 `e2e` 门禁当场抓住。
4. **现行文档引用**（31 个文件）：`MEMORY.md`、根 `README.md`、`application/README.md`、
   `application/core/README.md`、`application/core/agentteam/README.md`、`contract/README.md`、
   `contract/dto/README.md`、`workspace/README.md`、`internal/README.md`、`internal/adapters/README.md`、
   `internal/testutil/README.md`、`internal/toolrecord/README.md`、`seelebridge/README.md`、
   `seelebridge/tools/README.md`、`seelebridge/internal/mapper/README.md`、`gui/terminal/README.md`、
   `gui/frontend/README.md`、`gui/frontend/dist/{plugin-source,review-request,role-session-view}.js`（注释）、
   `docs/arch/` 10 个文件、`docs/gui/modules/workspace-sandbox.md`、`docs/product/roadmap-2026.md`。

**有意保留未改**（历史记录，改写等于伪造当时状态）：`CHANGELOG.md`、`docs/<日期>-*/`（62 文件）、
`docs/devlog/`（29 文件）、`docs/research/`（3 文件），合计 95 个文件；
另 `contract/README.md` 中 1 处旧路径是本次搬迁的说明文字，属有意保留。

## 4. 验证证据

### 4.1 M1-A

全部命令在 `G:\Program\go\seelex`（Windows / PowerShell）实跑，改动前基线为 HEAD `96fc163b`。

| 命令 | 结果 |
|---|---|
| `gofmt -l .` | 6 个文件，与基线**同名同数**（见 D-M1-3），无新增 |
| `go build ./...` | 通过（exit 0） |
| `go build -tags "gui,desktop,production" ./...` | 通过（exit 0） |
| `go vet ./...` | 通过（exit 0） |
| `go test ./... -count=1 -timeout=900s` | 73 个包 `ok`；仅 `application/core` 3 例既有红（与基线一致），无新增失败 |
| `node --test gui/frontend/dist/*.test.mjs` | 778 pass / 0 fail |
| 残留检查 `rg 'application/contract'`（排除历史目录） | 空；历史记录 95 个文件 + `contract/README.md` 的搬迁说明命中，均为有意保留 |
| `git diff --check` | 通过 |
| 影子树 `tmp/head-src` | 已不存在；`tmp/` 由 `.gitignore` 覆盖（P0 的清树目标已满足，本轮未删任何用户数据） |

结构性证据：

- `application/contract/` 目录已消失，唯一落点为顶层 `contract/`（`git status` 显示 46 条 rename）。
- 契约层 import 路径只剩一种写法，编译器在 353 个 `.go` 文件上强制校验（旧路径无法解析）。
- 两个「按路径读源码」的门禁用例（`e2e/dto_boundary_test.go`）在搬迁后先红后绿，证明门禁确实钉住了新位置。

### 3.2 M1-A2：gofmt 卫生项（独立主题，不计入 M1-A 的搬迁）

无搬迁、无重复实现删除，故**无删除清单**。改动仅为格式：

| 文件 | 改动 | HEAD 上的 gofmt 差异 |
|---|---|---|
| `application/core/completion.go` | 注释列表 `1)`→`1.`、补空注释行 | 21 行 |
| `application/core/project_board_test.go` | 同款注释列表格式 | 21 行 |
| `application/core/service_fakes_test.go` | 同款注释列表格式 | 18 行 |
| `gui/tool_full_chain_test.go` | 同款注释列表格式 | 14 行 |
| `seelebridge/internal/stream/stream.go` | 同款注释列表格式 | 24 行 |
| `tui/goalteam_test.go` | 文件首多余空行、末尾补换行 | 15 行 |

### 4.2 M1-A2

| 命令 | 结果 |
|---|---|
| `gofmt -w`（仅上述 6 文件） | 完成；每条 diff ≤ 6 行，全部为注释/空行/换行 |
| `gofmt -l .`（排除 `vendor/`、`tmp/`） | **空**（HEAD 基线为 6 个文件） |
| `go build ./...`、`go vet ./...` | 通过 |
| `go test ./application/core ./gui ./tui ./seelebridge/internal/stream -count=1` | 仅 `application/core` 3 例既有红，与基线一致 |

### 3.3 M1-B：门面构造函数迁出到 `wiring/`

**删除清单（原位置 → 唯一位置）**：门面包 `application` 的 7 个构造函数整组迁出，
原位置**不再保留转发**（plan §1.3 ④ 的「门面不持有构造函数」与 I3：
`application` 包内无 `func New*`）。实现体逐字未改，唯一实现仍在被转发包，
`wiring` 只提供组合根侧调用点。

| 原位置（`application/application.go`） | 新唯一位置 | 唯一实现 | 规模 |
|---|---|---|---|
| `func New(deps Dependencies) (*Service, error)` | `wiring/constructors.go` | `application/core.New` | 1 行 |
| `func NewEventHub() *EventHub` | `wiring/constructors.go` | `application/event.NewEventHub` | 1 行 |
| `func NewApprovalBroker(events *EventHub) *ApprovalBroker` | `wiring/constructors.go` | `application/approval.NewApprovalBroker` | 1 行 |
| `func NewToolHookBridge() *ToolHookBridge` | `wiring/constructors.go` | `application/core.NewToolHookBridge` | 1 行 |
| `func NewPromptStack() *PromptStack` | `wiring/constructors.go` | `application/prompt.NewPromptStack` | 1 行 |
| `func NewEffortManager(stack *PromptStack, engine …) *EffortManager` | `wiring/constructors.go` | `application/prompt.NewEffortManager` | 4 行 |
| `func WebSearch(ctx, config WebSearchConfig, …) (string, error)` | `wiring/constructors.go` | `seelebridge/search.WebSearch` | 2 行 |
| （门面包内的全部 `func New*`） | —— | —— | **清零**：`rg '^func New' application/` 为空 |

新增文件：`wiring/constructors.go`（本包唯一生产文件）、`wiring/README.md`（按 `AGENTS.md` §3 与
`docs/arch/readme-spec.md` 写全生态位/职责/依赖方向/「装配优先于继承」纪律/文件与函数索引）。

调用点改写（`application.New*(…)` → `wiring.New*(…)`，共 **29 处调用表达式 / 13 个文件**）：

| 分组 | 文件 | 处数 |
|---|---|---|
| 组合根（生产） | `main.go` | 4 |
| e2e 基座（生产） | `e2e/scenario/harness.go`（hub / broker / hooks / `New` 四段） | 4 |
| 根目录测试 | `tool_full_chain_test.go`(3)、`repro_hot_attach_background_tool_order_test.go`(3)、`manual_smoke_test.go`(3)、`permission_employee_panel_test.go`(2)、`release_test.go`(1) | 12 |
| 前端测试 | `gui/tool_full_chain_test.go`(4)、`gui/bridge_test.go`(1)、`tui/tui_test.go`(1)、`tui/queue_edit_test.go`(1)、`application/console/backend_console_test.go`(1) | 8 |
| e2e 测试 | `e2e/scenario/recorder_test.go` | 1 |

注：前端只改**测试**基座的构造调用（生产前端代码零改动，I5）；调用点里的类型沿用门面别名
（`application.Dependencies` 等），未顺手改成 `contract.*`。

同步更新的现行文档与注释（不是搬迁，是引用跟随）：

- `application/README.md`：生态位补「不做：不持有构造函数」、用例图 UC8 与时序图参与方
  `application.New` → `wiring.New`、核心运行流第 1 条、`WebSearch` 归属说明。
- 代码注释 3 处：`application/core/work_table.go`、`application/core/tool_record.go`、
  `teamwork_headless_smoke_test.go`（「装配顺序是契约」那句点名的是构造点，必须跟着搬）。
- `e2e/layout_test.go` 的 `repositoryModules` 追加 `"wiring"`（模块 README 与其链接的门禁名单）。

### 4.3 M1-B

全部命令在 `G:\Program\go\seelex` 实跑（Windows / PowerShell）：

| 命令 | 结果 |
|---|---|
| `gofmt -l .`（排除 `vendor/`、`tmp/`；含新增 `wiring/`） | **空** |
| `go build ./...` | 通过（exit 0） |
| `go build -tags "gui,desktop,production" ./...` | 通过（exit 0） |
| `go vet ./...` | 通过（exit 0） |
| `go test ./... -count=1 -timeout=900s` | 仅 `application/core` 3 例既有红，**无新增**；`e2e`、`e2e/scenario`、`gui`、`gui/terminal`、`tui`、`application/console` 全 `ok` |
| `go test ./e2e/ -count=1`（文档门禁重跑） | `ok` |
| `cmd /c "node --test gui/frontend/dist/*.test.mjs"` | 778 pass / 0 fail |
| `go list -f '{{.Name}} {{.ImportPath}}' ./wiring` | `wiring github.com/RedHuang-0622/seelex/wiring` |
| `rg '^func New' application/`（仅 `application` 包） | 空（I3 满足） |
| `rg 'application\.(New\|NewEventHub\|NewApprovalBroker\|NewToolHookBridge\|NewPromptStack\|NewEffortManager\|WebSearch)\('` | 空 |
| `git diff --check` | 通过（仅 CRLF 提示，无空白错误） |

结构性证据（不是「测试变绿」）：

- **编译器钉住唯一入口**：门面包 `application` 已无 `func New*`，旧调用写法无法解析——
  任何漏改的调用点会直接编译失败，而不是静默走另一条路。
- **类型身份未变**：`wiring` 的 7 个签名全部使用 `type X = Y` 别名指向的实体类型
  （`contract.Dependencies`、`core.Service`、`event.EventHub`、`approval.ApprovalBroker`、
  `core.ToolHookBridge`、`prompt.PromptStack`、`prompt.EffortManager`、`search.WebSearchConfig`），
  没有产生新类型，故前端与测试无需改类型引用（I4 在 M1-D 机械断言）。
- **同一份实现**：7 个函数体都是单行转发，实现体仍各只有一份（`AGENTS.md` §8）；
  `wiring` 内无判据、无默认值、无状态。
- **装配顺序契约随调用点更新**：`tool_full_chain_test.go` / `teamwork_headless_smoke_test.go` /
  `work_table.go` / `tool_record.go` 里点名「在 `…New` 之前注入」的注释已同步到 `wiring.New`，
  避免注释指向一个不存在的符号。

### 3.4 M1-C：`gui/bridge.go` 越层 import 改经门面

**删除清单（原位置 → 唯一位置）**：4 条越层 import 边**整条删除**，消费点改经门面。
没有搬迁文件、没有第二份实现；门面只新增「类型别名 + 1 个无状态函数」，不新增任何业务判据。

| 原位置（`gui/bridge.go` 的import / 消费点） | 新落点 | 机制 | 处数 |
|---|---|---|---|
| `seelebridge`（`ScheduledTaskSpec` / `ScheduledTaskStatus`） | `contract/dto`（**已存在**的事实源） | 直接改用上游类型：`dto.ScheduledTaskSpec` / `dto.ScheduledTaskStatus`；`seelebridge` 的同名符号只是 aliases（plan §2.10 列入删除） | 8 |
| `sessionstore`（`Config`） | 门面别名 `application.SessionStorageConfig` | `type SessionStorageConfig = sessionstore.Config`；终局由 M3 的 store DTO 承接 | 6 |
| `seelexctx/search`（`Result`、`Hit`、`ChatRecord`） | 门面别名 `application.SearchResult` / `SearchHit` / `SearchRecord` | 同上；终局是 plan §2.1 的 `contract.SearchPort`（M4） | 2 |
| `seelexctx`（`AccountContextWindowPresets`） | 门面函数 `application.AccountWindowPresets()` | 一行转发，挡位表仍只有 `seelexctx` 一份 | 1 |
| （4 条 import 语句本身） | —— | 整个 import 块只剩 `application` / `application/model` / `contract/dto` / `gui/terminal` | **4 条边清零** |

生产前端代码只有 `gui/bridge.go` 一个文件改动（plan §2.1 说的就是「只改 import」）；
`gui/bridge_test.go`、`gui/tool_full_chain_test.go` 等测试文件**未改**——别名是同一类型身份，
测试里的 `sessionstore.Config` / `seelebridge.ScheduledTaskSpec` 写法仍满足接口。

同步更新的现行文档：`application/README.md` 新增「门面再导出的 DTO（M1-C）」小节（逐条登记
门面导出、指向、消费用例与终局）。

### 4.4 M1-C

| 命令 | 结果 |
|---|---|
| `go build ./...`、`go build -tags "gui,desktop,production" ./...`、`go vet ./...` | 全部通过（exit 0） |
| `go test ./... -count=1 -timeout=900s` | 仅 `application/core` 3 例既有红，**无新增** |
| `cmd /c "node --test gui/frontend/dist/*.test.mjs"` | 778 pass / 0 fail |
| `gofmt -l application gui wiring e2e` | 空 |
| `go list -f '{{.Imports}}' ./gui` 中的 seelex 包 | `application`、`application/core/goal`、`application/model`、`contract/dto`、`gui/terminal`（**4 条被点名的越层边已消失**） |
| `go list -f '{{.Imports}}' ./tui` 中的 seelex 包 | `application`、`contract/dto`、`tui/splash`（前端侧本就干净） |
| `go list -f '{{.Imports}}' ./application/console` 中的 seelex 包 | `application`、`seelebridge`（D-M1-7，未点名，本轮不动） |
| `rg 'sessionstore\.\|seelebridge\.\|seelexctx\.\|seelexctxsearch\.' gui/bridge.go` | 空 |
| `git diff --check` | 通过 |

结构性证据（不是「测试变绿」）：

- **4 条边清零有直接证据**：`go list -f '{{.Imports}}' ./gui` 的输出里已经没有
  `sessionstore` / `seelexctx` / `seelexctx/search` / `seelebridge`——这是编译器认可的
  直接依赖表，不是注释或约定。
- **不新增第二份实现**：`AccountWindowPresets` 是一行转发；`SessionStorageConfig` /
  `SearchResult` 是 `type X = Y` 别名（同类型身份，编译期钉住），挡位表与配置结构仍各只有一份。
- **不新增事实源**：定时任务形状直接落到 `contract/dto/scheduler.go`（唯一事实源），
  而非在门面再造一份别名——顺着 plan §2.10「删 19 处别名、改用上游类型」的方向走。
- **门面依赖闭包未增长**：`sessionstore` / `seelexctx` / `seelexctx/search` 早已在
  `go list -deps ./application` 的传递闭包里（经 `application/core`），新增的只是**直接**
  import；I1 的 ratchet（传递闭包）不因本步增长。

### 3.5 M1-D：I1–I4 机械断言 + ratchet 允许清单

无搬迁、无重复实现删除，故**无删除清单**；新增一个门禁文件与两张清单。

| 落点 | 内容 |
|---|---|
| `e2e/facade_boundary_test.go`（新增） | I1–I4 六条断言 + 四个工具函数（`go list` 封装、包文件枚举、stdlib 判定、集合） |
| `facadeClosureAllowList` | 门面传递闭包里的 **66 个 Seelex 包**（2026-10-16 实测），只许收缩 |
| `contractClosureAllowList` | 契约层闭包里的 **29 个非 stdlib 包**（18 个 Seele/otel 等外部 + 11 个 Seelex 包），即 D-M1-1 的具象化 |
| `frontendDirectImportAllowList` | `./gui`（5）、`./tui`（3）、`./application/console`（2）的**直接** import |
| `e2e/README.md` | 子模块章节登记这组架构门禁（与 `layout_test.go` 并列），说明「ratchet 只许收缩」 |

四条不变量的落点与理由：

| ID | 断言 | 判据基座与理由 |
|---|---|---|
| I1 | `TestFacadeDependencyClosureRatchet`（门面闭包里的 Seelex 包 ⊆ 清单） | `go list -deps ./application`。**只 ratchet Seelex 前缀**：闭包今天还有 ~220 个第三方叶子包（Seele/viper/grpc/otel…），逐条登记只会淹没层边界的信号 |
| I1′ | `TestContractDependencyClosureIsStdlibOrAllowlisted`（契约层闭包的非 stdlib 包 ⊆ 清单） | 同上 `-deps`；这里是 plan §4 M1 的退出条件「闭包 = stdlib」，所以连 Seele 与 otel 都逐条列出 |
| I2 | `TestFrontendDirectImportsStayInsideFacade`（三个前端入口的**直接** import ⊆ 清单） | `go list -f '{{.Imports}}'`。用**直接**而非传递闭包：前端 import 门面，而门面今天仍传递依赖 `application/core → seelebridge/sessionstore/seelexctx`，「前端传递闭包 ⊆ 门面」在 M5 收窄之前物理上不可能成立（D-M1-2/D-M1-6/D-M1-7 记录了三处允许项） |
| I2′ | `TestGUIHasNoDirectBackendImports`（§3.3 点名的 4 条边任何一条回来即失败） | 单独一条，报错文案指向 M1-C 的删除清单——防止「允许清单被人手放宽」把回归掩盖掉 |
| I3 | `TestFacadeHasNoConstructors`（`application` 包内无 `func New*`） | 解析生产 `.go` 的 AST；与 M1-B 的结构性证据互补（那条靠编译器，这条靠断言） |
| I4 | `TestFacadeKeepsTypeIdentityByAlias`（门面包内每个 `type` 都是 `type X = Y`） | 同上；防「搬迁顺手造新类型身份」把前端的零改动前提悄悄破坏 |

### 4.5 M1-D

| 命令 | 结果 |
|---|---|
| `go test ./e2e/ -count=1 -run 'TestFacade\|TestContractDependency\|TestFrontendDirect\|TestGUIHasNoDirect' -v` | 6 个用例全 `PASS` |
| `go test ./e2e/ -count=1` | `ok`（含 layout 的 README/链接门禁） |
| `go test ./... -count=1 -timeout=900s`（重跑） | 仅 `application/core` 3 例既有红 |
| `gofmt -l e2e application gui wiring` | 空 |

**先红后绿证据**（门禁不是「假绿」）：把探针打进工作树、跑门禁、确认红、再撤销：

| 变异 | 期望 | 实际输出 |
|---|---|---|
| `facadeClosureAllowList` 删掉 `sessionstore` | I1 红 | `门面依赖闭包新增了未登记的 Seelex 包：github.com/RedHuang-0622/seelex/sessionstore` |
| console 清单删掉 `seelebridge` | I2 红 | `./application/console 直接 import 了门面之外的 Seelex 包：…/seelebridge` |
| `application/application.go` 加 `func NewProbeForGateCheck()` | I3 红 | `application.go 定义了构造函数 NewProbeForGateCheck：门面不持有构造函数（I3）…` |
| 同文件加 `type ProbeForGateCheck struct{}` | I4 红 | `application.go 定义了新类型 ProbeForGateCheck（不是 type X = Y 别名）…` |

四个变异全部按预期报红（同一次 `go test` 里 4 个用例同时 FAIL），撤销后 6 个用例恢复全绿；
`git diff --stat application/application.go` 证明探针已完全移除（只剩 M1-B/C 的预期改动）。

**为什么用 `go list` 而不是自己解析源码**：`go list` 按当前平台的 build tags 过滤文件，
手工解析会把 `main_windows.go` 之类的平台文件 import 也算进来，让门禁在别的平台误报。

## 5. M1-A 阶段审查（root 复核，2026-10-16）

审查方式：不看子代理自述，直接对工作树做机械核对。

| 审查项 | 方法与结果 |
|---|---|
| 是否夹带行为改动 | `git diff -U0 -- '*.go'` 的 750 条 `+/-` 行全部是 import 行、注释路径字样或 `filepath.Join` 段式路径；唯一改动 >4 行的 `.go` 文件是 `e2e/dto_boundary_test.go`（13+/13-，全部是路径字符串与一条错误消息文案） |
| 包名与导出面 | `go list -f '{{.Name}}' ./contract ./contract/dto` → `contract` / `dto`；`ports.go`、`dto/plan.go` 首行与 HEAD 一致 |
| 是否动了用户数据 | `git status --porcelain` 只有 `R`/`M`/`??`；无 `D`；`config/`、`dist/`、`.seelex/` 未出现 |
| 是否新增红灯 | 迁移后全量 `go test ./... -count=1 -timeout=900s` 的失败集合与基线**逐条相同**（`application/core` 3 例），无新增 |
| 前端行为 | `node --test gui/frontend/dist/*.test.mjs` → 778 pass / 0 fail |
| 门禁是否被削弱 | `e2e/dto_boundary_test.go` 的断言逻辑未改，只改路径与文案；`e2e/layout_test.go` 的仓库模块清单已同步为 `contract`（守卫跟着新位置走） |
| 文档链接漂移 | `python scripts/check_readme_refs.py` → 16 个 README 有未解析引用，**无一条与 `contract` 相关**（均为 J-1a 退役等既有漂移） |
| 历史记录是否被改写 | 残留扫描排除历史目录后仅剩 `contract/README.md` 里 1 处搬迁说明；`CHANGELOG.md`、`docs/<日期>-*/`、`docs/devlog/`、`docs/research/` 未被改写 |

结论：M1-A **通过**。遗留（不阻塞）：

1. `contract/README.md` 未按 [`../arch/readme-spec.md`](../arch/readme-spec.md) §3 追加「文件与函数索引」——搬迁前即缺，属既有文档债，建议随 M1-D 或门面收窄一起补。
2. `contract/ports.go` 的依赖闭包问题见 §2 的 D-M1-1，需用户裁决后才能收口。

## 6. M1-B/C/D 复核（2026-10-16）

**口径声明（重要）**：M1-B、M1-C、M1-D 由**同一个执行者**完成，因此本节不是独立第三方复核，
而是「机械复核 + 先红后绿证据」。原任务书（`tmp/reshape/m1b-task.md`）要求派子代理执行、
root 只做阶段审查，但本环境的子代理消息通道不可用（详见 §7），执行者与复核者被迫合一——
这一点必须让后续读者知道，不能把本节当作 M1-A 那种「不看出自述、直接核工作树」的独立复核。

| 复核项 | 方法 | 结果 |
|---|---|---|
| 是否夹带行为改动（M1-B） | `git diff -- application/application.go` 逐行读 | 只有：删 7 个构造函数、删 `context` import、加 3 处注释、`ValidEffortLevels`/`RenderDiag` 补 doc 首行。**无逻辑行** |
| 7 个构造函数是否只是转发 | 读 `wiring/constructors.go` | 每函数一行转发到被转发包；无判据、无默认值、无状态 |
| 类型身份是否变化（I4） | 机器断言 + 人工读签名 | 6 个用例全绿；签名全用别名指向的实体类型 |
| 门面是否还留兼容转发（I3） | `TestFacadeHasNoConstructors` + `rg '^func New' application/` | 两边都空 |
| 调用点是否改全 | `rg 'application\.(New\|NewEventHub\|…)\('` | 空；漏改会直接编译失败（旧写法无法解析） |
| 4 条越层边是否真消失（M1-C） | `go list -f '{{.Imports}}' ./gui` | 已消失；且 `TestGUIHasNoDirectBackendImports` 单独钉住这 4 条 |
| M1-C 是否只改 import | `git diff gui/bridge.go` | 只改限定名与 import 块；接口方法名、参数顺序、错误语义未变 |
| 是否动了用户数据 | `git status --porcelain` | 只有 `M`/`??`；无 `D`；`config/`、`dist/`、`.seelex/` 未出现 |
| 是否新增红灯 | 三次全量 `go test ./...` | 2 次只有基线 3 例；1 次多出 1 例既有偶发（D-M1-8，已隔离验证） |
| 门禁是否被削弱 | 读 4 个变异实验的输出 | 门禁在变异下全部按预期报红（§4.5） |
| 历史记录是否被改写 | `rg 'application\.New'`（排除 vendor） | 只剩 `docs/devlog/`（4 文件）与 `docs/2026-09-02-*` 的 1 处，均为**有意保留**（D-M1-4） |

结论：M1-B/C/D **通过机械复核**；但因执行者与复核者同一，建议下一轮由另一位执行者按
本节的复核表重跑一遍（尤其 §4.4 的 `go list` 输出与 §4.5 的变异实验）。

遗留（不阻塞，按优先级）：

1. **D-M1-1 / D-M1-6 / D-M1-7**：三处需要用户裁决的归属问题（契约层闭包、goal 域透传、
   console 的 seelebridge 依赖），已进 ratchet 清单，只许收缩。
2. **「文件与函数索引」缺口的既存债务**：`contract/README.md`、`contract/dto/README.md`、
   `e2e/README.md`、`application/README.md` 都没有 `## 文件与函数索引`（`docs/arch/readme-spec.md` §3
   要求）；生成器 `scripts/gen_core_readme_index.py` 目前只覆盖 `application/core`。本轮只保证**新增**的
   `wiring/README.md` 带索引。建议单独立项：把提取逻辑扩到全仓，一次补齐。
3. **D-M1-5**：`tmp/reshape/_headcheck/`（318 文件影子副本）要清掉或改成非 `.go`，否则
   `gofmt -l .` 与全局搜索会一直带噪声。属 M1-A 的交付物，未擅自删。

## 7. 子代理通道（为什么 M1-B/C/D 是同一个执行者做的）

任务书 `tmp/reshape/m1b-task.md` 要求「你是执行子代理，只做 M1-B」。实际执行链路：

| 观察 | 结论 |
|---|---|
| `followup_task` 给 `/root/m1b_wiring` 发完整任务书 + 状态 ping，等 3 分钟 + 再等 5 分钟，**零输出、零落盘**（`wiring/` 目录不存在、`git status` 无相关改动） | 消息没有真正触发对方回合 |
| `list_agents` 里 `/root/m1b_wiring` 一直显示 `running`，但机器上没有任何 `go`/编译器子进程 | 该条目是**停滞状态**，不是在执行 |
| `interrupt_agent /root/m1b_wiring` 返回 `an agent cannot interrupt itself`；对不存在的路径返回 `live agent path ... not found`；对 `/root/m1a_move2` 正常返回其 `previous_status` | 工具本身可用，但 `/root/m1b_wiring` 被解析成**调用者自己**——即那个「子代理」在环境里没有独立执行实体 |

因此「唤醒子代理」在当前环境**做不到**：没有可唤醒的活体。M1-A 的执行者（`/root/m1a_move2`）
独立遇到同一现象并得出同一结论，本轮的观察与它一致。

处置：M1-B/C/D 由 root 侧直接完成，并按任务书的验证清单逐条实跑（§4.3–§4.5）；
`tmp/reshape/m1b-task.md` 的**目标与判据全部保留**（7 个构造函数、零行为变化、I3、
调用点全量更新、README 要求、验证命令清单），只换了执行者。差别逐条列出：

| 任务书要求 | 本轮实际 |
|---|---|
| 只做 M1-B，不动 M1-C/D | **偏离**：M1-B 完成后继续做了 M1-C、M1-D（用户指令「continue」＋ plan §4 的 M1 退出条件包含它们）。M1-C/D 的记录在同文件 §3.4/§3.5，未与 M1-B 混在一节 |
| 不回头改 M1-A 的成果 | 遵守：未改 M1-A 的搬迁与 A2 的格式化结论（D-M1-5 只登记不处理） |
| 调用点里的类型沿用门面别名 | 遵守：`application.Dependencies` 等未改成 `contract.*` |
| 新建/改文件用 apply_patch | 遵守 |
| `node --test` 在 `cmd /c` 里跑 | 遵守（PowerShell 不展开通配符） |
| 不 commit、不 push | 遵守 |

## 8. root 复核：M1-B / M1-C / M1-D（2026-10-16）

复核方式与 §5 相同：不看执行者自述，直接对工作树做机械核对，并**自己重做变异实验**
（执行者与复核者是同一实体这一事实已记录在 §7，所以复核证据必须由 root 侧独立产出）。

### 8.1 独立变异实验（证明四条门禁不是"假绿"）

探针只打进工作树、跑门禁、按预期报红后**逐条撤销**，撤销后 6 条断言恢复全绿；
`rg 'ProbeForReview' application/application.go` 为空，工作树无残留。

| 变异（root 自己注入） | 期望 | 实测 |
|---|---|---|
| `application/application.go` 加 `func NewProbeForReview() {}` | I3 红 | `TestFacadeHasNoConstructors` FAIL：`application.go 定义了构造函数 NewProbeForReview：门面不持有构造函数（I3）…` |
| 同文件加 `type probeTypeForReview struct{}` | I4 红 | `TestFacadeKeepsTypeIdentityByAlias` FAIL：`application.go 定义了新类型 probeTypeForReview（不是 type X = Y 别名）…` |
| `facadeClosureAllowList` 删 `seelebridge` | I1 红 | `TestFacadeDependencyClosureRatchet` FAIL：`门面依赖闭包新增了未登记的 Seelex 包：…/seelebridge` |
| console 允许清单删 `seelebridge` | I2 红 | `TestFrontendDirectImportsStayInsideFacade` FAIL：`./application/console 直接 import 了门面之外的 Seelex 包：…/seelebridge` |

第 1 条探针第一次写成了 `probeNewProbeForReview`（不以 `New` 开头）而**未报红**，
改名后按预期报红——这说明 I3 的判据是"函数名以 `New` 开头"而不是"看起来像构造函数"，
也说明这条门禁对拼写敏感、不会误伤普通函数。

### 8.2 复核命令（root 实跑）

| 命令 | 结果 |
|---|---|
| `gofmt -l .`（排除 `vendor/`、`tmp/`） | 空 |
| `go build ./...` | exit 0 |
| `go build -tags "gui,desktop,production" ./...` | exit 0 |
| `go vet ./...` | exit 0 |
| `go test ./e2e/ -count=1 -run 'TestFacade\|TestContractDependency\|TestFrontendDirect\|TestGUIHasNoDirect' -v` | 6 条断言全 PASS |
| `go test ./... -count=1 -timeout=900s` | 73 包 `ok`；失败集合与基线**逐条相同**（`application/core` 3 例），本轮未出现 D-M1-8 的偶发 |
| `node --test gui/frontend/dist/*.test.mjs` | 778 pass / 0 fail |
| `go list -f '{{.Imports}}' ./gui` 里的 seelex 包 | `application`、`application/core/goal`(D-M1-6)、`application/model`(D-M1-2)、`contract/dto`(D-M1-2)、`gui/terminal`——**§3.3 点名的 4 条边确实不在** |
| `rg '^func New' application/` | 空（I3 结构性证据） |
| M1-B 调用点 diff 扫描（`main.go`、`e2e/scenario`、`gui`、`tui`、`application/console`） | 除 `wiring.` / `application.` / import 行外，只有 1 处空行-花括号的格式差异，无逻辑改动 |

### 8.3 复核结论

M1-B / M1-C / M1-D **通过复核**。plan §4 M1 的退出条件逐条对照：

| 退出条件 | 结论 |
|---|---|
| 契约层已提为顶层包 | 达成（M1-A，§5） |
| 契约层依赖闭包 = stdlib | **未达成**，D-M1-1 待裁决（本轮以 `contractClosureAllowList` 钉住 29 个非 stdlib 包） |
| 前端三入口依赖闭包 ⊆ 门面 | **直接依赖达成**（I2 + I2′）；**传递闭包未达成**（前端经门面仍触达 core/seelebridge——门面收窄属 M5），D-M1-2/D-M1-6/D-M1-7 记录了三处允许项 |
| 前端行为零变化 | 达成（前端生产代码只改 `gui/bridge.go` 的 import 与类型名；JS 778/778） |
| `go build ./...` 与测试通过 | 达成（相对基线不新增红灯） |
| 前端 4 条越层边消失 | 达成（有 `go list` 与 I2′ 双重证据） |
| 每步删除清单 | 达成（§3.1–§3.5，逐步列出"原位置 → 新唯一位置"） |

### 8.4 复核发现的小问题（不阻塞，登记备查）

1. `application.SearchHit` / `application.SearchRecord` 目前**没有任何消费点**（门面多出两个未使用的导出别名）。
   保留无害但属"多出来的面"，建议 M4 收口 `contract.SearchPort` 时删掉或改为按需再导出。
2. 门面新增了 3 条**直接** import（`sessionstore` / `seelexctx` / `seelexctx/search`）作为 M1-C 的过渡别名。
   传递闭包未增长（I1 因此不报警），但"I1 的层边界"在直接边层面尚未成立；已在 §3.4 与 `application/README.md`
   登记为终局待迁项。
3. `tmp/reshape/_headcheck/` 的 318 个副本已由 root 改名为 `.go.txt`（本环境策略拒绝 `Remove-Item -Recurse`，
   目录仍留在 gitignored 的 `tmp/` 内）。若要彻底清掉该目录，需要你在终端执行一次删除。
