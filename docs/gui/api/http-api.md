# HTTP API v2

> 状态：规划契约；当前桌面版仍使用进程内 Wails Bridge。
> Base path：`/api/v2`
> Media type：`application/json; charset=utf-8`

## 通用约定

- 每个请求接受或生成 `X-Request-ID`，响应原样返回合法 ID。
- mutation 必须带 `Idempotency-Key`；修改已有 scope 时还必须带 `If-Match: "<revision>"`。
- 时间使用 UTC RFC 3339，标识符均为 opaque string。
- JSON object 拒绝未知字段；body 默认上限 1 MiB，Card/上传类端点另设更小的显式限制。
- 成功响应不得混入 warning 字符串；可操作异常使用 [`error.schema.json`](../schemas/error.schema.json)。

## 端点

| Method | Path | Scope | 语义 |
|---|---|---|---|
| `GET` | `/health` | public/ops | 进程存活与兼容版本，不泄露配置 |
| `GET` | `/workbench/snapshot` | `workbench:read` | 权威 Workbench Snapshot |
| `GET` | `/sessions` | `session:read` | 分页列出 session summaries |
| `POST` | `/sessions` | `session:write` | 创建并打开 session |
| `GET` | `/sessions/{session_id}/snapshot` | `session:read` | 获取独立 Session Snapshot/历史窗口 |
| `POST` | `/sessions/{session_id}/turns` | `session:write` | 向明确 session 提交 turn |
| `POST` | `/sessions/{session_id}/cancel` | `session:write` | 取消目标 request |
| `POST` | `/sessions/{session_id}/interactions/{interaction_id}/resolve` | `approval:write` | 带 revision 解决审批 |
| `GET` | `/workspaces/{workspace_id}/entries` | `workspace:read` | 分页目录查询 |
| `GET` | `/workspaces/{workspace_id}/resources/{resource_id}` | `workspace:read` | 有界预览，不接受原始路径 |
| `GET` | `/sessions/{session_id}/generations` | `generation:read` | 分页列出已提交 generation |
| `POST` | `/sessions/{session_id}/generations` | `generation:write` | 创建 checkpoint |
| `POST` | `/sessions/{session_id}/generations/{generation_id}:rollback` | `generation:rollback` | CAS 切换 current generation |

## 请求示例

请求与接受响应分别由 [`turn-request.schema.json`](../schemas/turn-request.schema.json) 和 [`turn-accepted.schema.json`](../schemas/turn-accepted.schema.json) 约束。审批 body 使用 [`interaction-resolution.schema.json`](../schemas/interaction-resolution.schema.json)，checkpoint/rollback body 使用 [`generation-operation.schema.json`](../schemas/generation-operation.schema.json)。

```http
POST /api/v2/sessions/sess-design-01/turns HTTP/1.1
Authorization: Bearer <redacted>
Content-Type: application/json
Idempotency-Key: 01J3M6N9R7QZ
If-Match: "42"

{
  "input": "Review the generation boundary",
  "client_request_id": "req-01J3M6N9"
}
```

成功创建异步 turn 返回 `202 Accepted`：

```json
{
  "request_id": "req-01J3M6N9",
  "session_id": "sess-design-01",
  "accepted_revision": 42,
  "status": "queued"
}
```

同一主体、路由、Idempotency-Key 和相同规范化 body 重试返回原始状态码与 body；body 不同则返回 `IDEMPOTENCY_KEY_REUSED`。

## 条件请求

- Snapshot/Query 响应可带 `ETag: "<revision>"`。
- mutation 的 `If-Match` 与目标 scope revision 比较，不与 generation ID 比较。
- generation rollback 还在 body 中携带 `expected_generation_id`，防止 revision 与持久化指针跨层混淆。
- 缺少必须的 precondition 返回 `428 PRECONDITION_REQUIRED`；不匹配返回 `412 PRECONDITION_FAILED` 或领域型 `409`。

## 生命周期与取消

客户端断开会取消尚未进入 actor mailbox 的请求；已接受的 turn 不因 HTTP 连接断开自动取消。取消必须调用显式 cancel 端点。Server shutdown 先停止接收 mutation，再有界 drain，最后取消并 checkpoint interrupted session。

## 事件传输

P0 HTTP 只提供 Snapshot/Query/Action，不承诺 SSE/WebSocket。未来事件 adapter 必须保留 [`event.schema.json`](../schemas/event.schema.json) 的 scope/seq/revision，支持从游标恢复和 `resync.required`，不得把内部 Go channel 直接映射为无限队列。
