# 冷加载会话的 `/compact`：当场折叠，不再只登记（2026-09-24）

> 触发：用户在**刚冷加载**的会话里执行 `/compact`，拿到的是
> 「当前会话没有进行中的执行纪元（例如刚冷加载或刚清空），现在没有可折叠的请求上下文；
> 已登记：下一条消息组装上下文前立即压缩，压缩结果对那条消息生效。」
> 他接着问「我需要你的摘要内容」——而命令早已结束，登记的兑现要等下一条消息，
> 用户看到的是"按了没反应"。
>
> 本文记：为什么"没有在飞回合"被读成了"没有可折叠的上下文"、会话级维护身份怎么补上
> 这件事、以及有牙证明与仍然存在的边界。

## 1. 根因：判据是"有没有在飞回合"，需求是"有没有可折叠的内容"

显式压缩（`/compact`、`compact_context`）的落点是
`context_runtime.Coordinator.CompactContextNow`：它要求 **该会话有匹配当前 request 的
执行纪元**（`TaskExecutionState.RequestID`），因为 `prepareExecutionContextFor` 的装配、
`RecordContextCompactionLocked` 的记录、checkpoint 落盘都按 request 路由。

冷加载的会话**并不缺可折叠的上下文**：`resumeSession` 已把 transcript（事件尾窗）与
引擎历史（provider 尾窗）装载好，`/compact` 也确实能算出判据量、能装配。它缺的只是
"一个在飞回合的 `RequestID`"。而判据 `state == nil || state.RequestID == ""` 把
"没有在飞回合"直接读成了"没有可折叠的请求上下文"，于是走了登记分支：

```go
// 改前（application/core/context_runtime/coordinator.go）
state := c.tasks.CurrentTaskExecutionFor(sessionID)
if state == nil || strings.TrimSpace(state.RequestID) == "" {
    c.ScheduleForceCompact(sessionID)                 // 记一件待办
    return CompactResult{Outcome: CompactScheduled}, nil
}
```

2026-09-23 那轮拒绝"伪造纪元"是对的——真把 `state.RequestID` 写成某个回合 ID，
"有人在跑这个会话"就会漏进 `ChatState.Running`、任务注册表与快照 `RequestID`。但落到
"只登记"就把用户**明确要求**的压缩推迟到了他自己再发一条消息之后：命令已结束、界面
已回到空闲，用户合理地认为"这条命令什么也没做"。

## 2. 改法：会话级维护身份（而不是伪造回合）

`task_context` 新增 `session_context_maintenance.go`：

| 入口 | 作用 |
|---|---|
| `SessionMaintenanceRequestPrefix`（`session-maintenance:`） | 维护身份前缀；与真实回合 ID（`chat-<nanos>-<seq>`）不可能混淆，`sessionForRequestLocked` 只按绑定表反查 |
| `StatusIdle`（`"idle"`） | 会话持有上下文状态但没有在飞回合（前端状态面本就把"没有任务"渲染成 `idle`，不新增看不懂的状态值） |
| `BeginSessionContextMaintenanceLocked(sessionID) string` | 打开（或复用）维护身份：没有任务状态时按会话自己的事实建一份上下文状态（objective = transcript 里最后一条真实用户输入，跳过技能正文等内部材料）；**已有在飞回合或已有别的身份时返回空串**（不抢占真实身份） |
| `EndSessionContextMaintenanceLocked(sessionID, requestID)` | 撤销身份（`RequestID` 归空 + 解绑）：**保留**压缩产生的 `ContextVersion` / `ContextCompactions` / checkpoint——它们是会话的上下文事实，不是回合事实；快照任务面若还挂着维护身份，按会话上下文状态重建一次 |

`context_runtime` 侧：

```go
// CompactContextNow：冷加载/刚清空 → 当场折叠（返回 handled=false 表示该走纪元路径）
if result, handled, err := c.compactSessionContextWithoutEpoch(sessionID); handled {
    return result, err
}
```

- 有匹配 request 的纪元 → 与改前完全一致；
- 没有纪元但**有可折叠材料**（`hasFoldableSessionContext`：transcript 有事件，或引擎
  历史里有非 system 消息）→ `Begin` → 同一条显式折叠路径（判据量照算、记录照落、
  帧正文照出、引擎历史照换、按会话落盘）→ `End`（`End` 在成功与失败两条路径上都执行）；
- 没有纪元且**没有材料**（新建/刚清空的空会话）→ 维持登记语义：折叠空上下文只会产出
  一条区间为空的记录，那是把"没做事"记成"做了事"。

三条诚实约束写在代码与 README 里，并由测试钉住：不写 `ChatState.Running`、不设快照
`Chat.RequestID`、不建任务注册表条目；身份必须成对撤销。

### 2.1 回执：命令与工具共用同一句

`/compact` 命令过去在"已落记录"分支里**不读** `result.Note`，而是自己套
`compactionRecordNote(result)`；因此"这次压缩为什么能当场生效"必须写进那一句，否则命令
回执会把 `NoEpoch` 悄悄丢掉（首版实现就是这么漏的，被
`TestCompactCommandWithoutEpochFoldsImmediately` 抓到）。现改为：

- `ContextCompactionResult` 新增 `NoEpoch`（JSON `no_epoch`，omitempty）；
- `compactionRecordNote` 在 `NoEpoch` 时前置一句「会话没有在飞回合（冷加载或刚清空），
  已按会话级显式压缩立即执行（折叠已装载的上下文并落记录，无需下一条消息）：」；
- 登记分支的说明也改成真实原因：「没有在飞回合、也没有已装载的对话材料（空会话）」。

## 3. 有牙证明（红 → 绿）

| 测试 | PROBE（改回旧行为） | 结果 |
|---|---|---|
| `TestCompactContextWithoutTaskExecutionCompactsImmediately` | `compactSessionContextWithoutEpoch` 开头 `return CompactResult{}, false, nil` | 红：`冷加载会话必须当场压缩并留记录（不是登记）：{Compacted:false … Note:当前会话没有进行中的执行纪元…已登记…}` |
| `TestCompactWithoutEpochKeepsExecutionFacesClean` | 同上 | 红：`压缩后会话应保留上下文状态（版本/记录/checkpoint 的落点）` |
| `TestCompactCommandWithoutEpochFoldsImmediately` | 同上 | 红在「已登记」断言（回执只有登记措辞） |
| `TestCompactEmptySessionRegistersAndRedeemsOnNextMessage` | —（这条钉住**不变**的语义：空会话登记 + 下一条消息兑现） | 全程绿；改前同一夹具（旧用例）覆盖同一路径 |

第三条新用例在首版实现里抓到过一个真实缺陷：`compactSessionContextWithoutEpoch` 的
"空会话"分支最初只返回 `CompactScheduled` 而**忘了** `ScheduleForceCompact`，回执说
"已登记"但没有任何登记（下一条消息不会先压后发）。这正是"夹具顺序"（先追加轮次再登记
vs 先登记再追加）能验出来的差异——两种顺序现在都有用例覆盖。

## 4. 验证（本机、本轮）

```text
gofmt -l application/core                                   （空）
go build ./...                                              ok
go vet ./application/core/...                               ok
go test ./application/core/ ./application/core/context_runtime/ ./application/core/task_context/ -count=1
    ok  application/core            8.2s
    ok  application/core/context_runtime
    ok  application/core/task_context
go test ./application/... -count=1                          （全绿）
go test ./... -count=1 -timeout=120s                        无 FAIL（含根包 41.5s）
node --test gui/frontend/dist/compaction-format.test.mjs gui/frontend/dist/context-summary.test.mjs
    pass 18 / fail 0
python scripts/gen_core_readme_index.py                     （只刷新 context_runtime / task_context / README-context）
```

前端无需改动：状态页「上下文压缩」列表本来就读 `snapshot.task.context_compactions`，
任务状态面在没有任务时展示的默认值就是 `idle`（`gui/frontend/dist/app.js`），与
`StatusIdle` 同字面量。

## 5. 已知边界（本轮刻意不扩大范围）

1. **记录的持久化口径未变**：压缩记录挂在会话的上下文状态上，随
   `record.Execution.Task` 落盘；下一条消息开新回合时按 `IsContinuableStatus` 重建状态
   （不继承记录），因此该列表可能在**下一次回合收尾持久化**后不再出现在记录里——这与
   "回合之间压缩"的既有口径一致。帧正文（`frame_ref`）仍在会话内容存储里可按 ref 回读，
   折叠后的引擎历史也仍然是模型手里的上下文。
2. **帧正文的证据区**：冷加载且没有任务投影时，`objective` 取会话最后一条真实用户输入，
   plan 尾部照常保留；区间与四区 token 事实完整，但没有 durable checkpoint 证据时不会
   凭空编造证据（`TaskExecutionState.ContextSummary` 的空摘要分支）。
3. **后台会话的 checkpoint 记名**：`RememberCheckpointLocked` 仍按活跃会话登记 checkpoint
   （既有实现），冷加载压缩的主战场是当前视图会话，未一并改动。
