# Application 模块化 Qoder 交接

日期：2026-10-27

## 结论

Application 已完成所有可直接通过 owner 重定向退场的结构，当前只剩 4 个持有运行态与
Store 载荷的具体依赖。继续靠“移动目录”会产生 `kernel -> backend/store` 假中立层，
因此后续必须先定义消费方端口和 Contract DTO，再迁移实现。

## 当前基线

- `Service` 公开方法：0
- `application/core` 直接内部依赖：0
- `application/core` 生产文件：55
- `application/core` 生产代码：7,178 行
- `application/**` 具体 Backend/Middle 依赖包：3
- 剩余依赖：
  - `backend/engine/context`
  - `backend/engine/session`
  - `backend/engine/task`

预算入口：

- `application/testdata/application_budget.json`
- `application/testdata/application_wide_dependency_budget.json`

## 必须遵守

1. 先读根 `AGENTS.md`、`MEMORY.md` 和本工作包的 `plan.md`、`implementation.md`、
   `remaining-work.md`。
2. 先跑 `git status`；保留工作树中其他会话的改动，不执行 commit/push、clean、
   reset、rebuild 或 release。
3. 不改变 GUI/TUI wire、JSON 字段、事件 revision/seq 和持久化格式。
4. 每个能力面先写结构或行为红测，再做迁移；旧入口只允许单层兼容转发。
5. 不复制状态机，不把完整 runtime/Store 接口塞进 Context 或 `application/core`。
6. 每批不超过 600 行生产改动；先改依赖方，再删除旧实现。

## 剩余任务

### 1. Engine Context 端口

目标：移除 Application 对 `backend/engine/context` 的直接 import。

真实消费方：

- `application/conversation/context/engine_facade.go`
- `application/conversation/context/application.go`

已知依赖：

- `Coordinator`
- `HistoryCoordinator`
- `Deps`
- `CompactResult`
- `CompactionIndexPort`
- compaction gate/result 常量与纯 helper

建议顺序：

1. 把稳定 `CompactResult`、gate timing、结果状态等纯值下沉 Contract。
2. 在 `application/conversation/context` 定义真实调用方需要的窄接口。
3. 构造 `backend/engine/context.Coordinator` 留在 Wiring/Core 装配，不由 Application
   facade 创建。
4. 删除 `engine_facade.go` 中对 Backend 的类型 alias/constructor。
5. 收紧依赖预算到 2。

### 2. Engine Session 端口

目标：移除 Application 对 `backend/engine/session` 的直接 import。

主要消费方：

- `application/session/runtime/facade.go`
- `application/session/cold/*`
- `application/conversation/turn_{tail,commit,completion,finalize}.go`
- `application/session/fork_lineage.go`

必须先处理：

- `Location` 和纯 helper（title/workspace/record decode/clone）下沉 Contract/Kernel。
- 明确 Session Coordinator 的读、持久化、队列消费、fork/restore 子端口。
- 不允许 Application 持有完整 SessionGranularStore。

建议顺序：

1. Contract 化 `Location` 与跨层纯值。
2. 把 turn/cold/history 所需接口拆成 3～5 个消费方端口。
3. 删除 `application/session/runtime/facade.go`。
4. 收紧依赖预算到 1。

### 3. Engine Task 端口

目标：移除 Application 对 `backend/engine/task` 的直接 import。

主要消费方：

- `application/execution/taskcontext/facade.go`
- `application/conversation/turn_process.go`
- `application/conversation/context/recovery.go`

已知问题：

- `task_context.Coordinator` 的方法集合很大，禁止直接复制成万能接口。
- 先按能力拆：
  - Turn lifecycle / task state
  - Transcript append & sequence
  - Plan projection
  - ReAct budget
  - Tool observation
  - Ledger read
- 构造 `NewCoordinator` 留在 Core/Wiring；Application 只消费窄接口。

验收：

- `application/execution/taskcontext` 不再 import Backend。
- Core 仍可经装配获得具体 Coordinator。
- exact transcript、plan、task 终态行为 parity。
- 收紧依赖预算到 0。

## 最终门禁

```text
gofmt -l application contract kernel middle backend wiring e2e
go test ./application/... ./contract/... ./wiring/... -count=1
go test ./e2e -run 'Facade|Document|Layout' -count=1
go test -race ./application/... -count=1
go build ./...
go vet ./...
python scripts/check_readme_refs.py --strict
git diff --check
```

完成 Application 后不要进入 Store 实现改造；另开 Store 会话，沿用
`docs/2026-10-27-store-ddd-split/plan.md` 和根 `AGENTS.md` §10。
