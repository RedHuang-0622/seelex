# Application

## 模块定位

`application` 是 Seelex 的稳定应用层门面。根目录 composition root、TUI、GUI 和测试 harness 都依赖这里，而不直接驱动 Seele Engine、Plugin Manager 或存储实现。

它的生态位是“用例编排 + 权威状态”：把底层能力组合成聊天、命令、会话、项目、审批、Plan 和运行时切换，并通过 Snapshot/Event 向多个前端提供同一份事实。

## 子模块

| 目录 | 职责 |
|---|---|
| [`model/`](model/README.md) | Snapshot、Message、Plan、Interaction 等版本化 DTO。 |
| [`event/`](event/README.md) | 有序事件封装、订阅和 fan-out。 |
| [`approval/`](approval/README.md) | 异步审批请求、决议、超时和关闭。 |
| [`contract/`](contract/README.md) | Application 拥有的 Engine、Runtime、Plugin、Session、Workspace 端口。 |
| [`prompt/`](prompt/README.md) | PromptStack 与 Effort 策略。 |
| [`search/`](search/README.md) | Tavily Web Search 能力。 |
| [`core/`](core/README.md) | Service 用例、聊天状态机、命令、session/project 作用域和工具事件。 |

`application.go` 通过类型别名和薄转发保持外部 API 稳定，调用方不需要依赖内部子包。

## 依赖方向

```text
TUI / GUI / root adapters
          |
          v
    application facade
          |
          v
 application/core --> contract + model + event + approval + prompt
          ^
          |
  root adapters implement ports
```

接口定义在消费方 `contract/`，实现放在根目录适配器或基础设施模块。禁止 `application` 反向依赖 `gui`、`tui` 或 composition root。

## 核心运行流

1. `application.New` 接收 `Dependencies`，创建 `core.Service`。
2. 前端调用 `Submit`、`ResolveInteraction`、`BindWorkspace` 等用例。
3. Service 更新权威 Snapshot，并通过 EventHub 发布增量事件。
4. TUI/GUI 先读取 Snapshot，再应用连续 Event；出现序列缺口时重新同步 Snapshot。
5. 外部副作用只通过 Ports 进入 Engine、Runtime、Session Store 和 Workspace Repo。

## Review 指南

- 新业务状态应进入 `model.Snapshot` 或明确的内部状态，而不是只存在于某个前端。
- DTO 变更必须检查 GUI reducer、TUI rendering、clone helper 和协议测试。
- 不要把 Seele 深层类型泄漏给前端；在 adapter 边界转换。
- Service 的锁不能包住网络、LLM、数据库或长时间工具调用。
- session/project 操作必须以 ID 为键，显示名称允许重复。

## 测试

```text
go test ./application/... -count=1
go test ./application/... -race -count=1   # 需要 CGO/C toolchain
```

集成入口主要位于 `core/service_test.go`、`core/command_test.go` 和根目录 `application_adapters_test.go`。
