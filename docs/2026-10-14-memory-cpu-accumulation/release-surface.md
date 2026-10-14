# 会话释放面规范 v1（受保护集 + LRU 窗口 = 1）

本文件把用户给出的释放口径写成可实现的规范，并给出与现有实现的差异清单。
证据见 [`verdict.md`](verdict.md)；改动落点见 [`optimization-plan.md`](optimization-plan.md)。

## 1. 口径（用户原文拆解）

> 当前视图的，待审批的，和运行中的会话不做释放，但是加载过静默的会话同时加载过的
> （例如切换会话的时候运行完的会话）通过 LRU 算法做出释放，LRU 的窗口是 1，
> 即只保留上一个会话的内容。

拆成四条可判定规则：

| 规则 | 判定 |
|---|---|
| R1 | **当前视图会话**不释放（用户正在看的，切回必须瞬时） |
| R2 | **待审批会话**不释放（审批是外部事件的等待方，释放会打断） |
| R3 | **运行中会话**不释放（含排队 / 在飞请求） |
| R4 | 其余"**已加载过、且已静默**"的会话（典型：切走之后回合跑完的那个）按 LRU 释放，**窗口 = 1 ⇒ 只保留上一个** |

"保留上一个"的确切语义：受保护集之外，**最多留 1 个**最近使用过的已加载会话；
再往上（第 2 个及更旧）立即释放。

## 2. 与现有实现的差异

| 项 | 现状 | 差异 |
|---|---|---|
| 保护集判据 | 已有：`resident_lru.go:104-126` / `content_lru.go:150-179` 排除非视图、`restoring`、非驻留、`sessionBusy`；`sessionBusy`（`resident_lru.go:167-185`）覆盖待批 / 运行 / 排队 / 在飞 | **一致**（无需改判据，只需提成共用谓词） |
| 窗口语义 | `reconcileResidentLimit`（`resident_lru.go:62-102`）与 `pruneContentOrderLocked`（`content_lru.go:89-118`）的计数**把当前视图会话也算进 limit** | **不一致**：`limit=1` 实为"只留当前"，用户口径需要"受保护集之外计 1" |
| 释放动作 | `evictResident`（`resident_lru.go:128-166`）只调引擎端口 `UnloadSession`；**不触** `seelebridge.Runtime.bundles`（`verdict.md` §3 A1/A2） | **不完整**：释放后框架会话本体 + ctxStore + 主历史仍在 |
| 静默化触发 | 只在 `touchResident` / `touchContent` 时收敛 | **缺口**：会话"忙→闲"的那一刻没有收敛点（见 §4） |
| 会话删除 | `DeleteSession`（`workspace_usecase.go:21-44`）不释放内存 | **缺口**：删除应走完整释放面 |
| 两层窗口 | 引擎 6 / 内容 12（`seelexctx/limits.go:62-74`，默认 `:318-319`） | 需要统一到同一窗口（默认 1） |

## 3. 规范

### 3.1 受保护谓词（唯一判据）

```
protectedSession(sid) := sid == viewSessionID
                      ∨ isRestoring(sid)
                      ∨ sessionBusy(unit(sid))         // 待批 ∪ 运行 ∪ 排队 ∪ 在飞
```

`sessionBusy`（现成，`resident_lru.go:167-185`）的定义保持不变：
`PendingApprovalCount() > 0` ∨ `ChatState().Running` ∨ `ChatState().QueuedCount > 0` ∨
`len(PendingRequests()) > 0`。

要求：resident / content / 会话删除三条路径**共用同一个谓词**，不允许各自复制一份条件
（现有代码是两份近似实现；统一后"保护集"只有一处事实源）。

### 3.2 窗口语义（受保护集之外计数）

```
window := Limits().SessionReleaseWindow        // 默认 1
candidates := 已加载会话 - 受保护集
for len(candidates) > window {
    evict(LRU 最旧的一个 candidate)             // 一次会话级整面释放
}
```

- 计数**只数"可释放的已加载会话"**，受保护会话不计数（修正 §2 的第二行差异）。
- `window = 1` 即"只保留上一个"；`window = 0` 等价于"只保留受保护集"。
- 引擎层与内容层共用同一窗口值；旧键 `resident_limit` / `loaded_content_limit` 作为兼容别名
  映射到该窗口（或保留为"分层逃生门"，见 §3.5）。
- **容忍超限**：无候选时不驱逐（现状语义保留，`resident_lru.go:92-96` / `content_lru.go:141-145`）。

### 3.3 释放清单（一次会话级释放要清的面）

按"重到轻"排列，逐项都要有释放动作与"保留什么"的口径：

| 层 | 面 | 释放动作 | 保留 / 回读 |
|---|---|---|---|
| 1 | 引擎端口槽 `EnginePort.engines[sid]`、`engineCalls`、`sessionMaxLoops`、`handoffLatest` | `UnloadSession`（`engine_port.go:896-913`，需**补删** `handoffLatest`，见 `verdict.md` §3 B6） | 无；重开按进程默认值 |
| 2 | 框架会话本体 `Runtime.bundles[sid]`（含 `Session` / `LoopHooks` / `sessionBindings`） | **新增**桥侧释放面（`DropSession`）：删 `bundles[sid]`；活跃别名不得指向被释放会话 | ctxStore / 主历史随本体离场，重开由 `sessions-json` + record 重建 |
| 3 | 会话上下文存储（`SessionContextStoreFor`，压缩帧 / 上下文记录缓存） | 随第 2 层离场（同一 bundle 持有） | 磁盘 `SessionContextStore` 与 record 是事实源 |
| 4 | 可见正文 `View.Conversation` | 内容层释放：`Conversation = nil` + `ContentUnloaded = true`（**不重置** `HistoryOffset` / `TotalMessages` / `HasMoreHistory` / `ConversationWindow` / `ReadFiles` / 聊天运行态） | 回读走 `reloadSessionContent`（`content_lru.go:255-…`）尾部窗口 |
| 5 | LRU 使用序 `residentOrder` / `loadedContentOrder` | 从两序中移除（`evictResident` 已做移出，`content_lru.go:75-87` `forgetContentLocked`） | 重开时经 touch 重新入序（最近使用端） |
| 6 | 桥侧 per-会话/节点残格 | 会话段清理：`teamworkCoords[key]`、`liveHistory[nodeID]`、`routingNoticeSeen[key]`、`roleTurns`、`images[sid]`（`verdict.md` §3 B4） | 团队看板 / 子代理树 / 作业行按需从磁盘重读（已有缓存失效机制） |
| 7 | 会话单元 `Unit`（Chat 状态 / 待批 / 文件引用 / 窗口标志）+ runtime 槽 | **LRU 释放保留**（轻量，`Resident=false`）；**删除/归档走完整面**：`sessions.Remove` + `DropPlanProjection` + `UnloadSessionState` + `UnloadSessionTitle`（`session_lifecycle.go:150-160`） | 目录行与标题来自存储层枚举（零正文读） |

两条释放强度：

- **`releaseIdle`（LRU 用）**：第 1–6 层释放，第 7 层保留。
- **`releaseFull`（删除 / 归档 / 显式卸载用）**：第 1–7 层释放；运行中拒绝（`ErrChatRunning`）。

`DeleteSession`（`workspace_usecase.go:21-44`）在存储删除 + 释放异步作业 + 解绑之后，
**必须**补一次 `releaseFull`（修正 §2 最后一行差异）。

### 3.4 释放动作的顺序与失败语义

```
1. 状态二次确认（ViewMu 临界区内）：若期间变为 busy / 视图 / restoring ⇒ 放弃（返回 nil，容忍超限）
2. flush：非活跃会话 PersistCurrentSession(location, sid)     ← 失败则保持驻留（不丢数据）
3. 内容层释放（第 4 层）
4. 引擎端口卸载 + 桥本体释放（第 1–3 层）
5. 记序 / 清残格 / 请求目录刷新
```

失败语义（沿用现有纪律）：

- flush 失败 ⇒ **不卸载**，数据不丢，下一轮 touch 再试（`resident_lru.go:151-156` /
  `content_lru.go:199-204` 已是该语义）。
- 释放顺序上"内容先、引擎后"仅是先易后难；两者都必须在同一会话上完成，避免长期停在
  "引擎在、正文没了"的中间态（该中间态本身合法，只是口径上难以解释）。

### 3.5 分层逃生门（可选，P1）

允许把两层窗口解耦（`loaded_content_window` > `release_window`），用于"正文窗口便宜、
引擎本体贵"的取舍；默认仍共享同一窗口，避免出现两套口径。

### 3.6 触发点（何时收敛）

| 触发 | 现状 | 规范 |
|---|---|---|
| 冷加载完成 / 热挂载 / 分页读回 / 回读 | 有（`touchResident` / `touchContent`） | 保留 |
| **会话由忙转闲**（回合收尾、审批结案、队列排空） | **无** | **新增一次 `reconcileSessionRelease()`**：这是"切走之后那个会话跑完了"的收敛点，缺它就只能等下一次 touch |
| 会话激活 / 切换 | 有：热挂载 `session_lifecycle.go:111/113`；冷加载/恢复 `session_history.go:570/572`；分页 `session_history.go:928` | 保留 |
| 删除 / 归档 / 显式卸载 | 部分（归档有、删除无） | 补 `releaseFull` |

## 4. 不释放的对象（明文写清，避免过度释放）

| 对象 | 理由 |
|---|---|
| 磁盘会话事实（`sessions-json/…`） | 事实源，永不因内存压力改动 |
| 工作表格台账 / 定时任务 / 事件流 | 跨会话读模型，与视图无关 |
| 后台作业登记表与输出 | 框架侧**已封顶**（`verdict.md` §3 B5）；运行中作业不驱逐 |
| 子代理归档与会话快照 | 节点详情数据面（结束后仍可读回）；有界窗口见 `SubagentLiveWindow` |
| 草稿槽 | 未发送输入期间的占位，无会话事实 |

## 5. 验收判据（不变量）

| ID | 判据 | 可测形式 |
|---|---|---|
| INV-RS1 | 视图会话永不释放 | 切走瞬间仍 `HasSession(view)==true` |
| INV-RS2 | 忙会话永不释放 | 待批 / 运行 / 排队 / 在飞四态各一个用例，释放后仍在 |
| INV-RS3 | 静默会话数 > 窗口 ⇒ 释放最旧、保留最近 1 个 | 三个静默会话 ⇒ 释放后"可释放已加载会话数 ≤ 1" |
| INV-RS4 | 释放是完整面 | 释放后：引擎端口无该会话 ∧ 桥注册表无该会话 ∧ 单元仍在（`releaseIdle`） |
| INV-RS5 | 释放不丢数据 | flush 失败注入 ⇒ 会话保持驻留且磁盘无半截数据 |
| INV-RS6 | 释放后激活 ≡ 冷加载 | 释放 → 再激活：正文窗口、压缩帧、上下文与释放前一致 |
| INV-RS7 | 删除无残格 | `DeleteSession` 后：引擎 / 单元 / 标题 / 任务投影 / 桥残格全部不存在 |
| INV-RS8 | 计数口径 | 受保护集不计入窗口（构造 1 个视图 + 1 个待批 + 1 个静默 ⇒ 静默被释放） |
