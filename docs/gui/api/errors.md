# 错误契约

错误响应使用 `application/problem+json`，结构由 [`error.schema.json`](../schemas/error.schema.json) 定义，示例见 [`error.json`](../examples/error.json)。客户端只按 `status`、稳定 `code`、`retryable` 和字段错误分支，不解析 `detail`。

## 映射

| code 类别 | HTTP | retryable | 说明 |
|---|---:|:---:|---|
| `*_INVALID`, `VALIDATION_FAILED` | 400/422 | 否 | 请求语法或语义错误 |
| `AUTHENTICATION_REQUIRED` | 401 | 否 | 带标准 `WWW-Authenticate` |
| `PERMISSION_DENIED` | 403 | 否 | 不泄露目标是否存在 |
| `*_NOT_FOUND` | 404 | 否 | 对授权主体不可见或确实不存在 |
| `*_CONFLICT`, `IDEMPOTENCY_KEY_REUSED` | 409 | 视情况 | 状态冲突 |
| `PRECONDITION_FAILED` | 412 | 是 | 拉取新 Snapshot 后重试 |
| `PRECONDITION_REQUIRED` | 428 | 否 | 补 `If-Match` |
| `*_LIMIT_EXCEEDED`, `RATE_LIMITED` | 429 | 是 | 带 `Retry-After` |
| `DEPENDENCY_UNAVAILABLE` | 502/503 | 是 | provider/MCP/storage 暂时不可用 |
| `REQUEST_TIMEOUT` | 504 | 是 | 未知副作用前先查幂等结果 |
| `INTERNAL_ERROR` | 500 | 否 | detail 为安全通用文本 |

## 错误原子性

- validation/auth/precondition 失败不得改变领域状态。
- 已被 actor 接受后才失败的 mutation 必须能通过 Idempotency-Key 查询原结果，不能让客户端盲目重复。
- generation pointer 更新失败时返回专用 code，明确 generation 是否已完整发布；旧 current 继续有效。
- 批量端点 P0 不提供部分成功；未来若引入，必须逐 item 返回明确状态且整体 HTTP 语义固定。

## 日志关联

每个错误返回安全 request ID；服务端用它关联内部 cause chain。客户端看到的 `detail` 不包含绝对路径、堆栈、SQL/命令、token、prompt 或 provider 原文。
