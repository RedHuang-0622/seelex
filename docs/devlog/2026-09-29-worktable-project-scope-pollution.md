# 切换项目后的跨项目工作表格污染（请求尾部打点块）复现与修复（2026-09-29）

> 日期: 2026-09-29 | 范围: `application/core`（`workspace_usecase.go` 的项目切换路径）
> 承接: [2026-09-19-worktable-global-scope.md](2026-09-19-worktable-global-scope.md)（工作表格 = 项目/全局台账、打点块 = 会话级实发面）、
> [2026-09-06 会话切换隔离](../2026-09-06-session-switch-context-isolation/code-changes.md)（S1：打点块按会话取数）、
> [2026-09-14-session-project-root.md](2026-09-14-session-project-root.md)（工具根按会话分格）

修复类工作：先红灯复现 → 只改根因 → 红转绿 → 沉淀回归。

## 1. 症状（红灯复现）

用户在项目 A 里干活（会话 A 的实时注册表带着活动任务），切到项目 B：
**项目 B 会话的第一轮请求尾部打点块里出现了项目 A 的活动任务行**——上下文里
混进别的项目的打点，而项目 B 自己的行（还没有）当然也看不到。

复现（`application/core/work_table_project_scope_test.go`，修复前三红）：

```text
--- FAIL: TestProjectSwitchDoesNotInjectForeignWorkTableRows
    work_table_project_scope_test.go:146: 跨项目工作表格污染：项目 A 的活动任务进了项目 B 会话 "session-new" 的打点块：
        <!-- seelex:worktable:v1 -->
        # 工作打点表（系统维护，只读；任务状态与打点以工作表格为准）
        - task:1 pending A 项目的活动任务
        <!-- /seelex:worktable:v1 -->
    work_table_project_scope_test.go:156: 跨项目工作表格污染：项目 A 的活动任务被前置进项目 B 的请求输入
--- FAIL: TestProjectSwitchWorkTableBlockStaysCleanAcrossCompaction   # 折叠轮 + 折叠后一轮，都重拼出这一行
--- FAIL: TestProjectSwitchRebindsTaskRegistryToNewSession            # 注册表/执行纪元指针仍停在项目 A 的会话上
```

现场（同一夹具的实测读数，修复前）：

```text
sessionA=draft_..._1  sessionB=session-new
runtime.currentTaskSession = draft_..._1          ← 实时注册表仍属项目 A 的会话
SessionIDForRequest(task-routing) = draft_..._1   ← 新项目的回合登记到旧项目的会话
CurrentTaskExecutionFor(B) = nil ; CurrentTaskExecutionFor(A).RequestID = task-routing
block(B) = "- task:1 pending A 项目的活动任务"     ← 打点块把 A 的行当成本会话自己的行
```

## 2. 根因

项目切换的落点是 `workspace_usecase.go` 的 `bindWorkspaceInfo`。当目标项目与当前
项目不同、且当前会话已有历史时，它走 `startFreshSession` 分支：**新建并激活一条
独立会话**（`newGeneratedSessionID("ws")` + `Engine.ActivateSession`），把
`Snapshot.Session.ID` 换成新 ID，然后……**没有走会话切换协议**。

对比其余四条换会话的路径（全部成对调用）：

| 路径 | 换会话指针 | `sessions.SetActive` | `Runtime.SwitchSessionTasks` |
|---|---|---|---|
| `/new`（`session_draft.go`） | ✓ | ✓ | ✓ |
| 热挂载（`session_lifecycle.go`） | ✓ | ✓ | ✓ |
| 冷恢复 / 冷加载（`session_history.go`） | ✓ | ✓ | ✓ |
| 草稿物化（`session_draft.go`） | ✓ | ✓ | （沿用草稿指针） |
| **项目切换新建会话（本文）** | ✓ | **✗ → 已补** | **✗ → 已补** |

两个缺口的直接后果：

1. **会话域活跃指针没换** → `SessionIDForRequest` 反查落回旧会话：新项目的回合
   按旧指针登记（`CurrentTaskExecutionFor(新会话) == nil`，旧会话却拿到了这个
   `RequestID`），任务纪元 / plan / transcript 这一整套会话级状态都落在旧会话里。
2. **任务注册表指针没换** → 实时注册表仍属于旧项目的会话，而打点块对"视图会话"
   走 `workTableTraceBlockFor` 的「视图会话 = 实时注册表」那条读面
   （`TaskSnapshotFor("")`），于是把旧项目的活动任务当成新会话自己的行前置进
   `currentInput`。

## 3. 与压缩时机的关系（两条装配路径都复现）

打点块的注入发生在 `context_runtime.prepareExecutionContextFor` 的**最前面**——
折叠判据之前：

```go
if block := c.workTable(sessionID); block != "" { currentInput = block + "\n\n" + currentInput }
...
rawTokens := c.tasks.CountRequestTokens(systemPrompt, fullContext, currentInput, tools)
fold := rawTokens >= budget.SoftThreshold || hardCompact || options.forceCompact
```

因此污染有两个时机面，且**折叠不是一次清洗**：

- 时机①（未压缩装配）：污染块原样作为首轮请求输入发给 provider；
- 时机②（折叠轮 / 折叠后一轮）：块在判据之前就拼进了 `currentInput`，既进
  `rawTokens`（判据量）也进压缩后的请求；块按轮重拼（数据源是注册表，不是历史），
  所以折叠之后下一轮照样重新拼一遍。

回归用例把两个时机分别钉住（`...StaysCleanAcrossCompaction` 先断言这一轮**真的
折叠了**，否则两条时机其实是同一条路径、用例没有判别力）。

## 4. 修复（只改根因）

`application/core/workspace_usecase.go`：`startFreshSession` 分支补齐会话切换协议。

- `ViewMu` 临界区内：`service.sessions.SetActive(currentSessionID)`（与 `Snapshot.Session.ID`
  同处，顺序照热挂载）；
- 出临界区后：`Runtime.SwitchSessionTasks(currentSessionID, TaskSnapshotFor(currentSessionID))`
  → `ClearSubagentTree()` → `refreshWorkTableFromSources()`（顺序照 `/new`）。

旧项目的行**不丢**：`SwitchSessionTasks` 把它们搬进该会话自己的 scope 分区，
全局台账（工作表格）照旧看得见它们、归属会话也不变——工作表格是项目/全局台账、
打点块是会话级实发面，这条边界沿用 2026-09-19 的口径，不在本次改动里动。

## 5. 回归

```text
go test ./application/core/ -run "TestProjectSwitch" -count=1     # 三红 → 五绿
go test ./application/core/... ./seelebridge/... -count=1         # 全 ok
go test -race ./application/core/ -run "TestProjectSwitch|TestS0|TestS1|TestBackgroundSession" -count=1
go build ./... ; go vet ./application/core/ ; gofmt -l <改动文件>  # 干净
```

新增（`application/core/work_table_project_scope_test.go`，五条）：

- 时机①：`TestProjectSwitchDoesNotInjectForeignWorkTableRows`（数据面 + 实发面）
- 时机②：`TestProjectSwitchWorkTableBlockStaysCleanAcrossCompaction`（折叠轮 + 折叠后）
- 数据面：`TestProjectSwitchRebindsTaskRegistryToNewSession`（注册表指针 + 会话路由）
- 边界（防过度修复）：`TestProjectSwitchKeepsForeignRowsInLedger`（台账不得丢行、归属不得改写）
- 对照：`TestProjectSwitchWithoutFreshSessionKeepsOwnRows`（重复绑定同一项目不换会话，本会话自己的行照旧在块里）

README 分卷索引由 `scripts/gen_core_readme_index.py` 刷新（只多出本用例一节）。

## 6. 残留风险 / 未做

- **同族路径未逐一排查**：`UnbindWorkspace` 不换会话（无此问题）；`fork` 沿父会话
  项目（一期禁止跨项目 fork）。其余"只改 `Snapshot.Session.ID` 不换指针"的写法若
  以后新增，仍会复现同一类污染——本文件的表就是给后来者的对照表。
- **子代理树**：本次随协议一起 `ClearSubagentTree()`（与 `/new` 同序）。测试夹具的
  `fakeRuntime.ClearSubagentTree` 是空实现、`fakeEngine.SubAgentTree` 又是单会话桩，
  无法在单测里分辨"清的是哪棵"，故未加断言；生产侧由 `refreshWorkTableFromSources`
  的同一条路径重投影。
- **打点块的"不落历史"**：本次未改动。块落在 `currentInput`（回合内工作历史），
  回合收尾 `ReleaseWorkingHistoryFor` 清的是 provider 工作视图，下一轮按 transcript
  重建——不在本次根因链上。
