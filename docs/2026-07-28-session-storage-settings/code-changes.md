# 代码变更摘要

## 新增/修改文件

| 文件 | 类型 | 说明 |
|---|---|---|
| `sessionstore/sessionstore.go` | 新增 | `Repository` 原子读写契约、JSON generation/manifest backend、SQLite/PostgreSQL transaction backend、运行时 router。 |
| `sessionstore/sessionstore_test.go` | 新增 | 分片 generation 一致性、SQLite 回环和 backend 切换测试。 |
| `session/manager.go` | 修改 | 将生产调用路由到可配置的 atomic repository，同时保留旧 nested store 的兼容路径。 |
| `main.go` | 修改 | 从 `.seelex/session-storage.json` 装配 router，并在退出时关闭它。 |
| `application/session_storage.go` | 新增 | 应用层配置读取、连接测试和切换 API。 |
| `application_adapters.go` | 修改 | session manager storage adapter。 |
| `gui/bridge.go`、`gui/bridge_test.go` | 修改 | 向 Wails 前端暴露设置 API。 |
| `gui/frontend/dist/*` | 修改 | 右上角设置按钮、JSON/SQLite/PostgreSQL 表单与连接测试/保存。 |

## 原子性语义

- JSON：每次写入生成新的一组 shard；最后原子替换 `manifest.json`。读取固定一个 manifest 后只读取其中指定 generation，因此不会混读两次写入的 shard。
- SQLite / PostgreSQL：一个事务 upsert 完整序列化 history 和 metadata。
- Router：在持有读锁的存储操作完成后才允许 backend swap；切换先打开、初始化并 Ping 新 backend，再保存配置并交换引用。

## API

```go
type Repository interface {
    WriteAtomic(context.Context, Key, []types.Message) error
    Read(context.Context, Key) ([]types.Message, error)
    ReadRange(context.Context, Key, offset, limit int) ([]types.Message, int, error)
    List(context.Context, projectID string) ([]SessionMeta, error)
    Delete(context.Context, Key) error
    Ping(context.Context) error
    Close() error
}
```

## 验证

- `go test -p 1 ./... -count=1 -timeout 300s`：通过。
- `go vet ./...`：通过。
- `node --test gui/frontend/dist/*.test.mjs`：通过。
- GUI package 构建通过：`v0.1.0-alpha.1-storage-settings`。

## Graceful GUI exit follow-up

| File | Change | Purpose |
|---|---|---|
| `application/app.go` | Added `BeginGracefulShutdown` and `WaitForIdle` | Rejects new input and exposes a non-cancelling idle barrier. |
| `application/chat.go` | Keeps queued work in the running state until the final request completes | Prevents an observable idle gap between queued requests. |
| `gui/shutdown.go` | Added a testable close coordinator | Prevents window close during active work, then quits once idle. |
| `gui/run_wails.go` | Wires Wails `OnBeforeClose` to the close coordinator | Keeps the Wails loop alive so main cleanup cannot cancel the active chat early. |

The GUI does not call `CancelChat` when the user closes its window. It keeps the
window open, stops accepting new input, waits for accepted and queued work to
finish, then exits through Wails so no GUI process remains in the background.

Validation for this follow-up:

- `go test -p 1 ./... -count=1 -timeout 300s`: passed.
- `go test -tags "gui,desktop,production" ./application ./gui -count=1`: passed.
- `node --test gui/frontend/dist/*.test.mjs`: passed (26 tests).
- `go vet ./...`: passed.
- GUI package: `seelex-v0.1.0-alpha.1-storage-settings.2-windows-amd64-gui.zip`.

## Project-card follow-up

The right-side Project card now renders only `snapshot.current_workspace`.
The application launch directory is no longer inferred as a project, so an
unbound session explicitly shows `No project selected` and has no read/write
scope. A bound session renders its own `name` and `root_path`.

Account credentials remain intentionally excluded from the distributable ZIP.
An unpacked release needs a local `config/accounts.yaml` before it can use a
MiniMax account; the package includes only `accounts.example.yaml`.

## 建议提交信息

```text
feat(session-storage): add atomic pluggable persistence settings

- route all session reads and writes through an atomic repository
- support JSON, SQLite and PostgreSQL backends
- expose storage configuration in the GUI settings panel

Refs: session-storage-settings
```
