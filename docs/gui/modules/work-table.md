# Work Table（工作表格）模块

## 定位

工作表格是右侧工作台的统一只读视图：把 plan 节点、todolist 项（kind=todo）、
主动 task 条目（task_add）与 fork 子代理四种异构执行状态归一为同一张多维
表格（阶段/任务/描述/状态/Assignee/Dependency/附件），并按**批次分片**：
一批 = 一次 chat 请求创建的全部条目（BatchID = requestID，如
`chat-<nano>`），批次头可折叠、展示标签/时间/各类计数，批内按权威类型
（kind：plan/task/todo/subagent）chips 过滤。任务打点（trace）带进同一
数据面。右栏为「入口按钮 + 未读角标」，点开按钮弹出完整多维表格弹窗
（工作台窄，详情在弹窗内看全）；未读 = 新增或状态/retry 变化的条目
（`workTableSignatures`/`countUnread`，纯 UI 态，打开详情后清零）。节点
详情弹窗（会话记录/上下文/打点/时间线/工具活动）保留并复用。

主要调用方：`gui/frontend/dist/app.js`（渲染入口）、`seelebridge` 与
`application/core`（投影与事件数据源）。

## 职责与非职责

职责：

- 后端 `application/core/work_table.go` 把 `PlanState` + `TodoItems` +
  `SubAgentTree` 投影为扁平 `WorkItem` 行，并派生批次分片头
  `WorkTableBatches`（CQRS 读模型，纯函数、有界）。
- 后端以 `worktable.changed` 轻量增量发布表格与批次头（只含表格与批次，
  不整份 runtime）。
- 前端 `gui/frontend/dist/work-table.js` 渲染表格、筛选、展开与行内交互；
  按批次分组渲染（批次头可折叠 + 各类计数）、按 kind chips 过滤；todo 行
  三态更新经 `Bridge.UpdateWorkItemStatus` 回写后端权威状态；行区独立滚轮
  滚动（表头吸顶）+ 分页查看（每页 10/20/50，页码钳制）。

非职责：

- 不复制业务状态机：展开/筛选/trace 展开是纯 UI 态，状态与打点永远来自
  后端权威 JSON。
- 不新增 todolist 工具族：todolist 仍是 harness 默认工具，三态
  （pending/doing/done）只经 `SetTodoStatus` / `UpdateWorkItemStatus` 落地。
- plan/subagent 的状态由执行器权威管理，工作表格只读展示，不允许手动覆盖。

## 数据流

1. **task 注册表是唯一权威源**（`seelebridge/task/task.go`，Actor +
   Mailbox，保护粒度=task）：`task_add` 主动入表；todolist 融合为
   kind=todo 的 task；plan/subagent 生命周期被动同步
   （`syncTasksFromSources`）。`startChat` 写入当前批次（`SetCurrentTaskBatch`
   = requestID），新建条目自动盖章 `BatchID`（显式 spec.BatchID 优先；
   恢复/旧数据保留原值，空批次归入「早期任务」）。
2. 子代理生命周期是**被动触发**：`fork_subagents` 注册/节点完成时，
   `seelebridge` 树 observer（`SetSubagentTreeObserver`，main 组合根装配到
   `Service.RefreshWorkTableSnapshot`）自动发布 `worktable.changed`，不依赖
   模型调用任何工具。
3. 事件按责任链拆分（B2）：task 内部变更 → `task.changed`（逐任务增量，
   脏标记驱动，直发 hub 不丢）；worktable 结构 → `worktable.changed`（整表，
   CSP 汇聚 latest-wins）。
4. 前端 reducer（`protocol.js`）：`task.changed` 按 task_id 单行 upsert
   （结构共享）；`worktable.changed` 整表替换并附加 `batches`（缺失时保留
   既有批次头）；不克隆 plan。
5. `work-table.js` keyed reconcile：批次头按 batch_id 做 section 级
   reconciliation，行在批次容器内 keyed 重建；行详情复用
   `SubagentSessionDetail`（上下文/会话/工具活动）。分页与滚动是纯 UI 态：
   筛选/每页条数变化时页码重置，数据收缩时页码钳制，行数据永远来自后端
   权威 JSON。

### 类型轴（Kind 权威）与工具命名

- `Kind` 是唯一权威类型：`plan | todo | task | subagent`；`Phase` 是创建时
  由 Kind 派生的展示字段（todo→tasklist、task→task、plan→plan、
  subagent→subagent，`dto.PhaseForKind`），前端筛选/渲染按 `kind`。
- 工具命名体现条目类型：todo 清单族规范名为
  `todo_init/todo_add/todo_done/todo_status`（旧 `todolist_*` 保留为
  deprecated 兼容别名）；主动任务为 `task_add`（旧 `taskadd` 别名）。
  `task_complete/task_failed/task_needs_user_decision` 是**会话级终态
  协议**（task_context 域），与条目级工具不同层，不混用。
- 状态机按 kind 限定：todo 仅三态 pending/doing/completed（GUI 三态按钮）；
  task/plan/subagent 维持通用迁移（终态→retry 重开、running/doing 不回退
  queued/pending、retry 仅前向）。

### 幂等去重（B1）与子代理装配（B6）

- 归一化 goal（去空白 + 小写 + FNV-1a）作为精确幂等键；plan 用
  `plan:<node_id>`、subagent 用 `subagent:<id>`。
- 三层防御：① 提示词约束（taskadd 声明去重）；② 注册表精确键判重（命中
  直接返回既有 task，不重复建条目）；③ 可注入“审判钩子”（LLM 语义判重，
  重复则重新生成，有次数上限）——当前实现为可注入函数，默认精确键。
- fork 派工前 `bindSubagentTask`：命中 → 绑既有 task_id（参与者挂入）；
  未命中 → 子代理自行开 task；`NodeScope.TaskID` 只作绑定元数据，不进
  prompt（保护子代理 prompt 格式纯净）。

### Assignee 被动识别（role:sessionID）

Assignee 不是 AI 提供的自由文本，而是系统按「执行角色 + 会话 ID」被动生成的
身份标识（`dto.ActorIdentity`，格式 `<role>:<sessionID>`）：

- 主会话（main）执行者：`main:<mainSessionID>`（`sess_<nano>`）。主会话建立时
  （`Runtime.newMainSession`）注入 task 注册表默认身份；todolist、taskadd、
  plan 同步等所有由主会话创建的任务自动继承并自动上名单。
- 子代理执行者：`subagent:<subagentSessionID>`（`node-<hash>`，取自
  `SubAgentTreeNode.SessionID`）。子代理会话注册后经 `syncSubagentTask`
  被动认领（attach），Assignee 随之切换。
- 自动上名单：任务创建时 Assignee 自动写入 `Participants`；接管者
  （attach）追加进名单并成为当前 Assignee（认领语义，幂等去重）。
- 覆盖范围：tasklist（todo）、task、plan、subagent 统一适用；旧的
  「Assignee = 子代理 id / 执行节点」自由文本不再由 AI 或业务代码直接写入，
  旧数据在会话恢复后经同步路径被动补齐。

### retry（B3）

`status=retry` 时 `RetryCount` 自增，重跑 `running` 保留计数；前端展示
`RETRY n`；`task.changed` 携带最新 retry_count。重试状态由
`seelebridge` 生命周期写入：`fork_subagents` 重新命中既有 task（同 goal）
时，终态（completed/failed）重开为 `retry`，节点真正启动时转 `running`
（计数保留）；转换约束允许终态 → retry、禁止 retry 回退 queued/pending。

结果返回失败需要重试时（`final_output` 被截断 / `read_tool_result` 失败），
`fork_subagents` 先检查子代理树是否保留完整输出：全部命中则直接读回已保存
输出返回（`reused:true`，task 经 retry 计数后回到 completed），不再重新
执行——省 token；未命中才真正重跑。

### 持久化（T4）

task 快照随 `SessionRecord.Tasks` 复用 session stack 存储通道（与 PlanStack
同一 immutable 语义）；会话恢复时 `Runtime.RestoreTasks` 回填注册表。

子代理 done 节点**有界保留**（`subagentTreeRetainDone`=50，超限清最旧；
失败节点保留现场），直到 `ClearSubagentTree` 显式清空——工作表格因此能
被动展示已完成的子代理任务，而不是任务一结束就消失。

## 数据契约

- Snapshot：`runtime.work_table: WorkItem[]`（有界，行上限
  `limits.work_table_rows`，默认 200）+ `runtime.work_table_batches:
  WorkTableBatch[]`（批次头，按 CreatedAt 升序，早期会话置底）。
- 增量事件：`worktable.changed`，payload 见
  [`schemas/work-table.schema.json`](../schemas/work-table.schema.json)
  （`items` + 可选 `batches`）。
- WorkItem ID 稳定键：`plan:<id>` / `todo:<index>` / `subagent:<id>` /
  `task:<n>`；Dependency 引用同命名空间的 WorkItem ID；BatchID 引用批次头
  ID（空串 = 早期会话）。
- 行内打点上限：`workTableTraceLimit = 10`（概览最近 10 条；完整时间线仍
  在详情弹窗，上限 `limits.plan_node_events`）。

## 设计模式

| 关注点 | 模式 |
|---|---|
| 三态/清单资源并发安全 | Actor + Mailbox（`seelebridge/todo_tool.go`，单消费者串行，请求-应答走 channel） |
| 高并发事件流转 | CSP 汇聚发布器（channel cap=1，drain latest-wins，生产者阻塞背压） |
| 多源归一 | 读模型投影（纯函数，锁内构建，无 I/O） |
| 多维分片 | 批次 = chat 请求；`buildWorkTableBatches` 派生批次头（最早 CreatedAt 为批次时间），前端 section 级 keyed reconciliation |
| 前端增量 | keyed reconciliation + html 缓存（只重建变化行）+ 结构共享 reducer |

## 扩展方式

- 新增任务来源（如定时任务入表）：在 `buildWorkTable` 追加一行映射并保持
  ID 前缀唯一；事件发布点按需补充。
- 审判节点（B1 ③）：实现 `judgeTaskDuplicate` 注入函数（LLM 一轮对话对比
  注册表快照），当前默认精确键兜底。
- 调整行/trace 上限：`seele.yaml` `limits` 段
  （`work_table_rows` / `plan_node_events` / `evidence_chars`）。
- 调整默认分页大小：`createWorkTableView({ pageSize })`（默认 20）。
- 支持 plan/subagent 手动状态：需要执行器状态机语义，另行设计，不在本
  模块默认开启。

## Review 指南

- `worktable.changed` 是否只带表格（不整份 runtime）；revision 与内容是否
  在同一临界区生成。
- 行 ID 与 Dependency 引用是否稳定、是否有环/悬垂。
- 批次盖章是否一致：新条目是否归属当前 chat 请求批次；跨请求同步（plan/
  subagent 异步生命周期晚到）是否造成批次串写（v1 接受展示层偏差，批次
  不是权限边界）。
- trace/evidence 是否截断有界；文本是否全部 escape。
- Assignee 是否只来自 role:sessionID 被动识别并自动上名单（创建者 + 接管者）。
- todo 状态更新是否只走后端权威（拒绝路径不发布增量）。
- 发布器关闭是否排空尾态、Send 是否可退出（无 goroutine 泄漏）。

## 测试与验证

```text
go test ./application/core -run "WorkTable|UpdateWorkItemStatus|WorkTablePublisher" -count=1
go test -race ./application/core -run "TestWorkTableRace|TestWorkTablePublisher" -count=1
node --test gui/frontend/dist/work-table.test.mjs gui/frontend/dist/protocol.test.mjs
go test ./docs... 2>/dev/null; go test . -run TestGUIDocumentContracts -count=1
```

批次相关新增测试：`TestBuildWorkTableBatches`、
`TestTaskRecordToWorkItemCarriesBatch`（application/core）、
`TestTaskRegistryDefaultBatchStampsNewTasks`、
`TestTaskRegistryTodoKindRestrictsStateMachine`（seelebridge/task）。
