# R10 / R11 / P13 冻结契约（2026-10-17）

本文是**先定契约、再派实现**的那份契约。派给子代理的任务书必须引用本文；
**实现方不得改动本文列出的任何声明**（签名、名字、文件位置、错误语义、门禁判据）。
若实现中发现契约本身有问题，**停下来回报**，由 root 改本文后再继续。

## 0. 全局约束（三片共同）

1. `contract/**`（`Dependencies` / `RuntimePort` / `ChatEngine` / `Plugin/Skill/Session/WorkspacePort` /
   `dto/**`）**逐字节不变**：任何一片的提交里 `git diff --stat -- contract` 必须为空。
2. `middle/protocol/**`（`model.*` / `event.*` 形状）同样**不变**。
3. 前端（`gui/**`、`tui/**`）**零改动**（I5）：`git diff --stat -- gui tui` 必须为空。
4. 搬迁/收口**只改落点与装配**，行为零变化：不改任何错误文案、JSON 字段、持久化格式、路由语义；
   每条切片都要在记录里列"删除清单（原位置 → 新唯一位置）"与"保真证据"。
5. 基线（开工前即红，不许去修）：`application/core` 3 例
   （`TestContextBudgetSkipsCompactionWithoutMargin` / `TestRunningSessionAccumulatesAllRoundsInOrderControl` /
   `TestColdRestoreTailFirstRequestKeepsTranscriptOrder`）。
6. 提交纪律：一个主题一个 commit；**只 `git add` 显式路径**（工作树里有其他 agent 的在途改动）；
   不 `--amend`、不 push、不动 `main`。

## 1. R10：门面收窄成五组窄接口（先做 R10a，见 §1.3）

### 1.1 冻结的接口（新增 `application/faces.go`，包 `application`）

四个**行为面**接口 + 一份**类型别名清单**（A 组是类型身份，不能用方法集表达）：

```go
// A 组（类型与事件身份）用清单钉住：门面导出的类型别名集合必须逐字等于这份清单。
var FaceTypeNames = []string{ /* 见 §1.4 的清单 */ }

// B 组：读投影（Snapshot 及其派生读面、会话目录、历史检索、工作区读面…）。
type ViewFace interface { /* 方法集 = 今日 application 已导出的读投影方法，逐字 */ }

// C 组：命令面（提交/取消/会话生命周期/工作区绑定/子代理恢复…）。
type CommandFace interface { /* 逐字 */ }

// D 组：档位与策略（loop / effort / 权限档位 / 驻留窗口 / 账号 / 插件 / 调度…）。
type PolicyFace interface { /* 逐字 */ }

// E 组：队列与无状态函数（召回/调序/建议；HasSigilPrefix / SigilOf / RenderDiag / ValidEffortLevels…）。
type QueueFace interface { /* 逐字 */ }
```

**成员归属规则（冻结）**：

- 方法名**逐字取自今日 `application` 已导出的方法面**（`Service` 的方法集，去掉 `Set*` 内部装配口
  与测试专用口）；分组按 `plan.md` §1.3 的 A–E 表；
- **不得新增、删除、改名、改签名**：I5 要求前端零改动，而前端今天通过这些名字消费门面；
- 一个方法**只能属于一组**；`SetProjectBoardSink` 之类的装配口不进任何组（由 `wiring` 持有）。

### 1.2 冻结的断言（新增 `e2e/facade_narrow_faces_test.go`）

1. **编译期**：`var _ application.ViewFace = (*application.Service)(nil)`（四个面各一条，写在
   `application/faces.go` 里，作为结构满足的编译期钉子）；
2. **方法集快照**：用反射列出四个面的方法名（排序后）与**冻结清单**逐字比较——实现方要更新清单
   必须显式改测试（这就是"契约不许随手变"的机械保障）；
3. **别名集合**：反射列出 `application` 包导出的类型别名名集合，与 `FaceTypeNames` 逐字比较；
4. **前端零改动**：断言 `git diff --stat -- gui tui` 为空（用 `os/exec` 跑 git；无 git 时 skip）。

### 1.3 R10 分两片（**先 R10a，R10b 等 core 静默后再做**）

| 片 | 范围 | 为什么分开 |
|---|---|---|
| **R10a** | 只新增 `application/faces.go` + `e2e/facade_narrow_faces_test.go` + 相关 README | 不碰 `application/core/**`，可与"core 里正在搬别的文件"的切片并行 |
| R10b | 删 `application/core/aliases.go`（204 行）→ core 内部改用限定名（`model.X` / `dto.Y` / `event.Z`） | 要改**每一个** core 文件，必须在 core 没有其他在途改动时独家执行 |

### 1.4 A 组别名清单（R10a 生成，冻结规则）

清单 = 今日 `application/application.go` 里**指向 `contract` / `model` / `event` / `dto` 的类型别名**，
按**字母序**排列（含 `Service` / `Dependencies` / 各 Port）；实现方生成后必须把清单**逐字**写进
`faces.go`，并让断言 3 用它作为唯一事实源。

## 2. R11：`backend/engine/assemble/ports.go` 归零 + `Runtime` 按域下沉

### 2.1 冻结的接口

- `contract.RuntimePort`（以及 `Dependencies`）**签名不变**；`wiring/**` 只允许改 import 与构造调用；
- 窄转发的编译期钉子钉在装配侧，不在引擎侧：
  `var _ contract.RuntimePort = RuntimePort{}`（`wiring/adapters/engine_port.go`，已存在则复用）。
  `assemble.Runtime` 本身**不是**窄转发者，不要写 `var _ contract.RuntimePort = (*Runtime)(nil)`；
- `backend/engine/assemble.Runtime` 的**公开方法名与签名逐字不变**：R11 各片只在包内搬家，
  或把状态下沉到域包后留**一行薄转发**。不许删方法、改名、改签名——前端与 `wiring/adapters`
  的手写窄转发都靠它；
- 若某片确实需要新增/改动 `Runtime` 的公开方法，必须同步在
  `wiring/adapters/runtime_narrow_ports.go` 补窄转发 + `var _` 断言，并跑
  `go test ./wiring/... -run RuntimePort`；
- `ports.go` 的终点是**文件不存在**（内容按域搬进域包或 `runtime_<域>.go`），不是删面。
### 2.2 每个域的冻结形状（依赖倒置）

每个被拆出的域**在自己的包里**（例如 `backend/engine/task`、`backend/tool/record`…）：

```go
// <domain>/ports.go —— 该域需要的宿主能力面（零依赖契约：只用 stdlib + contract + middle/protocol）
type Host interface { /* 该域真正用到的读数/写面，逐条列出，不预留 */ }

// <domain>/<domain>.go
type Domain struct { /* 该域自己的状态 */ }
func New(host Host) *Domain
```

- **装配点在 `wiring/bootstrap`**：由它把 `Host` 实现（通常是 `Runtime` 或 core）注入；
- 域实现里**禁止** `if kind == …` / `if role == …` 之类按主体分支（AGENTS §8.5）；
- 域包**不得 import** `backend/engine/assemble`（否则只是把循环搬家）。

### 2.3 切片与验收（顺序冻结）

1. **R11-task**：`ports.go` 里 task 域方法（`TaskSnapshot*` / `TaskAdd*` / `ResolveTaskByKey*` /
   `TaskSetStatus*` / `TaskAttachParticipant` / `TaskAppendTrace` / `SwitchSessionTasks` /
   `SetCurrentTaskBatch` / `CurrentTaskBatchFor` …）→ `backend/engine/task`；`Runtime` 只保留薄转发
   或直接由装配点接到域实现；
2. 后续域逐个同法（record / media / account / mcp / plugin / skill / obs / schedule / workunit / teamwork…），
   每片一个 commit；
3. **收口片**：`ports.go` 删除（文件不存在），`Runtime` 只剩 deps/bundle/live/拓扑+关机。

**每片验收（缺一不可）**：

```text
gofmt -l .（排除 tmp/vendor/stage/dist）
go build ./...  go build -tags "gui,desktop,production" ./...  go vet ./...
go test ./e2e/ -count=1
go test ./backend/engine/... ./test/ -count=1
go list -deps ./backend/engine/<域包> | grep -c 'backend/engine/assemble'   # 必须为 0
```

## 3. P13：README「文件与函数索引」生成器全仓化

### 3.1 冻结的 CLI 与格式

```text
python scripts/gen_core_readme_index.py            # 全仓刷新（只写"已定案树"）
python scripts/gen_core_readme_index.py --check     # 只检查漂移：有漂移 exit 1，无漂移 exit 0，不写文件
python scripts/gen_core_readme_index.py --only <import-path>…   # 只处理给定包（调试用）
```

- **索引格式冻结**（与仓库既有 README 逐字一致）：
  小节标题 `## 文件与函数索引`；说明两行（`> 由源码 doc 注释自动提取（首行摘要）…` /
  `> 刷新方式：\`python scripts/gen_core_readme_index.py\`。`）；每个文件 `### <文件名>`；
  每个函数一行 `- \`func …\` — <doc 注释首行摘要>`；
- **幂等**：连跑两次，第二次 `git status --porcelain` 为空；
- **UTF-8 无 BOM**；含中文的生成逻辑落盘执行（脚本内，不经 shell 管道）；
- **不碰**：`docs/devlog/**`、`docs/research/**`、`docs/<日期>-*/**`、`CHANGELOG.md`、`tmp/**`、
  `vendor/**`、`stage/**`、`dist/**`；
- **搬迁中/收口中的树跳过并登记**（`application/core/**`、`middle/**`、`backend/engine/assemble/**`、
  `backend/tool/**` 的 README 由各自切片负责）。

### 3.2 验收

```text
python scripts/gen_core_readme_index.py && python scripts/gen_core_readme_index.py --check   # 第二次 exit 0
python scripts/check_readme_refs.py                                                          # 未解析条数不增
gofmt -l .（排除 tmp/vendor/stage/dist）空；go build ./... 0
```

并交付：修掉**既有漂移**（`application/core` 分卷 README 里与本轮无关的 9 处，见 `m5-record.md` §22 登记）
——注意那批属"搬迁中树"，只登记不改；其余已定案树的漂移要一次刷干净。

## 4. 文件所有权（防并发改同一文件）

| 路径 | 谁可改 |
|---|---|
| `application/faces.go`、`e2e/facade_narrow_faces_test.go` | R10a |
| `application/core/aliases.go` 及 core 内部限定名改写 | R10b（须等 core 静默） |
| `backend/engine/assemble/**` + 各域包 `ports.go` | R11 |
| `scripts/gen_core_readme_index.py` + 已定案树 README | P13 |
| `middle/**`、`application/core/*.go`（R6 片） | root（R6 主线） |
| `docs/2026-10-15-architecture-reshape/**` | root |

冲突处理：**任何一片发现自己要改的文件已被别人动过**（`git status` 里已是 `M`），停下来在
`tmp/reshape/<你自己>-progress.txt` 记一行并回报 root，不要覆盖别人的改动。
