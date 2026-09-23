# 压缩的「串行工作」必须当场看得见：起手帧 + 逐关耗时（2026-09-23 第 3 轮）

> 触发：门禁进度面（`compaction.progress`：六关、逐关一帧、终局一帧）已经落地，但用户实测的
> 反馈是「**看不到加载**」——按下 `/compact` 后界面毫无动静，随后直接出现一条已完成的压缩
> 记录 / 回执。用户据此判断压缩「不是串行的、看不到它在干什么」。
>
> 续接上一份 devlog（`2026-09-23-compaction-frame-visibility-and-panel-drift.md`）：那一轮让压缩
> **记录**看得见（区间、来源、帧正文可回读）；这一轮让压缩**过程**看得见。

## 1. 根因：不是没发事件，而是「最长的一段」没有任何事件

### 1.1 实测时间线（`TestExplicitCompactGateTimeline`：4 轮 ×160KB fixture，`go test -v` 输出）

| # | 门禁 | index | 本段耗时 | 事实（detail） |
|---|---|---|---|---|
| 0 | judge | 0/6 | 0ms | **起手帧**（本轮新增）——按下回车后立刻有反馈 |
| 1 | judge | 1/6 | **26ms** | `compared=163925 all=160079 soft=125106 hard=150127` |
| 2 | assemble | 2/6 | 6ms | `assembled=83887 target=112055 autonomous=false` |
| 3 | replace | 3/6 | 0ms | `messages=4` |
| 4 | frame | 4/6 | **17ms** | `bytes=552 injected=false` |
| 5 | store | 5/6 | 0ms | `bytes=552 tokens=174` |
| 6 | record | 6/6 | 0ms | `recorded=true version=2` |
| 7 | — | 6/6 | 0ms | `done reached=6/6` |

同一 fixture 跑 30 轮（`-count=30`）的分布：判据关 **31–64ms**（中位 40）、装配关 6–18ms
（中位 11）、替换关 ≤1ms、落帧关 30 轮里 29 轮为 0ms（一次 54ms 的调度离群）、落库关与记录关
恒为 0ms；`CompactContextNow` 整轮墙钟 40–121ms（中位 53）。

**这些毫秒数不是代码的属性，别把它当契约读。** 同一台机器、同一份 fixture，隔几分钟分别跑
`-count=1` 与 `-count=30`，判据关就读出 26ms 与 31–64ms 两组数；本文上一版记录的
「判据关 22–40ms、装配关 5–8ms、落帧关 15–19ms、门禁段墙钟 ≈45–65ms」今天已不复现（尤其
落帧关，今天几乎恒为 0ms）。稳定成立的只有**形状**：一格一格的逐段实测值、六关按权威顺序各
收口一次、且这些段时之和不超过整轮墙钟。数值随机器负载漂移，这一句本身就是本轮要留的记录。

结论：**有两段静默**，判据关一段（最长）+ 落帧关一段，而它们前面一个事件都没有。走旧路径
时，界面上「最长的一段」就是全黑；其余几关挤在十几毫秒里，任何刷新都来不及画一帧。用户看到
的「没反应 → 突然完成」是这条时间线的必然结果，不是错觉，也不是前端丢了事件。

### 1.2 三个怀疑点逐条排除（每条都留了钉子）

| 怀疑 | 事实 | 钉子 |
|---|---|---|
| 事件根本没发？ | 否：六关序列 + 终局都在，`detail` 带真实判据 | `TestExplicitCompactEmitsOrderedProgressGates` |
| 架桥/中继吞了？ | 否：`revision=0` + 会话路由的帧在 GUI 中继里原样到达渲染层 | `TestBridgeRelaysCompactionProgressToRenderer`（本轮新增） |
| 前端没画？ | 否：零记录时也出内容（折叠发生在写记录之前） | `renderContextCompactions([], {progress})` 用例 |

真正的问题正是 1.1：**它在「已经完成」之后才有东西可画**，而且画出来的东西**不含任何时间
信息**——一条瞬时满格的绿条。读者合理地认为什么都没发生。

### 1.3 「看不到串行」是同一件事的另一面

界面上唯一能表达「一关一关串行跑」的东西，就是把过程报出来。整轮只有几十毫秒，做不到
「进度条慢慢走」，所以能读的证据只有一个：**每一关自己花掉的时间**。说得出「判据估算 26ms、
装配 6ms、其余不到 1ms」（1.1 那张表的一次实测），用户才能确认它是串行跑的，并知道慢在哪一步。

## 2. 改法

### 2.1 起手帧（后端）：动第一个重活之前就说话

- 载荷新增 `Phase`、`ElapsedMS`；新增 `event.CompactionPhaseBegin`。
- `compactionProgress.begin()`：`index=0`（**一格都不预支**，进度条不许先走）、`gate=judge`
  （即将发生的那一步）、`version=0` = 尚未定稿（判据关收口时用 `setVersion` 补正——
  新版本号此刻确实还没算出来，绝不先猜）。
- **只有显式路径能提前开轮**：`options.forceCompact` 为真时开轮（它与 `fold` 判据里的
  `|| options.forceCompact` 是同一个条件，必然配对），自动路径仍等判据关收口之后开轮
  （要不要折叠正是这次估算的结果）。于是「**没折叠就没有进度**」继续成立
  （`TestNoProgressEventsWithoutFold`、`TestAutoCompactionEmitsProgressGates` 的反面断言）。

### 2.2 逐关计时（后端）：每帧带「刚刚过去那一段」

`elapsedLocked()` 返回距**上一帧**的毫秒数并推进基准：起手帧 0，每个门禁帧 = 这一关自己
花掉的时间（不是从开轮算起的累计值；累计值会把六个数读成「一关比一关慢」，而它们其实是
同一段的重复）。前端把各帧加起来才是总耗时。

### 2.3 前端：清单 + 纯函数

- `compaction-format.mergeCompactionProgress(previous, payload)`：判轮次边界、累加逐段耗时、
  终局沿用最后一条 running 的序号（中途失败停在真正走过的格子）、不用的帧（非对象 /
  `total=0`）原样返回 `previous`。**轮次边界 = 起手帧 或「上一轮已收口后又来 running」**——
  自动路径没有起手帧，只看 `phase` 会把两轮读成一轮。
- `context-summary.renderCompactionGateTimeline`：`<ol>` 一关一行，起手帧那一关写「进行中」
  （**不编耗时**），`compactionGateDurationText` 把 0 写成 `<1ms`（后端给的是截断毫秒，
  0 的含义是「不到一毫秒」）。
- `app.js`：从「存字段」改成 `mergeCompactionProgress(...)`，保留原撤条定时器（终局 2.5s /
  失败 6s）——清单因此来得及读。
- `styles.css`：耗时等宽右对齐，便于竖着比快慢。
- 静默降级：没有会话路由键或宿主不支持按会话订阅时，`startCompactionProgress` 返回 nil，
  压缩照常发生、只是没有进度（与本轮之前的可见面一致，不因此让压缩失败）。

## 3. 验证

| 命令 | 结果 |
|---|---|
| `go build ./...` | 通过 |
| `go vet ./application/... ./gui/...` | 通过 |
| `go test ./application/... ./gui/... -count=1` | 全部 `ok` |
| `node --test gui/frontend/dist/*.test.mjs` | **448 pass / 0 fail** |
| `go test ./application/core/ -run TestExplicitCompactGateTimeline -v` | 上面那张时间线表 |

有牙证明（PROBE：改回旧口径 → 测试必须变红 → 还原即绿）：

| PROBE | 改动 | 变红的测试 |
|---|---|---|
| 1 | 去掉 `progress.begin()`（不再发起手帧） | `TestExplicitCompactEmitsOrderedProgressGates`（起手帧 = 0 条）、`TestExplicitCompactGateTimeline`（第一帧不是起手帧） |
| 2 | `elapsedLocked` 改成「从开轮算起的累计」 | `TestExplicitCompactGateTimeline`（实测一次：逐关之和 387ms ≫ 门禁段墙钟 ~65ms） |
| 3 | 前端只按 `phase === "begin"` 判新轮 | `a new round never inherits the previous round's checklist`（清单 39 条 vs 11 条） |
| 4 | 前端让起手帧也入清单 | `the begin frame never becomes a checklist row of its own`（判据关凭空多一行 `<1ms`） |

PROBE 2 顺带修掉了判据本身的弱点：原先拿**整轮**墙钟当上界（整轮含建服务、订阅、落记录等
无关开销），在快机器上「累计口径」能蒙混过关；现在拿**门禁段**墙钟（起手帧到达 → 终局帧
到达）当上界，并留「帧数 + 5ms」的取整余量，六关累计值之和（约门禁段墙钟的 3–6 倍）必然被
抓住。这条是这一轮唯一「测试先没牙、补了牙」的地方。

## 4. 边界与未做

- **GUI 冒烟未执行**：运行中的 GUI 是旧构建，`dist/*.js` 与 `styles.css` 需要重开进程才会
  加载；「起手帧那一刻就有反馈」在实机上的确认要等新构建（同上一轮 devlog 的边界）。
- **控制台（TUI）不消费 `compaction.progress`**：本轮只做 GUI 侧；事件已在门面
  （`application` 的常量与载荷类型别名）与桥面上，接 TUI 是后续独立工作。
- **自动路径没有起手帧**：它最长的一段（同样是那一次全量估算）原理上无法提前反馈——要不要
  折叠正是这次估算的结果。这条不是实现疏漏，是判据的顺序；写成「先说要压缩」会在不压缩的
  回合里撒谎。
- **进度是瞬态的**：不进快照、不落盘、刷新即失（`revision=0`）。终局之后靠压缩记录读结果。
- 若某台机器上六关全都 <1ms，`TestExplicitCompactGateTimeline` 只剩 `sum > 0` 一条在生效
  （耗时判据失去分辨率）——这属于机器速度，不是契约，测试不会因此变红。

## 5. 补记：fork 口径声明（同一批，回应"需要声明是 forksession 还是 forksubagent"）

上一份 devlog（`2026-09-23-compaction-frame-visibility-and-panel-drift.md` §4.3）留了一条
「fork 的可达性裁剪是否计入压缩记录的 `frame_ref`——**未验证**」。核过工作区后：**已修**，
并且本轮把它的**适用范围**写成代码里的显式声明，因为"fork 会按 ref 裁剪可达集合"这句话只对
两种 fork 里的一种成立：

| | 会话分叉 ForkSession（`/fork`、GUI `ForkSessionLatest`） | 子代理派发 ForkSubagent（`fork_subagents`） |
|---|---|---|
| 落盘形态 | 独立项目级会话 + tool-results 整通道物理复制 | `subagent_<hash>/` 子树，**不建独立 blob 目录**、大工具输出引用主会话 `big_tool_result`（T-FK-05/06） |
| refs 索引 | 子会话自己的索引，读路径**强制**校验（`ReadToolResult` 对不在索引里的 ref → not-exist） | 派发链不经过 `PrepareFork`，没有"按 ref 裁剪"这一步 |
| 按 ref 裁剪 | 有（`PrepareFork` → `truncateForkRecord` → `reachableToolResultRefs`） | **没有这一层** |
| 压缩记录 `frame_ref` | 跟着继承的记录一起可达（本轮修） | 不适用（不经 `truncateForkRecord`） |

（表里"没有这一层"是按"派发链是否调用 `PrepareFork`"判定的事实；子代理侧大工具输出
具体在哪个 store 键下落盘，本轮**没有**追到底，所以在此不下断言。）

声明落在四处：`session_runtime/fork.go` 文件头（本文件只描述 ForkSession；子代理口径要走存储侧）
＋ `reachableToolResultRefs`/`truncateForkRecord` 函数注释；`application/core/session_fork.go`
（入口声明）；`sessionstore/fork_store.go` 头部（存储侧两条路交叉引用）；`docs/arch/README.md`
的「分支」链路一行。

顺带把注释里那句会被误读的话改准：原文写"属于 fork 点之前就已经产生的证据"，而压缩记录是
**整批继承**（`truncateForkRecord`：结论晚于切断点就清空，压缩历史保留），所以新注释明说口径是
「跟着被继承的记录走，不是按时间过滤」，并指出要改成"后切记录不进子会话"必须同时改记录侧，
否则记录与可读性会脱钩。

验证与钉子：

| 检查 | 结果 |
|---|---|
| `go build ./...` / `go vet ./application/... ./sessionstore/...` | 通过 |
| `go test ./application/... ./gui/... -count=1` | 全部 `ok`（20 包） |
| `go test ./sessionstore/ -count=1` | `ok 73.3s`（单跑） |
| 中途抖动 | `TestCommitNotBlockedByFullHistoryRead` 在「三包同跑」那一次红过（2.00s 超时），单跑 3 次与整包复跑全绿；本批对 `sessionstore/` 的改动**只有注释**（8 行），与它无关——属时序敏感用例在全机负载下抖动 |
| 新钉子 `TestForkSessionKeepsCompactionFrameRefReachable` | PASS：继承的 `FrameRef` 在子注册表里，且切断点之后的 `result:later` 仍被裁掉 |
| PROBE：把 `reachableToolResultRefs` 里的压缩帧块删掉 | `TestForkSessionKeepsCompactionFrameRefReachable`（`compaction frame ref "tr-frame-before" missing`）与 `TestPrepareForkTruncatesToRequestBoundary`（`reachable ref "tr-frame-before" missing`）同时变红；还原即绿 |

仍然没有做的事：子代理侧的对应口径没有加任何断言（本轮只是声明它"没有这一层"，没有构造
subagent 会话去读父会话压缩帧的端到端用例）；GUI 冒烟边界同 §4 第一条。
