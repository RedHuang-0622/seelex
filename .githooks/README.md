# .githooks —— 本仓库的提交/推送触发安排

启用方式（本仓库已设置）：

```text
git config core.hooksPath .githooks
```

## 钩子一览

| 钩子 | 何时触发 | 做什么 | 怎么跳过 |
|---|---|---|---|
| `pre-commit` | 每次 `git commit`（写对象之前） | **快速护栏**：暂存区里的 `.go` 文件必须 `gofmt` 干净、不得带 UTF-8 BOM 或 CRLF；不得暂存 `tmp/`、`_tmp/`、`_scratch/`、`_logs/`、`dist/`；不得暂存真实配置（`config/accounts.yaml`、`config/*.local.yaml`、`config/mcp.yaml`、`config/search_engine.yaml`）。空白错误只提示、不拦 | `SEELEX_SKIP_HOOKS=1`（会打印原因） |
| `post-commit` | 每次 `git commit` 之后 | **按需重建 dev 二进制**：只有本次提交动了"构建相关路径"才跑 `scripts/build-dev.sh`——判据是"产物会变"：非用例的 `.go`（**排除** `test/`、`e2e/`、`*_test.go`）、`go.mod`/`go.sum`、`config/*.yaml`、`scripts/*`、`plugins/*`、`Makefile`。纯文档提交与**纯用例提交**直接跳过（省下两个 30MB 二进制的构建，也不去动 `dist/` 里的数据） | `SEELEX_POST_COMMIT_BUILD=0` 强制跳过；`=1` 强制总是构建；`SKIP_BUILD=1` 仍然等价于跳过（向后兼容）；`SEELEX_BUILD_COMMAND=…` 只给自测替换构建命令 |
| `pre-push` | 每次 `git push`（发数据之前） | `go build ./...` + `go vet ./...` 必须过；不过就拒绝推送（防止把红的树推上去）。**找不到 Go 工具链时显式拒绝**并给出去向（把 Go 加进 PATH），而不是打一句 `command not found` 的糊涂红 | `SEELEX_SKIP_HOOKS=1` |

## 工具链解析（2026-10-16 补）

钩子在"精简 shell / GUI git 客户端"里跑时，PATH 里常常没有 Go。旧写法直接调 `gofmt`，
`2>/dev/null` 会把 `command not found` 一起吞掉——**格式检查看起来通过，实际从未执行**。
现在两个钩子都先解析工具链：

- 先看 `command -v`；再退到 `go env GOROOT`（`$GOROOT/bin/gofmt`）；
- `pre-commit` 两处都找不到时**大声警告"本次未执行格式检查"**（不静默通过）；
- `pre-push` 两处都找不到时**拒绝推送**（跑不了红线 = 不许让这次推送看起来通过）；
- `SEELEX_GOFMT` / `SEELEX_GO` 是给自测替换工具链的旋钮（下节），日常不要设。

这条缺口是自测抓出来的：`bash scripts/hooks-selftest.sh` 的 `no-toolchain` 用例要求
"没有 Go 时必须打印警告"。

## 为什么这么排

- **`dist/` 里有用户数据**（dev GUI 的配置副本、可能的 `.seelex/` 会话——见根 `MEMORY.md` 的 2026-08-12
  事故记录）。触发脚本不该在每次提交时都去碰它，所以 `post-commit` 改成"动了构建相关路径才重建"，
  且**linked worktree 里一律跳过**（`scripts/build-dev.sh` 自带这条判断，给子代理用的工作树不构建）。
- **长任务分支提交密度高**（架构重排动辄几十个 commit/天）：默认策略必须便宜；重活（两个二进制）按需触发。
- **纯用例提交不重建**（2026-10-16，M5-S4 之后可以这么判）：仓库根的 57 个集成用例搬进
  `test/` 包，"改了用例"与"改了产物"在路径上已经能分开——`test/`、`e2e/`、`*_test.go`
  都不进二进制。旧口径（见到 `.go` 就建）在搬迁前是必要的，因为用例与入口源码同层。
  自测：`bash scripts/hooks-selftest.sh`（覆盖 BOM/CRLF/未格式化/禁入路径/真实配置，
  以及 post-commit 的"文档与纯用例跳过构建、源码改动会构建"三类决策）。
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
- 改钩子先跑自测：`bash scripts/hooks-selftest.sh`（hermetic：临时仓库 + 假构建命令，
  不碰本仓库、不构建 30MB 二进制；13 条用例覆盖 BOM/CRLF/未格式化/禁入路径/真实配置
  与 post-commit 的四类决策）。
