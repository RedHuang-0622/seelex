# 代码变更摘要

## 新增/修改/删除文件

| 文件 | 类型 | 说明 |
|---|---|---|
| Seele `session/loop.go` | 修改 | tool_calls 分支保留模型 content |
| Seele `session/chat_test.go` | 修改 | 新红绿回归：第二请求完整回放模型输出 |
| `backend/engine/context/history.go` | 修改 | 工具轮 content 不再归零 |
| `store/durable_history.go` | 修改 | Durable 冷载保留工具轮 content |
| `store/provider_wire.go` | 删除 | 删除已失效的第二份归零实现 |
| `application/core/*` | 修改 | 生产探针改为非空 content 形状 |
| `test/prefix_invariant_fullchain_test.go` | 修改 | 全链路断言 content/reasoning/tool_calls 回放 |

## 删除清单

- `store/provider_wire.go` 的 `ProviderWireMessages` 全文件删除；唯一职责是对工具轮
  assistant 正文做二次归零。新唯一语义位于 Seele `session/loop.go`：模型生成什么，
  history 就保存什么。

## API 变更

无公开 API 签名变更。

## 循环依赖检查

- [x] 无新增循环依赖

## 依赖发布

已完成：

1. Seele 提交 `7d78abc`，发布 tag `v0.3.6` 并推送 main/tag。
2. Seelex 删除本地 replace，`require` 抬到 `v0.3.6`。
3. `GOWORK=off go mod tidy` / `go mod vendor` 完成。

## 建议提交

Seele：

```text
fix(session): replay tool-call model output
```

Seelex：

```text
fix(context): match DeepSeek output cache boundary
```
