# 阶段三开工简报：goal 看板 + V 模型 teamwork（leader 派活）

> **用途**：本文件是阶段三的**唯一权威输入**，供本轮派出的多个子代理并行开工。
> 每个子代理只读自己那一段，改自己那份文件集，不越界。
>
> **上游权威**：`docs/arch/teamwork-leader-worker-architecture.md`（M0–M4 目标设计 + §11 落地状态 + §12 已裁决不再开工的路径）、
> `docs/arch/agent-team-phase2-and-goal-vs-vmodel.md`（§4「goal 与 V 模型正交」）、
> `docs/devlog/2026-10-01-m4-deadcode-inventory.md`（§9 死代码逐条核实）。

---

## 0. 用户本轮的口径（原文要点，逐条落成工作项）

1. **goal 的调用方式**：从「席位制循环」改成「提示词规范 teammate 的使用方式」，让 goal
   **看起来更像一个 skill，而不是一个内置工具**；`/`（斜杠命令 / 内置工具面）下面的 goal 工具族可以处理了。
2. **goal 维护的 active seq**：从「原来的用户输出」变成 **agent 自己总结的 goal 看板**；
   agent 可以对看板内容做改动（**打点** + **更改 goal 目标内容**）；**active seq 在上下文里的位置不变**。
3. **goal 提示词**要让唤出来的 teamwork **整体遵循 V 模型开发范式**；代码工作遵循
   **先契约、再做契约实现**；**testcase 不只是写代码的测试用例**——要做**性能热点查看**与
   **全局冒烟测试**，冒烟 = **computer use 把每个操作都试一遍**。
4. **mainagent = leader**：负责按阶段派活，并**读取每一个阶段的更改内容**（diff），
   据此维护 goal 看板、推进/修改 goal 的实现里程碑。
5. **不再碰的地方**：`jobs` 迁到 Seele 这条路已经走完并认定失败
   （`docs/arch/teamwork-leader-worker-architecture.md` §12.4：`sink` 形状做不到会话事件流尾部追加），
   **本轮及以后不再开工，也不再等 §12.3 的契约增补**。
6. **死代码**：整体代码冗余，要删死代码、尤其**有误导性的陈旧代码**；
   **删一波 → 提交一次 → 全局冒烟 → 没问题再删下一波**（每轮可回溯）。
7. **Seelex 配置**：做出配置项，**通过配置决定程序是否同意多进程启动**。

---

## 1. 生态位：谁是 leader，谁是 worker（口径锁定）

- **mainagent = leader**：派活（`team_dispatch`）、读每阶段产物（git diff / 里程碑内容）、
  维护 goal 看板与里程碑。**不**用「座位」表达自己。
- **teammate = worker**：一人一会话一 worktree，一角色一 teammate；生命周期走 `team_retire`。
- **顺序的唯一事实** = `team_plan` 的 `stages[].depends_on`（不是隐式轮转）。
- **goal 域位置不变**：`goal.Controller` + 终态 gate + 逃生仍是「意图 + 完成判定 + 逃生」的宿主，
  只是**驱动方式**从席位环换成提示词驱动的 leader 派活。

---

## 2. 工作项拆分（四份互不重叠的文件集）

### W1 — 提示词层：`$goal` / `$teamwork` 重写（只改 plugins/）

**文件集**：`plugins/default/goal/SKILL.md`、`plugins/default/teamwork/SKILL.md`、
`plugins/default/README.md`（只改 Skill 清单/说明段）、必要时 `plugins/default/plugin.md`。

**必须写进去的规范**：

1. **goal = 看板维护者**（取代旧的 `[GOAL] 进度: 2/5` 文本块）：
   - 看板由 **agent 自己总结**（不是复述用户原话）；
   - agent 每阶段结束**做打点**，并可**修改 goal 目标内容**（正文/完成条件）；
   - 看板在上下文里的**位置不变**（沿用既有 active seq 的位置，W3 负责实现）。
2. **V 模型开发范式**（整体 teamwork 遵循）：
   - 左腿：需求 → 设计 → 模块分解；右腿：验收测试 → 集成测试 → 单元测试；
   - **每个左腿阶段 ↔ 一个右腿验证阶段**配对（V 模型的核心是「验收可追溯」）；
   - 顺序写进 `team_plan` 的 `stages[].depends_on`，末尾一个 review 阶段（review worker 有「牙齿」：只读 + 受限执行位）。
3. **代码工作先契约后实现**：先定接口/契约（签名、DTO、错误语义、不变式），
   契约评审通过后再做契约实现；契约变更 = 显式 replan，不偷偷改。
4. **testcase 不等于「写代码的测试用例」**，至少三层：
   - 单元/集成用例；
   - **性能热点查看**（profile / benchmark / pprof，指出热点与量级，而不是"看起来很快"）；
   - **全局冒烟 = computer use 把每个操作都试一遍**（逐个入口点一遍：打开、点按、输入、切换、关闭、恢复），
     并把「试过哪些操作、哪一步失败」写进证据。
5. **leader 读每阶段产物**：每个阶段收尾 leader 要 `jobs_manage(op=fetch)` + 看 diff /
   检查点，然后把结论落到看板与里程碑（`team_milestone`，内容由 leader 撰写）。
6. **失败/收口**：以证据为准；阶段推不动先看证据，再决定重派 / 收窄计划 / `task_needs_user_decision`。
7. 明确写出：**不再有席位轮转**；`goal_*` 系列内置工具**不再是主路径**（W3 会收敛它），
   goal 通过**提示词 + 团队作业面**驱动。

**纪律**：不改任何 `.go`；写得像 skill（可执行的动作规范），不写"目标设计"式散文。

### W2 — 配置层：多进程启动开关

**文件集**：`config/seelex.yaml`、`seelexctx/limits.go`（+ 新测试）、`config/README.md`、
`main.go`（**只加启动期闸门**，不动其它）、必要时新测试文件。

**要求**：
- 新增配置（建议落在 `limits` 之外的顶层块或 `limits.runtime`，命名如
  `runtime.allow_multi_process: false`）；**默认 false = 单实例**（与现状一致：
  `sessionstore/data_root_lock.go` 的「单数据根 = 单进程写者」）。
- **代码零值 = 关**（沿用本仓 `async_exec` / `context_compaction_summary` 的纪律：
  整块缺失或显式 false 都走"不允许"路径，可一键回滚）。
- 允许时：第二个进程**不再被数据根锁拒绝**（或按你选定的、有据可依的语义），
  并在文档里写清**代价**（谁保证一致性）。
- 启动期显式报错/拒绝要有可读信息（对齐 `ErrDataRootLocked` 的口径）。
- 测试两条臂：默认（拒绝/单实例）与开启（放行）。

### W3 — goal 看板（active seq 换源）+ goal 工具族收敛

**文件集**：`application/core/goal/*`、`application/core/goal_service.go`、
`application/core/goal_coordinator.go`、`register_goal_tools.go`、
`seelebridge/tools/policy.go`、`seelebridge/runtime.go`（只改 GoalActive 接线）、
`application/core/README-goal.md`、相关 `_test.go`。

**要求**：
1. **先定位**：找出「active seq / 活跃 goal 材料」**当前在上下文里的确切落点**，并写清楚
   （候选：`advanceAfterChat(ctx, sessionID, detail)` 的 `detail` 走 `work.progress` 帧 →
   `TLSessionEmbed.RenderText()`；以及 `injectGoalDirectives` / `formatDirectiveText` 注入引擎受信区的位置）。
   **位置不变**，只换内容来源。
2. **换源**：内容 = **agent 自己总结的 goal 看板**（打点列表 + 当前目标/完成条件 + 里程碑状态），
   不再是「用户/EXEC 的原始输出文本」。
3. **agent 可改看板**：允许 agent 面
   - **打点**（`goal_update` 的 milestone/finding/decision/risk 已有）；
   - **修改 goal 目标内容**（现被 `authorizeAgentGoalMutation` 拦住；按用户要求放开，
     但要留审计、要有界、要能被 gate 读到）。
4. **收敛内置工具面**：goal 从「内置工具驱动」变成「提示词驱动」——
   `isGoalTool` 门控、`GoalActive` 接线、`registerGoalTools` 的注册面按新口径调整
   （保留什么、退什么，须给引用事实 + 理由；**先建新面、后撤旧面**，别把现有用例删成空覆盖）。
5. **边界**：不动 `jobs` 契约、不动 seele 侧、不动席位/团队环的既有逃生记账。

### W4 — 死代码第 1 轮（删一批 + 提交一次）

**要求**：
- 先产出**引用事实**（`grep`/`go vet`/`deadcode` 之类，只算生产调用点，排除 `_test.go`），
  再动刀；`test-only` 的删除 = **改规格**，必须连带改用例并在提交信息里说明。
- 本轮**只删能证明无消费者**的干净条目 + **修正误导性陈旧注释/文档**；
  `blocked` 条目（等新面接管的）**不要动**，列进下一轮清单。
- **一次提交**（`refactor(...): ...`），提交信息写清「删了什么、为什么现在能删、回归证据」。
- 回归证据：`go build ./...`、`go vet`（相关包）、`go test`（相关包）。
- 产出 `docs/devlog/2026-10-01-m4-deadcode-round1.md`。

---

## 3. 全局纪律（每个子代理都遵守）

- **现状核实优先**：所有断言给 `文件:符号`；没有证据的写 **Hypothesis**。
- **不许破坏既有用例**：规格变了就改用例并在提交信息/文档里说明改了哪条、为什么。
- **中文注释与提交信息**（本仓惯例）；提交信息 `type(scope): 说明`。
- **不动** `_tmp` / `_scratch` / `vendor` / `dist`；不动 `seed`/`go.mod` 的 replace。
- **不碰** §12.4 裁决的 `jobs → Seele` 迁移路。
- 结束时给出：改动文件清单、`go build ./...` 结果、相关包测试结果、以及你自己发现的**未决问题**。
