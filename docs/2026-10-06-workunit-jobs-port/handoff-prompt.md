# 交接提示词：workunit 作业端口（步骤①：只定接口，不迁移）

一次性工作包。用法：把下面「提示词正文」整段贴给下一个会话（新会话没有本次上下文，所以它自带前置、读哪些文件、完成判据）。

## 审查结论（leader 于 2026-10-06，供下一会话建立上下文）

- **main 头 = 本文件所在提交**（开工前 `git log --oneline -6` 应看到 82e4361 / 17f37c4 / 599aef4 / c4492ab / e3594bf 五个交接点），三件活都已合并且门禁全绿（`build-exit=0` / `vet-exit=0` / `seelebridge` 全包 `ok` / e2e 布局 `ok`）。
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

## 现状与目标（字符画 + 大白话）

### 现状（2026-10-06，main = `17f37c4`）

```
① 生命周期：已经收成一份 ✔
   leader 派活
     │
     ├── subagent 注册点  workunit_node.go（58 行）   ─┐
     └── teammate 注册点  workunit_team.go（127 行）  ─┤ 方法体只有一句：
                                                       │   return u.host.Xxx(ctx, u.read)
                                                       ▼
                     workunit_parent.go:51  lifecycleHost（唯一实现）
                     Begin / Finish / Reclaim / Recover / AlreadySettled / Notice
                     var _ workunit.Lifecycle = (*lifecycleHost)(nil)      ← 编译器钉住
                                   │
        差异只剩策略：Immediate（subagent 收完就拆）／AtTeamClose（留给 team_close）
                                   └── 两者调的是同一个 Reclaim（一份实现，两个调用点）

② 作业面：还是两张表 ✘（唯一没并的本质重复）
     teammate  ──▶ 表① Seele jobs.Manager    Scope{Session, Subject}   ← team_items 认这张
     subagent  ─┐
     bash_bg   ─┴▶ 表② tools 自建 async 表     JobSpec.SessionID        ← job_manage 认这张
     同一个概念（一件在飞的活 = 句柄 + 状态 + 结果回读 + 取消 + 回收）长成两份，
     两套隔离键、两套回收时机；合表被冻结的框架契约挡住：CHANGELOG.md:112-116 记的七点不一致
     （不是工作量问题，是语义差异）

③ 读面：数据形状早就统一，折算还是三份手写 ✘
     两层都落同一张 sessionstore.NodeSessionRecord、都走 workunit.SessionLedger 端口
     但「记录 → 进度」这一步手写三处（subagent_sessions.go 写侧/读侧、workunit_team.go）
     teammate 的阶段打点恒为单元素 → 界面看起来"看不懂"
```

**大白话（现状）**：**该收的都收了，就剩作业面这一块还没收。**
生命周期以前是三层各手写一遍"建现场→收尾→回收→恢复"，于是漂移出 F2/F3/F4/F5 四条 bug；
现在只有一份实现（`lifecycleHost`），subagent 和 teammate 各留一个几十行的"登记处"，方法体就一句转发；
唯一的差异（什么时候拆现场）只剩一个策略：subagent 收完当场拆、teammate 留到 `team_close`。
编译器把这件事钉住了（谁想再写第二份，先编译不过）。

**还没收住的是"作业面"**：同一件事——一件在飞的活，有句柄、有状态、能读结果、能取消、能回收——
现在有两套实现：teammate 走 Seele 框架的 `jobs.Manager`（隔离键带 `Subject`），
subagent / `bash_bg` 走 tools 自己那张登记表（隔离键只有 `SessionID`）。工具面也各认一张
（`job_manage` 认 tools 句柄、`team_items` 认 jobs 句柄）。**这就是现在最大的一块本质重复**，
但它这一轮只能"把两个接口改成同形"，不能搬（搬表要先把七点语义差异解决掉）。

**读面**：数据形状其实早就统一了，两层写的是同一张记录、走的是同一个端口；真正让人"看不懂"的是
"把记录折算成进度"这一步被手写了三处，而且 teammate 的阶段打点永远只有一个元素。

### 目标效果（步骤① 做完之后）

```
                        ┌───────────────────────────────────────────────┐
                        │  lifecycleHost：一个生命周期实现（唯一）        │
                        │  Begin / Finish / Reclaim / Recover / …        │
                        └──┬────────┬─────────┬─────────┬───────────────┘
            只调端口        │        │         │         │      编排 / 装配 / 账本
      （不认识具体类型）     ▼        ▼         ▼         ▼      不进 workunit（回调接入）
                        作业面    现场      会话      读面
                        JobFace SceneFace SessionFace ReadFace
                          │        │         │         │
     实现A jobs.Manager ──┤        │         │         │
     实现B tools 表 ──────┘   Worktree    NodeSession   progress.go
     （两者形状同形，          Manager      Store        （折算只有一处：
       编译器钉住）            一份         一份            Stage / ProgressOf）
                          │
        装配处（new）决定装哪个实现 —— 换实现 = 改一行 + 编译期断言先红

   形态：每个形态只装配它需要的端口，没有上帝对象
     bash_bg   ：作业面
     subagent  ：作业面 + 现场 + 会话 + Immediate
     teammate  ：作业面 + 现场 + 会话 + AtTeamClose + 编排 + 装配
     只读巡检员：会话（未来）
```

**大白话（目标）**：**让"一件活的五件事"各自只有一个出处，谁都能单独换、单独假。**
五件事是：谁在跑（作业面）、干到哪了（读面）、现场在哪（现场端口）、下次怎么接着干（会话端口）、
什么时候拆（策略）。做完之后：

1. **一眼看懂**：`lifecycleHost` 里的每一步只调端口，端口背后是谁它不知道。
2. **没有上帝对象**：它不认识 `teamwork` / `worktree` / `session` 的具体类型；层与层的差异只体现在
   **装配表**（装哪几个端口）和**策略**（什么时候拆）里，实现体里搜不到 `if 哪一层`。
3. **换实现是便宜的**：两个作业面实现形状相同、被编译器钉住，换一个 = 装配处改一行、断言先红。
4. **能验的判据**（缺一条就不算做完）：只装配一部分端口也能跑；**每个端口都能换成假的**
   （不需要真 git、真进程）；实现里 grep 不到按层分支；换实现只改一行。
5. **这一轮不搬表**：只把接口改成同形；作业面上真正"只剩一份"要等步骤②（先把七点语义差异解决）。

## 提示词正文

```
【任务】步骤①：把 workunit 的作业端口从「只有 Reclaim」扩成完整作业面——只定接口，不迁移。

==================== 一、前置（新会话必须知道的事实与环境） ====================

仓库：G:/Program/go/seelex（Windows + PowerShell；go 可用）。
基线：main 头 = 本文件所在提交（开工前先 `git log --oneline -6` 核对，应看到五个交接点：
  82e4361 = 现状与目标、17f37c4 = 交接提示词、599aef4 = 读面端口落定、c4492ab = 父实现、
  e3594bf = merge 踢人修复），工作区干净。三件前置活都已合并、门禁全绿：
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
