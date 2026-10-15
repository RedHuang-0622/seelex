# 旧能力退场：A07–A10（保留面标注 + 旧 wire 冷读 fixture）

日期：2026-10-15
范围：`docs/2026-10-15-product-architecture-pre-design/legacy-capability-audit.md` 的
A07–A10 四项（A03–A06 见 [`2026-10-15-legacy-capability-retirement-a04-a06.md`](2026-10-15-legacy-capability-retirement-a04-a06.md)）。

## 结论先行

本批**零代码删除**：A07/A08/A09 的裁决都是"**保留 + 标注 + 验证**"（它们本就是审查里的
"仍有效底层能力 / 观察面"一类，不是退场面），A10 只**新增冷读 fixture 与文档区分**。
没有任何持久字段、历史文件或磁盘数据被改动或清理。

| ID | 裁决 | 落地动作 | 判据（静态索引） | 验证 |
|---|---|---|---|---|
| A07 | **保留**（角色执行与权限是活动链） | `seelebridge/runtime_role_turn.go` 加状态头，明确本层不是退场面 | `runRoleRound` 的两个消费者（`Runtime.RunWorker` → team_dispatch、`runtime_goal_tl.go` 的 ADVISOR 评审）都活着；`AssignEmployeePermissions`（`runtime_tools.go`）在每轮角色回合起手落 `emp_<role>` 主体；`teammateToolFace` 的 `fork_subagents` 硬移除仍在装配点 | `go test ./seelebridge/... -run 'Role|Permission|Teamwork' -count=1` 绿 |
| A08 | **保留为 legacy 可选能力面**（端口 + 实现 + DTO 都不删） | `application/contract/ports.go`（`RolePromptPort`、`Dependencies.RolePrompt`）、`seelebridge/runtime_role_prompt.go`、`seelebridge/runtime_role_plugins.go`、`application/contract/dto/agentteam.go` 各自加状态与移除条件 | `RolePromptPort` / `OptimizeRolePrompt` / `SetRolePromptProvider` / `SetRolePluginsProvider` 全仓静态索引只剩契约定义、`application/core/aliases.go` 别名、`seelebridge` 实现与测试；组合根 `main.go` 自 J-1a 起不再注入来源（读面未注入 = 空集/内置回退）；活动插件来源是 `team_plan members[].plugins` 的独立透传链 | `go test ./seelebridge/... -run 'Role|Plugin|Prompt|Permission' -count=1` 绿（`runtime_role_prompt*_test.go`、`runtime_role_plugins*_test.go` 仍在钉回退与"插件只收窄"） |
| A09 | **保留只读观察面**（`goal.gov_snapshot`），退场控制面按未知方法拒绝 | `gui/headless.go` 的 `goal.gov_snapshot` case 加当前语义注释；`docs/arch/teamwork-leader-worker-architecture.md` §7 加状态更新 | `gui/headless.go` 只有 `goal.begin/update/propose_finish/status/gov_snapshot` 五个 case；`goal.gov_next` / `goal.gov_break` 落到 `default` 分支按"未知 goal headless 方法"拒绝；`TeamRuntimeFor` 已在 A03 删除 | `go test ./gui -run TestHeadlessGoalChainReplication -count=1` 绿（含 `gov_snapshot` 返回活动视图、`gov_break` 被拒的 no-regression guard） |
| A10 | **旧 wire 冷读 + 文档区分**（两种 "stage" 不可混） | `sessionstore/teamwork_test.go` 新增两个冷读用例；`sessionstore/README.md` 增"计划阶段退场 ≠ 执行进度打点存活"表 | `TeamworkPlan` 结构体无 `Stages`（Go JSON 忽略未知字段）；`TeamworkEventRetire` 常量只读历史、`team_close` 起不再有新写入；`NodeSessionRecord.StagesJSON` 由 `workunit.EncodeStages/DecodeStages` 活动编解码 | `go test ./seelebridge/workunit ./sessionstore ./seelebridge/teamwork -count=1` 绿 |

## 逐项凭据

### A07 — 角色执行与权限保留

保留对象与它们的活动调用点：

- 角色回合原语 `Runtime.runRoleRound`（`seelebridge/runtime_role_turn.go`）：worker 作业执行体
  （`Runtime.RunWorker` → `team_dispatch`）与 ADVISOR 评审（`runtime_goal_tl.go`）共用；
- 装配期权限落点 `Runtime.AssignEmployeePermissions`（`seelebridge/runtime_tools.go`）：
  把 `RoleSpec.ToolsPolicy` / `PermissionGroups` 落成 `emp_<角色名>` 主体条目；
- 工具面收窄 `Runtime.AssignEmployeePermissions` + `teammateToolFace` +
  `seeltools.ToolFaceForContext`（`seelebridge/tools/policy.go`）：权限门是安全边界；
- 权限/身份的现行契约文档：`docs/arch/agent-permission-subjects.md`。

**命名裁决（不做只改名式重构）**：审计提到可把"员工库登记 / Agent Team role registry"
改成更中性的 "teammate/role execution"。本轮**不改名**：一是这些标识符散布 DTO / 端口 /
headless 方法名，改名是**对外 API 破坏 + 全仓联动**，属于独立的重命名变更而非退场清理
（审计自己也写明"不可做只改名式重构"）；二是"别混"的口径已经写进
[`sessionstore/README.md`](../../sessionstore/README.md) 的 legacy 兼容政策（teammate 执行链的
`ToolsPolicy`/`PermissionGroups`/`Plugins` 是活动配置，不属于 legacy 数据）。

### A08 — 提示词/插件读面：保留为 legacy 可选能力

三层保留、一个共同移除条件（**下一次对外 API 版本变更**）：

1. **读面默认回退是活动的**：`rolePromptFor` 未注入时返回空串，ADVISOR / worker 用内置
   提示词——这段不能删（删了会改 ADVISOR 输出契约）。
2. **读面注入源已退场**：`SetRolePromptProvider` / `SetRolePluginsProvider` 的来源是团队
   角色注册表，随 J-1a 删除；组合根不再调用（`main.go` 的 J-1a 说明），运行时行为与
   "注册表里没登记"逐字相同（未注入 = 空集/内置回退）。
3. **优化端口是可选对外契约**：`contract.RolePromptPort` / `OptimizeRolePrompt` 的入口
   （员工入职面板）已退场，静态索引无应用层调用者；保留的理由是它可能是宿主/第三方
   契约，删除 = 版本化 API 破坏。

**不可与活动插件透传混为一谈**：`team_plan members[].plugins`（`WorkerRequest.Plugins` →
`roleRoundSpec.Plugins`）是**独立的、活动的**能力来源，不经 `SetRolePluginsProvider`，不随
本批处理。

### A09 — 保留只读治理 snapshot，控制面按未知方法拒绝

- `goal.gov_snapshot` 返回当前视图会话的 `GoalGovernanceView` 投影，**不推进轮次、不改事实**；
  它是诊断 / GUI 观察面，不是席位控制面。
- `goal.gov_next` / `goal.gov_break` 已随 2026-10-03 阶段三 W3 的席位轮转退场，落到
  `dispatchGoal` 的 `default` 分支按未知方法拒绝；`gui/headless_goal_test.go` 的"被拒断言"
  作为 no-regression guard 保留，待 headless API 版本治理时统一处理。
- 旧文档纠偏：`docs/arch/teamwork-leader-worker-architecture.md` §7 加状态更新（该节是 D4
  设计意图，不是当前运行事实；座位环已退场，现由提示词驱动的 leader 派活 + 终态 goal gate
  替代）。§9 状态矩阵与 M1/M2 行属 A15 范围，本节显式指向 A15。

### A10 — 两种 "stage" 的冷读与区分

- **计划阶段模型退场**：`TeamworkPlan` 结构体没有 `Stages` 字段，旧 `plan.json` 里的
  `stages` / `order_roles` / 里程碑 `after` 都是未知字段，读入即忽略、不再作为顺序事实；
  新增 `TestTeamworkPlanColdReadsLegacyStagesPayload` 冷读一份手写的旧 shape head，断言
  （a）不报错，（b）里程碑屏障照旧解出，（c）**只读不改盘**（旧 `stages` 逐字留在盘上），
  （d）产品自己的整份替换写路径按当前 schema 接受该计划且不复活 stages。
- **历史 `retire` 审计只读**：`TeamworkEventRetire` 常量与解码保留的唯一理由是"老
  `events.jsonl` 还能读回"；新增 `TestTeamworkAuditColdReadsLegacyRetireRow` 冷读一行手写的
  旧 `retire` 行，并断言追加是**只追加不重写**。
- **执行进度阶段打点存活**：`NodeSessionRecord.StagesJSON` 由 `seelebridge/workunit` 的
  `EncodeStages` / `DecodeStages` 编解码（`ProgressOf` 折算进度），是 subagent / teammate
  的运行打点与中断恢复读数，**不属本条退场范围**。

> 与 AGENTS.md §8 的关系：本批**没有删除任何重复实现**，因此没有"原位置 → 唯一实现位置"
> 清单；A10 的两个用例是**冷读 fixture / 兼容 guard**，不是跨实现一致性用例。
> `agentteam.RoleSessionID` 与 `teamwork.DefaultRoleSessionID` 的"看似重复"裁决仍按 A12
> （不同空值/trim 语义，不合并）。

## 验证

- `go build ./...`、`go build -tags "gui,desktop,production" ./...`、`go vet ./...` 绿。
- `go test ./sessionstore -run 'TestTeamworkPlanColdReadsLegacyStagesPayload|TestTeamworkAuditColdReadsLegacyRetireRow' -count=1 -v` 两条新增用例 PASS。
- `go test ./gui -run TestHeadlessGoalChainReplication -count=1` 绿。
- `go test ./seelebridge/workunit ./sessionstore ./seelebridge/teamwork -count=1` 绿。
- `gofmt -l` 对本次改动文件为零命中；`git diff --check` 干净。

## 未执行 / 后续

- A05 的字段清除（`OrderPolicy*` / `TeamSchedule`）与 A06 的写面停用仍需独立版本化迁移
  （旧会话样例 + 冷恢复路径 + 回滚方案），本批不碰。
- A08 的端口/实现移除、`Dependencies.RolePrompt` 字段移除须与对外 API 版本变更同批。
- A11/A15 的历史叙述整理与 `KindSeat` 状态矩阵仍未执行；本批只在 §7 加状态更新并显式指向 A15。
