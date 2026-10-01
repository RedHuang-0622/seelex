# `@` 建议面与「重启后压缩栈为空」：被中断的一轮子代理留下的两个修复

- 日期：2026-10-01
- 触发：用户报告两件事 ——（1）输入框里打 `@` 下面**列不出任何团队**，可团队库里明明还有改良后的
  `goal-a2a`；（2）**重启应用后打开旧会话，右栏「上下文压缩」（压缩栈）整条为空**，用户怀疑与
  初始化上下文有关。
- 范围：`application/core` 的输入建议面（`completion.go` / `agentteam_service.go` / `service_state.go` /
  `input_team.go`）与会话冷恢复（`session_history.go` / `task_context/`）。
- 产出：两个修复 + 各自的复现/回归用例（RED→GREEN 双向证据见 §3）。**运行期的压缩记录口径不动**
  （`_RecordContextCompactionLocked` 的门槛保持原样）。
- 过程（为什么这份记录值得单列）：本轮工作由两批子代理执行。第一批（12:49 起）在上游账号余额失败
  （`HTTP 402 Insufficient Balance`，排除 3 个账号后无可用账号）时被打断：`@` 一侧已把修复
  `commit` 在分支 `seelex/team-summon-at`（`360c66f`），冷恢复一侧的改动只留在工作区、未提交，
  提交与汇报都没走完。恢复这批工作时：`@` 一侧在既有提交之上逐条对照要求，补上两处差距；冷恢复
  一侧逐行复核工作区里的既有改动、补上缺失的分支覆盖后落定。**两个修复都在主工作区验证通过，
  按仓库规范 §7 未自动提交。**

## 1. 现象 1：`@` 下面没有团队了

### 1.1 根因

`application/core/completion.go` 的 `Suggestions()` 在 `SigilTeam` 分支被 2026-10-01「删除内置团队形态」
那一轮**一起清空**了（当时的注释口径：「`@` 没有建议面：候选在团队库里，而 Suggestions 跑在 TUI 的
`View()` 渲染路径与 GUI 每次输入事件上，为它每次按键读一次盘不划算」）。于是：

- 团队库里有条目（`dto.TeamLibraryEntry`），`@<team_id>` 也确实能召唤（`input_team.go` 的
  `resolveTeamSummon` → `AgentTeamLibrary`），但 `@` 一个候选都不弹；
- 用户只能**靠记忆把 team_id 打全**，或提交一个空的 `@` 去读召唤面的回执（`teamSummonHelp`）。

那个取舍只对「**逐键读盘**」成立，不对「**没有建议面**」成立。

### 1.2 改法：读一次、留进程内快照

| 文件 | 改动 |
|---|---|
| `application/core/completion.go` | `Suggestions` 的 `SigilTeam` 分支改为取 `teamSuggestions()`；新增团队库进程内快照 `teamLibrarySuggestions`（自带 `RWMutex`，**按会话 ID 分键**）与 `teamSuggestion` 投影。 |
| `application/core/service_state.go` | 快照挂在既有 `serviceState` 上（与 `roleSessions` / `teamRuntimes` 同形，不走 `Core.ViewMu`：按键路径不该与视图快照事务互等）。 |
| `application/core/agentteam_service.go` | 团队库**写入口**（`AgentTeamSaveTeam` / `SaveCurrentTeam` / `DeleteTeam` / `PublishToGlobal`）与库**读回面**（`AgentTeamLibrary` / `AgentTeamGlobalConfig`）一律让快照过期。 |
| `application/core/input_team.go` | 更新文件头的建议面口径（删掉「不弹补全」的旧注释）。 |

关键口径：

- **命中路径零 I/O**：候选读一次留在进程内，按键只走内存；写库/删库后下一次按键重取一次
  （用例把「读了几次库」也钉住了）。
- **Text 用 `team_id`**：库里 `teamSummonIndex.match` 认 id / 名字 / kind，但展示名可能含空格，而前端把
  `Text` 原样插进输入框（`@<text> ` 再续写附言）——带空格的 Text 会在附言还没写之前就把输入推进
  参数区（面板自己消失）。展示名不另立一条候选（那会变成同一支团队两行、Text 不同），而是进一个
  不导出、不进 JSON 的补充过滤键 `alias`：打 `@改良` 也弹这一行，插进输入框的仍是 `team_id`。
- **失败不缓存**：「此刻读不到」被当成空库缓存下来会压住之后成功的读取。

## 2. 现象 2：重启后旧会话的压缩栈为空

### 2.1 根因（链路）

前端右栏「上下文压缩」的唯一数据源是快照的 `task.context_compactions`
（`gui/frontend/dist/context-summary.js`），运行期唯一事实源是 `TaskExecutionState.ContextCompactions`。
冷恢复把它丢了：

1. `application/core/session_history.go` 的 `resumeSessionCold` 调 `RestoreSessionTaskLocked(RestoredTaskState{…})`
   时只装 `PlanStack / ActivePlanID / Transcript / TranscriptSeq / Checkpoints / ToolResults / Projection /
   FallbackObjective` —— **没有** `record.Execution.Task`；
2. `application/core/task_context/coordinator.go` 的 `RestoredTaskState` 也没有承载「会话上下文事实」的字段；
3. `task_context_state.go` 的 `restoreTaskProjectionLocked` 用 `projection` 重建 `st.taskExecution`
   （`projection == nil` 时干脆置 `nil`），**不含** `ContextCompactions` / `ContextRetainedFrom`；
4. 于是 `_TaskStateFor` / `VisibleTaskStateFor` 返回的 `TaskState.ContextCompactions` 恒空 →
   `session_scope.go` 的 `snapshotOfResident` 组装出的 `snapshot.Task.ContextCompactions` 空 →
   右栏压缩栈表空。

注意「冷读面」`session_cold_read.go` 的 `snapshotOfCold` **是**读 `record.Execution.Task` 的——所以
**未驻留**会话反而正常；一旦会话被冷恢复成驻留会话，驻留读面就把它丢了。这解释了为什么现象是
「重启之后」。

两条后果比「面板为空」更重：

- **重启后下一次落盘抹历史**：`sessionRecordLocked` 从 `TaskStateFor` 取 `Execution.Task`，内存态空就
  把磁盘上的 `ContextCompactions` 写成空——**不可逆**，此后该会话右栏永久为空；
- **保留窗口起点归零**：`ContextRetainedFrom` 是「已被折出的前缀」的边界，归零会让下一次装配把这段
  前缀重新计入上下文预算（长会话稳定越线、每回合重新压一次）。

### 2.2 改法

| 文件 | 改动 |
|---|---|
| `task_context/coordinator.go` | `RestoredTaskState` 增 `ContextCompactions []model.ContextCompaction` + `ContextRetainedFrom int`。**选两个显式字段而不是整份 `Task *model.TaskState`**：`TaskState` 里还带着 `RequestID / Status / Summary / UpdatedAt` 这些**回合事实**，整份传进来等于邀请恢复路径伪造一个重启后已不存在的回合身份；显式字段把「会话事实可还原 / 回合事实不可还原」写在类型上。 |
| `session_history.go` | `resumeSessionCold` 从 `record.Execution.Task` 深拷贝压缩记录、推定保留窗口起点，并改按**目标会话**路由装载（`RestoreSessionTaskLockedFor(sessionID, …)`）：冷加载可能不激活视图（`mayActivate=false`），写活跃槽会让目标会话的任务面恒空、下一次落盘照样抹掉自己的历史。 |
| `task_context/task_context_state.go` | `restoreTaskProjectionLocked` 两个分支都把压缩栈/保留窗口起点落回 `st.taskExecution`；`projection == nil` 但确有压缩记录时按冷加载会话上下文状态重建（`StatusIdle`、`RequestID` 留空、`ContextVersion` 取记录里的最大值当下界）；`taskService` 绑定被恢复的那个会话。 |
| `task_context/plan_transcript.go` | 新增纯函数 `RetainedFromForCompactions(events, compactions) int`：取记录里最大的 `EventTo`、在事件流里按 `Seq` 定位、命中下一条即保留窗口起点；定位不到返回 0（不猜）。调用方只在**存储事件流**上用它（`RecordConversationTranscript` 重建的事件流序号被重编码，在那里定位会把窗口错误推到会话中段——丢历史比重折一次严重）。 |

这是 2026-09-23 修过的「折叠之后的下一轮压缩不见所踪」在**重启/冷恢复**上的孪生：会话上下文事实属于
**会话**，不属于回合，进程重启同样不该把它丢掉。

## 3. 验证

命令与结果（Windows 本地，`go 1.25`）：

```text
go build ./...                                                     → exit 0
go vet ./application/core/...                                      → exit 0
gofmt -l <改动的 .go 文件>                                          → 无输出
git diff --check                                                   → 无输出
go test ./application/core/... -count=1 -timeout=600s              → 全部 ok（含 task_context / context_runtime / session_runtime / resume / agentteam …）
go test ./application ./gui -count=1                               → ok
python scripts/gen_core_readme_index.py                            → exit 0（根包分卷覆盖自检通过）
```

复现证据（RED = 把生产改动回退到 `HEAD`、只留用例；GREEN = 恢复）：

```text
# 现象 1 —— 回退 completion.go / service_state.go / agentteam_service.go
--- FAIL: TestSuggestionsListTeamLibraryAndFollowLibraryWrites
    input_team_suggestions_repro_test.go:57: `@` 应列出团队库条目：[]core.Suggestion(nil)
--- FAIL: TestSuggestionsScopeTeamLibraryPerSession
    input_team_suggestions_repro_test.go:153: sess-a 的 `@` 应列出 A 侧的库：[]
FAIL  github.com/RedHuang-0622/seelex/application/core  0.374s
# 恢复后：4 支 TestSuggestions* 全 PASS（含空库/读不到库安静为空）

# 现象 2 —— 回退 session_history.go / task_context/{coordinator,plan_transcript,task_context_state}.go
--- FAIL: TestReproCompactionStackSurvivesProcessRestart
    红灯：重启冷加载后快照任务面丢了压缩记录（task=&{… ContextCompactions:[] …}）
    红灯：重启后的一次落盘把 record.Execution.Task.ContextCompactions 抹成空
--- FAIL: TestReproCompactionStackVisibleWithoutProjection
    红灯：没有 projection 时压缩栈没有落回内存任务面（task=<nil>）
--- FAIL: TestReproCompactionRetainedFromRestoredFromRecord
    红灯：保留窗口起点没有按 record 的压缩区间还原：4 → 0（want 4）
# 恢复后：4 支 TestReproCompaction* 全 PASS

```

用例位置：

- `application/core/input_team_suggestions_repro_test.go`：`@` 列出库条目、按 id 与按**展示名**前缀过滤、
  参数区不弹面板、逐键只读 1 次库、写库/删库后各重取 1 次、**按会话分键**、空库/读不到库安静为空；
- `application/core/context_compactions_restore_repro_test.go`：真跨一次「进程重启」（同一份 store 上新建
  Service + `ResumeSession`），三条判据 —— ①驻留快照带记录且 `Version/FrameRef/EventFrom/EventTo` 与
  落盘一致；②重启后一次落盘不把 `record.Execution.Task.ContextCompactions` 抹空；③无 projection 时压缩栈
  照样落回内存任务面且 `RequestID` 留空；外加保留窗口起点还原。

## 4. 边界与遗留

1. **另一进程改团队库不会自发现**：`@` 建议面的快照只在**本进程**的库写路径与库读回面失效；外部编辑器或
   另一个 Seelex 实例改了 `team/library.json` 时，本进程停在旧库直到上面两类事件之一发生。要有界收口只能加
   文件指纹或定时重读——那等于把「逐键零 I/O」换成「逐键 stat 一次盘」，对建议面不值（有意接受）。
2. **`ContextRetainedFrom` 在事件流被重建过时不推导**：`resumeSessionCold` 里若 transcript 因 durable 事件流
   缺失而被 `RecordConversationTranscript` 重建（序号重新编码），调用方**不**调用
   `RetainedFromForCompactions`，起点保持 0。后果是下一次装配可能把已经不在事件流里的旧前缀再计入一次预算
   （那段前缀本身不可见时会重新生成一个帧，不会重复进上下文），比推一个错边界（可能在会话中段切断历史）安全。
3. **`application/core/README-session.md` 与 `application/core/goal/README.md` 的生成器漂移未纳入本次改动**：
   `scripts/gen_core_readme_index.py` 会顺手修掉这两处与本次缺陷无关的陈旧索引（`check_readme_refs.py` 报的
   127 处未解析引用即属此类存量），为保持改动的单一主题，本轮把它们回退了。
4. **`gofmt -l .` 在 `main` 上本就有两处既有不合规**（`application/core/chat_hot_attach_reasoning_repro_test.go`、
   `application/core/history_safety.go`），与本次改动无关；本次改动的 11 个 `.go` 文件全部干净。
5. **真实 API 未复跑**：两个缺陷的复现都用确定性夹具（无网络、无凭据），证明的是**链路形状**；`@` 建议面的
   GUI/TUI 真机交互（弹面板、Tab 补全、中文展示名命中）仍需人工冒烟——运行中的 GUI 是旧构建，要重开进程才会
   加载新的 `dist/*.js`。
