# Codex 会话恢复与 rollout 有序存储调研

> 日期：2026-09-07
> 结论先行：Codex 以「每会话一个物理 append-only 的 rollout JSONL」为唯一权威事实源，事件**按发生顺序**跨类别混排（session_meta / turn_context / event_msg / response_item / compacted / token usage …），带全局单调 `ordinal`；恢复时对该有序流做「最新压缩检查点之后的前缀重放」，从而同时还原内容、调用顺序与 LLM 请求顺序；DB（SQLite）只是 rollout 的增量投影，不是事实源。

## 来源与口径

- OpenAI 官方文档站点（developers.openai.com）本次访问返回 403，无法作为引用来源，已在下方注明。
- 依据：开源仓库 `openai/codex` main 分支源码（codex-rs），下载自 GitHub 归档；只读查看了本机 `~/.codex` 的 rollout 目录与文件布局，**未读取或转发任何会话正文内容**。
- 关键代码位置：`codex-rs/rollout`（writer/ordinal/读取）、`codex-rs/history`（RolloutItem/RolloutLine/ResumedHistory）、`codex-rs/core/src/session/*`（turn/record/rollout_reconstruction）、`codex-rs/thread-store/src/local/*`（DB 投影）。

## 1. 存储：单会话单文件、append-only、全类别同序

### 1.1 布局

```text
~/.codex/sessions/YYYY/MM/DD/rollout-<timestamp>-<conversationID>.jsonl
```

- 一个会话一个 rollout JSONL；新会话、fork、revert 都会生成新的不可变 rollout 文件（revert 通过追加后缀的 rollout id 保持 thread ID 不变）。
- JSONL 单行记录形如 `{"timestamp","ordinal","type","payload..."}`；`ordinal` 是**每个 rollout 文件内全局单调递增**的序号（抽查过的文件连续无空洞；旧版本存在无 ordinal 的 legacy 模式，此时以文件行序为准）。
- `type/payload` 在行内扁平化展开，类型包括：
  - `session_meta`：会话首行，记录 session ID、cwd、CLI 版本、model_provider、history_mode、context_window、base_instructions、fork 血缘等；
  - `turn_context`：每轮开头的 model/turn_id/cwd/workspace/sandbox/approval/timezone/current_date 等上下文快照；
  - `world_state`：全量快照或 merge patch；
  - `event_msg`：运行时生命周期事件（task_started / item_completed / token_count / turn_started / turn_complete / rollback 等）；
  - `response_item`：模型侧可见单元（developer / user / reasoning / assistant / function_call / function_call_output …）；
  - `compacted`：压缩检查点（摘要 message + replacement_history + window 血缘 + retained/guardian 上下文 + token usage 快照）；
  - 其他如 token_usage_record、inter-agent communication、security risk score、realtime item。
- **所有类别混排在同一个有序流里**：LLM response 与其触发的 tool call/output、usage、事件互相交错，行序即真实发生序。

### 1.2 DB 只是投影

`thread_history_1.sqlite` 里：

- `thread_history_projection_state(thread_id, next_rollout_byte_offset, next_rollout_ordinal)`：记录 rollout 已被增量投影到的字节/序号水位；
- `thread_items / thread_turns`：把 rollout 行投影成 UI/交互友好的 items/turns 表。

投影事务与 rollout 字节游标一起提交；SQLite 失败则停留在旧水位，绝不自称投影过未读数据。thread 的 header/meta 另有 state DB。事实源始终是 rollout JSONL。

## 2. 写入路径：单 writer 保序、逐行 flush、可恢复追加

`codex-rs/rollout/src/recorder.rs`：

- `RolloutRecorder` 内部是**一个后台 writer task + 有界 channel**，命令为 AddItems / Persist / Flush / Shutdown；
- 所有条目先入 pending 队列，writer 串行写 JSONL，写入成功才推进 ordinal 并从 pending 移除；失败时丢句柄保留未写后缀，下次 barrier 重开文件重试；
- 新建会话首条写 `session_meta`；Resume 时以 append 模式打开既有文件，从文件尾扫描最后一条合法记录推得下一个 ordinal；
- 每次 `write_rollout_item` 都 `flush()`；崩溃后可继续追加（并自动补尾换行）。

运行期接入点（`core/src/session/mod.rs`、`session/turn.rs`）：

- `record_conversation_items`：先写入内存有序历史 `state.history`，再以同一批 `RolloutItem::ResponseItem` 调 `persist_rollout_items` → rollout recorder；内存与落盘顺序完全一致；
- 用户输入先 append 成 user message 再采样；采样请求 input = `clone_history()`（整个有序历史前缀）；工具输出执行完后 append，下一轮采样继续携带完整前缀；如此循环直至本轮结束；
- 每轮还落 turn_started/turn_complete/token usage/context update 等 event_msg / turn_context 记录。

## 3. 请求发送治理：完整前缀 + 追加，绝不重写旧记录

- 每次采样请求发送的 input 就是**当前完整有序历史**（含上一请求的 assistant item、tool call、tool output 与最新 user 消息），不存在按类别拼装/重建顺序的路径；
- 旧记录永不变更；要“忘掉”旧内容只有两种有序流内标记：
  - `ThreadRolledBack` 事件：恢复/投影时丢掉最后 N 个用户轮次（数据仍留在文件里）；
  - `Compacted` 条目：带 `replacement_history`（新基线的完整有序前缀），新请求从该替换历史之后的重放后缀继续，天然保证「前缀失效只发生一次」。

## 4. 恢复：逆序找检查点 + 正序重放后缀

`core/src/session/rollout_reconstruction.rs`：

1. 加载 rollout 条目（或从 DB 投影增量读）；
2. **逆序扫描**：找到最新且存活的 `Compacted`（带 replacement_history）作为基线，同时恢复 previous model settings、reference context item、world state baseline、window 血缘；
3. 对「基线之后的后缀」**正序重放**：response_item 按序写回内存历史；rollback 事件按“丢最近 N 个用户轮”截断；world_state 按 full/patch 语义恢复；retained/guardian 上下文恢复；
4. 用恢复出的历史替换内存 ContextManager，rollout 文件本身不修改；后续新事件从下一个 ordinal 继续 append。

因此 Codex 的恢复结果是：**可见内容、模型历史顺序、LLM 调用/工具调用次序、压缩窗口边界**全部一致，而不是“可见对话 + 尾部窗口”的近似。

## 5. 对 Seelex 的借鉴点（速览）

| Codex 机制 | Seelex 现状 | 借鉴点 |
|---|---|---|
| 会话权威 = 一条物理 append-only 有序流 | 按类别存 ProviderHistory / Events / State / ToolResults / ContextState，manifest 原子切 generation | 存储层增加“单会话全序日志”，类别成为派生投影 |
| 全局单调 ordinal + 行序 | TranscriptEvent 已有每会话 Seq，但未与 lifecycle/usage/请求边界统一穿起 | 统一 envelope 与单一写者 |
| 单 writer + append + barrier flush | JSON/SQL 每次整代重写合并 | 落盘改物理追加，崩溃可续 |
| 请求 = 完整有序前缀 + 追加 | 运行时已实现 prefix chain，但冷恢复只取尾部窗口 | 恢复与发送共用“重放基线 + 追加”语义 |
| Compacted / rollback 作为流内标记 | 压缩仅存 context store 栈帧/审计，无持久化重放基线 | 把压缩帧与回滚标记进有序流 |

## 6. 局限

- 官方文档不可达（403），本文结论基于开源 main 分支与本地安装实证，可能与最新发布版存在偏差；
- rollout 属于实现层事实，官方文档未必描述这些内部细节，实施时应以代码/测试为最终事实源。
