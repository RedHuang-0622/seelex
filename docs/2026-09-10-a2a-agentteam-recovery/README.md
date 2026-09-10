# A2A AgentTeam 管理与 subagent 恢复（2026-09-10 工作包）

本目录是一次性工作包，承载"角色管理参考酒馆、AgentTeam 工厂化、subagent 按
tool calling 恢复"这次用户裁决后的两条并行实现流。

权威设计：

- [`docs/arch/a2a-agent-team-factory.md`](../../arch/a2a-agent-team-factory.md)（本轮更新，长期边界）
- [`docs/2026-09-08-session-storage-architecture/my_design.md`](../2026-09-08-session-storage-architecture/my_design.md)（v8.3 存储口径）
- [`docs/2026-09-10-backend-cache-goal-session/app-layer-wiring-prompt.md`](../2026-09-10-backend-cache-goal-session/app-layer-wiring-prompt.md)（R1–R4 应用层接线口径）

## 用户裁决（本轮基线）

1. **subagent 是"臭外包"**：一份劳务派遣式的能力调用，等同一次 tool call，
   永远不是 AgentTeam 成员，不进成员表/`order_roles`/floor/role draft/sequencer。
2. **AgentTeam 是 A2A 抽象工厂的实例**：`goal` 的 TL 只是第一个实例；TL 的模式
   必须泛化到后续各类 A2A agentteam（review-team、research-team、自定义），
   最终把抽象工厂的装配开放到前端界面与后端角色管理设置。
3. **subagent 恢复 = 工具调用中断续跑**：终止前没做完的部分按 interrupted
   tool-chain 语义恢复；这份恢复程序本身是可泛化模板（见设计 §5.1）。
4. **角色只靠 metadata 表达**：provider role 只有标准集；只有用户输入是 `user`，
   task/goal/plan/subagent 的 active/状态材料与恢复说明一律 `system`。
5. **运行中的 active 不写进历史**：subagent 的 active 只活跃在表格；冷恢复发现
   active 且未完成时，重建该 subagent 现场、`system` 注入之前的上下文与恢复
   说明，然后重新跑（不是只留占位符）。

## 两条并行流

| 流 | 负责 agent | 规格 | 交付 |
|---|---|---|---|
| A：subagent 中断后恢复续跑 | subagent 流 | [`subagent-resume.md`](./subagent-resume.md) | 可泛化的恢复模板 + subagent 冷恢复续跑 + 表格 active 语义 |
| B：AgentTeam 管理 | team 流 | [`agentteam-management.md`](./agentteam-management.md) | AgentTeam 工厂/注册表泛化 goal TL + 角色管理端口/DTO + 前端装配面契约 |

两条流共享：

- 权威边界只认 [`a2a-agent-team-factory.md`](../../arch/a2a-agent-team-factory.md)；
  与它冲突时先改设计稿并通知对方，不允许各写一套。
- `role_name` 是 metadata；provider role 只能 `system/user/assistant/tool`。
- 存储/端口纪律：`application/` 只经 `application/contract` 端口与 DTO，
  `gui/`、`tui/` 只消费 Application API/Event。
- 纪律：先读根 `MEMORY.md` 与 `AGENTS.md`；不 commit/push；改接口同步 README 与测试；
  `gofmt`、`go vet`、相关包 `go test` 必须干净。

## 并行冲突纪律

- `gui/headless.go`：B 拥有 `role.*`/`team.*` 段，A 拥有 `subagent.*` 段；
  只改自己负责的段，不做跨段重排；需要动对方段时先给对方发消息。
- `application/contract/ports.go`：按类型就近插入，不做整文件重排。
- `sessionstore/`：B 拥有 `role_session*.go`/`lifecycle.go` 的角色字段；
  A 拥有 `node_session_store.go`/`fork_store.go` 的子代理记录字段。
- 同一文件不可避免要同时改时，先发消息约定切分，再动手。

## 状态

- 2026-09-10：工作包建立，设计稿 §2.1/§5/§5.1/§9 已按裁决同步；两条流的实现
  由各自 agent 推进，完成后在本文件追加"实现状态"。
- 2026-09-10：**A 流已实现并真实验收**。新增 `application/core/resume` 七步恢复
  模板、`seelebridge/runtime_subagent_resume.go` subagent 适配器、`subagent.list/
  fork/recover/resume` headless RPC；`TestSubagentResume*` 与
  `TestRealAPISubagentResumeLiveProbe` 通过（真实中断 → 冷启动 → system 注入 →
  同键续跑 → 收敛；pprof 无死锁，race 目标无 DATA RACE）。诊断用
  `SEELEX_SUBAGENT_HOLD_MS` 仅由冒烟测试设置，生产默认 0。
- 2026-09-10：**B 流已实现并真实验收**。`application/core/agentteam` 的
  RoleSpec/TeamSpec/Factory/Registry、`session/team/roles.json`、
  `team.*` RPC 与 `goal-a2a/review-team/research-team` preset 落地；
  `TestRealAPIAgentTeamLiveProbe` 通过（同工厂装配两个团队、subagent 不进成员表、
  定时 agent 不入 order_roles、pprof/race 干净）。
- 2026-09-10：**provider role 口径收口**。真实端点实验确认历史中部 `system` 可用；
  task/goal/plan/subagent internal 状态材料 provider role 统一为 `system`，真实用户
  输入保持 `user`；激活技能正文保留 internal user 轮次以维持稳定前缀缓存。
- 2026-09-10（工作流 B / AgentTeam 管理）：**P0 部分 + P1 已落**
  - 已落：`application/core/agentteam`（`TeamSpec`/`RoleSpec`、`Normalize` 顺序校验、
    `Factory.Materialize` 幂等装配、`Registry` CRUD、preset = `goal-a2a`/`review-team`/
    `research-team`）；`sessionstore/team_registry.go`（`session/team/roles.json` 整份替换型 +
    `EnsureRoleSessionWorkspace` 幂等 + `ReadLifecycleOrderWorkspace`）；
    `application/contract/dto/agentteam.go`（纯 DTO）；`internal/adapters/agentteam_ports.go`；
    `gui/headless_team.go`（`team.presets/materialize/view/put_role/delete_role/set_order`）。
  - AT8 证据：同一工厂/同一 RPC 面实例化 goal 之外的 `review-team`
    （`TestSecondTeamThroughSameFactory` + 真实 API 同进程装配两个团队）。
  - 真实 API 冒烟：`gui/team_live_probe_test.go`（env 门控）在普通与 `-race` 目标上 PASS，
    报告 `tmp/headless-smoke/reports/team-live-*.json`，`race_clean=true`、
    `real_turn{user_rows:1,assistant_rows:1}`、`design_notice=[]`；pprof 无本链路热点。
  - **未落**：P0 的「把 `application/core/goal` 的 `PeerState`/`Frame`/`Round`/`Mailbox`
    等 peer-session 原语提取到通用位置、goal 改为纯适配器」仍是待办（本轮的工厂是
    在存储/角色管理面泛化，未搬迁 goal 运行时原语）；P2 provider role 审计另行推进。
- 2026-09-10（P2 provider role 审计）：**已落**——
  [`provider-role-audit.md`](./provider-role-audit.md) 记录落点→实际 role→结论；
  回归测试 `application/core/task_context/provider_role_audit_test.go` 钉住
  「只有用户输入是 user」「编排态材料不得伪装成 assistant」「internal 材料不开启
  新 round」。
  **待用户裁决**：设计稿 §2.1 表写编排态材料 provider role = `system`，代码现状是
  internal 材料以 `Role="user"` + `Kind=internal` + `wire_material` 进入历史
  （逻辑归属 `role_name=system`）。改与不改取决于目标端点是否接受历史中部的
  `system` 消息，审计文档 §3 给出实验建议，未裁决前不改 provider role。
