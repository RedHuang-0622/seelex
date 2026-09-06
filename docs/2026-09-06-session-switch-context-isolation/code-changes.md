# 运行中会话切换的上下文/作用域隔离：复现与修复

日期：2026-09-06
性质：bugfix 工作记录（复现 → 修复 → 回归），承接当日「热会话切换」系列
关联：[hot-session-switch-intermittent](../2026-09-06-hot-session-switch-intermittent/code-changes.md)、
[followup-cannot-send-after-failed-switch](../2026-09-06-hot-session-switch-intermittent/followup-cannot-send-after-failed-switch.md)

## 1. 现象与现场证据

GUI dev 实例（`dist/seelex-gui-dev/.seelex`）在多会话/多工作区长时间运行后出现三类症状：

- **A. 运行中切换等待/上下文丢失**：≥3 个会话同时在跑时切会话，偶发要等当前
  运行收尾；切过去的会话看不到自己的内容（“正文停在基线/空壳”）。
- **B. 工具根不按会话恢复**：切到其它会话后，路径类工具解析到的项目根不是
  目标会话自己的工作区，而是“上一个/最近被绑定的项目根”（全局根像被后插入
  的缓存生态位一样覆盖了会话自己的工作区）。
- **C. 工作表格/打点跨会话耦合**：一个会话的请求尾部“打点表”
  （`<!-- seelex:worktable:v1 -->` 标记块）里出现**其它会话、其它项目**的活动
  任务行（现场可见 big_result 项目调研子任务的行混进 seelex 任务会话的上下文），
  而自己的行反而缺失 → 上下文串台、历史被污染。
  存储层同步症状：`.seelex/sessions-json/` 下同一会话目录（如
  `session-11de787937bee7ba`）同时出现在 seelex 与 big_result 两个项目分格目录。

## 2. 链路（问题所在的数据通路）

### A：运行中切换（本包以回归固化；主体能力由当日早前 commits 提供）

```
3 会话各 1 次 provider 请求在途（每账号 MaxConcurrency=1）
  ├─ ResumeSession(X) 必须毫秒级返回（hot attach / restoring 空壳 + 后台装载）
  ├─ 每个会话的对话只含自己的内容（Submission 显式路由 sessionID，不许
  │   TOCTOU 重读 current —— 已有 TestStressConcurrentSessionsDoNotPollute）
  └─ 全部释放后各会话收尾 idle
```

### B：工具/工作区根重绑（会话仍在运行时的进程级全局根）

```
bindProjectRootIfSafe(sessionID, rootPath)        application/core/session_scope.go
  ├─ 全局 projectScope = 进程级执行面（工具/工作树仍读全局根）
  ├─ 旧规则：运行中仅当 sessionID != current 才拒绝
  │    └─ 异步冷恢复把视图先切到目标 → 绑定方看到的 current 正是“目标”，
  │       旧规则放行 → 在途会话（原会话后台继续跑）的后续路径工具
  │       解析到新绑定的项目根（跨会话串写，工具根被“后插覆盖”）
  └─ 收尾点：runChat 尾部（进程回到空闲）rebindViewWorkspaceWhenIdle()
       → 全局根/Router 写作用域对齐到当前视图会话的工作区
```

### C：请求尾部“打点表”按进程级注册表取数

```
PrepareExecutionContextFor(sessionID, ...)        application/core/context_runtime
  ├─ 请求尾部注入工作打点表（不落历史、任务完成即删、system 前缀稳定）
  ├─ 旧取数：workTableTraceBlock() → Runtime.TaskSnapshot()（进程级实时注册表）
  │    └─ 实时注册表只属于“当前任务会话”
  │       → 后台会话组装自己下一次请求时，拿到的是活跃会话的打点
  │         （污染注入 + 自己的行缺失）
  └─ 正确取数：按正在组装上下文的 sessionID 取该会话自己的 task scope
       （活跃=实时注册表；后台= TaskSnapshotFor(sessionID) 分区）
```

## 3. 根因（已复现确认）

1. **B 根因**：进程级全局项目根是单例执行面，而会话是并行的。旧
   `bindProjectRootIfSafe` 只拒绝“目标 ≠ 当前视图”的重绑；一旦视图先切到目标
   （异步冷恢复形态），“目标 == 当前”放行就会给**仍在后台运行的原会话**改根。
   不变量应为：**只要还有任意会话在运行中，一律不重绑全局根**；运行中跳过的
   重绑在进程完全空闲后于 runChat 收尾统一对齐到当前视图会话。
2. **C 根因**：打点表生产端口 `WorkTableTraceBlock func() string` 没有会话
   维度。运行时无论哪个会话准备请求，注入的都是进程级实时注册表（当前视图
   会话的打点）——跨会话耦合的直接通道；在把另一项目会话接到同一进程
   （GUI dev 多工作区）时表现为“别的项目的行出现在本会话上下文”。

## 4. 复现（RED）

- **B**：`application/core/session_running_not_rerooted_test.go`
  `TestRunningSessionNotRerootedByAttach`
  视图=A、A 运行中，`bindProjectRootIfSafe(A, rootB)` 必须拒绝重绑；
  空闲后才允许。回退 `session_scope.go`/`chat.go` 修复运行（旧规则放行）：
  ```
  session_running_not_rerooted_test.go:36: bindProjectRootIfSafe rebound global root while a session is running
  --- FAIL
  ```
  带上修复后 PASS（红灯 → 绿灯）。
- **C**：`application/core/work_table_session_scope_test.go`
  `TestS1BackgroundSessionContextMustNotCarryActiveSessionWorkTable` /
  `TestWorkTableTraceBlockForScopesBySession`
  会话 A 为当前任务会话（实时注册表有 A 行）、B 后台分区有自己的行；
  B 准备请求时打点必须只含 B 行。旧全局语义运行：
  ```
  work_table_session_scope_test.go:54: background session lost its own worktable rows:
  "<!-- seelex:worktable:v1 -->...# 工作打点表...- plan:active running A 前台打点..."
  --- FAIL
  ```
  （后台上下文里只有 A 的打点、B 自己的行丢失）修复后 PASS。
- **A**：`repro_three_sessions_running_switch_test.go`
  `TestThreeRunningSessionsSwitchDoesNotWaitAndKeepsContext`
  3 个会话在途阻塞时切换不得 >5s、切后各会话上下文自持、释放后全部 idle
  ——E2E 绿灯回归（行为主体由当日早前会话切换修复提供，本用例固化）。

## 5. 修复

1. `application/core/session_scope.go`：`bindProjectRootIfSafe` 收紧为
   **任意会话运行中一律拒绝重绑**（不读 `current` 做例外）；新增
   `rebindViewWorkspaceWhenIdle()` 在进程完全空闲时把全局项目根与 Router
   写作用域对齐到当前视图会话工作区（当前视图无工作区则清掉残留根）。
2. `application/core/chat.go`：`runChat` 收尾调用
   `rebindViewWorkspaceWhenIdle()`，补齐运行期间因“运行中不改根”跳过的重绑
   （后台会话收尾时当前视图可能已切走，工具根必须跟随当前视图会话）。
3. `application/core/context_runtime/ports.go` +
   `service_assembler.go`：`WorkTableTraceBlock` 端口会话化
   `func(sessionID string) string`。
4. `application/core/context_runtime/coordinator.go`：
   `PrepareExecutionContextFor` 以 `c.workTable(sessionID)` 注入打点表。
5. `application/core/work_table.go`：新增 `workTableTraceBlockFor(sessionID)`
   ——活跃（视图）会话读实时注册表、后台会话读
   `TaskSnapshotFor(sessionID)` scope 分区；`workTableTraceBlock()` 退化为
   活跃包装（空会话 ID）。

## 6. 回归沉淀与验证

新增回归：
- `application/core/session_running_not_rerooted_test.go`（先红后绿，见上）
- `application/core/work_table_session_scope_test.go`（S1 两用例，先红后绿）
- `repro_three_sessions_running_switch_test.go`（A E2E，绿）
- README 分卷刷新（`scripts/gen_core_readme_index.py`）

验证命令与结果：
```text
go test ./application/core -count=1                                     ok (含 S0/S1/Stress 污染回归)
go test -race ./application/core ./seelebridge -count=1                 ok
go test ./seelebridge -count=1                                          ok
go test ./sessionstore ./session ./application/... -count=1             ok
go test . -run TestThreeRunningSessionsSwitchDoesNotWaitAndKeepsContext  ok (0.95s)
go build ./... / gofmt -l <改动文件>                                    干净
```

## 7. 改动文件

- `application/core/session_scope.go`（运行中不改根 + 空闲收尾对齐）
- `application/core/chat.go`（runChat 收尾 rebind）
- `application/core/work_table.go`（打点表会话化取数）
- `application/core/context_runtime/ports.go`（端口签名会话化）
- `application/core/context_runtime/coordinator.go`（按 sessionID 注入）
- `application/core/service_assembler.go`（装配接线）
- `application/core/session_running_not_rerooted_test.go`（新增，B 回归）
- `application/core/work_table_session_scope_test.go`（新增，C 回归）
- `repro_three_sessions_running_switch_test.go`（新增，A E2E 回归）
- `application/core/*/README*.md`（README 分卷刷新，生成器同步）
