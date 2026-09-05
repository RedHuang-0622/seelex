# Build and Maintenance Scripts

## 模块定位

`scripts/` 提供开发者入口脚本，封装常用构建、跨平台打包和本机账号同步。脚本不承载 Application 业务逻辑。
**所有构建产物路径的规范真源是 `scripts/build-layout.ps1`（PowerShell 侧单一真源），
目录分区总表见 [`.claude/build-convention.md`](../.claude/build-convention.md)。**
任何脚本不得自行发明输出目录；历史遗留目录（`staging-gui`、`tmp/smoke`、
`tmp/stash`、dist 根下的扁平 zip/exe 等）已废除，回归测试
`go test . -run 'BuildLayout'` 会拦截它们再次出现。

## 分区速览

```text
dist/                               对外产物（根下只允许这四个分区）
  dist/<os>-<arch>/                 P1 平台发布树（CLI + 运行时文件）
  dist/seelex-gui-dev/              P2 dev GUI 基线（用户数据，默认永不 clean）
  dist/archive/                     P3 发布归档（zip / tar.gz / sha256）
  dist/dev/                         P4 本地快速构建（post-commit → seelex.exe）
tmp/build/                          流程中间态（可整体删除）
  tmp/build/stage-gui/              T1 GUI 暂存区
  tmp/build/smoke/                  T2 冒烟报告
  tmp/build/stash/seelex-gui-dev/   T3 回滚 stash
  tmp/build/deploy.log              T4 部署日志
```

## 文件

| 脚本 | 用途 |
|---|---|
| `build-layout.ps1` | **目录分区单一真源**：所有 PS 脚本从这里取路径，含 dist 根守卫。 |
| `build.ps1` | Windows/PowerShell 跨平台构建+归档（P1 + P3，保留 P2）。 |
| `build.sh` | POSIX 构建入口（P1 + P3，保留 P2）。 |
| `build-dev.sh` | post-commit 快速重建 dev 二进制（CLI→`dist/dev/seelex.exe`，GUI→P2）。 |
| `build-gui.ps1` | Wails GUI 发布包（Publish/Dev），产物只进 `dist/archive/`。 |
| `seelex-flow.ps1` | 分阶段构建/部署/回滚/发布流程（Stage → Smoke → Deploy → Release）。 |
| `sync-claudecode-account.ps1` | 从本机 Claude Code 设置生成 local account 配置。 |

仓库级 clean/build 编排由根目录 `Makefile` 提供：
- `make build` / `make package`：平台树进 `dist/<os>-<arch>/`，归档进 `dist/archive/`；
- `make rebuild-gui VERSION=<tag>`：构建 Dev GUI（要求 `LOCAL_CONFIG`，
  默认 `config/accounts.yaml`，作为不透明文件复制为包内 `config/accounts.yaml`）；
- `make publish-rebuild-gui VERSION=<tag>`：构建 Publish GUI，只含 example；
- `make clean`：只清 P1/P3/P4，**默认保留 P2**（`CLEAN_DEV=1` 才删 P2）；
- `make guard-dist-layout`：校验 `dist/` 根只有规范分区，多余条目即失败。

## 分阶段部署流程（推荐）

日常更新 dev GUI 使用 `scripts/seelex-flow.ps1`（或 Makefile 封装），
把「更新当前可用基线」与「产出发布包」分开，全程不清理 P2 基线：

1. `make stage-gui`：把新 GUI 二进制构建到 `tmp/build/stage-gui/`（暂存区，不触碰基线）。
2. `make smoke-gui`：对暂存区二进制做无头冒烟（`-version` + backend 启动链路），
   报告保留在 `tmp/build/smoke/`（时间戳独立文件，可作恢复参照）。
3. `make deploy-gui`：检查运行中的 seelex 进程；无进程或进程退出且确认后，
   先把当前基线二进制存入 `tmp/build/stash/seelex-gui-dev/`，再覆盖
   `dist/seelex-gui-dev/seelex-gui.exe`。只替换二进制，`config/` 与 `.seelex/` 不变。
4. `make rollback-gui`：从 stash 恢复上一个可用版本（同样有进程检查与确认门禁）。
5. `make release-dev VERSION=vX.Y.Z`：构建各平台 CLI 发布包 + Windows GUI
   发布包（仅 example 配置，绝不含 `accounts.yaml` / `*.local.yaml`），
   平台树进 `dist/<os>-<arch>/`，归档进 `dist/archive/`；不清空 dist，基线不受影响。

一键流程：`make dev-flow VERSION=vX.Y.Z`（交互式确认每一阶段；
Agent/CI 场景在操作者已确认后加 `CONFIRMED=1`）。

`make release` 仍是按 AGENTS.md 要求的公开发布 clean → build → package 路径，
clean 只删除派生产物分区；P2（`dist/seelex-gui-dev/`）含用户数据，默认保留。

## 安全与可移植性

- 账号同步输出只能是 ignored `*.local.yaml`，不得打印 token。
- `build-gui.ps1` 默认且公开发布固定使用 `-BuildKind Publish`，该模式拒绝 `-LocalConfigPath`；只有 `-BuildKind Dev` 才要求并复制真实配置。本地生成的 Dev GUI ZIP 含账号配置，不得公开上传。
- 路径一律取自 `build-layout.ps1`（repo root 解析），不依赖调用者当前目录。
- Windows 与 POSIX 脚本保持同一张分区表（版本、文件白名单、输出命名一致）。
- 清理命令只作用于规范分区（P1 平台树、P3 `dist/archive/`、P4 `dist/dev/`），P2 需显式 `-CleanDev` / `CLEAN_DEV=1`。

## Review 与验证

修改脚本或目录后至少执行对应平台 dry run/build，并运行：

```text
go test . -run 'BuildLayout' -count=1   # 分区单源/遗留目录回归
make guard-dist-layout                  # dist 根只允许规范分区
go build ./...
go build -tags "gui,desktop,production" ./...
```

分区规范与输出约定详见 [`.claude/build-convention.md`](../.claude/build-convention.md)。
