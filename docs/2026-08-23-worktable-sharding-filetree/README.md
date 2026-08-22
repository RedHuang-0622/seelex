# Worktable 多维分片 + 工作台文件树（打点表）

> 性质：一次性工作包（plan + 打点表）。对应任务：
> ① worktable 多维分片（批次维度）与 task/tasklist 解耦、条目类型命名；
> ② 工作台文件数 + 工作树（替代「Agent 已读文件」）+ 文件内容详情查看调研。
> 本文先给问题定位（代码证据）、目标设计与改动范围（含需查看的文件清单），
> 然后由子代理按文件归属矩阵并行实施。

## 0. Root 复核修订（2026-08-23，开工前）

以下修订基于对当前代码的逐文件核对，子代理必须以此为准：

1. **文件路径勘误**：`seelebridge/task_registry.go` 实为
   `seelebridge/task/task.go`；`seelebridge/todo_tool.go` 实为
   `seelebridge/task/tools.go`。任务注册表域在 `seelebridge/task/` 子包
   （README 与 `application/core/work_table.go` 注释中的旧路径是历史遗留）。
2. **白名单缺口（已确认）**：`main.go defaultManualRules()` 目前只含
   `todolist_init/add/done/status`、`task_complete/failed/needs_user_decision`，
   **没有 `taskadd`**（现网 taskadd 在 manual 模式实际不可用）。本次新增
   `todo_*`/`task_add` 规范名时必须一并补白名单（新旧名同列，兼容期内
   旧名保留）。
3. **批号归属风险（设计取舍，v1 接受）**：`SubAgentTreeNode`/`PlanNode`
   均无 `RequestID` 字段；plan/subagent 生命周期是异步同步（
   `consumeSubagentLifecycle`/`consumePlanNodeEvents` 消费者），若子代理
   完成后 sync 发生在**下一次 chat 请求期间**，「当前批次」会错盖到新请求。
   v1 采用「`startChat` 设置注册表默认批次 + 工具创建时盖章」方案，并接受
   此归属偏差（批次是展示分组，不是权限边界）；未来若需要精确溯源，再给
   节点 DTO 增加 `RequestID` 下沉（本期不做）。
4. **工作树隐私红线**：树接口只返回元数据（路径/名称/类型/计数），**绝不
   返回文件内容**；忽略规则默认含 `.git`、`.seelex`、`dist`、`node_modules`，
   且从展示中排除 `config/accounts.yaml` 与 `*.local.yaml` 文件名（仅元数据
   也不展示，避免暴露真实配置文件名）；禁止读取这些文件做统计。
5. **契约测试约束**：`e2e/docs_contract_test.go TestGUIDocumentContracts`
   会校验 `docs/gui/examples/work-table.json` 必须满足更新后的
   `work-table.schema.json`，改 schema 必须同步 example；`protocol.js`
   `worktable.changed` reducer 仍要求 `payload.items` 为数组（`batches`
   只做增量附加，不破坏既有判定）。
6. **工作区现状**：`application/core/chat.go` 已有一处用户改动
   （`ApplyActiveTaskSystemPrompt` 改为无条件调用），任何子代理不得回退或
   混入无关 hunk；`docs/README.md` 已由上个工作包登记索引，追加时保留既有行。

### 0.2 Root 开工核验（2026-08-23，实机复测）

以下事实已由 root 在开工前逐条复核，子代理可直接采信：

1. `main.go:1052 defaultManualRules()` 确认**无 `taskadd`**（仅
   `todolist_init/add/done/status` 与 `task_complete/failed/needs_user_decision`），
   新增规范名时必须同步补白名单。
2. 工具注册实际发生在 **`seelebridge/runtime_tools.go`**（
   `task.NewTools(...).RegisterTaskTools()/RegisterTodoTools()`，
   第 95/100 行），不是 `seelebridge/runtime.go`——3.1 的文件清单已勘误。
3. `e2e/docs_contract_test.go`、`docs/gui/examples/work-table.json`、
   `workspace/workspace.go`、`internal/adapters/runtime_port.go`、
   `docs/gui/modules/workspace-sandbox.md` 均存在。
4. `application/core/chat.go:75` 为无条件 `ApplyActiveTaskSystemPrompt(requestID)`
   （用户改动），保持不动。
5. `git status`：`application/core/chat.go`（用户改动）、`docs/README.md`
   （上个工作包索引）为已修改文件；两个 2026-08-2x 工作包目录为未跟踪新目录。

## 1. 问题定位（代码证据）

### 1.1 worktable 批次混排

- `application/core/work_table.go` 的 `buildWorkTable` 把注册表全部
  `TaskRecord` 扁平化为一张表，仅按 `phase` 顺序 + ID 排序，没有任何
  “批次/请求”维度；`TaskRecord`/`TaskSpec`/`WorkItem` 都没有
  `BatchID`/`CreatedAt` 字段。
- 后果：用户先后提出多个请求（各产生 todo/task/plan/subagent 条目），
  所有条目堆在同一张表里，只能靠 `limits.work_table_rows`（默认 200）
  截断，无法区分“哪一批任务”。

### 1.2 类型粒度与工具命名

- `TaskRecord` 同时有 `Phase`（plan/tasklist/subagent/task）与 `Kind`
  （plan/todo/subagent/task）两个轴，映射松散（todo→tasklist、task→task）；
  前端筛选用 `phase`，后端语义用 `kind`，产生双轨漂移。
- 证据：`docs/gui/schemas/work-table.schema.json` 的 `phase` enum 只有
  `["plan","tasklist","subagent"]`，漏了 `task`；`kind` enum 只有
  `["plan","todo","subagent"]`，漏了 `task`。而前端
  `work-table.test.mjs` 已经出现 `phase:"task"` / `kind:"task"` 行。
- 工具命名未体现类型粒度：`todolist_init/add/done/status`（todo 类型）、
  `taskadd`（task 类型）、`task_complete/failed/needs_user_decision`
  （会话级终态协议）混在一起，名称无法从词面区分条目类型。

### 1.3 task 与 tasklist 耦合

- `seelebridge/task/task.go`：`todolist_*` 与 `taskadd` 共享同一注册表，
  注册表内部以 `state.todo []string` 维护“todo 有序列表”，
  `TodoItem`/`TodoItemStatus` 作为兼容 DTO 残留在
  `application/contract/dto/task.go`（注释已标注 2026-12-31 移除窗口）。
- `RuntimePort.TodoSnapshot()/SetTodoStatus()` 与
  `Service.UpdateWorkItemStatus`（只认 `todo:<index>` 前缀）是耦合的
  直接结果：todo 状态走 index 专用通道，而 task/plan/subagent 走另一套。
- `validateTaskTransition` 目前不区分 kind：todo 条目理论上可被置为
  `queued/running/retry/failed` 等非法状态。

### 1.4 工作台「已读文件」功能

- 右栏「Agent 已读文件」区块：`gui/frontend/dist/index.html`（
  `source-count`/`project-sources`）→ `app.js renderProject` 用
  `collectReadFileSources(conversation, read_files)` 渲染资料源列表。
- 后端持久化：`application/core/session_runtime/archive.go`
  `RecordReadFileLocked`（read_file 成功时记录 `ReadFileRef` 进
  `SessionRecord.Execution.ReadFiles`）。
- 问题：只展示“Agent 读过哪些文件”，无目录树、无文件数、无未读文件
  概览，信息密度低且与 Workspace 沙盒规划（
  `docs/gui/modules/workspace-sandbox.md` §Files）脱节。

## 2. 目标设计

### 2.1 Task 1：worktable 多维分片 + 类型解耦

- 新增**批次维度**：`TaskRecord`/`TaskSpec`/`WorkItem` 增加
  `BatchID` + `CreatedAt`；批次 = 发起该批条目的 chat 请求
  （`requestID`，形如 `chat-<nano>`），由 application 在 `startChat`
  时写入 Runtime 注册表默认批次，工具创建与生命周期同步自动盖章。
- **类型轴收敛**：`Kind` 为唯一权威类型
  （`todo | task | plan | subagent`）；`Phase` 保留为派生展示字段
  （todo→tasklist、task→task、plan→plan、subagent→subagent），
  创建时由 Kind 派生，前端筛选迁移到 `kind`。
- **每类状态机**：`validateTaskTransition(kind, current, next)` 限定
  todo ∈ {pending, doing, completed}；task/plan/subagent 维持现有迁移。
- **工具命名**：新增规范名 `todo_init/todo_add/todo_done/todo_status`、
  `task_add`；旧名 `todolist_*`/`taskadd` 保留为兼容别名（deprecated
  描述）。`task_complete/failed/needs_user_decision` 维持“会话级终态
  协议”定位，文档明确其与条目级工具的区别。
- **表格分片呈现**：`worktable.changed` payload 增加
  `batches: [{id, label, created_at, counts}]`；前端按批次分组渲染
  （批次头 = 标签 + 各类计数 + 可折叠），批内按类型 chips 过滤
  （全部/Plan/Task/Todo/Subagent）；`task.changed` 单行增量保持。

### 2.2 Task 2a：工作台文件数 + 工作树（实施）

- 后端 `workspace/` 新增目录树查询：`ListTree`（containment + 忽略规则 +
  dir-first 排序 + 有界）+ `CountFiles`（递归统计文件数）。
- application 增加 optional 端口（`WorkspaceTreePort`，新文件避免与
  worktable 改动冲突）：root 只能来自后端当前 workspace，客户端只能传
  相对路径。
- GUI Bridge 增加 `WorkspaceTree(relPath, depth)` /
  `WorkspaceFileCount()`；右栏把「Agent 已读文件」替换为「工作树」
  （惰性展开目录 + 文件数 badge），project status 的「资料源」改为
  「文件数」。
- 后端 `ReadFileRef` 归档保留（会话证据），不再作为右栏主 UI。

### 2.3 Task 2b：文件内容详情查看（调研）

- 产出 `docs/research/2026-08-23-file-content-preview.md`：约束分析、
  方案对比（后端截断 + 前端高亮 / 后端预高亮 / CodeMirror/Monaco /
  diff 渲染）、开源库清单与推荐、落地步骤。仅调研不改代码。

## 3. 改动范围与需查看文件清单

### 3.1 子代理 A：worktable 分片 + 解耦 + 命名

需查看（审计入口）：

| 文件 | 查看重点 |
|---|---|
| `application/contract/dto/task.go` | TaskRecord/TaskSpec/TodoItem 定义 |
| `application/model/state.go` | WorkItem/WorkTableEvent/CloneWorkItems |
| `application/core/work_table.go` | buildWorkTable/排序/taskRecordToWorkItem |
| `seelebridge/task/task.go` | 注册表 actor、addTaskLocked、validateTaskTransition |
| `seelebridge/task/tools.go` | todolist_*/taskadd 注册与 handler |
| `seelebridge/runtime_tools.go` | 工具族装配（RegisterTaskTools/RegisterTodoTools 注入点） |
| `application/core/chat.go` | startChat（设置当前批次） |
| `gui/frontend/dist/work-table.js` / `protocol.js` | 表格渲染与增量 reducer |
| `docs/gui/schemas/work-table.schema.json` / `examples/work-table.json` | 契约 |

改动文件（A 独占）：

| 文件 | 改动 |
|---|---|
| `application/contract/dto/task.go` | +BatchID/CreatedAt；Phase 派生；Todo 注释 |
| `application/model/state.go` | WorkItem +BatchID/BatchLabel/CreatedAt；WorkTableEvent +Batches；WorkTableBatch 定义 |
| `application/core/work_table.go` + 相关测试 | 批次分组排序、BatchLabel 计算、taskRecordToWorkItem 透传 |
| `application/contract/ports.go` | RuntimePort +SetCurrentTaskBatch |
| `internal/adapters/runtime_port.go` | 委托 |
| `seelebridge/task/task.go` | defaultBatchID、SetDefaultBatch、按 kind 状态机 |
| `seelebridge/task/tools.go` | todo_*/task_add 新名 + 旧名兼容别名、BatchID 盖章 |
| `seelebridge/runtime_tools.go` | SetTaskBatch 接线（工具 Deps 注入默认批次） |
| `application/core/chat.go` | startChat 设置当前批次 |
| `application/core/service_fakes_test.go`、`e2e/scenario/harness.go`、`gui/tool_full_chain_test.go` | RuntimePort 桩 |
| `main.go` | defaultManualRules 增补 todo_*/task_add |
| `gui/frontend/dist/work-table.js` + `work-table.test.mjs` | 批次分组渲染 + 类型 chips + 用例 |
| `gui/frontend/dist/protocol.js` + `protocol.test.mjs` | batches 增量 reducer + 用例 |
| `docs/gui/schemas/work-table.schema.json` + `examples/work-table.json` | kind/phase enum 补 task；batch 字段；batches 定义 |
| `docs/gui/modules/work-table.md`、`application/core/README-work-table.md`、`gui/frontend/README.md` | 批次/类型/工具命名/状态机文档 |

### 3.2 子代理 B：文件树 + 文件数（实施）

需查看：

| 文件 | 查看重点 |
|---|---|
| `docs/gui/modules/workspace-sandbox.md` | Files tab 规划、PathGuard、FilePreview |
| `workspace/workspace.go` | Repo/RootPath/原子写 |
| `seelebridge/security/project_scope.go` | containment 语义 |
| `gui/bridge.go` | Bridge 方法绑定模式 |
| `gui/frontend/dist/app.js` `renderProject`、`index.html` | 已读文件区块结构 |
| `gui/frontend/dist/read-sources.js` | 待替代实现 |

改动文件（B 独占）：

| 文件 | 改动 |
|---|---|
| `workspace/tree.go`（新）+ `workspace/workspace_test.go` | ListTree/CountFiles、忽略规则、containment、测试 |
| `application/contract/workspace_tree.go`（新） | optional WorkspaceTreePort 接口 |
| `application/core/workspace_usecase.go` | Service.WorkspaceTree / WorkspaceFileCount |
| `gui/bridge.go` + `gui/bridge_test.go` | WorkspaceTree/WorkspaceFileCount 绑定 + 测试 |
| `gui/frontend/dist/index.html` | 已读文件区块 → 工作树区块 |
| `gui/frontend/dist/app.js` | renderProject 改造（文件数 + 树） |
| `gui/frontend/dist/worktree-view.js`（新）+ `worktree-view.test.mjs`（新） | 惰性树渲染 + 计数 + 测试 |
| `gui/frontend/dist/styles.css` | 树样式 |
| `gui/frontend/dist/read-sources.js` | 删除引用（文件可保留 deprecated 或删除） |
| `docs/gui/modules/workspace-sandbox.md` | 可选：现状小节补充 |

### 3.3 子代理 C：文件内容详情查看（调研）

| 产出 | 说明 |
|---|---|
| `docs/research/2026-08-23-file-content-preview.md`（新） | 约束、方案对比、开源库清单、推荐路线、落地步骤 |

## 4. 并行冲突矩阵

| 文件/目录 | 归属 |
|---|---|
| `application/contract/ports.go`、`dto/task.go`、`model/state.go`、`core/work_table*`、`seelebridge/task/*`、`seelebridge/runtime.go`、`main.go`、`work-table.js`、`protocol.js`、`work-table.schema.json` | A |
| `workspace/tree.go`、`application/contract/workspace_tree.go`、`application/core/workspace_usecase.go`、`gui/bridge*`、`app.js`、`index.html`、`worktree-view.js`、`styles.css`、`read-sources.js` | B |
| `docs/research/2026-08-23-file-content-preview.md` | C |
| `docs/2026-08-23-worktable-sharding-filetree/`（本目录） | root |

禁止跨区改文件；跨区需要（如 A 需要 B 的 DTO 字段）先在消息中同步再动。

## 5. 验证清单（root 收尾执行）

```text
go build ./...
go vet ./...
gofmt -l .
go test ./application/core ./seelebridge ./workspace ./gui ./e2e -count=1 -timeout=180s
node --test gui/frontend/dist/*.test.mjs
go test . -run TestGUIDocumentContracts -count=1
```

最终由 root 汇总验证、同步模块 README、更新 docs 索引并汇报。
