# application/compat

## 生态位

迁移期前端兼容适配器。GUI、TUI、headless 和 wiring 暂时继续消费旧前端方法名，
但这些方法不再堆在 `application/core`。真正状态、锁和生命周期仍由 core 持有。

## 职责与非职责

职责：

- 用旧方法名直接转发到已提取的 Application 领域组件；
- 让消费方在迁移到窄能力接口前保持编译和行为兼容。

非职责：

- 不持有会话、队列、聊天回合或事件状态；
- 不获取 `ViewMu` 或实现业务判据；
- 不反向调用 GUI、TUI 或 wiring。

## 文件结构

- `frontend.go`：兼容适配器与当前已迁出的方法。

## 核心实现

`Frontend` 暂时内嵌 `*core.Frontend` 以承接尚未退场的 core 方法，同时持有一份
`core.FrontendPorts`。当前 127 个已迁移方法只调用领域组件，不在本包保存第二份状态。

## 数据流

调用方 → `compat.Frontend`（旧名）→ `application/admin|policy|workspace|session`
或尚未迁移的 core 状态方法。

## 依赖方向

允许依赖 `application/core` 与已提取的 Application 领域包。禁止被这些领域包反向
依赖；core 不依赖本包。

## 并发语义

本包没有锁，也不得新增锁。并发语义完全由被调用的 core 状态所有者或领域组件决定。

## 扩展与退场

新增兼容方法前必须先确认真实调用方；每次迁移完成后从 `Service` 删除同名入口，
再在这里增加一行转发。调用方改用窄接口后，本包方法应删除，最终整个包退场。

## Review 指南

- 方法体只能是一行转发；
- 不出现 `ViewMu.Lock`、状态字段或事件发布；
- 不在本包解释业务规则；
- `Service` 与 `Frontend` 不得同时保留同一兼容实现。

## 测试与验证

```text
go test ./application/compat -count=1
go test ./application/core ./gui ./tui ./wiring/... -count=1
```
