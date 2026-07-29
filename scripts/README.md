# Build and Maintenance Scripts

## 模块定位

`scripts/` 提供开发者入口脚本，封装常用构建、跨平台打包和本机账号同步。脚本不承载 Application 业务逻辑。

## 文件

| 脚本 | 用途 |
|---|---|
| `build.ps1` | Windows/PowerShell 构建入口。 |
| `build.sh` | POSIX 构建入口。 |
| `build-gui.ps1` | Wails GUI build tags、ldflags 与输出目录。 |
| `sync-claudecode-account.ps1` | 从本机 Claude Code 设置生成 local account 配置。 |

构建产物必须进入 `dist/<os>-<arch>/` 或版本化发行目录，不在仓库根产生二进制。

仓库级 clean/build 编排由根目录 `Makefile` 提供：`make release` 严格执行全部公开产物的 clean → build → package；`make rebuild-gui VERSION=<tag>` 构建 Dev GUI，要求 `LOCAL_CONFIG`（默认 `config/accounts.yaml`）存在，并把它作为不透明文件复制为包内 `config/accounts.yaml`；`make publish-rebuild-gui VERSION=<tag>` 构建 Publish GUI，只包含 example。`guard-dist` 会拒绝清理仓库 `dist/` 之外的路径。

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
