# DS-A2A 会话挂接详细设计：Goal 治理循环、视图心跳与依赖倒置装配

> 日期：2026-09-08 · 状态：详细设计（目标实现）
> 前置：`README.md`（治理循环抽象与 goal 适配，已落地并提交 49b1be0/190cbc0）、
> `docs/2026-09-07-seele-a2a-framework-req/`（DS-A2A 协议/详设，权威基线）、
> `docs/2026-09-07-goal-domain-techleader/`（goal 域演进档案）。
> 本文回答四件事：goal 如何由 mainAgent 发起、由 TL 收口；治理状态如何
> 投影到视图并回心跳；多会话如何隔离；A2A 循环如何逃生；以及如何以
> 依赖倒置接入 seelebridge/session 的 goal 装配。

---

## 1. 目标语义（一句话）

`#goal` / `goal_begin` 是 **mainAgent（EXEC，a）** 的发球动作；此后治理循环
在 a 与 **ADVISOR（TL，b）** 之间按座次轮转（exec → advisor → exec → …）；
**只有 TL 的终态裁决（verdict_done / escalate_human）可以收口**——mainAgent
只能提议（`goal_propose_finish`），不能直接结束 goal。a 永不等待 b
（B4）：TL 缺席/429/超时按缺席矩阵降级，治理循环由 maxRounds / Break /
escalate 提供逃生口。

```text
mainAgent(a) ──goal_begin──► Controller(g)  ──spawn──► ADVISOR(b)
     │  ▲                                             │
     │  │ goal_update / tool.checkpoint / 提议收口      │ TL 回合
     ▼  │                                             ▼
 治理循环 Governor（exec-a → advisor-b 轮转，Round++）
     │
     ├─ verdict_done      → Controller.Finish → 视图收口 + 心跳 done
     ├─ verdict_not_done  → goal 保持 active，指令回注 a
     └─ escalate_human / 429 / maxRounds / Break → 循环逃生（视图可见 reason）
```

---

## 2. 会话隔离与状态归属

### 2.1 状态归属（谁持有 goal 栈）

治理状态分三层，**严格按会话隔离**：

| 层 | 内容 | 归属 | 持久化 |
|---|---|---|---|
| 会话视图（SessionUnit） | goal 投影 + 心跳（只读） | `session/ports.go SessionUnit` | 随快照下发，不落盘 |
| 会话状态（TaskExecution） | goal 栈 / 状态机 | `task_context`（第五栈方向，goal 域 Controller 由装配根持有） | append-only goal 上下文账本（见 §2.1b） |
| 治理域（goal/govern） | Controller + Supervisor + Governor | `application/core/goal` + `govern`（叶子包） | Store 接口（v0 内存；后续 SessionContextStore） |

设计约束（沿用 goal 包现状）：`goal.Controller` 是**会话粒度单例**
（`Depth=1` 默认），`Supervisor` 绑定一个 Controller 与一个 TL 评估器；
多会话时由装配层按 sessionID 维护 map，**不共享** Controller/AdvisorSession
（会话间零共享，与 DS-A2A B1 一致）。

### 2.1a goal 栈语义：可嵌套、逐层弹栈、最终清空

goal 不是"一个会话一个对象"，而是 **LIFO goal 栈**（已有 `goal.Stack`/
`Controller`，`Depth` 默认 1 = 会话单例，放开 `Depth>1` 即支持嵌套）：

```text
begin(父 goal)                    begin(子 goal)                …
┌─────────┐                      ┌─────────┐  子 goal 完成
│ 父(active)│  ──begin 子 goal──► │ 父(paused)│ ──verdict_done──► 弹栈
└─────────┘                      │ 子(active)│                  ▼
                                 └─────────┘             父恢复 active
                                                            │
                               … 父 goal 也完成（TL 裁决）→ 弹栈
                                                            ▼
                                                     栈空 = goal 清空
```

契约：

1. **子 goal 先完成，父 goal 后完成**：mainAgent 只能在栈顶开子 goal，
   TL 只裁决栈顶（active）goal；栈顶 `verdict_done` → 弹栈（completed 入
   History 审计）→ 父 goal 从 paused 恢复 active；
2. **完成一个弹出一个**，逐层推进直到栈空——治理循环才可整体收口
   （"goal 最终会被清空"= 栈空 + `Round` 停止推进 + 视图复位）；
3. 中途 abort 同样弹栈（reason 入审计），栈空语义一致；
4. `Depth` 放开后仍需上限（建议沿用 `MaxStackDepth` 或装配层显式配置），
   防止无限嵌套；
5. 治理会话与 goal 栈同生命周期：栈空/收口后 Controller 可复用（新 goal
   begin 重新压栈），b（ADVISOR）按现有 `unbind+reap` 语义重建；
6. **栈空是治理收口前提**：治理循环每轮检查 `Controller.Status().Active ==
   nil`——栈空即不再推进 `Round`，governor 可复用或由装配方 `Break("goal
   stack empty")` 收束（TL verdict_done 弹栈后若栈仍非空，则父 goal 恢复
   active、治理继续下一轮，而不是整体收口）。

### 2.1b goal 设置（goal 栈/帧）落 append-only 会话上下文

goal 的"设置"（每次 begin/update/finish/abort 产生的 goal 帧与栈迁移）
不是可变覆盖，而是 **append-only 会话上下文事件流**：

```text
每次 goal 变更
   └─► 追加一条 GoalContextEntry（revision/seq 单调）到会话级 append-only 账本
          { seq, kind: begin|update|finish|abort|restore,
            goal_id, status, payload(有界), at }
   └─► 恢复/审计 = 从账本头重放（重放到最新 seq 即得当前栈）
```

落点与复用：

- **参考实现**：`sessionstore/event_store.go`（会话级执行事实事件库，
  append-only、按会话分片、Seq 严格递增）与 `task_context` transcript
  （append-only 已定稿轮次）——goal 上下文账本复用同一形态，不新建第六类
  存储；
- **当前栈是账本的投影**：内存 `Controller` 栈 = append-only 账本重放的
  缓存；`store.go` 的 `Save`（全量覆盖）仅作 P0 原型，P2 换成 append-only
  账本 + 重放恢复（对齐 transcript/事件库的"append-only 事实源"哲学）；
- **目标帧读取**（Goal 帧、TL 嵌入）读投影，不直接扫账本；账本负责
  崩溃恢复/审计/可重放（A6：由事件流全量重建 goal 条与治理面板）。

### 2.2 视图投影（session → application → GUI/TUI）

新增只读投影结构（`application/contract/dto` 或 `model`）：

```go
type GoalGovernanceView struct {
    Active        bool               `json:"active"`
    GoalID        string             `json:"goal_id,omitempty"`
    Title         string             `json:"title,omitempty"`
    Status        string             `json:"status,omitempty"`      // goal 状态
    Round         int                `json:"round"`                 // 治理轮次
    CurrentSeat   string             `json:"current_seat,omitempty"` // exec-a / advisor-b
    PeerState     string             `json:"peer_state,omitempty"`   // b 生命周期
    LastDirective string             `json:"last_directive,omitempty"`
    Broken        bool               `json:"broken"`
    BreakReason   string             `json:"break_reason,omitempty"`
    HeartbeatAt   int64              `json:"heartbeat_at,omitempty"`
    HeartbeatSeq  uint64             `json:"heartbeat_seq"`          // 单调心跳序号
}
```

写入路径（复用现有 `publishRuntimeProjections`）：

```text
goal.Controller/Governor/Supervisor（会话域）
   │ Snapshot（读面深拷贝）
   ▼
application Service.components.goal（按会话持有）
   │ 组装 GoalGovernanceView + 心跳（seq++/at=now）
   ▼
RuntimeVisibilityProjection（或新 GoalViewProjection）
   ▼
SessionUnit.Runtime（G1 会话槽）
   ▼
Snapshot.runtime.goal_governance
   ▼
GUI renderGoal / TUI 面板
```

**心跳**：治理每推进一个回合（exec/advisor Act 结束）或 goal 状态迁移，
装配方在 `publishRuntimeProjections` 时机更新 `HeartbeatSeq/HeartbeatAt`；
前端把 seq 单调视为"治理活着"，超过 `N` 秒无新 seq 显示
`governance stalled`（前端只读展示，不做业务决策）。

---

## 3. 页面效果（字符画）

### 3.1 GUI 右栏「目标 + 治理」面板（目标实现）

```text
┌─ 右侧栏 · 工作台 ─────────────────────────────────────────────┐
│  目标 ● GOAL                                   [ 治理 ][ 工作表 ]│
│  ┌──────────────────────────────────────────────────────────┐  │
│  │ Title   审查 Seelex goal 域与 thesis 开题报告             │  │
│  │ Status  active          Round 3 / ∞       心跳 ● 0.4s    │  │
│  │ 座次    ① exec-a  ▸ ② advisor-b  ▸（回 ① …）             │  │
│  │ ── 治理泳道（可折叠）───────────────────────────────     │  │
│  │  R1 advisor  checkpoint_ok   “补负路径单测”  corr-1      │  │
│  │  R2 advisor  verdict_not_done“缺证据：xx”    corr-2      │  │
│  │  R3 exec     goal_update     “已补 xx 与单测”            │  │
│  │  当前：advisor-b 评估中…（⏳ 最长 10s）                    │  │
│  │ ── TL 最近指令 ──────────────────────────────────────     │  │
│  │  [correct] 先补负路径单测再收口（P1）                      │  │
│  │  收口：✋ 仅 TL 可裁决（mainAgent 只能提议）              │  │
│  └──────────────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────────────┘
```

关键状态变化：

```text
无 goal          → 整块隐藏（复用 hasContent 逻辑）
goal active      → Title/Status/Round/座次/心跳 常显，泳道展开
TL 回合中        → 当前座次高亮 + “评估中…” 心跳转圈
verdict_done     → Status=completed、坏境收口、心跳 done、面板复位
escalate/429     → 面板显示 break reason + “转人工/缺席”横幅，goal 保持可见
```

### 3.2 会话内主转录（EXEC 可见）

mainAgent 消息流与现有转录一致，TL 产物**不进主转录**，只以受信注入区出现
（对齐 DS-A2A：不进 a 转录主历史）：

```text
[user]    #goal 审查 goal 域与 thesis，给出裁决与下一步
[assistant] 已注册目标 g-1（title=…）……（mainAgent 开始干活）
[tool]   goal_update（进度打点）          ← mainAgent 主动推进
[assistant] ……（继续执行）
[注入]   〔TL 指令 corr-2〕先补负路径单测   ← 下一轮前受信注入（灰底/边框）
[user]   继续
[assistant] ……（依指令修正）
[tool]   goal_propose_finish
[注入]   〔TL verdict corr-4〕done：全绿，收口
[系统]   goal g-1 completed ✓（面板复位）
```

---

## 4. 后端数据流（一次治理回合）

```text
 mainAgent 回合（ReAct loop）                     ADVISOR 回合（TL）
 ──────────────────────────                      ─────────────────────
 工具 goal_update / task 打点
   │ 会话装配层经 goal_port.Notify
   ▼
 Supervisor.execSeq++（a 账本）
   │ 触发策略（eval_window / 关键信号）
   ▼  on_eval：Mirror 补落后帧（goal.update 等）
 AdvisorSession.appendFrame（ref_seq 单调）
   ▼
 renderEmbed（锚点+帧+自身回合）→ TL 评估器（LLM 或 stub）
   ▼
 TLDirective{kind, content, corr} → DirectiveBus（cap32）
   ▼
 装配层 DrainDirectives → 下一轮受信注入 EXEC
   ▼
 Governor.Round++ / 快照 → 视图投影 + 心跳
```

治理循环驱动（`govern.Governor.Next`）由会话装配层在合适时机调用：

```text
装配层每轮选择：
  A) 回合边界自动推进（ChatStream 返回后 OnIterationComplete 钩子）
  B) 工具显式推进（headless goal_gov_next / 未来 goal 面板按钮）
  C) 关键信号立即推进（context_compacted / approval_asked / terminal）
```

---

## 5. A2A 循环逃生（B4/B6 + 护栏）

| 逃生口 | 触发 | 语义 |
|---|---|---|
| `verdict_done` | TL 裁决收口 | `Controller.Finish` → 出栈 + `peer.unbind(done)` + reap |
| `escalate_human` | TL 越权/无法判定 | goal 保持 active，状态 `waiting_human`，转人工 |
| `429/timeout` | TL 回合失败 | `peer.unbind(evicted)`，a 按缺席矩阵继续（低放行/高人工） |
| `maxRounds` | 治理循环护栏 | `Next` 返回 false，视图可见 “已达轮次上限” |
| `Break(reason)` | 外部中断/用户 stop/预算耗尽 | governor 收束，reason 入视图与审计 |
| 会话关闭 | ChatStream ctx 取消 | Supervisor 停议程 + reap b（B6），无孤儿 |

实现要点：逃生必须是**装配层显式动作**，goal 域只产生
`TurnAction.BreakLoop` / `DirectiveBreaksLoop` 判定，不停死循环
（`maxRounds` 与 ctx 取消由调用方保证）。

---

## 6. 依赖倒置：接入 seelebridge/session 的 goal 装配

### 6.1 问题：goal 不能 import session，session 也不该 import goal

`goal`/`govern` 是纯领域叶子包；`seelebridge/session` 与 `application/core`
都是装配/适配层。若让 session 直接 import goal，会引入领域包反向依赖
组合根的环。因此接入采用 **依赖倒置**：定义会话装配端口（在组合根侧），
goal 域只实现端口背后的小接口。

### 6.2 端口（装配侧定义，goal 侧实现）

```go
// session 挂接端口（装配层声明）
type GoalGovernorPort interface {
    Begin(ctx context.Context, req goal.BeginRequest) (*goal.GoalRecord, error)
    Update(ctx context.Context, req goal.UpdateRequest) (*goal.GoalRecord, error)
    ProposeFinish(ctx context.Context, req goal.FinishRequest) (goal.FinishProposalResult, error)
    Notify(ctx context.Context, sig goal.TLEvalSignal) error
    Next(ctx context.Context) (bool, error)
    Snapshot() (goalGovernanceView, error)   // 组装视图
}
```

### 6.3 装配（main / seelebridge 组合根）

```text
main 启动
  ├─ 装配 real TLEvaluator（账号池 → LLM client，headless 冒烟已证路径）
  ├─ 为每个会话（懒）创建：goal.Controller + Supervisor + Governor
  │    └─ Controller 按 sessionID 隔离（map，生命周期随会话）
  ├─ seelebridge.Runtime.RegisterTool 注册 goal 工具族：
  │    goal_begin / goal_update / goal_status / goal_propose_finish
  │    （+ headless：goal_tl_eval / goal_gov_next / goal_gov_snapshot）
  └─ ChatStream 边界接线（P0-wiring）：
       OnIterationComplete → goal_port.Next / Notify
       DrainDirectives → 受信注入（复用 injectPendingSubagentContexts 同类）
```

依赖方向：

```text
goal/govern（叶子，无 seelex 依赖）
    ▲ 实现端口
session 挂接端口（装配层声明）
    ▲ 注入
main / seelebridge 装配（真实 LLM、账号池、工具面）
```

这样 goal 不 import session；session 不 import goal 具体类型（只依赖端口）；
真实装配集中在组合根，替换 TL 评估器（stub/真实 API）不需要动领域包。

### 6.4 阶段性（避免一次改穿）

1. **P0（已完成）**：govern 抽象 + goal adapter + headless 真实 API 测试面；
2. **P1（本文目标）**：端口定义 + main 装配 + goal 工具注册 + 视图投影/心跳；
3. **P2**：append-only goal 上下文账本 + 会话恢复重建 Governor（重放恢复）；
4. **P3**：GUI/TUI 面板正式渲染（字符画 §3 形态）。

---

## 7. 测试与验收

真实 API 验收（环境变量门控，不默认跑）：

```text
$env:SEELEX_LIVE_SMOKE='1'
go test ./tmp/goal-tl-live-smoke -v -count=1
```

断言覆盖：

- mainAgent 发起（goal_begin）与 TL 收口（propose_finish）全链路合法；
- TL 裁决三态（done/not_done/escalate）均不破坏状态机一致性；
- 治理快照（round/current/broken）与 goal 状态、TL peer 状态一致；
- B4 缺席：429/超时 → 不阻塞 a；maxRounds/Break → 逃生可观测。

单元层（默认跑）：

```text
go test ./application/core/govern/ ./application/core/goal/ -count=1
```

---

## 8. 开放问题（待拍板）

- goal 栈"设置"账本的落点：扩展 `sessionstore/event_store.go`（会话级
  append-only）vs `SessionContextRecord` 追加 append 段 vs 独立 goal 通道
  —— 决策后定 schema；§2.1b 暂建议复用事件库形态；
- 心跳推给前端的方式：随 Snapshot 全量 vs 单独 `goal.heartbeat` 事件；
- mainAgent 显式 `#goal` 与工具 `goal_begin` 是否都作为发球入口（两者等价）；
- `peer.unbind` 后 TL 会话对象删除，治理快照是否保留 b 回合审计（协议 D3）。

---

## 9. 分阶段改动面与验收用例（实施清单）

> 本节把每个 P 的**波及文件**与**验收用例**落到仓库路径。文件按"新增 /
> 修改 / 删除"标注；用例分单元层（默认 `go test`）与真实 API 层
> （`SEELEX_LIVE_SMOKE=1`，非默认）。改动原则：goal/govern 保持叶子包
> 零 seelex 依赖；session 只依赖装配端口；组合根（main）负责接线。

### 9.1 P0：govern 抽象 + goal 适配 + headless 测试面（已完成，提交 49b1be0/190cbc0）

**波及文件（已提交）**

| 文件 | 动作 | 说明 |
|---|---|---|
| `application/core/govern/governance.go` | 新增 | Seat/Governor/TurnAction/Snapshot 抽象与默认实现 |
| `application/core/govern/governance_test.go` | 新增 | 治理循环单测（座次/断环/护栏/外部中断/错误） |
| `application/core/govern/README.md` | 新增 | 包生态位与索引 |
| `application/core/goal/adapter.go` | 新增 | `DirectiveBreaksLoop`/`NewAdvisorSeat`/`NewTurnGovernorForDSA2A` |
| `application/core/goal/adapter_test.go` | 新增 | 治理驱动 TL 回合 / verdict_done 收口 / TL 缺席 |
| `application/core/goal/headless.go` | 修改 | `WithGovernor` + `goal_gov_next/snapshot/break` RPC |
| `application/core/goal/headless_gov_test.go` | 新增 | headless 治理 RPC 单测 |
| `application/core/goal/README.md`、`application/core/README.md` | 修改 | 生态位与索引登记 |
| `docs/2026-09-08-govern-loop/README.md`、`docs/research/...` | 新增/修改 | 设计/调研 |

**验收用例**

| 用例 | 层 | 命令 |
|---|---|---|
| `TestTurnGovernorAlternatesSeatsByOrder` / `...BreaksWhenSeatRequests` / `...MaxRoundsStopsLoop` / `...ExternalBreak` / `...SeatErrorStops` | 单元 | `go test ./application/core/govern/ -count=1` |
| `TestGovernorDrivesAdvisorRound` / `TestGovernorBreaksOnVerdictDone` / `TestAdvisorSeatDisabledReportsTLDisabled` | 单元 | `go test ./application/core/goal/ -run 'TestGovernor|TestAdvisorSeat' -count=1` |
| `TestHeadlessGovernRPC` / `TestHeadlessGovernUnwired` | 单元 | `go test ./application/core/goal/ -run TestHeadlessGovern -count=1` |
| `TestTLRealLLMRound` / `TestTurnGovernorRealLLM` / `TestHeadlessGovernRealLLM` / `TestMainAgentToTLFinishRealLLM` | 真实 API | `$env:SEELEX_LIVE_SMOKE=1; go test ./tmp/goal-tl-live-smoke -v -count=1` |

---

### 9.2 P1：会话挂接（端口 + 装配 + goal 工具 + 视图投影/心跳）

**波及文件**

| 文件 | 动作 | 说明 |
|---|---|---|
| `application/contract/dto/projection.go` | 修改 | `RuntimeVisibilityProjection` 增 `GoalGovernance *GoalGovernanceView`（或独立投影类型） |
| `application/model/state.go` | 修改 | `RuntimeState`/`SessionRuntime` 增 `GoalGovernance *GoalGovernanceView`；`cloneRuntimeState` 深拷贝 |
| `application/core/view_state/coordinator.go` | 修改 | 组装 goal 治理视图进会话投影 |
| `application/core/session_scope.go` | 修改 | `sessionRuntimeOf` 带 goal 治理视图 |
| `application/core/service_components.go` | 修改 | `serviceComponents` 增 `goal *governance.Coordinator`（会话级持有器） |
| `application/core/service_assembler.go` | 修改 | 装配 goal 协调器（注入 Runtime/账号面） |
| `application/core/runtime_projection.go` | 修改 | `publishRuntimeProjections` 携带治理视图 + 心跳（seq/at） |
| `application/core/service.go` 或新 `goal_service.go` | 新增 | Service 侧 goal 方法面：Begin/Update/ProposeFinish/Notify/Next/Snapshot（按会话路由） |
| `application/core/goal/ports.go`（规划新增，现不存在） | 新增 | 会话挂接端口 `GoalGovernorPort`（装配侧声明；goal 域实现） |
| `seelebridge/runtime_tools.go` | 修改 | `registerGoalTools()`：goal_begin/update/status/propose_finish 注册（main agent 工具面） |
| `seelebridge/tools/policy.go` | 修改 | `isGoalTool` 门控（goal 激活/治理存在时对主代理可见） |
| `main.go` | 修改 | `initApplication` 后装配 goal 协调器 + 真实 TLEvaluator（账号池）+ 注册 goal 工具（`registerTaskTerminalTools` 同款模式） |
| `gui/headless.go` | 修改 | dispatch 增 `goal.<method>` 透传（接线位注释已预留） |
| `application/core/goal/headless.go` | 修改 | `WithGovernor` 已有；增 `goal_gov_view` 或复用 `SnapshotOf` |
| `tmp/goal-tl-live-smoke/smoke_test.go` | 修改 | P1 会话挂接冒烟（若独立服务形式则新增） |
| `docs/gui/`、`docs/2026-09-08-govern-loop/design.md` | 修改 | GUI 视图/协议字段同步 |

**验收用例（P1 核心：治理由会话装配驱动、视图可心跳）**

| 用例（建议命名/所在文件） | 断言 |
|---|---|
| `application/core/goal/ports_test.go`：`TestGoalPortBeginProposeFinishIsolation` | 两会话各自 Begin/Update 不串（sessionA 的 goal 不污染 sessionB） |
| `application/core/view_state` 或 `service_snapshot_test.go`：`TestSessionSnapshotCarriesGoalGovernanceHeartbeat` | Snapshot.Runtime 含治理视图，heartbeat_seq 单调递增 |
| `seelebridge`（`goal_tools_test.go`）：`TestGoalToolsRegisteredAndGated` | goal 工具注册；未激活/非 goal 会话时主代理不可见（policy） |
| `main` 装配测试/headless：`TestHeadlessGoalDispatchBeginToView` | `goal_begin` → 治理视图 active、goal 状态一致 |
| `seelebridge/session`（若 ChatStream 边界接 OnIterationComplete）：`TestGovernorAdvancesOnChatTurn` | 一次 main agent ChatStream 后 governor.Round≥1、TL 指令入注入队列 |
| 真实 API：`tmp/.../TestSessionGovernRealLLM`（P1 新增） | 会话级装配下 mainAgent→TL 端到端，视图心跳可观测 |

### 9.3 P2：持久化与恢复（goal 栈第五栈）

**波及文件**

| 文件 | 动作 | 说明 |
|---|---|---|
| `sessionstore/session_context.go` | 修改 | `SessionContextRecord` 增 `GoalStack []GoalFrame`；`PushGoal/CloseTopGoal/GoalStackSnapshot`；`SessionContextSchemaVersion` bump（v1→v2，校验旧记录兼容迁移或显式拒绝） |
| `sessionstore/session_context_test.go` | 修改 | 新增 goal 栈持久化/恢复/版本测试 |
| `sessionstore/context_state.go` 或复用 | 修改 | 若字段编码走独立 blob 无需改（context 通道已隔离） |
| `application/core/goal/store.go` | 修改 | 增 `ContextStateStore` 适配（把 goal.Controller 栈落 SessionContextStore）或 `Store` 实现 |
| `seelebridge/runtime_session.go` | 修改 | session bundle 装配/恢复时重建 goal Controller（`Reload`） |
| `application/core/goal/controller.go` | 修改 | `Reload` 与 SessionContextStore 对齐（已具备 Store 接口，需装配 Store） |
| `seelebridge/ports.go` / `runtime_context.go` | 修改 | `sessionContextStore()` 旁路或装配点把 goal store 接到会话 |
| `sessionstore/event_store.go`（goal 账本扩展） | 修改 | append-only goal 上下文账本（若第五栈走事件流而非 state blob） |
| `application/core/goal/controller.go`（栈深/嵌套） | 修改 | `Depth` 放开与上限装配；`finishOrAbort` 弹栈→父恢复已实现（补断言） |
| `application/core/goal/controller_test.go` | 修改 | 嵌套压栈/逐层弹栈/栈空清空断言 |
| `docs/2026-09-08-govern-loop/design.md`、`sessionstore/README.md` | 修改 | 第五栈语义文档 |

**验收用例**

| 用例 | 断言 |
|---|---|
| `sessionstore`：`TestGoalStackPersistReload` | Begin→Persist→新 SessionContextStore Load→栈/状态一致 |
| `sessionstore`：`TestGoalStackSchemaBumpRejectsOld` | v1 记录在 v2 下显式失败或迁移成功（按决策） |
| `goal`：`TestControllerReloadFromContextStore` | Controller.Reload 后 active goal/progress/directives 恢复 |
| `seelebridge`：`TestResumedSessionRebuildsGoalGovernor` | 恢复会话后治理 round/座次与持久化前一致 |
| fork 隔离：`TestForkSessionDoesNotInheritGoalStack` | 子会话 fork 不带父 goal 栈（对齐 plan/task 四栈语义） |
| `goal`：`TestNestedGoalsPopLIFOUntilEmpty` | 父→子 begin；子 finish→父恢复 active；父 finish→栈空、History=2、治理收口 |
| `goal`：`TestGoalContextAppendOnlyReplay` | begin/update/finish 各追加一条账本记录；按账本重放可重建栈与终态（含审计） |
| `goal`：`TestGoalStackDepthBound` | Depth 放开后仍受上限约束，超限 begin 拒绝 |
| `sessionstore`：`TestGoalContextAccountIsSessionScopedAppendOnly` | 两会话账本互不串写；记录只追加不回改（Seq 单调） |

### 9.4 P3：前端正式渲染（GUI/TUI 治理面板）

**波及文件**

| 文件 | 动作 | 说明 |
|---|---|---|
| `gui/frontend/dist/index.html` | 修改 | goal 面板结构扩展（状态行/座次/泳道/心跳/断环横幅） |
| `gui/frontend/dist/app.js` | 修改 | `renderGoal` 消费 `runtime.goal_governance`；心跳 seq 轮询/停滞提示 |
| `gui/frontend/dist/styles.css` | 修改 | 治理泳道/心跳/断环样式 |
| `gui/frontend/dist/snapshot-shape.js` | 修改 | `SESSION_RUNTIME_KEYS` 增 `goal_governance` |
| `gui/frontend/dist/*.test.mjs` | 修改/新增 | snapshot-shape/渲染测试 |
| `tui/state.go` / `tui/view.go`（若 TUI 先行） | 修改 | 治理面板（默认折叠） |
| `application/contract/dto/`（视图模型） | 修改 | 若 GUI 需分页/详情再扩展 |
| `docs/gui/` | 修改 | GUI 协议/DOM 文档同步 |

**验收用例**

| 用例 | 断言 |
|---|---|
| `snapshot-shape.test.mjs`：`goal_governance` 在 SessionRuntime 键集且不泄漏到进程键 | 字段归属正确 |
| `app.test.mjs` / 渲染单测：`renderGoal` 无 goal 隐藏、有 goal 显示状态/轮次/心跳 | DOM 行为符合字符画 §3.1 |
| 手工验收（GUI）：`#goal ...` 后右栏出现治理泳道，回合推进心跳 seq 增长，断环显示 reason | 与字符画一致 |
| E2E（Playwright/headless GUI，如 `gui/frontend/dist/consistency.test.mjs` 模式）：治理全流程驱动 | mainAgent 发起→TL 裁决→面板收口 |

### 9.5 依赖与回归护栏（各 P 通用）

- 每次 P 改动后跑：`go build ./... && go test ./application/core/govern/ ./application/core/goal/ -count=1`；
- P1+ 追加：`go test ./seelebridge/ ./application/core/... -count=1`（目标相关）；
- 前端 P3 追加：`node --test gui/frontend/dist/*.test.mjs`；
- 真实 API 验收（P0 已固化，P1+ 每阶段重跑）：`$env:SEELEX_LIVE_SMOKE=1; go test ./tmp/goal-tl-live-smoke -v -count=1`。

> 文件清单为**实施起点**，实际改动可能随代码现状微调；新增文件以
> "需要时才引入"为原则（例如 goal 协调器若可并入既有 serviceComponents
> 目录则不再新增包）。验收用例为必须满足的最小集，可扩展不可缩减。

---
