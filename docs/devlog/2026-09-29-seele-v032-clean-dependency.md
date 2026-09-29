# Seele v0.3.2：去掉临时本地 replace，回到纯净依赖

日期：2026-09-29
范围：`go.mod` / `go.sum`（`vendor/` 不入库，根 `.gitignore` 只忽略仓库根的那一份）

## 0. 一句话

方案 B（回合闸门 + 短临界区工作状态）已随 **Seele v0.3.2** 发 tag，宿主对它的迁移也已完成，
因此第三次联调用的临时 `replace github.com/RedHuang-0622/Seele => ../Seele` 到此结束：`require`
升到 `v0.3.2`，删掉 replace，`go mod tidy && go mod vendor`，回到纯净依赖。

## 1. 版本链（go.mod 注释同步更新）

- v0.3.1 = Linux 式权限模型（主体×路由组×rwx + sudo 与中间件判定）+ `session.InLoop` 环内历史把手；
- **v0.3.2**（2026-09-28，tag `v0.3.2` → 提交 `42b4807`）= 方案 B：以「回合闸门 + 短临界区工作
  状态」替换 InLoop 把手，`session/inloop.go` 整条删除，History 永不阻塞、回合内写历史经检查点
  排队。宿主必须跟着迁移，否则编译不过（旧把手不在）——迁移已随本仓此前的锁面修复批次落地。

三次本地 replace 联调（2026-09-15 权限模型、2026-09-26 InLoop、2026-09-28 方案 B）都在对应 tag
发布后移除，这是第三次，也是当前版本链的收尾。

## 2. 改法

```diff
-	github.com/RedHuang-0622/Seele v0.3.1
+	github.com/RedHuang-0622/Seele v0.3.2
…
-// replace 联调（临时）：Seele 工作树里的方案 B …尚未发 tag。
-replace github.com/RedHuang-0622/Seele => ../Seele
```

`go.sum` 随之换成 v0.3.2 的两行（`h1:` 与 `/go.mod h1:`）。`go mod tidy` 只动 Seele 一处，
其余依赖集合不变。

## 3. 两个坑（下次照做）

- `go mod vendor` 在 workspace 模式下被拒（仓库根有 `go.work`，`use (.)`）：
  `go: 'go mod vendor' cannot be run in workspace mode` ⇒ 用 `$env:GOWORK="off"; go mod vendor`，
  或改用 `go work vendor`（本仓只有单模块，前者更贴切）。
- 模块缓存里可能只有 `v0.3.2.mod` 而没有 `.zip`（`go list -m -json …@v0.3.2` 会成功但真跑构建
  才拉 zip）：先 `go mod download github.com/RedHuang-0622/Seele@v0.3.2` 把 zip 落到
  `$GOMODCACHE/cache/download/.../@v/`，再做 tidy/vendor。

## 4. 验收

- `go build ./...` ok（无 replace，vendor 内容由 `v0.3.2` 的 zip 生成，
  `vendor/modules.txt` 首行为 `# github.com/RedHuang-0622/Seele v0.3.2`，无 `=>` 行）；
- `go test ./seelexctx/... ./sessionstore/... ./seelebridge/ -count=1` 全绿；
- `go test ./application/core/... -count=1` 全绿。

## 5. 边界

- 本轮只做依赖与注释，不改 Seele 侧语义；上游若要改 `session` 的回合闸门行为，宿主侧只需跟着
  它的 tag 走，不再需要本地工作树联调。
- `../Seele` 工作树仍是 Seele 的开发树（`v0.3.2` tag 处、工作区干净），但它不再被本仓引用。
