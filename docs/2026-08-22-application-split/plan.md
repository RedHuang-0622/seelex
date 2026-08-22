# Application 容器化与 core 拆分方案

日期：2026-08-22
状态：方案 + 执行记录（本文件随阶段推进更新）
范围：`application/` 内部结构重组；不动业务语义，保持 `application` 门面 API 稳定
前置：`docs/2026-08-12-module-split/plan.md` 明确"不拆 application 内部结构（另立方案）"，本方案即为该另立方案。

## 1. 背景与目标

现状诊断（2026-08-22 盘点）：

- `application/core` 46 个非测试文件、16,453 行，`chat.go` 1339 行，边界只能靠约定维持。
- `contract` 一包两职：端口接口 + DTO + `Dependencies` 装配输入，且 `Dependencies` 直接持有
  `*event.EventHub`、`*approval.ApprovalBroker` 具体类型，合约层不是纯边界。
- `adapters` 是后端适配器却寄居在 `application/` 下，且反向 import 父门面 `application`；
  连带 `seelebridge/tools/websearch` 也 import 前端门面。
- `model`（前端协议 DTO）反渗后端类型：`seelebridge.ScheduledTaskStatus`、
  `seelebridge/plan.PlanEdge` 等；`docs/2026-08-14-decoupling/01` 已盘点同类重复。

目标：

1. 建立"合约层容器 / 前端逻辑容器 / 后端逻辑容器"三层结构；
2. `core` 按 chat/session/task/subagent/context/worktable/input/prompt/service 拆包，
   用装配件 + 消费方窄接口强制边界；
3. 每阶段保持 `go build ./...` 与既有测试全绿；外部调用方只依赖 `application` 门面。

## 2. 目标结构

```text
contracts/                  # 合约层：零实现、零后端依赖（迁移顺序见 P1/P4）
  ports.go                  # 端口接口 + Dependencies（全部接口化）
  dto/                      # 跨层结构体单源
  model/                    # Snapshot/Message/Plan 等前端协议 DTO
  event/                    # 事件协议类型 + Hub 接口

application/                # 前端逻辑容器：用例编排 + 权威状态 + 事件投影 + 门面
  application.go            # 兼容门面（alias，外部 API 不变）
  approval/ prompt/ search/ console/
  core/
    service/                # Service 门面、核心锁 + Snapshot、装配根
    chat/ session/ task/ subagent/ context/ worktable/ input/ prompt/

internal/adapters/          # 后端逻辑容器适配面（自 application/adapters 迁出）

seelebridge/ session/ sessionstore/ workspace/ plugin/ skill/   # 后端领域实现
```

## 3. core 拆分机制（装配件 + interface）

- 状态所有权唯一：锁与 `Snapshot` 由 Service 持有；各域子状态随代码下沉到域包。
- 接口定义在消费方（延续 `contextTaskPort`/`sessionTaskProjectionPort` 模式），装配根注入；
  provider 包可用编译期断言固化实现。
- `serviceAssembler` 是唯一装配根：`state := ...` → 依次构造各域 coordinator → 组装 `serviceComponents`。
- `core` 根包保留门面导出（`Service`、`Limits`、`LoadWindowConfig`、`CompressedTurnArchiver` 等，
  `main.go` 依赖），子包禁止 import `core` 根包。

## 4. 阶段计划

| 阶段 | 内容 | 验证 |
|---|---|---|
| P0 | 方案确认 + 存档 | 评审 |
| P1 | 合约层净化：`Dependencies` 接口化（`event.Hub`/`contract.ApprovalBroker`）；`model` 对后端类型的引用收敛到 `dto` 单源 | `go build ./...` + `go test ./application/...` |
| P2 | adapters 归位：`git mv` 到 `internal/adapters`，改吃 contract 类型、去掉对门面 import；websearch 解耦 | `go build ./...` + `go test ./e2e/...` |
| P3 | 拆大文件：同包内按职责拆分 `chat.go`（1339 行）、`internal/adapters/adapters.go`（1040 行）与巨型测试文件 `service_test.go`（2220 行），行为零变化 | `go build ./...` + `go test ./application/... ./internal/adapters/... -count=1` |
| P4 | core 子包化：先抽消费方接口 → 状态下沉到共享内核 → 组件化 → `git mv`；顺序 session → task → subagent → context → worktable → chat → input → prompt | 每步 `go test ./application/... -count=1` |
| P5 | 容器化收尾：合约层提升顶层（待拍板）；README/AGENTS/docs 同步；race + rebuild-gui 冒烟（按 MEMORY.md 预警流程） | 全量测试 + `make rebuild-gui VERSION=dev` |

## 5. 风险与约束

- 物理搬移前必须先组件化（seelebridge P3 经验："高耦合方法不能跨包"）。
- `ToolHookBridge` 与 `OnIterationComplete` 在 Session 锁内的重入约束在拆包后保持不变。
- 外部 API 只经 `application` 门面；alias 数量不减少。
- 实施时保留工作区未提交改动（worktable traceboard 相关文件），不覆盖、不混入。

## 执行记录

### P0（2026-08-22）

- 本方案文档入库。

### P1 合约层净化 —— 完成 ✅（2026-08-22）

- `event` 新增窄接口 `Hub`（Publish/Subscribe），`*EventHub` 编译期断言实现。
- `contract.Dependencies.Events/Approval` 改为接口（`event.Hub` / `contract.ApprovalBroker`），
  装配根负责注入默认实现。
- `approval.NewApprovalBroker` 改为接受 `event.Hub`。
- `model` 对后端类型的引用收敛到 `dto` 单源：`ScheduledTaskStatus`/`ScheduledCommandInfo`/
  `PlanEdge`（`PlanEdge` 定义迁入 `dto/plan.go`，`seelebridge/plan` 以 alias 复用）。
- 验证：`go build ./...` + `go test ./application/... -count=1` 全绿。

### P2 adapters 归位 —— 完成 ✅（2026-08-22）

- `git mv application/adapters → internal/adapters`：适配层改吃 `contract`/`model`/`approval`
  类型，删除对 `application` 门面的 import；`main.go`/`e2e` 引用同步。
- `git mv application/search → seelebridge/search`（后端能力归位）；
  `seelebridge/tools/websearch` 改吃 `seelebridge/search`；
  `application` 门面保留 `WebSearchConfig`/`WebSearch` 兼容别名。
- README/`e2e/layout_test.go` 模块清单同步。
- 验证：`go test ./application/... ./internal/adapters/... ./seelebridge/search/... ./seelebridge/tools/websearch/... ./e2e/...` 全绿。

### P3 拆大文件 —— 进行中（2026-08-22）

范围与拆分原则：同一包内按职责拆分，纯机械搬移、行为零变化；每文件保留完整
函数/类型/常量声明，import 按实际使用裁剪。目标：

- `application/core/chat.go`（1339 行）→ `chat_flow.go`（聊天流程/流式增量）+
  `tool_hooks.go`（工具事件 + ToolHookBridge）+ `plan_tools.go`（Plan 打点/replan）。
- `internal/adapters/adapters.go`（1040 行）→ `engine_port.go`（EnginePort）+
  `runtime_port.go`（RuntimePort）+ `session_workspace_ports.go`（Session/Workspace/
  Plugin/Skill 端口）+ `messages.go`（消息转换与审批适配）。
- `application/core/service_test.go`（2220 行）→ 按测试域拆为
  `service_session_test.go` / `service_chat_test.go` / `service_snapshot_test.go` 等。

### P3 拆大文件 —— 完成 ✅（2026-08-22）

- `chat.go`（1339 行）→ `chat.go`（410，聊天流程/流式增量）+ `plan_tools.go`（641，Plan
  打点/replan）+ `tool_hooks.go`（354，工具事件 + ToolHookBridge）。
- `internal/adapters/adapters.go`（1093 行）→ `engine_port.go`（435）+ `runtime_port.go`
  （136）+ `session_workspace_ports.go`（423）+ `messages.go`（134）。
- `service_test.go`（2220 行）→ `service_helpers_test.go`（fakes/工具）+ 5 个按测试域拆分的
  文件（session 23 / chat 8 / plan 10 / snapshot 5 / input 6，共 52 个测试，数量不变）。
- 验证：`go build ./...` 全绿；`go test ./application/... ./internal/adapters/... -count=1` 全绿。

### P4 core 子包化 —— 第一阶段进行中（2026-08-22）

已落地零依赖叶子域：

- `core/chat/`：`stream_batcher.go` + `visible_output.go`（+ 测试随包迁移），
  导出 `StreamBatcher`/`VisibleOutputStream`/`StripThoughtBlocks` 等。
- `core/worktable/`：`worktable_publisher.go`（+ 测试随包迁移），
  导出 `WorkTablePublisher`/`WorkTableUpdate`。

根包引用同步改为 `chat.*`/`worktable.*`；两包均配 README；测试全绿。
下一阶段：session/task/subagent/context/prompt/view 有状态协调器下沉到共享
状态内核（`core/internal/state`），按"先抽消费方接口 → 状态下沉 → 组件化 →
git mv"执行。

### P4 core 子包化 —— 叶子包设计与装配方案（2026-08-22）

#### 叶子包清单与职能

| 包 | 职能 | 关键类型/属性 | 依赖 |
|---|---|---|---|
| `core/chat` | 流式批次与可见输出 | `StreamBatcher`/`VisibleOutputStream` | `seelexctx/lifecycle` |
| `core/worktable` | 工作表格增量 CSP 汇聚发布 | `WorkTablePublisher`/`WorkTableUpdate` | `application/model` |
| `core/input_router` | 命令注册表 + 输入路由 | `CommandRegistry`/`Router`/`CommandFunc` | `application/model` |
| `core/context_control` | 窗口策略配置加载 | `WindowConfig`/`LoadWindowConfig` | `seelexctx` |
| `core/internal/state` | 共享状态内核：锁 + 权威 Snapshot + 端口依赖 + 事件/审批 | `Core{Mu, Snapshot, Deps, Events, Approval}` | `contract`/`model`/`event` |
| `core/session_runtime`（目标） | 会话持久化/目录/项目绑定 | `Coordinator` + `sessionRuntimeState` | `state.Core` + `TaskPersistencePort` |
| `core/task_context`（目标） | 任务执行/checkpoint/transcript | `Coordinator` + `taskRuntimeState` | `state.Core` + 消费方端口 |

#### 依赖解耦与装配（interface 模式）

```text
package session_runtime          # 消费方定义窄接口
  type TaskPersistencePort interface {
      Transcript() []model.TranscriptEvent
      PendingToolResults() []model.StoredToolResult
      TaskCheckpoints() []model.TaskCheckpoint
      ContinuationSummary(requestID string) string
      PushLoadedPlanLocked(arguments string, now time.Time)
      ...
  }
  type Deps struct { Core *state.Core; Tasks TaskPersistencePort }
  func NewCoordinator(deps Deps) *Coordinator   # a := NewA(b)

package task_context              # 实现 TaskPersistencePort
  func (c *Coordinator) Transcript() []model.TranscriptEvent { ... }

package core                      # 装配根（serviceAssembler）
  tasks := task_context.NewCoordinator(task_context.Deps{Core: kernel})
  sessions := session_runtime.NewCoordinator(session_runtime.Deps{Core: kernel, Tasks: tasks})
```

依赖方向：`core` 根包 → 域包 → `core/internal/state`；域包之间只依赖消费方
窄接口，禁止反向依赖 `core` 根包，避免循环。

#### 迁移顺序（高耦合域）

1. `core/internal/state` 内核落地：`mu/snapshot/deps/events/approval` 从
   `serviceState` 迁入（字段大写化，编译器兜底全部引用点）。
2. `core/session_runtime`：sessionCoordinator + session 状态 + 纯 helper 迁出；
   对 task/plan 状态的读写走 `TaskPersistencePort`；根包 Service 方法经
   `components.sessions` 装配调用。
3. `core/task_context`：taskContextCoordinator + task 状态迁出；实现
   `TaskPersistencePort`；chat 编排经端口访问。
4. subagent/context/prompt/view 依同法逐个下沉。

### P4 core 子包化 —— 完成 ✅（2026-08-22）

按 `design.md` 迁移步骤完成 S0–S3：

- S0 内核落地：`serviceState` 嵌入 `*state.Core`（`Mu/Snapshot/Deps/Events/
  Approval` 迁入内核），机械改名按接收者白名单 + 编译器兜底完成；
  `serviceAssembler` 经 `state.New` 构造内核。
- S1 `core/session_runtime`：会话协调器/状态/纯 helper 迁出；`TaskPersistencePort`
  定义并注入；catalog worker 随迁；跨域纯逻辑（预算/内部标记/警告文本/标题
  还原）以装配端口注入，避免反向依赖根包。
- S2 `core/task_context`：task/plan 子状态、`TaskService`、
  `CalibratedTokenCounter`、`planProjectionReader` 迁出；实现
  `TaskPersistencePort`（编译期断言）与 context 域 `TaskPort`；新增
  `internal/limits` 叶子包解耦 `Limits()`；chat/plan_tools/context 等根包
  调用点全部改走 `components.tasks` 导出方法。
- S3 域包逐个下沉：`view_state`（Snapshot 读写/事件发布/runtime 投影）、
  `prompt_layer`（system prompt 组装）、`subagent_view`（详情/live/树投影）、
  `context_runtime`（上下文装配/压缩 + 历史安全）完成物理归位。
- 各包补齐 README；`e2e/layout_test.go` 模块清单同步。

验证：`go build ./...`、`go test ./application/... ./internal/adapters/... -count=1`
全绿；`gofmt -l` 干净；`application` 门面导出符号不变；`serviceState` 不再
持有 mu/snapshot/deps/events/approval；session/task 协调器物理位于子包。
