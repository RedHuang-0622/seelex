# Storage

`seelebridge/internal/storage` 承载会话持久化的 legacy shard 文件存储
（`SessionStore`）与按工作区路由的嵌套存储（`NestedSessionStore`）。属于根
facade 的装配细节，置于 internal/；根包经 `storage_aliases.go` 重导出
`Message`/`SessionMeta`/`SessionStore`/`NestedSessionStore` 与构造器保持
公共 API（application/adapters 等外部调用面继续经 `seelebridge.*` 使用）。

## 验证

```text
go test ./seelebridge -count=1
```
