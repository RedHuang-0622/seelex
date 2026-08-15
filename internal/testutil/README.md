# internal/testutil

## 定位

`internal/testutil` 存放**仅供测试**的共享桩（Go internal 限制，禁止进入
产品代码路径）。目的：同一契约（如 `application/contract.ChatEngine`）的
多份测试假实现不再每份手写全量方法，接口新增方法时只改这里一处。

## 内容

- `chat_engine.go`：`EmbeddedChatEngine`——全方法 panic 的 `ChatEngine`
  底座。测试引擎以 `*EmbeddedChatEngine` 内嵌，只覆写测试路径实际用到的方法；
  未覆写方法被调用时 panic，明确提示需要补实现（fail fast）。

## 使用方式

```go
type myTestEngine struct {
	*testutil.EmbeddedChatEngine
	history []contract.EngineMessage
}

func (e *myTestEngine) History() []contract.EngineMessage { return e.history }
```

## 扩展方式

给 `ChatEngine` 新增方法时：在 `EmbeddedChatEngine` 补一个 panic 实现即可，
各测试桩自动满足接口；只有真正需要该方法的测试桩才覆写。

## Review 指南

- 测试桩是否只覆写所需方法？被测试路径意外触发的未实现方法会 panic——
  这是特性，不是 bug（提示测试装配缺失）。
- 本包只允许被 `_test.go` 或测试专用包（如 `e2e/scenario`）引用。
