# 旧能力收口：A11–A15（历史叙述纠偏 + 保留面裁决）

日期：2026-10-15
范围：`docs/2026-10-15-product-architecture-pre-design/legacy-capability-audit.md` 的
A11–A15 五项（A03–A06 见 [`2026-10-15-legacy-capability-retirement-a04-a06.md`](2026-10-15-legacy-capability-retirement-a04-a06.md)，
A07–A10 见 [`2026-10-15-legacy-capability-retirement-a07-a10.md`](2026-10-15-legacy-capability-retirement-a07-a10.md)）。

## 结论先行

本批**零业务代码删除**：A11/A15 是**历史叙述与状态矩阵纠偏**，A12/A13/A14 是**裁决 + 证据**
（A12 另加一条跨实现一致性用例）。没有持久字段、历史文件或磁盘数据被改动。

| ID | 裁决 | 落地动作 | 判据 / 验证 |
|---|---|---|---|
| A11 | 历史记录保留，**补"历史基线 / 现行路径"标注** | `docs/test/README.md`（两条 2026-09 报告行）、`docs/2026-09-16-team-work-record-dataflow/README.md`（退场前快照头）、`docs/2026-10-05-seelex-product-film-v2/{README,STORYBOARD}.md`（员工库镜头 = 历史画面） | 只加状态头与替代链接，正文一字未改；链接检查通过 |
| A12 | **不合并**两个角色会话号派生；补**跨实现一致性契约** | 新增 `seelebridge/teamwork/role_session_id_consistency_test.go` | `go test ./seelebridge/teamwork -run TestRoleSessionIDHelpersAgreeOnCanonicalInputs -count=1` 绿（非空规范输入下两实现逐字同解，无漂移） |
| A13 | **保留**恒空写路径，标注删除条件与删除清单 | `application/core/goal_team_recorder.go` 加裁决块（唯一目的 / 可达性 / 用户可见 TL 证据的独立路径 / 删除清单） | 守卫用例 `goal_team_recorder_test.go:TestRoleDraftRecordingIsUnreachableAfterJ1A` 仍在；`go test ./application/core -run 'RoleDraft|TLRound' -count=1` |
| A14 | **维持唯一实现**（不是待删代码） | 无代码改动，只复核 | `EncodeStages` / `DecodeStages` / `ClipPreview` / `ProgressOf` 全仓各只有一处定义（`seelebridge/workunit/progress.go`）；`go test ./seelebridge/workunit ./seelebridge -run 'Workunit\|Recovery\|Progress\|Stage' -count=1` 绿 |
| A15 | 只修**状态矩阵与现行架构段落**，旧 devlog 保留 | `docs/arch/teamwork-leader-worker-architecture.md`（D4 行、架构图注、§9 表逐行收口、§11 M1/M2/M4 行与结论更正） | 行文以"已退场/被替代 + 替代链接"改写，历史设计正文不删 |

## 逐项凭据

### A11 — 历史记录的现状标注

改的是**标注**，不是正文（历史报告与当时的实现描述必须保真）：

| 文件 | 改动 |
|---|---|
| [`docs/test/README.md`](../../docs/test/README.md) | 2026-09-10 A2A/恢复报告与 2026-09-08 治理循环真实 API 报告两行改标"**历史基线**"并写明退场时间点与现行路径（前者指向 sessionstore / teamwork 架构；后者写明 `goal_gov_next`/`goal_gov_break` 已随 2026-10-03 W3 退场，只剩只读 `goal.gov_snapshot`） |
| [`docs/2026-09-16-team-work-record-dataflow/README.md`](../../docs/2026-09-16-team-work-record-dataflow/README.md) | 顶部加"**历史工作包（退场前快照）**"：正文里的 `ensureGoalAgentTeam` 已于 2026-10-01 `970122c` 删除、席位轮转已于 2026-10-03 W3 退场，正文按当时事实保留 |
| [`docs/2026-10-05-seelex-product-film-v2/README.md`](../../docs/2026-10-05-seelex-product-film-v2/README.md)、[`STORYBOARD.md`](../../docs/2026-10-05-seelex-product-film-v2/STORYBOARD.md) | `06/07` 与 `crop-employee-library.png` 说明改标"**历史画面**"：右栏员工库/团队库/员工栏/发言调度面板已随 2026-10-12 J-1a 退场，截图不代表当前 UI |

**顺带发现的同类假事实（本轮一并修，只加状态不改正文）**：

- [`docs/gui/modules/right-sidebar.md`](../../docs/gui/modules/right-sidebar.md) 是 **GUI 权威设计**，
  但"Agent Team（状态子页）"整节仍以现在时描述员工库/团队库/员工栏/发言调度与 `Bridge.AgentTeam*`；
  已加退场状态头（2026-10-12 J-1a）并指向现行的 teamwork 看板设计。
- [`docs/gui/decisions.md`](../../docs/gui/decisions.md) 的 ADR-GUI-021（`@` 召唤团队）与
  ADR-GUI-022（`team.changed` 通告）状态从"已采用"改为"已退场/部分退场"。
- 复核中发现 `application/event.EventTeamChanged`（`team.changed`）**已无发布者**：只剩事件常量、
  白名单与 console 日志分支（`application/event/hub.go`、`application/core/aliases.go`、
  `application/console/backend_console.go`）。它与 A05 的 `TeamView.Schedule` 同族（**生产者消失、
  消费者只剩形状**），本轮**不动代码**，登记为后续可清理残边。

**A11 条目里已不成立的反证**：审计写"旧 `docs/arch/a2a-agent-team-factory.md` 正文仍写 seat ring"——
复核该文件 `seat`/`ring`/`席位`/`环` 零命中（除一条链接行），且文件头已有 J-1a 退场声明。该反证
**已被更早的退场轮次处理**，本轮不再动。

### A12 — 两个角色会话号派生：不合并，但钉住一致性

两个实现（判据不同，**不合并**）：

| 实现 | 位置 | 空值/空白语义 |
|---|---|---|
| `agentteam.RoleSessionID(mainSessionID, teamID, roleName)` | `application/core/agentteam/spec.go:263` | 空白 `team_id` 兜底 `dto.DefaultTeamID`（`"goal-a2a"`，历史身份分量）；三个分量都 `TrimSpace` |
| `teamwork.DefaultRoleSessionID(mainSessionID, teamID, roleName)` | `seelebridge/teamwork/teamwork.go:45` | 不做 team 兜底、只 trim 主会话号（输入已过 `ValidateTeamworkPlan` 的 `team_id` 非空校验） |

新增用例 `TestRoleSessionIDHelpersAgreeOnCanonicalInputs`（`seelebridge/teamwork`，该包已可依赖
`application/core/*`，不构成反向依赖）钉住"**非空规范输入必须逐字同解**"，覆盖普通三人组、
评审角色、默认团队名、Unicode 分量、下划线主体名与"无主会话归属"的退化形态。装配层仍可经
`teamwork.Options.DeriveRoleSessionID` 注入权威实现（当前生产走默认实现）。

### A13 — 恒空写路径：保留 + 写明删除清单

核实结果（均已写进 `goal_team_recorder.go` 的裁决块）：

- **唯一目的**：把 ADVISOR/TL 每回合原文落成 tl 角色 draft → sequencer 同步进主文档
  （`kind=tl_directive`、floor=tl）；它不是 goal 决策/审计的载体。
- **可达性**：`RecordTLRound` / `RecordMainTurn` / `ArchiveTLHistory` 三条来路都在
  `sessionAgentTeamViewJ1A` 的 `!view.Configured` 守卫处 no-op（与退场前"未装配团队"那一支
  **逐字同解**），守卫用例 `TestRoleDraftRecordingIsUnreachableAfterJ1A` 仍在。
- **用户可见 TL 证据不依赖本文件**：`GoalGovernanceView`（活动栈 / status / `RoundSteps`）、
  goal 的 `tl_directives`（`session_context.go` + headless `goal_tl_directives`）、
  GoalStack / GoalAudit / GoalBoard archive 都是独立路径。
- **未来删除清单**：本文件 + `goalTLRecorderFor` + `goalCoordinatorDeps.TLRecorderFor`
  （`goal_coordinator.go` 的 `sup.SetRoundRecorder` 调用点）+ `service_assembler.go` 注入 +
  goal 域 `TLRoundRecorder`/`TLHistoryArchiver` 端口与 `Supervisor.recorder` 分支；
  **保留** `AppendRoleDraft`/`SyncRoleDraft`/`RoleSnapshot`（headless `role.*` 仍在用）与
  role 字段编解码、旧 role draft 读取面。

### A14 — 唯一实现复核（无改动）

`EncodeStages`、`DecodeStages`、`ClipPreview`、`ProgressOf` 全仓各**只有一处定义**，都在
`seelebridge/workunit/progress.go`。层间差异只留在策略上，由这些用例钉住：
`seelebridge/workunit/contract_test.go:TestFinishPolicyIsTheOnlyDifference`、
`seelebridge/workunit/progress_test.go:TestUnitReaderCannotBranchOnKind`、
`seelebridge/workunit_chain_test.go:TestChainFinishPolicyIsTheOnlyLayerDifference`
（审计条目里写的 `workunit_single_path_test.go` 这个文件名**不存在**，实际锚点是上面三条）。

### A15 — 架构文档状态矩阵收口

`docs/arch/teamwork-leader-worker-architecture.md`：

- 顶部 D4 决策行与架构图的 `seat(...)` 加"已退场"标注；
- §7 加状态更新（该节是 D4 设计意图，非当前事实）；
- §9 死代码表逐行按**事后事实**收口：goal 治理座位循环 = 已退场（未按原计划保留成
  `KindSeat` Executor）、ADVISOR 座位 = 已落地为 review worker（`goalLLMEvaluator`）、
  座位派生读面 = 已随 A04 删除、旧顺序字段 = A05 read-compat、`RoleTurnRunner` 适配 = 已退场；
- §11 状态表 M1 行（`KindSeat` 已删）、M2 行（整行改判"已退场"、正文标为退场前快照）、
  M4 行（"待做" → "已收口"）与结论段的"已降级为 `jobs.KindSeat` 执行体"更正。
- 原有历史段落（设计目标、里程碑交付物）保留，只加状态与替代链接。

## 与其他条目的关系

- 与 AGENTS.md §8：本批**无重复实现被删除**；A12 的用例是**跨实现一致性用例**（同一份规范输入
  跑两遍派生），不是合并。
- 未执行、仍在账上的：A05 字段清除、A06 写面停用（都需版本化迁移 + 旧数据 fixture）、
  A13 的删除清单落地（需先确认用户是否还要"每回合原文逐字进主对话"这一层）、
  `team.changed` 生产者消失后的残边清理。

## 验证

- `go build ./...`、`go build -tags "gui,desktop,production" ./...`、`go vet ./...` 绿。
- `go test ./seelebridge/teamwork -run TestRoleSessionIDHelpersAgreeOnCanonicalInputs -count=1` 绿。
- `go test ./seelebridge/workunit ./seelebridge -run 'Workunit|Recovery|Progress|Stage' -count=1` 绿。
- `go test ./application/core -run 'RoleDraft|TLRound' -count=1` 绿。
- `go test ./... -count=1`、`git diff --check`、`gofmt -l`（改动文件）与 README 引用检查同上批口径。
