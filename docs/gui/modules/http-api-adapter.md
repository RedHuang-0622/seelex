# HTTP API Adapter 模块详细设计

> 状态：拟议方案
> 总体架构：[`../architecture.md`](../architecture.md)
> API：[`../api/http-api.md`](../api/http-api.md)

## 1. 职责与边界

HTTP adapter 把 Application query/action 映射为认证、限流、可观测的 JSON API。它拥有路由、认证、transport DTO、cursor codec、幂等记录和 HTTP 错误映射；不拥有 session 状态、权限业务决策、Workspace 路径或 generation 文件句柄。

## 2. 结构

```text
HTTP server
  → request limits / request ID
  → authentication
  → route scope authorization
  → JSON + Schema validation
  → idempotency / revision precondition
  → Application port
  → transport mapper
  → response / audit
```

计划目录 `transport/httpapi/` 按 `handler`、`auth`、`cursor`、`idempotency`、`problem` 和 `middleware` 分块。`cmd/seelex-server/` 只负责配置和生命周期。

## 3. 依赖接口

Authenticator、Authorizer、Clock、IDGenerator、CursorCodec、IdempotencyStore、AuditSink 和 Application API 全部构造注入。handler 只依赖最窄接口；内存实现仅用于测试/单机模式，不作为包级全局变量。

## 4. 并发与资源限制

- server 设置 header/body/read/write/idle 超时与最大 body。
- mutation 的幂等锁按 `(principal, route, idempotency-key)` 分片，不持锁调用 Application。
- 同 key 并发请求共享已完成结果或得到明确 in-progress 响应；不同 payload 重用 key 返回 `409`。
- SSE/流式传输若后续引入，使用每 scope 有界缓冲与 resync，不直接暴露内部 channel。

## 5. 安全

默认只监听 loopback。远程模式要求 TLS、显式 audience、短期 token 和最小 scopes。拒绝 query token、cookie 隐式认证、任意 CORS、路径到本地文件的直接映射及错误中的内部堆栈。

## 6. 兼容与错误

网络版本固定在 `/api/v2`。transport DTO 由 `docs/gui/schemas/` 约束；Wails 类型变化不会自动改变 HTTP。错误映射遵循 [`../api/errors.md`](../api/errors.md)，取消/超时不包装成 500。

## 7. 测试

使用 `httptest` 和 fake Application，覆盖认证、scope 越权、body 限制、未知字段、cursor 篡改、revision 冲突、幂等 replay/conflict、超时取消、日志脱敏和并发请求。测试不得访问真实模型或网络服务。
