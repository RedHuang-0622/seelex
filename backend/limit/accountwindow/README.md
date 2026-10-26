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

挡位值的事实源在 `contract/dto/account_window.go`；本包 `Presets()` 仅保留
兼容 alias 调用，校验逻辑仍由本包拥有。

物理位置：2026-10-16 按
[`../../../docs/2026-10-15-architecture-reshape/plan.md`](../../../docs/2026-10-15-architecture-reshape/plan.md)
§2.8 从 `backend/engine/context/seelectx/window.go`（该文件仍有窗口策略，但不含账号窗口）归位到 limit 板块（M2-C3）。

## 职责与非职责

- 做：给出界面挡位表；校验自定义窗口值（正数 + 防手滑上界）。
- 不做：不决定优先级的合并口径（运行时一处判定）；不落盘（覆盖层的落盘在
  [`../overlay`](../overlay/README.md)）；不改运行时状态（那是 `SetAccountWindow` 的事）。

## 目录与文件结构

| 文件 | 职责 |
|---|---|
| [`accountwindow.go`](accountwindow.go) | `MaxWindow`、`Presets()`、`Validate()` |
| [`setter.go`](setter.go) | `Setter`：可调面的判据本体（校验 → 先写盘再改内存 → 报回生效值），M5-R-limita 随接收者转移从 `application/core` 归位 |
| [`setter_test.go`](setter_test.go) | 上述判据的行为用例（原 core 侧用例已删除，随判据迁入） |

## 核心实现

```text
Presets() → []int{200_000, 400_000, 600_000, 1_000_000}   // 升序；前端只渲染
Validate(window) → error                                   // 正数 + <= MaxWindow(10_000_000)
```

**0 不是"合法的窗口"**：0 表示"清掉这个账号的覆盖"（回落到 `limits.context_window`），
那个语义由调用方在进 `Validate` 之前处置；进到这里必须是正数。

**上界是防手滑**，不是"模型真的有这么大"：它只拦掉多打一个 0 之类的输入；填得比模型
真实窗口大，请求会被 provider 拒。

**判据本体的落点**：`Setter.Set()` 是"设置 → 校验 → 先写盘再改内存 → 报回生效值"的唯一实现；
门面（`application/core.SetAccountWindow`）只解析宿主端口并转发（+ nil 守卫）。
依赖是契约端口，**结构满足即可**：`Runtime`（`contract.AccountWindowPort`，nil = 宿主未实现）
与 `Store`（`contract.AccountWindowStore`，nil = 不落盘）。

错误文案里的 `seelexctx:` 前缀是搬家前的历史包袱，按"行为零变化"逐字保留；
要改前缀属于产品可见面的决定，另立一项。

## 数据流

```text
GUI 设置 → 门面 AccountWindowPresets() → 本包 Presets()      （下发挡位表）
GUI 提交 → 门面 SetAccountWindow（解析宿主端口 + 转发）→ 本包 Setter.Set()（校验 → 写盘 → 改内存 → 报回生效值）
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
go test ./backend/limit/accountwindow/... -count=1     # 判据本体的行为用例：校验次序 / 先写盘 / 生效值 / 无端口
go test ./application/core -run AccountWindow -count=1 # 门面侧：nil 守卫 + 宿主端口断言
node --test gui/frontend/dist/account-window.test.mjs  # 两端同源
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### accountwindow.go

- `func Presets() []int` — Presets 返回界面上「每账号上下文窗口」的挡位（token，升序）。
- `func Validate(window int) error` — Validate 校验界面设的账号窗口。

### accountwindow_test.go

- `func TestPresetsAreAscendingAndValid(t *testing.T)` — TestPresetsAreAscendingAndValid：挡位表升序、不重复，且每一挡都过自己的校验。
- `func TestValidateRejectsNonPositive(t *testing.T)` — TestValidateRejectsNonPositive：0 不是"合法窗口"（0 = 清掉该账号的覆盖，由调用方
- `func TestValidateAcceptsBoundaryAndRejectsBeyond(t *testing.T)` — TestValidateAcceptsBoundaryAndRejectsBeyond：上界本身合法，超过上界被拒——

### setter.go

- `func (s Setter) Set(name string, window int) (int, error)` — Set 改一个账号的上下文窗口（设置弹窗的挡位/自定义值）。

### setter_test.go

- `func (store *recordingAccountWindowStore) SaveAccountWindow(name string, window int) error`
- `func (store *recordingAccountWindowStore) read(name string) (int, bool)`
- `func newFakeAccountWindowRuntime(global int) *fakeAccountWindowRuntime`
- `func (runtime *fakeAccountWindowRuntime) SetAccountWindow(name string, window int)`
- `func (runtime *fakeAccountWindowRuntime) AccountWindowFor(name string) int`
- `func TestSetterValidatesBeforeWriting(t *testing.T)` — TestSetterValidatesBeforeWriting：越界值与空账号名在动盘/动内存之前就被拒。
- `func TestSetterPersistsThenApplies(t *testing.T)` — TestSetterPersistsThenApplies：合法值先落盘、再改运行面，返回**生效值**。
- `func TestSetterStoreFailureKeepsRuntimeUntouched(t *testing.T)` — TestSetterStoreFailureKeepsRuntimeUntouched：写盘失败 ⇒ 运行面不动。
- `func TestSetterWithoutStoreStillApplies(t *testing.T)` — TestSetterWithoutStoreStillApplies：宿主没装落盘口 ⇒ 只本次运行有效，不算失败。
- `func TestSetterNeedsRuntimePort(t *testing.T)` — TestSetterNeedsRuntimePort：宿主没实现窄可选能力面时给出明确错误，而不是静默无效；
- `func TestAccountContextWindowPresetsAreUsable(t *testing.T)` — TestAccountContextWindowPresetsAreUsable：挡位表归后端（前端只渲染它），且每个挡位都过校验。

