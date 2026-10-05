# 交接提示词：步骤② —— 并掉剩余的本质重复（**不并表**）

一次性工作包。用法：把下面「提示词正文」整段贴给下一个会话。

## 步骤① 验收结论（leader，2026-10-06，verdict = PASS）

- 基线：`a6f8f2b`（① 的全部交付已提交：11 files, +1816/−323），此前五个交接点 `3e5a167` / `82e4361` / `17f37c4` / `599aef4` / `c4492ab`。
- 判据 A–H 逐条按**源码**复核（不引用交付自述）：`contract.go:219/228/241/253/263`（四格 + 合成）＋ `:277` 断言；
  `tools/async_exec.go:197`（tools 表唯一同形的一格）；`workunit/progress.go:49/58/81/115/141/165`；
  `workunit_assembly.go:106-108`；机械门禁 `e2e/workunit_ports_test.go:170` + 阴性对照 `:224`；
  `git status` 证明 `async_*_test.go` 一条未改、`worktree/**` 未碰。删除清单：**无**（① 的分界就是"只定接口"，逐条说明了为什么注定没有）。
- **leader 独立门禁**（合并后自己重跑，与交付自述分开列）：

```text
build-exit=0    vet-exit=0
ok  github.com/RedHuang-0622/seelex/e2e                 0.286s   ← 含新机械门禁（TestWorkunitPortGate + 阴性对照）
ok  github.com/RedHuang-0622/seelex/seelebridge/workunit 1.153s
ok  github.com/RedHuang-0622/seelex/seelebridge/tools    13.597s
ok  github.com/RedHuang-0622/seelex/seelebridge/teamwork 1.111s
ok  github.com/RedHuang-0622/seelex/seelebridge/worktree 10.309s
ok  github.com/RedHuang-0622/seelex/seelebridge/session  0.317s
targeted-exit=0
```

## 现状与目标（② 的字符画 + 大白话）

### 现状

```
② 现状：剩下的"重复"不是存储，而是判据 / 读法 / 容器

  ① 「记录 → 进度」折算 ── 手写 3 处
       session/subagent_sessions.go（写侧 buildRecordLocked）─┐
       session/subagent_sessions.go（读侧 restoreLocked）   ─┤ 三份各写各的
       workunit_team_records.go（saveTeamUnitRecord / teamUnitRecords）─┘
  ② 阶段 JSON 编解码 ── 2 份：session 侧 []model.NodeStageLog ／ team 侧 teamUnitStages
  ③ 恢复说明容器 ── 2 处拼装（runtime_subagent_resume.go:386、workunit_team.go:383；workunit.RecoveryNote 已有）
  ④ teammate 阶段打点**恒单元素**（workunit_team_records.go 的 teamUnitStages）→ 时间线只有一格 = 界面"看不懂"
  ⑤ 在跑词表已收成一处（workunit/session.go:64），只剩两处写入点转调

  两张作业表：**按裁决保持两张**（不是缺陷，是分层事实）
     表① 框架 jobs.Manager = 团队作业面（jobs_manage builtin / Scope{Session,Subject} / Events() 扇出）
     表② Seelex tools 表   = 工具后台作业面（job_manage / 进程树 / 输出文件语义 / Windows RemoveAll 约束）
     ⚠ 迁到 Seele 的路 2026-10-01 已裁决失败（§12.4）：event.Sink 必须构造期定死，
       框架全局序号 append 不到按会话排序的事件流尾部 → "不再开工、§12.3 降级留档"
```

### 目标

```
② 目标：折算/编解码/容器各一处；两张表登记为分层事实（不搬）

   lifecycleHost（唯一生命周期实现，① 已落地）
     ├── 现场端口 ── WorktreeManager
     ├── 会话端口 ── NodeSessionStore
     ├── 编排闸门端口 ── Coordinator（含全仓唯一作业回收调用点）
     └── 读面：workunit.ProgressOf / EncodeStages·DecodeStages   ← 唯一一处折算（① 已定形）

   subagent 侧（session/subagent_sessions.go）──转调──▶ ProgressOf ＋ Encode/DecodeStages
   teammate 侧（workunit_team_records.go）     ──转调──▶ 同一套（阶段打点不再恒单元素）

   判据（缺一条不算完成）：
     · 同一份收尾脚本在两层跑出**同一份 Stage 序列**（跨层一致性用例，先红后绿）
     · 三处旧体删除或转调，删除清单写"文件:行 → 新唯一位置"
     · 两张作业表一动没动；workunit 的作业端口形状没有被改成"为合表服务"
```

**大白话（现状）**：作业面那两张表**不并了**——不是忘了，是你自己裁决过：那条路走过、认定失败
（框架的作业事件 sink 必须在构造期定死，而框架事件是全局序号，塞不进"按会话追加"的事件流）。
所以剩下的"重复"根本不是存储，而是**判据和读法**：把一条记录折算成"这件事跑到哪了"这件事，
三个人各写了一遍；阶段 JSON 的编解码两份；恢复说明的拼装两处；teammate 的阶段打点还只记一格，
所以界面上连时间线都画不出来。

**大白话（目标）**：① 已经把"唯一位置"立住了（`workunit.ProgressOf`、`EncodeStages`/`DecodeStages`），
② 就是把那三处手写**改成转调、删掉旧体**，并让 teammate 也走同一套 Stage。验证方式很直白：
同一份收尾脚本，在 subagent 和 teammate 上跑出来**同一份 Stage 序列**。
两张表一张都不搬：表① 服务团队作业面（`jobs_manage`）、表② 服务工具后台作业面（`job_manage`），
它们只是长得像——登记成"分层事实"，写进 `workunit/README.md` 的依赖方向段，别再当缺陷。

## 提示词正文

```
【任务】步骤②：把剩余的"判据/读法/容器"类重复并成一份 —— **不并两张作业表**。

==================== 一、前置（新会话必须知道的事实与环境） ====================
仓库：G:/Program/go/seelex（Windows + PowerShell；go 可用）。
基线：main 头 = 本文件所在提交（开工前 `git log --oneline -8` 核对：a6f8f2b = 步骤① 落地、
  3e5a167/82e4361/17f37c4/0beae37 = 交接文档、599aef4 = 读面端口、c4492ab = 父实现、e3594bf = merge 修复），工作区干净。
步骤① 已完成并已被 leader 复核 PASS（结构证据 + 独立门禁全绿）。它留下的可复用形状：
  - 作业面：`workunit/contract.go:219/228/241/253/263` 四格（提交/读数/控制/信号）+ 合成，`:277` 编译期断言；
    tools 表唯一同形的一格被钉住（`tools/async_exec.go:197`）。
  - 生命周期：`workunit_parent.go` 只持三格宿主端口；装配处 = `workunit_assembly.go`（`hostPorts` 唯一实现）。
  - 读面（**本轮的收编目标**）：`workunit/progress.go` 的 `Stage`/`EncodeStages`/`DecodeStages`/`ProgressOf`/`UnitReader`
    ——**目前没有生产调用者**（① 的未决项 §6.1 明说），本轮就是给它接线。
  - 机械门禁：`e2e/workunit_ports_test.go`（`TestWorkunitPortGate` + 阴性对照）。

必须先接受的裁决（否则会白干一轮）：
  `docs/arch/teamwork-leader-worker-architecture.md` **§12.4**：把异步面/作业面迁到 Seele `jobs.Manager`
  **已走过并认定失败**（`event.Sink` 必须构造期定死；框架 `event.Recorder` 单例 + 全局序号，
  而 Seelex 会话事件库按会话追加/排序 → append 不到尾部）。原文结论：**"本步不再开工，也不必等
  §12.3 的契约增补；§12.3 降级为留档，不构成待办。"** 所以：**不并表、不迁 Seele、不反向合一**
  （反向合一 = 让 teammate 搬离 `jobs.Manager`，会掏空框架的 `jobs_manage`，同属重新裁决）。

纪律：AGENTS.md §8「复用与单一实现纪律」是交付口径；§0 危险操作铁律 + 新增代码前读 MEMORY.md 归属决策；
  不得读取或提交 config/accounts.yaml 与 *.local.yaml；桌面纪律：不置顶/不覆盖窗口、不抢前台、不合成键鼠；
  作业输出一次性取全（销项后日志会被删）；看板不可信（todo_init 是「并入」语义，表里有历史重复行与一个 interrupted 旧行）。

已知例外（不要顺手改）：`workunit/classify.go` 依赖 worktree 两个哨兵错误（门禁已把它登记为唯一例外）；
  `sessionstore.NodeSessionRecord` 是契约自己声明的记录形状（不是"持具体类型"）；`workunit_team.go` 注册点仍持
  `teamwork.WorkerRequest`（派发载荷读数，收口不在本轮）。

==================== 二、先读哪些文件（唯一口径，按序） ====================
1. docs/arch/workunit-ports-and-assembly.md —— 本轮任务书：§2 端口清单、§3 装配矩阵、§5②（**已改正：并判据/读法/容器，不并表**）
2. docs/2026-10-06-workunit-jobs-port/step-1-delivery.md —— ① 的交付记录：接口签名对照、断言位置、未决项 §6.1–§6.5
3. docs/arch/workunit-progress-read-surface.md —— 读面勘定：真因（折算三份手写 + 打点恒单元素）、U2–U6 未决项
4. docs/arch/teamwork-leader-worker-architecture.md **§12.4（必读）** + §12.3（留档：六个最小 jobs 增补）
5. AGENTS.md —— §0 / §1 / §3 / §5 / §8
6. 代码现场（只读，先别改）：seelebridge/workunit/progress.go、workunit/contract.go、workunit_assembly.go、
   session/subagent_sessions.go（写侧 buildRecordLocked / 读侧 restoreLocked）、workunit_team_records.go
   （saveTeamUnitRecord / teamUnitRecords / teamUnitStages）、runtime_subagent_resume.go

==================== 三、做到什么程度算完成（判据 + 交付格式） ====================
做这些（任务书 §5② 四行表）：折算三份 → `ProgressOf` 一处；阶段编解码两份 → `Encode/DecodeStages` 一处；
  teammate 阶段打点恒单元素 → 走同一套 `Stage`；恢复说明容器两份 → `RecoveryNote` 一处。

判定标准（缺一条不算完成）：
 A. `ProgressOf` / `EncodeStages` / `DecodeStages` **有生产调用点**（给出文件:行），三处旧体删除或转调。
 B. 跨层一致性用例在仓：**同一份收尾脚本**在 subagent 与 teammate 上跑出**同一份 Stage 序列**；
    并给出**先红后绿**的红灯原文（这是本轮唯一能证明"只剩一份判据"的用例）。
 C. teammate 阶段打点不再恒单元素，或写清为什么保留并给判据（勘定 U3/U4 要正面回答）。
 D. 恢复说明容器只剩一处（`workunit.RecoveryNote`）；两处旧拼装删除或转调。
 E. 既有用例一条不改（改断言 = 越界；确实需要适配形状的在报告里逐条说明并给理由）。
 F. 原始读数：gofmt -l（改动文件）/ go build ./... / go vet ./seelebridge/... / go test ./e2e/ -count=1 /
    go test ./seelebridge/... -count=1（含 workunit、session、teamwork）。
 G. **两张作业表一动没动**（`jobs.Manager` 与 tools 表的行为零变化）；`workunit` 的作业端口形状没有被改成"为合表服务"。
 H. 未决项：勘定 U2（timeline 归计划、不在记录里补时间戳）/ U3 / U4 / U6 逐条回答或标"仍开放"。

非目标（做了即越界）：不合表、不迁 Seele、不反向合一；不碰前端读面渲染；不碰调度闸门；
  不动 seelebridge/worktree/**；不改 Seele 的 jobs 包。

红线（碰到就停手报告，不要硬做）：
 - 若"不并表就无法收编折算"——停手说明现状，别自己把表搬了（§12.4 是裁决，不是遗留项）。
 - 若发现文档口径与仓库现状冲突——先改文档并说明理由，不许另立一套实现。
 - 若折算合并会改变落盘/回灌语义（勘定 U3 明说有此风险）——先写红灯用例把语义差异固定下来，再决定并法。

【交付格式】1) 改动文件清单；2) **删除清单**（文件:行 → 新唯一位置）；3) 跨层一致性用例的红→绿原文；
   4) 命令原始读数；5) 两张表"一动没动"的证据（`git diff --stat` 里没有它们）；
   6) 未决项（U2/U3/U4/U6 逐条）。
```
