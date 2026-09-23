# A3 写者单写者化核实 + A4 草稿尾恢复应用侧装配 + C1/H3 提交成本收口（2026-09-23）

> 范围：把打点表 §7 的 A3（写者 actor 化 + C1/C2 前置）与 A4（应用侧草稿尾恢复入口）
> 做到可验收，并量化 C1 的 30 ms 门；顺带回答「`-tags redprobe` 失败项是构建还是运行失败」。
> 证据口径：所有数字都来自本仓库探针（`-tags lockprobe`）与测试的真实输出，命令见文末。

## 1. A3「写者 actor 化」：机制已在库里，本次做的是核实与旁路清点

**事实（逐条带证据）**：8 个写调用点**全部**在同一条 per-session 单写者串行链上——
`Coordinator.RunSessionWrite`（`application/core/session_runtime/coordinator.go:288`，
底层 `writers *SessionTransitionManager`：per-key actor，channel + 单 goroutine，无共享
mutex，见 `transition_manager.go:38`）。7 处经 `PersistCurrentSession`
（`archive.go:34-41` 内部就包着 `RunSessionWrite`）：`chat.go:271`、`content_lru.go:194`、
`resident_lru.go:145`、`session_draft.go:56`、`session_lifecycle.go:148`、
`archive_session.go:67`、`context_runtime/coordinator.go:118`（压缩 checkpoint）；第 8 处
`session_fork.go:122` 在调用点显式包住子会话首落盘。

机制面的验收已经存在且全绿：`session_runtime/session_writer_test.go` 四例——同会话 FIFO
串行（并发峰值 = 1）、跨会话并行、**读路径不被写者阻塞**（C2）、关闭后退化为直通不卡死。

**本次新增的是「旁路清点」结论**（此前没有人做过，也是"被误标完成"的根源）——同一条链
之外的写点：
| 旁路写点 | 位置 | 性质 | 结论 |
|---|---|---|---|
| `SaveContextStateWorkspace(childID)` | `session_fork.go:127`（写者临界区**之后**） | fork 子会话的 context state 与快照不同锁 | 未收口（低风险：子会话此刻无其他写者） |
| durable queue 的 mark/confirm/fail 写 | `chat.go:401` 等（persist 返回**之后**） | 以"该轮是否已发布"为凭据，天然要求「先发布后确认」 | 顺序正确；但**不**与发布同锁 → 崩溃窗口里最多把已消费项留在队列（可见、不丢），设计如此 |
| 逐步草稿尾 `appendDraftRows` | 只有 `draft_append_test.go` 调用 | 应用侧尚无 per-step append 调用方 | 端口能力就绪、**应用侧未接线**（A2 残留） |
| LRU 删除 / compact / retention 重写分片 | `sessionstore` 内，持 `messageMu` | 与提交同模块锁 | 已串行 |

## 2. `-tags redprobe` 失败项：**运行期失败，不是构建失败**

- 构建面：`go vet -tags redprobe ./sessionstore` 退出码 0（构建没问题）。
- 运行面：`go test -tags redprobe ./sessionstore` 曾 **FAIL**，失败项是
  `gap_probe_test.go` 的 `TestProbeStackStatusUpdateUnpublishedInvisible`——它钉的是
  「未发布的状态迁移冷重载后必须不可见」，前提是把 `active.jsonl` 当追加型通道、用
  戳号做可见性闸门。该口径已按 D12/§2.0 通道类型表修订为**整份替换型**（文件不存在
  「已 append 未发布」中间态，戳号只是标签），于是这个探针在钉一个**已被撤销的口径**，
  长期红灯（打点表 §5 H9 写成"转成 T-STK-13 的语义断言"，但实际没转）。
- 处置：退役该探针（保留说明与出处），语义断言由**既有的常规测试** T-STK-13
  `channel_semantics_test.go:104`（`TestStackChannelActiveWholeReplacement`）承担。
- 复验：`go test -tags redprobe ./sessionstore -count=1` → **ok 46.4 s**（`redprobe` 回到
  "回归探针"语义：绿即契约成立）。

## 3. C1/H3 量化：打点表对成本结构的判断**是错的**

新增探针 `sessionstore/c1_publish_probe_test.go`（`-tags lockprobe`）把提交路径逐环拆开。
1500 行 / 每行 8 KB / 每片 100 行（末分片 834 KB）实测：

| 环节 | 实测 | 说明 |
|---|---|---|
| 整份 head 发布（Marshal payload + 校验和 + MarshalIndent + 临时文件 + rename） | **1.3–1.6 ms/次** | C1 说它是主成本 → **不成立**（约占提交 5%） |
| 分片**整片读回 + JSON 解码**（writeShardRowsLocked） | **15–27 ms/次** | 真正的成本大头 |
| 分片**整片 sha256** | 2.5–3.3 ms/次 | |
| append（含 `file.Sync()`） | 1.2–1.5 ms/次 | |
| head 体积 = f(分片数) | 826 B@1 片 → 4171 B@15 片 → 19836 B@80 片（≈247 B/片，**线性**） | 线性成立，但绝对量小 |

并且提交路径把「整片读回 + 解码」做了**两遍**：`reapUnpublishedLocked` →
`truncateMessageRowsAfter` 一遍、`writeShardRowsLocked` 一遍。

**本轮的优化（写侧免整片解码）**：
1. `shardInfo` 增加 `Bytes`（写入时记账文件字节数，随 head 原子发布）；
2. `shardTailTrust`：`Bytes` 已记账 + 文件当前字节数一致 + 以换行收尾 ⇒ 索引可信；
   再加上「索引末行的坐标 == 发布点」（`trustedTailFor`）；
3. 可信时：行数/区间直接取索引；摘要改**增量**计算
   `sha256(写入前原始字节 ++ 本次追加字节)`（仍覆盖整片，语义不变）；
4. `truncateMessageRowsAfter` 同判据下直接返回（无崩溃残尾、无未发布行）；
5. 任一不成立 → **退回整片读回的慢路径**：唯一失效模式是"慢"，不会错。

**前后量化**（同一次运行内的 A/B，关掉可信开关 = 退回改动前；本机文件打开成本高且噪声大，
只有同轮 A/B 可信）：
- 整片解码次数：**优化前 3 次/提交 → 优化后 1 次/提交**（100 行/次提交的场景）；
  1 行/次提交的场景为 **2 → 0**；
- 提交耗时（100 行/次，中位数）：**96.6 ms → 88.0 ms（省 8.6 ms）**；
- 结构面而非耗时面才是判据：`TestCommitSkipsTailShardDecode` 直接钉"提交时不发生整片解码"
  （可信索引下 0 次；抹掉 `Bytes` 记账后立刻 ≥1 次——有牙）。

**新发现（未修，列为待办）**：`openSharedRead`（C2 为 Windows 共享位引入的读原语）比
`os.Open` **慢 4–12 倍**（实测 678–1024 µs vs 80–233 µs，同一文件、同轮交替）。
它同时被 C2 的读路径与本次的可信判定使用，是"读路径解耦之后新出现的固定成本"，
值得单独归因（首要嫌疑：`os.NewFile` 对同步句柄的 poller 初始化）。

## 4. A4：草稿尾恢复从"端口能力"变成用户看得见的行为

应用侧此前**零调用方**（`RecoverPendingMessageTail*` 只存在于 `sessionstore` +
`internal/adapters`）。本轮装配：

- 端口：`session_runtime.SessionPendingTailPort`（可选能力，照 `SessionQueuePort` 范式）
  + `dto.PendingMessageTailReport`（跨层纯 DTO；适配器负责映射，应用层不依赖 sessionstore）；
- 决策：`application/core/session_pending_tail.go` 的
  `recoverPendingMessageTailAtLoad`——`clean` 静默／`recoverable` 显式恢复／`gap` **只报告**
  （红线 3：不发布、不清理、不猜测）；
- 时序：接在 `resumeSessionCold` **三读之前**（可见性闸门是发布点，先恢复再读，恢复出来的
  行才能进本次加载的可见会话）；
- 运行中不抢发布点：批次在跑的会话（`ChatState.Running`）连探测都不发（避免回到
  "两个写者互相 reap 草稿"）；
- **用户可感知**：结论作为一条 `system` 行进可见会话（
  "Recovered 2 uncommitted row(s) from before the interruption (publish point 7 → 9)."
  / "…cannot be recovered safely… The rows are kept on disk…"），走既有 `message.added`
  发布通道，前端与 GUI 无需新增契约。

验收：`application/core/session_pending_tail_test.go` 五例——恢复并可见（含端口调用序列
`[probe recover]` 与发布点位移文案）、gap 只探测不发布、clean 静默、运行中不触碰、
能力未装配时静默退回。

## 5. 验证记录（本机、本轮）

```
go build ./...                                      → ok
go vet ./...                                        → 干净（顺带修掉 A1 遗留的
                                                      durable_queue_wire_test.go 按值内嵌锁）
gofmt -l sessionstore application internal gui      → 干净
go test ./application/core -count=1                 → ok 8.3 s
go test -race ./sessionstore -count=1               → ok 57.0 s（无竞争报告）
go test . -count=1                                  → ok 17.0 s（根包全链路）
go test ./session/... ./tui/... ./gui/... ./e2e/...  → 全 ok
go test -tags redprobe ./sessionstore -count=1      → ok 46.4 s（此前运行期 FAIL）
go test -tags lockprobe ./sessionstore -run TestProbeCommitCostBreakdown -v
                                                    → 见 §3 表格（改前/改后同轮 A/B）
```

## 6. 仍未做（交接清单）

| 项 | 状态 | 依据 |
|---|---|---|
| 逐步草稿尾的**应用侧**接线（per-step append；A2 残留） | 未做 | `appendDraftRows` 仍只有测试调用方 |
| `SaveContextStateWorkspace`（fork 子会话）纳入单写者 | 未做 | `session_fork.go:127` 在写者临界区之外 |
| C1 的 head 体积与发布次数（格式/次数层面） | 未做 | head 发布实测仅 1.3–1.6 ms，优先级低于 H3；体积线性已量化 |
| 提交路径剩余的 1 次整片解码（慢路径回落点） | 未定位 | 需在写入器里逐分支计数 |
| `openSharedRead` 比 `os.Open` 慢 4–12× | 已量化、未归因/未修 | §3 末段 |
| fsync 策略（每步 append 是否 sync） | 未决 | 设计稿未拍；影响"断电可恢复 vs 写放大" |
| L3 在飞子代理"标 partial"的键空间漂移 | 部分成立 | `subagent_tree.go` 的 running/queued → interrupted 已实现且冷加载走到；`a5d36b4` 的复现用例钉的是"读不到 = 0 条"的缺陷态（当前绿），漂移面未收口 |
