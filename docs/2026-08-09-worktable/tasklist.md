# Work Table / Task 体系实现任务清单

> 一次性工作包：本文是本次实现的任务清单与文件归属矩阵（2026-08-09）。
> 设计依据：docs/gui/modules/work-table.md、ADR-GUI-020 与用户 2026-08-09
> 设计讨论结论。完成后以代码与测试为准，本文退化为实现记录。

## 已确认的设计决策

### 会话区（任务 A）

- **A1**：子代理 merge-back 注入的“`[子代理产出] ` 继承上下文”块**不得出现在
  可见会话区**；保留注入 provider history（模型仍可消费），仅在
  `Snapshot.Conversation` 投影层剔除标记消息（先例：`runtime_projection.go`
  对 marker 前缀已过滤；同 `stripThoughtBlocks` 思路）。
- **A2**：用户运行中输入 → 消息队列；**每轮 ReAct 迭代结束**（
  `OnIterationComplete`）即消费并清空队列，下一轮 LLM 可见；无需等整条
  loop 跑完。当前非 session-backed 路径已实现（`chat.go:1305`）；**生产
  session-backed 路径因 Session 锁内不可重入 `AppendHistory` 直接跳过**，
  需改为“队列非空 → 返回 false 中断本轮 → turn 边界提升并自动续跑”。
- **A3**：其余机制（`pendingSubagentContexts` 排队注入、turn 边界提升）保持
  现状不变。

### Worktable Task 体系（任务 B）

- **B0（融合）**：todolist 直接内化为 worktable 的**打点表**：todo 状态不再
  独立成 `todo_items` 业务实体，todolist_* 工具与 taskadd 共享同一个 task
  注册表（单一状态源），每次操作写入 task 的 trace 打点。
- **B1（幂等去重）**：三层防御——① 提示词约束；② registry 精确键判重
  （plan 用 `plan:<node_id>`、subagent 用 `subagent:<id>`、主动 taskadd 用
  归一化 goal hash），命中直接拒绝/合并；③ 产出前“审判节点”：用一轮临时
  对话对比当前 task 注册表快照做语义判重，重复则重新生成（有次数上限，
  防死循环）。**subagent 已开、main 又重开同一 task**：不重新生成已运行
  子代理，改为合并/挂参与者或拒绝并返回既有 `task:<id>`。
- **B2（事件 + 责任链）**：task 内部变更（状态/打点/retry）→ `task.changed`
  （按 task_id 单行增量）；worktable 结构变更（增删）→ `worktable.changed`
  （整表）。处理链：`taskadd/生命周期事件 → worktable gate → task actor`
  （责任链，两级）。保护粒度 = **task**（单一 registry actor，内部按 task
  键隔离状态；不采用每 task 一个 goroutine）。
- **B3（retry）**：task 增加 `retry` 状态与重试计数：失败可重试 →
  `status=retry` + `retry_count`（展示 `RETRY n`）；重跑 → running（计数保留）。
- **B4（存储复用）**：task/worktable 状态复用既有 session stack 机制
  （`SessionRecord.PlanStack` 同构的 immutable stack / sessionstore 通道），
  不另起存储。
- **B5（被动触发）**：plan 节点生命周期（`HandlePlanNodeComplete` /
  `HandlePlanBranchEvent`）与子代理树 observer（`SetSubagentTreeObserver`）
  经 CSP channel 通知 registry actor 更新 task；事件仍经汇聚发布器。
- **B6（子代理装配现成 task，装配件模式）**：先 tasklist 再开 subagent 的
  场景，派工前做 task 幂等校验（按归一化 goal 在注册表查重）。命中 → 用
  装配件把既有 `task_id` 绑进子代理 NodeScope（**只装配 task_id，不注入
  task 内容**，避免污染子代理 prompt 格式与处理）；未命中 → 子代理启动时
  自行开 task（自动注册 / taskadd）再绑定。效果：worktable 中每个 task 只
  出现一次，同一 task 的多个子代理作为参与者挂到同一 task。

## 任务列表（依赖 DAG）

| ID | 任务 | 依赖 | 子代理 |
|---|---|---|---|
| T1 | A1：会话区剔除子代理继承上下文（保留模型上下文） | 无 | A |
| T2 | A2：session-backed 每轮队列消费（OnIterationComplete 中断 + 自动续跑） | 无 | A |
| T3 | B 后端：task registry actor + taskadd + todolist 融合 + retry + 去重（精确键 + 审判钩子）+ task.changed/worktable.changed 事件 + B6 子代理装配 task_id | 无 | B |
| T4 | B 持久化：task/worktable 状态接入 session stack（SessionRecord） | T3 | B（T3 后） |
| T5 | B 前端：protocol.js task.changed 单行 upsert、work-table.js retry/打点展示、todo 融合 | T3 | B（T3 后） |
| T6 | 文档同步 + 全量验证 + GUI 重建 | T1..T5 | root |

## 文件归属矩阵（避免并行冲突）

| 文件 | 归属 |
|---|---|
| `application/core/service_input.go` | A（T2 队列）+ 保持 A3 现状 |
| `application/core/chat.go` | A 只改 `OnIterationComplete` 队列区（~1300 行区域）；B 只改 `HandlePlanNodeComplete`/`HandlePlanBranchEvent`（~776-940 区域） |
| `application/core/session_history.go` 等会话投影 | A（T1 剔除） |
| `seelebridge/todo_tool.go` + 新 `seelebridge/task*.go` + `seelebridge/fork_tool.go` / `agent_node.go`（NodeScope 绑 task_id） | B |
| `application/core/work_table.go` / `worktable_publisher.go` | B |
| `application/model/state.go`、`application/event/hub.go`、`application/core/aliases.go` | B |
| `gui/bridge.go`（taskadd/状态更新） | B |
| `gui/frontend/dist/protocol.js`、`work-table.js`、测试 | B |
| `docs/**` | root（T6） |

## 验收

- `go build ./...`、`go vet ./...`、`gofmt -l .`
- `go test ./application/core ./seelebridge ./gui ./seelexctx -count=1`
- `-race`：task registry / 队列并发 / worktable 发布器
- `node --test gui/frontend/dist/*.test.mjs`
- GUI 重建到 `dist/seelex-gui-dev`（root 收尾）
