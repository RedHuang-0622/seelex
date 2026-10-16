# backend/limit/overlay

## 生态位

`overlay` 管「运行时上限覆盖层」：用户在界面上改过的运行时上限（会话驻留窗口、
每账号上下文窗口、工具记录计数开关）落成**数据根**里的一份 JSON
（[`overlay.go`](overlay.go) 的 `FileName`），启动时作**最高优先覆盖层**叠在
`config/seelex.yaml` 的 limits 段之上。

主要调用方：根 `main.go`（启动期读盘 + 叠加 + 注入三个 `Save*` 落盘口）。

物理位置：2026-10-16 按
[`../../../docs/2026-10-15-architecture-reshape/plan.md`](../../../docs/2026-10-15-architecture-reshape/plan.md)
§2.8 从仓库根的 `runtime_limits_overlay.go`（已移除，当时属于 `package main`）归位到
`backend/limit/overlay`（M2-C2）。

## 职责与非职责

- 做：读/写覆盖层文件（读-改-写、临时文件 + rename 原子替换）、把覆盖层叠到
  `limit.Limits` 之上、把「界面上改过的那一格」落盘（`StoreFile` 实现三个 Save 口）。
- 不做：不决定数据根在哪（`Path(dataRoot)` 接收调用方给的数据根）；
  不决定覆盖层里有哪些格子的**语义**（那一格的含义属于它的消费方，
  例如窗口的读法仍是 `limit.Limits.ReleaseWindow`）。
- 刻意不做：不把覆盖值写回 `config/seelex.yaml`。那份文件是**用户的配置**
  （带注释、手改），程序回写要重排注释、抹平格式差异；而且"界面上拧的一个数"
  与"用户写进配置的数"本是两件事——覆盖层让它们各自留在自己的地方。

## 目录与文件结构

| 文件 | 职责 |
|---|---|
| [`overlay.go`](overlay.go) | `Overlay` 形状、`FileName`、`Path`、`Read`、`Apply`、三个 `Write*` 与 `StoreFile` |
| [`overlay_test.go`](overlay_test.go) | 落盘-读回闭环、坏文件/缺文件不阻断启动、覆盖优先级与校验、多键互不抹掉 |

## 核心实现

```text
Read(path)                 → Overlay（路径空 / 文件不存在 / 内容损坏 → 空覆盖）
Apply(base, overlay)       → limit.Limits（只在取值合法时压过 base；非法值记日志后跳过）
WriteSessionReleaseWindow(path, window) / WriteAccountWindow(path, name, window) /
WriteToolRecordEnabled(path, enabled)   → 读-改-写，临时文件 + rename 原子替换
StoreFile{Path}            → 实现宿主侧的三个 Save* 落盘口（空路径显式报错）
```

**为什么读-改-写**：同一份 JSON 被三个设置面（窗口 / 账号窗口 / 工具记录开关）共用，
谁后写都不能把别人那一格抹掉。

**为什么临时文件 + rename**：一次失败不会留下半份 JSON——半份 JSON 会被 `Read`
当成"损坏"**整份丢掉**，等于把用户之前改过的参数一起清掉。

**为什么不阻断启动**：界面上拧过的参数坏掉只该让那一格回到配置值（记一条日志），
不该让应用开不起来（与 `gui-appearance.json` 同一条口径）。

## 数据流

```mermaid
flowchart LR
    UI["GUI/TUI 设置面"] -->|Save*| STORE["overlay.StoreFile"]
    STORE --> FILE["数据根/runtime-limits.json"]
    FILE --> READ["overlay.Read"]
    CONFIG["config/seelex.yaml limits 段"] --> APPLY["overlay.Apply"]
    READ --> APPLY
    APPLY --> LIMITS["limit.Apply（进程级快照）"]
```

## 依赖方向

- 允许：标准库 + [`backend/limit`](../README.md)（`Limits` 与窗口校验）。
- 禁止：`application/*`、`seelebridge/*`、`seelexctx`——覆盖层在 limit 板块内部，
  不许回头依赖上层。核对：`go list -deps ./backend/limit/overlay`。

## 并发、存储与错误语义

- 本包**无内存状态**：每次读写都走文件，因此并发写入之间依靠"读-改-写 + rename"
  保持"最后一个写者赢、且不抹掉别人的格子"。
- 空路径（宿主没给数据根）在 `Write*` 与 `StoreFile.Save*` 上**显式报错**，
  不静默成功（静默成功会让"界面说存了、重启却没了"）。
- `Read` 对损坏文件的行为是**吞掉错误并记日志**（见上）；`Write*` 的 IO 错误照常返回。

## 扩展方式

新增一个界面可改的上限：在 `Overlay` 加一格（带 `omitempty`；需要区分"没改过"与
"改成零值"就用指针）→ 加一个 `Write*`（读-改-写）→ 在 `Apply` 里叠到 `limit.Limits` 上
（含取值校验）→ 在 `StoreFile` 上补对应 Save 方法 → 在 `overlay_test.go` 补"多键互不抹掉"。

## Review 指南

- 新格子是不是走"读-改-写"？直接整份覆盖会抹掉别人的格子。
- 非法值是不是"记日志 + 跳过"而不是"阻断启动"？
- 空路径是不是显式报错？
- 覆盖值的**语义**有没有被本包偷偷决定（应当由消费方决定）？

## 测试与验证

```text
go test ./backend/limit/overlay/... -count=1
go test ./backend/limit/... -count=1
```

## 文件与函数索引

### overlay.go

| 函数 | 签名 | 摘要 |
|---|---|---|
| `Path` | `func Path(dataRoot string) string` | 覆盖层落盘路径：数据根下的 `FileName` |
| `Read` | `func Read(path string) Overlay` | 读覆盖层；路径空/文件缺失/内容损坏一律回落"没有覆盖" |
| `Apply` | `func Apply(base limit.Limits, overlay Overlay) limit.Limits` | 把覆盖层叠到 limits 上（覆盖层优先，非法值跳过） |
| `WriteSessionReleaseWindow` | `func WriteSessionReleaseWindow(path string, window int) error` | 读-改-写：只动会话驻留窗口一格 |
| `WriteAccountWindow` | `func WriteAccountWindow(path, name string, window int) error` | 读-改-写：按账号写窗口（`<= 0` 清掉该键） |
| `WriteToolRecordEnabled` | `func WriteToolRecordEnabled(path string, enabled bool) error` | 读-改-写：工具记录计数开关 |
| `writeFile` | `func writeFile(path string, current Overlay) error` | 临时文件 + rename 原子落盘 |
| `SaveSessionReleaseWindow` | `func (store StoreFile) SaveSessionReleaseWindow(window int) error` | 落盘口：会话驻留窗口 |
| `SaveAccountWindow` | `func (store StoreFile) SaveAccountWindow(name string, window int) error` | 落盘口：账号窗口 |
| `SaveToolRecordEnabled` | `func (store StoreFile) SaveToolRecordEnabled(enabled bool) error` | 落盘口：工具记录计数开关 |

### overlay_test.go

| 测试 | 钉住什么 |
|---|---|
| `TestOverlayRoundTripsWindow` | 写进数据根 → 读回的闭环 |
| `TestReadFallsBackToNoOverlay` | 缺文件/坏文件按"没有覆盖"继续 |
| `TestApplyPrecedence` | 合法值压过配置；非法值忽略后仍是配置值 |
| `TestPathSitsInDataRoot` | `Path(dataRoot)` 的拼接口径 |
| `TestStoreFileWritesOverlay` | 落盘口写入 + 空路径显式报错 |
| `TestAccountWindowsOverlayRoundTripKeepsOtherKeys` | 多设置面共用一份文件时互不抹掉 |
