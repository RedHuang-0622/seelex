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

## 安全与可移植性

- 账号同步输出只能是 ignored `*.local.yaml`，不得打印 token。
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
