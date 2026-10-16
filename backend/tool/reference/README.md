# backend/tool/reference

## 生态位

`backend/tool/reference` 是**工具结果引用与分页**的判据落点：`node:` 前缀解析、contains 过滤 →
offset/limit 分页 → UTF-8 边界对齐，以及模型面 JSON 编码。

主要调用方：`application/core`（`ReadToolResultHandler` / `ToolResultContent` 两个工具面入口，
以及 `reference_tools.go` 里"从哪读"的编排：pending 内存态 → 会话落盘 → 引擎桥节点归档）。

落点沿革：2026-10-16 随 M5-R-tool-reference **接收者转移**从
`application/core/reference_tools.go` 归位（plan §2.6）。

## 职责与非职责

**做**：

1. `NodeResultRef`：解析 `node:<nodeID>:...` 前缀（非节点引用返回 false）；
2. `BuildPage`：contains 过滤（按行保留匹配行）→ 分页 → **UTF-8 边界对齐**（保证不切半个 rune）；
3. `EncodePage`：模型面 `read_tool_result` 的 JSON 编码，并对"offset 超出过滤后内容"显式报错。

**不做**：不决定"从哪读"（pending / 落盘 / 引擎桥由门面编排）；不做 `result:call_` 别名映射
（那要读任务域的工具调用映射）；不实现编码之外的载荷形状变化。

## 目录与文件结构

| 文件 | 职责 |
|---|---|
| `reference.go` | `NodeResultRef` / `BuildPage` / `EncodePage` + UTF-8 边界助手 |
| `reference_test.go` | 前缀解析表、分页的 UTF-8 边界、contains 过滤、JSON 形状与"一次一字节也要前进" |

## 核心实现

- `BuildPage(result, offset, limit, contains)`：**GUI 与模型面共用同一页结构**——两条通道读到同一份
  归档、得到同一页边界。`offset` 超出过滤后内容时返回空内容并**保留原 offset**，由调用方据
  `Offset > TotalBytes` 报错（错误文案在 `EncodePage`）。
- UTF-8 对齐：起点向前、终点向后各退到 rune 起点；若退完两端相等且仍有内容，取下一个完整 rune
  （保证"每次调用都前进"，模型不会卡在半个汉字上）。

## 数据流

```text
模型/前端请求（result_ref, offset, limit, contains）
  → 门面解析 ref（node: 前缀 → 引擎桥；result:call_ → 任务域别名映射；其余 → pending/落盘）
  → reference.BuildPage / EncodePage（本包：过滤 → 分页 → UTF-8 对齐 → JSON）
  → 模型面 JSON 字符串 / GUI 的 ToolResultPage
```

## 依赖方向

允许：标准库、`middle/protocol/model`（`StoredToolResult` / `ToolResultPage` 形状）。
禁止：`application/*`、`store`、引擎与工具实现包。

## 并发、存储与错误语义

- 本包**无状态、无锁**：纯函数，可并发调用。
- 不读盘、不写盘：数据来源由门面编排，本包只处理"已取到的内容"。
- 错误语义：`EncodePage` 对越界 offset 显式报错（`read_tool_result: offset exceeds filtered content`）。

## 扩展方式

- 新增引用前缀（例如未来的 `artifact:`）：在 `NodeResultRef` 同级加解析函数 + 门面加一条取数分支；
  分页与编码**不要**复制第二份。
- 改分页语义（对齐/前进规则）：只改 `BuildPage`，两条通道自动一致。

## Review 指南

- 分页是否仍**保证前进**（next_offset 严格增加或内容为空）？
- UTF-8 边界是否仍落在 rune 起点？越界 offset 是否仍由 `EncodePage` 显式报错？
- contains 过滤是否只做"按行保留匹配行"（不得偷偷改语义）？
- 是否出现第二份分页/编码实现（GUI 侧或工具侧）？

## 测试与验证

```text
go test ./backend/tool/reference/ -count=1
go test ./application/core/ -run 'ToolResult' -count=1   # 门面级集成（node 桥 / result:call_ 别名）
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### reference.go

- `func NodeResultRef(ref string) (string, bool)` — 解析 `node:<nodeID>:...` 前缀的子代理结果引用。
- `func BuildPage(result model.StoredToolResult, offset, limit int, contains string) model.ToolResultPage` — 计算一页（过滤 → 分页 → UTF-8 对齐）。
- `func EncodePage(result model.StoredToolResult, offset, limit int, contains string) (string, error)` — 模型面 JSON 编码；越界 offset 显式报错。
