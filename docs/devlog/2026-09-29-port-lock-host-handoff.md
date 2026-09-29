# 锁面普查批次 2：`port.mu` 内的宿主调用（PrepareHistory）

> 日期：2026-09-29
> 范围：`internal/adapters/{engine_port.go,README.md}`
> 用例：`internal/adapters/engine_port_handoff_test.go`（新增 4 个）
> 关联：[锁面普查：别处还有没有自锁/死锁](2026-09-29-lock-audit-other-loci.md) §2.7（本批的输入）、
> [锁面普查的修复批次](2026-09-29-lock-audit-fixes.md)（上一批；本批是它 §4「未做·第 3 条」的第一项）、
> [装配层折叠持 ViewMu 推帧](2026-09-29-compaction-fold-lock-granularity.md)（同一纪律的母本）

## 1. 本批修了什么

审计 §5 的「建议顺序」里上一批落了 1、2、3、5 与 4 的一半（`async_exec.go`），**4 的另一半
（`internal/adapters/engine_port.go` 在 `port.mu` 内调宿主 `prepareHistory`）**是本批：

| 审计条目 | 改动 | 用例 |
|---|---|---|
| §2.7 `port.mu` 内调宿主注入实现 `prepareHistory` | 三条安装路径 + 回合出口一律拆成「锁内决定、锁外调用」两段：新增 `historyHandoff` / `armHandoffLocked` / `runHandoff`；`ReplaceRawHistory`、`replaceRawHistoryFor`、`ResumeRawSession` 的体内逻辑搬进 `*Locked` 助手（只改注册表 + arm 交接），`installSessionEngineLocked`/`installPendingLocked` 改为**返回**待交接 | `engine_port_handoff_test.go` ×4 |
| §2.7 附带：锁内决定与锁外调用之间的窗口 | 交接带按会话的序号闸：本次决定已被更晚的一次取代时丢弃（否则 durable 的 next-load 被拉回过期历史） | `TestSupersededHandoffIsDropped` |
| README 口径与实现不符 | `internal/adapters/README.md` 不变量从三条扩到四条，并把「宿主注入实现只在 `port.mu` 之外被调」写进 Review 指南 | — |

为什么这不是「性能优化」而是缺陷：`prepareHistory` 的生产装配是
`Runtime.PrepareMainSessionHistory`（`bundlesMu.RLock` → `binding.mu.RLock` →
`DurableHistory.PrepareNextLoad`）——**另一端有锁、有落盘**。在 `port.mu` 内调它有两层后果：
慢活计进全进程端口锁（每次折叠/恢复都把宿主链路按在 `port.mu` 里，所有会话的查表、开回合、
读历史全排后面），以及宿主实现一回读端口就同步重入非重入锁 = 永久挂死（本仓 2026-09-23 /
2026-09-29 那两起就是这个形）。

## 2. 有牙证明（把修复挡回去，用例立刻红）

```text
$ go test ./internal/adapters/ -count=1 -run 'TestPrepareHistory|TestSuperseded|TestHandoffReachesHost|TestPendingInstall'   # 修后
ok  github.com/RedHuang-0622/seelex/internal/adapters  0.272s   # 4 个用例 + 3 个子用例全 PASS

# 反向护栏 ①：把 runHandoff 挪回 port.mu 之内（mutation: 在 replaceRawHistoryFor 里先交后解锁）
$ go test ./internal/adapters/ -run 'TestPrepareHistoryIsCalledOutsidePortLock' -count=1 -timeout 30s
--- FAIL: TestPrepareHistoryIsCalledOutsidePortLock (2.00s)
    engine_port_handoff_test.go:91: 对空闲会话做替换（宿主回读端口） 在 2s 内没返回（= 持着 port.mu 等别人的会话锁）
# 换成同步路径（ReplaceRawHistory）同样配方：测试 goroutine 直接卡在
#   EnginePort.runHandoff → sync.RWMutex.RLock  ← ReplaceRawHistory
# 直到 -timeout 触发——即「宿主回读端口」这一条被复现成永久等待。

# 反向护栏 ②：关掉序号闸（mutation: latest := true）
$ go test ./internal/adapters/ -run 'TestSupersededHandoffIsDropped' -count=1
--- FAIL: TestSupersededHandoffIsDropped (0.00s)
    engine_port_handoff_test.go:125: 过期交接没被丢弃（durable 会被拉回旧历史）：
      [{sessionID:sess-other history:SECOND} {sessionID:sess-other history:FIRST}]
```

四条判据分别是**返回值/预算**（宿主回读端口还返不返得回来）、**副作用顺序**（宿主收到的历史
列表长什么样）、**次数**（三条安装路径各兑现一次，不多不少）、**回合出口**（待安装在计数归零
时同样走到锁外）——都不是「读代码看着对」。

## 3. 验收（本机 2026-09-29）

| 命令 | 结果 |
|---|---|
| `go build ./...` | exit 0 |
| `go vet ./internal/adapters/ ./seelebridge/ ./application/core/` | 无输出 |
| `gofmt -l`（本批改动文件） | 空 |
| `go test ./internal/... ./seelebridge/... ./application/... . -count=1` | 全 ok（见下条备注） |
| `go test -race ./internal/adapters/ ./application/core/ ./seelebridge/ -count=1` | ok 1.6s / 41.1s / 16.9s |

备注（一次性抖动，已复核）：第一次跑上面那条全量命令时根包
`TestBackgroundCompletionWhileSwitchingToC` 在 `ctx` 60s 上超时失败。复核：该用例单独跑
0.75s ok、`-count=3` ok、把同一条全量命令原样重跑两次都 ok（根包 14.9s）。判为首次编译 +
40 余包并发跑测的机器争用，与本批改动无关（本批只在 `internal/adapters`，且该用例的失败形态
是 60s 预算耗尽，不是握手顺序）。

## 4. 未做 / 已知边界

1. **本批只覆盖「宿主注入实现」这一类**：`port.mu` 内不能再出现任何 `EnginePortDeps` 注入的
   函数调用。其余锁面无变化（引擎的历史读写仍是原来的「短临界区 + 检查点队列」形状）。
2. **序号闸是按会话的，不是按端口的**：同一会话的两次交接按序保留最后一次；跨会话互不干扰。
   若将来出现「同一个会话的交接必须严格按调用顺序落地」的需求（今天没有），要另立判据。
3. **审计 §5 剩余条目仍未动**（本批只做了 §2.7）：`sessionstore/session_context.go:811`
   （`s.mu` 内 `PushCompact` 落盘）、`sessionstore/event_store.go`（`store.mu` 内整文件重写）、
   `application/event/hub.go`（谓词内 actor 往返）、`sessionstore/runtime_api.go`（15 个直连入口
   不登记 `activeOps`）、`seelebridge/runtime.go:516-522`（纯读取写锁）。
4. **ABBA 的另一半**（`Supervisor.RunEval`/`runRoundLocked` 持 `s.mu` 跑评估回合）与
   「回合内自造 ctx 绕过在飞标记」仍是上一批 devlog §4 的边界，属独立批次。
5. **未跑真实 API / 长时压测**：本批判据全在确定性用例与 `-race` 上（`-race` 覆盖
   `internal/adapters`、`application/core`、`seelebridge` 三个包，不是全仓）。
