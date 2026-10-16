# middle/residency

## 生态位

`middle/residency` 是中间层的**常驻与预算**落点（plan §2.4）：先落"可见快照的预算判据"
（单条工具输出的预览截断），`resident_lru` / `content_lru` / `snapshot_budget` / `session_catalog` /
`session_cold_read` / `session_input_index` / `window` / `archive_session` 家族随 R6 续做搬入。

主要调用方：`application/core`（`snapshot_budget.go` 的 `boundToolResultForSnapshot` 与
`handleToolCompleteObserved` / 会话恢复路径）。

落点沿革：2026-10-16 随 M5-R6-residency-preview **接收者转移**从
`application/core/snapshot_budget.go` 归位（plan §2.4 的 `snapshot_budget.go` → `middle/residency/`），
是本包的建包片。

## 职责与非职责

**做**：`Preview(content, limit)` —— 把超限内容截断成快照预览：按 **UTF-8 rune 边界**安全切割
（不切坏多字节字符）、去掉尾部空白、追加一行可读的省略说明（含按 **rune** 计的总字符数）。

**不做**：不读配置（进程级预算 `snapshot_tool_output_chars` 的取数与默认值在宿主侧）；
不做归档（超限全文落 `result_ref` 由宿主的任务域端口负责）；不写快照。

## 目录与文件结构

| 文件 | 职责 |
|---|---|
| `preview.go` | `Preview`：预览截断的唯一实现（`limit <= 0` = 不截断） |
| `preview_test.go` | 截断/透传、UTF-8 边界与 rune 计数、`limit <= 0` 语义 |

## 核心实现

- `Preview(content string, limit int) (string, bool)`：判据只有一份。生产调用点由宿主先取进程级预算
  （`limit.Get().SnapshotToolOutputChars`，缺省 8000）再传进来——**计算入中间层、取数留宿主**
  （与 `middle/view` 同一条口径，计划 R5c 的先例）。
- `limit <= 0` 显式定义为"不截断"：宿主保证给正数，本包不猜默认值（否则默认值会有第二份）。

## 数据流

```text
工具完成 / 会话恢复
  → 宿主取进程级预算（limits.snapshot_tool_output_chars，缺省 8000）
  → residency.Preview(content, budget) → (预览, 是否截断)
  → 超限时宿主把全文归档为 result_ref，快照只放预览
```

## 依赖方向

允许：标准库。
禁止：`backend/*`、`application/*`、`store`——本包是中间层叶子，预算由参数注入（不读配置）。

## 并发、存储与错误语义

- 无状态、无锁、纯函数：可并发调用；同样输入必得同样输出。
- 不读盘、不写盘；不返回错误（截断不是错误，是否超限由返回值告知）。

## 扩展方式

- 新增预算判据（例如"事件载荷上限"）：在本包加纯函数，预算仍由宿主取数传入。
- 新增调用点：只调 `Preview`，不要复制一份截断（省略说明的文案属于同一判据）。

## Review 指南

- 截断点是否仍落在 rune 起点？省略说明里的字符数是否仍按 **rune** 计（不是字节）？
- `limit <= 0` 的语义是否仍是"不截断"（不得偷偷回退到某个默认值）？
- 是否出现第二份预览/截断实现（宿主侧或前端侧）？

## 测试与验证

```text
go test ./middle/residency/ -count=1
go test ./application/core/ -run 'SnapshotBudget|ToolResult|Truncation' -count=1   # 宿主级：归档 + 预算注入 + 分页读回
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### preview.go

- `func Preview(content string, limit int) (string, bool)` — 把超限内容截断为快照预览（UTF-8 边界安全，含按 rune 计数的省略说明）。
