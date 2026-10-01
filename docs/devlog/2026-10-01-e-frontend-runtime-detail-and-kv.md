# E：员工运行详情（刷新键 + 切员工）与员工行 k→v 表格化（2026-10-01）

> **口径**：本文是用例 4/5（前端）的实现记录——`docs/devlog/2026-10-01-teamwork-6case-audit.md`
> §5 列出的四条缺口，逐条给**修前事实**（可核对）、**改法**与**回归证据**。
> 结论分 **Confirmed**（有代码/用例证据）与 **Hypothesis**（待验证，写明验证方式）。

---

## 0. 为什么没有心跳

ClaudeTeamwork 用 `.teamwork/heartbeats/`（文件心跳：谁还活着、各自最后活动时间）判存活。
Seelex 的作业表本身就是存活事实：`running` / `done` / `killed` 是状态机的终态，不需要
"最后心跳时间"再推一层。所以 E 一律走**事件驱动 + 手动刷新键**（用户已定：E3「无心跳」）。

## 1. 修前事实（Confirmed）

| # | 缺口（audit §5 原文） | 修前事实 | 位置 |
|---|---|---|---|
| 1 | 员工运行详情是**一次性拉取**，无刷新键、无事件驱动 | `openRoleSessionDetail` 只 `invoke("AgentTeamRoleSnapshot")` 一次，`renderRoleSessionDetail(snapshot)` 无任何刷新入口；`team.changed` 只作废 Agent Team 面板缓存，不碰这个弹窗 | `gui/frontend/dist/app.js`、`agent-team-view.js` |
| 2 | **没有手动刷新键** | 面板只有"展开即取 + `team.changed` + 切会话重取"三条路径，用户想"现在就要最新"没有按钮 | `app.js` 的 `refreshAgentTeam` |
| 3 | 「单个员工的 **k→v 全字段**」只在懒加载的编辑面板里 | 员工库行内只有 `chip`（类型/权限）回显，全字段要开「修改员工」面板才看得到 | `agent-team-view.js` 的 `employeeLibraryBlock` |
| 4 | **对话视图切到员工**（用例 5） | 成员行的「查看」开一个角色会话弹窗；没有"把视图目标换成员工会话"的动作 | `app.js` 的 `openRoleSessionDetail` |
| 5 | **母本 CRUD 不发事件** | 会话装配面（`MaterializeAgentTeam` 等）发 `team.changed`；员工库 / 团队库 / 默认顺序 / 普及这几条母本写**不发** | `application/core/agentteam_service.go` |

## 2. 改法

**前端（`agent-team-view.js`，纯渲染，可脱 DOM 单测）**

- `employeeFieldRows(role)` → k→v 全字段行（类型 / 入职 / 在席 / 权限档 / 权限格 / 模型 /
  提示词），`employeeFieldTable` 把它渲染成可折叠的 `.team-kv` 表**嵌进员工行**——
  空栏也照列（值退化成"继承 / 未登记"），因为"这一栏为空"本身是一条事实。
- `renderAgentTeam` 顶部加常驻手动刷新键（`.team-panel-toolbar` + `data-team-refresh`）。
- `renderRoleSessionDetail(snapshot, identity)`：多了 `identity`（`roleName` /
  `roleSessionID` / `members`），弹窗顶部加工具栏（`data-role-session-refresh`，键上带身份）
  与「切员工」切换条 `renderRoleSessionSwitcher`（**多名员工时才摆**：只有一个 chip 的条
  是噪音）。

**前端（`app.js`，接线）**

- `roleSessionDetail` 记当前视图目标（身份 + 可切换员工名单），刷新键 / 切员工 /
  `team.changed` 都靠它原样重放，不从 DOM 里猜。
- `renderRoleSessionView` 只重绘内容、不动弹窗开合（"刷新一下弹窗闪一下"）；取数期间
  用户切了人/关了视图，回写前先比对目标是否还是同一份（`roleSessionDetail === detail`）。
- 事件驱动：`team.changed` 里 `if (roleSessionDetail) refreshRoleSessionDetail()`——
  视图开着才重取，关着什么都不做。
- 面板刷新键：`data-team-refresh` → `refreshAgentTeam({ force: true })`。
- 「切员工」：`data-role-session-switch`（键上带 `data-role-session-switch-sid`）→
  `openRoleSessionDetail(role, sid)`，目标换人、弹窗不关。

**后端（`application/core/agentteam_service.go`）**

- 母本 CRUD 成功后在 `AgentTeamSaveTeam` / `AgentTeamDeleteTeam` /
  `AgentTeamSaveEmployee` / `AgentTeamDeleteEmployee` / `AgentTeamSetDefaultOrder` /
  `AgentTeamPublishToGlobal` 六条路径上补 `service.publishTeamChanged(mainSessionID)`
  （会话级 `team.changed`，`revision=0`，与会话装配面同口径）。

## 3. 回归（红 → 绿）

| 断言 | 位置 | 修前 | 修后 |
|---|---|---|---|
| `employeeFieldRows` 摊出 k→v；`renderAgentTeam` 里每个员工行带 `data-team-employee-kv` + `data-team-kv="*"` | `gui/frontend/dist/agent-team-view.test.mjs` | 无（函数不存在，RED） | GREEN |
| 面板常驻 `data-team-refresh`（装配与未装配都有） | 同上 | RED | GREEN |
| 运行详情带 `data-role-session-refresh`（身份在键上） | 同上 | RED | GREEN |
| `renderRoleSessionSwitcher` 多员工摆条、当前位高亮、单员工不摆 | 同上 | RED | GREEN |
| `app.js` 接线：面板刷新键 / 运行详情刷新键 / `team.changed` 热更新 / 切员工 | `gui/frontend/dist/agent-team-refresh.test.mjs` | RED | GREEN |
| 母本六条 CRUD 各发一条会话级 `team.changed`（`revision=0`） | `application/core/input_team_test.go` 的 `TestMasterCRUDsPublishTeamChanged` | 六条全 RED | GREEN |

验证：

```text
node --test gui/frontend/dist/*.test.mjs                                  # 550 pass
go test ./application/core/ -run "TestMasterCRUDsPublishTeamChanged|TestReadPathDoesNotPublishTeamChanged" -count=1
```

## 4. 边界（如实标注）

- **"对话视图切员工"的落点是弹窗内的切换条**，不是把 GUI 主对话区（`#conversation`）
  的目标换成员工会话。原因（Confirmed）：角色会话是**主会话根下的子树**
  （`sessionstore/role_session.go`：`role_<hash>/` 与 `goal_<hash>/`），
  `Router.ListRoleSessions` 只枚举目录名（注释原样写着"仅用于审计/测试"），
  GUI 的主视图会话切换（`ResumeSession`）作用在**顶层会话**上，无法指到子树。
  把主对话区指向角色会话需要新造一条"视图目标 = 嵌套会话"的后端通道，不在本次改动范围。
  本次交付的是 A4 结论里那条**已存在**的会话视图（`renderRoleSessionDetail` 就是该
  员工自己的会话行），把它的目标换成"可切换的员工会话"。
- **刷新是"重取一次"而不是"订阅一条流"**：`AgentTeamRoleSnapshot` 是拉取面；
  草稿同步频率高于 `team.changed` 时，详情视图不会逐帧跟（刷新键兜底）。这是
  "无心跳"口径的直接后果，不是遗漏。
