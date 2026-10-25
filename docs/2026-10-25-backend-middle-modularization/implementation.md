# Backend / Middle 领域化实施记录

日期：2026-10-26

状态：**BM-001 至 BM-004 完成；BM-005 已生成迁移清单与边界门禁，文件搬迁未开始。**

## 1. 依赖数量

| 边 | 开始时 | 当前 | 目标 |
|---|---:|---:|---:|
| backend → middle | 16 | 6 | 最终 0 |
| middle → backend | 6 | 0 | 0 |
| backend → application | 2 | 0 | 0 |
| backend 内部直接包边 | 106 | 107 | 仅白名单 |

当前 backend → middle 的 6 条：

```text
assemble -> middle/switch
assemble -> middle/view/board
assemble -> middle/view/sessionaccess
engine/context -> middle/protocol/event
engine/session -> middle/protocol/event
limit/approval -> middle/protocol/event
```

后三条是 event 投递中间件，计划随 BM-005 runtime 装配迁出。BM-003 完成后
middle → backend 已归零；BM-004 完成后 backend → application 已归零。新增的
一条 backend 内部边是 `engine/task -> limit/loop`：任务状态直接使用执行预算的
权威值类型，不再经 Application prompt 兼容类型绕行。

## 2. 已完成的批次

### BM-001 依赖预算

- 新增 `contract/architecture_test.go`，冻结四组跨层边和 106 条 backend 内部
  生产包边；
- 冻结 34 个既有超 500 行文件，只允许缩短；新文件不得超过 500 行；
- `TestBM001DependencyBudgetIsFrozen` 与 `TestBM001FileSizeBudget` 通过。

### BM-002 共享协议模型契约化

- 将 `middle/protocol/model` 的权威 DTO、clone helper 与工具调用存档转换下沉到
  `contract/protocol`；
- `middle/protocol/model` 只保留同身份 alias、常量转发和 helper 一行转发；
- backend 54 个生产/测试文件的 import 改为 `contract/protocol`；
- backend → middle 中 10 条 `middle/protocol/model` 边降为 0；
- JSON tag、字段名、类型身份、revision/seq 语义均未修改；原 middle model 用例
  通过 alias 继续覆盖实现。

## 3. 已完成端口收口

### BM-003 Middle 只读端口

已完成：

- `view/subagent` 的 `SubagentToolEvent`、`ContextSnapshot` 改为直接使用
  `contract/dto`，删除到 `backend/multiagent/subagent` 与 seelectx snapshot 的边；
- `view/state` 的 think 块过滤实现下沉 `contract/protocol/visible_output.go`，
  backend/react 保留 alias，删除 state → backend/react 的边；
- `view/board` 已确认实际只需要 `EvidenceChars` 一个只读字段。

- `contract/dto.ViewLimits` 冻结 `HistoryWindow`、`PlanNodeEvents`、`EvidenceChars`
  三格；`backend/limit.Limits.ViewLimits()` 是唯一投影点；
- `view/state` 与 `view/subagent` 改收 `func() dto.ViewLimits`，Application 在装配处
  唯一投影；
- `board.ProjectBoardDeps.EvidenceChars` 由 Wiring 从生效 limits 注入，
  `BuildProjectBoardRows` 只接收该窄值；
- 三条 `middle -> backend/limit` 生产边全部删除。

### BM-004 Backend 反向依赖清理

已完成：

- `backend/engine/prompt` 不再 import `application/prompt`；
- 新增消费者端口 `PromptStackPort` 与 `EffortManagerPort`，Application 的现有指针
  结构直接满足，不需要适配器。

- `PromptLayer` 下沉 `contract/dto`，`application/prompt` 保留同身份 alias；
- `backend/engine/task` 的 skill 层改指 `dto.PromptLayer`，ReAct 预算直接使用
  `backend/limit/loop.Budget`；
- `backend -> application` 生产边归零。

## 4. 契约收尾

1. 中间层需要运行时上限时只接收 `dto.ViewLimits` 或更窄字段，禁止 import
   `backend/limit`。
2. `ProjectBoardDeps.EvidenceChars` 是装配注入端口；投影函数不得自取全局 limits。
3. backend 需要 prompt 形状时使用 `dto.PromptLayer`；需要执行预算时使用
   `backend/limit/loop.Budget`，不得经 Application alias 绕行。
4. Application 的 facade 闭包显式登记 `contract/protocol`；E2E 的 wire 词表门禁
   直接读取 `contract/protocol/state.go`。

## 5. 兼容层台账

| 旧面 | 新面 | 当前状态 | 删除里程碑 |
|---|---|---|---|
| `middle/protocol/model` | `contract/protocol` | D1 alias/一行转发 | BM-007 |
| `backend/engine/react.VisibleOutputStream` | `contract/protocol` | D1 alias/一行转发 | BM-007 |
| middle 的 `backend/limit` 读数 | `dto.ViewLimits` | D1 alias/唯一投影，旧边已删 | BM-007 |
| `application/prompt.PromptLayer` | `dto.PromptLayer` | D1 alias，backend 已迁契约 | BM-007 |
| `state.Core.Events` 的 middle event hub | runtime event port/DTO | D0，等待 BM-005 | BM-005/BM-007 |

## 6. 文件清单

### contract

- `contract/architecture_test.go`（新增）
- `contract/runtime_migration_test.go`（BM-005 文件归属与组合根反向依赖门禁）
- `contract/README.md`
- `contract/protocol/README.md`（新增）
- `contract/protocol/state.go`（由 middle model 迁入）
- `contract/protocol/context.go`（迁入）
- `contract/protocol/fork.go`（迁入）
- `contract/protocol/session_input_index.go`（迁入）
- `contract/protocol/toolcall_status_record.go`（迁入）
- `contract/protocol/visible_output.go`（由 backend/react 迁入）
- `contract/dto/prompt_layer.go`（新增）
- `contract/dto/view_limits.go`（新增）
- `backend/limit/view_limits.go`（新增唯一投影）

### middle

- `middle/protocol/model/model.go`（新增兼容 facade）
- `middle/protocol/model/alias_identity_test.go`（编译期身份钉子）
- `middle/protocol/model/README.md`
- `middle/protocol/model/{context,fork,session_input_index,state,toolcall_status_record}.go`
  （实体迁出，由 facade 替代）
- `middle/view/subagent/coordinator.go`
- `middle/view/subagent/adapt_test.go`
- `middle/view/subagent/README.md`
- `middle/view/subagent/{coordinator,adapt_test,record_attribution_test}.go`
- `middle/view/state/coordinator.go`
- `middle/view/state/README.md`
- `middle/view/board/{projection,syncer,projection_test,syncer_test}.go`
- `middle/view/board/README.md`

### backend

- `backend/engine/prompt/coordinator.go`
- `backend/engine/prompt/README.md`
- `backend/engine/react/visible_output.go`（改为 alias）
- `backend/engine/task/{coordinator,task_context_state,task_execution}.go`
- `backend/engine/task/coordinator_async_budget_test.go`
- 54 个 backend 文件的 import 从 `middle/protocol/model` 改为
  `contract/protocol`，分布在 `engine/{assemble,context,input,prompt,react,session,skill,task}`、
  `limit/{,approval}` 与 `tool/reference`。

### 工作包

- `docs/2026-10-25-backend-middle-modularization/plan.md`
- `docs/2026-10-25-backend-middle-modularization/implementation.md`（本文件）
- `docs/2026-10-25-backend-middle-modularization/runtime-migration-inventory.md`

### Application / Wiring / E2E

- `application/prompt/stack.go`（PromptLayer 同身份 alias）
- `application/core/service_assembler.go`（注入 `dto.ViewLimits`）
- `application/core/project_board_read_test.go`
- `wiring/bootstrap/bootstrap.go`
- `wiring/registry/registry.go`
- `test/project_board_{write,wiring,i7}_test.go`
- `e2e/facade_boundary_test.go`
- `e2e/dto_boundary_test.go`
- `e2e/subagent_status_vocabulary_gate_test.go`

本轮未执行 commit/push，也未运行 clean/rebuild/release。

## 7. 验证

通过：

```text
go test ./contract -run 'TestBM001|TestBM002' -count=1
go test ./backend/... ./middle/... -count=1
go test ./test -count=1
go test -race ./backend/multiagent/teamwork ./backend/engine/assemble ./middle/view/... -count=1
go build ./...
go build -tags "gui,desktop,production" ./...
go vet ./...
node --test gui/frontend/dist/*.test.mjs
git diff --check
```

目标边门禁已转绿：

```text
go test ./contract -run 'TestBM00' -count=1
go test ./contract -run 'TestBM005' -count=1
go test ./e2e -run 'TestRoleSessionDTOFieldsMatchWireContract|TestStateEnumFieldCastInDeclaredFiles|TestSubagentStatusVocabularyGate|TestFacadeDependencyClosureRatchet|TestContractDependencyClosureIsStdlibOrAllowlisted' -count=1
go test ./test -run 'ProjectBoard' -count=1
go test ./wiring/... -count=1
```
