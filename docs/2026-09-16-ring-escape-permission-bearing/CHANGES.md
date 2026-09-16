# 环逃生收口 / 座位去硬编码 / 权限拦截承重面 / team 会话优先（2026-09-16）

四条诉求逐条落地：**逃生即 goal 收口 + 归档 tl 历史**、**座位按角色 kind 派生**、
**权限拦截真正成立（按构造 + 按归属）**、**team 先改会话设置再同步到整体**。
文末是验证证据与已知风险（含一条**与本轮无关的既有测试夹具竞态**）。

---

## 1. 环逃生 → goal 直接 turn off + 归档 tl 历史

**修复前**：逃生只在 application 层调 `governor.Break(reason)`，goal 仍停在 `active`——
治理循环停了，goal 却还在等一个永远不会来的 ADVISOR 回合（面板恒 0 轮）；而任何
"直接终态化"的路径（headless 的 `goal_finish/goal_abort`、`Controller.Finish/Abort`）
**不 reap b 的 peer**，于是同一个会话下一个 goal 会带着上一轮 b 的锚点/帧继续跑。

**改动**
- 新增 `application/core/goal/escape.go`：`Supervisor.AbortOnEscape(ctx, reason)`
  三部曲——① 归档 b 会话历史（进程内的锚点/帧/回合摘要渲染成原文）② 把 goal 落成
  `aborted`（`escape:<reason>`，**不过 gate**：b 正是被判"不值得再问"的一侧，再去问它等于自锁）
  ③ reap peer（`PeerReaped` = 下一个 goal 重建 ADVISOR，清空前一轮记忆）。
  幂等：无 active goal 时 no-op。
- 归档写面用**类型断言**接进既有记录器（`TLHistoryArchiver` 由 `TLRoundRecorder` 可选实现），
  不新增 dep、不逼所有记录器跟上；`goalTLRecorder.ArchiveTLHistory` 把归档行落进
  tl 角色历史（`kind=goal_archive`，同一条 draft → sequencer 通道）。
- `goalCoordinator.AdvanceAfterChat`：环判定逃生后，先 `gov.Break`，再 `sup.AbortOnEscape`。

**证据**（域 + 接线各一组，含反例面）
- `application/core/goal/escape_test.go`：收口状态/归档次数与内容/reap 原因/幂等/无归档面/无 goal；
  **`TestEscapeDoesNotLeakTLHistoryIntoNextGoal`** 钉住"下一个 goal 拿到全新 peer"。
- `application/core/goal_ring_escape_test.go`：环逃生 → goal 栈空 + history+1 + tl 角色 draft 出现
  `goal_archive` 行（带逃生原因）；重复逃生幂等。

## 2. 座位去硬编码（"改名不再丢 ADVISOR 座位"）

**修复前**：`newGovernor` 用字面量比较（`roleName == "main"` / `"tl"`）决定谁有座位。
TL 角色一旦改名（用户改名，或自定义预设用了别的名字），**ADVISOR 座位直接消失**——
治理循环只剩 EXEC 一座，ADVISOR 被噤声且不报错，只是"永远不评估"。

**改动**
- `goalCoordinatorDeps` 增 `RoleSeatsFor`；`newGoalCoordinator` 装配 `service.teamRoleSeatsFor`
  （顺序取 `lifecycle.order_roles`＝唯一顺序事实；kind 取成员表 `role_kind`）。
- `seatsFromSpecs`：**按 kind 派生**（main→EXEC、techlead→ADVISOR；agent/timer/user 目前无
  治理座位实现，在环里发言但不推进治理循环——诚实标注，不假装有执行面）。
- `seatsFromOrder` 保留为退化路径（读不到注册表的老宿主/桩），不为改名兜底。

**证据**：`application/core/goal_seats_test.go`（表格用例：内置 / 改名 / 改名+倒序 /
无 techlead / agent 角色；协调器级；退化路径；装配层座位来源顺序与 order_roles 一致）。

## 3. 权限拦截：从"读面先接上"变成"真的拦得住"

**修复前的两处漏**：
1. **锚错了**：main.go 的解析器锚在"当前视图会话"（`app.Snapshot().Session.ID`）——**后台会话**
   的角色会话一律查不到 → 落回 root = 不拦。权限面的正确性不该取决于用户此刻看着哪个会话。
2. **没有承重面**：除 tl 的 ADVISOR 回合外没有角色回合执行体，且它不持工具（代码注释自述），
   所以拦截逻辑没有活体调用可拦。

**改动**
- 新增 `application/core/agentteam_role_index.go`：角色会话 → 归属(主会话, 角色名, 权责)
  反向索引，**登记点=注册表读取唯一入口** `agentTeamRawView`；对外 `RoleSessionToolsPolicy`
  （+`RoleSessionOwner` 供诊断）。
- **歧义口径 fail-closed**：角色会话号 `teamID-roleName` 不含主会话身份，跨会话重号时取**最严**
  口径（readonly < readwrite < 其他），而不是落回 root。
- main.go 解析器改为 `app.RoleSessionToolsPolicy(roleSessionID)`（去掉"当前视图会话"锚）。
- seelebridge 新增 `WithEmployeeSubjectClass(ctx, toolsPolicy)`：角色回合执行体按构造把主体类
  放进 ctx（`classFor` 优先读 ctx）——即使索引冷启动没查到，角色回合照样按员工口径拦。

**证据**
- `application/core/agentteam_role_index_test.go`：按角色会话号解析归属（与"当前看着哪会话"无关）、
  未登记不猜权责、**重号取最严**、同归属更新即时生效、未配置注册表不污染索引。
- `seelebridge/tools/permission_employee_binding_test.go`：**无解析器**时 ctx 主体类就足以拦下
  readonly 员工的写调用（工具不执行 + 顶执行选择页面）；root 不被员工口径收窄；
  两条路（按构造/按反查）结论一致。

**仍然缺的一环（必须说清）**：今天的运行时里**没有角色回合执行体**——没有任何员工会带着工具
跑一轮，所以"拦截"目前是**已就位且被测试钉住的判定链**，而不是每次会话都真的发生的拒停事件。
本轮的仓库里也没新造它（这是独立的一件事）。它的待接位置是：角色回合起手处调
`WithEmployeeSubjectClass`，或在帧/回合执行体内由 `sessionID` 走索引解析。

## 4. team：先改会话设置，再通过同步改整体配置

**结论：这条在 application 层已经成立**，本轮补的是**可验证的契约**，不是新功能：
- 会话内 `AgentTeamPutRole` / `AgentTeamSetOrder` / `AgentTeamInstantiateRole` 全部只写会话副本
  （registry + lifecycle head），不碰母本；
- `AgentTeamPublishToGlobal`（「确认普及搭配到全局」）才把会话 {员工, 顺序} 回写母本
  （员工库 → 默认顺序 → 团队库条目，顺序有依赖：先员工后顺序），既有
  `TestAgentTeamGlobalPublish` 已覆盖后半句。

**证据**：新增 `application/core/agentteam_session_first_test.go`
（会话内改角色口径/改序/入职后，母本写次数不变、母本仍为空，而会话副本已更新）。

---

## 验证证据（本轮全量）

| 类别 | 命令 | 结果 |
|------|------|------|
| 编译 | `go build ./...` | 通过 |
| 静态 | `go vet ./...` | 无输出（干净） |
| 格式 | `gofmt -l application seelebridge main.go gui` | 无输出（`tmp/goal-tl-live-smoke/smoke_test.go` 为既有文件，未动） |
| 全量测试 | `go test ./... -count=1` | **全部 ok**（60+ 包） |
| 竞态 | `go test -race ./application/core/goal/ ./seelebridge/tools/` | ok |
| 定向 | 新增 5 个测试文件、约 20 个用例 | 先跑红/再转绿（含反例与幂等面） |

## 已知风险与未做项（不含糊）

1. **`-race` 在 `application/core` 报一处既有的测试夹具竞态**（不是本轮改动引入）：
   `fakeRuntime.BindProjectRoot` 直接写无锁字段 `projectRoot`，而测试体内直接读它；
   读方/写方都未经本轮改动的代码。复现：`go test -race ./application/core/ -run TestBackgroundSessionKeepsOwnProjectRoot -count=20`。
   建议修法：给 `fakeRuntime` 加 `projectRootMu` + `ProjectRootValue()`，并把 5 个测试文件里的
   12 处直接读改成 getter（纯测试夹具改动，本轮未做，避免跨文件无关 churn）。
2. **角色回合执行面**：**已由后续一轮交付**（2026-09-16 追加，见 `FOLLOWUP-role-turn-body.md`）。
   本轮的判定链（按构造 + 按归属）当时没有活体调用可拦；现在 `seelebridge.RunRoleTurn`
   在开角色会话时分配 `emp_<角色名>` 主体、回合起手按构造带主体，agent 座位真的会带工具跑一轮。
   §3 末的"仍然缺的一环"因此不再成立（本轮的历史描述保留，不改写记录）。
3. **角色会话号不含主会话身份**：跨会话重号靠"取最严"兜住，要从根上消除需把主会话身份编进
   角色会话号（存储迁移），本轮未做。
4. **ADVISOR 无工具、无法验证**：详见 `A2A-VALUE-REVIEW.md` §2.5/§3.3——这是"评鉴"部分给出的
   最高性价比改进项，不是本轮交付。
5. 团队环的**座位顺序**：仍保留"EXEC 先于 ADVISOR"的相对次序（`orderSeats`），改的是
   **座位存在性**的派生依据（kind）；链序里的 EXEC/ADVISOR 相对次序这一产品语义未动。
