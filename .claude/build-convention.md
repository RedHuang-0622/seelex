# 构建产物分区规范（Build Layout Convention）

## 目的

**每个构建分区必须且只能落在固定的文件夹下。** 任何构建/打包产物不得
"今天落一个文件夹、明天又改一个文件夹"。本文件是分区目录的规范真源之一
（另一真源是 `scripts/build-layout.ps1`，负责 PowerShell 侧的实际路径常量）；
新增目标、修改脚本、调整 CI 之前，必须先改这里，再改 `build-layout.ps1`
与其 bash/Makefile 镜像，最后跑一致性守卫：

```text
go test . -run 'BuildLayout' -count=1
make guard-dist-layout        # dist 根只允许规范分区，多出任何条目即失败
```

## 分区总表（固定，禁止漂移）

### 对外产物：`dist/`（git 忽略，根下只允许以下五类）

| 分区 | 固定文件夹 | 内容 | 写入方 | clean 可删 |
|------|-----------|------|--------|-----------|
| P1 平台发布树 | `dist/<os>-<arch>/` | CLI 二进制 `seelex[.exe]` + `config/`(example) + `plugins/` + 许可/说明 | `make build` / `build.ps1` / `build.sh` / flow `Release` | 是 |
| P2 dev GUI 基线 | `dist/seelex-gui-dev/` | `seelex-gui.exe` + 用户 `config/accounts.yaml`、`seelex.yaml`、`.seelex/`、`plugins/` | flow `Deploy`/`Rollback`、post-commit hook | **否（除非 `CLEAN_DEV=1`）** |
| P3 发布归档区 | `dist/archive/` | `seelex-v<v>-<os>-<arch>[-gui].(zip|tar.gz)` + `.sha256` | `make package` / `build.ps1` / `build-gui.ps1` / flow `Release` | 是 |
| P4 本地快速构建区 | `dist/dev/` | post-commit 快速构建 CLI `seelex.exe` | `.githooks/post-commit` → `build-dev.sh` | 是 |
| P5 GUI 暂存区 | `dist/stage-gui/` | `seelex-gui.exe` + `version.txt`（待发布/待部署单个 exe 的放置区） | flow `Stage` | 是 |

约束：
- `dist/` 根除 P1~P5 外不得出现任何文件/目录（含 zip、版本目录、游离 exe）。
  `.seelex/`（gitignored 运行时会话数据，dev 二进制以 dist 为工作目录运行产生）
  允许存在，但构建脚本不得写入它。
- 版本化展开目录（`seelex-v<v>-.../`）只允许作为归档打包的瞬时暂存（位于
  `dist/archive/` 内），打包后必须删除。
- P2 含真实用户数据，任何 `clean` 默认保留；确需清除时显式 `CLEAN_DEV=1`。

### 流程中间态：`tmp/build/`（git 忽略，可整体删除，无用户数据）

| 分区 | 固定文件夹 | 内容 | 写入方 |
|------|-----------|------|--------|
| T1 冒烟报告 | `tmp/build/smoke/` | `smoke-*.log` / `boot-*.log` / `store-*` | flow `Smoke` |
| T2 回滚 stash | `tmp/build/stash/seelex-gui-dev/` | `seelex-gui.previous.exe` + 最近 5 份历史 + `README.txt` | flow `Deploy`/`Rollback` |
| T3 部署日志 | `tmp/build/deploy.log` | 追加的 deploy/rollback 记录 | flow `Deploy` |

约束：流程脚本（`seelex-flow.ps1`）只写上述 T1~T3；`tmp/` 其余目录是个人
草稿区，不属构建管线，规范不管理它们。

## 文件名规范

- 二进制一律叫 `seelex[.exe]`（平台树 / P4），GUI dev 基线固定 `seelex-gui.exe`。
- 归档命名：`seelex-v<版本>-<os>-<arch>.<ext>`；GUI 额外 `-gui` 段；
  checksum 与归档同名加 `.sha256`。
- 版本一律由 `ldflags -X internal/buildinfo.Version=` 注入，源码保持 `dev`。

## 主要命令对照

| 用途 | 命令 | 落点 |
|------|------|------|
| 跨平台 CLI 构建 | `make build VERSION=x.y.z` | `dist/<os>-<arch>/` |
| CLI 归档 | `make package VERSION=x.y.z` | `dist/archive/*.tar.gz(.sha256)` |
| Windows GUI 发布包 | `make publish-build-gui VERSION=vX.Y.Z` | `dist/archive/seelex-v<v>-windows-amd64-gui.zip(.sha256)` |
| dev GUI 分阶段流程 | `make dev-flow VERSION=vX.Y.Z`（或 `scripts/seelex-flow.ps1`） | Stage→`dist/stage-gui`，Deploy→`dist/seelex-gui-dev/`，Smoke→`tmp/build/smoke`，Rollback→`tmp/build/stash/...`，Release→`dist/<os>-<arch>/`+`dist/archive/` |
| 提交后快速重建 dev | post-commit hook（`build-dev.sh`） | CLI→`dist/dev/seelex.exe`，GUI→`dist/seelex-gui-dev/seelex-gui.exe` |
| 全量清理（保留 P2） | `make clean`（`CLEAN_DEV=1` 才删 P2） | P1/P3/P4/P5 |

## 构建约束（Go）

- GUI 版本必需三个 tags：`gui,desktop,production`，并
  `-X internal/buildinfo.DefaultFrontend=gui`；否则 Wails v2 拒绝构建。
- 跨平台 CLI：`CGO_ENABLED=0` + `GOOS`/`GOARCH`。
- 前端资源：`gui/frontend/dist/*` 由 `gui/assets.go` `//go:embed` 编译进二进制，
  改 HTML/JS/CSS 后必须重新 `go build` 才生效（无热更新）。

## 单一真源与守卫

1. PowerShell 脚本一律 dot-source `scripts/build-layout.ps1` 取路径，
   禁止各自硬编码输出目录。
2. `Makefile`、`build.sh`、`build-dev.sh`、CI（`.github/workflows/release.yml`）
   镜像同一张表；`release_test.go` 的 `TestBuildLayout*` 会扫描这些文件，
   发现历史遗留目录 token（`staging-gui`、`tmp/smoke`、`tmp/stash`、
   `tmp/deploy.log`、`seelex-dev` 等）或规范 token 缺失即失败。
3. `make guard-dist-layout` 校验 `dist/` 根只有 P1~P5，多余条目直接报错，
   防止任何人再往根里放东西。
4. 修改本规范 = 必须同步 `scripts/build-layout.ps1`、脚本、Makefile、CI 与测试，
   并留一条新目录映射的提交。

## 前端资源更新（提醒）

```bash
# GUI 开发基线快速重建
go build -tags "gui,desktop,production" \
  -ldflags "-X github.com/RedHuang-0622/seelex/internal/buildinfo.DefaultFrontend=gui" \
  -o dist/seelex-gui-dev/seelex-gui.exe .
```
