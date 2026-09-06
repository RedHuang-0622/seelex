# Finish Review：运行中会话切换的上下文/作用域隔离

日期：2026-09-06
审查对象：[code-changes.md](code-changes.md) 所列改动
审查方式：五轴快速复核（正确性 / 安全性 / 性能 / 可维护性 / 一致性）

## 正确性

- B：`bindProjectRootIfSafe` 新不变量「任意会话运行中一律不重绑」与注释一致；
  单元回归覆盖“目标==当前视图 + 该会话仍在运行”这一旧规则放行场景
  （RED→GREEN 已验证）。空闲收尾路径 `rebindViewWorkspaceWhenIdle()` 不持
  ViewMu 调用（先 RLock 取视图 ID/空闲判定，再走既有加锁绑定流程），无锁序问题。
- C：`workTableTraceBlockFor` 以 `viewID` 判定活跃会话、其余走
  `TaskSnapshotFor(sessionID)` 分区；空会话 ID 视为活跃，与 Runtime
  `TaskSnapshotFor("")` 语义一致。S0 既有分区回归 + 新增 S1 均绿。
- 请求尾部打点语义未变（无活动任务返回空、任务终态即删、围栏标记、行上限、
  retry 计数）——仅在“取哪份注册表”上按会话分流。

## 安全性

- 未新增全局可变状态；打点只读注册表快照（Runtime 侧返回拷贝）。
- 锁序：`ViewMu.RLock → TaskSnapshot/TaskSnapshotFor`（Runtime 内部自有锁），
  与既有「ViewMu → 域锁」方向一致；不反向持锁调用。
- 打点内容仍为白名单化任务摘要（truncateWorkEvidence），不引入新的敏感面。

## 性能

- 后台会话注入改为分区快照拷贝，量级与实时注册表同阶；无额外通道/goroutine。
- `-race` 全绿，未引入新的锁竞争热点。

## 可维护性

- 端口签名 `WorkTableTraceBlock func(sessionID string) string` 使“打点按会话
  取数”成为显式契约，装配点集中在 service_assembler 一处。
- 新增测试以 S0/S1 靶场风格命名并注释场景，README 分卷由生成器刷新。

## 一致性

- 与当日「热会话切换」系列修复同一批目标：事件/提交按会话显式路由 →
  项目根绑定与打点注入同样按会话收敛，消除进程级单例泄漏。
- 与 `TaskSnapshotFor` 既有分区语义一致；空会话/活跃会话回退路径与旧行为
  兼容（既有 trace-block 用例不改语义即绿）。

## 验证

```text
go test ./application/core -count=1                                     ok
go test ./application/core -run 'S0|S1|Pollute|WorkTableTraceBlock'      ok
go test -race ./application/core ./seelebridge -count=1                 ok
go test ./seelebridge ./sessionstore ./session ./application/...        ok
go test . -run TestThreeRunningSessionsSwitchDoesNotWaitAndKeepsContext  ok
go build ./... ; gofmt -l 改动文件                                       干净
```

结论：可合并。
