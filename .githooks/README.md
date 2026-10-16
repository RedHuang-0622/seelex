# .githooks —— 本仓库的提交/推送触发安排

启用方式（本仓库已设置）：

```text
git config core.hooksPath .githooks
```

## 钩子一览

| 钩子 | 何时触发 | 做什么 | 怎么跳过 |
|---|---|---|---|
| `pre-commit` | 每次 `git commit`（写对象之前） | **快速护栏**：暂存区里的 `.go` 文件必须 `gofmt` 干净、不得带 UTF-8 BOM 或 CRLF；不得暂存 `tmp/`、`_tmp/`、`_scratch/`、`_logs/`、`dist/`；不得暂存真实配置（`config/accounts.yaml`、`config/*.local.yaml`、`config/mcp.yaml`、`config/search_engine.yaml`）。空白错误只提示、不拦 | `SEELEX_SKIP_HOOKS=1`（会打印原因） |
| `post-commit` | 每次 `git commit` 之后 | **按需重建 dev 二进制**：只有本次提交动了"构建相关路径"（`*.go`、`go.mod`/`go.sum`、`config/*.yaml`、`scripts/*`、`plugins/*`、`Makefile`）才跑 `scripts/build-dev.sh`；纯文档/记录类提交直接跳过（省下两个 30MB 二进制的构建，也不去动 `dist/` 里的数据） | `SEELEX_POST_COMMIT_BUILD=0` 强制跳过；`=1` 强制总是构建；`SKIP_BUILD=1` 仍然等价于跳过（向后兼容） |
| `pre-push` | 每次 `git push`（发数据之前） | `go build ./...` + `go vet ./...` 必须过；不过就拒绝推送（防止把红的树推上去） | `SEELEX_SKIP_HOOKS=1` |

## 为什么这么排

- **`dist/` 里有用户数据**（dev GUI 的配置副本、可能的 `.seelex/` 会话——见根 `MEMORY.md` 的 2026-08-12
  事故记录）。触发脚本不该在每次提交时都去碰它，所以 `post-commit` 改成"动了构建相关路径才重建"，
  且**linked worktree 里一律跳过**（`scripts/build-dev.sh` 自带这条判断，给子代理用的工作树不构建）。
- **长任务分支提交密度高**（架构重排动辄几十个 commit/天）：默认策略必须便宜；重活（两个二进制）按需触发。
- **三类事故都更适合在本地拦**：BOM（PowerShell `Set-Content`）、CRLF（脚本写 Go 文件没指定 `newline="\n"`）、
  仓库内临时 `.go` 探针（`go build ./...` 会捡起来）——过去它们分别靠 `gofmt -l`、构建报错、
  人肉发现才暴露，现在在 `pre-commit` 一次拦下。
- CI 另有一套（`.github/workflows/ci.yml`，见 [`.github/AUTOMATION.md`](../.github/AUTOMATION.md)）：
  push/PR 到 `main`/`gui` 时跑 gofmt、build、vet、全量测试、GUI 前端用例、race 与发布包白名单。
  本地钩子是"快而窄"，CI 是"慢而全"，两者不重复同一件事。

## 约定

- 长任务按"一片一个 commit"推进（主题清晰、可独立回滚）；`pre-commit` 只做机械检查，不判断主题。
- 钩子只读暂存区与工作树，除 `post-commit` 的构建（写 `dist/`）外不产生副作用。
- 跳过钩子必须写清理由（`SEELEX_SKIP_HOOKS=1 git commit ...`），别默默绕。
