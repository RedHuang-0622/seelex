# DS-A2A 双会话不对称 A2A：详细设计（运行时 + 演示 + 前端角色会话显示）

> **状态：当前基线（权威详设）。** 运行时组件、图 2 时序演示、验收 A1–A7 与前端角色显示均以本文为准；
> 与 `ds-a2a-protocol.md`（协议 v0.1）配套。本目录统一基线入口与文档状态见 `README.md`。

> 系列：`docs/2026-09-07-seele-a2a-framework-req/`，本文承接
> `ds-a2a-protocol.md`（协议 v0.1，**唯一协议真相源**）。`requirements.md` 为需求单、
> `same-session-governance-math.md` 为数学与 Diff、`dual-session-alternative.md` 为路线评估。
> 本文回答三件事：**运行时长什么样（可执行）**、**演示图（协议在每一跳发挥什么作用）**、
> **前端如何显示不同角色的会话**。日期：2026-09-07。状态：当前基线（权威详设，协议 v0.1 配套）。

---

## 1. 阅读地图（本文的图）

| 图 | 内容 | 说明什么 |
|---|---|---|
| 图 1 | 系统架构（含协议双流标注） | 组件边界 + 协议作用点 |
| 图 2 | 端到端执行时序（演示 trace） | 协议帧在每一跳的时序与作用，可直接照着实现/复述 |
| 图 3 | 水位 / 帧单调性示意 | ref_seq 因果与幂等 |
| 图 4 | b 生命周期状态机 | 何时 bind/unbind/reap，与 goal 状态联动 |
| 图 5 | 前端三轨视图布局（TUI 线框） | 不同角色会话如何显示 |
| 图 6 | GUI 角色泳道示意 | 浏览器端不同角色显示 |

**读法**：图 1 → 图 2（最重要，逐步执行）→ 图 4 → 图 5/6；组件细节与代码草案穿插在 §4。

---

## 2. 系统架构与协议作用点（图 1）

```text
                    ┌────────────────────────────────────────────────────┐
                    │  UI 层（前端，见 §7）：                              │
                    │   主转录流(EXEC) │ goal 条 │ ADVISOR 面板 │ 事件泳道  │
                    └───────▲───────────────────────▲────────────────────┘
                            │ projection/事件订阅     │ 角色渲染(§7 DTO)
┌───────────────────────────┴───────────────────────┴────────────────────────┐
│ 运行时（seelex 会话运行时 + goal 域）                                         │
│                                                                              │
│  ┌────────────────────── EXEC 会话 (a) ──────────────────────┐               │
│  │ C_a = P_a ⊕ 全量转录 H_a    写工具 / plan/task/goal 主状态 │               │
│  │ 唯一 writer；用户可见视图                                     │               │
│  └───────────┬────────────────────────────────────────────────┘               │
│              │ a 事件日志（账本源，seq 单调）                                  │
│              ▼                                                               │
│  ┌─────────────────────── CONTROLLER（goal 域 owner）─────────────┐           │
│  │ PeerSessionManager(生命周期) │ Mirror(水位/游标) │ DirectiveBus │           │
│  │ FrameCodec(帧编解码/幂等)    │ GateBridge(终态裁决/B4降级)       │           │
│  └───────┬───────────────────────────────────────────┬──────────┘           │
│    ①协议  │ frame.append(见协议§4)                    │ ②协议             │
│   a→b 流  ▼                                            │  b→a 流           │
│  ┌────────────────── ADVISOR 会话 (b) ────────────────────┐  ▲               │
│  │ C_b = P_b ⊕ 帧历史 ⊕ 自身回合段                          │  │ gate.verdict │
│  │ 只读/无工具面；后台 ReAct；回合产物=建议帧                  │  │ advisory.send│
│  │ 前缀稳定 P_b + 只尾部追加 ⇒ 缓存命中(§6)                 │  └───────────────┘
│  └──────────────────────────────────────────────────────────┘               │
│                                                                              │
│  事件账本（持久 EventStore）：EXEC 事件 + 协议事件 + 回合/verdict/缓存观测 → 前端  │
└──────────────────────────────────────────────────────────────────────────────┘

协议作用点标注：
  ① a→b 帧同步流 = 协议 §4 帧表 + §7 规则3(on_eval 事务同步)
  ② b→a 建议流 = 协议 §5 载荷表；结构化、受信注入、不进 a 主转录
  （生命周期 peer.bind/unbind、错误码/降级 = 协议 §8-§10）
```

---

## 3. 端到端执行演示（图 2，最重要）

场景：goal = “修复 login 模块的 401 竞态，补负路径单测后收口”。EXEC(a) 执行，ADVISOR(b) 后台评审。
协议帧用 `◆kind` 标注在发送一侧；`[cache]` 标 b 的缓存行为。

```text
时间轴（1 次 goal 生命周期）                    EXEC a（用户可见）        ADVISOR b（后台，C_b 尾随状态）
─────────────────────────────────────────────────────────────────────────────────────────
t0  goal_begin
    CONTROLLER: spawn b（只读工具面+预算）       转录:5 ← goal.start 注入      C_b = P_b ⊕ [5]
    ◆peer.bind{role:advisor, on_eval}           （goal 条置顶）             （锚点已落 b）
─────────────────────────────────────────────────────────────────────────────────────────
t1  a 干活：读代码/复现/写补丁（转录 6,7… 长链）
    mirror 按 on_eval 抽帧：尚未到回合水位       转录: 6 7 8 …              C_b 不动（落后）
    ───────────────────────────────────────────────────────────────────── [cache] b 无调用
t2  工具里程碑：a 跑完单测 = tool.checkpoint
    ◆transcript.inc? no   ◆tool.checkpoint{summary:UT PASS}   → 按策略入 b
    mirror：from=applied → QueryRange(a,seq) 一次性 append 落后帧
    b 回合（1 次 LLM 调用）                       a 继续（不被阻塞）         C_b = P_b⊕[5]⊕[帧:6..checkpoint]
    ─────────────────────────────────────────────────────────────────────── [cache] 首帧新增段计费
t3  b 出建议                              ◆advisory.send{type:directive, text:“401 竞态根因是
                                         token 刷新无双锁，建议先补两路并发单测”, ref_seq}
    DirectiveBus 去重入队（cap32）→ 下一轮前排空受信注入 a（不混入转录）
    a 依建议补并发单测 → 转录推进
t4  a 提议终态                               ◆terminal.proposed{candidate:finish, evidence}
    GateBridge: verdict 需要 ref_seq ≥ 提议水位；mirror 同步至水位后触发 b 回合
    b 评估（旧前缀全命中 + 新帧段）           a 等 verdict（有 deadline）    C_b ⊕= [帧:…]
    ─────────────────────────────────────────────────────────────────────── [cache] 命中回升
t5  b 判 not_done                          ◆gate.verdict{not_done, reason:缺负路径}
    a 补负路径 → 转录推进 → t4 再来一轮
    （每轮 C_b 前缀稳定 → 仅新增段计费 → 命中持续回升）
t6  最终 verdict done → goal 收口
    CONTROLLER：◆peer.unbind{reason:done} → 停议程 → drain pending → reap b
    审计：b 全部回合/verdict 已入账本（删的是会话对象不是账）
───────（若某步 provider 429/超时）─────────────────────────────────────────────
    协议 §8：◆error{429} → unbind{reason:429} → a gate 判负/升人工 → a direct 模式继续
    铁律：a 永不等待 b；429 下 goal 流仍收敛（写成 -race 断言）
```

**协议在每一跳的作用（一句话）**：① 帧流让 b 用**受信增量**重建“它自己的上下文”，不需要读 a 的
全量转录；② 水位 ref_seq 保证 b 的每个判断都基于“它确实看过的 a 状态”，因果不错位；③ 建议流
结构化 + 幂等，让 a 只采纳结构不吞文本；④ bind/unbind + 错误码把“b 生与死”显式交给 owner，
任何一步 b 缺席都有安全默认。

### 3.1 水位与幂等（图 3）

```text
a 账本 seq:     1──2──3──4──5──6──7──8──9──10──11──12
                │           │      │        │
                ▼           ▼      ▼        ▼
b 帧历史:     goal.start  帧A    checkpoint 帧B        （帧带 ref_seq，只尾部追加）
              ref=5      ref=7   ref=8     ref=11
规则：应用帧要求 ref_seq > last_applied（单调）；ref_seq ≤ last_applied ⇒ ◆dup_frame 幂等丢弃。
b 回合输出带 ref_seq：gate 只接受 ref_seq ≥ 裁决所需水位；否则按 §8（缺席/要求重估）。
跳帧合法（b 可落后到 8 再评估 11），回退非法 ⇒ 账本一致性 = a 全局单调 + b 帧单调。
```

---

## 4. 运行时组件详细设计（产品层；全部映射 seelex 现成件）

> 归属：seelex 产品层新包（建议 `application/core/peersession/`），协议类型放 `peersession/protocol`
> （与 `ds-a2a-protocol.md` 一一对应，schema 为协议 v0.1 真相的 Go 镜像）。

| 组件 | 职责 | 关键 API（草案） | 映射 seelex 现成件（证据） |
|---|---|---|---|
| `PeerSessionManager` | b 生命周期 owner：spawn（components 模板=角色模板）/bind/unbind/reap；goal 状态联动 | `Spawn(ctx, role, goalID) (peerID,error)`；`Unbind(reason)`；`Reap()`（ctx cancel+10s 护栏）；状态机 §图4 | seelex main 会话装配函数；Seele `session.NewSession/Chat/ReActLoop/Reset`；现有子代理回收 stopAndReap 10s 语义 |
| `Mirror` | on_eval 事务同步：维护 `lastAppliedSeq` 游标；回合前一次性拉新帧 | `BeforeEval(ctx) (syncedSeq, changed bool, err)`；内部 `QueryRange(a, from=applied,to=now)`→帧编码 | `seelebridge/events_unified.go:118-140` LoadRange/QueryRange；事件 seq=At.UnixNano（85-94）；EventStore 持久双写（events.go:32-59） |
| `FrameCodec` | 帧 schema v1 编解码；幂等（ref_seq 单调/dup 账）；有界（帧体积上限） | `Encode(frame)/Decode([]byte)`；`Apply(ctx, frame) (dup bool, err)` | goal 帧渲染参数可复用（goal 帧 + tail≤8 + pending≤N） |
| `DirectiveBus` | b→a 建议信封：corr 去重、有界队列(cap32)、受信注入排空 | `Enqueue(d Advisory)`；`DrainFor(sessionID)`（渲染为受信帧，不进转录） | `application/core/service_input.go:25` `injectPendingSubagentContextsFor` + chat.go:118/205 每轮前调用；goal a2a.go:26 MaxDirectiveQueue=32 |
| `GateBridge` | 终态裁决：terminal.proposed→（水位门槛+回合预算）→verdict；**B4 缺席降级矩阵** | `ProposeFinish(ctx, evidence) (verdict, err)`；`ref_seq` 校验；429/timeout→§8 | goal MVP `gate.go`（终态/审批预筛/verdict 语义可复用，换协议面） |
| `CacheReporter` | b 每回合记录 `usage.input_tokens` + provider `cached_input_tokens`；审计 | `Record(peerID, seq, usage)` | seelex telemetry/事件账本；provider 响应字段 |
| `PeerEvents` | 协议事件发布（帧/回合/verdict/水位/缺席）→ 前端订阅 | `Publish(kind, payload)` | seelex `/events` 订阅面 + 统一事件 reader |

**回合执行序列（Mirror + b 一步事务，对应协议 §7 规则 3）**：

```text
1  GateBridge/议程 触发（水位门槛已过）
2  Mirror.BeforeEval：applied<now ? QueryRange→FrameCodec.Append 到 b 历史 : 跳过
3  b 一次 LLM 调用（其上下文 = 稳定前缀 + 已 append 段）
4  CacheReporter 记 usage/cached；回合产物结构化
5  verdict/advisory → DirectiveBus（corr 去重、受信注入排空）
6  生命周期位更新（bound→evaluating→advisory_pending，图4）
```

### 4.1 b 生命周期状态机（图 4，与 goal 联动）

```text
                goal_begin
   detached ──────────► binding ──bind ok──► bound(g)
                     spawn 失败/429            │ 回合触发（水位+预算）
                                             ▼
                                      evaluating ──产物──► advisory_pending ──排空──┐
                                             │                                      │
                                             └───── 仍 bound（同 goal 内循环）◄──────┘
   终态：done   ：goal 收口 → unbind(done) → 停议程 → drain → reap b（删会话对象，账本留档）
         evicted：429/error/timeout → unbind(reason) → 按 §8 降级（low放行/high人工）→ reap
         killed ：会话关/超时护栏 → reap
协议消息联动：peer.bind(进 bound) / peer.unbind(任一终态) / error{429|timeout|bad_seq|dup_frame|schema_violation}
```

---

## 5. 缓存命中执行模型（数字示例，协议 §2 C1–C5）

设 P_b 稳定前缀（角色 system+工具 schema+输出格式）≈ 2000 tok；一帧平均 ≈ 400 tok。

```text
连续镜像（每轮同步，坏）                      on_eval 事务同步（协议默认，好）
────────────────────────────────          ────────────────────────────────────
round1 前缀2000+帧400 = 2400 (miss)        round1 前缀2000+帧400 = 2400 (miss)
round2 前缀2000+帧400 = 2400 (miss)        round2 前缀2400(hit)+帧400 = 400 (新增计费)
round3 前缀2000+帧400 = 2400 (miss)        round3 前缀2800(hit)+帧400 = 400
round4 前缀2000+帧400 = 2400 (miss)        round4 前缀3200(hit)+帧400 = 400
累计付费 ≈ 9600，0 命中                    累计付费 ≈ 2400+400*3 = 3600，命中 7200
```
→ b 越跑越省：前缀命中回升（观测 `cached_input_tokens` 单调上升即达标）。加上 C4（零增复核全命中），
终态裁决/低噪重估接近零成本。

---

## 6. 前端设计：不同角色的会话显示

### 6.1 视图模型（角色 → 显示策略）

| 角色/流 | 内容 | 显示位置 | 关键规则 |
|---|---|---|---|
| EXEC 主转录（a） | 用户消息、a 助手回复、工具调用结果 | **主面板（用户唯一"正式"聊天流）** | 现有消息渲染；不变 |
| ADVISOR（b） | 回合历史、verdict、directive、镜像水位/落后、缓存命中 | **独立 ADVISOR 面板 / 泳道（默认折叠）** | **绝不混入主转录**；结构卡（verdict 徽章 done/not_done/rework/escalate；directive 卡片） |
| goal 条 | goal 栈顶 statement/acceptance/进度、帧同步水位 | 顶部目标条 | 由 `GoalsProjection` 驱动（新增） |
| CONTROLLER 系统行 | 协议事件：peer.bind/unbind、帧同步、error{429}、审计 | 可折叠"协议泳道/调试视图" | 前端只读；429 时显示"TL 缺席→人工门"徽章 |

**显示不变量（前端侧）**：a 的转录只含 a 的采纳结果（若采纳 TL 建议，由 a 复述/落证据，不引用 TL 原文进
转录）；TL 一切产物只在 ADVISOR 面板与受信注入区显示。理由：用户视角"聊天记录 = 我 + 执行 agent"，
评审是后台动作，防止 TL 文本污染主上下文语义。

### 6.2 投影 DTO 草案（前端消费）

```go
// 扩展现有 RuntimeVisibilityProjection（见 application/contract/dto/projection.go:4）
type RuntimeVisibilityProjection struct {
    GoalSkillActive bool
    Goals   *GoalsProjection   `json:"goals,omitempty"`   // 新增：goal 条 + ADVISOR 状态
}

type GoalsProjection struct {
    Active *GoalView      `json:"active,omitempty"`
    Stack  []GoalView     `json:"stack,omitempty"`        // 下层 goal（设计 D1/D3）
    Advisor *AdvisorView  `json:"advisor,omitempty"`      // ADVISOR 泳道数据
}

type GoalView struct { ID, Statement, Acceptance, Status string; Progress int }
type AdvisorView struct {                                // b 状态（后台面板）
    PeerID    string   // sess-b
    State     string   // bound|evaluating|advisory_pending|evicted|reaped
    SyncedAt  uint64   // lastAppliedSeq（b 已看到 a 的水位）
    HeadSeq   uint64   // a 当前水位 ⇒ 前端可显示“落后 N 事件”
    LastRound *RoundView
    Verdict   *VerdictView  // 最近 gate 裁决（done/not_done/rework/escalate + reason + ref_seq）
    Cached    *CacheView    // {input_tokens, cached_input_tokens, hit_ratio}
    Absent    string        // 429/timeout/unbound ⇒ 徽章“TL 缺席”
}
type RoundView struct { At string; Output string; RefSeq uint64 }
type VerdictView struct { Decision, Reason, RefSeq, Corr string; At string }
```

### 6.3 TUI 线框（图 5，Bubble Tea——tui/ 现有 Cell{Role} 渲染，tui/state.go:14-20）

```text
┌─ goal 条 ──────────────────────────────────────────────────────────────┐
│ ◎ [goal-1] 修复 login 401 竞态 · 栈深1 · ADVISOR 落后 3 事件 · 命中72%  │
├─ 主转录流（EXEC，用户可见）─────────────┬─ ADVISOR 面板（默认折叠，可开）┤
│ you: 修一下 401 竞态                    │  [TL] ● bound │ 水位 8/11      │
│ ✦ a: 复现… 定位 token 刷新无锁            │  ┌ verdict ────────────────┐ │
│   ▶ tool: UT PASS ✓ (checkpoint)        │  │ ✗ not_done(ref=11)      │ │
│ ✦ a: 已补并发单测，提议收口              │  │ 缺负路径，补后再提        │ │
│   ⓘ [受信注入] TL 建议：补两路并发单测    │  └─────────────────────────┘ │
│ ✦ a: 补负路径单测通过 → 收口             │  ├ round 09:44 (ref=11)      │
│                                         │  └ 缓存 3600/7200 (hit 67%)   │
├─ 事件/审计泳道（折叠，协议调试）──────────┴──────────────────────────────┤
│ ◆tool.checkpoint ref=8 → b · ◆gate.verdict not_done · ◆unbind done      │
└─────────────────────────────────────────────────────────────────────────┘
```
角色渲染：ADVISOR 用独立前缀色 + 结构卡（verdict/directive），**不进主转录滚动区**；键盘可开/关面板
（对齐 CLI/TUI 交互规范，配色按角色语义：EXEC 蓝、ADVISOR 紫、系统灰）。

### 6.4 GUI 角色泳道示意（图 6，wails 前端 / events 订阅）

```text
┌ top bar ── goal 条（GoalsProjection.Active/Stack）──────────── 落后徽章 命中% ┐
├ 左 主会话（EXEC）       ├ 中 目标/计划/任务 tab          ├ 右 ADVISOR 泳道      │
│ you/a/工具调用（现状）  │ （现状 plan/task 视图）         │ 回合时间线：          │
│                          │                               │  ● bind(ref=5)       │
│  ⓘ受信注入卡             │                               │  ● checkpoint ref=8  │
│   （TL 建议 → a 采纳）    │                               │  ● verdict ✗not_done  │
│                          │                               │   —— reason 卡        │
│                          │                               │   —— 缓存命中 折线     │
├ 底 协议泳道（可折叠）bind/unbind/error{429}/审计 ───────────────────────────────┤
```
GUI 数据流：Runtime 投影（Goals/Advisor）驱动左中右面板；`/events` 订阅实时水位/回合/裁决增量；
不混转录（右面板独立渲染）。

---

## 7. 验收清单（协议不变量 → 运行时/前端断言）

| # | 断言 | 层 |
|---|---|---|
| A1 | a 转录只增；b 帧只尾部追加（ref_seq 单调，dup 幂等） | 运行时 `-race` + 单测（协议 §7.2） |
| A2 | on_eval 事务：b 每回合 ≤1 次 LLM 调用 + 一次性同步；C_b 前缀不变 | 运行时单测 + 观测 `cached_input_tokens` 单调回升 |
| A3 | a 永不等待 b：模拟 429/timeout 下 goal 流仍收敛（gate 判负/人工） | 运行时 `-race`（B4 断言） |
| A4 | verdict ref_seq ≥ 裁决水位才被采纳 | GateBridge 单测 |
| A5 | TL 产物不出现在 a 主转录（只 ADVISOR 面板/受信注入区） | 前端 demo + 事件快照断言 |
| A6 | goal 条 / ADVISOR 面板 / 协议泳道可由 `GoalsProjection` + 事件流全量重建（重放） | 前端 + 事件快照 |
| A7 | 生命周期：done/evicted/killed 三终态都能 reap 且审计留档、无孤儿 | 运行时（§图4 + 10s 护栏） |

---

## 8. 落地切片建议（POC → 主线 → 前端）

1. **POC（goals 域内）**：PeerSessionManager + Mirror(on_eval) + DirectiveBus 替换 goal MVP 同步信箱 TL；
   模拟器驱动（fake events + fake provider 返回 cached token）跑 §3 trace，断言 A1–A4/A7。
2. **主线接线**：goal_begin→spawn b 接真实装配（只读工具面）；事件账本已接；429 真实降级。
3. **前端（P2 goal 视图）**：先 TUI（Cell.Role 扩展 ADVISOR 角色 + ADVISOR 面板），再 GUI 泳道；
   DTO = §6.2；数据流 = 投影 + /events。
4. **可选上提框架**：mirror/directivebus 通用形状浅层上提（仅在你坚持"自由 A2A 轮子"时）；前端 TL
   泳道是否值 R6 事件扩展（接 dual-session §5 决策）。

## 9. 待拍板（协议 §12 + 前端）

1. mirror_mode 默认 on_eval（本设计全部按此）vs 连续推送。
2. 默认抽帧集（transcript.inc 默认不进 b）。
3. 缺席裁决 low 放行 / high 人工。
4. 前端第一落地面：TUI（bubbletea）先行还是 GUI（wails）先行？（P2 goal 视图已排 GUI，TUI 可同源复用 DTO）
5. ADVISOR 面板默认折叠 + 快捷开关是否满足，还是要求常驻（影响布局与焦点管理）。

## 10. 证据索引
- 协议：`docs/2026-09-07-seele-a2a-framework-req/ds-a2a-protocol.md`（§2 缓存 C1–C5、§4/§5 帧表、§7 一致性、
  §8 缺席、§9 生命周期、§10 错误码）。
- seelex 现成件：`seelebridge/events_unified.go:85-94,118-140`；`seelebridge/events.go:32-59`；
  `application/core/service_input.go:25` + `chat.go:118,205`；`application/core/goal/a2a.go:26`；
  `application/contract/dto/projection.go:4`；`tui/state.go:14-20`（Cell.Role）；`tui/`（bubbletea 视图）。
- Seele：`session/`（NewSession/Chat/ReActLoop）；`agent/bridge/registry_runtime.go:28-96`（只读工具面）。
- 前置文档：`same-session-governance-math.md`（不变量 I1–I8 与 B1–B6 在此具象为 A1–A7）、
  `dual-session-alternative.md`、`requirements.md`。
