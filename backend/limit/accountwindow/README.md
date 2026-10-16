# backend/limit/accountwindow

## 生态位

`accountwindow` 是「每账号上下文窗口」（界面 设置 → 账号）的**唯一判据处**：
界面挡位表与自定义值的校验边界。窗口是压缩预算的基数（预算 = 窗口 − 账号档
`max_tokens` − 窗口÷安全除数），而"主账号 1M、便宜的子代理账号 200k"是常态，
所以它是**逐账号**的设置，不是一刀切。

三个来源各有自己的家，本包只管"界面这一路的取值是否合法"：

| 来源 | 家 |
|---|---|
| 界面上设的（挡位/自定义） | 数据根的覆盖层 [`../overlay`](../overlay/README.md)（`account_windows`） |
| 全局阈值 | `config/seelex.yaml` 的 `limits.context_window`（[`backend/limit`](../README.md)） |
| 账号档自报值 | 账号池（遗留兜底） |

优先级（账号级 > 全局 > 账号档）由运行时的 `ContextWindowFor` 一处判定；
`application/core` 的 `SetAccountWindow` 只做"校验 → 写盘 → 改内存 → 报回生效值"。

主要调用方：`application/core`（`service_account_window.go` 的校验）、
门面 `application.AccountWindowPresets()`（把挡位表下发给前端）。

物理位置：2026-10-16 按
[`../../../docs/2026-10-15-architecture-reshape/plan.md`](../../../docs/2026-10-15-architecture-reshape/plan.md)
§2.8 从 `seelexctx/window.go`（该文件仍有窗口策略，但不含账号窗口）归位到 limit 板块（M2-C3）。

## 职责与非职责

- 做：给出界面挡位表；校验自定义窗口值（正数 + 防手滑上界）。
- 不做：不决定优先级的合并口径（运行时一处判定）；不落盘（覆盖层的落盘在
  [`../overlay`](../overlay/README.md)）；不改运行时状态（那是 `SetAccountWindow` 的事）。

## 目录与文件结构

| 文件 | 职责 |
|---|---|
| [`accountwindow.go`](accountwindow.go) | `MaxWindow`、`Presets()`、`Validate()` |

## 核心实现

```text
Presets() → []int{200_000, 400_000, 600_000, 1_000_000}   // 升序；前端只渲染
Validate(window) → error                                   // 正数 + <= MaxWindow(10_000_000)
```

**0 不是"合法的窗口"**：0 表示"清掉这个账号的覆盖"（回落到 `limits.context_window`），
那个语义由调用方在进 `Validate` 之前处置；进到这里必须是正数。

**上界是防手滑**，不是"模型真的有这么大"：它只拦掉多打一个 0 之类的输入；填得比模型
真实窗口大，请求会被 provider 拒。

错误文案里的 `seelexctx:` 前缀是搬家前的历史包袱，按"行为零变化"逐字保留；
要改前缀属于产品可见面的决定，另立一项。

## 数据流

```text
GUI 设置 → 门面 AccountWindowPresets() → 本包 Presets()      （下发挡位表）
GUI 提交 → application/core.SetAccountWindow → 本包 Validate()
                                              → overlay.WriteAccountWindow（落盘）
                                              → RuntimePort.SetAccountWindow（生效）
```

## 依赖方向

- 允许：标准库（只有 `fmt`）。
- 禁止：`application/*`、`seelebridge/*`、`seelexctx`——判据层不许回头依赖上层。
  核对：`go list -deps ./backend/limit/accountwindow`。

## 并发、存储与错误语义

- 无状态、无锁、无 IO：两个函数都是纯函数，可并发调用。
- 错误语义：`Validate` 只返回"取值不合法"，不改写、不夹带兜底；调用方拿到错误就原样
  返回给界面（不静默取默认值，否则"界面说设了、实际没设"）。

## 扩展方式

加一挡：改 `Presets()` 的切片并同步
[`../../../gui/frontend/dist/account-window.test.mjs`](../../../gui/frontend/dist/account-window.test.mjs)
的期望值（那条用例是"两端同源"的机械门禁，会逼你同时改）。
改上界：改 `MaxWindow` 并在 README 的"防手滑"说明里写清理由。

## Review 指南

- 挡位表有没有在前端或见 `application/core` 里又抄一份（必须只有这里一份）？
- `Validate` 有没有被改成"静默兜底"（应当原样报错）？
- 有没有人把 0 当成合法窗口传进来（0 是"清除覆盖"，语义不同）？

## 测试与验证

```text
go test ./backend/limit/accountwindow/... -count=1     # 本包目前无独立用例（薄判据，由下面两条覆盖）
go test ./application/core -run AccountWindow -count=1 # 用法、次序、报错面
node --test gui/frontend/dist/account-window.test.mjs  # 两端同源
```

## 文件与函数索引

### accountwindow.go

| 函数 | 签名 | 摘要 |
|---|---|---|
| `Presets` | `func Presets() []int` | 返回界面挡位表（token，升序） |
| `Validate` | `func Validate(window int) error` | 校验界面设的账号窗口（正数 + 防手滑上界） |
