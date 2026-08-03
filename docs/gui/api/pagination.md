# 分页与 Cursor 语义

## 请求

分页端点统一接受：

| 参数 | 规则 |
|---|---|
| `limit` | 可选，默认 50，范围 1–200；服务端可降低 |
| `cursor` | 可选 opaque token；有 cursor 时其他排序/过滤参数必须与首次请求一致 |
| `sort` / filter | 端点白名单；进入 query fingerprint |

Offset 不作为跨写入稳定分页契约。历史内部实现可以使用 offset，但 HTTP 只暴露 cursor。

## 响应

Page 至少包含 `items`、`has_more`、`snapshot_revision`；有下一页时必须包含 `next_cursor`。Workspace 示例见 [`workspace-page.json`](../examples/workspace-page.json)，契约见 [`page.schema.json`](../schemas/page.schema.json)。

## Cursor 内容与保护

服务端 cursor codec 封装并认证以下逻辑字段：version、endpoint、principal/tenant binding、scope ID、query fingerprint、sort key、snapshot revision 或 generation ID、issued/expiry。客户端只能原样回传。

推荐使用随机 server-side handle，或经 AEAD/HMAC 保护的紧凑 token。仅 base64 JSON 不安全。密钥轮换期间允许有限旧 key ID，过期后返回 `CURSOR_EXPIRED`。

## 一致性

- 首次请求固定 snapshot revision/generation；后续页不混入更新后的数据。
- 固定基线仍可读取时返回稳定页；已回收则返回 `CURSOR_BASELINE_GONE`，客户端从第一页重启。
- 删除项不导致已发页面重排；排序键必须含唯一 tie-breaker。
- cursor 不跨 principal、endpoint、scope 或 filter 使用，任何不匹配返回 400，不泄露内部字段。

## 错误

| code | HTTP | 行为 |
|---|---:|---|
| `CURSOR_INVALID` | 400 | token 损坏、签名失败或参数不匹配 |
| `CURSOR_EXPIRED` | 410 | 从第一页重启 |
| `CURSOR_BASELINE_GONE` | 410 | 基线已回收，从新 Snapshot 重启 |
| `PAGE_LIMIT_INVALID` | 400 | 修正 limit |
