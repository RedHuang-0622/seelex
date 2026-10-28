# Store DDD M11 测试报告

日期：2026-10-28
范围：M0–M10 合并后的发布候选，含最新 `main` 融合。

## 概览

| 项目 | 结果 | 说明 |
|---|---:|---|
| `go build ./...` | PASS | 普通构建 |
| GUI 生产构建 | PASS | `-tags "gui,desktop,production"` |
| `go vet ./...` | PASS | 无输出 |
| `go test ./... -count=1 -timeout=1200s` | PASS | 全仓 Go 包 |
| `go test ./test/ -count=1` | PASS | 仓库级集成用例独立复跑 |
| Node 前端测试 | PASS | 874 tests / 874 passed |
| `go test -race` | PASS | `store/...`、subagent、worktree |
| `gofmt -l .` | PASS | 无输出 |
| `git diff --check` | PASS | 仅出现 Windows 行尾提示，无 whitespace error |

## 分层覆盖

| 维度 | 结果 | 关键证据 |
|---|:---:|---|
| 单元测试 | PASS | `go test ./...` |
| 集成测试 | PASS | `./test/`、`application/assembly/integration`、`e2e/scenario` |
| 边界测试 | PASS | M10 import/God ratchet、contract 文件预算、状态词表门禁 |
| 性能测试 | PASS | `TestCommitNotBlockedByFullHistoryRead` 全仓复跑通过 |
| 并发测试 | PASS | `go test -race ./store/... ./backend/multiagent/subagent ./backend/tool/worktree` |
| 内存/句柄 | PASS | registry 句柄用例定向复跑通过；全仓复跑通过 |
| 静态分析 | PASS | `go vet ./...` |
| 安全边界 | PASS | 配置、密钥、越界路径门禁随全仓测试通过 |

## 冷读与生命周期

全仓测试覆盖并通过：

- 会话 layout golden、冷读 fixture、message/event/compact/retention/lifecycle；
- tool result、media、fork、subagent recovery、role draft、team_close；
- 数据根多进程锁、陈旧锁恢复、run/workspace 并发隔离；
- 会话 release、Runtime shutdown、worktree prune 与资源关闭路径。

## 失败与恢复记录

首次全仓复跑发现：

| 用例 | 根因 | 处理 |
|---|---|---|
| `TestSubagentSessionsToolResultArchiverLazyAndReuse` | 节点归档读面把裸调用 ID 当成非 wire ref | `InMemoryToolResultArchiver.Read` 兼容裸调用 ID；定向与全仓复跑通过 |
| registry TempDir cleanup | 全仓并行负载下文件句柄释放时序 | 定向复跑通过；后续全仓复跑通过 |
| `TestCommitNotBlockedByFullHistoryRead` | 全仓并行磁盘负载导致单次绝对耗时超兜底 | 定向复跑与后续全仓复跑通过 |
| contract DAG 文件预算 | M7 窄快照导入多 1 行 | 合并 import 分组，行为不变 |
| e2e 旧 `wiring/*` 路径 | main 已迁到 `boot/`、`internal/host/` | 门禁与 README 链接跟随新 owner |

## 综合判断

- [x] ✅ 通过
- [ ] ⚠️ 有条件通过
- [ ] 🚨 不通过
