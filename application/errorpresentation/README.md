# application/errorpresentation

## 生态位

`application/errorpresentation` 是 Application 的**错误呈现** owner：把引擎与工具产生的错误
翻译成稳定的用户可见文本（模块 / 方法 / 摘要 / 下一步四段），供**工具错误出口**与视图侧消费。

主要调用方：

- `application/assembly` 通过 `contract.ErrorPresentationPort` 消费工具错误与结构化包装；
  Wiring adapter 只转发本包，不复制分类或文案；
- 未来收口后的 `backend/engine/*`（同款注入面）。

跨层共享的判据（结构化错误码、provider 失败分类、ReAct 预算哨兵）不在本包，而在
[`contract`](../../contract/README.md)：plan §1.1 规定
Application、Middle 与 Backend 都只能依赖契约层的错误判据，
所以判据必须落在契约，本包只消费。

## 职责与非职责

职责：

- 按**固定优先级**把错误分类成 `Presented`：结构化 code → 取消 → 上下文耗尽 → 空内容 →
  计划预检 → 执行预算 → result_ref → provider 超时/服务端故障 → 结构化字段回退 → 兜底；
- 给错误挂结构化信封（`Wrap`）与读取信封上的码（`StructuredCode`）；
- 保证呈现文本**不含** provider 状态码、request_id 与原始传输载荷（诊断细节只留在原始错误里）。

非职责：

- 不做**恢复决策**（是否写有界检查点、能否重放一次归 `backend/engine` 的恢复策略）；
- 不定义判据本身（分类词表在 `contract`，本包不复制一份）；
- 不写会话事实、不持久化、不碰 Snapshot。

## 目录结构

| 文件 | 职责 |
|---|---|
| `codes.go` | 结构化信封的挂载/读取（`Wrap` / `StructuredCode` / `ClassifyStructured`）与各分类的呈现文本构造器 |
| `presentation.go` | `Presented` 类型、分类主入口 `Classify`、`IsUnclassified`、工具出口 `PresentToolError` |
| `presentation_test.go` | 分类与"不泄露 provider 细节"的用例（从 `application/assembly` 随迁） |

## 核心实现

- `type Presented struct{ module, method, summary, next string; unclassified bool }`：四段文本 +
  兜底标记；字段不导出（只有本包能造，调用方只能 `String()`）。
- `func Classify(err error) Presented`：**唯一分类判据**。判据顺序即优先级，逐条与迁移前一致
  （迁移是纯归位，不改分类语义）。
- `func PresentToolError(toolName string, err error) string`：工具错误出口。若分类落到"代理运行时"
  兜底，则改写成"工具执行"四段（保留 `handleToolComplete(<tool>)` 方法名，工具名经 `safeToolName` 兜底）。
- `func Wrap(err error, code string) error` / `func StructuredCode(err error) string`：结构化信封的
  写入与读取（`Seele/errors` 的 `Wrap`/`From`）。包装后 `err.Error()` 保留原始消息，
  字符串匹配仍可回退（过渡期语义，未变）。

## 数据流

```text
引擎/工具产生 error
  → （产出侧）Wrap(err, contract.ErrorCode*)          // 挂稳定码
  → （消费侧）Classify(err) → Presented                // 分类选文案
       ├─ 工具出口：PresentToolError(tool, err) → ToolCall.Error（wire 上 {"error": …}）
       └─ chat 出口：**不经过本包**——chat.Error / 会话 error 行 / EventError 直接是原始正文
```

## 依赖方向

允许：标准库、`contract`、`Seele/errors`（**只读错误信封结构**）。
禁止：`backend/*`、`middle/*`、`store` 写面与前端包；本包是 Application 呈现 owner，
由 Frontend/Wiring 消费。

## 并发与错误语义

本包**无状态、无锁、无 goroutine**：全部是纯函数，同样的输入必得同样的输出（可并发调用）。
唯一的外部约束是 `Seele/errors` 的信封语义（`Wrap` 原地补字段、`From` 沿链查找）。

## 扩展方式

- 新增一类错误：先在 `contract` 加**码或分类词**（判据唯一），再在本包 `Classify` 的
  switch 里加一条分支与对应呈现文本构造器；不要在本包另写一份判据函数。
- 新增调用方：只依赖 `Classify` / `PresentToolError` / `Wrap` 三个入口，不要绕过它们直接读码。

## Review 指南

- 分类优先级是否被改动？（顺序即行为；任何"顺手重排"都是行为变更，要单独立项）
- 呈现文本是否泄露 provider 细节（HTTP 码、request_id、原始 payload）？
- 是否出现第二份判据（本包复制 `contract` 的码/分类词，或调用方自己判 `errors.Is`）？
- 用例是否仍钉住"结构化码 → 呈现文本"与"取消语义优先"两条？

## 测试与验证

```text
go test ./application/errorpresentation -count=1
go test ./application/assembly/ -run 'ChatErrorSurface|RawUnclassified|PersistenceFailure' -count=1
go test ./e2e/ -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### codes.go

- `func Wrap(err error, code string) error` — Wrap 给错误附加结构化上下文（seeleerrors.Wrap 语义）：
- `func StructuredCode(err error) string` — StructuredCode 返回错误链上的结构化错误码（seeleerrors.From）；
- `func ClassifyStructured(err error) (Presented, bool)` — ClassifyStructured 按结构化定位字段（Function/Step/Path）推断分类
- `func persistenceFailurePresentation() Presented`
- `func reactBudgetPresentation() Presented`
- `func resultRefUnavailablePresentation() Presented` — resultRefUnavailablePresentation 是 read_tool_result 结果引用不可读的
- `func contextExhaustedPresentation() Presented`
- `func unclassifiedPresentation() Presented`

### presentation.go

- `func (presentation Presented) String() string`
- `func IsUnclassified(err error) bool` — IsUnclassified 判定错误是否落到兜底分类（调用方据此决定是否用原始正文）。
- `func Classify(err error) Presented` — Classify 把错误分类成用户可见的呈现文本。判据顺序即优先级：
- `func PresentToolError(toolName string, err error) string` — PresentToolError 把工具错误呈现为 provider 可见文本（工具错误唯一出口）。
- `func safeToolName(name string) string`

### presentation_test.go

- `func TestPresentedClassificationIdentifiesSourceWithoutProviderDetails(t *testing.T)`
- `func TestClassifyStructuredErrorsByCode(t *testing.T)`
