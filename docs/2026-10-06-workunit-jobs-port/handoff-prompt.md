# 交接提示词：workunit 作业端口（步骤①：只定接口，不迁移）

一次性工作包。用法：把下面「提示词正文」整段贴给下一个会话。

## 审查结论（leader 于 2026-10-06，供下一会话建立上下文）

- **main = `599aef4`**，三件活都已合并且门禁全绿（`build-exit=0` / `vet-exit=0` / `seelebridge` 全包 `ok` / e2e 布局 `ok`）。
- **a1（父实现）**：`workunit.Lifecycle` 契约面（`workunit/contract.go`）+ `lifecycleHost`（`workunit_parent.go:51` 编译期断言）；
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

【先读（唯一口径，按序）】
1. docs/arch/workunit-ports-and-assembly.md      —— 父/端口清单 + 装配矩阵 + §5 两步计划（① 六项）
2. docs/arch/workunit-single-lifecycle-one-implementation.md —— §3 接口先行 / §7 纪律 / §8 交集与析构 / §10 红灯先行
3. docs/arch/workunit-duplication-inventory.md、docs/arch/workunit-progress-read-surface.md —— 现状锚点
4. AGENTS.md §8「复用与单一实现纪律」             —— 你的交付口径由它管

【当前状态】main = 599aef4；父实现重构（a1）、merge 踢人修复（a2）、读面勘定（a3）都已合；
合并后门禁 build/vet/seelebridge 全包/e2e 布局全绿；seelebridge/workunit/contract.go 已空出，可以直接动。

【做这六件】（明细见 ports §5 ①）
1) contract.go：Jobs 从只有 Reclaim 扩成完整作业面（Submit / Status / Peek / Kill / Done / Reclaim(scope) / Events / Snapshot，
   只收现在真正用到的几家）；保留 var _ Jobs = (jobs.Manager)(nil) 编译期断言；
   写明 Peek = 增量读不推进游标、Snapshot = 全量读、Reclaim 按 scope 回收。
2) tools 那张 async 作业表补『结构上满足』的编译期断言（让两个父实现同形被编译器钉住）；不改它的行为。
3) 装配表落地一版：生命周期实现只持端口，不持 teamwork / worktree / session 的具体类型。
4) 读面定形：新增 seelebridge/workunit/progress.go（Stage / EncodeStages·DecodeStages / ProgressOf / UnitReader）；
   三份手写『记录→进度折算』的**合并不在本轮**（它跨 session/ 与 workunit_team_records.go，归步骤②）。
5) 真跑并记录：gofmt -l（改动文件）/ go build ./... / go vet ./seelebridge/... / go test ./seelebridge/... -count=1。
6) 机械门禁：源码扫描断言（生命周期实现里无按层分支 + 每个作业面实现都带编译期断言），落 e2e/ 或包内测试。

【不许】不迁移（既有 async_*_test.go 一条不改，改了就是越界）；不碰前端读面渲染；不碰调度闸门；
   不动 seelebridge/worktree/**；与设计文档冲突时先改文档，不许自己另立一套。

【纪律】动手前先找既有实现（结构上满足 > 适配器 > 复制）；只并『判据同 + 语义同 + 会一起漂移』的本质重复，
   长得像的不并但要写清为什么像却不并；一份判据 = 一处函数；修漂移类缺陷先写会红的用例。
   交付必须列『删掉了哪些重复实现（文件:行 → 新唯一位置）』；证据是『只剩一份实现 / 编译器钉住』，不是『用例全绿』。
   作业输出要一次性取全（大 max_bytes、别急着销项——终态作业销项后日志会被删）。
   桌面纪律：不置顶/不覆盖窗口、不抢前台、不合成键鼠；看图走无窗口路径。

【明知未决（不要顺手改，写进报告）】
- workunit/classify.go 仍 import worktree 两个哨兵错误（契约包依赖例外；改法 = 把哨兵搬到零依赖处，代价单列）。
- scheduler/scheduler.go:34 scheduledStatusRunning 是 scheduler 自己的词表（另一个概念），别误并。
- a3 的文档锚点只读勘定、未过编译验证，行号可能已漂。
- 看板：todo_init 是『并入』语义而非替换，清账要手工挑。

【交付格式】1) 改动文件清单；2) 新旧接口签名对照；3) 编译期断言位置；4) 跑过的命令与原始读数；
   5) 未决项 —— 尤其：tools 那张表为承载完整作业面还缺哪几个方法（这决定步骤② 的先行项）。
```
