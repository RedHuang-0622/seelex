# Backend / Middle 领域化实施记录

日期：2026-10-25

状态：**BM-001、BM-002 完成；BM-003、BM-004 因 Application/Wiring 接口配合受阻；BM-005 未启动。**

## 1. 依赖数量

| 边 | 开始时 | 当前 | 目标 |
|---|---:|---:|---:|
| backend → middle | 16 | 6 | 最终 0 |
| middle → backend | 6 | 3 | 0 |
| backend → application | 2 | 1 | 0 |
| backend 内部直接包边 | 106 | 106 | 仅白名单 |

当前 backend → middle 的 6 条：

```text
assemble -> middle/switch
assemble -> middle/view/board
assemble -> middle/view/sessionaccess
engine/context -> middle/protocol/event
engine/session -> middle/protocol/event
limit/approval -> middle/protocol/event
```

后三条是 event 投递中间件，计划随 BM-005 runtime 装配迁出。middle → backend
的 3 条均只读取 `limit.Limits`：

```text
view/board -> backend/limit
view/state -> backend/limit
view/subagent -> backend/limit
```

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

## 3. 部分完成

### BM-003 Middle 只读端口

已完成：

- `view/subagent` 的 `SubagentToolEvent`、`ContextSnapshot` 改为直接使用
  `contract/dto`，删除到 `backend/multiagent/subagent` 与 seelectx snapshot 的边；
- `view/state` 的 think 块过滤实现下沉 `contract/protocol/visible_output.go`，
  backend/react 保留 alias，删除 state → backend/react 的边；
- `view/board` 已确认实际只需要 `EvidenceChars` 一个只读字段。

受阻：三个 view 构造点仍以 `func() limit.Limits` 接收完整 backend 类型。改成
`func() dto.ViewLimits` 会改变公开结构字段类型，必须同步修改 Application/Wiring；
本轮不允许修改这些目录。尝试用泛型或“返回 interface 的函数”保持源兼容均不可行，
因为 Go 不协变函数返回类型，泛型类型也不能在原有复合字面量中省略实例参数。

### BM-004 Backend 反向依赖清理

已完成：

- `backend/engine/prompt` 不再 import `application/prompt`；
- 新增消费者端口 `PromptStackPort` 与 `EffortManagerPort`，Application 的现有指针
  结构直接满足，不需要适配器。

受阻：`backend/engine/task` 仍引用 `application/prompt.PromptLayer` 作为任务状态
字段和激活入口参数。该 named type 不是 alias，只改 backend 会破坏 Application
调用点。

## 4. 精确迁移请求

1. 在 `contract/dto` 增加 `ViewLimits{HistoryWindow, PlanNodeEvents, EvidenceChars}`；
   `backend/limit.Limits` 增加唯一投影方法 `ViewLimits()`。
2. `application/core/service_assembler.go` 中 view/state 与 view/subagent 的
   `Limits: Limits` 改成注入 `func() dto.ViewLimits`。
3. `board.ProjectBoardDeps` 增加 `EvidenceChars func() int`；
   `wiring/registry.ProjectBoardSinkFor` 从生效 limits 注入。
4. 将 `PromptLayer` 下沉 `contract/dto`，`application/prompt` 保留
   `type PromptLayer = dto.PromptLayer`；backend/task 改指契约类型。
5. Application 迁到 `contract/protocol` 后，E2E facade 闭包允许表删除旧 alias
   链或显式登记 `contract/protocol`。

## 5. 兼容层台账

| 旧面 | 新面 | 当前状态 | 删除里程碑 |
|---|---|---|---|
| `middle/protocol/model` | `contract/protocol` | D1 alias/一行转发 | BM-007 |
| `backend/engine/react.VisibleOutputStream` | `contract/protocol` | D1 alias/一行转发 | BM-007 |
| middle 的 `backend/limit` 读数 | `dto.ViewLimits` | D0，等待 Application/Wiring | BM-003 收尾后 BM-007 |
| `state.Core.Events` 的 middle event hub | runtime event port/DTO | D0，等待 BM-005 | BM-005/BM-007 |

## 6. 文件清单

### contract

- `contract/architecture_test.go`（新增）
- `contract/README.md`
- `contract/protocol/README.md`（新增）
- `contract/protocol/state.go`（由 middle model 迁入）
- `contract/protocol/context.go`（迁入）
- `contract/protocol/fork.go`（迁入）
- `contract/protocol/session_input_index.go`（迁入）
- `contract/protocol/toolcall_status_record.go`（迁入）
- `contract/protocol/visible_output.go`（由 backend/react 迁入）

### middle

- `middle/protocol/model/model.go`（新增兼容 facade）
- `middle/protocol/model/alias_identity_test.go`（编译期身份钉子）
- `middle/protocol/model/README.md`
- `middle/protocol/model/{context,fork,session_input_index,state,toolcall_status_record}.go`
  （实体迁出，由 facade 替代）
- `middle/view/subagent/coordinator.go`
- `middle/view/subagent/adapt_test.go`
- `middle/view/subagent/README.md`
- `middle/view/state/coordinator.go`
- `middle/view/state/README.md`
- `middle/view/board/README.md`（记录待注入端口）

### backend

- `backend/engine/prompt/coordinator.go`
- `backend/engine/prompt/README.md`
- `backend/engine/react/visible_output.go`（改为 alias）
- 54 个 backend 文件的 import 从 `middle/protocol/model` 改为
  `contract/protocol`，分布在 `engine/{assemble,context,input,prompt,react,session,skill,task}`、
  `limit/{,approval}` 与 `tool/reference`。

### 工作包

- `docs/2026-10-25-backend-middle-modularization/plan.md`
- `docs/2026-10-25-backend-middle-modularization/implementation.md`（本文件）

本轮没有修改 `application/**`、未执行 commit/push，也未运行 clean/rebuild/release。

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

预期红测：

```text
go test ./contract -run TestBM003AndBM004TargetEdgesAreZero -count=1
```

结果精确列出 3 条 middle → backend 和 1 条 backend → application。

全量 `go test ./...` 另外受两类非本轮代码阻塞：

- `e2e/facade_boundary_test.go` 尚未登记新的 `contract/protocol` 传递闭包；
- 并行中的 Application API 拆分尚未完成，`e2e/facade_narrow_faces_test.go`
  引用了当前不存在的 `application.FaceTypeNames`，且 facade type-identity 用例
  对新增 named API 类型报红。本轮没有修改这些 Application/E2E 文件。
