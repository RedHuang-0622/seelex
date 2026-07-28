# Session Manager

## 模块定位

`session` 是 Application 与会话存储之间的用例适配层。它统一保存/恢复 callback、active workspace routing、显式 project-scoped read 和存储设置，不实现具体 JSON/SQL 格式。

## 核心实现

- `Store`：兼容旧 Seele store 的最小 List/Delete/Load/Range/Count 接口。
- `Manager`：持有 legacy store、可选 `NestedSessionStore` 和生产 `sessionstore.Router`。
- `InjectSaveLoad`：连接 Engine 当前会话的 Save/Resume callback。
- `SetWorkspace`/`Workspace`：只影响后续默认读写 scope。
- `ListByWorkspace`/`LoadHistoryByWorkspace`/`DeleteByWorkspace`：不改变 active scope 的显式读取。
- `StorageConfig`/`TestStorage`/`ConfigureStorage`：委托 Router 原子切换 backend。

## 生态位

Application 关心“保存/恢复哪一个 session”，sessionstore 关心“如何原子存储”；Manager 隔离两者并保留旧调用兼容。

## 并发与 Review

- save/load callbacks 受 mutex 保护，但不能在回调内反向调用持同一锁的方法。
- 跨项目读取必须优先显式 API，不能临时切 active workspace 后忘记恢复。
- Router 可用时它是生产事实源；legacy nested store 仅兼容旧数据路径。
- 配置切换错误必须保留旧 repository 可用。

## 测试

```text
go test ./session -count=1
go test ./application/core -run 'Session|History|Workspace' -count=1
```
