# 会话内多 Agent 治理：数学模型调研、Seele 现状模型与差距（Diff）

> **状态：研究档案（方案 A 候选，架构未采纳）。** 本文的单会话共享 σ 方案（M 模型表 + §3 目标模型 + §5 差距）
> 是候选分析；架构决策已转向**双会话 DS-A2A**（`ds-a2a-protocol.md`），但 §4 Seele 现状模型与 §7 证据索引
> 仍是**有效事实基线**（协议/详设复用其"框架现状"结论）。统一基线见 `README.md`。

> 定位：`docs/2026-09-07-seele-a2a-framework-req/` 系列第 2 篇，承接 `requirements.md`（R1–R8 需求单）。
> 本文件回答三个问题：
> 1. **需要的数学模型是什么**——"多 agent engine 治理同一会话"的完整建模（业界调研 + 本设计选型）；
> 2. **目前 Seele 的数学模型是什么**——从代码证据归纳（本地 HEAD = GitHub HEAD `69374b7`，与 seelex pin 的 v0.1.2 同源且领先）；
> 3. **Diff**——Seele 相对目标模型缺什么原语、要补哪些内容，每项归属框架或产品并回链 `requirements.md` 的 R 编号。
> 状态：研究档案（方案 A 未采纳；顶部横幅已注明）。所有"现状"断言均带 `路径:L行` 证据；推理部分以"H"标注假设。

---

## 1. 问题域与设计原则

**治理同一会话（same-session governance）**：多个 agent engine 围绕**一个共享会话状态**工作——不是
"每人一份隔离会话再合并"（那是跨会话 fork 路线，Seele 已有），而是同一份转录/目标/计划/任务在多个
角色间流转：谁有权改、谁能看、谁何时发言、谁决定"结束"、冲突/故障如何收敛。

### 1.1 会话状态的一等公民（目标域）
记会话状态为 σ，至少含：
- 转录 ℳ（messages，append-only 事件日志的可视面）、
- 目标栈 𝒢（goal stack）、计划 𝒫（plan DAG）、任务 𝒯（task 终态机）、
- 工作区/工具可见性（workspace scope、tool visibility）、
- 投影 𝓥（frontend/RuntimeVisibilityProjection）。

### 1.2 设计原则（承接用户裁定 + 09-07 架构 D1–D8）
1. **单写者**：任一时刻 σ 的"正式版本"只允许一个 writer（main engine）推进；其余 agent 是
   advisor/worker，只能产出证据、指令、verdict，经有界信道交给 writer 决策落盘。
2. **共享 ≠ 无隔离**：worker（子代理）写的是 fork 出的不可变基线快照副本（Seele `ParentSnapshot` 已有此语义）；
   advisor 只有只读视图。
3. **生命周期有 owner**：谁 spawn 谁 reap；孤儿由监督者回收；超时/预算用 lease + heartbeat 判定（Seele event
   已有 HeartbeatLease 可复用）。
4. **结束是自动机不是模型自觉**：终态 gate 是显式 verdict 自动机（goal 域 MVP 已验证方向）。
5. **确定性可重放**：σ 可折叠（fold）自 append-only 事件日志 ℒ；同一日志 + 同一议程 ⇒ 同一行为序列。
6. **有界性**：所有信箱/队列/回合预算有界；溢出计数不阻塞（seelex `seelactor`/mailbox 已验证语义）。

---

## 2. 业界与理论模型调研（选型依据）

> 数学候选按"是否直接支撑同会话治理"归类。符号为统一记号（§3 复用）。理论出处为主（CSP/actor/session
> types/event sourcing/statecharts 等为学科经典，不作逐行引用；工程实践给链接）。

| # | 模型 | 数学核心（简述） | 支撑同会话治理的点 | 不适配点 | 采用？ |
|---|---|---|---|---|---|
| M1 | **有限状态机/状态图**（FSM/Harel statecharts） | 状态集 S、事件 E、转移 δ:S×E→S，层次化 + 并发子状态 | 生命周期/终态 gate 天然表达：agent、task、goal、会话都是状态机 | 单机语义，无并发数据面 | ✅ 采用（生命周期/verdict 层） |
| M2 | **Actor 模型**（Hewitt/Agha） | 实体=actor，只经异步 mailbox 通信；地址、行为、无共享可变态 | 每个 agent=actor，信箱有界、消息驱动、故障隔离 | 需要共享会话转录时，actor 需额外"订阅/账本" | ✅ 采用（通信层） |
| M3 | **CSP**（Hoare） | 进程 + 通道；`P ‖ Q`、rendezvous、内部选择；trace/refusal 语义、死锁自由证明 | 进程组合/死锁分析的形式语言；"谁等谁"可判 | rendezvous 阻塞不适合 LLM 慢回合 | ⚠️ 参考（死锁论证借用 trace；通信用异步） |
| M4 | **π-calculus / session types**（Milner/Honda） | 移动进程；channel 类型化协议（session types 定序/选择/递归） | 为 agent 间协议（request/verdict/handoff）定型化 | 类型系统过重，LLM 语义不可静态定型 | ⚠️ 借鉴（协议 = 类型化有限对话，不做静态证明） |
| M5 | **事件溯源 + fold**（event sourcing） | 命令→事件 e；σ = fold(ℒ)；投影可重建、可重放 | 会话转录即账本；单写者把派生状态写进投影，顾问读快照 | 全量溯源成本（可用 checkpoint 截断） | ✅ 采用（会话账本层） |
| M6 | **回合制/发言权（turn-taking）**（AutoGen GroupChat、对话系统经典） | 序列 ⟨m₀,…,mₖ⟩；speaker selector π(m_history)→agent；agent 按序发言 | "同一会话多 agent 谁发言"就是议程函数；可混单写者（写者回合）与顾问回合 | 若无约束会退化为全体广播、上下文风暴 | ✅ 采用（议程层，约束窗口/预算） |
| M7 | **监督者模式（supervisor）**（LangGraph supervisor、Anthropic/OpenAI handoff 实践） | supervisor agent 是路由器：持有 worker 工具表，决策 next；或显式 handoff 转移控制权 | 单一决策点防止竞写；与"单写者"正交且互补 | supervisor 仍是 LLM 判断，需 gate/护栏兜底 | ✅ 采用（角色层，护栏化） |
| M8 | **事务/锁与 MVCC**（数据库理论） | 可串行化、二阶段锁、多版本读、快照隔离 | 若真允许多写者，需事务语义 | 目标模型是单写者，事务**不必要**（回避而非解决） | ❌ 不采用（保持单写者，避免给框架加事务面） |
| M9 | **fork-join / 屏障**（并行计算） | 并行段 + 汇合屏障；join 策略（全部/成功子集） | worker 并行段与合并语义现成（Seele forkexec 即此） | 是"分而治之"不是"同会话治理" | ✅ 复用（worker 层，已有） |
| M10 | **租约/心跳/监督树**（Erlang/OS、分布式租约） | 租约 T 过期即回收；heartbeat 续租；监督树声明谁重启谁 | 生命周期 owner + 孤儿回收 + 慢 agent 判定 | — | ✅ 采用（生命周期层，Seele event HeartbeatLease 可复用） |
| M11 | **Petri 网 / BPMN 工作流** | 库所/变迁标记（marking）表示资源与互斥 | 可建模"谁持写权"的资源/互斥 | 建模重，图上手成本高 | ❌ 不采用（仅作分析直觉） |
| M12 | **多智能体 RL/博弈**（MDP/POMDP、随机博弈） | 策略 π(a|s)；联合策略、均衡、回报 | 只适合"学策略"，不适合工程治理 | LLM 治理重点是约束不是优化 | ❌ 不采用 |
| M13 | **A2A 协议（Google/Linux 基金会）** | AgentCard（能力声明）+ task 生命周期（submitted/working/input-required/completed/failed/canceled）+ JSON-RPC message | 跨进程 agent 互操作标准：信封、task 状态机、artifact 传递 | 面向远程/异构互操作，会话内太重 | ⚠️ 预留（远程 transport 时映射，进程内用轻量同构） |

**组合选型（本设计）**：以 **M2 actor 通信 + M5 事件溯源会话账本 + M1 状态图（角色/任务/终态自动机）+
M6 回合议程 + M7 护栏化监督 + M10 租约** 为骨架，worker 并行复用 M9（Seele forkexec），远程扩展预留 M13。
明确**不引入 M8 事务**（单写者使多写并发问题不存在，符合"用架构消除而不是用机制解决"）。

---

## 3. 目标数学模型（需要的模型，详细设计）

### 3.1 符号表
| 记号 | 含义 |
|---|---|
| `𝒮` | 会话（运行时对象；1 会话 = 1 治理域） |
| `ℒ = ⟨e₀,…,e_{k-1}⟩` | 会话事件日志（append-only、带全局 seq） |
| `σ = fold(ℒ)` | 派生会话状态（转录/目标栈/计划/任务/投影） |
| `𝓐 = {a_main} ∪ 𝓐_adv ∪ 𝓐_wk` | 会话内 agent 集：main(owner)、advisors、workers |
| `M_i` | agent i 的信箱（有界队列，(cap K_i, overflow cnt_i)） |
| `𝓡(r) ∈ {owner, advisor, worker}` | 角色能力（写者 / 只读+出证据 / 隔离写者副本） |
| `ℰ(t,src,dst,payload)` | 类型化信封（§3.3） |
| `π(e | history, window)` | 议程函数：事件 e 后是否/何时触发 agent 回合 |
| `ℋ` | 治理自动机（会话/目标终态、审批预筛、verdict） |
| `ℬ` | 会话预算簿（token/回合/时间额度、lease） |

### 3.2 会话账本与派生状态（M5）
```
命令(命令由 owner 触发，或由受信 gate 触发)
   ──(校验+落账)──► e = (seq, ts, type, actor, payload)
ℒ 追加唯一写入口：Append(e) 原子自增 seq。
σ := fold(ℒ) = (ℳ, 𝒢, 𝒫, 𝒯, 𝓥)          // 投影可延迟、可重放
快照 checkpoint：σ_c = fold(ℒ[0..c])     // 截断旧日志，保留 fold 基
```
- **不变量 I1（账本单调）**：ℒ 只增；seq 单调；同 seq 幂等。
- **不变量 I2（单写者）**：除 owner 外任何 agent 不能调用 Append 修改 σ 的正式版本。
  advisor 的产出以 `directive/verdict/evidence` 事件入账（作为**建议**，非状态变更）；
  owner 采纳后才产生对应状态迁移事件。

### 3.3 通信层：类型化信封 + actor（M2/CSP/π 混合）
```
Envelope = (id, correlation_id?, type, version, from, to?,
            deadline?, scope{trace/session/plan/node/role}, payload_ref)
```
- 投递语义三种：`Send`（单向，满丢最旧 + overflow 计数，非阻塞）、`Request/Reply`（带
  `correlation_id` + deadline，用于 verdict 等确定性 gate）、`Publish`（事件流，见 R6）。
- 类型化：payload schema 由上层注册（requirements R2），框架只做信封与校验缝。
- **不变量 I3（有界性）**：∀i: |M_i| ≤ K_i；溢出只增计数不阻塞；读面可追平（丢最旧可重读）。
- **不变量 I4（无环等待）**：请求/应答图中，advisor→owner 是"投递即返回（异步）+ 超时判负"，
  不允许 owner 持锁等待 advisor（锁序纪律由 actor 化消解，参照 seelex
  `seelebridge/session/subagent_context.go` 的 SendTimeout 10s 模式）。

### 3.4 治理自动机 ℋ（M1 状态图，role 视角正交）
以"目标/会话收敛"为例（goal MVP `gate.go` 已实现同构语义）：
```
会话/目标状态: Idle ─goal_begin→ Active(g) ──…──► ProposeFinish
ProposeFinish ─verdict=done→ Completed(弹栈+审计)
              ├verdict=not_done(d)→ Correcting(d 写回) ─→ Active
              ├verdict=escalate→ HumanGate ─批准→ Completed / 驳回→ Active
审批预筛: PreScreen(risk) = low∧∈白名单 → TL approve/deny；否则 escalate_human（默认拒绝）
```
- 转移全部以事件入账；守卫函数（guards）在 Append 前校验（如 verdict 必须对应 active goal id）。
- **不变量 I5（终态可达/不靠模型自觉）**：Completed 只由 gate 自动机到达，模型不能自报完成即完成。

### 3.5 议程与回合（M6/M7）
```
π(e) 决策是否触发 advisor a 的回合：
  立即集：context_compacted / budget_warning / approval_asked / terminal_proposal
  窗口集：step_checkpoint（满足 eval_window w 已过才评估）
  不触发：turn_completed（仅计数）、goal_updated
回合预算：cost(round) ≤ ℬ.a 余量；TL/advisor 回合并发上限（默认 ≤1）；
回合载荷有界：goal 帧重建 + tail ≤8 + pending ≤N + content ≤1200 runes（goal MVP 已实现）
```
- **不变量 I6（无活锁）**：窗口计数有上界；任一窗口内回合数 ≤ ℬ 额度；advisor 永不无限发言。
- **不变量 I7（建议必达/可排空）**：owner 下一回合前能排空待处理 directives（Drain 幂等）。

### 3.6 生命周期 owner 与租约（M10）
```
spawn(a)：owner(会话运行时)创建 a，注入 {只读 σ 快照视图, 信箱, 预算, ctx}
pause/resume/cancel(a)：显式消息；cancel 传播 ctx；reap(a)：确认退出并回收（无孤儿）
lease：a 每轮心跳续租；租约过期 → 监督者 cancel + 上报 fault 事件（复用 event.HeartbeatLease）
```
- **不变量 I8（无孤儿）**：会话关闭时，owner 回收全部存活 advisor/worker，`reap` 在超时护栏内确认
  全部退出（对照 seelex `stopAndReap` 10s 模式）。

### 3.7 worker 隔离与合并（复用 M9，现有）
worker 在 `ParentSnapshot`（不可变基线副本）上执行；结果按确定性顺序
（require_all / successful + 分支 ID 稳定序）合并回 σ（见 Seele `forkexec.ContextManager.Join` 与
seelex merge-back 顺序化提交）。

### 3.8 需要验证的性质（验收断言）
- Safety：I2（单写者）恒真；I3 有界；I8 无孤儿。
- Liveness：I6 无活锁；I5 终态 gate 可达；任何 pending directive 在有限步内被排空或被丢弃计数。
- Deadlock-freedom：I4（无持锁等待）⇒ 无循环等待；回合超时判负保证 gate 不悬挂。
- Determinism：同一 ℒ + 同一议程 ⇒ 同一 σ（fold 确定、Join 确定）。
- Race-free：信箱/账本单写，读快照不可变 ⇒ `-race` 下无数据竞争（把 I2 落为架构而非锁纪律）。

---

## 4. Seele 现状数学模型（从代码证据归纳）

### 4.1 模型总述
Seele 当前形态可形式化为：**"隔离会话的工作流执行 + 事件观测"**——
- 执行单位是 WorkPlan DAG 上的 node；每个 agent node 由 `AgentFactory.NewAgent` **新建一个隔离 Session**
  （`agent/bridge/workplan_agentfactory.go:72-94`"Each node gets a newly assembled Session so its working
  history is isolated from other nodes by default"）。⇒ **同一会话多角色在框架层不存在**：agent = ReAct
  循环实例 + 工具门，而不是持久 actor 身份。
- 并发只在"波次内就绪节点"之间出现，其余串行（见 4.2）。
- 治理语义（如 TL×Programmer、goal gate、审批预筛）目前由产品 seelex 用自建包表达
  （`application/core/goal/`），尚未进框架。

### 4.2 DAG 波次调度（workplan/scheduler）
```
G = (V, E)；entry ∈ V；依赖入度 d(v)=|{u: u→v 激活}|。
执行 = 拓扑"波次"循环：
  ready = 当前入度归零且未执行的节点集；
  每波：对 ready 内全部节点开 forkexec 分支并发执行（波内并发 ≤ K=MaxForkConcurrency，默认 3：
      scheduler.go:72 New → MaxForkConcurrency: 3；SetMaxForkConcurrency 校验 ≤0 回退 3）
  波末 Join（deterministic order）→ 推进 completedDeps → 生成下一波 ready。
策略：ForkPolicy ∈ {fail_fast(默认), best_effort}；JoinPolicy ∈ {require_all, successful}
恢复：Runner.Resume 从 checkpoint 快照按 node 续跑；Plan.Resolve 决策下一点。
```
⇒ Seele 的"并发模型"是 **fork-join 屏障（M9）**：并发是"分治子任务"，不是"同状态多角色"。

### 4.3 分支状态机与隔离（forkexec）
- BranchState ∈ {queued, started, completed, failed, canceled, panicked}
  （`workplan/runtime/forkexec/forkexec.go:49-54`），事件同步发射。
- 隔离：`ParentSnapshot` = 父 WorkflowContext 不可变深拷贝；`BranchContext` 每分支独占
  （forkexec.go:73-90 "Branch execution never receives a pointer to the mutable parent context"）。
- 限流：`Limiter.Acquire/Release` 注入点 + 内部信号量。
⇒ Seele 有完整、可复用的"并发分支隔离 + 汇合"数学层（目标模型 §3.7 直接复用，无需新造）。

### 4.4 Session 模型（session）
- `NewSession(Components)` 组一个会话；`Chat/ChatStream` 单轮对话；`NewReActLoop` 是执行循环；
  working history（可 Reset）+ 可选 durable history（显式耦合才共享）。
- 默认工作区隔离：history 每会话独立 ⇒ "同会话共享转录"要由调用方显式把同一 history 注入多个会话
  （`workplan_agentfactory.go` 注释明说"Reusing a durable history is an explicit caller choice"）。
⇒ 框架支持"共享同一 history 的多 Session"，但**没有治理语义**（谁写、谁读、谁裁决都未定义）。

### 4.5 Agent 运行时的工具门（agent/bridge RegistryRuntime）
- `RegistryRuntime.VisibleTools(ctx)` + `Dispatch(ctx,name,args)` 构成请求级工具可见性与分发
  （registry_runtime.go:85-96）；`VisibilityPolicy` 由产品注入。
- 这是**角色差别的唯一内建机制**：靠"给谁看什么工具"区分角色，无消息/角色/生命周期抽象。
⇒ 若要做只读 advisor，可**只给 advisor 空工具面 + 只读快照**——但框架目前没有"只读会话视图"概念。

### 4.6 事件契约（event）
- `Type ∈ {lifecycle, progress, heartbeat, fault}`（event/event.go:17-21）；
  `Status ∈ {queued, running, completed, failed, canceled, panicked}`；
  Scope(trace/run/plan/node) + Locator(agent_id/session_id 举例) + Recorder + HeartbeatLease。
- workplan/forkexec/agent 侧均已接 Locator 发射（workplan/event_locator.go、forkexec/event.go）。
⇒ 事件"信封"现成且健壮，但只覆盖框架自身执行/生命周期；**没有 A2A 消息/角色回合/会话级治理事件类型**
（product goal 域的 `/events` 快照事件是 seelex 自建面，与框架 event 并行未融合）。

### 4.7 seelex 产品侧补充（现状的"治理味"在哪）
| 件 | 证据 | 说明 |
|---|---|---|
| fork_subagents 编排 | `seelebridge/fork/tool.go`：spec → buildForkPlan(kind auto/agent/summary) | A2A-like 编排在产品层，靠 plan DAG |
| 有界信箱范式 | `seelebridge/session/subagent_context.go`：`seelactor`(cap256)/queue cap 4096/cmd timeout 10s/overflow/Drain；**actor 在 seelex internal** | 消息原语只在产品 internal，未进框架公共层 |
| merge-back 顺序化 | 同文件 handleMerge + 近期"顺序添加"提交 | worker→owner 汇合已在产品实现 |
| 审批人工门 | `application/approval/broker.go`（Request/Decision/Risk/Pending） | 人工 gate 现成；预筛/代答在 goal MVP |
| goal 域 MVP | `application/core/goal/{a2a,techleader,gate,headless}.go` | 同会话 TL×main 的协议/信箱/gate **自造验证轮子**（同步信箱、共享内存+锁序注释） |
| plan 预算护栏 | seelex `seelebridge/plan/preflight.go` / policy.go（max_loops/max_output_tokens/MaxForkConcurrency 注入） | 预算在"单 agent node"粒度，无"多角色分额" |

### 4.8 Seele 现状模型小结（形式化一句）
> Seele = 在**隔离 session 副本**上执行 **DAG 拓扑波次 + fork-join 屏障（M9）** 的工作流内核，
> 配 **可观测事件契约（M1 子集：只覆盖生命周期状态）** 与 **请求级工具可见性**；
> 产品 seelex 在其上以 **plan DAG 编排 + internal actor 信箱 + 人工审批** 拼出 A2A-like 编排。
> **缺失**：会话内多角色共同治理所需的 单写者约束、advisor 只读视图、类型化 agent 消息信封、
> 回合议程、owner 生命周期 API、agent 级租约回收、verdict/gate 自动机骨架、角色/消息事件类型。

---

## 5. Diff：Seele 相对目标模型需补全的内容

> 维度行 = 目标模型章节；"现"列给出 Seele 已有可复用件；"缺"列 = 本次要补的原语。
> 归属：**框架新增原语**（F，R 编号对应 requirements.md）vs **产品注册语义**（P，seelex 上层做）。

| # | 维度 | 目标模型（§3） | Seele 现状（§4 证据） | 差距（缺） | 归属/回链 |
|---|---|---|---|---|---|
| D1 | 会话内 agent 抽象 | 𝓐 有持久身份、状态机、角色（owner/advisor/worker） | agent = 每 node 新建隔离 Session + ReAct；无持久 agent 身份/角色 | **Agent/Engine 一等抽象**：身份、角色枚举、生命周期状态机（registered/ready/running/paused/stopped/failed）、能力注册表 | F → R1 |
| D2 | 类型化消息 | Envelope(§3.3) + schema 注册 | 消息只在 seelex internal actor；框架层零消息面 | **框架公共消息包**：有界 mailbox + 类型化信封 + request/reply/单向/发布 + 校验缝 | F → R2/R4 |
| D3 | 单写者 | I2 只 owner 写 σ；advisor 只读+出证据 | 无 σ 概念（每 node 隔离）；无只读会话视图 | **会话账本/派生状态层** + **advisor 只读运行时**（空工具面+只读快照注入） | F 骨架 + P 语义 → R5 |
| D4 | 回合议程 | π(e|history,window) + 预算 | 无"回合/发言权"；只有 node 执行序 | **议程/回合调度原语**（立即集/窗口/计数/额度） | F 骨架 + P 注册 → R3 |
| D5 | 治理自动机 | ℋ（终态 gate/审批预筛 verdict） | 人工审批 broker 现成；goal gate 在产品自造 | **gate/verdict 自动机骨架**（门转移 + verdict 代数插件口，产品注册裁决函数） | F 骨架 + P 语义 → R2/R6 |
| D6 | 生命周期 owner | spawn/pause/resume/cancel/reap + 租约 | 只有 ctx cancel + 每会话运行；event 有 HeartbeatLease | **agent 生命周期治理 API + lease 回收/孤儿清理** | F → R3 |
| D7 | 事件类型 | 角色/消息/回合/verdict 事件 | Type 仅 lifecycle/progress/heartbeat/fault | **agent 协作事件类型扩展**（role/message/turn/verdict；scope 加 role/agent_id 泛化） | F → R6 |
| D8 | worker 隔离/合并 | §3.7 fork-join 复用 | forkexec ParentSnapshot/BranchContext/Join 完备 | 基本不缺；只差**合并策略可注入**（现为 fail_fast/best_effort × require_all/successful 固定两维） | F 轻量 → R7 |
| D9 | 预算分额 | ℬ 多角色分额 | plan node 级预算（产品 policy） | **会话级多 agent 预算簿**（各角色回合/token 分额共享封顶） | F 原语 + P 规则 → R3 |
| D10 | 公共 mailbox 位置 | 框架公共 | seelactor 在 `seelex/seelebridge/internal/actor` | **把有界 mailbox 上提到框架公共包**（cap/overflow/SendTimeout 语义保留） | F → R4 |
| D11 | 远程互操作 | 预留 A2A wire（M13） | 无 | 进程内先行；AgentCard/task 状态机映射预留 | F 后置 → 需求单 Q1 |
| D12 | 发版/并存 | — | seelex pin v0.1.2，GitHub HEAD 领先 | **框架发版 + seelex 升 pin**；新旧路径共存回归 | F（流程）→ R8 |

### 5.1 Seele 建议新增的框架原语面（按 D 排序）
1. `governance/engine.go`：`Engine`/`AgentSpec`（身份、角色、能力、状态机）、`Registry`（按角色/能力查询）→ D1。
2. `governance/mailbox.go`（或公共 `mailbox`）：`Mailbox[T]`（cap/overflow/SendTimeout/Request/Reply）
   与 `Envelope` 类型化信封 → D2/D10。
3. `session/view.go`：`ReadOnlyView(session)`（不可变快照句柄，防写）→ D3。
4. `governance/agenda.go`：回合议程注册表 + 窗口/预算计数 → D4/D9。
5. `governance/gate.go`：门自动机骨架（状态转移 + verdict 接口，产品注入裁决器）→ D5。
6. `governance/lifecycle.go`：spawn/pause/resume/cancel/reap + lease 判定 → D6。
7. `event` 扩展：Type 增 `role/turn/message/verdict`（或独立 `event/a2a` 契约包，需求单 Q6）→ D7。
8. forkexec 合并策略小扩展（注入维度）→ D8。

**约束（写进框架验收）**：以上原语**不 import 任何 seelex 类型**，语义由产品注册（requirements.md §2 原则）。
现有 workplan/forkexec/event/session 路径**不回归**（R8）。

---

## 6. 落地顺序与验收（映射到 requirements.md）
- **P0 基座**（D1/D2/D10/D3 + 发版 R8）：框架给 Engine/Envelope/Mailbox/ReadOnlyView；
  验收 = 上游 example：两个引擎注册、互发 typed 消息、main 只读拉起 advisor，不经过 plan。
- **P1 治理+可视化**（D4/D5/D6/D7）：agenda/gate/lifecycle/事件扩展；
  验收 = seelex 把 goal/TL 语义注册其上（Goal 栈 + 双角色泳道 + verdict 事件 → 前端 demo 渲染），
  替换 `application/core/goal` 的自造信箱/锁序（`techleader.go` 升级 actor，注释路径兑现）。
- **P2**：D8 合并策略注入 + D11 远程 A2A wire 预留。
- **不变量验收**：§3.8 全表转为 `-race` 测试断言（单写者/有界/无孤儿/无活锁/确定性）。

---

## 7. 证据索引与参考
### Seele 框架证据（GitHub `RedHuang-0622/Seele` == 本地 HEAD `69374b7`，go.mod pin v0.1.2）
- `workplan/core/node/base_node.go:15-37,79` NodeKind 全集（method/llm/agent/auto/strategy/approve/if/switch/loop/fork/join/checkpoint/emit）。
- `workplan/core/types/status.go:8-31` Status（pending/running/completed/failed/aborted）。
- `workplan/runtime/scheduler/scheduler.go:18-77,89-186` 波次拓扑执行、MaxForkConcurrency=3、ForkPolicy/JoinPolicy、checkpoint。
- `workplan/runtime/forkexec/forkexec.go:22-54,73-98,125-166,302-352` Fork/JoinPolicy、BranchState、ParentSnapshot 不可变、Join 确定性。
- `workplan/runtime/runner/runner.go:81-206` Resume/checkpoint/heartbeat。
- `agent/bridge/workplan_agentfactory.go:23-94` 每 node 新建隔离 Session（显式耦合才共享 history）。
- `agent/bridge/registry_runtime.go:28-96,117-128` RegistryRuntime 工具可见性/分发、VisibilityPolicy、ErrToolNotVisible。
- `event/event.go:14-34` Type/Status；`event/README.md` Recorder/Locator/Heartbeat/Scope。
- `session/README.md` NewSession/Chat/ReActLoop/Reset/durable 语义。

### seelex 证据（本仓库）
- `seelebridge/fork/tool.go` fork_subagents → buildForkPlan(auto/agent/summary)。
- `seelebridge/session/subagent_context.go:70-72,88-127` seelactor cap256/queue4096/10s/overflow/Drain/handleMerge（actor 在 `seelebridge/internal/actor`）。
- `application/approval/broker.go` 人工审批 Request/Decision/Risk。
- `application/core/goal/{a2a,techleader,gate,headless}.go` 自造同会话 TL 协议（同步信箱 + 锁序注释）；`docs/2026-09-07-goal-domain-techleader/` 设计。
- `seelebridge/plan/{executor,preflight,policy}.go` plan 槽位/预算/MaxForkConcurrency 注入。

### 调研来源（定性引用，不逐行）
- AutoGen multi-agent conversation / group chat speaker 选择：Wu et al., *AutoGen*, arXiv:2308.08155；AutoGen 文档。
- LangGraph supervisor / 共享 state graph / checkpoint：LangChain 官方文档。
- OpenAI Agents SDK / Anthropic Claude Agent SDK：handoff 转移控制权实践。
- Google A2A（Agent2Agent）协议：AgentCard + Task 生命周期 + JSON-RPC（Linux Foundation 开源，2025）。
- 理论：Hoare, *Communicating Sequential Processes* (1985)；Hewitt/Agha actor model；Milner, *π-calculus*；
  Honda, session types；Fowler, Event Sourcing/CQRS；Harel, statecharts；Herlihy & Wing, linearizability（对比用，未采用）。
- Erlang/OTP 监督树、分布式租约（heartbeat/lease）作为生命周期 owner 与回收的工程范式。
