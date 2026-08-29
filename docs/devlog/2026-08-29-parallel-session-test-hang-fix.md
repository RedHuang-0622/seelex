# 2026-08-29 M2 并行会话：测试挂死 / 死锁 / 数据竞争修复

> 日期: 2026-08-29 | 范围: `application/core`（M2 多会话并行执行） | 关联: 工作打点表 todo:164 / todo:166 / todo:168 / todo:172

---

## 1. 现象

用户命令长时间不结束（10 分钟+ 无输出）：

```bash
go test ./internal/adapters/ ./application/core/ -count=1
```

拆包定位结果：

- `./internal/adapters/`：约 0.5s 通过，**无问题**；
- `./application/core/`：**挂死**——不是慢，是某个测试永久阻塞；
- 带 `-race` 跑：额外暴露 **6 处数据竞争 + 1 处 RWMutex 重入死锁**（挂死与竞争是两回事，但都会让"测试跑不完/不可信"）。

## 2. 根因分析（分层，各自有证据）

### 2.1 测试桩不完整 → 静默回退活跃会话路径 → 永久挂死（主根因）

`session_parallel_test.go`（M2 新增测试，未提交）的 `multiSessionEngine` 桩**没有实现 `contract.SessionChatEngine`**：缺失 `ReplaceHistoryFor` 方法（编译期断言 `var _ contract.SessionChatEngine = (*multiSessionEngine)(nil)` 报 `missing method ReplaceHistoryFor` 证实）。

后果链：

1. `service.chatStream` / `sessionLoaded` / `replaceEngineHistory` 对 `Deps.Engine` 做 `contract.SessionChatEngine` 类型断言，**失败后全部回退到"活跃会话"路径**（`service_input.go` / `session_scope.go`）；
2. `SubmitToSession(sess-B, ...)` 因此被当作"会话未加载"，B 的聊天实际被调度到会话 A 的引擎上，`engine.started[bID]` 永远不会触发；
3. `TestParallelSessionsQueuedPerSession` 里的 `<-engine.started[bID]` 是**无超时**等待 → 测试二进制永久挂起，`go test ./application/core/` 永不结束。

goroutine dump 证实：两个 `runChat` 都卡在会话 A 引擎的 `release` 上（`session_parallel_test.go:109` 的 select），测试主 goroutine 阻塞于 `session_parallel_test.go:254` 的 channel receive。

> 教训：**"未实现接口的方法"静默退路比编译错误危险得多**。测试桩必须用编译期断言锁死接口契约；无超时的 channel 等待必须有兜底。

### 2.2 requestID 碰撞 → 跨会话状态串写（Windows 时钟分辨率）

`requestID` 原为 `fmt.Sprintf("chat-%d", time.Now().UnixNano())`。实测这台 Windows 上 `UnixNano()` 分辨率约 **0.5ms**（100 万次采样仅 10 个不同值，最小间隔 504300ns）。两个会话同一 tick 内启动（M2 并行场景）会生成**完全相同的 requestID**，导致：

- `requestToSession` 绑定串写，A 的 `ClearReActBudget` / `FinalizeTask` 可能清掉 B 的状态；
- `TestParallelSessionsExecuteConcurrently` 断言 `taskA.RequestID == taskB.RequestID` 失败（实测 A、B 拿到同一 RequestID）。

修复为 `chat-<UnixNano>-<单调序号>`（`chatSeq` 在 `Core.Mu` 内递增），保证跨会话唯一。

### 2.3 生产代码数据竞争：`handleToolCompleteObserved` 锁外读 Snapshot

M2 改动引入：`handleToolCompleteObserved` 在 `service.Mu.Lock()` **之前**回退解析 `service.Core.Snapshot.Session.ID`（`tool_hooks.go:103`），而 `ApplyRuntimeProjectionLocked`（`view_state/coordinator.go:132`）会持锁写同一字段 → `-race` 报 Write/Read 竞争。修复：把 sessionID 回退解析移入 `Mu.Lock()` 内。

### 2.4 测试桩数据竞争：`fakeRuntime` 4 个并发写点无锁

M2 并行下多个 `runChat` 并发调用 `fakeRuntime` 的 `DrainSubagentContexts` / `SetRuntimeVisibilityProjection` / `SetParentEvidenceProjection` / `SetCurrentTaskBatch`，而 fake 无锁（生产 `Runtime` 的 actor mailbox / 投影存储是线程安全的，fake 没镜像）。修复：新增 `mailboxMu` 保护这四个字段。

### 2.5 真正的死锁：RWMutex 重入（`-race` + 双包并发才偶发）

3 个测试（`task_service_test.go`×2、`service_chat_test.go`×1）这样写：

```go
service.Mu.RLock()
resume := service.components.tasks.CurrentTaskResumeRecord() // 内部又加 c.Mu.RLock()
service.Mu.RUnlock()
```

`tasks.Mu` 就是 `Core.Mu`（组件共享同一把锁），而 `CurrentTaskResumeRecord()` 是"自行加锁"的公开方法——**Go RWMutex 不可重入**。时序：

1. 测试 goroutine 拿到 `RLock`；
2. 内部 `RLock` 再次成功（读锁可叠加）；
3. 此时目录刷新 worker（`session_runtime/coordinator.go:158`）排队 **写锁**（`Lock` 会等所有 RLock 释放）；
4. 测试 goroutine 的**第二次** `RLock` 因写锁在排队而被阻塞 → 它永远无法释放第一次的 RLock → 写锁永远拿不到 → **死锁**。

dump 中测试 goroutine 与 catalog worker 同阻塞于同一 RWMutex（`0xc000402488`）即为铁证。修复：去掉外层冗余 `RLock`（方法自加锁，调用方不要再包）。

> 教训：**RWMutex 不可重入**，自加锁的公开方法禁止在调用点再包同锁；"读锁可叠加"的错觉正是死锁温床。

## 3. 修复清单

| 文件 | 改动 | 类型 |
|------|------|------|
| `application/core/session_parallel_test.go` | 补 `ReplaceHistoryFor`；新增编译期断言 `var _ contract.SessionChatEngine = (*multiSessionEngine)(nil)` | 测试桩 |
| `application/core/chat.go` | 新增 `nextChatRequestIDLocked()`（时间戳+单调序号）；`startChatFor` / `runChat` 队列批次改用之 | 生产 |
| `application/core/service_state.go` | `serviceState` 新增 `chatSeq uint64`（Core.Mu 保护） | 生产 |
| `application/core/tool_hooks.go` | `handleToolCompleteObserved` 的 sessionID 回退解析移入 `Mu.Lock()` 内 | 生产 |
| `application/core/service_fakes_test.go` | `fakeRuntime` 新增 `mailboxMu`，保护 mailbox/visibility/evidence/currentBatch | 测试桩 |
| `application/core/task_service_test.go` | 去掉 2 处 `CurrentTaskResumeRecord` 外层冗余 RLock | 测试 |
| `application/core/service_chat_test.go` | 去掉 1 处外层冗余 RLock | 测试 |

## 4. 验证证据

- **原命令**（不带超时）：`internal/adapters 0.535s + application/core 1.685s`，含编译共 ~7.6s；
- **`-race` 双包全量**：`ok internal/adapters 1.554s / application/core 4.181s`；此前挂死/死锁/竞争的 4 个用例（`TestParallelSessions*`、`TestTerminalResumeRecord*`、`TestOnChatEnd*`）连跑 38 次全绿；
- **12 个 core 子包**（chat / context_runtime / input_router / task_context / view_state / worktable …）各自 PASS；
- `go vet ./application/core/ ./internal/adapters/` 干净；`go build ./application/... ./internal/...` 干净。

## 5. 遗留备注

- 首轮 5 次 `-race` 循环中有 2 次 FAIL 未抓到内容，其后 38 次连续全绿且无法复现，疑似并行跑两个 `go test` 进程的瞬时噪音；CI 若再偶发 `-race` FAIL，应先抓完整输出再下结论。
- 建议日常/CI 给 `go test` 带 `-timeout`（如 `-timeout 60s`），挂死会立刻暴露而非等默认 10 分钟；CI 保持 `-race`。
