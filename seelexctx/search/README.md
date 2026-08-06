# seelexctx/search — 历史记录检索

## 定位

超长会话里窗口外轮次被压缩成 CompactStack 摘要（栈顶递归内嵌），模型在
请求中只剩摘要——久远但相关的细节丢失，且没有按需读回的入口。本包把
「用压缩栈做语义索引，在索引范围内查聊天记录」做成显式一步：

```text
查询 + CompactStack 全部帧（语义索引）
  └─ memory.Select：词法相关性选相关帧（top-K，recency 加分）
  └─ 按命中帧 [From..To] 单元范围从事件流读回真实聊天记录（clamp 到边界）
  └─ token 预算内有界返回（帧命中 + 范围 + 记录 + 相关性排序）
```

## 契约

| 符号 | 语义 |
|---|---|
| `Searcher.Search(query, opts)` | 核心检索：索引路径（压缩栈）/ 兜底路径（尾部扫描） |
| `StackSource` | 压缩栈读取（`sessionstore.SessionContextStore` / `seelexctx.CompactStackStore` 满足） |
| `EventSource` | 事件流读取（`NewRouterEventSource` 绑定 Router；测试可用假实现） |
| `Result` | 权威返回：hits + 索引规模 + 轮数 + 预算 + 边界提示（JSON 序列化给工具/UI） |
| `Options.Limit` | 命中数上限（默认 3，硬上限 20） |
| `Options.TokenBudget` | 记录总预算（默认 4000，硬上限 12000——绝不无界） |

## 边界语义

- **空查询**：显式拒绝（`ErrEmptyQuery`），不静默全量返回。
- **无压缩栈**：尾部扫描兜底——事件流全部轮次按单元展开为候选（复用
  `memory.Select` 同款打分），只扫描最近 `MaxFallbackScanUnits`（300）轮；
  提示文案明示「历史未压缩：可检索性有限」。选兜底而非拒绝：短会话
  （尚未触发压缩）同样需要可检索，且全量扫描无额外 I/O（事件流本来就
  要完整读取以对齐单元索引）。
- **帧范围越界**：`[From..To]` 与 `CompleteEventUnits` 单元下标近似对齐，
  clamp 到事件流边界；clamp 后倒置 → 空命中（不报错）。
- **预算耗尽**：按相关性顺序累计，帧内记录截断（`Hit.Truncated`）或
  后续命中丢弃（`Result.Truncated`）——硬上限内绝不无界拼接。
- **单条内容**：截断到 `maxRecordChars`（800 字符），结果摘要同样截断。

## 设计原则

- **复用**：选取直接用 `seelexctx/memory`（`Select` + `Score` 同款打分），
  不复制打分逻辑；范围渲染与 `gap.go` / `controller.go` 同风格。
- **纯数据 + 构造注入**：输入输出都是数据结构，StackSource/EventSource
  接口化——未来换向量检索器只换装配点。
- **事件流是真相源**：读回的是 append-only 事件库里的真实记录
  （用户输入 + assistant 摘要 + 工具名/结果摘要），不读压缩摘要拼凑。

## 测试

```text
go test ./seelexctx/search -count=1
```