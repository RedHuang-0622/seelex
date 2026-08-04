# 测试报告

## 概览

| 套件 | 结果 | 关键指标 |
|---|:---:|---|
| Go 全量构建/静态/测试 | ✅ | 普通 build、GUI production-tag build、`go vet ./...`、`go test ./...` 全通过 |
| Windows race | ✅ | 4 个关键包，`-race -count=3` 全通过 |
| Frontend Node tests | ✅ | 54/54 通过 |
| 覆盖率 | ⚠️ | 受影响四包整体 74.6%；lifecycle 83.3%、core 75.7%、bridge 71.9%、gui 79.8% |
| Plan benchmark | ✅ | median 153090 ns/op，约 69902 B/op、597 allocs/op；无历史基线可比较 |

## 各维度

| 维度 | 结果 | 证据 |
|---|:---:|---|
| 单元/边界 | ✅ | Actor close 后 Snapshot、Pipeline 原有存储计数、窗口截断、完整历史合并、递归 reducer |
| 集成 | ✅ | Runtime callback → Application/EventHub、GUI Bridge relay、frontend `event-chain.test.mjs` |
| 并发 | ✅ | `go test -race ./seelexctx/lifecycle ./application/core ./seelebridge ./gui -count=3 -timeout=300s` |
| 性能/内存 | ✅ | 10k lifecycle mock、批次 flush 次数、Plan benchmark；Snapshot/DOM/管道均有界 |
| 静态 | ✅ | `go vet ./...`、两种 build、所有 frontend JS `node --check`、`git diff --check` |
| 安全 | ✅ | 前端工具证据 escape；新增 diff 无密钥；Bridge 不改写/扩散敏感 payload |
| 模糊/泄漏 | — | 本次采用标准层；未执行深度 fuzz/soak/FD 检测 |

## 关键命令

```text
go build ./...
go build -tags "gui,desktop,production" ./...
go vet ./...
go test ./... -count=1 -timeout=120s
go test -race ./seelexctx/lifecycle ./application/core ./seelebridge ./gui -count=3 -timeout=300s
node --test gui/frontend/dist/*.test.mjs
go test ./seelebridge -run '^$' -bench '^BenchmarkPlanLoadSmoke$' -benchmem -benchtime=1s -count=3
git diff --check
```

## 非阻塞说明

- 通用 `test-suite` 建议标准层包覆盖率 ≥85%，但当前数字是整个历史包的 statement coverage，不是本次 diff coverage；新增关键边界均有直接测试，因此不作为功能阻塞项。
- `gofmt -l .` 仍列出 5 个本次变更前已存在、且不在当前 diff 中的文件；所有本次修改 Go 文件均已 gofmt。
- `govulncheck` 当前环境未安装；本次未引入新第三方依赖。

## 综合判断

- [x] ✅ 功能、集成和并发验证通过
- [x] ⚠️ 记录包级覆盖率与既有格式化债务，不阻塞本次交付
