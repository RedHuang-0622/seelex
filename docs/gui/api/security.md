# HTTP 与跨模块安全

## 默认部署

服务默认只绑定 `127.0.0.1`/`::1`，生成随机端口或由配置指定。监听非 loopback 必须同时启用 TLS、认证、显式 allowed origins 和审计；启动时缺任一项应失败关闭，而不是降级为匿名访问。

## 认证与授权

- 使用 `Authorization: Bearer`；token 禁止放入 query、cookie、日志或错误。
- 校验 issuer、audience、expiry、not-before、algorithm 和 key ID；拒绝 `none` 与算法降级。
- scope 最小化：`workbench:read`、`session:read/write`、`workspace:read/write`、`approval:write`、`generation:read/write/rollback`、`admin`。
- session/workspace 资源还必须做对象级授权，拥有路由 scope 不等于可访问任意 ID。
- 浏览器调用执行严格 Origin allowlist；API 不依赖 cookie，因此不启用宽泛 credentialed CORS。

## 输入和输出

- JSON 拒绝重复/未知字段、超深对象、过大字符串和非法 UTF-8。
- 路由 ID 是 opaque identifier；不用于拼接文件路径、命令或日志格式串。
- Workspace 只接受 resource ID；每次访问重新执行 root、symlink/reparse、类型和 revision 校验。
- Markdown/Card/Tool output 按不可信内容处理；HTML 转义、URL scheme allowlist、预览截断、下载使用 inert media type。
- 错误不返回堆栈、绝对路径、环境变量、命令行、模型密钥或内部 provider body。

## 幂等、重放与审计

mutation 的 Idempotency-Key 绑定 principal、route 和 canonical body hash。记录有 TTL 和容量上限；相同 key/不同 body 立即冲突。审批和 rollback 额外校验 expected revision/generation，防止合法请求被延迟重放。

审计记录主体、操作、scope、opaque resource、结果、request ID、时间和安全决策。prompt、tool arguments、file content、token 和完整路径默认不记录；必要关联使用不可逆 hash。

## 资源保护

- 设置连接数、并发请求、body、header、响应、查询页和执行队列上限。
- 429 带 `Retry-After`；限流不得通过启动无界 goroutine 规避。
- outbound provider/MCP 请求使用 allowlist、超时和 context；HTTP adapter 不提供通用 URL fetch。
- generation 解包/恢复拒绝绝对路径、`..`、链接、设备文件和大小/数量超限。

## 密钥管理

密钥只来自环境或受限配置提供者，通过构造注入进入 authenticator/provider。配置示例只能使用占位符。轮换支持 key overlap；日志和诊断只显示 key ID，不显示值。
