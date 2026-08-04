# 测试报告

## 概览

| 套件 | 结果 | 关键指标 |
|---|:---:|---|
| Go 全量构建/静态/测试 | ✅ | 普通 build、GUI production-tag build、`go vet ./...`、`go test ./...` 全通过 |
| Windows race | ✅ | 4 个关键包，`-race -count=3` 全通过 |
| Frontend Node tests | ✅ | 57/57 通过 |
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
- 2026-08-04 的 `full_access` 复验环境为 Windows/amd64 且 `CGO_ENABLED=0`，机器未安装 `gcc`，因此新增全链路用例本轮无法再次执行 `-race`；原关键包 race 结果保留有效，本轮用 `-count=20` 重复全链路和 `-count=10` 重复 Bridge/工具定向测试补充验证。
- 首次把全量 test、vet 和两种 build 同时并行时，Windows linker 因系统提交限制触发 `VirtualAlloc errno=1455`；改为 `GOMAXPROCS=2`、`-p 1` 串行后，全量测试与 GUI production-tag build 均通过，确认不是代码或断言失败。

## full_access 工具事件全链路复验

| 链路 | 结果 | 证据 |
|---|:---:|---|
| 真实 `Runtime → Session → bash → ToolHookBridge → Application` | ✅ | 本地 mock provider 在第二次模型请求阻塞期间已经收到 `tool.completed`；工具结果进入下一次 provider 请求，最终回复进入 Conversation |
| 真实账号流式请求 | ✅ | opt-in `TestManualSmokeRealAccountBashFullChain`，`bash(pwd)`、完成事件、最终 idle 全通过，耗时 2.83s |
| Application EventHub → GUI Bridge | ✅ | 主事件与子代理事件 relay 定向测试各重复 10 次通过 |
| `seelex:event → reducer → tool card` | ✅ | 新增主代理工具事件 mock；OUT 渲染真实 JSON、状态为 success，明确断言不再出现 `Waiting for output…` |
| Wails runtime 延迟就绪 | ✅ | 未就绪时不静默跳过；就绪后只绑定一次，ready snapshot 异常进入前端错误边界 |

```text
go test . -run '^TestFullAccessBashToolCompletionReachesApplication$' -count=20 -timeout=60s
$env:SEELEX_SMOKE_ACCOUNTS=(Resolve-Path config/accounts.yaml).Path
go test -tags manualsmoke . -run '^TestManualSmokeRealAccountBashFullChain$' -count=1 -timeout=3m -v
go test ./gui -run 'TestBridgeRelays(ApplicationEvents|SubagentToolEventsToSeelexEvent)$' -count=10 -timeout=60s
node --test gui/frontend/dist/*.test.mjs
```

## 综合判断

- [x] ✅ 功能、集成和并发验证通过
- [x] ⚠️ 记录包级覆盖率与既有格式化债务，不阻塞本次交付

## GUI Bridge permission 增量验收（2026-08-04）

本轮最终命令结果以提交前重新执行的数据为准；验收边界固定为 GUI 后端端口，不以 Application/EventHub 单层测试替代：

```text
go test ./gui -run 'TestGUIBackend(PortRelaysToolCompletion|FullAccessReleasesPendingApproval)' -count=10
go test ./application/approval ./seelebridge -run 'ResolveAll|PermissionGateReportsFullAccessState' -count=10
node --test gui/frontend/dist/*.test.mjs
```

最终复验结果：GUI `Bridge` 工具完成与 FA 解锁用例 `-count=10` 通过；Approval/permission 定向竞争用例 `-count=10` 通过；前端 Node 57/57 通过；`go test ./... -p 1 -count=1 -timeout=120s`、`go vet -p 1 ./...`、普通 `go build -p 1 ./...` 和 `go build -p 1 -tags "gui,desktop,production" ./...` 均通过。Windows 本机 `CGO_ENABLED=0` 且未安装 GCC，新增路径本轮未声明为 `-race` 验证；以重复定向并发用例补充覆盖。

后续运行期复现补强：完整套件首次正确捕获 Git Bash 的 POSIX 路径显示与原 Windows 绝对路径断言不一致，已把断言收敛为 project basename；这证明命令已经成功执行而非卡住。修正后 `go test ./... -p 1 -count=1 -timeout=120s` 全绿；新增 `PermissionAutoApproval` 和 GUI Bridge 测试重复 10 次全绿，Windows scoped `bash` 的 `pwd && ls -la` 重复 5 次全绿。
