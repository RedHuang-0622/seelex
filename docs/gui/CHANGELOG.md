# GUI / Agent Workbench 设计变更记录

本文件记录会改变模块边界、跨模块契约、兼容性、持久化或运行流程的重要设计。纯文字修正不记录。

## 2026-08-09

### Added

- 新增 `worktable.changed` 轻量增量事件与 `schemas/work-table.schema.json`
  （工作表格行 + 任务打点，只带表格不整份 runtime）。
- 新增 Work Table 模块（`modules/work-table.md`）：右侧工作台统一表格视图，
  替换 Plan 树 / 待办 / 子代理三个分区；节点详情弹窗保留。
- `application/core/work_table.go`：plan/todo/subagent → 扁平 WorkItem 读模型
  （有界：`work_table_rows` / `plan_node_events` / `evidence_chars`）。
- `application/core/worktable_publisher.go`：CSP 汇聚发布器（latest-wins，
  生产者阻塞背压，关闭排空尾态）。
- `seelebridge/todo_tool.go`：todoState 改为 Actor + Mailbox；TodoItem 增加
  三态 `status`（pending/doing/done），`Done` 为派生字段；新增
  `Runtime.SetTodoStatus`（不新增工具族）。
- Bridge 新增 `UpdateWorkItemStatus`（v1 仅 todo 三态）。
- 前端 `work-table.js`：条目展开多维表格、筛选、行内打点、todo 三态按钮、
  plan/subagent 详情入口；keyed reconciliation + html 缓存（只重建变化行）。
- 前端 reducer 内存优化：`runtime.changed` 不再预克隆 plan；
  `subagent.*` 改为路径级结构共享（`protocol.js`）。
- 子代理树生命周期被动触发：`SetSubagentTreeObserver` → 
  `Service.RefreshWorkTableSnapshot`，fork 注册/完成自动发布
  `worktable.changed`（无需模型调用工具）；done 节点有界保留
  （`subagentTreeRetainDone`=50，超限清最旧，`ClearSubagentTree` 显式清空）。
- worktable task 状态体系：task 就是 worktable 条目，单一 task 注册表
  （Actor + Mailbox，保护粒度=task）；todolist 融合为 kind=todo 的 task
  （打点表，无独立 todo 实体）；主动 `taskadd` 工具 + 被动 plan/subagent
  生命周期同步（CSP channel 通知）；retry 状态 + retry_count；幂等去重
  （归一化 goal 精确键 + 提示词约束 + 可注入审判钩子，B6 装配件只绑
  task_id）；`task.changed` 逐任务增量 + `worktable.changed` 结构增量；
  task 快照随 SessionRecord 复用 stack 存储（T4）。

### Changed

- 右侧工作台 `index.html`：`plan-section` / `subagent-section` / `todo-section`
  合并为 `work-section`（`work-table-view`）。
- 运行上限新增 `limits.work_table_rows`（默认 200）。
- `event.schema.json` 的 `kind` 允许 `worktable.changed`（pattern 兼容）。

### Design decisions

- worktable.changed 只携带表格投影，不整份 runtime；revision 与 items 在同一
  临界区生成。
- plan/subagent 状态由执行器权威管理，手动状态更新仅开放 todo 三态。
- todolist 保持 harness 默认工具族，三态只经 `SetTodoStatus` 落地。
- 工作表格是子代理的被动观察者：数据面只依赖后端生命周期投影，不依赖
  模型主观意愿打点。
- 事件语义按责任链拆分：task 内部变更 → task.changed；worktable 增删 →
  worktable.changed；todo 状态在 task 注册表内流转（todo:done 映射）。

## 2026-07-24

### Added

- 建立 `architecture.md` 作为 GUI/Agent Workbench 权威总体架构入口。
- 建立机器可读 `module_dotting.json`，登记模块状态、职责、接口、实现路径、输入输出和依赖。
- 冻结 protocol v2 的 Snapshot、Event、Page、Error、Card 和 Generation Manifest JSON Schema。
- 增加与 Schema 对应的可执行示例和 Go 契约测试。
- 定义规划中的 HTTP API、安全、分页、错误、幂等、条件请求和 Snapshot 语义。
- 定义 generation 提交、回滚、重建和故障恢复 recipes。
- 增加 Generation Repository 与 HTTP API Adapter 模块详细设计。
- 增加证据门禁驱动的需求到 Dev 自迭代模块、运行 recipe、Evidence Assessment 与 Dev Iteration Schema/示例。

### Changed

- `docs/arch/agent-workbench-architecture.md` 降为方案推演材料；字段、依赖和发布语义以 `docs/gui/` 为准。
- 规划模块的总体架构链接统一指向 `docs/gui/architecture.md`。
- 明确当前实现仍为 protocol v1/单 Engine；v2/多 SessionActor/HTTP/generation repository 均为规划状态。

### Design decisions

- Event sequence 改为 per-scope，不采用全局高频序列。
- 大型 Workspace/历史数据使用 cursor Query Page，不进入 Workbench Snapshot。
- generation 采用不可变资源目录、manifest hash 和原子 current 指针，不允许原地覆盖。
- HTTP 与 Wails 是并列 adapter，共享 Application ports，但不共享 transport DTO。
- 模块依赖必须为 DAG，并纳入自动化验证。
- RAG 从辅助上下文升级为工程证据获取机制；在线使用 evidence readiness，低证据条目不删除，E2E 反馈按需求/架构/详设/Dev/Test 层精确重开。
