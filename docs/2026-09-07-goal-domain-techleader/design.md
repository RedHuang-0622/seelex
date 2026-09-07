# Goal 域 + TechLeader A2A 详细设计

> 状态：Draft（设计提案，非实现事实）
> 日期：2026-09-07
> 配套：架构总览见同目录 `architecture.md`（§决策清单 D1–D8 待用户拍板）。
> 关联：`docs/2026-08-07-agile-a2a/`（TL×Programmer 敏捷 A2A）、`docs/research/goal-context-governance-2026-09.md`、
> `docs/research/codex-claude-goal-harness-2026-09.md`、`docs/arch/context-prefix-chain.md`。
> 代码锚点均为当前仓库文件/行（2026-09-07 核对）。

---

## 1. 结论摘要

1. **goal 发起/存储（Part I）**：新增会话粒度的 Goal 对象与 goal 栈（与 plan/task 同一
   `sessionTaskRuntime` 数据面、同一 state blob）；工具面 `goal_begin / goal_update / goal_finish /
   goal_status` 入 `seelebridge/tools/policy.go` 门控（与 plan 工具族同 gate：仅 goal skill 激活可见）；
   Goal 帧按装配顺序插在 plan 前、每轮重建、不参与压缩；工作台目标改为「goal 栈顶 + 下层状态」。
2. **goal 实现（Part II）**：goal 条件下会话装配双角色——mainagent（执行，不感知 TL）与 TechLeader
   （共享同一会话、只读评估、输出纠偏/规范指令、预筛 low-stakes 审批）；TL system prompt 每次评估
   强制嵌入 goal 内容；角色间经 **TechLeaderMailbox（CSP actor）** 收「执行事件信号 + 有界会话嵌入」、
   发「TLDirective」。
3. **会话结束判定状态机**：mainagent 终态提议（task_complete / goal_finish / needs_user_decision）
   不直接生效——进入 review → TL 校验 verdict → completed / back-to-active / escalate-human；结束
   判定是 harness 协议，不是模型自觉。
4. **护栏**：goal 预算（loops/tokens）耗尽 → goal 状态 `waiting_human`；TL 无写工具；指令注入只在
   ChatStream 边界；actor 有界命令通道 + 超时 + overflow 计数（不丢内容，复用
   `seelebridge/session/subagent_context.go` 范式）。

---

## 2. 现状锚点（读代码确认）

| 锚点 | 事实 |
|---|---|
| `application/core/input.go:68-110` | 非 goal：`loopLimit=maxLoopsFor(effort)`；goal skill 激活：`SetMaxLoops(9999)`（无限循环，无完成判定） |
| `application/contract/dto/projection.go` | `RuntimeVisibilityProjection{GoalSkillActive bool}`、`ParentEvidenceProjection{SessionID, Goal string, ConversationCount}` |
| `application/core/runtime_projection.go` | `latestVisibleUserGoal` = 最近一条非 marker user 消息截断 200 runes → 工作台"目标" |
| `seelebridge/tools/policy.go:46,64-81` | plan 工具族（plan_load/plan_run/…/fork_subagents 逻辑上同组）仅 `goalSkillActive()` 时对主代理可见；`isPlanTool` 判断 |
| `application/core/task_context/coordinator.go` | `sessionStates map[string]*sessionTaskRuntime`；`sessionTaskRuntime` 含 `taskExecution/taskService/planStack/activePlanID/reactBudget/...`；`stateMu` 保护；`syncGoalSkillActiveLocked` 已有 |
| `seelexctx/README.md`、`docs/arch/context-prefix-chain.md` | 装配顺序 `system→project→memory→compact→context(累积)→plan/task 尾部→当前输入`；plan/task 每轮重建、不参与压缩；skill 内容 = system 尾部 task 级段 |
| `sessionstore/README.md`、`session_context.go` | state blob 按 `(project_id, session_id)`；`SessionContextStore` 持久化 Plan/Task/Skill/Compact 四栈 + 聊天队列；schema 版本校验失败显式拒绝加载 |
| `seelebridge/session/subagent_context.go` | CSP actor 范式：`seelactor.Actor[T]` + 有界命令通道（cap 256）+ 命令超时（10s）+ 单消费者 goroutine + atomic.Pointer 读面 + overflow 计数不丢内容 |
| `seelebridge/runtime_tools.go:140` / `main.go:141` | `RegisterBuiltins`/`registerProductTools` 注册工具（goal 工具族在此类注册点接入） |

---

## 3. Part I：Goal 域（发起/存储/投影）

### 3.1 概念模型与术语

- **Goal（目标文档）**：会话粒度的结构化目标记录。字段见 §3.2。
- **goal 栈**：会话内 LIFO 目标栈。**栈顶 = 当前 active goal**（工作台主目标）；下层 = 被挂起/待办的
  goal（含各自状态）。`goal_begin` push、`goal_finish` pop、`goal_update` 更新栈顶。
- **会话单例（D2）**：v0 默认 `goal_stack_depth=1`——同一会话同一时刻至多一个 goal（新 begin 需先
  finish 或显式 suspend），保持"发起即聚焦"；配置放开后可嵌套（上层完成后自动恢复下层）。
- **Goal vs Task/Plan**：goal 是「一段时间内的总目标与总账」，plan/task 是它的一次次执行载体。
  task 终态 ≠ goal 终态：goal 未 finish 而 task_complete = 单步完成、目标继续（对应长任务）；
  goal_finish 才把目标收口。

### 3.2 GoalRecord（持久化 schema，state blob `goal` 栈）

```go
// GoalRecord 是 goal 栈的持久化单元（state blob：<project>/<session>/goal/stack + history）
type GoalRecord struct {
	ID          string       `json:"id"`           // 会话内唯一（如 "g-<n>"，单调）
	SessionID   string       `json:"session_id"`
	Title       string       `json:"title"`        // 一句话目标（工作台/栈顶标题）
	Statement   string       `json:"statement"`    // 目标正文（含 why/how 边界，有限长，供 Goal 帧与 TL 嵌入）
	Acceptance  []string     `json:"acceptance"`   // 完成条件（可验证条目；TL 据此判完成）
	OutOfScope  []string     `json:"out_of_scope,omitempty"`
	Budget      GoalBudget   `json:"budget"`       // goal 级预算（loops/tokens/TL 份额）
	Status      GoalStatus   `json:"status"`       // active/paused/reviewing/completed/failed/aborted/waiting_human
	Progress    []GoalUpdate `json:"progress,omitempty"` // 里程碑/发现（goal_update 追加，有界）
	TLDirective []string     `json:"tl_directives,omitempty"` // 最近 TL 指令摘要（环形，≤N）
	CreatedAt   int64        `json:"created_at"`
	UpdatedAt   int64        `json:"updated_at"`
	FinishedAt  int64        `json:"finished_at,omitempty"`
	Meta        map[string]string `json:"meta,omitempty"` // 溯源/统计扩展点
}

type GoalBudget struct {
	MaxLoops      int `json:"max_loops"`                // 默认 = Limits().InputLoopLimit（9999）语义
	MaxTokens     int `json:"max_tokens,omitempty"`     // goal 总 token 护栏（seele.yaml limits 派生）
	TLTokensShare int `json:"tl_tokens_share,omitempty"` // TL 评估预算占比（默认 20%）
}

type GoalUpdate struct {
	At      int64  `json:"at"`
	Kind    string `json:"kind"`    // milestone | finding | decision | risk
	Content string `json:"content"` // 有界（≤ evidence_chars 类上限）
}
```

校验规则（单测覆盖）：Title 非空；Statement ≤ 上限（如 4k chars）；Budget.MaxLoops ≥ 0；
Acceptance 为空时 TL 判完成 = 仅按状态机人工/指令（弱完成条件，需标注）。

### 3.3 Goal 状态机

```text
                  goal_begin
                       │
                       ▼
                   active ──goal_update──► active（改内容/追加 progress）
        goal_begin(新栈顶, D2 放开嵌套)     │
                       │  suspend        │
                       ▼                 │
                    paused ◄─────────────┤  （栈下层，弹栈回 active）
                       │                 │
                       │  mainagent 提议终态（task_complete / goal_finish）
                       ▼                 │
                   reviewing ────────────┤  TL verdict: not_done → back-to-active（附纠偏）
                       │                 │
        TL verdict: done / 或人工确认      │
                       ▼                 │
               completed ──goal_finish──► 出栈（运行时删除；审计归档可选）
                 failed / aborted        │  预算耗尽 → waiting_human（人工续/放弃）
                       ▲                 │
                       └─────────────────┘
```

状态事件表（P0 只落 active/paused/completed/failed/aborted/waiting_human；reviewing 在 P2 接入）：

| 状态 | 进入 | 执行者 | 动作 |
|---|---|---|---|
| active | goal_begin（栈顶） | 主代理 | Goal 帧注入、投影更新 |
| paused | 新 goal 压栈（D2 放开时）/ 显式 suspend | 主代理 | 保留下层状态 |
| reviewing | mainagent 终态提议 | 框架 + TL（P2） | TL 校验（§5.4） |
| completed | TL verdict done / 人工确认 | TL/人工 | 允许 goal_finish |
| failed/aborted | TL 判不可达成 / 显式放弃 | TL/主代理 | 出栈并记录 |
| waiting_human | 预算耗尽 / 无法判定 / 越权 | 框架 | task_needs_user_decision 终态语义 |

### 3.4 工具面（goal 工具族）

> 用户调研的 getGoal/updateGoal/finishGoal 对应下面 GoalStatus/GoalUpdate/GoalFinish；
> 创建（begin）也是注册动作，goal skill 激活后首个显式注册即 begin。

| 工具 | 语义 | 备注 |
|---|---|---|
| `goal_begin {title, statement, acceptance, budget?, out_of_scope?}` | 注册并压栈（栈空→active；栈深超限→按 D2 拒绝） | 幂等：同标题已 active 时返回现有 goal 引用 |
| `goal_status {id?}` | 读 goal 栈（栈顶 + 下层各状态） | 等价外部 getGoal；响应进投影与 Goal 帧 |
| `goal_update {content?, kind?, progress?}` | 更新栈顶 goal（追加 progress / 改边界） | 有界；改 Acceptance 仅 active 态允许 |
| `goal_finish {result?, reason?}` | 弹栈删除（运行时），标记 completed | 终态；审计归档按 D3 |

注册与门控：`seelebridge/runtime_tools.go` 注册 + `seelebridge/tools/policy.go` 新增
`isGoalTool(name)` 加入与 `isPlanTool` 相同的 `goalSkillActive()` 门控分支（line 46 同款），
且子代理（RoleSubAgent）不可见（与 plan 工具族一致）。

### 3.5 Goal 帧（注入）与装配顺序

- 新装配顺序（D8）：`system → project → memory → compact → context(累积) →【goal】→ plan → task → 当前输入`
- goal 帧 = `sessionTaskRuntime.activeGoal`（栈顶）渲染：Title / Status / Acceptance（摘要）/
  Progress 最近 1-2 条 / 预算水位（TL 份额外不注入）——有界（token 预算见
  `seelexctx` `Limits`），每轮从 coordinator 重建（不参与压缩，同 plan/task 尾部语义）。
- 实现落点：与 plan/task 尾部消息同类，在 prompt_layer/seelexctx 装配路径新增 goal 帧构建器
  （渲染函数 + 单测：帧 ≤ 预算、栈空渲染空、Status 变更即字节变化导致该帧失效一次——对齐前缀缓存）。

### 3.6 工作台投影（目标区改造）

- `RuntimeVisibilityProjection` 扩展（`application/contract/dto/projection.go`）：

```go
type RuntimeVisibilityProjection struct {
	GoalSkillActive bool
	Goals           []GoalView // 栈底→栈顶；工作台取最后一个=栈顶为主目标
}
type GoalView struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	UpdatedAt int64  `json:"updated_at"`
}
```

- `ParentEvidenceProjection.Goal`：goal 栈非空 → 栈顶 `Title`（结构化，替代 `latestVisibleUserGoal`
  的最近聊天消息）；栈空 → 回退现有行为（保持单会话/非 goal 兼容，`runtime_projection.go` 只改取数源）。
- 投影发布：`publishRuntimeProjections`（`application/core/runtime_projection.go`）在 ViewMu 下拷贝
  goal 栈视图（不可变切片）后发布，锁序不变。

### 3.7 持久化 / 恢复 / fork / 清理

- state blob：`SessionContextStore` 由 Plan/Task/Skill/Compact 四栈升级为**五栈（+Goal 栈）**，
  schema 版本 bump（现有「版本校验失败显式拒绝加载」策略沿用 → 旧记录走迁移/拒绝两条路径需在 P0
  明确：v0 建议拒绝旧 schema 并提示重建，迁移按需）。
- 恢复：goal 栈随会话冷加载恢复 → `activeGoal` 重建 + 投影恢复（对齐 plan 恢复锚点机制）；goal 的
  PendingWork 续跑 = 复用 `ContextSnapshot.PendingWork`（恢复后主代理可见"上次到哪"）。
- fork：默认不继承父 goal 栈（D4，对齐「fork 不继承父 todolist」既有语义）；可选把父栈顶
  Statement 作为新会话 goal_begin 草稿（skill 提示）。
- 删除会话 / Clear：级联清理 goal 栈与 history（走现有 session 清理路径扩展）。

---

## 4. Part II 前置：会话双角色模型

### 4.1 角色与职责

| | mainagent（执行者） | TechLeader（监督者，goal 条件下注册） |
|---|---|---|
| 承载 | 现有 ReAct 主循环 | goal 条件下的第二个循环（评估） |
| 视角 | 全量：工具执行、plan/task、goal 栈 | 只读：goal 内容（强制嵌入）+ 有界会话嵌入 + 事件信号 |
| 工具 | 全量（含 goal 工具族、写工具） | 无执行写工具；只产出 TLDirective |
| 目标记忆 | Goal 帧（§3.5）+ plan/task | **system prompt 每次嵌入 goal 内容**（§5.3） |
| 终态 | 提议（task_complete/goal_finish/needs_user_decision） | 校验/否决/升级（verdict） |
| 学习（L） | 执行产出 findings | 目标级教训（记入 goal history，供后续复用） |

### 4.2 角色共享 session 的形态

- **同一会话、双视角，不复制会话**：mainagent 与 TL 共享同一 durable history / state blob / goal 栈；
  差异只在"装配与权限"。
- 每一轮：mainagent 请求照常（system 已含 Goal 帧 + 最近 TL 指令摘要）；TL 不是每轮出现，而是
  **事件驱动的评估回合**（§5.2）——TL 回合 = 一次独立 LLM 调用，输入 TL 专用 prompt
  （角色模板 + goal 嵌入 + 有界会话嵌入 + 待处理信号 + 最近 TL 指令），输出结构化
  `TLDirective`（§5.4），在 ChatStream 边界注入 mainagent 下一轮（对齐
  `injectPendingSubagentContexts` 模式，不持 Engine 锁反向写）。
- 消息标记：TL 注入的消息带角色 marker（类 `view_state.SubagentContextMarker` 前缀 `tl-directive:`），
  保证：装配过滤不被当成用户目标、投影取数（§3.6）不受污染、GUI 可区分角色气泡。

---

## 5. TechLeader A2A（CSP channel + TL 回合 + 状态机）

### 5.1 TechLeaderMailbox（CSP actor）

落点：`seelebridge/session/techleader.go`（复用 `seelactor.Actor` 范式，参照
`subagent_context.go`：有界命令通道 + 单消费者 goroutine + 命令超时 + atomic.Pointer 读面 +
overflow 计数不丢内容）。

```go
type TechLeaderMailbox struct {
	actor  *seelactor.Actor[techleaderCmd]
	state  atomic.Pointer[TLState] // 读面：pending signals 数/最近 embed ref/最近 verdict
	// 仅在 actor goroutine 内访问：
	queueSignals []TLEvalSignal
	queueEmbeds  []TLSessionEmbed
	queueOut     []TLDirective
	overflow     atomic.Int64
}
```

命令（channel cmd，超时 10s）：
- `CmdEnqueueSignal(sig TLEvalSignal)` — mainagent 执行侧投递信号（低优先级，软容量超限计数不丢）
- `CmdDeliverEmbed(embed TLSessionEmbed)` — 有界会话嵌入投递（触发条件见 §5.2）
- `CmdEval(trigger)` — 消费者触发一次 TL 评估回合（有界；TL 预算内）
- `CmdPublishDirective(d TLDirective)` — TL 回合产物（由 TL 执行器写回）
- `CmdDrainDirectives()` / `CmdSnapshot()` — 主代理侧 ChatStream 边界排空指令 / 读状态
- `CmdShutdown()`

### 5.2 信号分类与触发策略（D5）

mainagent 执行事件 → 信号（来源接线）：

| 信号 | 来源 | 触发 TL 回合 |
|---|---|---|
| `turn_completed` | 每轮 mainagent 提交后 | 否（计入间隔计数） |
| `step_checkpoint` | plan 节点打点 / `task_check_node` / todo | 是（每节点 ≤1 次） |
| `context_compacted` | 压缩后 | 是（TL 重锚 goal，防遗忘） |
| `budget_warning` | goal 预算水位（如 70%） | 是 |
| `approval_asked` | `ask_approve` / ApprovalBroker 待批 | 是（预筛，§5.5） |
| `terminal_proposal` | mainagent 调 task_complete/goal_finish/needs_user_decision | 是（终态校验，§5.6） |
| `goal_updated` | goal_update | 否（刷新 Goal 帧即可） |

策略：`eval_window`（如最近 3-5 轮内最多 1 次非关键评估）+ 关键信号（compacted/budget/approval/
terminal）立即评估；TL 回合数/预算计入 `GoalBudget.TLTokensShare`（默认 20%），耗尽后转人工。

### 5.3 TLSessionEmbed（有界会话嵌入）与 TL prompt

```go
// TLSessionEmbed：投递给 TL 的输入（有界，见 seelexctx.Limits）
type TLSessionEmbed struct {
	Goal       GoalFrame    // 栈顶 goal：title/statement/acceptance/status/progress(≤3)
	SessionTail []TurnBrief // 最近 ≤K 轮的事件标题级摘要（role marker / 工具 / 关键结果一句话）
	PlanTask    PlanBrief    // activePlan/任务状态、打点
	Signals     []TLEvalSignal
	TLMemory    []string    // 最近 TL 指令摘要（环形 ≤5，来自 goal.TLDirective）
}
```

TL system prompt 装配（每次 TL 回合重建，确保「不忘 goal」）：
`[TL 角色模板]（纠偏规则/规范提示词规则/完成判定规则/升级规则）
+ ## Goal（嵌入栈顶 goal：Statement/Acceptance/OutOfScope/Progress/预算水位）
+ ## 会话嵌入（§5.3 embed）
+ ## 待处理信号清单`
输出契约：单个结构化 `TLDirective` JSON（§5.4），非法/超限输出丢弃并告警（有界）。

### 5.4 TLDirective（TL → mainagent）

```go
type TLDirective struct {
	GoalID   string `json:"goal_id"`
	Kind     string `json:"kind"`   // correct | normative_prompt | checkpoint_ok | verdict_done | verdict_not_done | escalate_human
	Content  string `json:"content"` // 有界指令文本（注入 mainagent 下一轮）
	Refs     []string `json:"refs,omitempty"` // 事件/文件引用
	Severity string `json:"severity,omitempty"` // P0/P1/P2（escalate 时携带）
}
```

- `correct`：过程纠偏（例如：跳过验证、任务切分越界、改动 OutOfScope 文件的意图）。
- `normative_prompt`：给 mainagent 的规范提示词（对"无关紧要"判断的 LLM 代答，减少 human judge）。
- `verdict_done / verdict_not_done`：终态校验结果（§5.6）。
- `escalate_human`：TL 判不了/越权 → 转人工（approval / needs_user_decision）。

### 5.5 审批预筛（减少 human judge；D7）

现有审批链（`ask_approve` / ApprovalBroker）在 goal 条件下插入 TL 预筛：

```text
approval_asked（低风险白名单：如 reads/常规命令/低危写入）
  → TechLeaderMailbox 信号 → TL 回合 → TLDirective:
       approve（代答，替代人工） → 原审批流放行 + 审计 TL verdict
       deny/问询              → 回 mainagent 修正
       escalate_human（越权/高危）→ 原人工审批（默认拒绝兜底不变）
```

白名单范围 v0：仅**低风险**（不触碰 config/账号/高危路径；与 `permission-path-gating`
低危档一致）；高写风险默认人工——"无关紧要的 human judge 由 LLM 代答，要紧的仍留人"。

### 5.6 会话结束判定状态机（终态协议扩展）

mainagent 的终态提议不再直接终态，改为进入 goal/session 级 finish gate：

```text
mainagent 调 task_complete / goal_finish / task_needs_user_decision
        │
        ▼
  proposal_received（信号 terminal_proposal → TL 回合）
        │
        ├─ TL verdict_done ─────────────► 迁移 goal: reviewing→completed（/ 会话结束判定）
        ├─ TL verdict_not_done（附 correct 纠偏）► goal: back-to-active；注入指令；不终态
        └─ TL escalate_human ────────────► 人工（task_needs_user_decision / approval）
```

会话结束判定（goal 条件）：
- goal 栈空（无 active goal）→ 普通会话语义，沿用现有 task 终态协议；
- goal completed 且栈空 → 目标会话自然收口（Goal 帧退出、TL 循环停止、工作台回退）；用户可继续普通对话；
- 栈下层有 goal（D2 放开）→ 弹栈后自动恢复下层 active（投影切换）。

---

## 6. 并发、锁与安全

- **锁序不变**：goal 栈属 `sessionTaskRuntime`，所有变更在 `stateMu`（coordinator 自有）下执行
  （对齐现有 plan/task 域）；投影拷贝在 `ViewMu` 下做快照后释放再发布（沿用
  `publishRuntimeProjections` 模式）。新增规则：`stateMu` 内不得调用 TL actor（无反向锁依赖）。
- **TL actor 免锁**：全部可变状态收进 actor goroutine；外部读走 atomic.Pointer 快照。
- **注入只在边界**：`DrainDirectives` 与注入只发生在 ChatStream 前/后边界（与
  `injectPendingSubagentContexts` 同类），禁止在 Engine 锁内反向写历史。
- **无双重执行写**：TL 回合不调写工具（D6）；mainagent 是唯一执行者——避免双写冲突与审批绕过。
- **有界性**：命令通道/队列/embed 全部有界 + overflow 计数（诊断，不丢）；TL 回合输出超限丢弃告警；
  指令注入长度受 `Limits` 约束。
- **审计**：TL 每次 verdict/directive 记录 goal 事件（`goal.TLDirective` + 会话事件库
  `seelex.goal.tl`），审批代答标记 reviewer=TL（对应 ReviewVerdict.ReviewerID 审计理念）。

---

## 7. 依赖与边界

**做**：goal 栈/对象/工具/投影/持久化；TL mailbox + 回合 + 指令；终态 gate 与审批预筛；goal 预算护栏。
**不做（Out）**：
- 不复制会话（TL 与 mainagent 共享，不做第二独立 Session——与子代理区分）；
- TL 不直接编辑文件/不执行写工具（只出指令）；
- 不自动 push/merge/发布（对齐 A2A 人工生产门）；
- 不做 OS 级沙箱（沿用 PathGate）；
- goal 栈嵌套默认关闭（D2，配置放开）；跨会话 goal 记忆为后续项（复用调研 M1 路线）。
- 不改变 effort 档位的 MaxLoops 语义（goal 9999 维持，新增的是 goal 级预算护栏而非档位）。

---

## 8. 实现分阶段（P0–P2）与验收

### P0 — Goal 域落地（goal 发起/存储/投影；不含 TL）

内容：
- `application/core/task_context/goal.go`（新文件）：GoalRecord/状态机/`sessionTaskRuntime.goalStack`/
  `activeGoal`；coordinator 方法 `BeginGoal/UpdateGoal/FinishGoal/GoalStatus/_*Locked`（锁序同 plan/task）；
  `syncGoalStateLocked` 并入既有 `sync*Locked` 投影族。
- 工具：`goal_begin/update/finish/status` 注册 + `isGoalTool` 门控（policy.go）+ 子代理不可见。
- Goal 帧：prompt_layer/seelexctx 装配路径新增 goal 帧渲染（插在 plan 前；有界；每轮重建）。
- 持久化：`SessionContextStore` 五栈 + schema bump + 恢复/重建路径；fork 不继承（D4）；删除级联。
- 投影：`RuntimeVisibilityProjection.Goals` + `ParentEvidenceProjection.Goal` 取数切换 + 栈空回退。

验收（命令式，可测）：
- `go test ./application/core/task_context/... ./seelebridge/tools/... ./sessionstore/... -count=1` 全绿；
- `go test -race ./application/core/task_context/... -count=1` 全绿（goal 栈并发 push/finish/pop 无竞态）；
- 单测：goal 状态机全转移、栈深度限制（D2）、finish 弹栈+投影更新、schema 版本拒绝旧加载、fork 不带父栈；
- scripted e2e：`goal_begin → goal_update → goal_status（栈顶/下层状态） → goal_finish（弹栈删除+投影回退）`
  断言中间态投影与持久化 roundtrip。

### P1 — TechLeader A2A（CSP channel + TL 回合）

内容：
- `seelebridge/session/techleader.go`：`TechLeaderMailbox` actor（§5.1）+ 单测/race。
- 信号接线：turn/step_checkpoint/context_compacted/budget_warning/approval_asked/terminal_proposal
  从执行路径（plan/task/compaction/approval/task terminal）投递。
- `TLSessionEmbed` 导出器（复用 `seelexctx` provider/snapshot 有界导出 + 事件尾窗摘要）。
- TL 回合执行器：TL prompt 装配（角色模板 + **goal 嵌入** + embed + 信号）+ `TLDirective` 输出契约
  + 触发策略/预算（§5.2，D5）。
- 注入：ChatStream 边界 `DrainDirectives` → mainagent 下一轮（TL marker 段）。
- TL 无写工具约束（D6）+ 审计事件。

验收：
- actor 单元 + race：有界通道满时不丢内容仅计数、命令超时、关闭后快速失败（对照 merge-back 用例风格）；
- scripted e2e：mainagent 节点打点 → 信号入队 → TL 回合产出 directive（goal 嵌入存在于 TL 请求快照断言）
  → 注入 mainagent 下一轮（marker 存在、不被当用户目标）；
- `context_compacted` 触发回合后 TL 请求仍含完整 goal 帧（防遗忘断言）。

### P2 — 收敛与治理（human judge 前置 + 状态机 + GUI）

内容：
- 会话结束判定状态机（§5.6）：终态提议 gate（task_complete/goal_finish/needs_user_decision 拦截 + TL verdict）。
- 审批预筛（§5.5，D7）：低风险白名单 TL 代答 + 审计；高危默认人工。
- goal 预算护栏：耗尽 → `waiting_human` → `task_needs_user_decision`（PartialProgress 携带目标进度）；
  不静默重试。
- GUI：goal 工作台目标区（栈顶 + 下层状态 + 进度/预算水位），后端仅出投影、前端消费
  `RuntimeVisibilityProjection.Goals`。
- 度量：goal 完成率/时长/TL 回合数/TL 代答审批数/人工门次数（对齐 A2A 度量表）。

验收：
- 负向场景：构造"mainagent 提前 task_complete"，TL 必须 verdict_not_done 拦截并注入纠偏；
- 审批预筛负向：高危写请求不得被 TL 放行（默认人工）；
- 预算耗尽：goal 进入 waiting_human 且携带 PartialProgress；无静默重试样本；
- e2e 全链：goal → plan/task 执行 → TL 纠偏 → 终态 gate → completed/出栈 → 工作台回退。

---

## 9. 风险护栏

| 风险 | 现象 | 缓解 |
|---|---|---|
| TL 忘目标/漂移 | TL 纠偏与 goal 无关 | TL system 每次嵌入 goal 帧；compacted 后强制重锚回合 |
| 双角色写冲突 | mainagent/TL 同时写 | TL 无写工具（D6）；只读评估 + 指令 |
| 注入污染 | TL 指令被当用户目标 | TL marker 前缀 + 装配/投影过滤（同 SubagentContextMarker 惯例） |
| 指令风暴 | TL 每轮刷屏高 token | 触发策略（eval_window + 关键信号优先）；TL 预算 20% 封顶 |
| human judge 前置过度 | 高危审批被 TL 放行 | 白名单仅低风险；高危默认人工（默认拒绝兜底） |
| goal 栈失控 | 无限压栈/僵尸 goal | 深度默认 1（D2）；预算护栏；waiting_human 显式 |
| 恢复不一致 | 崩溃后 goal 栈与投影漂移 | state blob 五栈原子写 + schema 校验；冷加载重建投影 |
| actor 阻塞 | TL actor 停摆拖住执行 | 有界命令 + 10s 超时快速失败（沿用 merge-back 语义） |

---

## 10. 决策清单与待确认项

| # | 决策 | 当前默认 | 需要用户拍板 |
|---|---|---|---|
| D1 | goal 栈语义 | LIFO；栈顶=active | ✅ |
| D2 | 会话单例 | `goal_stack_depth=1`（v0 严格单例，配置放开嵌套） | ✅ |
| D3 | finish 删除语义 | 运行时/工作台删除 + state blob 审计归档（可关 strict delete） | ✅ |
| D4 | fork 继承 | 默认不继承父栈；可带父 goal 文本作草稿 | ✅ |
| D5 | TL 触发策略 | eval_window（≤1 次/3-5 轮）+ 关键信号立即 | ✅ |
| D6 | TL 工具边界 | TL 无写工具，只出指令 | ✅ |
| D7 | 审批预筛范围 | 仅低风险白名单由 TL 代答；高危人工 | ✅（需圈定白名单清单） |
| D8 | goal 帧落点 | goal 帧插在 plan 前、不参与压缩、每轮重建 | ✅ |
| D9 | 外部调研工具名 | getGoal/updateGoal/finishGoal → goal_status/goal_update/goal_finish（+goal_begin） | ✅（命名确认） |

---

## 11. 来源

- 仓库现状：§2 锚点（文件/行均 2026-09-07 核对）。
- 既有设计：`docs/2026-08-07-agile-a2a/{architecture,design}.md`（TL×Programmer、消息 schema、门禁、
  状态映射、缺口清单）；`docs/arch/context-prefix-chain.md`（装配顺序、plan/task 尾部）；
  `seelexctx/README.md`（snapshot/merger/memory/压缩 DAG）。
- 竞品调研：`docs/research/goal-context-governance-2026-09.md`（机制六路径）；
  `docs/research/codex-claude-goal-harness-2026-09.md`（Codex/Claude /goal harness 分层实证）。
