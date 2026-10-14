# 内存 / CPU 随时间累积（重启归零）：裁定、证据与治理方案

- 日期：2026-10-14
- 状态：裁定已定稿；**P0（释放面）+ 可调窗口已落地，全量回归全绿**（`go test ./... -count=1`：
  根包 90.4s / `application/core` 65.5s / `e2e` 12.1s / `internal/adapters` 8.1s /
  `seelebridge` 96.2s / `sessionstore` 80.3s / `workspace` 39.8s，无 FAIL）；
  实现清单与验证见 [`implementation.md`](implementation.md)；P1/P2 未开始
  （落点见 [`optimization-plan.md`](optimization-plan.md)）
- 证据等级：裁定部分为**静态源码证据**（file:line）+ 仓储级搜索断言（"无删除面"）；
  **未**做运行时内存曲线实测（见 [`verdict.md`](verdict.md) §5 未证项）；
  实现部分为 `go test` 回归结果（见 `implementation.md` §测试与验证）
- 本工作包不改用户数据：只读诊断 + 文档 + 代码；配置面只增键（`config/seelex.yaml`
  的原件备份在 `config/seelex.yaml.bak-2026-10-14`）

## 一句话结论

"内存/CPU 随时间累积、重启归零"不是一个 bug，而是**三件事叠在一起**：

1. **会话级驻留的释放面不完整**：驻留 LRU（`resident_limit`）只丢了 `EnginePort` 的
   map 引用，框架会话本体仍在 `seelebridge.Runtime.bundles`（只增不删）里 —— 所谓
   "驱逐"在内存上近似空操作。
2. **进程级无界累积**：内存遥测 `telemetry.MemoryTracer`（traces/spans/events/audits/
   metrics）只有 append、没有任何删除面；`Runtime` 侧的 `teamworkCoords` /
   `liveHistory` / `routingNoticeSeen` 等 per-会话/节点残格同样只写不删。
3. **每次热路径的固定成本随规模线性上升**：`Bridge.Snapshot()` 每次深拷贝整份快照
   （正文 + Plan + 子代理树 + 工作表格 + 团队看板）；`TokenCountFor` 每次在**全进程
   事件集**上做一遍全量 `Query` + 排序；目录枚举每次 `ReadDir` + 逐会话读 head 文件。

三者的公共形态：**由活动量驱动、无上限、只随进程存活时间增长 ⇒ 重启归零**，
与用户观察完全一致。

## 文档导航

| 文档 | 内容 |
|---|---|
| [`verdict.md`](verdict.md) | 完整裁定：总表 + 逐条证据（含 file:line）+ 归因排序 + 未证项 |
| [`release-surface.md`](release-surface.md) | **会话释放面规范 v1**：受保护集（当前视图 / 待审批 / 运行中不释放）+ LRU 窗口 = 1（只保留上一个）+ 释放清单与冷回读契约 |
| [`optimization-plan.md`](optimization-plan.md) | 分级优化方案（P0/P1/P2）：改动落点、收益、风险、红灯用例与验证方式 |
| [`implementation.md`](implementation.md) | **实现状态**：改了什么、配置面（`limits.session_release_window`）、测试证据、已知退化与残留 |

## 与用户口径的对应关系

用户给出的释放面口径（当前视图 / 待审批 / 运行中不释放；其余"加载过且静默"的
会话按 LRU 窗口 = 1 释放，只保留上一个）**与现有守卫判据一致**，需要补的是两件事
（**均已实现**，见 [`implementation.md`](implementation.md)）：

- **计数口径**：现有实现的限额把"当前视图会话"也算进上限，`resident_limit=1` 的真实
  语义是"只留当前"，不是"当前 + 上一个"（`resident_lru.go:62`、`content_lru.go:89`）。
- **释放动作**：释放清单必须覆盖框架会话本体（`bundles`）与 per-会话残格，否则窗口
  再小也只是少丢引用（见 [`release-surface.md`](release-surface.md) §释放清单）。

## 工作打点（本工作包）

| # | 事项 | 状态 |
|---|---|---|
| 1 | 摸底：Seele 框架 / Seelex 宿主的运行时所有权边界 | 完成（见 `verdict.md` §2 表 "归属" 列） |
| 2 | 怀疑点 1：引擎 bundle / 会话单元在删除·驱逐后是否释放 | 完成：**驱逐不释放本体；删除走精简路径且不卸载** |
| 3 | 怀疑点 2：后台作业进程树 / 日志 / 登记表是否封顶 | 完成：**已封顶**（`jobs.manager.prune` 退休终态作业并删托管输出文件） |
| 4 | 怀疑点 3：子代理节点缓冲 / teamwork Coordinator / per-会话残格 | 完成：`teamworkCoords` / `liveHistory` / `routingNoticeSeen` / `roleTurns` / `images` **无删除面** |
| 5 | 横向排查：遥测无上限 + 快照热路径全量扫描 | 完成：见 `verdict.md` §3 B、§4 C |
| 6 | 汇总：成因解释 + 优化方案（证据、验证方式、改动落点） | 完成（本文档集） |
| 7 | 实现：释放面（引擎 + 桥侧本体 + 正文 + 下游句柄）+ 窗口成为配置项 | 完成（[`implementation.md`](implementation.md)） |
| 8 | 实现：telemetry 封顶 / 快照深拷贝 / 目录缓存（P1） | 未开始 |
