# 测试报告

## 概览

| 维度 | 结果 | 关键指标 |
|---|:---:|---|
| Application 单元/边界 | ✅ | `task_complete`、`task_failed`、checkpoint 清理、无进展熔断通过 |
| Prompt harness | ✅ | 终态协议及既有交付/验证边界通过 |
| 静态检查 | ✅ | `go vet ./...` |
| 普通构建 | ✅ | `go build ./...` |
| 全仓测试 | ✅ | `go test ./... -count=1 -timeout=180s`，116.5 秒 |
| Race | ⚠️ 未执行 | Windows 环境缺少 `gcc`；`CGO_ENABLED=1 go test -race` 在 `runtime/cgo` 构建前失败，未发现也未掩盖 race 结果 |
| Dev GUI 构建 | ✅ | `dist/seelex-v0.1.0-alpha.1-windows-amd64-gui.zip` |
| Frontend 回归 | ✅ | `node --test gui/frontend/dist/*.test.mjs`，35/35 通过 |

## 已执行命令

```text
go test ./application/core ./application/prompt ./internal/promptassets -count=1 -timeout=120s
go vet ./...
go build ./...
go test ./... -count=1 -timeout=180s
CGO_ENABLED=1 go test ./application/core -race -count=1 -timeout=180s
powershell.exe -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File scripts/build-gui.ps1 -Version v0.1.0-alpha.1 -BuildKind Dev -LocalConfigPath config/accounts.yaml
node --test gui/frontend/dist/*.test.mjs
```

## Race 限制

默认 Go 环境禁用 CGO；显式启用后报 `cgo: C compiler "gcc" not found`。这是本机工具链缺失，不能等价表述为“race 已通过”。在装有 Windows C 编译器的环境中，应重跑上述 `-race` 命令。

## 综合判断

- [x] 普通测试、静态检查、普通构建和 Dev GUI 构建通过。
- [x] 未读取、打印或提交本地账户配置；GUI 脚本仅不透明复制它到 ignored `dist` 产物。
- [ ] Race 仍待具备 C 编译器的 Windows 或 Linux CI 环境补测。
# Service refactor verification (2026-07-29)

- `go test ./application/core -count=1 -timeout=120s` passed.
- `go vet ./...`, `go build ./...`, and `go test ./... -count=1 -timeout=180s` passed.
- `git diff --check` passed.
