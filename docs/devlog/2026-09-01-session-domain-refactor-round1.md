# 2026-09-01 会话域重构第一轮（Session Domain Independence — Round 1）

> 日期: 2026-09-01 | 范围: `session/` + `application/core` + `seelebridge` +
> 前端 | 关联: `docs/2026-08-30-session-resource-refactor/` 系列

## 背景

用户要求从生态位层面重构，而不是继续打补丁：会话完全独立出 core（core 只保留
当前会话视图指针 V）、会话间线程隔离防串行/污染、继承只走深拷贝；配套边界/
暴力/冒烟三档测试；先用对抗性审查再实施。

## 设计

- [session-domain-design.md](../2026-08-30-session-resource-refactor/session-domain-design.md)
  为权威设计（含红队对抗性审查记录 §8 与实施记录 §9）。
- [niche-redesign.md](../2026-08-30-session-resource-refactor/niche-redesign.md)
  为生态位重划定位稿。

## 实施（首轮完成项）

1. **B 容器上移**：`state.Core.SessionViews` 与 `sessionChat` map 迁入
   `session/domain.go`；`view_state` 经 `session.Domain` 读写；core 仅保留
   Snapshot 镜像（V 指针）；每会话 Unit 私有锁 + 生命周期状态机
   （COLD→PREPARED→LIVE→COLD，非法迁移拒绝）。
2. **C task 分区路由**：`RuntimePort` 增加 `TaskAddFor` /
   `ResolveTaskByKeyFor` / `TaskSetStatusFor`；后台会话写自身 scope 分区，
   当前注册表不被污染；`syncTasksFromSourcesFor(sid)` 按会话同步；
   `RestoreSessionTaskLockedFor` 支持 per-session plan/task 状态写。
3. **D 前端收口**：`protocol.js` 按 `event.session_id` 过滤跨会话负载事件。
4. **E 部分**：`session.Manager` 降级标注；README 生态位更新。
5. **红队补充并入**：`PlanNodeEvent.SessionID` 契约 + executor 填充。

## 测试

- S0 靶场：前端 3 例（跨会话污染红→绿）+ 后端 `TestS0BackgroundSessionTaskWriteMustNotPolluteActiveRegistry`。
- 边界：`session/domain_test.go`（状态机/线程隔离/V 指针）。
- 暴力：`application/core/session_stress_test.go`（6 会话并行 + 高频切 V +
  并发排队，`-race` 全绿）。
- 契约：`seelebridge/task_partition_test.go`（分区路由/幂等/状态路由）。
- 全量：`go test ./... -p 1` 全绿；`-race` 关键包全绿；GUI tag 构建 ok；
  前端 164/164。

## 遗留（下一轮）

- 子代理对抗性审查并入（设计 §8.2）。
- plan executor / subagent tree per-session 化（F4 runtime.changed 收口）。
- Snapshot.Task/Plan/WorkTable 完全收口到会话域视图派生。

## 追加：第二轮（同日，红队并行实施并入）

红队在实施过程中以对抗视角补齐并修复（全部由测试抓到后修复，设计 §8.2
R1–R5 逐条记录）：

- **F4 per-session plan 投影**：`planProjections` 缓存 + `planEventSession`
  （事件自带 sid）→ `HandlePlanNodeComplete` 只更新归属会话投影；plan/subagent/
  tool/runtime/context 事件全部经 `publishSessionEvent` 携带 sid。
- **R1（blocker）**：`SubmitToSession` TOCTOU 竞态——路由只认显式 sid，
  删除 current 重读（压力测试抓到 queued-2 进 sess-4 后修复）。
- **R4（major）**：hot_attach/cold_load 全局 `BindProjectRoot` 改为
  `bindProjectRootIfSafe`（有其它会话运行中跳过全局重绑）。
- **R2/R3**：测试桩 double-close/nil channel 与锁外读 map 两处 race 修复。
- **D 完整**：bridge 第一道过滤（`currentSessionID` 跟踪）+ client-state
  切换基线 resync（新增 S0 用例，前端 165/165）。
- **SwitchSessionTasks 修正**：后台分区写不再被切换换血覆盖。

验证（第二轮终态）：`go test ./... -p 1` 全绿；`-race`
`./application/core ./session ./seelebridge ./gui` 全绿；前端 165/165；
`go build -tags "gui,desktop,production" ./...` ok；linux/darwin 交叉编译 ok；
产物冒烟 PASS（`tmp/smoke/smoke-seelex-gui-20260901-021001.log`）。

有意的范围收敛（设计 §9.2）：View 未加独立锁（Core.Mu 单锁已 race 干净）；
task 分区用快照分区而非完整 per-session Registry；项目根用安全守卫而非
per-session 绑定。均留待 seelebridge 能力层后续演进。
