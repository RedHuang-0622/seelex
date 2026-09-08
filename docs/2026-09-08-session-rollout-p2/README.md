# Session Rollout 全序模型 —— P2 恢复改造（第一批垂直切片）

> 状态：JSON 后端垂直切片已交付（2026-09-08）。目标模型与实施路线见
> [session-order-log](../2026-09-07-session-order-log/README.md)；
> 本文件记录 rollout 全序模型 P2 恢复改造的第一批落地与剩余增量。

## 1. 目标模型（回顾）

每会话一条物理 append-only `rollout.jsonl` 作为“发生顺序”的唯一事实源：

```text
rollout.jsonl（Ordinal 全局单调；类别混排）
  session_meta / user_input / internal_user / assistant / reasoning
  / tool_call / tool_output / request_begin / request_end / turn_begin
  / turn_end / world_state / context_change / compacted / rolled_back
  / token_usage / subagent
      ▼ 派生投影
state.json / history shards / transcript / UI conversation
恢复 = 最新压缩检查点之后正序重放
```

## 2. 本批已落地（JSON 后端垂直切片）

1. **Rollout 契约**（[sessionstore/rollout.go](../../sessionstore/rollout.go)）：
   - `SessionLogEntry{Ordinal, Seq, Kind, RequestID/TurnID/MessageID, CreatedAt, TokenCount, Payload}`；
   - 全量 kind 常量（含 request/lifecycle/compacted 等预留）；
   - 对话类 Event → Rollout kind 映射（`EventKindOf` 同源回退）；
   - JSONL 读/写：`readRolloutLocked`（缺失=空、崩溃残尾跳过、head 续算）、
     `appendRolloutLocked`（写前截断残尾再追加，Ordinal 从 head+1 续写）。
2. **提交双写**（[sessionstore.go](../../sessionstore/sessionstore.go)）：
   `WriteCommit` 在 manifest 切换前，把本次提交的对话类增量事件与首条
   `session_meta` 追加进 `rollout.jsonl`；重复提交幂等（按 Seq 增量），
   失败不发布 manifest。旧通道（transcript.log / history / state）继续双写，
   作为兼容投影与旧会话兜底。
3. **契约测试**（[rollout_test.go](../../sessionstore/rollout_test.go)）：
   - `TestJSONRolloutAppendOnlyOrdinals`：meta ordinal=1、对话事件 ordinal
     连续、幂等重提不重复；
   - `TestJSONRolloutCrashTailResumes`：崩溃残尾跳过且下次提交续写无空洞。
4. **生命周期 kind 单写**（同文件 `commitRolloutLocked`）：按提交顺序生成
   `session_meta → request_begin/turn_begin → 对话事件 → request_end/turn_end
   → compacted → token_usage`，全部按指纹幂等（重复持久化不重写）；
   `rolled_back` 常量预留（当前无回滚事件源）。
5. **resume 重放切换**：`resumeSessionCold` 读 transcript 时优先从 rollout
   正序重放对话事件（[session_history.go](../../application/core/session_history.go)、
   [archive.go](../../application/core/session_runtime/archive.go)），rollout
   缺失/损坏/落后于旧通道时回退旧三读；可见会话与 provider 顺序由重放结果
   驱动。

验证：`go test ./sessionstore ./application/core -count=1` 全绿；
`go test . -run TestHeadlessRestorePrefixProbe` 绿（双进程恢复 + rollout
存在性与 lifecycle kind 断言，重启后首请求 = 未重启第 13 轮）。

## 3. 剩余增量（P2 完成定义）

1. ✅ 恢复重放切换：rollout 正序重放优先，旧三读兜底（本批完成）。
2. ✅ 生命周期 kind：request/turn/token_usage/compacted 由提交路径单写、
   指纹幂等（本批完成）；`rolled_back` 待回滚事件源落地后补。
3. ✅ 发送治理（P3，契约版）：重放基线 + 追加已由回归钉住——
   - I-LOG-3：`TestJSONRolloutAppendOnlyNoRewrite`（已提交前缀逐字节不变，
     只追加；崩溃残尾截断是唯一例外）；
   - I-LOG-4：`TestHeadlessRestorePrefixProbe`（重启后首请求与未重启第 13
     轮逐条一致，即 rollout 重放基线 + 追加 = 运行期装配结果）；
   - I-LOG-5：`TestJSONRolloutLifecycleKinds` 第二段（compacted 之后日志
     继续追加并可重放续播）。
   说明：运行期“每次请求都从 rollout 重新装配”尚未做（保持内存 retained
   基线 + 追加的高效路径）；跨重启一致性以本批探针为准。
4. ⏳ 其它后端：SQLite/PostgreSQL/Redis 的 rollout 通道 + 全后端契约测试。
5. ⏳ 存量迁移：旧会话无 rollout——按“下一次提交起双写 + legacy_import
   基线”策略，不静默改写 `.seelex`/dist 数据（先备份+预警+确认）。

## 4. 涉及代码入口

- `sessionstore/sessionstore.go`：WriteCommit / Repository；
- `application/core/session_history.go`：resumeSessionCold；
- `application/core/session_runtime/archive.go`：PersistCurrentSession /
  LoadSessionTranscript；
- `application/core/task_context/task_context_state.go`：transcript 追加点
  （生命周期 kind 单写者入口候选）；
- `application/core/context_runtime/coordinator.go`：压缩点写 compacted。
