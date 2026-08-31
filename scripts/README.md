# Build and Maintenance Scripts

## 模块定位

`scripts/` 提供开发者入口脚本，封装常用构建、跨平台打包和本机账号同步。脚本不承载 Application 业务逻辑。

## 文件

| 脚本 | 用途 |
|---|---|
| `build.ps1` | Windows/PowerShell 构建入口。 |
| `build.sh` | POSIX 构建入口。 |
| `build-gui.ps1` | Wails GUI build tags、ldflags 与输出目录。 |
| `seelex-flow.ps1` | 分阶段构建/部署/回滚/发布流程（Stage → Smoke → Deploy → Release）。 |
| `sync-claudecode-account.ps1` | 从本机 Claude Code 设置生成 local account 配置。 |

构建产物必须进入 `dist/<os>-<arch>/` 或版本化发行目录，不在仓库根产生二进制。

仓库级 clean/build 编排由根目录 `Makefile` 提供：`make release` 严格执行全部公开产物的 clean → build → package；`make rebuild-gui VERSION=<tag>` 构建 Dev GUI，要求 `LOCAL_CONFIG`（默认 `config/accounts.yaml`）存在，并把它作为不透明文件复制为包内 `config/accounts.yaml`；`make publish-rebuild-gui VERSION=<tag>` 构建 Publish GUI，只包含 example。`guard-dist` 会拒绝清理仓库 `dist/` 之外的路径。

## 分阶段部署流程（推荐）

日常更新 dev GUI 使用 `scripts/seelex-flow.ps1`（或 Makefile 封装），
把「更新当前可用基线」与「产出发布包」分开，全程不清理 `dist/`：

1. `make stage-gui`：把新 GUI 二进制构建到 `tmp/staging-gui/`（暂存区），不触碰基线。
2. `make smoke-gui`：对暂存区二进制做无头冒烟（`-version` + backend 启动链路），
   报告保留在 `tmp/smoke/`（时间戳独立文件，可作恢复参照）。
3. `make deploy-gui`：检查运行中的 seelex 进程；无进程或进程退出且确认后，
   先把当前基线二进制存入 `tmp/stash/seelex-gui-dev/`，再覆盖
   `dist/seelex-gui-dev/seelex-gui.exe`。只替换二进制，`config/` 与 `.seelex/` 不变。
4. `make rollback-gui`：从 stash 恢复上一个可用版本（同样有进程检查与确认门禁）。
5. `make release-dev VERSION=vX.Y.Z`：构建各平台 CLI 发布包 + Windows GUI
   发布包（仅 example 配置，绝不含 `accounts.yaml` / `*.local.yaml`），
   不清空 dist，基线工作区不受影响。

一键流程：`make dev-flow VERSION=vX.Y.Z`（交互式确认每一阶段；
Agent/CI 场景在操作者已确认后加 `CONFIRMED=1`）。

`make release` 仍是按 AGENTS.md 要求的公开发布 clean → build → package 路径，
执行前必须按 `MEMORY.md` 检查进程、备份 `dist/` 内用户数据。

## 安全与可移植性

- 账号同步输出只能是 ignored `*.local.yaml`，不得打印 token。
- `build-gui.ps1` 默认且公开发布固定使用 `-BuildKind Publish`，该模式拒绝 `-LocalConfigPath`；只有 `-BuildKind Dev` 才要求并复制真实配置。本地生成的 Dev GUI ZIP 含账号配置，不得公开上传。
- 路径使用 script root/repo root 解析，不依赖调用者当前目录。
- Windows 与 POSIX 脚本应保持版本、文件白名单和输出命名一致。
- 清理命令在删除前验证目标位于 `dist/`。

## Review 与验证

修改脚本后至少执行对应平台 dry run/build，并运行：

```text
go test . -run 'Release|Build' -count=1
go build ./...
go build -tags "gui,desktop,production" ./...
```

更详细的输出约定见 [`.claude/build-convention.md`](../.claude/build-convention.md)。
