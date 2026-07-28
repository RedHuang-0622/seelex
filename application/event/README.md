# Application Events

## 定位

本包把 Application 权威状态变化发布给 TUI、GUI Bridge 和测试观察者。Snapshot 是完整事实，Event 是连续增量；Event 不能成为唯一持久状态。

## 核心实现

- `EventKind`：消息新增/增量、工具、Runtime、Interaction、Snapshot 和 Error 等事件类型。
- `Event`：包含 `ProtocolVersion`、全局 `Seq`、Snapshot `Revision`、request ID 和 JSON payload。
- `EventHub`：为每个 subscriber 分配 channel，负责 fan-out 和关闭。
- `Subscription`：暴露只读事件 channel 与幂等 `Close`。

发布时 Hub 递增 seq；慢订阅者不得阻塞业务线程，因此 buffer 大小和丢失后的 resync 语义由消费者共同承担。

## 依赖和边界

本包只依赖 `application/model`，不读取 Service 内部状态。GUI 的 reducer 在 seq gap 时重新请求 Snapshot，详见 [`docs/gui`](../../docs/gui/README.md)。

## Review 指南

- 新事件必须能由 Snapshot 重建，不能制造只存在于事件流的事实。
- 修改 seq/revision 含义时同步更新 `gui/frontend/dist/client-state.js` 及协议测试。
- Publish/Close/Subscribe 的竞态必须用 race test 验证。
- payload 应是稳定 DTO，避免传递可被后续修改的共享 slice/map。

## 测试

```text
go test ./application/core -run EventHub -count=1
node --test gui/frontend/dist/client-state.test.mjs
```
