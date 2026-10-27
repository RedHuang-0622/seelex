# application/window

## 生态位

`application/window` 是上下文窗口配置的 Application 入口。真实实现、默认值、YAML
解析与 provider 轮数推导仍在
[`kernel/contextcontrol`](../../kernel/contextcontrol/README.md)
和 Engine context policy；本包只保留同身份 alias 与一行转发。主要调用方是
`application/core` 与 Wiring 启动期 window helper。

## 职责与非职责

- 做：暴露 `WindowConfig` / `DefaultWindowPolicy`，转发 Apply/Current/Load/Default/New。
- 做：计算 retained-read tail budget、保护区 floor，并执行启动期
  `ValidateRetainWindow` 组合校验。
- 不做：不定义 window 默认值、不解析 YAML、不复制轮数推导或保留窗口校验。

## 文件结构

- `policy.go`：类型 alias 与六个转发入口。
- `retention.go`：读尾预算、保护区 floor 与 window/limits 组合校验。
- `policy_test.go`：类型身份与默认值 parity。

## 核心实现

`WindowConfig` 和 `DefaultWindowPolicy` 是 Engine 类型的 identity alias。所有方法在
alias 上保持可用；转发函数不增加判据。

## 数据流

启动参数路径 → Application window Load → Engine contextcontrol；
运行期 window helper → Apply/Current → Engine 进程级策略。

## 依赖方向

允许依赖 `kernel/contextcontrol`。禁止依赖 `application/core`、GUI/TUI、
`store` 或重新实现窗口默认表。

## 并发与错误语义

本包无状态，仅转发 Engine 已有的进程级配置。`LoadWindowConfig` 的错误与 Engine
逐字一致；本包不吞错、不 fallback。

## 扩展方式

新增窗口入口时先判断 Engine contextcontrol 是否已有唯一实现；本包只增加一行转发。
不要在这里创建第二份 `WindowConfig.WithDefaults` 或轮数策略。

## Review 指南

- 类型是否仍是 identity alias？
- 是否出现第二份默认值或校验？
- import 是否只到 Engine contextcontrol？

## 测试与验证

```text
go test ./application/window -count=1
go test ./kernel/contextcontrol -count=1
go test ./application/core -run 'Window|Retain' -count=1
```

## 文件与函数索引

### `policy.go`

- `type WindowConfig = contextcontrol.WindowConfig` — 上下文窗口配置的同身份 alias。
- `type DefaultWindowPolicy = contextcontrol.DefaultWindowPolicy` — 默认窗口策略的同身份 alias。
- `func NewDefaultWindowPolicy/DefaultWindowConfig/LoadWindowConfig/Apply/Current(...)` — 逐项转发 Engine 唯一实现。
