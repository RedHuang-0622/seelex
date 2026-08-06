# Seelex「需求→coding→test→product」自迭代试点方案

> 状态：Draft，待确认后进入实现
> 日期：2026-08-06
> 背景：竞品格局调研见同目录 `research-agent-landscape-2026-08.md`。本方案是调研结论的落地重点（用户倾向方向）。
> 原则：**不改动产品代码即可先验证闭环价值**——先用现有 harness 手工驱动跑通试点，再逐步把试点过程固化为产品能力；试点过程中发现的缺口（headless、checkpoint、定时等）按调研文档优先级排期。
> 文档结构：目标 → 竞品共识（六环 + 失败模式）→ 能力拼装图 → 试点范围 → 工作流设计 → MVP 步骤 → 验收标准 → 风险 → 演进。

---

## 1. 目标

在 seelex 上运行一个「**需求 → coding → test → product**」的自迭代试点：给定一句需求，seelex 自动完成 需求分析（产编号可测试验收标准）→ 设计 → 编码 → 测试 → 真实测试验证 → 失败自动回环 → 独立评审 → 交付物（变更摘要/测试结果/PR 描述），全程在 GUI/TUI 上可观测、关键点有人工门。

试点的双重目的：
1. **验证闭环价值**：用真实小任务度量「需求到测试通过再到交付物」的端到端成功率、迭代轮次、耗时与成本，回答"seelex 的现有 Plan + 子代理 + 打点体系能否承载自迭代产品"。
2. **牵引能力补齐**：试点暴露的缺口（headless 执行、verify 回环、review 节点、checkpoint、定时化）按调研文档的优先级清单排期落地——试点是 roadmap 的「需求挖掘器」。

非目标：不做自动合并/自动发布（人工生产门必须保留）；不做跨仓库；不引入 OS 级沙箱（用现有 PathGate + 独立 workspace 兜底）。

---

## 2. 竞品共识：闭环的六环骨架与已知失败模式

### 2.1 六环骨架（Devin / Factory / Claude Code / Replit 2026 年收敛共识）

| 环 | 共识机制 | 行业证据 |
|---|---|---|
| ① 需求捕获 | 验收标准以**编号、可测试形式写死**（Devin 5-part prompt、Anthropic 200+ 条 `passes:false` JSON 清单） | Devin spec-to-PR；Claude Long-Running Agent Harness |
| ② 任务分解 | 主代理拆子任务，任务切片"宽而浅"（<90 分钟人工工作量 + 客观验证机制）；可并行（Devin Managed Devins、Replit 10 并发、Factory droid 分工） | Devin / Replit / Factory |
| ③ 执行 + 测试反馈 | 核心循环 `写→测→读失败→改→重测`；**适应度函数 = 真实测试 exit code / CI 状态，不用 LLM 当裁判** | ruflo、Claude AutoFix、Factory Test Droid（TDD-first） |
| ④ 验证门 | 机器可验证的 `verify`（测试+类型+linter）是"完成"的唯一权威；**独立评审上下文**（写者不给自己打分：独立 Claude 调用/独立 droid） | Claude security-guidance、Dynamic Workflows 对抗验证、Factory Review Droid、Copilot 自我评审 |
| ⑤ 交付/回滚 | 产物 = 可评审 PR + 自动合并（可选）；会话级 checkpoint 可回滚（Devin `/fork` `/revert`、Claude /rewind） | Devin、Claude Code |
| ⑥ 人工门 | "低风险自动、生产人工"：agent 只能提案不能决策；审批按风险分级配流程重量 | Devin 人工检查点、agent-sop"永不自我发布"、Copilot 评审队列 |

### 2.2 已知失败模式（试点护栏的直接依据）

| 失败模式 | 现象 | 试点护栏 |
|---|---|---|
| context rot | 长循环中漏 import、自相矛盾（60 分钟起明显）；1M 上下文下精度仍衰减 | 每节点独立 Session + 验收标准文件外置（状态放文件不放窗口） |
| Compaction 假事实 | 压缩摘要把"进行中"记为"已确认"（arXiv:2607.13071 实测） | verify 断言外部真实状态（exit code + 文件存在），不依赖压缩摘要 |
| 假绿（测试空洞） | 无测试时"绿勾只证明能跑不证明正确"；agent 自写自测形成循环验证 | 测试意图由用户在 approve_req 锁定；review 节点独立审测试质量 |
| flaky 误诊 | AutoFix 把 flaky 当真 bug，产生投机式修复 + 无限重跑（5–30 轮、10 倍成本） | verify 失败先重跑 1 次确认，分类真 bug / flaky |
| 回归破坏 | 无真实验证环时约 12% 既有功能被破坏（CoderCup） | 验证门必跑全量测试 + 主分支基线对比 |
| 一次性冲刺 | 上下文耗尽后半成品宣布完成 | 交付物必须含测试结果证据，无证据不 deliver |
| 越权/自审批 | 给 agent 自审自合的工具链构成护栏缺位 | approve 人工门 + 不自动 push + manual 权限模式 |
| 需求模糊 | 模糊 brief 失败率高（Devin 被指上限所在） | req 节点强制产编号验收标准，人工 approve 后才开工 |

---

## 3. 与 seelex 现有能力的拼装图

### 3.1 闭环环节 × seelex 能力映射

| 闭环环节 | seelex 现有能力（证据） | 试点用法 | 缺口（试点要补） |
|---|---|---|---|
| 需求捕获 | `kind:agent` 节点 + 独立 Session + NodeScope + 父证据注入（`seelebridge/agent_node.go`） | req 节点产出 `REQUIREMENTS.md`（编号验收标准 REQ-01…，每条含通过条件） | 无产品缺口；靠试点 Skill/prompt 模板约束格式 |
| 需求确认（人工门 1） | `kind:approve` 节点 + 异步 ApprovalBroker + `ask_approve`（`application/approval/broker.go`） | approve_req 节点：用户确认验收标准（可修改后重确认） | 无（能力现成） |
| 设计 | agent 节点 | design 节点产出文件改动清单 + 测试计划 | 无 |
| 编码 | agent 节点（独立 Session、MaxLoops 15、账号池 subagent 角色路由） | code 节点实现；可按模块拆并行分支（forkexec，`SetMaxForkConcurrency`） | 无 |
| 测试 | agent 节点 + `verify` 节点 kind（Plan DSL 已有 verify/deliver 节点类型，`seelebridge/plan_factory.go`） | test 节点写测试；verify 节点跑真实命令（`go test ./... && go vet`） | **verify 节点当前语义需确认/强化**：接入真实命令 + 失败输出结构化回传（见 §5.3） |
| 失败回环 | 任务终态协议（`task_failed`/`task_needs_user_decision`）；replan（隔离规划会话） | verify 失败 → 修复循环（有界）→ 重验 | **无自动回环**：需 tasklist 手动循环（M1）或 DAG 失败边/replan（M2） |
| 独立评审 | 子代理机制现成（独立 Session + 独立账号） | review 节点（独立上下文审代码 diff 与测试质量，输出 verdict：approved / blocking_issues / suggestions） | review 节点输入输出契约未定义（新定义，改动小） |
| 交付 | `kind:deliver` 节点 + `task_complete` 终态 + `task_check_node` 打点 | deliver 节点产出交付物（变更摘要/测试结果/PR 描述 markdown）；git commit 封装（不 push） | 交付物输出契约未定义 |
| 交付确认（人工门 2） | approve 节点 | approve_deliver：用户确认后试点任务 `task_complete` | 无 |
| 可观测 | GUI Plan 面板（点击节点看会话记录/功能打点/事件时间线/工具活动）、tasklist 实时勾选（`task_check_node` → 前端 tasklist） | 试点全程 GUI 驱动：节点状态 + 打点 + 里程碑徽章 | **SubAgentTree 树形视图**（既有 P0 设计未上线，试点 M2 前优先补） |
| 可恢复 | 四后端持久化 + `--resume` + EventStore | 试点会话中断可续跑 | 无（能力现成） |
| 成本控制 | Effort 预算（lite/medium/high/max）、账号池租约（lease-until-EOF） | 试点统一 Effort=medium（48 loops，≤4 节点强制串行）起步 | token 预算上限配置化（试点配置项） |
| 权限 | seele.yaml LMRW + 路径分区 + manual 审批模式 | 试点 workspace 独立目录 + manual 模式 | 无（能力现成，禁止 full_access） |
| 无人值守 | — | M4 后试点转周期任务 | headless 非交互执行（方向 C1，M 级） |

### 3.2 两种执行模式（MVP 分两步走）

**模式一：Tasklist 串行（M1，先跑）**——主代理在 tasklist 模式串行执行整个闭环：每个环节结束立即 `task_check_node` 打点（前端 tasklist 实时勾选），verify 由主代理调测试工具执行并读 exit code，失败则在同一预算内修复重验，终态 `task_complete`。零新增产品代码，先验证闭环语义与验收标准设计。

**模式二：Plan DAG 子代理（M2，加固）**——闭环固化为 `plan_load` JSON DSL：req/design/code/test/review 各为 `kind:agent` 节点（独立 Session + 独立账号 + 预算），approve 为人工门，verify 为确定性验证节点。依赖 §5.3 的 verify 强化与失败回环方案。

---

## 4. 试点范围与载体选型

### 4.1 范围边界（In / Out）

**In**：
- 单仓库、单 workspace（独立临时目录，`workspace` 绑定机制现成）
- 小型需求：一个自包含功能 / 一个小重构（行业建议"宽而浅"切片，<90 分钟人工工作量）
- 语言限定 Go（试点仓库用 Go，复用现有测试基建与 go vet）

**Out**：
- 自动 push / 自动合并 / 自动部署（人工生产门保留）
- 跨仓库、外部 ticket 系统接入（Devin 式 Auto-Triage 是后话）
- 定时化执行（试点成功后的承接形态，见 §9）
- 沙箱升级（沿用 PathGate + 独立目录，不并线做 OS 沙箱）

### 4.2 载体选型（两个候选，推荐先 B 后 A）

| 候选 | 内容 | 优点 | 风险 | 结论 |
|---|---|---|---|---|
| A. seelex 自身 dogfooding | 试点任务 = seelex 的插件/文档/小功能改进 | 真实反馈、直接产生价值 | 改自己 = 破坏性风险叠加 | **M2 后期启用**（闭环稳定后） |
| B. 独立示例仓库 | 小 Go CLI 工具（如 `mdtable`：markdown 表格列排序） | 隔离风险、结果可独立评判 | 价值间接 | **M1 首选** |

推荐：M1 全部用 B；M2 后半段可对 seelex 自身跑"低风险任务"（如 `go vet` CI 步骤、README 修订类），检验闭环在真实仓库上的表现。

### 4.3 试点任务样例（M1 首批）

1. `mdtable sort`：读取 markdown 表格，按指定列排序输出（小型、可测、验收标准好写）。
2. 为示例仓库补充 `go vet` 进 CI 的 workflow 文件（配置类，验证"写配置"型任务）。
3. 重构：把一个函数拆成接口 + 实现，行为不变（验证"重构类"任务，回归风险敏感）。

---

## 5. 试点工作流设计

### 5.1 验收标准格式（req 节点强制输出，人工门锁定）

```
REQUIREMENTS.md
- REQ-01（功能）: `mdtable sort -c 2` 对 3 列表格按第 2 列升序输出
  - 通过条件: 对照 fixture 输出 diff 为空；exit code = 0
- REQ-02（错误处理）: 列号越界时输出 stderr 错误并 exit code = 1
- REQ-03（回归）: 既有功能 `mdtable help` 行为不变（基线测试全绿）
```

规则：每条 REQ 必须有「通过条件」，且通过条件可由真实命令判定（不允许"由 LLM 主观判定"）；REQ 清单由用户在 approve_req 确认后不可被 agent 单方修改（测试意图锁定）。

### 5.2 Plan DAG 模板（M2 目标形态，JSON DSL 草案）

```json
{
  "entry": "req",
  "nodes": {
    "req":            { "kind": "agent",  "input": "分析需求，产出 REQUIREMENTS.md（编号可测试验收标准）+ 目标文件清单" },
    "approve_req":    { "kind": "approve", "input": "用户确认验收标准（可修改后重确认）" },
    "design":         { "kind": "agent",  "input": "依据 REQ 产出设计：改动文件、接口、测试计划" },
    "code":           { "kind": "agent",  "input": "按设计实现，对照 REQ-01..N" },
    "test":           { "kind": "agent",  "input": "编写/补充测试，覆盖 REQ 通过条件" },
    "verify":         { "kind": "verify", "input": "执行 go test ./... && go vet；exit code 即验证结果" },
    "fix":            { "kind": "agent",  "input": "读取 verify 失败输出（含具体失败用例），修复并说明根因" },
    "review":         { "kind": "agent",  "input": "独立上下文审查代码 diff 与测试质量，输出 verdict" },
    "deliver":        { "kind": "deliver", "input": "产出交付物：变更摘要/测试结果证据/PR 描述（git commit 本地化，不 push）" },
    "approve_deliver": { "kind": "approve", "input": "用户确认交付物" }
  },
  "edges": [
    ["req", "approve_req"], ["approve_req", "design"], ["design", "code"],
    ["code", "test"], ["test", "verify"],
    ["verify", "fix"], ["fix", "test"],                 // 失败回环（有界）
    ["verify", "review"], ["review", "deliver"],
    ["deliver", "approve_deliver"]
  ]
}
```

### 5.3 关键机制确认项（实现侧待验证，标 * 的为试点前置小改动）

| 机制 | 现状 | 试点要求 | 实现侧动作 |
|---|---|---|---|
| verify 节点语义 * | Plan DSL 已有 `verify`/`deliver` 节点类型；当前为确定性节点（输出=输入类） | verify 必须执行真实命令并以其 exit code 决定成败 | 确认/强化 verify 节点：绑定命令模板、失败输出结构化回传（对照 REQ 失败用例列表）——**S 级改动** |
| 失败回环 | 无自动回环；DAG 静态拓扑 | verify 失败 → fix → test 循环，有界（≤5 轮或 Effort 预算） | 三选一：① M1 用 tasklist 模式主代理手写循环（零改动）；② 静态 DAG + 失败边语义（需确认 DSL 是否支持条件边，可能小扩展）；③ 复用 replan（隔离会话）产出修复子计划——**按实现侧确认结果选择** |
| review 节点 * | 子代理机制现成 | 独立上下文 + 独立账号角色（subagent role 路由现成）；输出 JSON verdict | 定义 review 输入输出契约（读 diff + 测试文件 → verdict）——**S 级改动** |
| 交付物契约 * | deliver 节点存在 | 输出 markdown 交付物 + git commit 封装（不 push） | 定义 deliver 输出 schema——**S 级改动** |
| token 预算 | Effort 预算现成 | 单任务上限配置（如 30 万 tokens，按 Effort 缩放），超限转 `task_needs_user_decision` | 试点配置项（不改代码即可起步，M3 固化为产品配置） |

### 5.4 人机交互设计

- **人工门三个**：approve_req（验收标准锁定，最关键）、approve_deliver（交付确认）、`task_needs_user_decision`（回环超限/方案分歧/需求变更时）。
- **审批流**：复用 ApprovalBroker 异步审批（GUI 审批对话框现成），拒绝时必须带回原因，agent 据此修订后重新提交。
- **越权约束**：试点任务全程 manual 权限模式；deliver 只做本地 `git commit`（不 push、不 merge）；任何自动"发布"动作在试点中不存在。

---

## 6. MVP 步骤

### M0：试点准备（S，1–3 天，不写产品代码）
- 建试点仓库 B（`mdtable` 小 CLI + fixture 测试基线）
- 写试点工作流 Skill/提示模板（验收标准格式、REQ 规则、回环纪律——即"试点规则书"，后续固化进产品 prompt 资产）
- e2e scripted 场景骨架：用现有 scripted engine 编写「需求→编码→测试失败→修复→测试通过→交付」的确定性契约测试（断言打点序列与终态，无真实 LLM）
- **验收**：scripted 契约测试全绿；试点仓库基线 `go test` 全绿

### M1：串行闭环验证（M，1–2 周）
- 用 seelex 自身（TUI 或 GUI）在 tasklist 模式跑通试点任务：req → approve → design → code → test → verify → 修复回环 → deliver，全程 `task_check_node` 打点
- 3 个代表性任务各跑 1–2 次，采集：到达终态率、回环轮数、耗时、token（复用现有 telemetry）
- **验收**：≥2/3 任务到达"测试通过 + 交付物"终态；试点仓库零回归；打点与 GUI tasklist 勾选一致

### M2：Plan DAG 子代理闭环（M，1–2 周）
- §5.3 前置小改动落地：verify 命令绑定与失败输出回传、review/deliver 契约（三个 S 级）
- 闭环固化为 §5.2 DAG；失败回环按实现侧确认的方案接入
- 子代理分节点执行 + approve 人工门 + GUI Plan 面板观测（先补 SubAgentTree 树形视图，P0 项）
- 后半段对 seelex 自身跑低风险 dogfooding 任务（CI 配置类）
- **验收**：同一批任务 Plan 模式结果与 M1 一致或更优；DAG 事件在 GUI 完整可见；并发节点写入无冲突（medium 先串行）

### M3：护栏与质量（S–M，1 周）
- 失败分类：verify 失败先重跑 1 次确认（flaky 与真 bug 分流，对齐 Claude AutoFix 教训）
- review 节点拦截空洞测试（无断言测试/测试未覆盖 REQ 的通过条件 → blocking）
- token/轮次预算上限 + 超限转 `task_needs_user_decision`
- **验收**：假绿防御通过（构造"无测试"任务，verify 必须失败）；review 能拦截空洞测试

### M4：试点固化（S–M，1 周）
- 试点运行报告模板 + metrics 落盘（`.seelex/pilot/`：任务/轮次/耗时/token/人工门次数）
- 试点结果评审：确定后续投入方向（checkpoint 回滚 / 跨会话记忆 / headless + 定时化），同步进 roadmap
- **验收**：试点文档回写本方案（实际数据 + 修正项），形成"可重复的试点 SOP"

---

## 7. 验收标准（汇总）

| # | 验收项 | 标准 | 验证方式 |
|---|---|---|---|
| AC-1 | 端到端成功率 | scripted 契约 100%（确定性）；真实模型 smoke ≥2/3（3 个代表性任务） | e2e scenario + 手工记录 |
| AC-2 | 验证门强制 | verify 失败或测试文件缺失的任务不得进入 deliver；deliver 交付物必须含测试结果证据 | 场景断言 |
| AC-3 | 回环有界 | 单任务修复循环 ≤5 轮（或 Effort=medium 预算内）；超限转 `task_needs_user_decision` 且不静默 | 场景断言 |
| AC-4 | 人工门 | 试点流程必经 approve_req 与 approve_deliver；拒绝时带原因并驱动修订；无人工确认不发生任何 git commit | 场景断言 + 记录 |
| AC-5 | 零回归 | 试点任务执行后试点仓库 `go test ./...` 全绿（基线对比） | CI 命令 |
| AC-6 | 成本可控 | 单任务 token ≤ 预算上限（M1 阶段配置 30 万），超限显式上报 | telemetry |
| AC-7 | 可观测 | GUI Plan 面板显示全部节点状态/打点/事件时间线；tasklist 勾选实时且与终态一致 | GUI 冒烟 |
| AC-8 | 可恢复 | 试点会话中断后 `--resume` 续跑，不丢已确认的 REQ 与已完成的节点事实 | 手工验证 |
| AC-9 | 防假绿 | 构造"测试空洞"任务（测试无断言）被 verify/review 拦截 | 负向场景 |
| AC-10 | 打点完整性 | 每个环节结束有且仅有一次 `task_check_node`（或 plan_run 事件） | 事件断言 |

---

## 8. 风险与缓解

| # | 风险 | 等级 | 缓解 |
|---|---|---|---|
| R1 | 长循环 context rot（节点内漂移） | 高 | 每节点独立 Session；REQUIREMENTS.md 外置并每节点对照（外部化 checklist，Anthropic 10/10 vs 5/10 结论）；不依赖压缩摘要作事实 |
| R2 | 假绿（自写自测循环验证） | 高 | 测试意图在 approve_req 人工锁定；review 节点独立上下文审测试质量；verify 校验测试文件与 exit code |
| R3 | 修复循环失控 | 中 | 有界重试（≤5 轮/Effort 预算）+ 超限转 `task_needs_user_decision` |
| R4 | flaky 误诊导致的投机修复 | 中 | verify 失败先重跑 1 次确认；真 bug → 修复，flaky → 记录并隔离（不修改代码） |
| R5 | 越权/自审批 | 中 | manual 权限模式；deliver 仅本地 commit 不 push；approve 人工门不可绕过（复用 ApprovalBroker 终态收敛校验） |
| R6 | 沙箱缺失下的破坏性执行 | 高 | 试点 workspace 独立临时目录 + PathGate 约束；试点期间禁止 full_access；dogfooding 任务（M2 后期）只选低风险类型 |
| R7 | 成本失控 | 中 | token 预算上限 + 账号池租约 + M1 阶段统一 Effort=medium（48 loops/96 tool calls 硬顶） |
| R8 | 提前宣布完成（one-shotting） | 中 | 交付物强制含测试结果证据（AC-2）；无证据不得 deliver |
| R9 | 需求模糊 | 中 | req 节点强制产编号验收标准 + approve_req 人工门后才开工；模糊需求由人工门先澄清 |
| R10 | 并行子代理写冲突（M2） | 中 | M2 前期 Effort=medium 强制串行（≤4 节点）；放开并行的前提是分支文件分区不重叠（模块边界清晰的任务） |
| R11 | 试点结论被"成功任务"样本污染 | 低 | 记录含失败任务的全部样本；报告区分 scripted 与真实模型口径；厂商式"自报分数"不作数 |

---

## 9. 里程碑与后续演进

```
M0 准备 ──S── M1 串行闭环 ──M── M2 Plan DAG 闭环 ──M── M3 护栏 ──S–M── M4 固化 ──S–M
                                    │
                                    └─（并行/前置）方向 B1 SubAgentTree（P0 项）
```

试点成功后的承接形态（按调研文档方向清单排期）：

| 承接 | 依赖 | 优先级 |
|---|---|---|
| 闭环固化为产品能力（试点 Skill → 内置 skill/模板 + verify/review/deliver 契约入产品） | M3 | 高 |
| 跨会话事实记忆（试点教训/需求跨会话复用 → 方向 A1） | 试点数据积累 | 中 |
| 会话级 checkpoint（试点撤销/回滚需求 → 方向 B3，既有 P0） | M2 后可并线 | 高 |
| headless 非交互执行（`seelex -p` + 护栏参数，试点无人值守前提） | M4 | 高 |
| 定时化：试点任务转周期运行（方向 C3，对齐 Codex Automations/Devin scheduled） | headless | 中 |
| 事件触发（GitHub PR 触发修复闭环，对齐 Copilot/OpenHands） | 定时化 | 低 |

试点数据同时服务于两条决策链：产品侧（能否产品化为"一键需求交付"）与工程侧（哪些 harness 缺口最影响闭环成功率——预计沙箱与记忆将因试点数据被强化优先级）。

---

## 10. 一句话

**用 seelex 现有的 Plan DAG + 打点 + 审批 + 子代理跑通一个"验收标准先写死、真实测试做裁判、独立评审把关、人工门收口"的迷你软件交付闭环；先用串行模式验证价值，再用子代理模式加固，试点暴露的缺口（verify 回环、review 契约、headless、checkpoint）按调研清单逐个补齐。**
