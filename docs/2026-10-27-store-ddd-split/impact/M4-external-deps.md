# M4 外部依赖影响清单

本批只把 snapshot 持久化调用点接到 application 的 TurnStoreCommit，并用 store 暴露的
三个单聚合写口执行原有写序。

| 文件 | 变化 | 验证 |
|---|---|---|
| `application/conversation/turn_store_commit.go` | 新增应用层 UoW：Artifact → Session → Compaction | `go test ./application/conversation` |
| `application/conversation/turn_store_commit_test.go` | 钉住 artifact → message → compaction 及失败短路 | `go test ./application/conversation` |
| `wiring/adapters/session_workspace_ports.go` | snapshot 持久化改调 UoW；adapter 将 app DTO 映射到 store 三段写口 | `go test ./wiring/adapters` |

保持不变：provider history/state 停写、session record 编码、fork 栈重建、M0 半失败矩阵。

执行：隔离分支 `codex/store-ddd-M4`，checkpoint `ba80a33f`；dry-run 与 apply 通过，
journal `%TEMP%\seelex-store-ddd-M4-16576.json`。禁止触碰用户数据。
