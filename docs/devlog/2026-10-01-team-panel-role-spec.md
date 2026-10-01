# 前端团队面板：成员带整份 RoleSpec（生态位 / 人的档案）+ 形态级门禁·压缩（2026-10-01）

> **口径**：本文记的是「团队面板把 `TeamSpec`/`RoleSpec` 字段丢在保存这一步」这件事的
> **修前事实**（可核对）、**改法**与**回归证据**。触发场景是用户口径的一次 GUI 实操：
> 用计算机操作把 `goal-a2a` **就地覆盖**成 `tl + worker + reviewer`。
> 结论分 **Confirmed**（有代码/用例证据）与 **Hypothesis**（待验证，写明验证方式）。

---

## 1. 现场症状（Confirmed）

面板存出来的条目（`dist/seelex-gui-dev/.seelex/sessions-json/team/library.json`，2026-10-01T01:37:19Z）：

```json
{ "role_name": "tl", "role_kind": "agent", "tools_policy": "readwrite", "system_prompt": "<Tech Lead 提示词>" }
```

条目**没有** `join_policy` / `presence_policy` / `directive_schema`，团队条目也**没有**
`gate_policy` / `compact_policy`。对照内置 preset（`application/core/agentteam/presets.go`
的 `goalA2APreset`）：`tl` 是 `RoleKindTechlead` + `JoinPolicy: on_goal_create` +
`PresencePolicy: online_when_goal_active` + `DirectiveSchema: [verdict_done, verdict_not_done,
escalate_human]`；形态级 `GatePolicy: goal_finish_gate`、`CompactPolicy: main_authoritative`。

**为什么这不是"少存几个字符串"**：座位派生按 `RoleKind` 走 ——
`application/core/goal_coordinator.go` 的 `seatPlan.seats()`：

- `RoleKindTechlead` → `goaldomain.NewAdvisorSeat(...)`（**ADVISOR 评审座位**）；
- `RoleKindAgent` → `newRoleTurnSeat(...)`，且 `plan.Runner == nil` 时 `continue`（**不给座位**）。

所以"条目里 tl 变成 agent"= 装配后的 goal 团队**不再派生 ADVISOR 座位**。现场可见症状：
员工栏里那一行是「ADVISOR tl `[agent]` `[读写]`」（标签说 ADVISOR、生态位说 agent），
团队卡片「暂无发言权」；团队库行「3 人 · **固定循环 user → main ↔ TL**」还把班底写死成三档
（成员表早就可以加工人）。

## 2. 根因：三处都在前端（Confirmed）

| # | 位置 | 修前事实 |
|---|---|---|
| 1 | `gui/frontend/dist/app.js` `agentTeamEntryFromForm` | 每个成员只搬 `role_name` / `role_kind` / `tools_policy` / `system_prompt`（外加可选 `permission_groups`）；`role_kind` 用 `known?.role_kind \|\| "agent"` 兜底 —— 员工库那一份是 agent 时，保存就把**形态定义的 techlead 覆盖掉**。`gate_policy` / `compact_policy` 根本不带 |
| 2 | `gui/frontend/dist/agent-team-view.js` `fillAgentTeamFormFromPreset` | 「用内置形态起手」只填 `团队名 / 团队形态 / 顺序策略 + 成员名`，preset `roles[]` 里的 kind/join/presence/directive/tools **整个丢掉** |
| 3 | 同文件 `normalizeRoleSpecs` / `normalizeTeamLibrary` | 归一化层不认识 `directive_schema` / `order_priority`，也不映射团队条目的 `gate_policy` / `compact_policy` —— 面板**连看都看不到**这两条事实，保存自然清空 |

（后端的 `dto.TeamLibraryEntry` 一直带着这些字段，`NormalizeLibraryEntry` → `NormalizeRole`
也原样保留；不需要动后端与 contract。）

## 3. 改法（只改前端）

**纯函数（`agent-team-view.js`，可脱 DOM 单测）**

- `TEAM_ROLE_SPEC_FIELDS` + `teamRoleSpec`：团队成员能存的 RoleSpec 字段清单；**两种拼法
  都认**（协议载荷 snake_case / 归一化对象 camelCase），出站一律 snake_case，空值与数值 0
  不落盘（"没登记"不伪造成"登记了空"）。
- `teamMemberSpecMap(names, {entry, preset, pool})`：**生态位**（`role_kind` / `join_policy` /
  `presence_policy` / `directive_schema` / `order_priority`）以**团队形态 preset** 为准；
  **人的档案**（提示词 / 权限档 / 逐格权限 / 模型档）以**员工库 / 本会话在编**为准，条目里
  存过的那份次之。形态里没有的角色（自己加的 worker / reviewer）整体走"人的档案"。
- `teamEntryFromMembers(...)`：草稿 → 团队库条目载荷（`order_roles = user → main → 成员`，
  `roles[]` 逐成员带整份规格，带 `gate_policy` / `compact_policy`）。
- `roleKindLabel`：未知 `role_kind` **原样显示**，不折成 agent。

**成员行（草稿活在 DOM 里）**

- `renderTeamMemberList(names, pool, specs)` 每行带 `data-team-member-kind`（可见 chip，
  座位由此派生）与 `data-team-member-spec`（JSON 载荷，跟着行走）。
- `app.js` 的四条重绘路径（添加 / 移除 / 拖拽调序 / 从当前会话填充）都回流规格
  （`teamMemberListSpecs` / `teamMemberSpecsForWrite`）—— 否则"删掉一个不相关的人"就把
  别人的生态位洗掉。

**表单**

- `teamEditorPanel` 加 `data-team-form-gate` / `data-team-form-compact` 隐藏字段（随条目或
  形态带入）+ 一行可见小字（门禁 / 压缩），保存时原样送回后端。
- 成员表说明改成"每行的生态位（role_kind）决定装配后的座位，由团队形态定义"。

**归一化补字段**：`normalizeRoleSpecs` 加 `directiveSchema` / `orderPriority`；
`normalizeTeamLibrary` 加 `gatePolicy` / `compactPolicy`。

**文案**：`POLICY_LABEL.goal_loop` 不再写 "user → main ↔ TL"；团队库行的**真实顺序进 title**
（策略名只说"谁决定顺序"，不承诺班底）。

## 4. 回归证据（Confirmed）

| 命令 | 结果 |
|---|---|
| `node --test gui/frontend/dist/*.test.mjs` | **557 pass / 0 fail**（`agent-team-view` 41 条，含新增 7 条） |
| `node --check gui/frontend/dist/*.js`（逐文件） | 全部通过 |
| `go test ./application ./gui -count=1` | `ok github.com/RedHuang-0622/seelex/gui 3.775s`（含 `bridge_test.go` 对嵌入前端的断言） |

新增用例（`gui/frontend/dist/agent-team-view.test.mjs`）：

1. `teamMemberSpecMap`：生态位取自形态（tl → techlead + `on_goal_create` + verdict 指令集），
   人的档案取自员工库（`tools_policy: readwrite`、逐格权限、提示词不被形态的 readonly 覆盖）；
2. **编辑被降级过的条目**（`role_kind: agent`）时 tl 回到 techlead（自愈路径）；
3. `teamEntryFromMembers` 保留 kind/join/directive + `gate_policy` / `compact_policy`；
4. `teamRoleSpec` 空值 / 空数组 / 0 不算事实；
5. `roleKindLabel` 未知生态位原样显示；
6. 团队编辑器成员行带生态位 + 规格载荷，表单带 gate/compact；
7. 团队库行不再出现「固定循环 user → main ↔ TL」，真实顺序在 title 里。

## 5. 未做 / 待定

- **运行中的 GUI 还是旧前端**（Hypothesis → 可验证）：`gui/assets.go` 是
  `//go:embed all:frontend/dist`，改动要**重新构建 + 重启** `dist/seelex-gui-dev/seelex-gui.exe`
  才生效。本轮没有重建、也没有动用户正在用的那个进程。
- **落盘的那份 `goal-a2a` 条目仍是被降级过的**（tl = agent、无 gate/compact）：
  修好后在面板里打开这支团队**直接「保存团队」**即可自愈（生态位从形态回填），
  或重建 GUI 后回跑一次「内置形态起手 → 加 worker/reviewer → 保存」。
- 会话副本 vs 母本的漂移判定目前只比**名单**（`teamGlobalDrift`），权限差异不算漂移 ——
  本轮未改（属产品口径，需用户拍板）。
