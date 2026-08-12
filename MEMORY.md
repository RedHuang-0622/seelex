# Seelex 协作记忆（危险操作铁律）

创建：2026-08-12
范围：所有 Agent 与维护者在仓库内执行**可能删除、覆盖或移动用户数据**的操作前必须阅读并遵守本文件；AGENTS.md 已引用。

## 铁律：先中文预警，再执行

执行以下任何一类操作前，**必须先用中文向用户说清楚**：将删除什么、影响哪些用户数据、是否可恢复、建议的备份方式；得到确认后再动手。

- `make clean` / `make release` / `rebuild-gui` / `publish-rebuild-gui`（内部含 `rm -rf`）；
- `rm`、`Remove-Item`、`git clean`、`git reset --hard`、`git checkout --`、目录/文件移动；
- 任何以 `dist/`、`.seelex/`、`config/` 为目标或会重建这些目录的命令；
- 任何清理"生成物/临时目录"的操作。

## 执行前必查清单

1. **运行中的进程**：`Get-Process | Where-Object { $_.ProcessName -match 'seelex' }`——有则禁止 clean/删除，先请用户关闭；
2. **会话记录**：目标目录内是否存在 `.seelex/`（`sessions`、`sessions-json`、`sessionstore` 数据）；
3. **真实配置**：是否存在 `config/accounts.yaml`、`*.local.yaml`、`seele.yaml`、`seelex.yaml` 或 dist 内的配置副本；
4. **备份**：可恢复数据先复制到临时目录（如 `$env:TEMP`）再操作；
5. **确认**：把上述检查结果用中文告知用户，明确"将删除 X、影响 Y、可/不可恢复"，获得同意后执行。

## 会话记录位置（本仓库已知）

- 仓库根 `.seelex/`：CLI 或从仓库根启动时的会话（`sessions/`、`sessions-json/`）；
- `dist/seelex-gui-dev/.seelex/`：dev GUI 在 dist 目录内启动时产生的会话——**此目录曾于 2026-08-12 被 `make release` 的 clean 误删**，必须视为用户数据；
- `seelebridge/.seelex/`：仅 mcp-traces，非会话数据（若出现 sessions 同样视为用户数据）；
- 用户目录/AppData：本项目当前未使用，检查时顺带确认。

## 事故记录（2026-08-12）

- 现象：`make release` 的 `clean` 执行 `rm -rf dist`，删除了 `dist/seelex-gui-dev/` 内的账号配置副本与（若存在）`.seelex` 会话记录；运行中的 `seelex-gui.exe` 因文件锁未被删除，但目录内其它内容被清空。
- 根因：构建系统将 `dist/` 视为纯可丢弃产物，但 dev 构建会把真实账号配置复制进 dist，且应用按运行目录存放会话（双击 dist 内 exe 时数据落在 dist 内）；操作前未检查进程、未预警。
- 教训：**任何"清理类"命令默认 dist 内可能有用户数据**；先查进程、先查 `.seelex` 与配置、先备份、先中文预警。
