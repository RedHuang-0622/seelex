# seelexctx/tokens

## 模块定位

`seelexctx/tokens` 是 Seelex 的 token 估算基础设施：提供无外部依赖、确定性的“脚本感知”估算（CJK/ASCII/符号分别计价），替代旧 `len/3` 字节估算；被 `seelexctx` 根包、`application/core`、`mcpstack` 等消费。真实计数（provider usage）不属于本包职责，由 application 侧 `TokenAudit` 记录并反馈校准。

## 职责与非职责

- 职责：`Count` / `CountMessage` / `CountHistory` 估算；纯函数、无状态、零外部依赖。
- 非职责：不做模型感知 BPE 分词（tiktoken 等）；不访问网络；不记账（usage 记账在 `application/core` 的 `TokenAudit`）。

## 文件结构

- `tokens.go` — 估算实现
- `tokens_test.go` — 单测

## 核心实现

`Count(text)` 按 rune 分类计价：

| 脚本类型 | 计价 | 说明 |
|---|---|---|
| CJK（Han/假名/谚文） | 1 字符 ≈ 1 token | 多数模型实际 0.6~0.9，取 1 偏保守 |
| ASCII 字母/数字 | 4 字符 ≈ 1 token | 英文实际约 3.5~4.5 字符/token |
| 非 ASCII 字母/数字 | 1 字符 ≈ 1 token | 西里尔等按保守计价 |
| 其余符号/空白/emoji | 2 字符 ≈ 1 token | 标点常与词合并 |

各段向上取整，整体偏保守（宁高勿低），避免请求超出上下文窗口。

`CountMessage` / `CountHistory` 在文本估算上叠加 role、工具调用的协议开销，与旧 `ConservativeTokenCounter` 同形。

## 数据流

`seelexctx/controller.go` 默认计数器、`gap.go` 兜底、`seele.go` 兼容变量 `EstimateTokens`、`compactor`、`memory/block`、`search`、`mcpstack`、`application/core` 计数器均消费本包估算；application 侧随后用 provider usage 校准（见 `application/core/task_context/token_counter.go` 的 `CalibratedTokenCounter`）。

## 依赖方向

- 仅依赖标准库与 Seele `types`；不得反向依赖 `seelexctx` 根包或 `application`。
- 允许任意包依赖本包（无环）。

## 并发、存储、安全语义

- 纯函数、无状态，天然并发安全。
- 估算偏保守（高估），避免“估算说够、实际爆顶”。

## 扩展方式

- 接入模型感知 tokenizer 时，保持本包函数签名不变，在 `calibratedTokenCounter` 或新实现中替换基础估算。
- 新增脚本类型：扩展 `Count` 内的 rune 分类。

## Review 指南

- 计价系数是否仍偏保守（高估）；
- 是否保持无状态、无外部依赖；
- `CountMessage` 的协议开销是否与 provider 消息结构对齐。

## 测试与验证

```text
go test ./seelexctx/tokens -count=1
```
