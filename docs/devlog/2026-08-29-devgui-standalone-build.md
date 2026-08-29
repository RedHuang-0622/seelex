# 2026-08-29 devgui 独立构建 + 会话上下文继承

> 日期: 2026-08-29 | 范围: 构建/部署（`dist/devgui/`），无源码变更 | 关联: M2 并行会话修复后的 GUI 重建需求

## 背景

用户要求：用当前源码重建开发用 GUI（devgui），放到独立位置；关闭当前运行中的
旧 devgui 后，新 GUI 能直接继承会话上下文与配置。

## 关键事实（调查结论）

- 运行中的旧 GUI：`dist/seelex-vdev-windows-amd64-gui/seelex-gui.exe`
  （PID 127428，8/28 启动，无参数 → 默认 `-store .seelex/sessions`，CWD=其所在目录）。
- **当前会话存储（上下文）**：`dist/seelex-vdev-windows-amd64-gui/.seelex/`
  （`sessions-json/project-c0e88e…/session-ceaa08a9…/framework-events.json` 在会话期间
  实时写入，内容含本会话 session_id 与工具事件 → 确认为活跃 store）。
- 配置来源三通道：`accounts.yaml` 取 exe 所在目录（构建期 opaque 副本）；
  `seelex.yaml` / `seele.yaml` / `plugins` 按 CWD 解析。

## 产物

`dist/devgui/`（/dist 已 gitignore，不会入库）：

- `seelex-gui.exe`：`-tags gui,desktop,production`、`-H windowsgui`、
  `DefaultFrontend=gui`、`vcs.revision=dabe8c9`（当前 HEAD，含 8-29 修复）。
- `config/`：accounts.yaml（opaque 复制，哈希逐字节一致）+ seelex.yaml/seele.yaml 等。
- `plugins/` + 文档；`start-devgui.cmd` 启动器；`README.md` 说明。

## 继承机制（start-devgui.cmd）

1. `cd` 到仓库根 → `seelex.yaml`/`seele.yaml`/`plugins` 用实时版本；
2. `-store "…\seelex-vdev-windows-amd64-gui\.seelex\sessions"` 显式指向旧 store
   （baseDir=`.seelex`，与旧 GUI 同一存储 → 会话列表含当前会话）；
3. `accounts.yaml` 由新包自身提供（最新真实副本，`accountsPath()` 优先 exe 目录）。

## 验证

- `go version -m`：tags=gui,desktop,production、GOOS=windows、HEAD=dabe8c9。
- 无头自检：`-frontend backend -backend-prompt /help -store tmp/devgui_boot` →
  启动链路全绿、本地命令处理、`shutdown complete` 干净退出；未触发 LLM 调用。
- 批处理路径解析：REPO_ROOT / STORE / EXE 均解析正确。

## 注意（重要）

**首次 `make dev-build-gui` 失败造成旧 store 部分文件被清理**：
PowerShell `Remove-Item -Recurse` 删除旧包时先删掉了 `.seelex` 磁盘文件，随后因
exe 被运行中进程占用而报错中止（PermissionDenied）。运行中的旧 GUI 会话在内存中，
优雅关闭会重新落盘当前会话（已观察到 framework-events.json 被运行进程实时重建）；
但该 store 中其它历史会话的磁盘记录可能已丢失。后续构建应避免对运行中 exe 所在
包执行 `clean-gui`/`rebuild-gui`；新 devgui 已改用独立目录规避该冲突。
