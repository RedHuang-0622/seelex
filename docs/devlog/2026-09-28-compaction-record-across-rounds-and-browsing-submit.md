# 折叠记录跨回合消失 + 回看期间的用户行被吞（2026-09-28）

> 触发：用户报告两条现场症状（同一轮反馈）：
>
> 1. 「我在第一个对话轮次对话完了之后，查看之前的对话历史……导致了长期看不到最新的消息
>    甚至是只有后续对话完成了才做出刷新，否则界面刷新都不刷新一下，点击跳转到最新情况的
>    按钮也是没有任何用处」；
> 2. 「折叠执行之后的下一轮对话压缩已经不见所踪」；以及「用户输入被吞」。
>
> 两条症状是**两处状态归属错误**，都落在「哪一层的事实该由谁继承 / 哪一层的判据在该越界」
> 这一类问题上。本文记：根因、红灯复现、修法与仍然存在的边界。

## 1. 缺陷一：折叠记录与保留窗口起点在下一回合被丢掉

### 现场

折叠（`/compact`、达峰自动压缩、冷加载维护身份下的显式压缩）之后，**下一轮对话**里：

- 状态页「上下文压缩」条目、轨迹「压缩」轨刻度、对话区那条折叠分界一起消失；
- 保留窗口起点退休为 0：下一次装配把**已被折出的前缀**重新计入上下文，长会话稳定越过
  软阈值，于是「一发消息一条压缩记录」。

### 根因

`application/core/task_context/task_execution.go` 的 `continuationTaskExecutionState` 把
**回合续接判据**（`IsContinuableStatus`）当成了**会话上下文事实**的门：

```go
state := NewTaskExecutionState(requestID, objective, effort)
if previous == nil || !IsContinuableStatus(previous.Status) {
    return state            // ← ContextVersion / ContextRetainedFrom / ContextCompactions 全在这里被丢掉
}
```

而 `IsContinuableStatus` 只认 `running / interrupted / blocked / needs_user_decision`：

- 回合正常收尾后状态是 `completed`（`task_service.go`）；
- 冷加载维护身份撤销后状态是 `idle`（`session_context_maintenance.go`，
  `BeginSessionContextMaintenanceLocked` / `EndSessionContextMaintenanceLocked`）。

两者都不在名单里，于是**每一次正常回合边界**都会把上一轮的压缩记录与保留窗口起点清空。
`session_context_maintenance.go` 的注释其实已经写明契约——「下一次 `BeginTask` 照常开新回合，
并把这份**上下文状态**当作上一份状态处理」——代码没有做到。

### 修法

会话上下文事实无条件继承；只有回合事实（objective / Plan 参数 / checkpoint / 工具签名 /
`ProgressEpoch` / `TokenAudit`）继续留在续接判据后面：

```go
if previous == nil {
    return state
}
state.ContextVersion = previous.ContextVersion           // 会话事实：无条件继承
state.ContextRetainedFrom = previous.ContextRetainedFrom
state.ContextCompactions = append([]model.ContextCompaction(nil), previous.ContextCompactions...)
if !IsContinuableStatus(previous.Status) {
    return state
}
```

## 2. 缺陷二：回看历史时发出的输入被吞

### 现场

用户向上翻更早历史（窗口锚定在更早位置、`history_offset + 可见条数 < total_messages`）之后
直接发下一条消息：输入框清空、后端确实开了这一轮，但对话区里自己的消息与整个回复都不出现，
界面在整个回合里一动不动，只有等回合收尾落盘（或手动「回到最新」）之后才刷新。

### 根因：两条各自正确的规则叠加成一个洞

- 后端 `view_state.AppendMessageWithOriginLockedFor`：窗口未贴尾时，新行只推进
  `TotalMessages`、**不写进可见窗口**（写进去会在窗口与尾之间插一道谁也不显示的断层，
  并且把正在回看的用户拽回尾部）；
- 前端 `gui/frontend/dist/protocol.js` 的 reducer：`historyWindowed` 时同样**不把
  `message.added` 落进列表**（同样是为了避免断层），只提示「下方还有新内容」，内容留给
  「回到最新」的基线刷新带回。

两条规则都没有覆盖「**用户主动发起的回合**」：回看是阅读手势，发言是参与手势——用户按回车
意味着「从现在起看最新」。缺失这条分流时，这一轮自己的行落在窗口之外，事件又被 reducer
按同一判据丢掉，于是它在两边都不存在。

### 修法

用户行是这条规则的边界：追加前先结束回看（窗口原地重置为「以这条新消息为尾」，与内容 LRU
卸载后的重置同一形状），再照常追加。窗口因此重新贴尾：`historyWindowed` 转假，前端不再吞
事件，本轮的用户行、助手行与流式增量都落在用户正在看的位置；更早的历史仍可经
`LoadMoreHistory` 冷读翻回（`HasMoreHistory` 不谎报）。助手/工具行继续保持原规则
（`TestAppendWhileBrowsingHistoryKeepsWindow` 钉住的正是那一侧）。

## 3. 红灯复现（先红后绿）

| 红灯用例 | 复现前判据 |
|---|---|
| `TestReproCompactionRecordSurvivesNextRoundAfterColdMaintenance` | `压缩记录在下一回合被丢掉：0 条（want 1）` |
| `TestReproContextFactsSurviveCompletedTurnBoundary` | `回合收尾边界丢掉压缩记录：0 条（want 1）` |
| `TestReproSubmitWhileBrowsingKeepsUserRowInWindow` | `回看状态下提交的用户行不在可见窗口里……offset=8 visible=[durable-8 … durable-13] total=22` |
| `TestReproSubmitWhileBrowsingEndsBrowsingState` | `用户已发言，会话不该再被当成「回看中」` |

文件：`application/core/context_compact_across_rounds_repro_test.go`、
`application/core/session_history_browsing_submit_repro_test.go`（两条用例共用
`session_history_pagination_test.go` 的分页夹具，包含「内存窗口领先磁盘」的在飞回合形状）。

## 4. 已知边界（本轮刻意不动）

1. **回合进行中主动翻回更早页**：在飞（尚未到发布点）的行只活在内存窗口里；用户在中途翻页会
   让它们滑出窗口，此时「回到最新」只能带回**已发布**的那部分，剩余部分要等这一轮落盘
   （`README-session.md` §6~§9 的既有口径）。缺陷二的修法覆盖的是「提交后窗口必须贴尾」，
   不是「任意时刻翻页都无损」。
2. **压缩记录的持久化口径未变**：记录仍挂在会话上下文状态上、随 `record.Execution.Task`
   落盘；本轮只保证**内存态跨回合不丢**（这也是「下一轮就不见了」的直接来路），没有把它
   迁到会话级独立存档。

## 5. 验证

```text
gofmt -l application/core                                     (空)
go test ./application/core/ -run TestRepro -count=1            ok（四条红灯用例转绿）
go test ./... -count=1 -timeout=600s                          全绿（含根包 repro_* / gui / seelebridge）
node --test gui/frontend/dist/*.test.mjs                      pass 492 / fail 0（前端未改）
python scripts/gen_core_readme_index.py                       刷新 context / session / view_state 索引
```

索引与叙述同步：`application/core/README-session.md`（分页契约第 4 条、第 10 条）、
`application/core/view_state/README.md`（窗口锚点新增「用户行例外」）、
`application/core/task_context/README.md`（跨回合继承的口径）、`CHANGELOG.md`（Unreleased/Fixed）。
