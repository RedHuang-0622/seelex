# govern — 多代理治理循环抽象

## 生态位

把"多代理按回合次序共同治理一个会话/目标"沉淀为可复用的编排原语。
主要调用方：`application/core/goal`（DS-A2A adapter，把 EXEC/TL 语义翻译为
治理座位）、未来 seelebridge/application 装配层的多代理会话治理。本包只
定义**座次、轮次、行动与断环**契约，不绑定任何单角色实现。

## 职责与非职责

职责：

- `Seat`：一个治理参与者在循环中的身份与行动入口（Name/Kind/Act）；
- `TurnAction`：一次行动的最小结果（是否请求断环 + 一句话 Note）；
- `Governor`：主持人/发牌者——座次注册、逐座推进、轮次计数、主动断环；
- `Snapshot`/`SnapshotOf`：治理循环读面（审计/前端投影素材）。

非职责：

- 不定义具体代理协议（TLDirective/信号/审批都不在本包，属 goal 域）；
- 不持有会话历史/帧账本/LLM 客户端（装配方注入 Seat 动作）；
- 不负责 goal 栈或持久化（那是 Controller/sessionstore 的职责）。

## 目录或文件结构

| 文件 | 职责 |
|---|---|
| `governance.go` | 回合制治理循环抽象与默认实现（NewTurnGovernor） |
| `governance_test.go` | 座次交替 / 断环 / 轮次护栏 / 外部中断 / 错误透传单测 |

## 核心实现

`NewTurnGovernor(seats, maxRounds)` 返回固定座次的顺序推进循环：

```text
第 n 轮：seat[0].Act → seat[1].Act → … → seat[k].Act（按注册顺序）
           └─ 任一 TurnAction.BreakLoop=true 时立即收束
           └─ maxRounds>0 且已满轮数时收束（防死循环）
外部：Governor.Break(reason) 可随时主动停（幂等）
```

`Governor.Next` 每次让当前座位行动并把发牌权交给下一座位；一轮结束后
`Round()` 自增。错误透传不吞（座位失败由调用方决定缺席/升级策略）。

## 数据流或生命周期

```text
装配方 ──(Seat 列表)──► NewTurnGovernor ──(逐座 Next)──► 座位 Act
                                                          │
          ◄── TurnAction{BreakLoop, Note} ────────────────┘
断环原因 / 轮次 / 当前座位 ──► Snapshot（审计/前端）
```

## 依赖方向

- 本包不依赖 goal/session/seelebridge（纯编排原语）；
- `application/core/goal/adapter.go` 依赖本包（goal → govern 单向）；
- 装配层可同时依赖本包与 goal 包，不产生环。

## 并发、存储、安全或错误语义

- `turnGovernor` 未内置锁：单治理循环由单驱动 goroutine 推进；
  多 goroutine 并发调用 Next 需要外部串行（与 goal Supervisor 锁序一致：
  Supervisor.mu 已保证 TL 回合串行）。
- 无持久化：断环原因与座次仅为内存读面；需审计时由上层经事件账本落盘。
- 座位 Act 错误原样透传，不静默吞掉——B4 缺席矩阵由装配方决策。

## 扩展方式

新增治理角色（审计员、评审员、审批代理等）：

1. 实现 `Seat`（Name/Kind/Act），动作内调用自己的回合逻辑；
2. 经 `NewTurnGovernor`（或 goal 包 `NewTurnGovernorForDSA2A`）加入座次；
3. 若角色拥有"终局裁决权"，在 Act 中设置 `TurnAction.BreakLoop`。

协议语义变更（新增 DirectiveKind 断环规则）只需改
`goal.DirectiveBreaksLoop`，治理循环骨架无需动。

## Review 指南

- 断环语义是否与真实协议一致（如 verdict_not_done 不该断环、escalate 应断）；
- 座位失败是否被静默吞掉（应当透传由装配方按缺席矩阵处理）；
- maxRounds 护栏是否存在，防真实 LLM 死循环；
- 快照字段是否足够审计（轮次/座次/断环原因）。

## 测试与验证

```text
go test ./application/core/govern/ -count=1
go test ./application/core/goal/ -run 'TestGovernor|TestAdvisorSeat' -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### governance.go

- `func ShouldBreak(actions []TurnAction) bool` — ShouldBreak 是每轮结束时的断环判定：任一参与者请求打破循环即停。
- `func NewTurnGovernor(seats []Seat, maxRounds int) Governor` — NewTurnGovernor 构造一个固定座次的回合制治理循环。
- `func SnapshotOf(g Governor) Snapshot` — SnapshotOf 从任意 Governor 快照出可 JSON 化的读面。
- `func newTurnGovernor(seats []Seat, maxRounds int) *turnGovernor`
- `func (g *turnGovernor) AddSeat(seat Seat) error`
- `func (g *turnGovernor) Next(ctx context.Context) (bool, error)`
- `func (g *turnGovernor) Current() string`
- `func (g *turnGovernor) Seats() []string`
- `func (g *turnGovernor) Round() int`
- `func (g *turnGovernor) Break(reason string)`
- `func (g *turnGovernor) Broken() (bool, string)`

### governance_test.go

- `func newStubSeat(name string, kind AgentKind, calls ...TurnAction) *stubSeat`
- `func (s *stubSeat) Name() string`
- `func (s *stubSeat) Kind() AgentKind`
- `func (s *stubSeat) Act(_ context.Context) (TurnAction, error)`
- `func (s *stubSeat) acted() int`
- `func TestTurnGovernorAlternatesSeatsByOrder(t *testing.T)`
- `func TestTurnGovernorBreaksWhenSeatRequests(t *testing.T)`
- `func TestTurnGovernorMaxRoundsStopsLoop(t *testing.T)`
- `func TestTurnGovernorExternalBreak(t *testing.T)`
- `func TestTurnGovernorSeatErrorStops(t *testing.T)`
- `func (f *failingSeat) Name() string`
- `func (f *failingSeat) Kind() AgentKind`
- `func (f *failingSeat) Act(context.Context) (TurnAction, error)`
