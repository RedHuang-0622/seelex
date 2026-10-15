# 性能与可观测性审查：Application → Runtime → Tool/Job → Store/MCP/子进程

- 日期：2026-10-15
- 状态：审查记录 + 提案。**没有实测数据，本文不宣称瓶颈、不给出优化比例**；所有量化目标以 §4 的基准计划产出为准。
- 证据口径：〔源码〕/〔待测〕/〔历史基线〕/〔提案〕，见 [README](README.md)。

---

## 1. 关键路径总览

```
用户输入 (tui/gui/headless)
  → application/core Service（用例、队列、取消）
    → context_runtime.Coordinator（上下文装配/token 预算/压缩）── seelexctx.Assembler
    → view_state.Coordinator（Snapshot 权威、revision）
    → event.Hub（事件发布、订阅、resync）
  → seelebridge.Runtime（EnginePort、会话 bundles、workunit 生命周期）
    → tools/router（同步工具 + 子进程树）｜ async_exec（后台 Job 32/256）
    → mcpstack（MCP 熔断事件 + trace 落盘）→ Seele tools/mcp（stdio 子进程）
  → sessionstore（分片追加 + module head 原子发布 + 结构性事件 fsync 通道）
```

---

## 2. 分段审查：源码可确认的事实

### 2.1 上下文装配（Application/Runtime）

| # | 事实〔源码〕 | 证据 |
|---|---|---|
| A1 | 每轮请求全量重排上下文（system→project→memory→prefix 栈→静态块→WorkingHistory→tail 栈），**无跨轮本地缓存**；命中依赖 provider 侧 prefix cache（「投影 == 已发出字节」约定） | `seelexctx/assembler.go:70-137` |
| A2 | token 估算为启发式（CJK≈1、ASCII≈4 字符/token），`CountHistory` 每次线性扫全量消息 | `seelexctx/tokens/tokens.go:23-42,76-82` |
| A3 | 每轮装配走 `CountRequestTokens`；超预算时 shrink 循环逐级重扫 events（最多 `ContextMaxUnits` 档），N 轮 transcript 下形态为 O(N×档数) | `application/core/context_runtime/coordinator.go:1287,1260-1265` |
| A4 | 前缀复用是设计目标而非本地缓存：累积模式只追加保留段之后的新事件；冷恢复保留段是后缀时回退全量重建 | `application/core/context_runtime/coordinator.go:863-881` |
| A5 | system prompt 有内容缓存（相等则跳过 `SetSystemPrompt`）——少数已存在的装配缓存点 | `application/core/prompt_layer/coordinator.go:52-53,84-88` |
| A6 | 显式压缩前跑**两次全量请求估算**（原始累积 + 引擎缓存峰值），注释自述「可达数百毫秒」 | `application/event/hub.go:144-147` |

### 2.2 序列化与深拷贝

| # | 事实〔源码〕 | 证据 |
|---|---|---|
| B1 | 每次 `Snapshot()` 全量深拷贝：`CloneSnapshot` 逐条拷 Conversation/Tool 指针；`CloneRuntimeState` 克隆 VisibleTools/Skills/Todo/Plan/SubAgentTree/WorkTable/TeamworkBoard | `application/core/view_state/coordinator.go:144-148` → `application/model/state.go:923-961,964-990` |
| B2 | 每条事件发布都 `json.Marshal` 载荷（错误被丢弃） | `application/event/hub.go:352-354` |
| B3 | 流式批次 sink 再拷贝一份批次 | `application/core/chat/stream_batcher.go:99` |
| B4 | fork 会话全量深拷贝 record/events/tool-results/context | `application/core/session_fork.go:171` |
| B5 | DTO 克隆散布多处（TeamworkBoardView、review_request、ClonePlanNode 等） | `application/model/state.go:996`、`application/contract/dto/review_request.go:92`、`application/core/subagent_view/coordinator.go:129` |

### 2.3 事件投影

| # | 事实〔源码〕 | 证据 |
|---|---|---|
| C1 | 投影 = **增量写 + 整份读**：写侧逐条 append + bump；读侧每次全量 CloneSnapshot；缺口/溢出时前端整份重拉（resync） | `application/core/chat/chat.go:783-795`、`application/event/hub.go:424-430` |
| C2 | runtime 投影**每回合尾 + 每次工具边界整份重建**：`CollectRuntimeProjectionFor` 调用十余个端口（Model/VisibleTools/TodoSnapshot/TaskSnapshot/SubAgentTree…）+ 逐字段 append 拷贝；注释自述「每次整份重建 Runtime」 | `application/core/view_state/coordinator.go:177-247,48-50` |
| C3 | revision/seq 语义：全局 Seq + 订阅内 DeliverySeq + Revision；「锁内 bump、锁外 Publish」为既定纪律 | `application/event/hub.go:105-120`、`application/core/README.md:257` |
| C4 | 每批次 flush = 一次全局 bump + 一次 JSON Marshal + 全订阅者投递 | `application/core/chat/chat.go:793-795` |

### 2.4 锁竞争

| # | 事实〔源码〕 | 证据 |
|---|---|---|
| D1 | 进程级单一锁 `state.Core.ViewMu`（RWMutex）保护 Snapshot 与全部 *Locked 域方法；会话单元另有 Unit.mu/View.mu 叶子锁 | `application/core/internal/state/state.go:29-35` |
| D2 | 流式主路径每批 flush 取**全局写锁**（`appendVisibleDelta`/`consumeVisibleChunk` 在 `ViewMu.Lock()` 内做 view mutate + mirror + bump）；后台会话路径才走单元锁零全局锁 | `application/core/chat/chat.go:758-794,711-727,800-833` |
| D3 | `startChatFor` 在 ViewMu 临界区内做标题生成、checkpoint 构建、两次 appendMessage、bump；LLM 调用在锁外（符合 README 纪律「锁不包网络/LLM/长工具调用」） | `application/core/chat/chat.go:62-140,207`、`application/README.md:227` |
| D4 | EventHub 发布持 `publishMu` 串行投递全部订阅者；慢订阅者拉长 publishMu 持锁（投递本身非阻塞，filter/窗口拷贝在锁内） | `application/event/hub.go:359-374` |
| D5 | 已有拆锁先例：压缩推帧从 ViewMu 拆出独立 `compactionPushLock` | `application/core/context_runtime/coordinator.go:80-101` |
| D6 | 历史反面教材已修复并留回归：framework Session 锁曾被 ChatStream 全程持有 | `application/core/README.md:236-241`、`repro_two_running_view_third_then_switched_finishes_test.go` |

### 2.5 队列与背压

| # | 事实〔源码〕 | 证据 |
|---|---|---|
| E1 | 订阅缓冲：GUI 256 / replay 窗口 1024 / headless 4096；溢出策略 = 排空缓冲强投 `resync.required`（整份重拉）或 replay 窗口淘汰后退化重拉 | `gui/bridge.go:289-290,431`、`gui/headless.go:36-38`、`application/event/hub.go:396-431` |
| E2 | StreamBatcher 有界（FlushSize=32、Buffer=128、40ms）；满时 `ErrPipelineFull` → 生产者 `runtime.Gosched()` **无限自旋重试**——不丢数据，但满时烧 CPU 且背压直达 LLM 流回调 | `application/core/chat/stream_batcher.go:46-61`、`chat.go:669` |
| E3 | 后台 Job：硬上限 32 在途 / 256 总槽 / 30min 硬顶；**背压 = 直接拒绝**（无排队，由模型自行重试）；超 256 驱逐最老已完成项；取回 `wait_ms` 默认 5s、上限 60s | `seelebridge/tools/async_exec.go:43-48,308-310,394-420`、`job_run.go:18-46` |
| E4 | worktable 发布器 cap=1 + latest-wins；输入队列是有界语义的 slice（四类转换点共用 ViewMu） | `application/core/worktable/worktable_publisher.go:39,58-81`、`application/core/service_queue.go:11-34` |
| E5 | scheduler 单 ticker（200ms）单循环，到期任务各起 goroutine，**无并发上限、无错过追补** | `seelebridge/scheduler/scheduler.go:24,164-190` |

### 2.6 存储写放大（sessionstore）

| # | 事实〔源码〕 | 证据 |
|---|---|---|
| F1 | 分片**真追加**：只写增量行到物理尾分片（默认 100 行/片），满片滚新文件；提交不做整片解码 | `sessionstore/message_rows.go:682-706`、`shard_write_no_decode_test.go` |
| F2 | **每次提交一次 module head 整份重写**（`publishModuleHead` → `writeAtomic` tmp+rename，renameBackoff 2/5/10/20/40ms 重试）；README 明示 C1（head 每次提交整份重写）与 H3（fileSHA256 整片重算）是**已知未完成项** | `sessionstore/message_rows.go:440`、`module_heads.go:344-374`、`sessionstore.go:1942-1960`、`sessionstore/README.md:121` |
| F3 | 框架事件库为 O_APPEND 单行追加，写侧不做 fsync | `sessionstore/README.md:445-459`、`event_store.go:74-92` |
| F4 | **结构性事件通道逐写 fsync**：structural_events / compaction / retention / role_session / stack_journal 五处每次追加 `file.Sync()` | `sessionstore/structural_events.go:446-487`、`compaction_records.go:213`、`compact_frames.go:169`、`retention.go:212`、`role_session.go:269`、`stack_journal_json.go:380` |
| F5 | 每次 Chat 前 Load、结束后 Save；崩溃残尾按 1MiB 块反向截半行 | `sessionstore/durable_history.go:34-35`、`jsonl_io.go:10-43` |

### 2.7 MCP 历史与自动保存

| # | 事实〔源码〕 | 证据 |
|---|---|---|
| G1 | `mcpstack.Record()` 末尾 `autoSave()` = 整份 `json.MarshalIndent` + tmp+rename，**全程持 `s.mu` 写锁**；`AfterCall` 完成时再写一次 → 一次被记录的调用 = 2 次全文件重写 | `mcpstack/stack.go:149-170`、`persist.go:46-64` |
| G2 | 生产链路只记录熔断事件（BeforeCall/AfterCall 仅测试使用）：每次熔断状态跳变一次全量重写 `mcp-traces.json`（单文件、进程级、非按会话分片）；Calls 只 append 无上限 | `mcpstack/interceptor.go:92-105`、`breaker.go:18-51`、`seelebridge/runtime.go:332-338` |
| G3 | MCP server 进程 stdio 启动；**无自动重启**——熔断恢复靠后台 ping，未见进程级重拉；`Manager.Attach` 每次新起一条 `ListenBreaker` goroutine | `Seele@v0.3.4/tools/mcp/provider.go:100-157`、`seelebridge/mcp/mcp.go:107-111` |

### 2.8 子进程与媒体生命周期

| # | 事实〔源码〕 | 证据 |
|---|---|---|
| H1 | 子进程统一装配只有一份：`exec.CommandContext` + 进程树（Windows=Job Object KILL_ON_JOB_CLOSE，POSIX=进程组）+ `cmd.Cancel` + `WaitDelay=5s`；挂树失败**退化**为只杀直接子进程（`tree.Degraded()` 标出） | `seelebridge/tools/router.go:570-593`、`security/process_tree_windows.go:16-32`、`async_run.go:30` |
| H2 | 会话销毁 → `killSession` 杀全部在途；进程级 `CloseAsync` 删临时目录（注释：实测曾累积 44 个 `seelex-async-*`）；后台作业日志 1MiB 封顶 | `async_exec.go:451-487`、`async_run.go:96-109,45-78` |
| H3 | 媒体内容寻址（`meta/<sha256>/<原名>`）**永不截断**；GC 需显式调 `CollectMedia`，未见周期性自动调用点 | `sessionstore/media.go:12-19,235-250` |
| H4 | 截图字节同时落盘 + 驻留 `imageattach.Registry`（Take「至多送一次」）；会话不再发请求则字节驻留内存**无 TTL/上限**；`ScaleNearest` 为 O(W×H) 纯 Go 逐像素 | `seelebridge/runtime_image.go:45-96`、`imageattach/registry.go:8-9,68-84`、`computer/image.go:7-38` |
| H5 | 零散 exec 点中 `workspace/workspace.go:372` `git remote -v` **无 ctx** | `workspace/workspace.go:372` |

### 2.9 取消传播

| # | 事实〔源码〕 | 证据 |
|---|---|---|
| I1 | 主链路完整：用户停止 → `Service.CancelChat` → `runtime.CancelFunc()()` → LLM 流、ReAct、同步工具、子进程树（`cmd.Cancel`）全部收到 | `application/core/service_input.go:332-346`、`seelebridge/tools/router.go:604` |
| I2 | **队列提升的下一轮 ctx 根是 `context.Background()`**——与外层应用生命周期脱钩，仅靠 `SetCancel` 登记 | `application/core/chat/chat.go:368`（对比首轮 `:90`） |
| I3 | 后台作业一律 `context.WithoutCancel` + 30min 硬顶——**有意为之**（点停止不杀 bash_bg/subagent） | `async_run.go:244`、`fork/tool.go:174`、`service_input.go:320-326` |
| I4 | 大量旁路读用 `context.Background()`，不响应用户取消：补全提交、compaction 索引、tool hooks、subagent 恢复、sessionstore 上下文 4 处等 | `application/core/chat.go:280,573-576`、`async_completion.go:86,211`、`compaction_index.go:79,210`、`tool_hooks.go:115`、`service_subagent_resume.go:33,44,68`、`sessionstore/session_context.go` |
| I5 | scheduler base ctx = Background，仅 Stop 时取消；停机等待 3s 后强取消 | `seelebridge/scheduler/scheduler.go:127,27-29,696-719` |

---

## 3. 待测假设与历史基线

### 3.1 待测假设（〔待测〕，源码无法定论，禁止提前宣称）

| # | 假设 | 裁决所需数据 |
|---|---|---|
| T1 | 长会话下每次 commit 的 head 整份重写 + MarshalIndent 耗时、renameBackoff 命中率 | pprof profile + 写入计数（已有 `lock_hotspot_probe_test.go`，需 `-tags lockprobe` 跑数） |
| T2 | `CountRequestTokens`/shrink 循环在 N 轮 transcript 下的实测量级（是否可见） | benchmark：N∈{50,200,800} 轮 transcript 的装配耗时曲线 |
| T3 | CloneSnapshot 成本随 Conversation/Plan/SubAgentTree 规模的增长曲线；GUI 是否在每事件后全量拉取 | benchmark + GUI bridge 消费频率打点 |
| T4 | publishMu 下单事件全订阅者投递 + 每事件 Marshal 在 message.delta 高频（40ms/批）下的尾延迟 | 投递耗时直方图打点 |
| T5 | StreamBatcher 满时 Gosched 自旋的 CPU 代价与是否反压 provider 流读取 | pprof CPU profile（构造慢消费场景） |
| T6 | 结构性事件逐写 fsync 在 compaction/fork 密集场景的 fsync 次数/秒与磁盘延迟贡献 | 计数打点 + 磁盘延迟基线 |
| T7 | 熔断振荡（Recovering/Recovered 抖动）下 `mcp-traces.json` 全量重写放大倍数 | 故障注入：反复断网/恢复 MCP server |
| T8 | Job 32 在途上限在 subagent 群扇出下的拒绝率与模型重试轮次 | 压测脚本（并发 bash_bg ×64） |
| T9 | imageattach 长会话驻留内存曲线；`Prepare` 按 ref 整图读回频率 | heap profile + 场景复现 |
| T10 | `seelex-async-*` 在崩溃后的实际残留率 | 故障注入：kill -9 后清点临时目录 |
| T11 | `awaitAsyncDeadline` 60s 上限内 ChatStream 持会话锁对用户输入/审批的阻塞分布 | 锁等待打点（mutex/block profile） |

### 3.2 历史基线（〔历史基线〕，引用须带状态）

- **2026-10-14 memory-cpu-accumulation**（`docs/2026-10-14-memory-cpu-accumulation/`）：裁定三件叠加——会话释放面不完整（bundles 只增不删）、进程级无界累积（MemoryTracer append-only、teamworkCoords/liveHistory 等残格）、热路径固定成本随规模线性上升（Snapshot 深拷贝、TokenCountFor 全进程事件集 Query）。**P0 释放面已落地（`limits.session_release_window`），P1/P2 未开始**。按 global-review F06/F07 口径：这些是修复前线索，当前释放正确性须查新代码，不重报旧缺陷。
- **ARCHITECTURE_REVIEW.md H1–H28**：历史风险假设表（ctx 不传播、goroutine 无 panic recovery、Compressor 静默回退等），与本文 I4/F4/G3 部分条目呼应，但状态需逐条以当前源码复核。
- **2026-09-25 compaction-blocking-and-usage-curve / 2026-09-27 gui-render-flash-and-jank**：更早的局部性能工作包，作为场景背景。

---

## 4. 可复现的基准与 profiling 计划〔提案〕

### 4.1 指标定义（先定义，后测量）

| 类别 | 指标 | 定义与采集点 |
|---|---|---|
| 延迟 | TTFT | 用户提交 → 首个可见 chunk 投影到 Snapshot（`Service.Submit` 打点 → 首个 `message.delta` flush） |
| 延迟 | 回合延迟 | Submit → 回合收尾事件（含全部工具边界） |
| 延迟 | Snapshot p50/p99 | 单次 `Snapshot()` 调用耗时（view_state 打点） |
| 延迟 | 事件投递延迟 | EventHub Publish → 订阅者 channel 入队（hub 打点，按订阅者类型分桶） |
| 延迟 | 工具边界投影耗时 | `CollectRuntimeProjectionFor` 单次耗时 |
| 吞吐 | 流式批次率 | message.delta 批次/秒、每批字节 |
| 吞吐 | 事件率 | EventHub 事件/秒（按 Kind 分桶） |
| 内存 | RSS/Heap 曲线 | 固定剧本（N 会话 × M 回合 × K 工具调用）浸泡，每分钟采样；对照 2026-10-14 P0 修复后基线 |
| 内存 | per-会话驻留 | bundles/EnginePort/imageattach 的堆对象归因（heap profile） |
| 写入频率 | commit 率与 head 重写字节 | sessionstore 打点：commit 次数/s、head 字节/s、renameBackoff 命中/s |
| 写入频率 | fsync 率 | 五条结构性通道各自 Sync 次数/s |
| 写入频率 | MCP trace 重写字节 | mcpstack save 次数与字节/s |
| 恢复时间 | 冷恢复 | 会话重开 → 可提交（LoadTail/装配重建）耗时；分会话规模分桶 |
| 恢复时间 | 会话切换 | 切换当前视图会话 → 首帧 Snapshot 可用 |
| 取消响应 | 取消延迟 | CancelChat → LLM 流停止、→ 工具进程树终止（分别计时） |

### 4.2 工具与命令（现仓库已具备）

```bash
# 1) pprof（已就绪：tag + mutex/block 采样已开）
go build -tags pprof -o seelex-pprof.exe .
SEELEX_PPROF_ADDR=127.0.0.1:6060 ./seelex-pprof.exe -frontend headless ...
go tool pprof http://127.0.0.1:6060/debug/pprof/profile?seconds=60
go tool pprof http://127.0.0.1:6060/debug/pprof/heap
go tool pprof http://127.0.0.1:6060/debug/pprof/mutex   # 锁竞争（D1/D2/D4 裁决）
go tool pprof http://127.0.0.1:6060/debug/pprof/block   # 阻塞（T5/T11 裁决）

# 2) 既有探针与 bench
go test -tags lockprobe -run LockHotspot ./sessionstore/...   # T1
go test -bench . -benchmem ./application/...                  # 复用 session_chain_bench_test.go 等

# 3) 请求指纹（LLM 侧延迟/体积，不落原文）
SEELEX_REQUEST_LOG=<path>.jsonl ./seelex-pprof.exe ...

# 4) 故障注入（T7/T10）：脚本反复断/拉 MCP server；kill -9 后清点 seelex-async-*
```

缺失需补的打点（最小侵入，均〔提案〕）：Snapshot 耗时直方图、hub 投递延迟、commit/head/fsync 计数器、取消延迟计时。落点建议统一走 §5 的 metrics Sink，不散设日志行。

### 4.3 基准剧本（可复现）

| 剧本 | 内容 | 覆盖假设 |
|---|---|---|
| S1 长会话流式 | 单会话 200 回合，每回合 3 次工具调用，持续流式输出 | T2/T3/T4/T5、内存曲线 |
| S2 多会话浸泡 | 20 会话轮换，含 fork/compact，4 小时 | 内存曲线、T1/T6、恢复时间 |
| S3 并发后台 | bash_bg ×64 扇出 + 批量 subagent | T8/T11 |
| S4 MCP 抖动 | MCP server 反复断网/恢复 100 次 | T7、G2 放大倍数 |
| S5 冷恢复 | 100/1000 行会话分别冷启动重开 | 恢复时间基线 |

每个剧本产出：pprof（CPU/heap/mutex/block）+ 指标 JSON + 原始计数，存档 `_logs/` 并在工作包内记录结论。

---

## 5. 现架构可立即补齐的可观测性〔提案〕

**前提：单进程不等于无法 tracing。** 现状缺口与补齐项：

| 缺口〔源码〕 | 补齐项〔提案〕 | 依据/落点 |
|---|---|---|
| 无 metrics 依赖与 Sink（全仓 grep 零命中） | 实现 Seele `telemetry` 的 MetricSink 接口，落进程内计数器 + `/debug/metrics`（expvar 或自渲染 JSON），先把 §4.1 的计数器接上 | Seele@v0.3.4 `telemetry/README.md`（Sink 三接口）；Seelex 装配点 `seelebridge/runtime.go:326-327` |
| `telemetry.OTelTracer` 存在于 Seele 但未接线 | 以 build tag/env 启用 OTelTracer（OTLP exporter，stdout collector 起步，无需外部服务即可验证 span 形态） | Seele@v0.3.4 `telemetry/otel.go`；装配点同上 |
| MemoryTracer 无界 append（历史基线 P1 未做） | 环形窗口或按 session 驱逐的 Sink 包装；注意 P0 已修释放面，勿重复报旧缺陷 | `seelebridge/internal/telemetry/telemetry.go:13-22` |
| 日志仅标准库 `log`、无结构化/级别 | 最小改动：引 `log/slog`（stdlib，无新依赖），先包一层统一入口，级别经 `seelex.yaml` 配置 | 现状：`import "log"` 散布 15+ 文件 |
| mcp-traces 单文件整份重写、Calls 无上限 | 改 append-only JSONL（与框架事件库同形态）+ 按会话分片；先测 T7 再定优先级 | `mcpstack/persist.go:46-64` |
| 取消链路无计时 | CancelChat 入口到 LLM 停止/进程树终止各打一个 span | §4.1 取消延迟指标 |

---

## 6. 未来跨进程 trace/context 传播〔提案〕

当多进程（见 [multi-binary-architecture.md](multi-binary-architecture.md)）落地后：

1. **trace 传播**：采用 W3C `traceparent`/`tracestate`。
   - MCP 通道：经请求 `_meta` 字段注入/提取 traceparent（MCP 协议允许 `_meta` 扩展）；`computer-use-mcp` 是首个试验点——主进程在 MCP 调用侧注入，MCP server 侧提取续 span。
   - 自有 IPC（若有）：JSON envelope 固定 `trace` 字段，与 `application/contract/dto` 同级定义 wire 契约。
2. **context 传播**：取消/截止时间与 trace 分开处理——跨进程取消不能依赖 `context.Context`（进程内对象），需要**显式取消消息 + 租约**：命令带 `deadline` 与 `cancel_token`，执行方到期自动放弃，控制方发取消消息立即终止（对照现有 `killSession` 语义跨进程化）。
3. **落地顺序**：先在单进程把 OTelTracer 接线并验证 span 形态（§5），再选 `computer-use-mcp` 做 `_meta` 注入/提取试点，最后才推广到自有 IPC。禁止先建 broker 再补语义。

---

## 7. 拆进程的成本分析〔提案〕

| 成本维度 | 内容 | 现架构对照 |
|---|---|---|
| IPC | 全仓当前**无网络服务端**（`net.Listen` 零命中，remote-agent-node-feasibility 结论）；需新造 IPC 层（stdio/named pipe/loopback HTTP）+ 协议版本治理 | 现为直接函数调用 |
| 序列化 | `RuntimePort` 含 5 处 `<-chan` 成员、回调与函数注入，**不可直接跨进程**；必须在端口下重切 request/response + event stream 的 wire 契约（dto/model 层已零依赖可复用） | 进程内接口零序列化成本 |
| 调度 | 进程级故障域、看门狗、重启策略（MCP server 现状就是无自动重启的反例 G3） | 单进程内 goroutine 生命周期已由 lifecycleHost 统一管理 |
| 部署 | 单 exe → 多 exe 的版本矩阵、签名、升级原子性；Windows GUI 安装包复杂度上升 | 现 Makefile 只产主二进制 + 手工构建 computer-use-mcp |
| 数据一致性 | data root 单写者/单进程锁是现状约束；多节点须各自 data root，会话/看板跨节点归属需新协议 | sessionstore 原子性由单进程保证 |
| 可观测性 | 跨进程 trace/context 传播成本（§6） | §5 单进程方案即可覆盖当前需求 |

**应先在现架构完成的优化**（拆进程不能替代）：

1. 释放面与无界累积治理（2026-10-14 P1/P2：telemetry 窗口、Snapshot 深拷贝、TokenCountFor 全量 Query）——拆进程只是把内存分摊到多个进程，不消除泄漏形态。
2. module head 每次提交整份重写（C1）与结构性通道 fsync 频率（F2/F4）——单写者存储问题，拆进程只会把它变成跨进程一致性问题。
3. 锁粒度与投影整份重建（D1-D5/C2）——拆进程不减少单会话内的锁竞争。
4. 可观测性打点（§4/§5）——没有基线数据，拆进程后的「收益」无法度量。

**拆进程真正买到的**：故障隔离（MCP/工具执行崩溃不拖垮主进程）、资源限额（OS 级 cgroup/job object）、独立重启（执行单元升级不断会话）、远程部署（Dot 方向）。只有当这四项之一成为明确需求时，才值得支付上表成本——详见架构文档的三方案对比。
