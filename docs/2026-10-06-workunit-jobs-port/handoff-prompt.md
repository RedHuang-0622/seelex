# 交接提示词：workunit 作业端口（步骤①：只定接口，不迁移）

一次性工作包。用法：把下面「提示词正文」整段贴给下一个会话（新会话没有本次上下文，所以它自带前置、读哪些文件、完成判据）。

## 审查结论（leader 于 2026-10-06，供下一会话建立上下文）

- **main = `0beae37`（本交接文档那次提交；其后 main 只会前进这一笔）**，三件活都已合并且门禁全绿（`build-exit=0` / `vet-exit=0` / `seelebridge` 全包 `ok` / e2e 布局 `ok`）。
- **a1（父实现）**：`workunit.Lifecycle` 契约面（`seelebridge/workunit/contract.go`）+ `lifecycleHost`（`workunit_parent.go:51` 编译期断言）；
  两个注册点退化成纯转发（`workunit_node.go` 58 行 / `workunit_team.go` 127 行，方法体全是 `return u.host.Xxx(ctx, u.read)`）；
  策略收口成同一个析构（`Immediate` 调 `lifecycle.Reclaim`，`AtTeamClose` 返回 `nil`）；在跑词表收成 `workunit/session.go:64` 一处；
  两个注册点已接生产链（`runtime_teamwork_items.go:42`、`workunit_parent.go:443/462`）。
- **a2（merge 踢人）**：红灯用例 `seelebridge/worktree/worktree_merge_kickback_test.go`；根因 `worktree_manager.go:498`
  （合并以 cwd = 主工作区执行，而落后判定/变基目标读的是现场记录的 `MainBranch`）；修法 `alignMergeTarget`（`:497/:513/:528`）。
- **a3（读面勘定）**：`docs/arch/workunit-progress-read-surface.md`；真因是"记录→进度折算"三份手写 + teammate 阶段打点恒单元素，
  不是形状不统一。
- **因此 `seelebridge/workunit/contract.go` 现在是空的**，步骤① 可以开工。
- 教训（已记纪律）：a1/a2 的交付尾巴丢了——终态作业销项后 job 日志会被删；**取回要一次性拿全**。

## 提示词正文

```
【任务】步骤①：把 workunit 的作业端口从「只有 Reclaim」扩成完整作业面——只定接口，不迁移。

==================== 一、前置（新会话必须知道的事实与环境） ====================

仓库：G:/Program/go/seelex（Windows + PowerShell；go 可用）。
基线：main 头（本交接文档提交之后的头；开工前先 `git log --oneline -5` 核对，应看到
  0beae37 = 交接文档、599aef4 = 读面端口落定、c4492ab = 父实现、e3594bf = merge 踢人修复 四个交接点），
  工作区干净。三件前置活都已合并、门禁全绿：
  - a1 父实现：workunit.Lifecycle 契约面 + lifecycleHost 唯一实现（workunit_parent.go:51 编译期断言），
    两个注册点纯转发（workunit_node.go 58 行 / workunit_team.go 127 行），已接生产链
    （runtime_teamwork_items.go:42、workunit_parent.go:443/462）。
  - a2：merge 踢人修复（worktree_manager.go:497 alignMergeTarget + 红灯用例 worktree_merge_kickback_test.go）。
  - a3：读面勘定（docs/arch/workunit-progress-read-surface.md）。
  => 所以 seelebridge/workunit/contract.go 现在空着，可以直接动；本轮不需要先合任何分支。

必须先接受的两个既有事实（否则会走错路）：
  1. 「作业面」现在有两张表：Seele 框架的 jobs.Manager（teammate 在用，`jobs.Scope{Session,Subject}`）
     与 tools 自建 async 表（subagent / bash_bg / read_batch / job_manage 在用，`JobSpec.SessionID`）。
     本轮**只让两者接口同形**，一张表都不搬——搬表是步骤②（CHANGELOG.md:112-116 已登记七点不一致）。
  2. `github.com/RedHuang-0622/Seele/jobs` 是 vendored 外部契约，本轮**不改它**；沿用仓库既有手法
     「窄端口 + 结构上满足 + 编译期断言」（先例：contract.go 的 Jobs 端口与 SessionLedger 端口）。

纪律（会用来验收你的交付）：
  - AGENTS.md §8「复用与单一实现纪律」是交付口径；新增代码前还要读 MEMORY.md 的「新功能归属决策」。
  - AGENTS.md §0：任何删除/覆盖类操作先读 MEMORY.md、先中文预警、先确认；不得读取或提交 config/accounts.yaml 与 *.local.yaml。
  - 桌面纪律：不置顶/不覆盖窗口、不抢前台、不合成键鼠。
  - 作业输出一次性取全（大 max_bytes、别急着销项）；销项后日志会被删。
  - 看板不可信：todo_init 是「并入」语义，表里有历史重复行与一个 interrupted 旧行；口径以文档为准。

==================== 二、先读哪些文件（唯一口径，按序） ====================

1. docs/arch/workunit-ports-and-assembly.md
   —— 父/端口清单（§2）、装配矩阵（§3）、判据（§4）、两步计划 ① 六项（§5）。**这就是本轮任务书。**
2. docs/arch/workunit-single-lifecycle-one-implementation.md
   —— §3 接口先行、§7 纪律（判重/交集）、§8 交集与析构函数、§10 红灯先行。口径冲突以它为准。
3. docs/arch/workunit-duplication-inventory.md
   —— 现状锚点（文件:行）：同一件事在仓库里有几份实现。
4. docs/arch/workunit-progress-read-surface.md
   —— 读面勘定：两层共用一张 sessionstore.NodeSessionRecord + 一个 workunit.SessionLedger；
     真因是「记录→进度折算」三份手写；建议的 progress.go 形状。
5. AGENTS.md（§0 危险操作、§1 事实来源、§3 模块 README、§5 验证命令、§8 复用纪律）。
6. 代码现场（读，不要先改）：seelebridge/workunit/contract.go（Jobs / Lifecycle / FinishPolicy）、
   seelebridge/workunit_parent.go、seelebridge/workunit_node.go、seelebridge/workunit_team.go、
   seelebridge/tools/job_contract.go、seelebridge/tools/job_subagent.go、seelebridge/workunit/README.md。
7. CHANGELOG.md:104-120 —— 步骤② 的七点不一致原文（本轮只看，不动手）。

==================== 三、做到什么程度算完成（完成判据 + 交付格式） ====================

做这六件（明细见 ports §5 ①）：
1) contract.go：Jobs 从只有 Reclaim 扩成完整作业面（Submit / Status / Peek / Kill / Done / Reclaim(scope) / Events / Snapshot，
   只收现在真正用到的几家）；保留 var _ Jobs = (jobs.Manager)(nil)；注释写明 Peek=增量读不推进游标、
   Snapshot=全量读、Reclaim 按 scope 回收。
2) tools 那张 async 作业表补「结构上满足」的编译期断言（两个父实现同形被编译器钉住）；不改它的行为。
3) 装配表落地一版：生命周期实现只持端口，不持 teamwork / worktree / session 的具体类型。
4) 读面定形：新增 seelebridge/workunit/progress.go（Stage / EncodeStages·DecodeStages / ProgressOf / UnitReader）；
   三份手写「记录→进度折算」的**合并不在本轮**（跨 session/ 与 workunit_team_records.go，归步骤②）。
5) 真跑并记录：gofmt -l（改动文件）/ go build ./... / go vet ./seelebridge/... / go test ./seelebridge/... -count=1。
6) 机械门禁：源码扫描断言（生命周期实现里无按层分支 + 每个作业面实现都带编译期断言），落 e2e/ 或包内测试，
   并能举出一个故意违规样例让它变红（证明它真的会拦人）。

交付的判定标准（缺一条就不算完成）：
- A. `seelebridge/workunit/contract.go` 的 `Jobs` 是完整作业面，方法逐个列出，且编译期断言仍在原处（给出文件:行）。
- B. tools 表的编译期断言落地：给出文件:行，且该表的 diff **只有断言行**（行为零变化）。
- C. 装配表落地：能证明生命周期实现里 grep 不到 teamwork / worktree / session 的具体类型
   （已登记例外：workunit/classify.go 依赖 worktree 两个哨兵错误——写进报告，不要顺手改）。
- D. `workunit/progress.go` 存在且含上述五个形状；它是否已有生产调用者要**明说**（有→给锚点；没有→写清只有形状与原因）。
- E. 六条验证命令的**原始读数**（不是"通过了"三个字）：build / vet / test / gofmt / 机械门禁 / 布局门禁。
- F. 既有 async_*_test.go **一条都没改**（改动一旦触及它们，本轮即越界）。
- G. **删除清单**：删掉了哪些重复实现（文件:行 → 新唯一位置）；本轮若确实没有可删的，就写"无"，并说明为什么这轮注定没有。
- H. 未决项：尤其 **tools 表为承载完整作业面还缺哪几个方法**（它直接决定步骤② 的先行项）。

非目标（做了就是越界）：不迁移任何作业面；不碰前端读面渲染；不碰调度闸门；不动 seelebridge/worktree/**；
不改 Seele 框架的 jobs 包。

红线（碰到就停手并报告，不要硬做）：
- 若发现"不迁移就无法通过编译/测试"——说明 ① 的分界切错了，停下来说明现状，别自己把迁移做了。
- 若发现文档口径与仓库现状冲突——先改文档并说明理由，不许另立一套实现。

【交付格式】1) 改动文件清单；2) 新旧接口签名对照；3) 编译期断言与机械门禁的位置（文件:行）；
   4) 六条命令的原始读数；5) 删除清单；6) 未决项（含 tools 表缺哪几个方法）。
```
