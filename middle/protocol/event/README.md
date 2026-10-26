# Middle Protocol Event

## 生态位

`middle/protocol/event` 是共享事件 Hub 的 Middle 兼容面。唯一实现位于
[`kernel/event`](../../../kernel/event/README.md)；本包只保留同身份 alias、常量与
构造转发，不拥有第二份 Hub、订阅队列或路由判据。

主要调用方：仍依赖 Middle 路径的 Backend、GUI/TUI Bridge 与历史测试。
Application 新代码直接使用 `application/event` → `kernel/event`。

## 职责与非职责

- 职责：保持旧 import path 与类型身份兼容，便于调用方分批迁移。
- 非职责：不实现发布、订阅、重放、白名单或频道；不拥有 DTO。
- 删除条件：全仓不再 import `middle/protocol/event` 后删除本包。

## 依赖方向

`middle/protocol/event → kernel/event → contract/protocol`。禁止 kernel 反向依赖
Middle、Application、Backend 或 Store。

## Review 指南

- 只能出现 alias、常量 re-export 与单行构造转发；
- `Event`、`Subscription`、`EventHub` 必须与 kernel owner 同身份；
- 不允许为了兼容复制 Hub 状态机或 `PublishDiagnostic` 的第二份可变实现。

## 测试与验证

```text
go test ./middle/protocol/event ./kernel/event -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### hub.go

- `func KindIsProcessClass(kind EventKind) bool` — KindIsProcessClass reports whether a kind is process-scoped.
- `func KindIsSessionClass(kind EventKind) bool` — KindIsSessionClass reports whether a kind is session-scoped.
- `func ValidateSessionRouting(kind EventKind, sessionID string) error` — ValidateSessionRouting validates a kind/session routing pair.
- `func NewEventHub() *EventHub` — NewEventHub constructs the shared kernel event hub.

### hub_replay_test.go

- `func TestReplaySubscriptionKeepsOverflowReplayable(t *testing.T)` — TestReplaySubscriptionKeepsOverflowReplayable 覆盖 C4 的核心改变：开启重放
- `func TestReplayWindowEvictionRequiresFullResync(t *testing.T)` — TestReplayWindowEvictionRequiresFullResync 钉住窗口上限：落后超过窗口时
- `func TestReplayWindowRespectsDeliveryFilter(t *testing.T)` — TestReplayWindowRespectsDeliveryFilter 确认补取与投递同一套归属口径：被过滤
- `func TestReplaySubscriptionSurvivesClose(t *testing.T)` — TestReplaySubscriptionSurvivesClose 确认关闭后的订阅不会给出可用窗口：

### hub_test.go

- `func TestSubscribeSessionDropsForeignEventsAtDelivery(t *testing.T)` — TestSubscribeSessionDropsForeignEventsAtDelivery 验证会话级订阅在投递端
- `func TestFilteredSubscriptionDeliverySeqIsContiguous(t *testing.T)` — TestFilteredSubscriptionDeliverySeqIsContiguous 验证过滤后的投递序号连续：
- `func TestOverflowResyncIsDeliveredGlobally(t *testing.T)` — TestOverflowResyncIsDeliveredGlobally 验证缓冲溢出的 resync 一定可达：它必须
- `func TestSubscribeFilteredFollowsLivePredicate(t *testing.T)` — TestSubscribeFilteredFollowsLivePredicate 验证谓词在每次投递时求值：视图

