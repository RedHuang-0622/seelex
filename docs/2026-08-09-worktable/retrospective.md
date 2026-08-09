# 2026-08-09 Worktable / Task 体系多轮问题回顾

> 一次性工作包：记录 2026-08-09 从「工作台统一工作表格」到「task 状态体系 +
> CSP 并发重构 + 缓存命中优化」的若干轮问题、决策与修改方案。事实以代码与
> 测试为准；本文是过程记录与决策留痕。

## R1：工作台统一工作表格 + task 状态体系

**问题**：右侧工作台 Plan 树 / 待办 / 子代理树三个分区各自为政；fork 子代理
期间 `snapshot.Runtime.Plan` 为 nil，`HandlePlanNodeComplete` 直接返回，工作台
看不到子代理；成功子代理“跑完即清走”，结束后无证据。

**决策**：
- 右侧工作台由「入口按钮 + 未读角标」承载，点开弹窗看完整多维表格
  （阶段/任务/描述/状态/Assignee/Dependency/附件）。
- task 即 worktable 条目：单一注册表 actor（Actor + Mailbox，保护粒度=task）。
- 事件语义（责任链）：task 内部变更 → `task.changed`；worktable 增删 →
  `worktable.changed`。
- todolist 融合为 kind=todo 的 task（打点表），不新增工具族；主动 `taskadd`
  工具 + 被动 plan/subagent 生命周期同步。
- 幂等去重三层：提示词约束 + 归一化 goal 精确键 + 可注入审判钩子；B6 装配件
  只把 task_id 绑进子代理 NodeScope（不进 prompt）。
- retry 状态 + retry_count；持久化复用 SessionRecord（stack 通道）。

**修改**：`seelebridge/task_registry.go`、`todo_tool.go`（融合）、`fork_tool.go`
（B6）、`application/core/work_table.go`、`worktable_publisher.go`、
`gui/frontend/dist/work-table.js`、`protocol.js`（task.changed 单行 upsert）。

## R2：会话区与消息队列

**问题**：子代理完成会把「`[子代理产出]` 继承上下文」块写进可见会话区；
session-backed 引擎下运行中输入要等整条 loop 结束才消费。

**决策**：
- 继承上下文只进 provider history，可见 `Snapshot.Conversation` 剔除 marker
  前缀消息（保留模型上下文，用户界面无噪音）。
- 每轮 ReAct 迭代结束（`OnIterationComplete`）检查输入队列，非空则中断本轮，
  由 turn 边界自动续跑并清空（一轮一消费）。

**修改**：`service_snapshot.go`（marker 剔除）、`service_input.go`、
`chat.go`（session-backed 迭代中断）。

## R3：缓存命中率（对照 codex-cli ~99.8%）

**问题**：Seelex 命中率约 66%（命中 142,976 / 未命中 72,176），codex-cli
约 99.8%。根因：system prompt 每次 turn 重建且嵌入随节点变化的
`current_node` → 节点推进即整段前缀缓存失效。

**决策**：
- system prompt 只放 plan 级稳定信息（plan_ref），`current_node` 移除；
  节点状态由请求尾部 plan 上下文消息与 read_plan 提供。
- 动态任务状态不再进 system prompt，改由请求尾部的「工作打点表」标记块
  （`<!-- seelex:worktable:v1 -->`）承载：只含未终态任务、终态即删、不落
  历史（天然不可压缩）、尾部更新缓存友好。
- system prompt 幂等：内容不变时不重复 `SetSystemPrompt`。

**修改**：`service_prompt.go`、`work_table.go`（workTableTraceBlock）、
`context_controller.go`（尾部注入）。

## R4：并发 / 死锁审计与 CSP 重构

**问题**：怀疑并发数据竞争扩大为死锁；四个子代理全部卡 queued；状态多写者、
回调式 observer 同步嵌套、持久化路径持锁调 actor 都是隐患。

**决策**：
- 回调 observer 改 CSP：子代理树生命周期、plan 节点事件、task 变更都经
  channel 流转，application 侧消费者 goroutine 串行处理（`startLifecycleConsumers`）。
- task 变更由注册表输出 channel 直发（取代拉式 DrainDirty），`emitChange`
  **非阻塞**（满则丢弃 + 计数）——绝不因消费者慢而让 actor 停摆。
- `RefreshWorkTableSnapshot` 锁外取子代理树（ExportSnapshot 不持 service.mu），
  消除 `service.mu → 子代理会话锁 → actor → channel` 环路死锁。
- 状态单调迁移：终态不可回退、running/doing 不可退回 queued/pending。
- 持久化路径锁外取 tasks。

**修改**：`task_registry.go`、`subagent_tree.go`、`runtime.go`、`plan_events.go`、
`work_table.go`（消费者）、`service_assembler.go`、`session_archive.go`、
`main.go`（移除同步接线）、Docker 恢复测试环境开关。

**验证（防复发）**：
- `go test -race` 覆盖 seelebridge / application/core / gui / seelexctx / e2e。
- 新增远超并发上限 mock：2 账号跑 8 子代理（`TestForkSubagentsExceedsConcurrencyLimitQueued`）、
  注册表 32 并发突发（`TestTaskRegistryHighConcurrencyBurst`）、actor 不因
  changes channel 满而阻塞（`TestTaskRegistryEmitChangeNeverBlocksActor`）。

## R5：内存优化（实测 WebView 200+MB→40MB，后端 40MB→12MB）

**决策**：去掉“每事件深拷贝整棵 plan / 整树 DOM 重建 / 整份 runtime 发布”，
改路径级结构共享 + keyed reconciliation + 扁平有界增量（行 ≤200、trace ≤10、
done 树有界 50、todolist 融合单一注册表）。

## 遗留（规划/待办，未宣称已实现）

- LLM 语义审判钩子（B1③）为可注入函数，默认精确键，未接真实 LLM 轮。
- plan/subagent 状态手动覆盖未开放（执行器权威）。
- 队列输入“软注入”替代 turn 重启、system prompt 跨任务进一步稳定化。
- RuntimePort fake 收敛、WorkItem/TaskRecord 双模型收敛。
- Docker 恢复测试需显式 `SEELEX_DOCKER_RECOVERY_TESTS=1` 才运行（daemon 状态相关）。
