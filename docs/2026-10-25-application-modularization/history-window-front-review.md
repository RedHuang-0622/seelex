# History Window 外部端口前置审查

日期：2026-10-26

## 需求摘要

删除 `application/core` 对 `middle/switch/history` 的直接 import，由
`application/history.WindowAssembler` 消费方端口和 Wiring 注入的 Middle 实现承接
现有窗口装配判据。

## 真实调用方

| 调用方 | 当前调用 |
|---|---|
| `application/core/session_history.go:loadEarlierVisiblePage` | `ContainsSameRow`、`HasRowIdentity` |
| `application/core/session_history.go:installVisibleHistory` | `DurableRows`、`ReplaceTail`、`Prepend` |
| `application/core/session_history.go:visibleWindowFirstRow` | `DurableRows` |
| `application/history.Application` | 经 Deps 回调触发上述三段 |

提示词给出的四方法接口不完整：冷读扩窗还需要 `ContainsSameRow` 与
`HasRowIdentity`。若在 core 改用 `IndexOfSameRow >= 0` 或手写字段判断，会复制
行身份判据，因此端口必须按真实调用扩为六方法。

## 影响文件

| 文件 | 类型 | 原因 |
|---|---|---|
| `middle/switch/history/assembler.go` | 新增 | `Assembler` 逐项转发既有纯函数 |
| `middle/switch/history/assembler_test.go` | 新增 | 原函数/adapter 一致性、接缝失败与空占位 |
| `application/history/application.go` | 修改 | `Deps.WindowAssembler` 和回调签名 |
| `application/history/application_test.go` | 修改 | 回调装配新参数 |
| `application/core/session_history.go` | 修改 | 删除 Middle import，改用端口 |
| `application/core/session_conversation_history_query.go` | 修改 | 注入 assembler |
| `application/core/service.go` / `service_assembler.go` | 修改 | 构造器接收 assembler |
| `wiring/adapters/history_window.go` | 新增 | 构造 Middle `Assembler`，不复制判据 |
| `wiring/constructors.go` | 修改 | 组合根注入 |
| `application/*` 预算与 README | 修改 | core 依赖下降与迁移登记 |

## 依赖与锁边界

- 端口定义在 `application/history`（消费方），只使用协议消息值。
- 实现仍在 `middle/switch/history`；Wiring 只返回结构满足端口的实现。
- 冷读仍在 `ViewMu` 外；安装仍在 core 的短临界区内。
- 不新增状态、goroutine、锁或缓存。
- 不修改 GUI/TUI wire、事件 revision/seq、持久化格式。

## 风险

| 风险 | 概率 | 严重度 | 缓解 |
|---|---|---|---|
| 回调参数改动导致查询测试漏装 assembler | 中 | 中 | 构造期空端口显式失败；全部 history 测试补 fake/real assembler |
| core 复制 Contains/HasRowIdentity 判据 | 高 | 高 | 端口扩为六方法；依赖门禁检查 core import 为零 |
| 冷读锁边界被顺带改变 | 低 | 高 | 不改 Application 分页顺序或 core 锁结构 |
| adapter 与原函数漂移 | 中 | 高 | 同输入逐字段 parity 用例 |

## 建议

采用“消费方端口 + Middle Assembler + Wiring 工厂”方案。先加 core import 红测与
adapter 一致性红测，再补端口和实现；不移动分页用例，不把持久化或锁搬进
`application/history`。
