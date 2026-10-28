# M10 Facade 清零外部影响清单

日期：2026-10-28
机器清单：[`M10-external-deps.json`](M10-external-deps.json)

## 批次边界

- worktree：`G:\seelex-worktrees\store-ddd-M8-to-M11`
- branch：`codex/store-ddd-M8-to-M11`
- base：M9 完成后的交接 commit `d192dab7`
- 迁移脚本：`scripts/store-ddd/M10-external-migrate.py`
- 可重放补丁：`scripts/store-ddd/M10-external.patch.b64`
- checkpoint：`f49f4ec791957d8e581d3f3874580511c84848c1`
- replay branch：`codex/store-ddd-M10-replay`
- apply journal：`%TEMP%\seelex-store-ddd-M10-43bfjp6f\journal.json`

## 结构变化

删除运行期能力发现出口：

| 旧面 | 新唯一位置 |
|---|---|
| `Router.BoardsFor` / `Router.BoardsForSession` | 组合根 `store.Stores.Boards` → 会话绑定的 `store/board.BoundStore` |
| `Router.TeamworkFor` | 组合根 `store.Stores.Teamwork` → 消费方 `teamwork.planStoreRepository` |
| `Router.ToolRecordFor` | 组合根 `store.Stores.ToolRecords` → bootstrap 的 `toolRecordStore` 窄端口 |
| `MediaStoreOf` | 消费方自定义 `multimodal.MediaReader` / 组合根直接注入 |

领域归位：

| 原位置 | 新唯一位置 |
|---|---|
| `store/board.go` 的 goal/team 会话存档类型、校验、head 读写 | `store/board/session.go`，跨层形状在 `contract/dto/session_board.go` |
| `store/tool_record.go` 的 JSONL 追加、残尾与最后快照读取 | `store/record/store.go` |
| `store/router` 的运行期具体能力断言 | `store/stores.go` 的 typed `Stores` 组合根值对象 |

## 消费方

- `wiring/bootstrap` 一次打开 typed store bundle，之后只把具体 owner 或消费方窄端口注入。
- `wiring/adapters` 从 Runtime 取得会话绑定的 Board owner，不再向 Router 探测能力。
- `backend/multiagent/teamwork` 的计划/审计/绑定端口直接由 `store/teamwork.Store` 结构满足。
- `kernel/goal`、`application/goal` 与测试使用 Board domain owner 或 DTO，不依赖根能力发现。
- `backend/tool/io/multimodal` 定义自己的只读媒体端口，不再依赖根 `MediaStoreOf`。

## 验证

```text
go build ./...
go test ./application/... ./middle/... ./backend/... ./store/... ./wiring/... ./e2e/... -count=1
go vet ./...
git diff --check
```

## 数据与回滚

- 路径、JSON tag、head/watermark、schema 与用户数据零迁移。
- 外部批次可用 `scripts/store-ddd/M10-external-migrate.py --dry-run` 预检。
- apply 写系统临时目录 journal；失败自动恢复；`--rollback <journal>` 按文件哈希恢复。
- 本批次已验证 61 个目标 dry-run/apply，隔离 worktree `go build ./...` 与结构门禁通过；
  rollback 后工作树零差异，main/replay 规范化内容 parity 为 ok。
- 代码回滚不触碰 `.seelex`、`config/`、`dist/` 或会话 fixture。
