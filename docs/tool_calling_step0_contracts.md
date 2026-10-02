# 打点表：契约落实（P0）→ 契约下的实现（P1）→ 契约进 seele + loop 并发（P2）

> 前置：`docs/tool_concurrency_design.md`（v3 详细设计）
> 测试用例：`docs/subprocess_contract_testcases.md`
> 本文所有「现状」陈述均带 `file:line`，可直接复核；标注 **【提议】** 的是设计而非现状。

---

## 打点表使用法

一条打点 = **一个可独立完成、可独立验证、可独立回滚的动作**。每行五列：

| 列 | 含义 |
|---|---|
| **ID** | 打点编号（`K-*` = P0，`L-*` = P1，`S-*` = P2） |
| **动作** | 要做什么（一句话，动词开头） |
| **落点** | 具体文件 / 符号（可 `file:line` 复核） |
| **判据** | **可执行**的验收：一条命令、一个断言或一张表驱动用例（用例编号指向测试用例文档） |
| **依赖** | 前置打点；空 = 可立即开工 |

**阶段门**：P0 与 P1 **同步推进**（用户裁定）；**P2 有门**——必须先用 P1 的实测数据证明"结果晚一轮不可接受"（L-3 的判据），否则不开工。

---

## P0：契约落实

| ID | 动作 | 落点 | 判据 | 依赖 |
|---|---|---|---|---|
| **K-0** | 给 `RegisterTool` 加 meta 声明路径，并给**全部现有工具**填 `Groups` | `seelebridge/runtime_tools.go:264`、`seelebridge/tools/registry_state.go:48-64`、`main.go` 内联注册处 | 遍历全量注册工具断言 `Groups` 非空（TC-K0-1）；`go test ./seelebridge/...` 全绿 | — |
| **K-1** | 定义 `JobTool` interface（`Add` + **具名四动作** `Status`/`Fetch`/`Kill`/`Done`，出参一律 `[]byte`）并写进代码 | `seelebridge/tools/job_contract.go` | 编译期断言：`bashBgTool` / `inlineReadTool` / `subagentTool` 三类实现；`go build ./...`（TC-K1-1） | — |
| **K-1b** | 命名确认（用户裁定 2026-09-26）：查看动作 = `Status`；载荷出参 = `[]byte`；**`Done` 必须在 `JobTool` 下** | 设计文档 §A.1 | 契约里能看见 `Done`（不是内部钩子）；四个动作各有一个名字 | — |
| **K-2** | 行 schema 收敛：`Kind` / `Summary`（≤512B）/ `Notified` / `Index` 加到作业记录与投影 DTO | `application/contract/dto/async_run.go:20-45`、`seelebridge/tools/async_probe.go:24-40` | 字段搬运测试（`runtime_async.go:asyncRunRecordFrom` 逐字段，TC-K2-1）；既有 `work_table_async_test.go` 全绿 | — |
| **K-3** | bash 工具族注册：`bash`（保留）/ `bash_read`（只读）/ `bash_bg`（后台受管），并填权限组 | `seelebridge/tools/router.go:92-105`、`seelebridge/tools/permission_policy.go:90-115` | 路由断言：`bash`/`bash_bg` → `rw`，`bash_read` → `ro`（TC-K3-1，扩展 `async_exec_test.go:530` 的既有用例）；子代理对 `bash_read` 可见、对 `bash_bg` 需位齐 | K-0 |
| **K-4** | `ClassifyCommand` 服务端守卫（纯函数 + 表驱动）并接进 `bash_read` handler | 新增 `seelebridge/security/command_class.go`；`router.go` 的 `bash_read` 分支 | 表驱动：写命令（`git commit`、`rm -rf`、`echo x > f`、`sudo …`）**必须被拒**；只读命令（`ls`、`git status`）通过（TC-K4-1..TC-K4-4） | K-3 |
| **K-5** | 回填规范落地：完成行带 `Summary` 进打点块 + 五条约束 | `application/core/work_table_async.go:46-100,148-170`、`seelebridge/tools/async_exec.go:336-366` | 有界：注入 100 个完成作业 → 投影行数 ≤ `asyncWorkMaxRows`、块行数 ≤ `workTableTraceMaxLines`（TC-K5-1）；幂等：重复 `finish` 不产生第二次回填（TC-K5-2） | K-2 |
| **K-6** | `done` 双入口：运行体系被动 + 模型侧主动，同一状态机 | `async_exec.go:336`（被动）、新增 `Manage(OpDone)` 入口 | 状态机断言：两入口到同一终态，且**每个 handle 只迁移一次**（TC-K6-1） | K-1 |
| **K-7** | 契约落实的文档同步（设计文档 §A、本文、测试用例） | `docs/` | 三份文档交叉引用可解析（无死链）；`grep` 无自相矛盾表述；模块 README（`seelebridge/tools`、`seelebridge/security`）同步 | — |

---

## P1：契约下的实现

| ID | 动作 | 落点 | 判据 | 依赖 |
|---|---|---|---|---|
| **L-1** | 后台 bash 换壳到 `Add`/`Manage`（`bash_bg`）：**不动状态机**，只换入口与回执面 | `seelebridge/tools/async_run.go:151,178,222`、`async_tools.go`、`async_exec.go` | 派发回执与旧 `bash(background=true)` **逐字段等价**（TC-L1-1）；`async_exec_test.go` / `async_kill_test.go` / `async_probe_test.go` 全绿 | K-1, K-3 |
| **L-2** | **替代旧取回工具**：`jobManager.Fetch` 承接消费式取回（工具名收敛进 `job_manage`） | `async_tools.go`（删除）、`job_tools.go`、`router.go`、`permission_policy.go` | 代码与配置里旧名零残留（`TestRetiredJobToolNamesLeaveNoResidue`）；**消费式游标**回归断言（TC-L2-1）；Status 不推进游标（TC-L2-2） | L-1 |
| **L-3** | 批量读作业化（`Kind=inline`）：一次调用派发 N 个作业，回执含 N 个 handle | `job_tools.go` 的 `read_batch`；`async_exec.go` 状态机复用 | 派发即返回（`TestReadBatchDispatchesJobsAndReturnsImmediately`）；**N 文件并行读 vs 串行的墙钟对比**（人工验收 AA-1，**P2 门的判据数据**） | L-1 |
| **L-4** | 旧终止工具并入 `jobManager.Kill`，并**泛化到 subagent** | `job_tools.go`、`job_subagent.go`、`seelebridge/fork/` | kill 后**已产出内容不丢**（`TestJobKillPreservesProducedContent`）；kill 测试全绿 | L-1 |
| **L-5** | subagent 纳入契约：`fork_subagents{async:true}` 作业化派发 + `observe`/`kill`/`done` | `fork/types.go`（`SubagentJobs` 端口）、`fork/tool.go`（`dispatchJobs`）、`runtime_plan.go`（适配器）、`job_subagent.go` | 模型面可 observe / kill / fetch / done（`TestSubagentJobContract`、`TestForkSubagentsAsyncRunsAsJobs`）；阻塞模式与 merge-back/summary 行为不变（`fork_*` 测试全绿） | L-4 |
| **L-6** | 提示词 / 配置 / 文档迁移（五个名字 + 旧名下线 + 能力开关语义） | `config/seelex.yaml`、`seelexctx/limits.go`、`application/contract/ports.go`、`seelebridge/tools/README.md`、`seelebridge/security/README.md` | 工具描述里 `bash_read` 明写只允许只读命令；配置与注释与新名字一致（残留由同一条用例守住） | K-3, L-2 |
| **L-7** | 迁移面清点：把旧取回工具的 7 个面**逐条**核过（注册/权限/描述/开关/测试/文档/别名） | 设计文档 §B.2 表 | 7 项逐条勾掉（见下方「落地记录」）；旧名残留由仓库级用例机械守住（TC-L7-1） | L-2 |

---

### 落地记录（2026-09-26，P0 + P1 一次完成）

实跑口径（本次交付，Windows 本机）：

```text
gofmt -l .                                  # 仅 vendor/ 与 tmp/pidprobe 有既有噪声
go build ./...                              # exit 0
go build -tags "gui,desktop,production" ./...   # 见 §12 基线（GUI 标签由 CI 覆盖）
go vet ./...                                # exit 0
go test ./... -count=1 -timeout=300s        # 全绿（含原先那条 vendor README 环境红，见下）
go vet -tags asynclive .                    # 活体探针（tag 门后）编译通过
go vet -tags manualsmoke .                  # A/B 活体（tag 门后）编译通过

# 真实 API 活体（真账号、真模型；派发 / 观察 / 终止 / 取回四步都由模型自己发起）
$env:SEELEX_SMOKE_ACCOUNTS = (Resolve-Path config/accounts.yaml)
go test -tags manualsmoke . -run TestManualSmokeRealAccountJobContract -count=1 -v -timeout=10m
#   --- PASS: TestManualSmokeRealAccountJobContract (5.59s)
#   bash_bg → accepted(handle=a1) → observe(process/running/末行) → kill → fetch(killed/137 + tick-1…)
```

| ID | 状态 | 落点 / 证据 |
|---|---|---|
| K-0 | ✅ | `seelebridge/tools/tool_meta.go`（`DeclaredToolMeta`，事实源 = 路由组表）、`registry_state.go`（`AddInline` 填 Meta + `UndeclaredTools`）、`permission_policy.go`（`sanitizeMeta`：声明的簇不在本次配置里就作废，防静默放权）。判据：`seelebridge/tool_meta_declaration_test.go`、根包 `tool_meta_declaration_test.go`（源码扫描）、`TestBuiltinToolsDeclareGroupsAndMetas` |
| K-1 | ✅ | `job_contract.go`：`JobTool{Add,Status,Fetch,Kill,Done}` 全 `[]byte`；`jobManager` 是四个管理动作的唯一实现；`bashBgTool` / `inlineReadTool` / `subagentTool` 三处编译期断言 |
| K-1b | ✅ | 用户裁定：`Status`（查看）/`[]byte`（出参）/`Done` 必须在 `JobTool` 下 |
| K-2 | ✅ | `asyncRun.{kind,index,summary,lines,notified}` → `AsyncRunInfo` → `dto.AsyncRunRecord`（三跳逐字段搬运，`TestAsyncRunRecordMappingCarriesEveryColumn` 钉住不漏列） |
| K-3 | ✅ | `router.go` 注册 `bash` / `bash_read` / `bash_bg` / `read_batch` / `job_manage`；`permission_policy.go` 分组（rw/rw/ro/ro/rw）+ `bash_bg` 继承 bash 的规则口径；`policyArgsFor` 覆盖 `bash_bg`。判据：`TestBashFamilyRoutingTable`、`TestJobContractToolFaceRoutingTable` |
| K-4 | ✅ | `seelebridge/security/command_class.go`（纯函数 + 保守默认：分类失败一律按写处理）+ `command_class_test.go`（含"模型主张不构成授权依据"一条）；接线在 `router.go:scopedBashRead`（判定 → 拒绝并指向 `bash`） |
| K-5 | ✅ | `finish` 幂等迁移 + 终态算一次有界摘要（≤512B，**按字节**截断且落在 UTF-8 边界）+ `notified` 位；core 侧 `asyncTraceLines` 把**完成行带摘要**回填进打点块，并在超限时补一行汇总。判据：`TestJobSummaryIsBounded`、`TestAsyncTraceLinesCarryNoPathsOrLogContent`、`TestAsyncBackfillStaysBounded` |
| K-6 | ✅ | 两个入口：运行体系 `CompleteJob`（迁移终态）/ 模型侧 `job_manage(op=done)`（销项）。守卫：`finish`/`CompleteJob` 均"状态非 running 即返回"（避免了二次 `close(run.done)` panic）；在途 `done` 显式报错要求 `kill`。判据：`TestJobTerminalTransitionHappensOnce`、`TestJobManageEntryRejectsInvalidCalls`、`TestSubagentJobContract` |
| K-7 | ✅ | 本文 + `docs/tool_concurrency_design.md`（§A.1 契约已按裁定改写、§A.5/§B.5 判据表换成本仓真实用例名）+ `docs/subprocess_contract_testcases.md`；模块 README：`seelebridge/tools/README.md`、`seelebridge/security/README.md` |
| L-1 | ✅ | `bash_bg` = `Add` 的工具面；回执仍由**同一条** `renderJobAccepted` 渲染（逐字段等价有结构保证），`TestJobAcceptedReceiptMatchesLegacyShape` 钉住字段 |
| L-2 | ✅ | `async_tools.go` 删除，能力收进 `job_tools.go` 的 `job_manage`（`op=fetch` 消费式）；`TestRetiredJobToolNamesLeaveNoResidue` 机械守住"代码与配置零残留" |
| L-3 | ✅ | `read_batch`（`Kind=inline`，一次派发 N 个句柄，行窗口 `start_line`/`end_line` 真生效）；`TestReadBatchDispatchesJobsAndReturnsImmediately` |
| L-4 | ✅ | `jobManager.Kill` 统一进程树终止与 ctx 取消；`TestJobKillPreservesProducedContent`、`TestAsyncKillTerminatesProcessTree`（`exit=137` 注记仍可 fetch） |
| L-5 | ✅ | `fork_subagents{async:true}`：`SubagentJobs` 端口（fork 不 import tools）、`dispatchJobs`（派发即返回 + 后台编排 + 逐句柄正文/终态）、`subagentJobsAdapter`（延迟解析工具路由）。判据：`TestSubagentJobContract`、`TestForkSubagentsAsyncRunsAsJobs` |
| L-6 | ✅ | `config/seelex.yaml` 注释、`seelexctx/limits.go`、`application/contract/ports.go`、`seelebridge/runtime_tools.go`、`router.go` 的 Deps 注释、两处模块 README 全部换到新名字与"作业面"语义 |
| L-7 | ✅ | 7 个面：① 注册（`router.go` 只注册新名字）② 权限组（`permission_policy.go` 删旧名、加五名字）③ 描述（`job_tools.go` 的 schema/描述）④ 注册开关（`allowBackground` 门控三个作业工具 + fork async 拒绝）⑤ 测试（`async_*_test.go` 全部迁移；两个 tag 门后的活体探针也迁移）⑥ 文档（两处模块 README + 三份设计文档 + CHANGELOG）⑦ 别名（**不留**：旧名出现在任何未被标注为迁移说明的位置都会被 `TestRetiredJobToolNamesLeaveNoResidue` 拦下） |

**顺带修掉的既有环境红（Step -1）**：`e2e.TestEveryGoPackageDirectoryHasReadme` 原先在
每一个 `vendor/` 子包上失败（vendor 是第三方只读副本，不受模块 README 约定约束），
已在 `e2e/layout_test.go` 的 skip 表里补上 `vendor`；该包测试现在全绿，真正的红不再被淹没。

**仍然留给 P2 的两件事**（不在 P0/P1 判据内）：① 墙钟收益数据（AA-1，P2 门的判据）；
② 把句柄注进子代理的节点提示词，让它能自己调 `job_manage(op=done)`（现在由运行体系
在节点收尾时以 `CompleteJob` 代它落地）。

---

## P2：契约进 seele + loop 并发（**有门**）

| ID | 动作 | 落点 | 判据 | 依赖 |
|---|---|---|---|---|
| **S-0** | **门**：证明"结果晚一轮不可接受" | L-3 的墙钟数据 + 一轮真实任务复盘 | 只有门通过才启动 S-1..S-4 | L-3 |
| **S-1** | 契约模型同步进 seele（进程型工具成为框架一等范式） | Seele 仓 `tools/`、`session/`；`go.mod` 升 tag + `go mod vendor` | 见 §10.0 基座流程（**先定基座**） | S-0 |
| **S-2** | loop 内并发工具调用（并发派发 + 按 wire 下标保序回填） | `vendor/.../Seele/session/loop.go:327-415` | I-1..I-8 全绿（本文 §9.1 的 1–11 号用例） | S-1, S-3 |
| **S-3** | `ToolCallInfo` 补 `ID` / `Index` / `BatchSize` | `Seele/session/hooks.go:24-31`、`loop.go:332,375` | 同批同参调用**不再错配**（本文 §9.1 的 14 号用例） | S-1 |
| **S-4** | 批量前后沿（hook 单 goroutine、按 Index 序触发） | `Seele/session/loop.go`、`application/core/tool_hooks.go:414-450` | hook 内并发探测恒为 1（本文 §9.1 的 9 号用例） | S-3 |

> **独立立项（不在这三张表内）**：子代理**进程化**（OS 进程隔离）。它不解决 P0/P1 的任何缺口，且会同时引入生命周期 + 序列化 + 数十个 `subagent_*` 测试面的改动。

---

## 与旧章节的映射（本文其余部分是**实现级现状锚点**，按阶段归位）

| 本文位置 | 归属阶段 | v3 下的状态 |
|---|---|---|
| §0 A1–A12 现状锚点 | **全阶段** | **全部有效**，可直接复核 |
| §1 三个纯数据字段（身份/顺序/批大小）→ seele | **P2** | 归入 S-3 |
| §2 loop 重构（`ExecuteToolCalls`） | **P2** | 归入 S-2 |
| §3.4 / §5 权限与超时矩阵（A7/A10/A11） | **全阶段** | **仍然有效且更紧急**：K-4 的守卫判定结果喂给同一套 `Groups`/权限门，不吃并发槽、不改超时矩阵 |
| §4.2 应用侧改动、§8 投影详设 | **P0/P1** | 仍然有效；投影语义按设计文档 §A.3 收敛（加 `Kind`/`State`/有界 `Summary`/`Notified`，全文不入投影） |
| §9 测试用例清单 | **P2 为主** | P0/P1 的用例已迁到 `docs/subprocess_contract_testcases.md`；本文 §9.1 的 1–11/14 号仍是 P2 的主清单 |
| §10 双仓分工 | **P2** | 基座流程（§10.0）与 seele 改动（§10.1）在 P2 启用 |
| §11 锚点校正、§12 基线实跑 | **全阶段** | 仍然有效（含 Step -1 的既有红） |

> **v3 再修正（读下文旧表述时请套用）**：
> - 表中所有"**降级为可选**"一律读作"**归入 P2**"；
> - 所有"**seele 侧改动 = 0**"一律读作"**仅 P0/P1 阶段为 0**"（P2 明确要改，见设计文档 §C）；
> - 所有"**新增 `async_probe`**"一律读作"**`Manage(OpObserve)`**"，"**保留 `async_output`**"一律读作"**由 `Manage(OpFetch)` 替代**"；
> - **旧编号重定向**：旧 `P0-1..P0-5` → v3 的 **`K-0`（A8 填表）/ `K-1`（interface）/ `K-3`（bash 工具族）/ `K-4`（守卫）/ `K-5`（回填）**；旧 `P0-3`（批量读）→ **`L-3`**；旧 `P1-1..P1-3` → **`L-1`..`L-3`**；旧 `Phase 0..5` → v3 的 **P0/P1/P2**（进程化独立立项）。

---

## v2 修订前置说明（2026-09-26 之后，历史留档）

`docs/tool_concurrency_design.md` 已切换主线到 **§10「子进程调用系契约」**：并发来自**后台作业**，loop 保持串行。本文的**现状锚点（§0 A1–A12、§11 校正表）全部仍然有效**，但**结论层**按下表调整：

| 本文位置 | v2 下的状态 | 说明 |
|---|---|---|
| §1 三个纯数据字段（身份/顺序/批大小）→ seele | **降级为可选** | 仅在 D-1 取"loop 内并发"时才需要；v2 下 loop 串行，hook 仍单 goroutine、按 wire 序，A1/A4/A5 的静默错配**不会被触发** |
| §2 loop 重构（§2.1 的 `ExecuteToolCalls` 接口） | **降级为可选** | 同上；v2 不需要 loop 内分派 |
| §3.4 / §5 权限与超时（A7/A10/A11） | **仍然有效且更紧急** | v2 新增 P0-5（bash 读写服务端分类），其判定结果**喂给同一套 Groups/权限门**，不吃并发槽、不改超时矩阵 |
| §4.2 应用侧改动、§8 投影 | **仍然有效，但语义按 §10.3 收敛** | 投影从"只投在途"扩为"投在途 + 待取回完成行"，**字段加 `Kind`/`State`/有界 `Summary`**；全文不入投影 |
| §10.2 seelex 改动清单 | **有效，另加 3 项** | **P0-4**（作业契约命名，零语义变更）、**P0-5**（bash 服务端分类）、**P1-1/P1-2/P1-3**（`kind` 字段 + `inline` 作业 + 批量读作业化） |

> ⚠️ **A8（`AddInline` 的 `Meta` 留 nil）在 v3 下依然是硬前置**（打点 **K-0**）——权限与分类都靠 `Groups`，与并发机制无关。
>
> ⚠️ **A1（`ToolCallInfo` 无 ID）不属于 P0/P1**：P0/P1 下 loop 串行 ⇒ hook 仍单 goroutine、按 wire 序触发 ⇒ A4/A5 的静默错配不会被触发；它**归入 P2 的 S-3**（届时是硬前置，详见设计文档 §C）。

---

## 0. 现状锚点（已核实，设计的地基）

| # | 事实 | 位置 | 对设计的影响 |
|---|---|---|---|
| A1 | `ToolCallInfo` 只有 `Turn/Name/Arguments/Result/Error/Duration`，**无 ID** | `vendor/.../Seele/session/hooks.go:24-31` | 框架必须加一个字段 |
| A2 | `loop.go` 串行 for，每个调用即时 `OnToolStart`→dispatch→`OnToolComplete`→append | `vendor/.../Seele/session/loop.go:327-415` | 待重构的唯一函数 |
| A3 | `TypedMessage.ToolCallID` 已填 `tc.ID`，`tc.ID` 已在 telemetry 用两处 | `loop.go:403`、`loop.go:351,365` | **wire 侧 ID 已就位** |
| A4 | `ToolHookBridge` **自己合成 ID** `tool-%d`，与 wire `tc.ID` **是两个 ID 空间** | `application/core/tool_hooks.go:443-446` | 身份必须归一 |
| A5 | start↔complete 配对键 = `(turn, name, args)` 三元组，值是 **FIFO 队列** | `tool_hooks.go:414-441,448-450` | **并行下的静默错配就在这里** |
| A6 | application 侧**已全部按 ID 寻址**：`EnsureToolCallTranscriptLocked(…,id,…)`、`view.Conversation[i].Tool.ID == id`、`Tool{ID, Status:"running"}`→按 ID 回写 `Status/Result/Error/Duration` | `tool_hooks.go:32,140,170,45,48,171` | **application 侧原生支持 N 个并行 running**，R-3 需下调 |
| A7 | 注册表只有一个全局 `callTimeout`，`context.WithTimeout` 包住**整条装饰链（含权限门）** | `vendor/.../tools/tools.go:194-200,354-373` | **审批等待在吃执行预算**，见 §5.3 |
| A8 | 生产 `RegisterTool` 不带 meta，`AddInline` 的 `Meta` 留 nil | `seelebridge/runtime_tools.go:264`、`tools/registry_state.go:48-64` | §4 的硬前置 |
| A9 | 中间件装配：`WithMiddleware(诊断)` 内层、`WithMetaMiddleware(事件, 权限门)` 外层 | `tools/registry_state.go:29-35` | 并发槽插在 plain 层末位 |
| A10 | bash 自带 `scopedToolTimeout`：显式 > `tool_call_timeout` > 30min | `tools/router.go:623-636` | §5 的第三层 |
| A11 | 默认值：`tool_call_timeout=1800s`、`approval_timeout=600s`；且 `tool_call_timeout` 的 `0` = **显式无限制**（与 `approval_timeout` 的 `0`=补默认 语义相反） | `seelexctx/limits.go:150-151,208-212` | §5.1 的非对称必须先修 |
| A12 | 框架在 vendor 内（`vendor/github.com/RedHuang-0622/Seele`），非 `replace` 本地路径 | `go.mod:17` | 框架改动需升版本或 vendor patch |

### A5 展开：并行下唯一会「错了也看不出来」的地方

```go
// tool_hooks.go:448
func toolHookKey(info session.ToolCallInfo) string {
    return fmt.Sprintf("%d\x00%s\x00%s", info.Turn, info.Name, info.Arguments)
}
// tool_hooks.go:414-423
func (bridge *ToolHookBridge) beginTool(info session.ToolCallInfo) (*Service, string) {
    id := bridge.nextToolIDLocked()                  // tool-%d，合成本地 ID
    key := toolHookKey(info)
    bridge.pending[key] = append(bridge.pending[key], id)   // FIFO
    return bridge.service, id
}
```

配对正确性**只依赖**「两条边都按 wire 序、单 goroutine 迭代」这条隐式不变量。同批内两个调用 `(turn,name,args)` 相同时（同命令两次 bash、同路径两次 read），配对靠 FIFO 顺序对齐——一旦有一段以完成序迭代，就**静默错配**，症状只是「错误信息挂到了另一个工具行」。

**这不是并行才有的 bug，是并行会把它从「不可能触发」变成「容易触发」。**

---

## 1. 接口契约

### 1.1 包布局与依赖方向

```
application/toolpolicy/            ← 纯声明：策略（无并发原语）
        ▲
        │ 只被上层依赖，不依赖任何执行实现
        │
seelebridge/dispatch/              ← 分派与回填（goroutine/WaitGroup）
seelebridge/tools/                 ← 并发槽中间件（信号量/锁/排队）
```

依赖倒置落点：**`application` 描述「什么可并行、按什么键隔离」，不知道「怎么并行」。** 换实现（goroutine → 进程池 → 远程）不动 application 一行。

### 1.2 `application/toolpolicy` —— 策略端口

```go
// Package toolpolicy 从既有的路由组（dto.PermissionGroup*）派生并发策略。
// 它是**纯函数包**：不持有状态、不引并发原语、可被任意层依赖。
package toolpolicy

// Class 是并发隔离级别。
type Class uint8

const (
    ClassSerialGlobal  Class = iota // 全局串行（adm/ctl/rw_desktop）
    ClassSerialSession              // 会话内串行（rw_session）
    ClassSerialKey                  // 按资源键串行（rw，键 = 规范化路径）
    ClassParallel                   // 可并行（ro）
)

// Serial 报告该类是否需要排队。
func (c Class) Serial() bool { return c != ClassParallel }

// Policy 是一次调用的并发策略。
type Policy struct {
    Class Class
    // Key 仅在 Class == ClassSerialKey 时非空，标识隔离维度
    // （如项目内相对路径；空 = 退化为 ClassSerialGlobal）。
    Key string
}

// PolicyFor 从工具携带的路由组派生策略。
//
// 优先级：先扫串行组、再扫并行组——同一工具同时属于 ro 与 ctl 时以串行为准
// （安全侧优先）。未分类（nil/空）= ClassSerialGlobal：这是**保守默认**，
// 因为生产路径的 ToolMeta 目前全为空（A8），默认并行会在上线第一天全线并行。
func PolicyFor(groups []string) Class

// KeyFor 从工具名与原始参数 JSON 派生资源键。
// 只对 ClassSerialKey 有意义；返回空串由调用方退化为全局串行。
func KeyFor(toolName, argsJSON string) string

// Resolve 是 PolicyFor + KeyFor 的组合入口（调用方只需这一个函数）。
func Resolve(toolName string, groups []string, argsJSON string) Policy
```

**为什么优先级是「先串行后并行」**：`ro` + `ctl` 同时出现时，并行的前提（无共享状态）已被 `ctl` 否定。取串行不会丢正确性，取并行会。

### 1.3 `seelebridge/dispatch` —— 分派与回填端口

```go
// Package dispatch 负责一轮 ReAct 里 N 个 tool_call 的并发派发与保序回填。
package dispatch

// Call 是一个待派发的工具调用。Index 即 wire 顺序，是唯一稳定排序键。
type Call struct {
    Index     int    // assistantMsg.ToolCalls 的下标 —— 顺序
    CallID    string // provider 的 tool_call id —— 身份
    Name      string
    Arguments string
}

// Outcome 是一个调用的结果，永远按 Index 定址。
type Outcome struct {
    Index    int
    CallID   string
    Result   string
    Err      error
    Duration time.Duration
}

// Slot 是并发槽的获取结果。
type Slot interface {
    Release()
}

// Acquirer 取槽。ok=false 表示取槽失败（排队超时/上下文取消），
// **不得静默降级为串行执行**——必须让调用方看见失败原因（见 §5.4）。
type Acquirer interface {
    Acquire(ctx context.Context, p toolpolicy.Policy) (Slot, bool)
}

// Runner 是「真正跑一个调用」的端口；见 §1.5 的输入侧倒置。
type Runner interface {
    Run(ctx context.Context, call Call) (string, error)
}

// Dispatcher 并发派发并保序回填。
//
// 契约（必须由测试锁死，对应设计文档 I-1..I-6）：
//   - 返回的 []Outcome 长度 == len(calls)，且 Outcomes[i].Index == i
//   - 返回顺序 == 传入顺序（wire 序），与完成顺序无关
//   - 同 Policy.Key 的调用在时间上不重叠
//   - 单个调用失败不改变其它调用在切片中的位置
type Dispatcher interface {
    Dispatch(ctx context.Context, calls []Call, policyOf func(Call) toolpolicy.Policy) []Outcome
}

// Edges 描述批量前后沿：hook 只在单 goroutine、按 Index 序触发。
// 这是既有契约（hooks.go：回调在循环内同步调用）的保持方式。
type Edges interface {
    OnBatchStart(calls []Call)  // 全部 start，按 Index 序，派发之前
    OnBatchEnd(outcomes []Outcome) // 全部 complete，按 Index 序，等齐之后
}
```

### 1.4 框架侧最小 patch（唯一必须改 vendor 的地方）

```go
// vendor/.../Seele/session/hooks.go:22
type ToolCallInfo struct {
    Turn      int
    Name      string
    Arguments string
    Result    string
    Error     error
    Duration  time.Duration
    ID        string   // ← 新增：provider 的 tool_call id（wire 侧身份）
    Index     int      // ← 新增：本条在 assistantMsg.ToolCalls 中的下标（顺序）
    BatchSize int      // ← 新增：本批调用总数（供 UI 表达"N 个并行"）
}
```

loop 侧两处填充（`loop.go:332` 与 `loop.go:375`）：

```go
rl.hooks.OnToolStart(ctx, ToolCallInfo{
    Turn: loop, Name: tc.Function.Name, Arguments: tc.Function.Arguments,
    ID: tc.ID, Index: i, BatchSize: len(assistantMsg.ToolCalls),   // ← 新增
})
```

**为什么必须进框架**：设想过不改框架、让见侧从 `ctx` 取 ID——但 `loop.go:332` 传的是**批共用**的 `ctx`，不是 per-tool ctx，也没有 per-tool 挂载点。所以这是不可回避的一行。

**改动成本**：`ToolCallInfo` 是纯数据 struct，新增字段**对既有调用方零影响**（Go 结构体字面量按名初始化）。`A12` 说明 Seele 与 seelex 同作者维护，属正常版本迭代。

### 1.5 并发槽端口（`seelebridge/tools`）

```go
// SlotMiddleware 是 plain 层末位中间件：读 ToolMeta 派生策略 → 取槽 → 放行 → 还槽。
//
// 为什么必须在 plain 层（而非 meta 层）：见 A9，meta 在权限门之外。
// 若把取槽放在权限门之外，一次等人点头几分钟的审批会一直占着槽位。
func SlotMiddleware(acquirer dispatch.Acquirer, groupsOf func(toolName string) []string) frameworktools.Middleware

// SemaphoreAcquirer 是 Acquirer 的默认实现：并行类走计数信号量，
// 串行类走 per-Key 互斥队列。
type SemaphoreAcquirer struct {
    MaxParallel int                        // 并行桶上限
    WaitTimeout time.Duration              // 排队超时（§5.2 第 3 层）
    mu          sync.Mutex
    sem         chan struct{}
    queues      map[string]chan struct{}   // Policy.Key → 队列
}
func (a *SemaphoreAcquirer) Acquire(ctx context.Context, p toolpolicy.Policy) (dispatch.Slot, bool)
```

### 1.6 契约总表

| 端口 | 定义位置 | 实现位置 | 依赖方向 |
|---|---|---|---|
| `toolpolicy.PolicyFor/Resolve` | `application/toolpolicy` | 同（纯函数） | 不依赖任何实现 |
| `dispatch.Dispatcher` | `seelebridge/dispatch` | 同 | 依赖 `Runner`/`Acquirer` 接口 |
| `dispatch.Acquirer` | `seelebridge/dispatch` | `seelebridge/tools.SemaphoreAcquirer` | 倒置 |
| `dispatch.Runner` | `seelebridge/dispatch` | loop 传入的闭包（`rl.agent.Dispatch`） | 倒置：dispatcher 不知道 registry |
| `dispatch.Edges` | `seelebridge/dispatch` | `application/core.ToolHookBridge` | 倒置 |

---

## 2. 重构后的 loop

替换 `loop.go:327-415`（现状见 A2）。**注意三个「不动」**：不动 LLM 调用段、不动 `handleContextEvent`、不动 `OnIterationComplete`。

> **基座警告（§10.0）**：若基座是本地 `session/` 重构（`rl.state.append` + `drain()` + 事件自带 History），本片段要改三处（B1/B2/B3）。

```go
// ─── 1) 构建 Call 列表（wire 序 = Index 序）─────────────────────────
calls := make([]dispatch.Call, 0, len(assistantMsg.ToolCalls))
for i, tc := range assistantMsg.ToolCalls {
    if rl.agent == nil {
        return "", fmt.Errorf("session: model requested tool %q but no tool runtime is configured", tc.Function.Name)
    }
    calls = append(calls, dispatch.Call{
        Index: i, CallID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments,
    })
}

// ─── 2) 前后沿：全部 OnToolStart，按 Index 序，单 goroutine，派发之前 ──
// 契约来源：hooks.go「回调在循环内同步调用」。并行化不改变这条。
if rl.hooks != nil && rl.hooks.OnToolStart != nil {
    for _, c := range calls {
        rl.hooks.OnToolStart(ctx, ToolCallInfo{
            Turn: loop, Name: c.Name, Arguments: c.Arguments,
            ID: c.CallID, Index: c.Index, BatchSize: len(calls),
        })
    }
}

// ─── 3) 并发派发（Slot 中间件在 registry 链内取槽，这里不再取）────────
//   policyOf 决定并行桶 / 串行桶（组由 registry 侧 ToolMeta 提供）
outcomes := rl.dispatcher.Dispatch(ctx, calls, func(c dispatch.Call) toolpolicy.Policy {
    return toolpolicy.Resolve(c.Name, rl.groupsOf(c.Name), c.Arguments)
})

// ─── 4) 后沿 + 保序回填：全部 OnToolComplete，按 Index 序，等齐之后 ────
for _, o := range outcomes {
    // 4a. span / telemetry（现状 loop.go:333-368 的等价物，按 Index 定址）
    rl.finishToolSpan(rootCtx, o)

    if rl.hooks != nil && rl.hooks.OnToolComplete != nil {
        rl.hooks.OnToolComplete(ctx, ToolCallInfo{
            Turn: loop, Name: o.Name, Arguments: o.Arguments,
            Result: o.Result, Error: o.Err, Duration: o.Duration,
            ID: o.CallID, Index: o.Index, BatchSize: len(calls),
        })
    }

    // 4b. 结果的规范化（现状 loop.go:377-397，语义不变）
    out := o.Result
    if o.Err != nil {
        out = fmt.Sprintf(`{"error": %q}`, o.Err.Error())
    }
    content := out
    if rl.toolResultProcessor != nil {
        view, processErr := rl.toolResultProcessor.Process(ctx, seelectx.ToolResult{
            CallID: o.CallID, Name: o.Name, Arguments: o.Arguments, Raw: out, Err: o.Err,
        })
        if processErr != nil {
            return "", fmt.Errorf("session: process tool result %q: %w", o.Name, processErr)
        }
        content = view.Content
    } else {
        content = truncateResult(out, rl.cfg.MaxToolResultChars)
    }

    // 4c. 回填 history —— **按 wire 序**，与完成序无关。这是「看起来串行」的来源。
    rl.history = append(rl.history, types.Message{
        Role: "tool", ToolCallID: o.CallID, Name: o.Name, Content: &content,
    })

    toolResult := seelectx.ToolResult{
        CallID: o.CallID, Name: o.Name, Arguments: o.Arguments, Raw: out, Err: o.Err,
    }
    if err := rl.handleContextEvent(ctx, seelectx.ContextEvent{
        Kind: seelectx.ContextAfterTool, Turn: loop, Query: userInput,
        History: rl.History(), Tool: &toolResult,
    }); err != nil {
        return "", err
    }
}

// ─── 5) OnIterationComplete：完全不动（loop.go:413）────────────────
```

### 2.1 三个必须写进注释的约束

1. **`OnToolStart` 提前触发的代价**：UI 看到的「运行中」跨度 = 整批跨度。`ToolCallInfo.Duration` 仍填**真实单工具耗时**（§1.4 的 `Duration` 在 complete 事件里），所以只丢可视化跨度，不丢数据。
2. **回填循环必须按 `outcomes` 的切片序**，而 `Dispatcher` 已保证它就是 Index 序。**不要在回填里再排序**——那会引入第二处顺序事实源。
3. **`handleContextEvent` 仍是每调用一次、串行**。它内部会走 `publishHistory` + 上下文控制器，不并发调用的约束没变。

---

## 3. 不同权限 → 不同责任链

### 3.1 现状：责任链已经存在，是**三轴正交**的

```
事件(meta)  →  权限门(meta)  →  诊断(plain)  →  handler
              ↑ registry_state.go:22-39（A9）

权限门内部（frameworktools/permission.Gate.Decide）：
  ① Enforcer（BitEnforcer）—— PermissionGate.Enforce            registry_state.go:392-397
       · root + 会话 full 档            → ActionAllow, true（短路放行）
       · 其余 → enforceClass：sub（位齐放行/位缺拒绝）、员工（位齐交回/位缺提权）
  ② checker 规则表（按「主体类 + 会话档位」选表）                registry_state.go:310-318
  ③ ask 分支 → ApprovalHandler（人）
       · sub 主体：Approval = nil，**无可问的人** → 框架直接返回拒绝   registry_state.go:377-379
       · Timeout = approval_timeout，DenyWithoutPrompt = true
```

三条轴**互不干扰**，这是既有设计的要点：

| 轴 | 取值 | 决定什么 | 不决定什么 |
|---|---|---|---|
| **主体类** | `root` / `sub` / `emp_<角色>` | 有没有位、能不能提权、有没有人类可问 | 不决定档位 |
| **会话档位** | `full` / `manual` / `edit` / `auto` | root 的**问/放**（`ApplyTier` 剪掉若干 ask） | **绝不改变任何主体的「有没有位」** |
| **路由组 × 位** | 6 组 × (r/w/x) | 该工具需要什么、主体被授予什么 | 不决定问不问 |

### 3.2 【提议】新增第 4 环，并把「责任链」显式化

```
事件(meta) → 权限门(meta) → 诊断(plain) → 并发槽(plain) → handler
                                          ↑ 新增（A9 推出必须在权限门之内）
```

**位置论证**：取槽必须在审批**之后**。否则一次等人点头 10 分钟的审批会占着并发槽，把并行能力浪费在等待上。

### 3.3 责任链矩阵（按权限位 × 主体类）

| # | 主体类 | 档位 | 请求的位 | 责任链 | 人参与 | 并发槽 |
|---|---|---|---|---|---|---|
| C1 | `root` | `full` | 任意 | 事件 → **短路放行** → 诊断 → 槽 → handler | ❌ | 按 Groups |
| C2 | `root` | `edit`/`auto` | 命中 allow 规则 | 事件 → 规则 allow → 诊断 → 槽 → handler | ❌ | 按 Groups |
| C3 | `root` | `manual`/`edit`/`auto` | 无命中规则 | 事件 → **ask** → 审批面板 →(点放行)→ 槽 → handler | ✅ | **取槽在点之后** |
| C4 | `root` | 任意 | 命中 deny 规则 | 事件 → **直接拒绝**（`DenyWithoutPrompt`），不进面板 | ❌ | 不取槽 |
| C5 | `sub`（子代理） | 任意（档位不吃） | 位齐 | 事件 → 放行 → 诊断 → 槽 → handler | ❌ | 按 Groups |
| C6 | `sub` | 任意 | 位缺 | 事件 → **直接拒绝**（`Approval=nil`，无人可问） | ❌ | 不取槽 |
| C7 | `emp_<角色>` | 由 `ToolsPolicy` 派生 | 位齐 | 事件 → **交回框架判定** → 诊断 → 槽 → handler | 视规则 | 按 Groups |
| C8 | `emp_<角色>` | 同上 | 位缺（越权） | 事件 → **执行选择页面提权**，呈现归属折算到宿主主会话 | ✅ | **取槽在点之后** |

**C6 是最容易被并行化打破的一条**：子代理「违权直接拒绝，绝不挂起等一个不会有人回应嘅选择页面」——这条语义必须保持；并发槽若放在权限门之外，C6 会变成「先占槽再被拒」，白占槽。

### 3.4 并发策略与权限位的关系：**正交，不要交叉**

**提议**：并发策略**只从 `Groups` 派生，不从位派生**。

| 依赖 | 对不对 | 理由 |
|---|---|---|
| 并发 ← `Groups` | ✅ | 组表达的是**资源**（哪块共享状态），并发隔离的本质就是资源隔离 |
| 并发 ← 位（r/w/x） | ❌ | 位表达的是**动作许可**。`ro` 只给 r 位、`rw` 给 w 位，但**两组的隔离需求恰恰相反**（ro 可并行、rw 按路径）——位分不出这个差别 |

这条避免了第二事实源：**资源轴只有 `Groups` 一份。**

---

## 4. 涉及到的文件

### 4.1 必须改（Phase 0）

| 文件 | 改什么 | 为什么 | 风险 |
|---|---|---|---|
| `vendor/.../Seele/session/hooks.go` | `ToolCallInfo` 加 `ID`/`Index`/`BatchSize` | A1：身份进不了钩子 | 低（纯数据 struct 加字段，按名初始化，零影响） |
| `vendor/.../Seele/session/loop.go` | `:332`、`:375` 两处填充新字段 | A1 | 低 |
| `seelebridge/runtime_tools.go` | `RegisterTool` 加 meta 声明路径 | A8：前置 | 低（可做纯追加形参或名字→meta 表） |
| `seelebridge/tools/router.go` | 给 `read_file/grep_search/glob` 填 `ro`，`write_file/edit_file/bash` 填 `rw`+资源键 | A8 | 低 |
| `main.go` | 其余内联工具填 Groups（`switch_plugin`/`plan_*`/`todo_*`/`async_*`/`fork_subagents`…） | A8 | 低，但**量大**，是 Phase 0 的主要工作量 |
| `application/core/tool_hooks.go` | ① `beginTool/completeTool` 改为**优先用 `info.ID`**，`tool-%d` 降为兜底；② `toolHookKey` 保留但不再是唯一配对依据 | A4/A5：**消除静默错配** | 中（配对语义变化，须配 `I-2` 断言） |

### 4.2 应当改（Phase 1）

| 文件 | 改什么 | 为什么 |
|---|---|---|
| `application/toolpolicy/policy.go` | **新建**：`Class`/`Policy`/`PolicyFor`/`KeyFor`/`Resolve` | §1.2 |
| `application/toolpolicy/policy_test.go` | **新建**：空 Groups、多组冲突（ro+ctl）、rw 键派生 | 契约测试 |
| `seelebridge/dispatch/dispatcher.go` | **新建**：`Call`/`Outcome`/`Dispatcher`/`Edges` + `WaitGroup` 实现 | §1.3 |
| `seelebridge/dispatch/dispatcher_test.go` | **新建**：I-1..I-8 | 契约测试 |
| `seelebridge/tools/slot.go` | **新建**：`SlotMiddleware` + `SemaphoreAcquirer` | §1.5 |
| `seelebridge/tools/registry_state.go` | `:24-30` `NewRegistryState` 的 `WithMiddleware(...)` 追加 `slotMiddleware`（**plain 层末位**） | A9：必须在权限门之内 |
| `seelebridge/runtime.go` | `:326` 装配处传入 acquirer | A9 |
| `seelebridge/dispatch/edges.go` | **新建**：把 `Dispatcher` 的批量前后沿接到 `ToolHookBridge` | §1.3 |
| `seelebridge/tools/permission_policy.go` | 超时/排队失败的错误分类（§5.4） | §5.4 |
| `application/core/tool_hooks.go:238` | **空 assistant 占位只在批内最后一个 Index 触发** | 见 §4.4：否则会产生 N-1 条冗余空 assistant 消息 |

### 4.3 看了但不改（要明确写出来，防止误改）

| 文件 | 为什么不动 |
|---|---|
| `seelebridge/fs/filesystem_actor.go:39,51-64` | per-path 锁**已实现** `rw` 隔离，不要重复加锁 |
| `application/core/tool_hooks.go:32,140,170,171` | application 侧**已按 ID 寻址**（A6），原生支持 N 个并行 running，不要重写 |
| `vendor/.../tools/tools.go:295-296` | `chain`/`chainMeta` 装配顺序是框架契约，靠**追加中间件**而非改这里 |
| `loop.go:413` `OnIterationComplete` | 与并行正交，不动 |
| `seelebridge/tools/router.go:576-588` | `scopedToolTimeout` 作为第三层保留（§5.1），不删 |

### 4.4 并行化会直接踩到的两个顺序敏感点（已核实）

**① 空 assistant 占位会从 1 条变成 N 条。**

```go
// tool_hooks.go:232-239（handleToolCompleteObserved 内）
message = *service.appendSessionMessageWithOriginLocked(sessionID, "tool_result", content, &ToolCall{
    ID: id, Name: name, Result: visibleContent, Status: status, ...,
}, ...)
// tool_result 之后补空 assistant 占位（活跃与后台同一语义）：后台会话若
// 缺占位，后续 appendVisibleDelta(Background) 会把下一段 LLM 正文并入
// 工具之前的旧 assistant 空消息，工具看起来"后插入"到会话末尾
if appended := service.appendAssistantPlaceholderAfterToolLocked(sessionID); appended != nil {
    assistant = appended
}
```

现状（1 调用）：`tool_result` → `assistant(empty)`。
并行批（3 调用，按 Index 回填）：

```
assistant(tool_calls)
tool_result(0) → assistant(empty)     ← 冗余
tool_result(1) → assistant(empty)     ← 冗余
tool_result(2) → assistant(empty)     ← 只有这条是注释里要的那一条
```

**语义正确但产物冗余**：N-1 条空 assistant 消息会进可见会话（`publishSessionEvent(EventMessageAdded, …)`），每条都是真实消息。修法是**只在 `Index == BatchSize-1` 时补占位**——这需要 `ToolCallInfo.BatchSize`（§1.4 新增字段的一个具体用途）。

**② 叙述归位只发生一次。**

```go
// tool_hooks.go:39（handleToolStart 内）
service.components.tasks.AttributeToolNarrationLocked(sessionID,
    TranscriptToolCall{ID: id, Name: name, Arguments: arguments},
    service.streamedAssistantTextLocked(sessionID))
```

它把「本迭代刚 flush 出来的流式正文」归位到该 tool_call 事件。批量前后沿下，**所有 start 连续触发**，于是：第一次 start 拿到正文并归位、其余 start 拿到空串。

串行时同理（只有第一个调用拿得到正文），**所以这不是新引入的退化**——但**归位到「哪个 index」需要显式决定**：应归给批内**第一个 Index**（= 模型叙述后紧跟的第一个动作），而不是「第一个完成的」。批量前后沿（按 Index 序）天然满足这一点。这正是选用批量前后沿而非「完成即触发」的第二个理由。

---

## 5. 不同权限 → 默认超时 + 超时受理方案

### 5.1 现状：只有一个旋钮，且有两个坑

```go
// vendor/.../tools/tools.go:194-200 —— 全局唯一超时
func WithCallTimeout(timeout time.Duration) RegistryOption {
    return func(r *Registry) { if timeout > 0 { r.callTimeout = timeout } }
}
// :362-369 —— 包住整条装饰链
if timeout > 0 {
    if deadline, has := ctx.Deadline(); !has || time.Until(deadline) > timeout {
        callCtx, cancel = context.WithTimeout(ctx, timeout)
    }
}
result, err := entry.Handler.Execute(callCtx, call.ArgumentsJSON)   // :373
```

**坑 1（A11）：两个超时的 `0` 语义相反。**

| 配置键 | 默认 | `0` 的含义 | 位置 |
|---|---|---|---|
| `tool_call_timeout` | 1800s | **显式无限制** | `seelexctx/limits.go:208-212` |
| `approval_timeout` | 600s | **补默认**（600s） | 同上 `:211-212` |

同一个配置文件里，两个相邻的键，`0` 一个是「无限」一个是「默认」。这是必须在 Phase 0 一并修的（要么统一，要么在 `Validate` 里拒绝歧义写法）。

**坑 2（A7）：审批等待在吃执行预算。**

`callCtx` 包住的 `entry.Handler` 是**整条装饰链**（`tools.go:295-296` 装配），含权限门。于是：

| `tool_call_timeout` | `approval_timeout` | 人实际能看多久 |
|---|---|---|
| 1800s | 600s | 600s（审批先到期，无害） |
| **300s** | 600s | **300s ← 人的窗口被静默砍半** |

第二行不报错、不告警，只是审批提前被 `context.DeadlineExceeded` 掐掉。**并行化会让这个坑更容易踩**（见 §5.3）。

### 5.2 【提议】四层超时矩阵

| # | 层 | 默认 | 按权限类取值 | 包住什么 |
|---|---|---|---|---|
| T1 | **外层安全网**（registry `callTimeout`） | `max(审批+执行+排队)+余量` | 全局一个，不分类 | 整条链（含审批） |
| T2 | **审批等待**（`approval_timeout`） | 600s | 全局一个 | 人的点头 |
| T3 | **排队等待**（**新增**） | 见下表 | 按 `Class` | 等并发槽 |
| T4 | **执行超时**（**新增**，按类） | 见下表 | 按 `Class` | handler 本体 |

**T3/T4 的按类默认值【提议】：**

| Class | 触发组 | 排队超时 T3 | 执行超时 T4 | 依据 |
|---|---|---|---|---|
| `ClassParallel` | `ro` | 不需要（不限） | **120s** | 读是快操作；120s 远超正常，超了必是异常 |
| `ClassSerialKey` | `rw` | **300s** | **300s** | 等同文件前一个写者；写操作本身不应长 |
| `ClassSerialSession` | `rw_session` | **120s** | **60s** | 会话内状态变更应当即时 |
| `ClassSerialGlobal` | `ctl`/`adm`/`rw_desktop` | **120s** | **600s** | 桌面操作含等待（`computer_*` 要等人机交互节奏） |
| （保留）`bash` 显式 `timeout` 参数 | — | — | 调用方指定优先 | A10 |

**T1 的推导规则（必须能自动校验）**：

```
T1_min = T2 + max(T3) + max(T4) + 60s余量
```

即 `tool_call_timeout ≥ approval_timeout + max排队 + max执行 + 60`。默认值代进去：`600 + 300 + 600 + 60 = 1560s ≤ 1800s` ✅ 当前默认恰好合法——**但纯属巧合，没有任何机制保证它**。

**【提议】启动期校验**：`seelexctx.Limits.Validate` 里加这条（`limits.go:373` 已有 `Validate` 的位置），不满足就**显式失败**而不是静默降级。这与该文件既有风格一致：

> 装配路径宁可显式失败，也不要静默写进一条无意义的授权（那会变成「看起来分配了，其实按宿主默认判」）

### 5.3 为什么并行化让超时更糟（必须一起改）

串行时，第 k 个调用卡住，前 k-1 个**已经出结果**。并行时，N 个调用共享同一个等待窗口与同一个 `callCtx` 截止时间：

| 场景 | 串行 | 并行 |
|---|---|---|
| 1 个卡满 1800s，其余 5 个各 2s | 前 5 个 10s 出结果，第 6 个卡 1800s → 中途有产出 | 6 个一起等满 1800s → **中途零产出** |
| 审批（T2=600s）+ 执行（T4=600s）在一个 `callCtx` 内 | 顺序累加 | **累加后可能超 T1** |

所以 Phase 1 **必须**同时做两件事：① T4 从 T1 里独立出来（不让审批吃执行预算）；② N 个调用**不共享同一个 `callCtx` 截止时间**——每个调用按自己的 `Index` 单独派生 `context.WithTimeout`。

### 5.4 超时受理方案（四类超时，四种受理）

**问题**：现在超时错误是不透明的。并行下模型必须能区分「它没跑（没抢到槽）」和「它跑了 30 分钟被杀」——前者应拆小重试，后者应改方案。

**【提议】统一带阶段的超时错误**，落在原 `Index` 上（不改变回填序）：

```go
// seelebridge/dispatch/timeout.go
type Stage string

const (
    StageQueue    Stage = "queue"     // 等并发槽超时（工具根本没执行）
    StageApproval Stage = "approval"  // 等人点头超时
    StageExecute  Stage = "execute"   // 执行本体超时（可能已产生副作用）
    StageBackend  Stage = "backend"   // 后台进程探针等待超时（§见设计文档 §5）
)

type TimeoutError struct {
    Stage     Stage
    Tool      string
    Index     int
    Policy    toolpolicy.Policy
    Waited    time.Duration
    Limit     time.Duration
}
func (e *TimeoutError) Error() string
func (e *TimeoutError) Is(target error) bool   // 支持 errors.Is(err, context.DeadlineExceeded)
```

**受理矩阵：**

| 阶段 | 谁产生 | 是否已执行 | 副作用 | 受理动作 | 呈现给模型的话 |
|---|---|---|---|---|---|
| `StageQueue` | `SemaphoreAcquirer.Acquire` | **否** | 无 | **不改状态**，直接按 Index 落结果 | 「未执行：等同类资源槽超时（N 排队中）。本批其它调用不受影响」 |
| `StageApproval` | 权限门 `Gate` | 否 | 无 | 走既有 `DenialError` 语义（`DenyWithoutPrompt=true`） | 「拒绝：审批未在 N 秒内响应」 |
| `StageExecute` | T4 中间件 | **是** | **可能有**（写了一半） | **必须标记「结果未知」**，不得当作「未发生」 | 「已执行但超时：副作用状态未知，勿盲目重试；先核实」 |
| `StageBackend` | `async_*` 探针 | 是（后台） | 有 | 走既有 `handle` 语义（可继续 `async_output` 轮询） | 「后台仍在跑，handle=…，可继续轮询」 |

**`StageExecute` 必须显式标「结果未知」**，这是并行化引入的新危险：串行时超时的工具后面没有别的动作；并行时同一批里可能有依赖它的调用已经被并发发出去了。

**受理序（不变量）**：无论哪种超时，都**在 `Index` 位置落一个 Outcome**，保持 `len(outcomes) == len(calls)` 且 `Outcomes[i].Index == i`。**不允许「跳过」或「缩短」切片**——否则回填序会错位，直接破坏 I-1/I-5。

---

## 6. 落地顺序（Phase 0 的合并版）

按依赖关系排序，每步可独立验证：

| 序 | 步骤 | 验证 |
|---|---|---|
| 1 | 框架 `ToolCallInfo` 加 `ID/Index/BatchSize` + loop 两处填充 | 编译 + 现有测试全绿 |
| 2 | `ToolHookBridge.beginTool/completeTool` 优先用 `info.ID` | 新增测试：同批两个 `(name,args)` 完全相同的调用，断言配对正确 |
| 2b | 空 assistant 占位改为**仅批内最后一个 Index** 触发（§4.4①） | 新增测试：3 调用批产出**恰好 1 条**空 assistant |
| 3 | 修 `limits` 的 `0` 语义歧义（A11）+ `Validate` 加 T1 校验 | 配置测试：`tool_call_timeout=300, approval_timeout=600` 须**报错** |
| 4 | `RegisterTool` meta 路径 + 全量工具填 `Groups` | 遍历断言：所有注册工具 `Groups` 非空 |
| 5 | `application/toolpolicy` + 单测 | 契约测试（空/多组冲突/rw 键派生） |
| 6 | `async_probe` 只读旁路（设计文档 §5.1） | 探针读后 `async_output` 仍能取回同一段 |
| 7 | batch 读工具（多文件并行读） | **收益试金石**：18 文件并行 vs 串行墙钟对比 |

**第 7 步是决策点**：收益明显才付 Phase 1 的框架代价。大头（bash 长命令）已被 async 覆盖，单轮多调用的占比未必高。

---

## 7. 本文相对前文的修正

| 前文 | 修正 | 依据 |
|---|---|---|
| R-3「application 假定 start→complete 严格交替，工作量最大」 | **下调，但落到两个具体点**：① `tool_hooks.go` 已全按 ID 寻址（A6），`Tool{ID, Status:"running"}` 原生表达并行 running，**存储层不需要改**；② 真正要改的是 **`tool_hooks.go:238` 的空 assistant 占位**（并行批会产出 N-1 条冗余，见 §4.4①）与 **`:36` 的叙述归位**（须明确归给批内第一个 Index，见 §4.4②） | `tool_hooks.go:32,45,140,170,171`；`tool_hooks.go:232-239,36-38` |
| 「P0-1 补 CallID」 | **细化**：wire 侧 `tc.ID` 已在用（A3），缺的是**进框架钩子载荷**（A1）；且必须同时**归一 `tool-%d` 与 `tc.ID` 两个 ID 空间**（A4），否则加了字段仍有两套身份 | `loop.go:403` vs `tool_hooks.go:443-446` |
| 「超时阈值放羊」（笼统） | **精确到两个坑**：`0` 语义相反（A11）；`callCtx` 把审批等待算进执行预算（A7），`tool_call_timeout=300` 会静默把人 600s 的窗口砍到 300s | `limits.go:208-212`、`tools.go:295-296,362-369` |

---

# 第二部分：打点表、测试用例、双仓分工

> §8 展开设计文档 §6；§9 是可执行用例清单；§10 是「seele 改什么 / seelex 改什么」的边界。
> §11 是本次对全文 `file:line` 的复核结果（有几处要改）；§12 是改动前的基线实跑。

---

## 8. 打点表详细设计：在途调用纳入投影

### 8.1 现状锚点（已复核，基座 = vendor 的 Seele v0.3.1）

| # | 事实 | 位置 |
|---|---|---|
| W1 | 行模型是 `WorkItem`（23 字段，含 `ID/Phase/Kind/SourceID/Task/Description/Status/Trace/BatchID/BatchLabel`） | `application/model/state.go:532` |
| W2 | 三个读面共用同一个建表函数 `buildWorkTable(plan, tasks, subagentTree, asyncRuns)` | `application/core/work_table.go:30` |
| W3 | 三处建表入口：实时重投影、会话快照、冷读面 | `work_table.go:250`、`session_scope.go:513`、`session_cold_read.go:60` |
| W4 | 后台行投影：**在途全留，终态只补到上限** | `application/core/work_table_async.go:31-48`（上限 `asyncWorkMaxRows = 32`，`:20`） |
| W5 | 行硬上限 `Limits().WorkTableRows`（默认 200），超出尾部截断 | `work_table.go:55-57`、`seelexctx/limits.go:114,189` |
| W6 | 打点块 `workTableTraceBlockFor`：marker 包裹，只投**未终态** task + **本会话 running** async | `work_table.go:676`（块常量 `:648-650`） |
| W7 | 块文案：上限触发写「…（打点表已达上限，详情见工作表格）」；有 async 行时附一句用法说明 | `work_table.go:641`、`:647-650` |
| W8 | 注入点：块拼进 `current_input` 前缀 | `application/core/context_runtime/coordinator.go:455` |
| W9 | async 探针**已是只读旁路**：`Router.AsyncRuns() → AsyncRunInfo`，不碰游标 | `seelebridge/tools/async_probe.go:24-58` |
| W10 | 消费式游标只有一个入口：`advanceTail` + `markCursor` | `async_exec.go:380,415`；唯一调用点 `async_tools.go:46-50` |
| W11 | 模型面后台工具只有两个：`async_output` / `async_kill` | `seelebridge/tools/router.go:103-104` |

> **W9 更正了设计文档 §5 的过时陈述**：那里把「只读探针（不推进游标）」标成 ❌ 需新增，实际已经是现状。§5.1 的**要求**（探针不推进游标）是对的，但**不必再做**——补一条回归断言即可（§9.1-8）。

### 8.2 缺口：内联调用在执行期间是黑洞

并行化引入的新盲区：一批 N 个 tool_call **执行期间**，

- history 里**还没有** tool 消息 —— 回填在「等齐之后」（契约 §2 第 4 步），所以那段窗口里历史只有 `assistant(tool_calls)`；
- 工作表格与打点块里**没有**对应行 —— `buildWorkTable` 只认注册表 task + 后台登记表，内联调用两处都不在。

后果：**若压缩恰好落在这个窗口内**，`assistant(tool_calls)` 被压缩，模型就失去「我刚派了 3 个并行 read，其中 2 个该回来了」这个事实，只能靠复述。串行时不存在这个窗口（每个调用完成即 append，中间态极短）；并行把窗口放大到整批的墙钟时长 —— **这正是「并行化把问题从不可能触发变成容易触发」的又一个实例**（同 A5）。

结论：打点表要补**一类**行，事实源是新的、形态与 async 行同构。

### 8.3 数据模型（最小新增）

```go
// application/contract/dto/inline_call.go（新增）
// InlineCallRecord 是一条「在途内联工具调用」的只读投影源。
// 与 AsyncRunRecord 同构：内存态、不落盘、不做第二事实源。
type InlineCallRecord struct {
    SessionID string
    BatchID   string    // = 本会话 requestID，与 async 行同批次口径
    Turn      int       // ReAct 轮次
    Index     int       // wire 下标 —— 顺序键（契约 §1.3）
    CallID    string    // provider 的 tool_call id —— 身份键
    Name      string
    Summary   string    // 参数摘要，≤40 字符（沿用 truncateWorkEvidence 口径）
    StartedAt time.Time
}
```

登记表：`pendingInline`（会话级 map，`sync.Mutex` 保护），**不进 task 注册表、不随会话落盘**——理由与 `work_table_async.go:14` 逐字相同：注册表 record 会持久化并回灌，句柄表在内存，真进去就会在重启后留下永久 `running` 假行；要避免就得给关停补终态回执，等于把「不做持久化」重新买回来（不变式 I-21）。

行映射**复用现有列位**，不新增 Kind：

| WorkItem 字段 | 取值 | 理由 |
|---|---|---|
| `ID` / `SourceID` | `inline:<turn>#<index>` | 与 `async:<handle>` 同形，前端按前缀分流 |
| `Phase` / `Kind` | `"task"` / `"task"` | 落在既有 `phaseOrder` 内（`work_table.go:33`），不引入第五阶段 |
| `Task` | 工具名 | 行标题 |
| `Description` | `并行调用: <name> <summary>（batch=<turn>#<index>/<N>）` | 与 async 行的「后台命令: …」同构 |
| `Status` | `running` | 在途→消失，**不产生终态**（见 §8.6） |
| `BatchID` | 会话 requestID | 与 async 行、task 行同批次分片 |
| `Trace` | 一条 `WorkTracePoint{Operation:"inline_dispatch", Evidence:"已派发，等齐回填"}` | 与 `asyncProbeOperation` 同形的打点行 |

### 8.4 投影规则（四条，全部必须写成测试）

| # | 规则 | 依据 |
|---|---|---|
| **R1** | 只投**本会话 + 在途**。批结束即整批消失；终态**不进**打点块 | 与 `asyncTraceLines`（`work_table_async.go:122-143`）同语义：终态进块只会让每轮白烧 token |
| **R2** | **排序键 = wire 下标 `Index`，不是完成序** | 与契约 §1.3/§1.4 同源：模型在块里看到的顺序，必须等于它将看到的回填顺序。**这是打点表与 loop 设计唯一强耦合的点，也是最容易搞错的一处** |
| **R3** | 在途行**优先于**终态行占用块预算；总行预算仍是 `workTableTraceMaxLines = 30`，超出沿用既有文案 | 在途优先是 async 行已有的口径（`asyncWorkItems` 先 running 后 finished） |
| **R4** | 字段收窄：只投 id、状态、工具名、参数摘要（≤40 字符）。**不投完整参数、不投结果** | 结果必须走 history 回填（§8.5 边界）；探针读数重复进上下文只会每轮烧 token（`work_table_async.go:120-121` 的同一条判断） |

### 8.5 抗压缩，与一条不得破的边界

**抗压缩**：块每轮重播进 `current_input` 前缀（W8），因此**随每一轮重建**，不依赖历史。压缩把 `assistant(tool_calls)` 折掉之后，模型仍能从块里看到「在途的 3 个并行 read」。

**边界（不得破）**：**投影是视图，不是通道。** 结果只能从 history 回填路径进入上下文（契约 §2 第 4c 步）。不得让模型「从打点块里取工具结果」——那会让块从只读视图变成第二事实源，与 W4/§8.3 的设计前提直接冲突。

### 8.6 生命周期与失败面

```
批开始 ──► 登记 N 条（派发之前，wire 序）        ← 与「批量前沿 OnToolStart」同一时刻
执行中 ──► 登记表**不变**（它只表达「在途」；完成与否由回填表达）
批结束 ──► 等齐后**一次性清空**，defer 保证 panic / early-return 也清
```

三条必须写死的约束：

1. **清空与 hooks / telemetry 解耦**：不能挂在 `OnToolComplete` 上（hook 可能为 nil、可能被跳过），也不能挂在 span 结束上。清空点是「批后沿」本身。
2. **清空先于回填**：否则回填期间打点块仍显示在途，出现「结果已经进历史、块里还说在跑」的自相矛盾。
3. **失败/超时不留半途状态**：任一调用失败或 `StageQueue`/`StageExecute` 超时，其 Outcome 仍落在原 `Index`（契约 §5.4 受理序），批结束时一并从登记表消失——**打点表里没有「失败的在途行」这一态**。

### 8.7 前端面（seelex 侧）

工作表格面板只需多认一个 `inline:` ID 前缀（与 `async:` 同形），数据源仍是同一份 `WorkTable` / `worktable.changed`，**不新增通道**。行标题由 §8.3 的 `Description` 承担，前端不需要新字段。

---

## 9. 测试用例清单

约定：**I-x** 是设计文档 §4 的不变量，**T-x** 是本节新增的打点表不变量。

### 9.1 新增单测

| # | 文件（新/扩展） | 测试函数 | 断言 | 覆盖 |
|---|---|---|---|---|
| 1 | `application/toolpolicy/policy_test.go`（新） | `TestPolicyFor_NilAndUnknownGroupIsSerial` | nil / 未知组 → `ClassSerialGlobal` | I-7 |
| 2 | 同上 | `TestPolicyFor_SerialGroupBeatsParallel` | `[ro, ctl]` → 串行（安全侧优先） | I-7 |
| 3 | 同上 | `TestPolicyFor_RWIgnoresCrossPath` | 同组不同路径 → 同 Class、不同 Key | I-4 前置 |
| 4 | 同上 | `TestResolve_KeyStableForSameArgs` | 同 args 两次 → 同 Key（幂等，无随机量） | I-4 |
| 5 | `seelebridge/dispatch/dispatcher_test.go`（新） | `TestDispatch_OutcomesIndexAddressed` | 人为乱序完成 → `Outcomes[i].Index == i`，长度 == len(calls) | I-5 |
| 6 | 同上 | `TestDispatch_PrefixStableUnderShuffledCompletion` | 同批跑两次、完成序不同 → 回填 history 的 `(role,call_id,content)` 序列**逐字节相等** | **I-1** |
| 7 | 同上 | `TestDispatch_SameKeyNeverOverlaps` | 记录 enter/exit 时间戳，同 Key 区间无交叠 | I-4 |
| 8 | 同上 | `TestDispatch_FailureIsolatedAtItsIndex` | 注入必败工具 → 其它 Outcome 照常，错误落在原 Index | I-6 |
| 9 | 同上 | `TestDispatch_HooksSingleThreadedAndBatched` | hook 内原子计数恒 == 1；事件序列 = `start×N` 然后 `complete×N`，均按 Index 序 | **I-8** |
| 10 | 同上 | `TestDispatch_AcquireTimeoutIsExplicit` | 取槽超时 → `StageQueue` 错误落在原 Index，**不静默降级为串行** | §5.4 |
| 11 | 同上 | `TestDispatch_NoGoroutineLeakOnCancel` | ctx 取消后 goroutine 数回落 | §7 R-7 |
| 12 | `seelebridge/tools/slot_test.go`（新） | `TestSlotMiddleware_ApprovalOutsideSlot` | 并发发起 N 个待批调用，审批等待期间槽位占用 == 0 | **I-3** |
| 13 | 同上 | `TestSlotMiddleware_SingleCallPathUnchanged` | 单调用（N=1）产物与改造前一致（防回归） | — |
| 14 | `application/core/tool_hooks_ids_test.go`（新） | `TestToolHookPairing_PrefersWireID` | 同批两个 `(name,args)` **完全相同**的调用 → 配对正确（现状会靠 FIFO 对齐） | **I-2** |
| 15 | 同上 | `TestToolHookPairing_FallsBackToSeqWhenIDEmpty` | `info.ID == ""` 时退化为 `tool-%d` 旧路径 | A4 兼容 |
| 16 | `application/core/tool_hooks_batch_test.go`（新） | `TestBatchPlaceholder_ExactlyOneEmptyAssistant` | 3 调用批 → 可见会话里**恰好 1 条**空 assistant | §4.4① |
| 17 | 同上 | `TestBatchNarration_AttributedToFirstIndex` | 叙述归位给 `Index == 0`（不是「第一个完成的」） | §4.4② |
| 18 | `application/core/work_table_inline_test.go`（新） | `TestInlineCallsProjectedWhileInFlight` | 批执行中 `buildWorkTable` 含 N 条 `inline:` 行、`Status==running` | T-1 |
| 19 | 同上 | `TestInlineCallsDisappearAfterBatch` | 等齐回填后 `inline:` 行数 == 0（且清空先于回填） | T-1 |
| 20 | 同上 | `TestInlineCallOrderFollowsWireIndex` | 人为让 `Index=2` 先完成 → 块内顺序仍是 0,1,2 | **T-2（R2）** |
| 21 | 同上 | `TestInlineCallsNotPersisted` | 会话落盘 → 冷读/恢复后**无** `inline:` 残留行 | I-21 同源 |
| 22 | 同上 | `TestInlineProjectionBudgetSharedWithAsync` | 在途行数 > 预算 → 在途优先，超限写既有上限文案 | T-3（R3） |
| 23 | 同上 | `TestTraceBlock_InlineAndAsyncCoexist` | 块内 inline 行 + async 行 + 既有用法说明行都不被挤掉 | W7 回归 |
| 24 | 同上 | `TestInlineProjectionDoesNotCarryResults` | 同批某调用已返回 → 其内容**不出现**在任何在途行的字段里 | R4/§8.5 边界 |
| 25 | `seelexctx/limits_timeout_test.go`（扩展） | `TestLimitsZeroSemantics_NoLongerInverted` | 两键 `0` 语义不再相反（或 `Validate` 显式拒绝歧义写法） | A11 |
| 26 | 同上 | `TestLimitsValidate_OuterTimeoutMustCoverInner` | `tool_call_timeout=300` + `approval_timeout=600` → **显式失败**（不是静默降级） | §5.2 T1 |
| 27 | `seelebridge/tools/async_probe_test.go`（扩展） | `TestProbeDoesNotAdvanceCursor` | 探针读后 `async_output` 仍能取回**同一段**（现状已实现，补回归断言） | §5.1 / W9 |
| 28 | `application/core/work_table_test.go`（扩展） | `TestWorkTableRowsLimitStillApplies` | 行上限 200 仍在（内联行不得绕过） | W5 |

### 9.2 必须保持全绿的既有测试（改动前基线，见 §12）

`application/core/`：`work_table_test.go`、`work_table_async_test.go`、`work_table_ab_test.go`、`work_table_plan_deps_test.go`、`work_table_race_test.go`、`work_table_session_axis_test.go`、`work_table_session_scope_test.go`、`work_table_subagent_status_test.go`、`work_table_fuzz_test.go`、`tool_hook_diagnostic_test.go`、`tool_hooks_truncation_test.go`、`fixture_concurrency_test.go`、`session_concurrent_content_isolation_test.go`
`application/core/worktable/`：`worktable_publisher_test.go`
`seelebridge/tools/`：`async_exec_test.go`、`async_exec_raceproof_test.go`、`async_kill_test.go`、`async_probe_test.go`、`async_progress_test.go`、`permission_*.go`（12 个）
`seelebridge/`：`runtime_async_test.go`、`worktable_global_scope_test.go`、`worktable_ledger_identity_test.go`、`worktable_session_axis_test.go`、`fork_concurrency_test.go`、`fork_concurrency_repro_test.go`、`merge_back_concurrency_test.go`
根包：`tool_full_chain_test.go`、`prefix_invariant_fullchain_test.go`、`async_tool_wire_live_probe_test.go`、`repro_multi_round_llm_preserved_test.go`、`repro_second_round_engine_released_test.go`

### 9.3 人工/端到端验收（不进 CI）

1. **P0-3 收益试金石**：18 文件并行读 vs 串行的墙钟对比；收益不明显则 Phase 1 缓（设计文档 §7）。
2. **打点表肉眼验收**：一批 3 个 read 时，模型侧上下文里出现 3 行 `inline:`；批结束后消失；同一批里 async 行不受影响。
3. **审批 × 并行**：并发发起 2 个待批 `write_file`，确认审批面板出现 2 笔、且等待期间不占并发槽。

### 9.4 不变量 → 用例映射

| 不变量 | 用例 |
|---|---|
| I-1 前缀稳定 | 6 |
| I-2 配对唯一 | 14 |
| I-3 槽外审批 | 12 |
| I-4 桶内串行 | 7（+ 3、4 派生键） |
| I-5 结果保序 | 5（+ 20 同源） |
| I-6 失败隔离 | 8 |
| I-7 未分类串行 | 1、2 |
| I-8 hook 不并发 | 9 |
| T-1 在途可见可消失 | 18、19 |
| T-2 在途顺序 = wire 序 | 20 |
| T-3 预算与裁剪 | 22、23 |
| T-4 投影不载结果 | 24 |

---

## 10. 双仓改动清单：seele 侧 / seelex 侧

### 10.0 先定基座（这是前置，不是细节）

**事实（已核实）**：

| 事实 | 证据 |
|---|---|
| seelex 钉 `Seele v0.3.1`，vendor 模式生效 | `go.mod:17`；`vendor/modules.txt:8` |
| `vendor/` **不在版本控制里**（本地生成物） | `.gitignore:39` = `/vendor/`；`git ls-files vendor` = 0 |
| 本地 Seele 仓 `G:\Program\go\seele` 的 HEAD **就是 v0.3.1** | `git describe` → `v0.3.1`（annotated tag → commit `6a04a7c`） |
| **但它的工作树是脏的，正处在 `session/` 重构中途** | `M session/loop.go, session/chat.go, session/session.go`；`D session/inloop.go, session/inloop_test.go`；`?? session/state.go` |
| 因此本地 loop.go（607 行）≠ vendor loop.go（614 行） | 哈希不同；89 个同名文件内容不同 |

**这件事直接改设计代码**——重构基座上有三处语义变化，契约 §2 的 loop 重写片段必须相应调整：

| # | vendor（v0.3.1） | 重构基座（本地工作树） | 对设计的影响 |
|---|---|---|---|
| B1 | `rl.history = append(rl.history, …)`（`loop.go:398-400`） | `rl.state.append(types.Message{…})` | §2 片段第 4c 步改 `rl.state.append` |
| B2 | 调用方传 `History: rl.History()`（`loop.go:405-408`） | `handleContextEvent` **自行填充** History，调用方不传 | §2 片段删掉 `History:` 字段 |
| B3 | 无 | 落历史前新增 `rl.state.drain()`（排空宿主排队写入） | **批量回填前调用一次 `drain()`**：串行时每个 append 前各 drain 一次，批量后一次即可且语义等价（所有 handler 已在等齐前结束） |

**建议：以本地在改的 `session/` 重构为基座**——它改的正好是本设计要动的历史写入点，若以 v0.3.1 为基座会立刻撞车。**先把这批改动提交或明确成基线**，再落 P0-1。

### 10.1 seele 侧改动（极小、纯加、零调用方影响）

| # | 文件 | 位置 | 改什么 | 为什么 |
|---|---|---|---|---|
| S1 | `session/hooks.go` | `:24-31` | `ToolCallInfo` 加 `ID string` / `Index int` / `BatchSize int` | A1：wire 身份进不了钩子（唯一必须改框架的地方）；`BatchSize` 是 §4.4① 唯一的修法依据 |
| S2 | `session/loop.go` | `:332`（OnToolStart 填充）、`:375`（OnToolComplete 填充） | 填 `ID: tc.ID, Index: i, BatchSize: len(assistantMsg.ToolCalls)` | 同上 |
| S3 | `session/loop.go`（Phase 1） | `:327-415` 替换为 §2 的重写 | 并发派发 + 保序回填 | 核心 |

**不改**（明确写出来防误改）：`tools.go` 的 `chain`/`chainMeta` 装配顺序（`:295-296`）、`OnIterationComplete`、LLM 调用段、`handleContextEvent` 本体。

**交付路径**：Seele 仓提交 → tag `v0.3.2` → seelex `go get github.com/RedHuang-0622/Seele@v0.3.2` → `go mod vendor` → 入库的是 `go.mod`/`go.sum`（`vendor/` 被忽略）。联调期可临时 `replace` 到本地路径，**发版后必须移除**（`go.mod:5-12` 的注释已经立了这条规矩）。

### 10.2 seelex 侧改动

**Phase 0（零风险，先做；不动 loop）**

| # | 文件 | 位置 | 改什么 | 依据 |
|---|---|---|---|---|
| X1 | `seelebridge/runtime_tools.go` | `:264` `RegisterTool` | 加 meta 声明路径（按名查表或可选形参） | A8，**硬前置** |
| X2 | `seelebridge/tools/router.go` | 工具注册处 | `read_file/grep_search/glob` → `ro`；`write_file/edit_file/bash` → `rw` + 资源键 | A8 |
| X3 | `main.go` | 内联工具注册处 | 其余工具填 `Groups`（`switch_plugin`/`plan_*`/`todo_*`/`async_*`/`fork_subagents`/`computer_*`…） | A8，**Phase 0 主要工作量** |
| X4 | `application/core/tool_hooks.go` | `:414-441`、`:443-446`、`:448-450` | `beginTool`/`completeTool` 优先用 `info.ID`；`tool-%d` 降兜底（**归一两个 ID 空间**） | A4/A5，消除静默错配 |
| X5 | `application/core/tool_hooks.go` | `:238` | 空 assistant 占位只在 `Index == BatchSize-1` 触发 | §4.4① |
| X6 | `application/core/tool_hooks.go` | `:39` | 叙述归位显式归给批内 `Index == 0` | §4.4② |
| X7 | `seelexctx/limits.go` | `:49-50`、`:150-151`、`:208-212`、`:373` | 修 `0` 语义歧义 + `Validate` 加 T1 覆盖校验 | A11 / §5.2 |
| X8 | `e2e/layout_test.go` | `:243-246` | `skipped` 加 `"vendor": true` | §12 基线红（Step -1） |

**Phase 1（P0-3 试金石通过后）**

| # | 文件 | 改什么 |
|---|---|---|
| Y1 | `application/toolpolicy/policy.go`（新） | `Class`/`Policy`/`PolicyFor`/`KeyFor`/`Resolve` |
| Y2 | `seelebridge/dispatch/dispatcher.go`（新） | `Call`/`Outcome`/`Dispatcher`/`Edges` + `WaitGroup` 实现 |
| Y3 | `seelebridge/dispatch/edges.go`（新） | 批量前后沿接到 `ToolHookBridge` |
| Y4 | `seelebridge/tools/slot.go`（新） | `SlotMiddleware` + `SemaphoreAcquirer` |
| Y5 | `seelebridge/tools/registry_state.go` | `:24-30` 的 `WithMiddleware(...)` **追加** slotMiddleware（plain 层末位 = 审批之后） |
| Y6 | `seelebridge/runtime.go` | 装配处传入 acquirer |
| Y7 | `main.go` | `groupsOf(name)` 读 `ToolMeta` 注入 loop |
| Y8 | `application/core/work_table_async.go`（新 `work_table_inline.go`） | §8 的在途内联投影 + 登记/清空 |
| Y9 | `application/core/tool_hooks.go` | 在途登记接线（派发前登记、批后清空，§8.6） |

**不改**（防误改，逐条已在正文核过）：`seelebridge/fs/filesystem_actor.go:47-64`（per-path 锁已实现 `rw` 隔离）；`tool_hooks.go:33,50,54`（application 侧已按 ID 寻址，原生支持 N 个并行 running）；`vendor/.../tools/tools.go:295-296`（装配顺序是框架契约）。

### 10.3 一句话分工

> **seele 侧只加三个纯数据字段**（身份、顺序、批大小）——它是「必答不可」的一行；
> **seelex 侧承担全部策略与执行**：声明（`ToolPolicy`/`Groups`）、并发原语（`dispatch`/`slot`）、投影（`WorkItem`）。
> 依赖方向恒为 **application 描述「什么可并行、按什么键隔离」 → seelebridge 决定「怎么并行」**，框架不知道并发这件事。

> **v2 修订**：上面这行"seele 侧只加三个纯数据字段"是**v1 备选路径**的结论。
> **v2 主线（作业派发）下 seele 侧改动 = 0**：并行在作业系统内发生，loop 串行，hook 无需并发改造。
> 三个字段（身份/顺序/批大小）届时**仍可能是好设计**（它们让"若将来要 loop 内并发"变得便宜），但**不是本设计的必要条件**，不应作为前置。
> **v2 的第一件事是**：`Groups` 填表（A8/P0-2）+ 作业契约命名（P0-4）+ bash 服务端分类（P0-5）。

---

## 11. 全文锚点校正（本次逐条复核，基座 = vendor Seele v0.3.1）

复核方法：`Select-String` / `[System.IO.File]::ReadAllLines()` 取行号（本机 `Get-Content` 的中文重码会并行走，行号不可信）。

| 文档写的 | 实测 | 出现位置 |
|---|---|---|
| `hooks.go:22-29` | **`:24-31`** | A1 |
| `loop.go:323-411` | **`:327-415`** | A2、§2 标题 |
| `loop.go:328`（OnToolStart 填充） | **`:332`** | A3 行、§1.4、§4.1 |
| `loop.go:371`（OnToolComplete 填充） | **`:375`** | A3 行、§1.4、§4.1 |
| `loop.go:399`（`ToolCallID` 落历史） | **`:403`** | A3 |
| `loop.go:347,361`（telemetry 两处） | **`:351,365`** | A3 |
| `tool_hooks.go:374-401,408-410` | **`:414-441,448-450`** | A5 |
| `tool_hooks.go:403-406`（`tool-%d`） | **`:443-446`** | A4 |
| `tool_hooks.go:32,140,170,45,48,171` | **`:33`**（EnsureToolCallTranscriptLocked）**`:50`**（`Tool{ID,Status:"running"}`）**`:54`**（appendSessionMessage）；其余 3 处未逐一复核 | A6 |
| `router.go:574-588`（`scopedToolTimeout`） | **`:623-636`** | A10 |
| `tools.go:294-295`（chain/chainMeta） | **`:295-296`** | 设计文档 §事实 4、§4.3、修正表 |
| `tools.go:104`（`ToolEntry.Meta`） | `ToolEntry` 在 **`:95`** | 设计文档 §事实 2 |
| §5 表格「只读探针 ❌ 需新增」 | **已实现**：`async_probe.go:24-58`（`Router.AsyncRuns()`/`AsyncRunInfo`）+ W10 | 设计文档 §5 |

**未变的（复核通过）**：`go.mod:17`、`registry_state.go:24-30`（`AddInline` `:41-64`）、`limits.go:150-151,208-212,373`、`tools.go:194-200,295-296,366,373`、`runtime_tools.go:264`、`tool_hooks.go:238`、`fs/filesystem_actor.go:47,51,61`、`work_table_async.go` 全部锚点、`tableRows` 相关全部锚点。

> 影响评估：校正全部是**平移**，不改变任何结论（没有一处是「锚点指错了对象」，除了 §5 探针那条**实质过时**）。但本文档的开篇承诺是「所有现状陈述均带 `file:line`，可直接复核」——锚点漂移会持续消耗复核者的信任，所以按上表改。

---

## 12. 基线测试结果（改动前实跑，2026-09-26）

工具链：`go1.25.8 windows/amd64`，仓库 `G:\Program\go\seelex`，vendor 模式。

| 命令 | 结果 |
|---|---|
| `go build ./...` | exit 0 |
| `go vet ./...` | 无输出（干净） |
| `go test ./...` | **66 包 ok / 17 包无测试文件 / 1 包 FAIL**（根包 `ok 51.3s`） |

**唯一失败**：

```
--- FAIL: TestEveryGoPackageDirectoryHasReadme (0.36s)
    layout_test.go:275: Go package directory "vendor\\..." has no README.md
FAIL  github.com/RedHuang-0622/seelex/e2e  4.593s
```

- 失败目录 **255 个，全部在 `vendor\` 下**（`Select-String` 计数：255/255）。
- 成因（已核实）：`vendor/` 被 `.gitignore:39` 忽略且未被 git 跟踪（`git ls-files vendor` = 0），但该测试的 `skipped` 集合（`e2e/layout_test.go:243-246`）**不含 `"vendor"`** → 任何跑过 `go mod vendor` 的开发者本地跑 `go test ./...` 必红；CI 干净检出（无 vendor）通过。
- **判定：与本次设计无关的既有红**，但它污染基线——Phase 0/1 的任何失败都要先扣掉这条噪声才能归因。
- **建议 Step -1**：`skipped` 加 `"vendor": true`（一行，X8）。同文件的另一个 `skipped`（`:65-68`，`TestModuleReadmesMermaidBlocksAreStructurallyValid`）同样缺 `"vendor"`，建议一并加，避免同类复发。

**结论：除 `e2e` 这一条环境型红外，全仓基线是绿的**——Phase 0 的每一步都可以用「是否引入新红」作为验收判据。
