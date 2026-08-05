# 会话恢复与内存峰值前置审查

## 需求摘要

修复 GUI 点击已有会话后下一次请求无法获得恢复上下文的问题，并限制长历史会话恢复、会话目录刷新和前端快照链路的内存增长。

## 影响文件清单

| 文件 | 修改类型 | 位置 | 原因 |
|---|---|---|---|
| `sessionstore/durable_history.go` | 修改 | `DurableHistory.Load` | 框架恢复前先消费应用刚装配的有界 provider history，避免覆盖 checkpoint/system prompt。 |
| `seelebridge/runtime.go` | 修改 | 主会话 history owner 装配 | 暴露受控的“下一次 Load 使用已装配历史”边界，不让 Runtime 反查 Application。 |
| `application_adapters.go` | 修改 | `enginePort` | 在新 Reactor 安装后把有界历史交给 DurableHistory 的一次性恢复边界。 |
| `main.go` | 修改 | 启动装配 | 注入 Runtime-owned history preparer。 |
| `application/core/session_scope.go` | 修改 | `sessionName` | 会话标题只读取记录或有界尾窗，不再为目录刷新全量读取历史。 |
| `sessionstore/sessionstore.go` | 修改 | JSON legacy `ReadRange` | 为旧 manifest 推导并缓存 shard 计数，使 total 查询和尾窗读取不重复解析完整历史。 |
| `*_test.go` | 新增/修改 | 对应模块 | 覆盖恢复上下文不被覆盖、legacy 尾窗有界和重复 Resume 内存行为。 |

## 依赖分析

- `Service` 负责恢复 `SessionRecord`、Transcript 和 provider history，并在锁外调用 Engine。
- `enginePort` 负责把逻辑 session ID 映射到新的框架 Reactor。
- `Runtime` 只持有自己的 DurableHistory 投影；不会通过 callback 反查 Application。
- GUI 的 snapshot reducer 已采用替换/有界 reconcile，本次重点修后端输入体积与恢复边界。

## 风险评估

- 一次性 prepared history 必须只消费一次，避免跨请求复用旧快照。
- legacy shard 计数缓存必须按 generation 隔离，并限制缓存规模，避免形成新的无界内存。
- 用户已有 `docs/2026-08-04-liveness-remediation/` 未提交修改必须保留且不纳入本次提交。

## 验收标准

- 点击恢复后下一次 ChatStream 看到应用装配的 system/checkpoint/历史尾窗。
- 重复 Resume 同一大历史会话时，catalog 与尾窗读取不再每次全量解析。
- 定向测试、`go test ./...` 和可用环境下的 race 测试通过。

## Follow-up liveness fixes

- `main.go` now wires a lazy `enginePort` without calling `initEngine` during
  startup. The initial application snapshot has no Session ID; first submit
  materializes via `StartSession`, while resume materializes only the requested
  session through `ReplaceHistory`.
- `application/core/chat.go` acknowledges queued inputs immediately after a
  framework-backed `ChatStream` returns. Persistence and the next turn remain
  outside the application lock, so the UI no longer shows a stale queue while
  the previous turn is being persisted. The acknowledged inputs remain in an
  internal deferred queue until that persistence boundary has completed.
- `gui/frontend/dist/app.js` asks the backend to cancel the current active
  request instead of trusting a possibly stale renderer request ID.
- `gui/bridge.go` retries cancellation with an empty request ID when a stale
  renderer ID is rejected. This keeps the stop button effective across queued
  turn promotion while remaining scoped to the current application session.
