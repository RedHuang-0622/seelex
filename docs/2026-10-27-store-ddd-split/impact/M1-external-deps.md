# M1 外部依赖影响清单

范围：只把项目看板消费方从根 `store` 具体类型切到消费方窄端口、contract DTO 和
`store/board` 唯一折叠实现。JSON 形状、路径与业务判定不变。

| 文件 | 变化 | 验证 |
|---|---|---|
| `wiring/registry/registry.go` | `RegisterProjectBoardReadTool` 接收 `board.ProjectBoardReader`；内部行视图改用 `dto.ProjectBoardRow` | `go test ./wiring/registry` |
| `kernel/board/boardtest/store.go` | 内存替身折叠读改调 `store/board.Fold`，不再依赖根包 | `go test ./kernel/board` |

反向依赖：`kernel/board` 的测试通过 `boardtest.Store` 使用该替身；生产读取逻辑仍只依赖
`dto` 与消费方端口。

执行约束：

- 分支：`codex/store-ddd-M1`
- 基线：`6c0545fdbb1e007bff4e914431ac7c7eb54fb716`
- 脚本：`scripts/store-ddd/M1-external-migrate.py`
- 只在隔离 worktree 运行；支持 dry-run、apply、rollback。
- 不触碰 `.seelex`、`config`、`dist` 或任何用户数据。
