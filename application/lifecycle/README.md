# application/lifecycle

## 生态位

`application/lifecycle` 承载 graceful drain、取消、幂等 Shutdown 和 fatal lifecycle
fault guard。它不拥有 SessionUnit、Engine、Runtime 或 EventHub。

主要调用方：`application/assembly` 的兼容 Service 与 wiring/headless 关闭路径。

## 职责

- 定义关闭和取消的应用编排顺序。
- 保证 Shutdown 与失败降级只执行一次。
- 通过窄 deps 调用宿主状态和清理动作。

## 非职责

- 不实现 SessionUnit cancel、目录 worker、Session 域关闭或 EventHub。
- 不导入 `application/assembly`。
- 不改变 frontend wire。

## 文件结构

- `application.go`：drain、idle、running、cancel 与 Shutdown 顺序。
- `fault_guard.go`：降级 once、清理顺序和 panic 重抛。
- `application_test.go`：idle、Shutdown 幂等和 fault fail-fast。

## 核心实现

`Application` 与 `FaultGuardApplication` 只保存 deps 和 fault once 状态。所有可变态
由调用方注入。

## 数据流

Service 兼容入口 → lifecycle Application → 宿主锁/取消/清理函数 → 目录、会话和
运行事件。

## 依赖方向

仅依赖标准库。禁止依赖 `application/assembly`、backend 或 middle 具体实现。

## 并发与错误

- WaitForIdle 不取消工作，只等待 idle channel 或 ctx。
- Shutdown 由 BeginShutdown 返回 false 时直接结束。
- FaultGuard 必须在清理后重新 panic，禁止静默继续。

## 扩展方式

新增清理步骤只能在 `Deps` 和固定顺序中增加，并同步 Shutdown parity 测试。

## Review 指南

- 检查 Shutdown 与 fault cleanup 是否出现第二套状态。
- 检查 cancel 是否在锁外执行。
- 检查 panic 是否仍原样重抛。

## 测试与验证

```text
go test ./application/lifecycle/... -count=1
go test ./application/... -count=1
go test -race ./application/... -count=1
```
