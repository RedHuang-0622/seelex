# Seelex 敏捷开发 A2A 详细设计（Tech Leader × Programmer）

> 状态：Draft（设计提案，非实现事实）
> 日期：2026-08-07
> 配套：架构总览、角色模型、状态机、映射表见同目录 `architecture.md`。
> 承接：W4 试点方案 [`../2026-08-06-agent-landscape-research/plan-iterative-product-pilot.md`](../2026-08-06-agent-landscape-research/plan-iterative-product-pilot.md)（M0–M4 阶段、验收标准格式、失败模式护栏）。

---

## 1. 结论摘要

1. **消息协议**：6 类 A2A 消息（TaskAssignment / WorkResult / BlockerReport / ReviewRequest / ReviewVerdict / ReplanRequest），M0 以「落盘任务卡 + skill 契约」零产品改动起步，M2 后视需要把结构化字段接入 Plan DSL。
2. **失败回环推荐**：DAG 失败边（verify 失败 → 自动路由 fix → 重验，深度 ≤5）为主线；tasklist 手写循环仅 M1 验证语义用；replan 用于战略级重规划。
3. **programmer 契约**：必须产出「代码 + 测试 + 自检结果 + 改动清单」四件套，禁止改验收标准、禁止跳过 verify/评审。
4. **门禁**：四道门（需求锁/验证门/评审门/生产门），验收标准必须可测试（命令 + exit code，不用 LLM 当裁判）。
5. **MVP**：M0 协议落库 → M1 串行双角色 → M2 角色固化（verify/review/deliver 三契约 + 失败边）→ M3 护栏 → M4 固化观测。

---

## 2. 消息 Schema

> 落位建议（M0）：新增 `seelebridge/a2a/` 包（纯类型 + JSON 校验 + 单测），不接线；M1 由 skill 契约驱动读写；M2 后评估把 `TaskAssignment` 的 `Acceptance/Scope` 字段接入 `plan_load` 节点 input（需 DSL 扩展，见 §7 缺口 A1）。

### 2.1 TaskAssignment（TL → Programmer：任务下发）

```go
// TaskAssignment 是任务下发消息；TL 同时把它落盘为任务卡
// （<workspace>/.seelex/a2a/tasks/<task_id>.json），文件是权威事实，
// 消息/节点 input 只携带 goal 摘要 + task_id 指针。
type TaskAssignment struct {
	TaskID     string       `json:"task_id"`               // 任务 ID（= Plan 节点 ID 前缀，如 "T-01"）
	Iteration  string       `json:"iteration"`             // 所属交付/迭代（如 "iter-1"）
	ReqIDs     []string     `json:"req_ids"`               // 本任务覆盖的 REQ 编号（REQ 不可跨任务拆分）
	Goal       string       `json:"goal"`                  // 一句话目标（注入节点 system prompt）
	Scope      []string     `json:"scope"`                 // 允许读写的文件/目录（写集互不重叠是并行前提）
	OutOfScope []string     `json:"out_of_scope"`          // 禁止触碰：REQUIREMENTS.md、DESIGN.md、其他任务模块、基线测试等
	Acceptance []Acceptance `json:"acceptance"`            // 本任务验收标准（逐条可测试）
	Baseline   string       `json:"baseline"`              // 回归基线命令（必跑，如 "go test ./... -count=1"）
	Budget     Budget       `json:"budget"`                // 执行预算（loops / tokens）
	DependsOn  []string     `json:"depends_on,omitempty"`  // 依赖的任务（组内串行依据）
}

type Acceptance struct {
	Req          string `json:"req"`                       // 对应 REQ 编号（REQUIREMENTS.md 中锁定）
	Description  string `json:"description"`               // 自然语言描述（供 Programmer 理解意图）
	Command      string `json:"command"`                   // 通过条件判定命令（真实执行，如 "go run . sort -c 2 < fixtures/in.md"）
	ExpectExit   int    `json:"expect_exit"`               // 期望 exit code（优先判定，0 通常即通过）
	ExpectOutput string `json:"expect_output,omitempty"`   // 期望输出包含的子串（可选第二判定）
	Kind         string `json:"kind"`                      // function | error_handling | regression
}

type Budget struct {
	MaxLoops  int `json:"max_loops"`
	MaxTokens int `json:"max_tokens,omitempty"`
}
```

校验规则（M0 单测覆盖）：
- `ReqIDs` 非空且均为 `REQUIREMENTS.md` 中已锁定编号；`Acceptance[].Req` 必须属于 `ReqIDs`。
- `Acceptance` 非空，且每条同时满足：`Command` 非空、`ExpectExit` 有值、`Kind` 枚举合法。
- `Scope` 非空；`Scope` 与 `OutOfScope` 不重叠；`OutOfScope` 必须含 `REQUIREMENTS.md`。
- `Budget.MaxLoops > 0` 且 ≤ PlanPolicy 上限。

### 2.2 WorkResult（Programmer → TL：结果回传）

```go
// WorkResult 是结果回传消息；Programmer 在最终回复中以结构化 findings
// 输出（merge-back 的 Findings 上卷 TL 上下文），TL 侧据任务卡逐条核验。
type WorkResult struct {
	TaskID       string      `json:"task_id"`
	Status       string      `json:"status"`        // done | done_with_risk | blocked | failed
	ChangedFiles []string    `json:"changed_files"` // 与打点 ChangedFiles 一致
	Tests        []TestResult `json:"tests"`        // 自检记录：真实命令 + exit code（证据优先）
	SelfCheck    []CheckItem `json:"self_check"`    // 逐条对照 Acceptance
	Findings     []string    `json:"findings"`      // 结论/风险/遗留（merge-back 承载）
	Commits      []string    `json:"commits"`       // 本地 commit 摘要（git log --oneline 形态）
}

type TestResult struct {
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
	Passed   bool   `json:"passed"`
	Output   string `json:"output,omitempty"` // 有界摘要（≤ limits.evidence_chars，默认 800）
	Retried  bool   `json:"retried,omitempty"` // 是否重跑确认（flaky 分流标记）
	Flaky    bool   `json:"flaky,omitempty"`
}

type CheckItem struct {
	Req    string `json:"req"`
	Passed bool   `json:"passed"`
	Note   string `json:"note,omitempty"`
}
```

TL 侧验收判定（skill 契约）：`SelfCheck` 全 `passed=true` 且 `Tests` 全部 `Passed=true` 且存在对应命令的 `TestResult` 时，才允许进入 verify 门；`Status != done` 时不得声明完成。

### 2.3 BlockerReport（Programmer → TL：阻塞上报）

```go
type BlockerReport struct {
	TaskID    string   `json:"task_id"`
	BlockType string   `json:"block_type"` // missing_spec | dependency | environment | permission | budget_exhausted
	Question  string   `json:"question"`
	Options   []string `json:"options,omitempty"`
	Evidence  []string `json:"evidence,omitempty"`
}
```

分级升级规则（架构 §3.3）：Programmer 产出 blocker → TL 决策（rescope / 补规格 / 调依赖 / 升级人工）；TL 决策不了才 `task_needs_user_decision`（DecisionQuestion/DecisionOptions 复用现有终态协议）。`budget_exhausted` 必须携带已耗轮次与局部产物（`PartialProgress`），TL 据此决定 retry / replan / abandon。

### 2.4 ReviewRequest / ReviewVerdict（TL ↔ 独立评审）

```go
type ReviewRequest struct {
	TaskID     string       `json:"task_id"`
	Diff       string       `json:"diff"`      // diff 范围：commit 区间或 worktree ref（如 "T-01..merge"）
	Tests      []string     `json:"tests"`     // 新增/修改的测试文件
	Acceptance []Acceptance `json:"acceptance"` // 评审依据的验收标准
	Baseline   string       `json:"baseline"`
}

type ReviewVerdict struct {
	TaskID         string  `json:"task_id"`
	Verdict        string  `json:"verdict"`    // approved | blocking_issues | suggestions
	BlockingIssues []Issue `json:"blocking_issues,omitempty"`
	Suggestions    []Issue `json:"suggestions,omitempty"`
	TestQuality    string  `json:"test_quality"` // adequate | hollow（空洞测试拦截，评审必答项）
	ReviewerID     string  `json:"reviewer_id"`  // 独立 Session/账号 ID（写者不给自己打分的审计证据）
}

type Issue struct {
	Severity string `json:"severity"` // P0 | P1 | P2
	Location string `json:"location,omitempty"`
	Message  string `json:"message"`
}
```

review 节点输入输出契约要点：
- 输入 = ReviewRequest（含 diff 与测试文件，**不注入** programmer 的完整会话轨迹，也不注入 TL 的全局上下文）。
- 输出 = ReviewVerdict JSON；`TestQuality=hollow`（无断言 / 未覆盖 REQ 通过条件）→ 自动视为 blocking（等价 verdict=blocking_issues）。
- `blocking_issues` 非空时任务回到测试链（fix 轮），修复后重新 verify → 重新评审（写者修、评审判，角色不合并）。

### 2.5 ReplanRequest（TL 内部决策记录）

```go
type ReplanRequest struct {
	TaskID   string `json:"task_id"`
	Reason   string `json:"reason"` // verify_failed | review_blocked | requirement_changed | budget_exhausted
	Rounds   int    `json:"rounds"` // 已耗修复轮次
	Option   string `json:"option"` // rescope | retry | abandon | escalate
	Proposal string `json:"proposal"` // 方案说明（新任务卡 / 范围调整 / 升级人工）
}
```

TL 决策树：`requirement_changed` → rescope（新任务卡，重走门0 人工确认）；`review_blocked` 且 >2 轮 → 检查验收标准合理性（可能回门0 修订——但修订必须经人工）；`budget_exhausted` → escalate；其余 → retry（含深度上限检查）。

### 2.6 落盘布局（权威事实层）

```
<workspace>/
  REQUIREMENTS.md            REQ-01..N（人工锁定，agent 不得改）
  DESIGN.md                  模块划分 / 文件归属 / 任务切分依据
  .seelex/a2a/tasks/<T-xx>.json    TaskAssignment 落盘（TL 写）
  .seelex/a2a/results/<T-xx>.json  WorkResult 落盘（Programmer 终态写）
  .seelex/a2a/reviews/<T-xx>.json  ReviewVerdict 落盘（评审写）
  .seelex/pilot/metrics.json       M4 起的试点度量（任务/轮次/耗时/token/人工门次数）
```

设计理由：状态落盘是 context rot 防护的第一道结构（试点 R1：状态放文件不放窗口）；`--resume` 恢复时任务卡与结果文件即事实源，不依赖压缩摘要。

---

## 3. TL 决策规则

### 3.1 任务粒度切分策略（按优先级，前三条同时满足才切）

1. **验收标准边界优先**：一个 REQ 的全部通过条件必须在同一任务内闭环（任务 = ≥1 个完整 REQ）。REQ 间通过条件相互依赖（验收耦合）→ 合并为同一任务，**不硬拆**。
2. **文件所有权第二**：同一文件只允许一个任务写；共享文件只读。写集冲突的任务视为同一组（串行或合并）。这是 worktree 并行合并安全的文件级前提（试点 R10）。
3. **工作量第三**：「宽而浅」，估算 <90 分钟人工等价工作量；超出 → 拆（前提是满足 1、2，否则宁可串行）。
4. **依赖任务分组**：下游任务验收依赖上游产物 → 串行分组，组内并行。

切分信号（TL skill 内置检查）：
- 任务内出现跨 REQ 相互依赖 → 任务粒度太小，合并。
- 任务估算显著 >90 分钟但 REQ 边界不允许拆 → 接受任务较宽（REQ 优先于工作量），并增大该任务预算。
- `Acceptance` 中任何一条通过条件无法写成命令 → 该 REQ 不合格，退回需求分析（门0 前置）。

### 3.2 并行度决策

- **默认串行**：Effort=medium 阶段强制串行（≤4 节点，试点 §3.1 口径）；放开并行的前提是 **独立 worktree + 文件写集互不重叠 + 单一合并目标** 同时成立。
- 放开后默认 `max_concurrency = min(组内任务数, PlanPolicy.concurrency)`；`fork_subagents` 的 `max_concurrency` 参数即此值。
- **读依赖并行允许**：多个任务读同一基线文件 → 各自 worktree 有副本，可并行。
- **冲突兜底**：worktree 变基 + 合并审批现成（`finishNodeWorktree`），冲突按合并审批流程处理，不静默覆盖。

### 3.3 失败回环三选一（推荐：② DAG 失败边）

| 方案 | 机制 | 优点 | 缺点 | 结论 |
|---|---|---|---|---|
| ① tasklist 手写循环 | TL 在同一会话内驱动 fix→test→verify | 零产品改动，M1 即可验证闭环语义 | 非声明式、不可复用、观测弱 | **仅 M1 使用** |
| ② DAG 失败边 | verify 失败 → 自动路由 fix 节点 → 回到 test/verify；深度有界 | 声明式、节点状态天然打点可观测、与预算机制同构、改动集中 | DSL 需小扩展（条件边/回边）；实现侧需确认 | **推荐主线（M2 起）** |
| ③ replan | 隔离规划会话产出修复子计划 | 上下文干净、适合复杂修复 | 重量级、每次失败开新规划会话成本高 | **战略级重规划专用**（与 ② 互补） |

**推荐理由**：② 把「失败回环」变成 DAG 的静态拓扑语义——verify 节点失败即该节点终态为失败，TL/内核按失败边路由到 fix，fix 完成回到 test/verify；深度由节点预算与回环计数器共同约束（≤5 轮或 Effort 预算），超限转 `task_needs_user_decision` 且不静默。这最贴近"有界、可观测、可测试"的试点护栏要求。③ 保留给需求变更、方案分歧、多任务连锁失败——`taskTerminal.ReplanRecommended` 字段即为其预留信号。

**回环附加规则（对齐试点 R4/D5 护栏）**：
- verify 失败先重跑 1 次确认：第二次仍失败 → 分类；flaky（两次结果不一致或确认环境抖动）→ 记录隔离，**不改代码**；真 bug → fix 并说明根因。
- 同一任务修复轮 >5 或预算耗尽 → 升级（needs_user_decision 或 BlockerReport→TL 决策），禁止无限重试。
- fix 轮必须携带上次 verify 的失败用例列表（结构化回传，缺口 V1），禁止"盲修"。

### 3.4 TL 知识面维护

TL 上下文只保留：REQUIREMENTS.md 摘要 + 任务卡状态表（指针）+ 最近 ReviewVerdict + 失败统计。详细内容一律读文件，不常驻上下文（对应架构 §2.2）。

---

## 4. Programmer 执行契约

### 4.1 必须（写进 programmer skill，契约非建议）

1. **先读任务卡**：领取任务后先读 Goal / Acceptance / Scope / OutOfScope / Baseline / Budget；有疑问先 BlockerReport，不猜规格。
2. **实现前对照验收标准**：产出简短实现计划（改动文件 + 测试计划），随 WorkResult 回传。
3. **为每条 REQ 写/补测试**：测试必须真实断言（无断言 = 空洞测试，禁止）；测试意图以人工锁定的 REQ 为准。
4. **自检序列（固定顺序）**：跑 Baseline（回归）→ 跑本任务测试 → 逐条对照 Acceptance 填 SelfCheck → 全部通过才 commit。
5. **本地提交**：`git add -A && git commit -m "seelex/<task_id>: <摘要>"`（不 push、不 merge、不 checkout 主分支——框架的事，收尾契约现有块已约束）。
6. **回传 WorkResult**：最终回复输出结构化 findings（ChangedFiles / Tests / SelfCheck / Findings），merge-back 上卷 TL。
7. **verify 失败时**：先重跑 1 次确认分类（真 bug → 修复并说明根因；flaky → 记录不上报为 bug）。

### 4.2 不得（硬约束，违反即失败/评审阻断）

1. 修改 `REQUIREMENTS.md` 或任何 REQ 通过条件（测试意图人工锁定，单方修改 = 越权）。
2. 跳过 verify / 伪造测试结果（无测试证据不得声明完成；one-shotting 阻断）。
3. 触碰 OutOfScope 文件（含其他任务模块、基线测试、主分支配置）。
4. 自评自审（评审必须是独立 Session/账号；自己的结论不算 verdict）。
5. 无界重试（预算耗尽即 BlockerReport / 失败上报，不静默）。
6. 直接向用户升级阻塞（先 BlockerReport 给 TL）。

---

## 5. 门禁体系

| 门 | 位置 | 判定者 | 规则 | 违反后果 |
|---|---|---|---|---|
| 门0 需求锁 | `approve_req` 节点 | 人工 | REQ 清单每条有可测试通过条件（命令 + exit code）；确认后 agent 不得单方修改 | 未确认不得开工 |
| 门1 验证门 | verify 节点 | 机器 | `exit code = 0` 且测试文件存在且覆盖 REQ（空洞检测：无断言/未引用 REQ → 拒）；失败 → 有界回环 | 不通过不得 review |
| 门2 评审门 | review 节点 | 独立评审 | `verdict = approved` 才可 deliver；`blocking_issues` → 回 fix；`TestQuality = hollow` → 自动 blocking | 未通过不得 deliver |
| 门3 生产门 | `approve_deliver` 节点 | 人工 | 交付物含变更摘要 + 测试结果证据 + PR 描述；确认后 `task_complete` | 不确认不交付；永不自动 push/merge |

### 5.1 验收标准必须可测试（硬规则）

- 通过条件 = **真实命令 + 期望 exit code**（优先）+ 期望输出子串（可选）。示例（对齐试点 §5.1）：

```text
REQ-01（功能）: mdtable sort -c 2 对 3 列表格按第 2 列升序输出
  通过条件: go run . sort -c 2 < fixtures/in.md > out.md && diff -q out.md fixtures/expected.md；exit code = 0
REQ-02（错误处理）: 列号越界输出 stderr 错误
  通过条件: go run . sort -c 99 < fixtures/in.md 2>&1 | grep -q "column out of range"；exit code = 1
REQ-03（回归）: 既有功能不变
  通过条件: go test ./... -count=1 && go vet ./...；exit code = 0
```

- 禁止「由 LLM 主观判定通过」；verify 节点只认命令退出状态。
- 回归类 REQ 的通过条件 = 基线测试命令全绿（全量测试 + 主分支基线对比，试点 R5 护栏）。

### 5.2 评审独立性保证

- review 节点独立 Session + 独立账号（subagent role 路由现成）。
- ReviewRequest 只含 diff + 测试文件 + 验收标准，**不注入** programmer 会话轨迹与 TL 全局上下文。
- `ReviewerID` 必须与 programmer 的会话/账号不同（写者不给自己打分）。
- 空洞测试拦截（无断言测试 / 测试未覆盖 REQ 通过条件）→ `TestQuality=hollow` → 自动 blocking（试点 AC-9）。

---

## 6. MVP 步骤（M0–M4，对齐试点方案阶段）

### M0：协议与载体准备（S，1–3 天，不写产品代码）

- 内容：
  - A2A 消息 schema 落库：新增 `seelebridge/a2a/` 包（§2 全部类型 + JSON 校验 + 单测），纯类型不接线。
  - 试点仓库 B（`mdtable` 小 CLI + fixture 测试基线）与 REQUIREMENTS.md 模板。
  - `tl` / `programmer` 两份 skill 草案（TL 决策规则 §3、programmer 契约 §4、评审评审清单）。
  - scripted e2e 契约测试：用现有 scripted engine 断言消息序列（TaskAssignment → WorkResult → ReviewVerdict）与打点终态（无真实 LLM）。
- **验收标准**：
  - schema 校验单测全绿（含全部非法样例）；scripted 契约测试全绿。
  - 试点仓库基线 `go test ./...` 全绿。
  - 任务卡文件布局（§2.6）可被 skill 按模板生成。

### M1：串行双角色闭环（M，1–2 周）

- 内容：
  - tasklist 模式：TL=主代理手写驱动——拆 REQ → 写任务卡 → `fork_subagents` 下发（programmer 语义）→ TL 调 bash 跑 verify 读 exit code → 失败手写回环（方案①）→ 汇总 → 交付；全程 `task_check_node` 打点。
  - 3 个代表性任务（mdtable sort / CI 配置 / 重构）各跑 1–2 次；采集终态率、回环轮数、耗时、token（复用现有 telemetry）。
- **验收标准**：
  - ≥2/3 任务到达「测试通过 + 交付物」终态；试点仓库零回归。
  - 打点与 GUI tasklist 勾选一致；任务卡/结果文件生成完整。
  - 失败任务记录完整样本（含失败原因分类），不丢弃（试点 R11）。

### M2：角色固化（M，1–2 周）

- 内容：
  - 三个 S 级契约落地：**V1** verify 命令绑定 + 失败输出结构化回传 + 重跑 1 次确认；**R1** review 节点契约（ReviewRequest → ReviewVerdict，独立账号路由）；**D1** deliver 输出契约（变更摘要/测试证据/PR 描述 + 本地 commit 封装）。
  - **L1** DAG 失败边接入（方案②：verify 失败 → fix → test → verify，深度 ≤5）。
  - TL 决策规则（§3）固化为 tl skill；双角色进 Plan DAG 形态（架构 §4.2 模板）。
  - SubAgentTree 树形视图接通（P0 项，试点 M2 观测前提）。
- **验收标准**：
  - 同一批任务 Plan DAG 模式结果 ≥ M1（或更优）；失败回环自动且有界。
  - verify 失败输出含具体失败用例（结构化回传）；DAG 事件在 GUI 完整可见。
  - 并发节点写入无冲突（medium 先串行；放开并行的前提验收见 §3.2）。

### M3：门禁与护栏（S–M，1 周）

- 内容：
  - 空洞测试拦截：verify 校验测试文件存在 + 覆盖 REQ；review 的 `TestQuality=hollow` → blocking。
  - 预算上限：单任务 token 上限配置化（如 30 万，按 Effort 缩放），超限转 `task_needs_user_decision` 且不静默。
  - flaky 分流护栏全量落地（重跑确认 + 分类记录）。
- **验收标准**：
  - 假绿防御通过：构造「测试空洞」任务，verify/review 必须拦截（试点 AC-9 负向场景）。
  - 超限任务显式升级，报告含 PartialProgress；无「静默重试 >5 轮」样本。

### M4：固化与观测（S–M，1 周）

- 内容：
  - 度量落盘：`.seelex/pilot/metrics.json`（任务/轮次/耗时/token/人工门次数/失败分类统计）；TL 报告模板（交付物 + 变更摘要 + 测试证据 + 下一步建议）。
  - 试点评审：确定后续投入方向（headless 无人值守、checkpoint 回滚、跨会话记忆），同步进 roadmap（对齐试点 §9 承接表）。
  - 试点文档回写（实际数据 + 修正项），形成可重复的「敏捷 A2A 试点 SOP」。
- **验收标准**：
  - SOP 可在试点仓库重复执行且数据可比；度量文件格式稳定。
  - 输出「缺口 V1/R1/D1/L1/A1 的落地评审结论」（继续产品化 or 维持试点资产）。

---

## 7. 风险护栏

| 风险 | 现象 | A2A 层缓解 |
|---|---|---|
| context rot | 长循环中 Programmer 自相矛盾/漏 import | 状态落盘（任务卡/REQ/结果文件）；Programmer 上下文有界裁剪（Scope 内文件 + 有界 evidence）；TL 只留指针 + 摘要；每节点独立 Session |
| 假绿（测试空洞） | 无断言测试显示绿勾 | 测试意图人工锁定（门0）；空洞检测（门1 verify 校验测试文件）；评审必答 `TestQuality`（门2） |
| flaky 投机修复 | 把 flaky 当真 bug 无限重跑（5–30 轮） | verify 失败先重跑 1 次确认；分类后 flaky 记录隔离、不改代码；回环深度 ≤5 |
| 无验证环回归 | 既有功能被破坏（~12% 回归率） | 门1 必跑全量测试 + Baseline 命令；回归类 REQ 通过条件 = 基线全绿 |
| one-shotting | 上下文耗尽后半成品宣布完成 | WorkResult 必须含 Tests 证据 + SelfCheck 全通过；无证据不得进 verify/deliver |
| 越权/自审批 | agent 自审自合 | Programmer 不得自评（门2 独立账号 + ReviewerID 审计）；不自动 push/merge；manual 权限模式 |
| 并行写冲突 | 多 Programmer 同写一文件 | 文件写集互不重叠（切分规则 2）；worktree 隔离 + 变基合并审批兜底；M2 前期串行 |
| 回环失控 | 修复循环无界消耗预算 | 深度 ≤5 + 预算上限；超限显式升级（needs_user_decision / BlockerReport），禁止静默 |
| 需求模糊 | 模糊 brief 导致失败率高 | 门0 人工澄清后才能开工；验收标准写不出命令的 REQ 退回需求分析 |
| 评审走过场 | 评审与 writer 同上下文同账号 | ReviewRequest 不注入 writer 轨迹；独立 Session/账号；verdict 结构化必答项 |

---

## 8. 度量与观测

| 度量 | 采集点 | 用途 |
|---|---|---|
| 到达终态率（per 任务形态 A/B） | task 终态事件 | M1/M2 形态对比 |
| 回环轮次分布 | fix 节点重复计数 | 任务切分质量与验收标准质量 |
| verify 失败分类（真 bug / flaky / 空洞） | verify 输出 + 重跑记录 | 护栏有效性 |
| 人工门次数与拒绝原因 | approve 事件 | 门禁负担与验收标准质量 |
| 单任务耗时 / token | telemetry | 成本控制与预算校准 |
| 评审 verdict 分布（approved/blocking/suggestions + hollow） | ReviewVerdict 落盘 | 评审有效性 |

GUI 观测（M2 起）：Plan 面板节点状态 + 打点时间线 + SubAgentTree 树形视图（TL 根 → Programmer 节点 → 评审节点）；任务卡/结果文件可点击直达（文件链接现成）。

---

## 9. 缺口落地顺序（排期建议）

| 顺序 | 缺口 | 依赖 | 里程碑 |
|---|---|---|---|
| 1 | A1–A4（消息 schema 落库 + skill 契约 + 状态映射） | 无 | M0 |
| 2 | V1 verify 强化（命令绑定 + 失败回传 + 重跑确认） | A1 | M2 前置 |
| 3 | R1 review 契约 | 子代理机制现成 | M2 前置 |
| 4 | D1 deliver 契约 | V1 | M2 前置 |
| 5 | L1 DAG 失败边 | V1 | M2 |
| 6 | O1 SubAgentTree | 既有 P0 设计 | M2 |
| 7 | M1 跨会话记忆 | 试点数据 | M4 后 |
