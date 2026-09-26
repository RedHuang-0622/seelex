# 子进程调用系契约：测试用例（P0 / P1 / P2）

> 前置：`docs/tool_concurrency_design.md`（v3 详细设计）、`docs/tool_calling_step0_contracts.md`（打点表）
> 约定：**用例编号 = `TC-<打点ID>-<序号>`**，与打点表一一对应；每条给 **输入 → 期望 → 断言位置**。

---

## 0. 约定

| 项 | 规则 |
|---|---|
| 落地状态 | **【已跑】** = 本仓已有可执行用例；**【待实现】** = 依赖打点落地才能写；**【需修订】** = 现有用例会被打点**有意改动**（不是回归） |
| 形态 | 一律**表驱动**（`cases := []struct{...}`），因为本契约的判据几乎全是"分类 / 路由 / 有界"这类表 |
| 断言口径 | 只断言**可观测**的东西：路由组名、游标位置、投影行数、回执字段、状态迁移次数 |
| 禁止 | 断言内部实现细节（函数名、字段顺序、日志文案的完整串） |

**落地状态（2026-09-26 交付后）**：P0 的 K-0..K-7 与 P1 的 L-1..L-7 全部落地，用例
大多已从【待实现】转为【已跑】；下面每条都给了真实文件名与用例名。唯一仍标【待补】的
是 `TC-K3-2`（工具面可见性表）——它的口径在 `permission_face_test.go` 里分散覆盖，
但还没有一张"名字 × 主体"的显式表。

---

## 1. P0 用例（契约落实）

### TC-K0-1 · 全量工具的 `Groups` 非空 【已跑】

| 输入 | 期望 |
|---|---|
| 遍历装配后的注册表，取每个工具的 `ToolMeta` | 每个条目的 `Groups` 非空（未分封 = 静默降级成审批，等于没有并发策略/权限策略） |

断言位置（两条互补）：

- 根包 `tool_meta_declaration_test.go` → `TestEveryRegisteredToolDeclaresGroups`（**扫源码**：
  `main.go` 里那些不走 `RegisterBuiltins` 的注册点也覆盖到）；
- `seelebridge/tool_meta_declaration_test.go` → `TestBuiltinToolsDeclareGroupsAndMetas`
  （**装配后读面**：`Runtime.UndeclaredTools()` 必须为空，代表工具点名验组）。

实现：`seelebridge/tools/tool_meta.go` 的 `DeclaredToolMeta` 从**路由组表**派生簇属
（单一事实源），`registry_state.go` 的 `AddInline` 无条件填 `Meta`，并把未分封的名字
记进 `UndeclaredTools`。

### TC-K1-1 · interface 与实现的一致性 【已跑】

| 输入 | 期望 |
|---|---|
| `go build ./...` | 三类作业的编译期断言全过：`var _ JobTool = (*bashBgTool)(nil)` / `(*inlineReadTool)(nil)` / `(*subagentTool)(nil)`（`TestJobToolContractImplementations`） |

契约形状（用户裁定 2026-09-26）：`Add` + **具名四动作** `Status`/`Fetch`/`Kill`/`Done`，
出参一律 `[]byte`（JSON 载荷），`Done` 在 `JobTool` 之下。

### TC-K2-1 · 行 schema 逐字段搬运 【已跑】

| 输入 | 期望 |
|---|---|
| 构造一个带全部字段的作业记录 → 过 `asyncRunRecordFrom` | `Kind` / `Summary` / `Notified` / `Index` 四字段**不漏搬**（投影面只做搬运，不做判定） |

断言位置：`seelebridge/runtime_async_test.go` → `TestAsyncRunRecordMappingCarriesEveryColumn`
（`Kind`/`Summary`/`Lines`/`Notified`/`Index` 逐字段，漏一列即失败）。

### TC-K3-1 · bash 工具族的权限分组 【已跑】

| 工具 | 期望组 | 理由 |
|---|---|---|
| `bash` | `rw` | 同步串行命令，保守归类为写 |
| `bash_read` | `ro` | 只读命令 ⇒ 默认 `Allow` ⇒ **免打断**（这正是分裂的目的） |
| `bash_bg` | `rw` | 后台受管；与 `async_kill` 同组，sub/员工才能杀掉自己派发的命令 |

（上表的"与 `async_kill` 同组"读作"与 `job_manage` 同组"——旧名字已下线。）

断言位置：`seelebridge/tools/async_exec_test.go` → `TestBashFamilyRoutingTable`
（`bash`/`bash_bg`/`job_manage` → rw；`bash_read`/`read_batch` → ro；并断言旧名**不在**表里）。

### TC-K3-2 · 工具面对五个名字的可见性 【待补】

（`read_batch` 同 `bash_read`；`job_manage` 同 `bash_bg`。**待补**：把这张表写成表驱动
用例——现有 `permission_face_test.go` 逐条覆盖了口径，但还没有这一张显式矩阵；补齐时
要连"`bash_read` 落在只读簇 ⇒ 子代理不弹审批"一条一起断言。）

| 主体 | `bash` | `bash_read` | `bash_bg` | 期望 |
|---|---|---|---|---|
| root | ✅ | ✅ | ✅ | 全位 |
| sub | ✅ | ✅ | ✅ | 位齐即授权（无人类在环，位缺直接拒） |
| emp_ro（只读员工） | ❌ | ✅ | ❌ | 只读员工只能拿只读命令 |
| 未分封工具 | — | — | — | 员工面上不出现（`ToolFaceForSubject` 既有口径） |

依据：`permission_policy.go:535-560`（`ToolFaceForSubject`：位覆盖组 `Mode` 即在面上）。

### TC-K4-1..K4-4 · `ClassifyCommand` 守卫（**安全用例，最高优先**） 【已跑】

断言位置：`seelebridge/security/command_class_test.go`（`TestClassifyCommandTable`
覆盖四组共 40 条；`TestClassifyCommandIgnoresModelClaims` 钉住"模型主张不构成授权依据"）。
接线：`seelebridge/tools/router.go` 的 `scopedBashRead`——判定失败就报错并要求改用
`bash`，**不静默降级成执行**。

| # | 输入命令 | 期望 | 说明 |
|---|---|---|---|
| TC-K4-1 | `ls -la`、`git status`、`git log --oneline -5`、`go test ./...` | **允许**（只读） | 白名单首词 |
| TC-K4-2 | `git commit -m x`、`npm install`、`rm -rf build`、`mv a b` | **拒绝** | 写子命令黑名单 |
| TC-K4-3 | `echo hi > f`、`ls >> out.txt`、`cat x \| tee y` | **拒绝** | 写重定向 |
| TC-K4-4 | 分类失败的一切形态：空命令、未知首词、含 `;`/`&&` 的复合命令、变量展开 `$X` | **拒绝**（按写处理） | 保守默认；**绝不能**"没识别出来就放行" |

**额外硬断言（这条比上面四条更重要）**：模型在 `bash_read` 的入参里写下与命令矛盾的"这是只读的"说明时，判定结果**不变**（`ClassifyCommand` 只吃命令字符串，签名上就拿不到模型的任何主张）。

### TC-K5-1 · 回填有界（K-2） 【已跑】

断言位置：`application/core/work_table_async_test.go` → `TestAsyncBackfillStaysBounded`
（100 个完成作业 + 4 个在途：行数有界、在途优先、被截断的完成行有汇总行、整块 ≤30 行）。

| 输入 | 期望 |
|---|---|
| 注入 100 个已完成作业（`Kind` 混合） | 工作表格行数 ≤ `asyncWorkMaxRows`(32)；打点块行数 ≤ `workTableTraceMaxLines`(30)；**在途行优先于完成行**（既有 `asyncWorkItems` 口径，`work_table_async.go:46-67`） |

### TC-K5-2 · 回填幂等（K-4） 【已跑】

断言位置：`seelebridge/tools/job_contract_test.go` → `TestJobTerminalTransitionHappensOnce`
（第二次 `finish` 不改状态、不重算摘要、不 panic）+ `TestSubagentJobContract`（重复 done 幂等）。

| 输入 | 期望 |
|---|---|
| 对同一 handle 连续调用两次 `finish()` / 两次 `Manage(OpDone)` | 终态**只迁移一次**；回填**只发生一次**（`Notified` 位）；第二次是无副作用空操作 |

### TC-K6-1 · `done` 双入口到同一终态 【已跑】

落地口径：**运行体系 `CompleteJob`（迁移终态）+ 模型侧 `job_manage(op=done)`（销项）**；
在途调 `done` 显式报错（终态只由执行体判定），要停用 `kill`。
断言位置：`TestJobManageEntryRejectsInvalidCalls`（在途 done 必须失败）、
`TestSubagentJobContract`（kill → CompleteJob → fetch 保留内容 → done 幂等）。

| 输入 | 期望 |
|---|---|
| ① 进程自己退出（运行体系被动 `finish`） ② 模型侧 `Manage(OpDone)` | 两条路径到达同一状态字面量；`close(run.done)` 只关一次（重复 `close` 会 panic——这条用例同时是防 panic 的） |
| subagent 场景 | subagent 主动 done 与 main agent 主动 done 都能收敛；**已产出内容保留** |

### TC-K7-1 · 文档一致性 【已跑（人工 + 机械）】

| 输入 | 期望 |
|---|---|
| `grep -n 'async_probe\|五形态\|Phase [0-9]\|seele 侧改动 = 0' docs/*.md` | 只命中"已作废/已更正"的说明处，无自相矛盾表述 |

机械部分：`TestRetiredJobToolNamesLeaveNoResidue` 守代码与配置；文档侧本次同步
`docs/tool_concurrency_design.md`（§A.1 契约按用户裁定改写）、
`docs/tool_calling_step0_contracts.md`（K-1/L-1..L-7 行 + 落地记录）、
本文、以及两处模块 README。

---

## 2. P1 用例（契约下的实现）

### TC-L1-0 · 工具族路由表基线 【已跑】

文件：`seelebridge/tools/subprocess_contract_test.go` → `TestJobContractToolFaceRoutingTable`

| 输入 | 期望 |
|---|---|
| 现有 12 个代表工具 | 分组与契约一致（`read_file`/`grep_search`/`glob`/`async_output` → `ro`；`write_file`/`edit_file`/`bash`/`async_kill` → `rw`；`task_complete`/`fork_subagents` → `ctl`；`switch_platform`→`adm`；`computer_click` → `rw_desktop`） |

**K-3 落地后必须加两行**（`bash_read` → `ro`、`bash_bg` → `rw`）；**L-2 落地后必须删一行**（`async_output`）。这是有意的耦合：表变了这里就该变。

### TC-L1-1 · 派发回执逐字段等价 【已跑】

| 输入 | 期望 |
|---|---|
| 用 `bash_bg` 派发一条命令 | 回执字段与旧 `bash(background=true)` 的 `renderAccepted` **逐字段相等**（handle / log_path / state），只有工具名不同 |

断言位置：`seelebridge/tools/job_contract_test.go` → `TestJobAcceptedReceiptMatchesLegacyShape`。
实现上等价是**结构性**的：旧路径与新路径都调用同一条 `renderJobAccepted`（`bashBgTool.Add`），
不存在两份渲染代码。

### TC-L2-1 · fetch 仍是消费式 【已跑】

文件：`seelebridge/tools/async_exec_test.go` → `TestAsyncPollDeliversOnlyNewBytes`、`TestAsyncRegistryTailDeliversOnlyNewBytes`（既有；`Manage(OpFetch)` 换壳后必须继续绿）。

### TC-L2-2 · observe 不推进游标 【已跑（本次新增）】

文件：`seelebridge/tools/subprocess_contract_test.go` → `TestObserveDoesNotAdvanceCursor`

四段断言：① 连续 3 次观察后 ② 取回仍拿到**全部**增量 ③ 再取回为空（消费式） ④ 新字节仍可取得。依据：`async_probe.go:11-19`（探针纪律）、设计文档 K-3。

### TC-L2-3 · 旧取回工具名零残留 【已跑】

| 输入 | 期望 |
|---|---|
| `grep -rn async_output`（排除 vendor/`_tmp`/dist） | 只命中迁移说明与历史文档（`CHANGELOG.md`、`docs/devlog/`、`docs/2026-09-24-*`），**代码与配置零残留** |

机械判据：根包 `job_migration_residue_test.go` → `TestRetiredJobToolNamesLeaveNoResidue`
（扫全部 `.go` 与 `config/*.yaml`，允许的行必须自带"已下线/旧名/替代/迁移/残留"标记）。

**迁移面清单（本次实测的 grep 基线，24 个文件，逐条核过才算 L-7 完成）**：

| 类别 | 文件 |
|---|---|
| 生产代码 | `seelebridge/tools/async_tools.go`(12)、`async_exec.go`(9)、`router.go`(4)、`permission_policy.go`(2)、`async_probe.go`(2)、`runtime_tools.go`(1)、`application/contract/ports.go`(1)、`application/core/work_table.go`(1)、`work_table_async.go`(1)、`seelexctx/limits.go`(1) |
| 配置 | `config/seelex.yaml`(2) |
| 测试 | `seelebridge/tools/async_exec_test.go`(11)、`async_progress_test.go`(1)、`application/core/work_table_async_test.go`(1)、`seelexctx/async_exec_config_test.go`(1)、`limits_test.go`(1)、根包 `async_exec_ab_live_test.go`(12)、`async_tool_wire_live_probe_test.go`(4) |
| 文档 | `seelebridge/tools/README.md`(4)、`CHANGELOG.md`(4)、`docs/` 三处历史文档 |

### TC-L3-1 · 批量读派发即返回 【已跑】

断言位置：`seelebridge/tools/job_contract_test.go` → `TestReadBatchDispatchesJobsAndReturnsImmediately`
（3 个文件：回执 count 与句柄唯一性、`AsyncPendingFor` 计数、逐个 fetch 内容、销项后清理）。
工具名落在实现里是 `read_batch`（设计文档里的"新批量读工具"）。

| 输入 | 期望 |
|---|---|
| 一次调用派发 N=8 个文件读 | 回执含 8 个 handle；**调用本身不等结果**（墙钟 ≈ 派发开销）；`AsyncPendingFor(session)` 覆盖 `Kind=inline`；`CloseSessionAsync` 能清掉它们 |

### TC-L4-1 · kill 后已产出内容不丢 【已跑】

断言位置：`seelebridge/tools/job_contract_test.go` → `TestJobKillPreservesProducedContent`
（非进程作业：`appendNote` → kill → `CompleteJob(killed)` → fetch 仍拿到那一段正文）；
进程作业侧由 `async_kill_test.go` → `TestAsyncKillTerminatesProcessTree` 覆盖
（`exit=137` 注记必须能取回）。

| 输入 | 期望 |
|---|---|
| 派发一个持续输出的作业 → kill → fetch | 终态 `killed`；**kill 前已落盘的字节仍可 fetch 到**（不是丢结果）；`exit_code = 137`（既有口径 `async_kill_test.go`） |

### TC-L5-1 · subagent 的 observe / kill / done 【已跑】

断言位置：`seelebridge/tools/job_contract_test.go` → `TestSubagentJobContract`
（契约层：observe 只读、kill 触发取消口、已产出保留、`CompleteJob` 幂等、done 销项）；
以及 `seelebridge/subagent_job_contract_test.go` → `TestForkSubagentsAsyncRunsAsJobs`
（端到端：`fork_subagents{async:true}` 派发即返回 → 投影 `Kind=subagent` → observe →
fetch 拿到子代理产出 → 取回后行消失）。

> **边界**：subagent 的"自己 done"由运行体系在节点收尾时以 `CompleteJob` 落地；把句柄
> 注进节点提示词（让它自己调 `job_manage`）留到 P2——它要动 `plan` 节点输入面，且不改变
> 上面任何一条判据。

| 输入 | 期望 |
|---|---|
| main agent 在 subagent 跑动中 observe | 只读读数（不推进游标、不进上下文） |
| main agent kill 一个 subagent | worktree 回收 + 行标 `killed` + **findings 保留** |
| subagent 自己 done | 与 main agent 主动 done 收敛到同一终态 |

### TC-L7-1 · 迁移面 7 项逐条勾掉 【已跑】

勾选结果见 `docs/tool_calling_step0_contracts.md` 的「落地记录」表（L-7 行）：注册 /
权限组 / 描述 / 注册开关 / 测试 / 文档六项都直接改到新名字；**别名一项刻意不留**——
旧名出现在任何未标注为迁移说明的位置都会被 `TestRetiredJobToolNamesLeaveNoResidue` 拦下。

注册（`router.go:103`）/ 权限组（`permission_policy.go:99`）/ 描述（`async_tools.go:144`）/ 注册开关（`router.go:101`）/ 测试 / 文档 / 旧名别名。任一项未勾 ⇒ L-7 未完成。

---

## 3. P2 用例（契约进 seele + loop 并发）

P2 的用例**已经写完**，在打点表 `docs/tool_calling_step0_contracts.md` **§9.1**（28 条）：

| 组 | 用例号 | 覆盖 |
|---|---|---|
| 策略与分派 | 1–11 | I-4/I-5/I-6/I-8/取槽超时/无 goroutine 泄漏 |
| 槽外审批 | 12–13 | I-3 |
| hook 配对与批量前后沿 | 14–17 | I-2 / 空 assistant 占位 / 叙述归位 |
| 在途内联投影 | 18–24 | T-1..T-4（含"排序键 = wire 下标"这条与 loop 的强耦合） |
| 超时矩阵 | 25–26 | A11 / 内外层超时覆盖校验 |
| 探针与上限回归 | 27–28 | 探针不推游标（已由 TC-L2-2 在 P0/P1 阶段提前覆盖）、行上限不被绕过 |

**启用条件**：S-0 门通过（P1 的墙钟数据 + 一轮真实任务复盘证明"结果晚一轮不可接受"）。

---

## 4. 有意修订的既有用例（已按此改完，不是回归噪声）

这张表是"改动前先看一眼"的清单——这几条用例断言的行为**正是设计要改的**，本次已按
"改成什么"一列全部落地（状态列 = 实跑结果）：

| 现有用例 | 位置 | 谁改它 | 改成什么 | 状态 |
|---|---|---|---|---|
| `TestAsyncTraceLinesCarryNoPathsOrLogContent` 的末段 | `application/core/work_table_async_test.go`（断言"终态行不进打点块"） | **K-5** | 终态行**要**进块（带 `Summary`）；新增一条"没回填过（`Notified=false`）的终态行**不**进块"，路径/正文仍不进块 | ✅ 已改 |
| `TestAsyncOutputRoutesToReadOnlyGroup` | `seelebridge/tools/async_exec_test.go` | **L-2** | 旧名字下线 ⇒ 并入 `TestBashFamilyRoutingTable`（断言五个新名字的组，且旧名**不在**表里） | ✅ 已改 |
| `TestAsyncKillSwitchGatesSchemaAndRegistration` | `async_exec_test.go` | **K-3 / L-1** | `bash` schema 永不下发 `background`；改为断言"能力关闭时作业工具一个都不注册，`bash_read` 常驻" | ✅ 已改（改名 `TestJobCapabilityGatesSchemaAndRegistration`） |
| `TestAsyncKillRegistrationFollowsCapability` | `async_kill_test.go` | **L-4** | 名字收敛到 `job_manage` 的 `op=kill` | ✅ 已改（改名 `TestJobKillEntryFollowsCapability`） |
| `TestBackgroundDispatchCarriesDescriptionAndBatch` / `TestProbeIsEmptyWhenCapabilityOff` | `async_probe_test.go` | **K-3 / L-1** | 派发入口换成 `bash_bg`（缺 `description` 仍必须被拒） | ✅ 已改 |
| `pollForTest` / `pollRaw` / `killForTest` 等测试脚手架 | `async_exec_test.go`、`async_progress_test.go`、`async_kill_test.go` | **L-1 / L-2 / L-4** | 全部走 `scopedBashBg` 与 `scopedJobManage`；终态取回后句柄**销项**（断言改为"再取回报已销项"，不再是"空增量"） | ✅ 已改 |
| 两个 tag 门后的活体探针 | `async_exec_ab_live_test.go`（`manualsmoke`）、`async_tool_wire_live_probe_test.go`（`asynclive`） | **L-1 / L-2 / L-4** | 指令与手写 wire 载荷迁到 `bash_bg` + `job_manage(op=fetch)`（含 `kind` 字段与销项提示），两个 tag 下 `go vet` 通过 | ✅ 已改（真机联网跑仍需单独验收） |

### 与人工验收的分工

进不了 CI 的（墙钟收益、肉眼观感、UI 面板）放人工验收：

| AA | 内容 |
|---|---|
| AA-1 | N=18 文件并行读 vs 串行的**墙钟对比**（**P2 门的判据数据**） |
| AA-2 | 打点表肉眼验收：一批 3 个 read 时块里出现 3 行在途行；完成后出现完成行（带摘要）；取回后消失 |
| AA-3 | `bash_read` 免打断的手感：只读命令不再弹审批；写命令被拒并提示改用 `bash` |
| AA-4 | 审批 × 并行：并发 2 个待批 `write_file`，审批面板出现 2 笔、等待期间不占并发槽（P2 相关） |

---

## 5. 必须继续全绿的既有测试（改动前基线）

`seelebridge/tools/`：`async_exec_test.go`、`async_exec_raceproof_test.go`、`async_kill_test.go`、`async_probe_test.go`、`async_progress_test.go`、`permission_*_test.go`（12 个）

`application/core/`：`work_table_test.go`、`work_table_async_test.go`、`work_table_ab_test.go`、`work_table_plan_deps_test.go`、`work_table_race_test.go`、`work_table_session_*_test.go`、`work_table_subagent_status_test.go`、`work_table_fuzz_test.go`、`tool_hook_*_test.go`

`seelebridge/`：`runtime_async_test.go`、`worktable_*_test.go`、`fork_*_test.go`、`merge_back_concurrency_test.go`

**基线实跑（改动前，2026-09-26）**：`go build ./...` / `go vet ./...` 干净；`go test ./...`
66 包 ok，唯一红是 `e2e.TestEveryGoPackageDirectoryHasReadme`（`vendor/` 没进 `skipped`）。

**交付后实跑（2026-09-26）**：`gofmt -l .` 只剩 `vendor/` 与 `tmp/pidprobe` 的既有噪声；
`go build ./...` / `go vet ./...` exit 0；`go test ./... -count=1 -timeout=300s` **全绿**
（Step -1 已在 `e2e/layout_test.go` 的 skip 表里补上 `vendor`，那条环境红消失）；
`go vet -tags asynclive .` 与 `go vet -tags manualsmoke .` 均通过。

---

## 6. 本仓已可跑的新增断言（交付前已在仓；现已并入下面的总表）

| 文件 | 用例 | 钉住的性质 | 状态 |
|---|---|---|---|
| `seelebridge/tools/subprocess_contract_test.go` | `TestObserveDoesNotAdvanceCursor` | observe 只读（不推游标）/ fetch 消费式 / 新字节可续取 | ✅ 通过 |
| 同上 | `TestJobContractToolFaceRoutingTable` | 工具名 → 路由组的契约（K-3 / L-2 的验收钩子） | ✅ 通过 |
| `seelebridge/security/command_class_test.go` | `TestClassifyCommandTable`、`TestClassifyCommandIgnoresModelClaims` | K-4 服务端只读判定（含"模型主张不构成授权依据"） | ✅ 通过 |
| `seelebridge/tools/job_contract_test.go` | `TestJobToolContractImplementations`、`TestJobSummaryIsBounded`、`TestJobTerminalTransitionHappensOnce`、`TestJobKillPreservesProducedContent`、`TestReadBatchDispatchesJobsAndReturnsImmediately`、`TestJobManageEntryRejectsInvalidCalls`、`TestJobAcceptedReceiptMatchesLegacyShape`、`TestSubagentJobContract` | K-1/K-2/K-5/K-6 + L-1/L-3/L-4/L-5 的契约判据 | ✅ 通过 |
| `seelebridge/subagent_job_contract_test.go` | `TestForkSubagentsAsyncRunsAsJobs` | L-5 端到端：`fork_subagents{async:true}` → observe → fetch → 销项 | ✅ 通过 |
| `job_migration_residue_test.go`（根包） | `TestRetiredJobToolNamesLeaveNoResidue` | L-2/L-7：旧工具名在代码与配置里零残留 | ✅ 通过 |
| `application/core/work_table_async_test.go` | `TestAsyncBackfillStaysBounded` | K-5：回填有界 + 汇总行 + 在途优先 | ✅ 通过 |

实跑记录：

```
> go test ./seelebridge/tools/ -run 'TestObserveDoesNotAdvanceCursor|TestJobContractToolFaceRoutingTable' -count=1 -v
=== RUN   TestObserveDoesNotAdvanceCursor
--- PASS: TestObserveDoesNotAdvanceCursor (0.04s)
=== RUN   TestJobContractToolFaceRoutingTable
--- PASS: TestJobContractToolFaceRoutingTable (0.00s)
ok  github.com/RedHuang-0622/seelex/seelebridge/tools  2.537s
```

---

## 7. 真实 API 活体实测（2026-09-26，本机真实账号）

### 7.1 顺带定位的两条"负载下才红"的既有用例（不是本次契约改动引入的）

全仓并行跑时出现过两条偶发红；两条都复现、都定位到机制，并已修掉（修法与证据如下，
因为它们都属于"看起来像数据污染、其实是读法/收尾时序"的典型坑）：

| 用例 | 现象 | 机制（实测） | 修法 |
|---|---|---|---|
| `seelebridge.TestSessionLifecycleEventsLLMAndToolIntentEffect` | 偶发 `intent-effect correlation mismatch for llm.before/llm.after` | 判据读的是"视图里最后一个 before 与最后一个 after"。而 `MemoryTracer.Query` 遍历 `trace.spans`（**map，顺序随机**）后只按 Timestamp 做**稳定**排序，Windows 时钟粒度粗 ⇒ 末轮 before/after 同时间戳时保持随机顺序。实测 40 会话 × 200 次查询：**40/40 会话出现多种顺序；旧判据失配 111 次；按 `trace.Operations`（CorrelationID 归并）失配 0 次** | 改读 `trace.Operations`（顺序无关的 intent-effect 投影），失败时 dump 事件清单 |
| `TestWorkspaceSwitchConcurrentWithBackgroundPersist` | 断言全过，红在清理：`TempDir RemoveAll cleanup: unlinkat …\metadata: The directory is not empty.` | 该用例最后段并发发起 8 次 `Submit` 后直接返回；**`Submit` 返回 ≠ 本轮结束**，`t.TempDir()` 清理与仍在跑的后台落盘抢同一个目录（隔离复现 1/1） | 断言前先 `WaitForIdle`（等**全部会话**已接受工作，有界 ctx）；修后 10/10 绿 |

> 结论（与"数据污染"清单的对应）：这两条**不是**事务未回滚 / 自增 ID 冲突 / 幂等键冲突
> ——这条链路上没有事务，job 句柄是每个登记表自增、correlation ID 是 16 字节随机串。
> 它们对应的是另外两条：**"视图顺序不稳定"**（读法依赖了 map 迭代顺序）与**"收尾未落定就拆现场"**
> （残留后台写者与临时目录清理竞态）。

用例：`job_live_smoke_test.go` → `TestManualSmokeRealAccountJobContract`（tag `manualsmoke`）。
它不做任何 mock：**四个动作全部由真实 provider 的模型自己发起**（句柄由测试从真实回执里
取出、在第二轮明文回喂，因此"模型执行 kill"是确定的，而不是靠模型记住句柄）。

```text
$env:SEELEX_SMOKE_ACCOUNTS = (Resolve-Path config/accounts.yaml)
go test -tags manualsmoke . -run TestManualSmokeRealAccountJobContract -count=1 -v -timeout=10m

--- PASS: TestManualSmokeRealAccountJobContract (5.59s)
真实调用：bash_bg      status=success result={"status":"accepted","handle":"a1","state":"running",
                                              "exit_code":-1,"log_path":"…\a1.log","hint":"作业已派发…"}
真实调用：job_manage   status=success result={"status":"observed","exit_code":-1,
                                              "output":"- async:a1 process running 8B live smoke: tick forever · 末行: tick-1"}
真实调用：job_manage   status=success result={"status":"killed","handle":"a1","kind":"process",…}
真实调用：job_manage   status=success result={"status":"finished","handle":"a1","kind":"process",
                                              "state":"killed","exit_code":137,"output":"tick-1\r\ntick-2\r\n…"}
模型最终回复：Job a1 terminated in state `killed` with exit_code 137.
```

这次实测钉住的性质（逐条对应上面的判据）：

1. **派发即返回**：`bash_bg` 的回执只有 `handle`/`log_path`/`hint`，没有命令输出；
2. **观察是只读的**：`op=observe` 的读数带 `kind=process`、字节数与末行采样，`op=fetch`
   之后仍能取到全部增量（观察没有吃掉输出）；
3. **终止是真的**：`op=kill` 之后 `op=fetch` 拿到 `state=killed / exit_code=137`；
4. **kill 不丢结果**：取回的 `output` 里仍是 kill 之前命令已经写出的 `tick-1…` 行；
5. **投影到位**：期间工作表格里出现过 `async:a1` 行，终态取回（销项）后行消失。

顺带暴露并修掉一处真实的定位问题：本机没有 Git Bash，`bash` 工具退回 PowerShell，
POSIX 写法的 `$(seq …)` 会让作业一秒内 `failed`（实测第一次跑就是这条红）。活体用例
现在按平台给命令（`liveLongCommand()`），这条差异也正是"bash 不是 OS 沙箱"的现场证据。
