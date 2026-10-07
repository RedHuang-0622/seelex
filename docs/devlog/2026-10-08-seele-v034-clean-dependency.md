# Seele v0.3.4：去掉临时本地 replace，回到纯净依赖

日期：2026-10-08
范围：`go.mod` / `go.sum`（`vendor/` 不入库，根 `.gitignore` 忽略仓库根那一份）

## 0. 一句话

第四次联调（2026-10-05 起，为验证「`reasoning_effort` 上到 wire」）的临时
`replace github.com/RedHuang-0622/Seele => G:/program/go/seele` 到此结束：Seele 已发
**v0.3.4**（2026-10-08，tag `v0.3.4` → 提交 `ecca676`，`release: v0.3.4 — api
reasoning_effort 真正上到 wire 且运行期可切换`），`require` 升到 `v0.3.4`，删掉 replace，
`go mod tidy` + `GOWORK=off go mod vendor`，回到纯净依赖。

## 1. 版本链（`go.mod` 注释同步更新）

- v0.3.1 = Linux 式权限模型（主体×路由组×rwx + sudo 与中间件判定）+ `session.InLoop` 环内历史把手；
- v0.3.2（2026-09-28）= 方案 B：以「回合闸门 + 短临界区工作状态」替换 InLoop 把手，`session/inloop.go`
  整条删除，History 永不阻塞、回合内写历史经检查点排队（见 `2026-09-29-seele-v032-clean-dependency.md`）；
- v0.3.3（2026-10-03，`2e4a7d4`）= 异步作业根能力（作业契约 + Manager + `jobs_manage`）；
- **v0.3.4（2026-10-08，`ecca676`）= `reasoning_effort` 真正写进请求体**（`agent/core/api` 下
  openai / anthropic 两个 strategy 各自落字段），**且运行期可切换**（`94b5d0a`
  `ChatClient` 支持切换）。宿主侧 `application/contract/ports.go` 的 `ReasoningEffortPort`
  与 `application/core` 的档位下发链路要的就是这条能力。

## 2. 改法

```diff
-	github.com/RedHuang-0622/Seele v0.3.3
+	github.com/RedHuang-0622/Seele v0.3.4
…
-// ── 本地联调 replace（临时态，2026-10-05 起）─────────────────────────────
-// …（整段删除）
-replace github.com/RedHuang-0622/Seele => G:/program/go/seele
```

`go.sum` 随之把 Seele 收敛成 v0.3.4 两行：删掉 `v0.3.2`（`h1`+`/go.mod`）与 `v0.3.3`
（`h1`+`/go.mod`）四行，加入 `v0.3.4 h1:mqxvVxs11hKiTbJiP/EvbpYHsHSxq9Do0MgrzXRq4XA=` 与
`v0.3.4/go.mod h1:GG1DAQvKcjSldXzKllqodLiCghK+GhzEkNZZKfqmLUM=`。`go mod tidy` 只动 Seele
一处，其余依赖集合不变。

`vendor/modules.txt` 首部 Seele 行由 `v0.3.3 => G:/program/go/seele` 变为
`# github.com/RedHuang-0622/Seele v0.3.4`（**无 `=>`**）。

## 3. 两个坑（v0.3.2 那篇记过，本轮复核仍成立）

- `go mod vendor` 在 workspace 模式下被拒（仓库根有 `go.work`，`use (.)`）：
  `go: 'go mod vendor' cannot be run in workspace mode` ⇒ 用 `$env:GOWORK="off"; go mod vendor`。
  注意 **module 模式的 `vendor/` 与仓库根的 `go.work` 并不冲突**：本轮实测默认 workspace 模式下
  `go build ./...` 直接通过，不需要为构建也关掉 GOWORK。
- 先 `go mod download github.com/RedHuang-0622/Seele@v0.3.4` 把 `.zip` 落到
  `$GOMODCACHE/cache/download/.../@v/`，再 tidy/vendor（只有 `.mod` 没有 `.zip` 时 `go list -m`
  会成功但真跑构建才拉 zip）。

## 4. 验收（本轮真实读数）

- `go mod tidy` → exit 0；`go mod vendor`（GOWORK=off）→ exit 0；
- `vendor/modules.txt` Seele 行为 `v0.3.4` 且全文件无 `=>`；
- `go build ./...` → **exit 0**；
- `go test ./... -count=1 -timeout 600s` → **exit 0**：**73 个包 `ok`、11 个 `[no test files]`、
  0 个 FAIL**；关键包耗时读数：`seelex`(root) 41.4s、`seelebridge` 56.6s、
  `seelebridge/worktree` 46.2s、`sessionstore` 44.3s、`application/core` 28.1s、
  `workspace` 25.4s、`seelebridge/tools` 21.9s。

## 5. 边界

- 本轮只做依赖与文档，不改 Seele 侧语义；本地 Seele 工作树（`G:/program/go/seele`）在工作区干净、
  `git describe --tags` = `v0.3.4`，即**摘 replace 不引入任何未发布语义**（replace 目标内容与本轮
  采用的 tag 等价），差异纯粹是「本机绝对路径 → 可移植 tag」。
- 同步更新的陈旧口径：`README.md`（版本链）、`AGENTS.md`（§1 依赖边界）、`seelebridge/README.md`、
  `sessionstore/README.md`。`stage/seelex-v0.1.1-*` 下是历史发布快照，不追改。
- `go.work` / `go.work.sum` / `vendor/` 均被 `.gitignore` 忽略，属本机派生物；`go.work.sum` 里仍留
  有更早版本（v0.1.3 / v0.3.1）的 Seele 校验行，对本轮两种模式（workspace / GOWORK=off）的构建与
  测试均无影响。
