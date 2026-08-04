# 工具完成投影死锁复盘

日期：2026-08-04

## 现象

Windows GUI 在工具调用后停在 `Waiting for output…`；关闭窗口时 chat 仍处于运行态，表现为未响应。`bash.exe` 本身已经在约 90ms 内退出，因此工具沙盒和 shell 进程不是阻塞点。

## 根因

`ToolHookBridge -> Service.handleToolCompleteObserved` 持有 `service.mu`，并在更新可见快照时调用：

```text
refreshRuntimeLocked -> Runtime.VisibleTools
  -> seelexVisibilityPolicy -> goalSkillActiveFn
  -> Service.ActiveSkillIDs -> service.mu.RLock
```

同一 goroutine 已持有 `service.mu` 的写锁，不能再取得其读锁，形成确定性的 `sync.RWMutex` 自锁。它只在真实 Runtime 注入 `SetGoalSkillProvider(func() { app.ActiveSkillIDs() })` 后出现，因此 fake Runtime 单元测试未覆盖到该依赖环。

## 修复

- `taskRuntimeState.goalSkillActive` 作为 task projection 的原子布尔快照。
- task skills 激活、会话恢复、切换/新建会话清理时，在已持锁状态下同步该快照。
- `Runtime.SetGoalSkillProvider(app.GoalSkillActive)` 仅执行无锁原子读取，不再从 Runtime 层反向取得 application 锁。

`ActiveSkillIDs` 仍保留给锁外的完整列表读取；它不再处于 Runtime 的热路径。

## 验证

- `TestToolCompletionDoesNotReenterServiceLockForGoalSkillVisibility`：覆盖完整工具完成投影中的可见工具刷新，1 秒内必须完成。
- 真实账号 backend smoke：`bash(pwd)` 进程耗时 66ms，随后连续出现 `toolhook.complete.runtime.done`、`tool.completed` 与 `chat.idle`；此前该阶段会等待到 45 秒上下文超时。
- `go test -p 1 ./... -count=1`、`go vet -p 1 ./...`、`go build -p 1 ./...`、`go build -p 1 -tags "gui,desktop,production" ./...` 和 `node --test gui/frontend/dist/*.test.mjs` 全部通过。

本机缺少 GCC，且以 `CGO_ENABLED=0` 运行，因此未宣称执行 Go `-race`；CI 的 Linux race 门禁仍应保留。
