# 删除内置团队形态目录（preset catalog）：形态从代码里消失，团队只从数据来（2026-10-01）

> **触发**：用户在看 Agent Team 面板时指出——「小字部分比如 goal-a2a 啊、review-team 啊、
> research-team 这些可以删掉了，对应的硬编码也可以删掉了」，并且追问「发言顺序？现在还有
> 这个设定还需要编排这种东西吗？同样的还有顺序策略和团队形态」。
> 本文记 **Confirmed**（有代码/文档/用例证据）的事实、改法与回归；**Hypothesis** 单独标明。
> 本文覆盖 **Go/契约/headless/测试/文档** 那一半；面板那半（撤掉 chip、形态输入、顺序
> 策略只读框、人工调序）另见 [`2026-10-01-team-panel-no-shapes.md`](./2026-10-01-team-panel-no-shapes.md)。

---

## 1. 现场：那行「小字」是什么（Confirmed）

面板上「团队库」块底下那行小字 = `内置 goal-a2a review-team research-team`，来自
`Bridge.AgentTeamPresets()` → `agentteam.Presets()` → 代码里的三支 TeamSpec
（`application/core/agentteam/presets.go`）：

| 形态 | 成员 | 它存在的理由（原注释自述） |
|---|---|---|
| `goal-a2a` | user / main / tl(techlead) | goal 的 TL 编排是"第一个实例"；`tl` 声明 `JoinPolicy=on_goal_create` |
| `review-team` | user / main / reviewer | "第二个实例（**AT8 证据**）：同一工厂、同一 sequencer" |
| `research-team` | user / main / researcher + digest(timer) | "**演示**定时 agent 不入 `order_roles`" |

后两支的自述直接说明它们是**脚手架/证据**，不是产品能力；`goal-a2a` 则被写成"goal 上线
的默认团队"。三者加上配套入口构成一处"代码里的模板目录"：

- `agentteam.Preset(teamKind)` / `Presets()`；
- `Service.AgentTeamPresets()` + `Bridge.AgentTeamPresets()`（面板的 chip 数据源）；
- `Service.MaterializeAgentTeamPreset()` + `Bridge.AgentTeamMaterialize()`（chip 点击）；
- headless `team.presets`，且 `team.materialize` 支持「只给 `team_kind`」；
- `@` 召唤的内置形态分支（`teamPresetTargets()` / `teamSummonTarget.Preset`）；
- 输入建议面的形态候选（`teamPresetSuggestions()`）+ 跨域迁移提示（`isPresetTeam()`）。

## 2. 为什么删（Confirmed）

1. **模板说不了真话**：一支团队有谁、什么顺序，本来就是**会话/团队库的数据**。面板成员表
   早就能加工人，可库里那行还写着"固定循环 user → main ↔ TL"（上一轮已改文案，本轮直接删掉
   这类说法赖以存在的形态概念）。
2. **`team_kind` 不是行为开关**：全仓检索，没有任何按 `team_kind == "goal-a2a"` 分支的运行时
   逻辑（`Preset()` 的 switch 只服务"按形态名解析"，而解析只服务上面那批入口）。它一直是
   展示别名——既然如此，"选一支形态"这个动作就没有语义。
3. **它带来一个事故：goal 上线会砸掉会话已有的团队。** `GoalBeginFor` 调
   `ensureGoalAgentTeam` → 按 `goal-a2a` 模板装配 → `factory.Materialize` = **注册表整份替换**
   （`WriteTeamRegistry`）+ **lifecycle 顺序整份替换**（`SetLifecycleOrder`）。也就是说：用户
   手工加了 worker/reviewer，只要再 `goal.begin` 一次，团队就被冲回模板那三个人。
   这不是"自动化的边界问题"，是数据被覆盖。
4. **goal 的评估链不依赖它**：没有装配团队时 `goalCoordinator.seatsFor` 返回空，`newGovernor`
   走 `goaldomain.NewTurnGovernorForDSA2A`（EXEC + **ADVISOR(supervisor)**）——TL 裁决照常在。
   团队席位是"在编员工各自的回合"这一层的增量，属于"会话里谁在编"这件产品事实。

## 3. 改法（本轮，Go 侧）

| 面 | 删除/改动 |
|---|---|
| `application/core/agentteam/presets.go` | **整文件删除**（三支形态、`Preset`、`Presets`、`ErrUnknownPreset`） |
| `agentteam/spec.go` | 包注释改为"没有内置形态目录"；`Normalize` 不再有"缺省形态"回退：`team_id`/`team_kind` 必须至少给一个（互为镜像），**都给不出就是调用方错误** |
| `agentteam/factory.go` | `teamKindOf` 空值回退改为 `team_id`（不再伪装成某个形态）；入职路径的 `team_kind` 缺省 = 该会话的 `team_id` |
| `agentteam/library.go` | 库条目 `team_kind` 缺省 = `team_id`（沿用既有 `firstNonEmpty`）；`EntryFromSpec` 的 `origin` 缺省由 `"preset"` 改为 `"custom"` |
| `agentteam/registry.go` | 新增 `registryIdentity()`：`View`/`SetOrder` 两条路径用 `Normalize` 校验时补身份（未装配团队就入职的会话没有 `team_id`），**只用于校验、不写盘** |
| `agentteam.resolveOrderRoles` | 顺带修一处潜伏 bug：`Roles` 里显式写了 `user`/`main` 且未给 `OrderRoles` 时，推导出的链表会**重复**它们（旧形态都显式给了 `OrderRoles`，所以一直没暴露） |
| `dto/agentteam.go` | 删 `TeamKindGoalA2A/Review/Research`、`DefaultTeamKind`；新增 `DefaultTeamID = "goal-a2a"`（**身份回退字面量**，不是形态：`RoleSessionID` 的派生分量，改它 = 既有角色会话号全体分裂） |
| `application/core/goal_service.go` | 删 `ensureGoalAgentTeam` 与调用；`GoalBeginFor` 的 doc 写明"不再自动装配"的两条理由（会砸掉团队 + 评估链不依赖它） |
| `application/core/agentteam_service.go` | 删 `AgentTeamPresets` / `MaterializeAgentTeamPreset` |
| `application/core/input_team.go` | `@` 只认团队库条目：删 `teamPresetTargets`、`teamSummonTarget.Preset`；`teamSummonHelp(sessionID)` 改为**读团队库列出可用团队**（空库时给"先去面板新建一支"的指引） |
| `application/core/completion.go` | 删 `teamPresetSuggestions` / `isPresetTeam` / `teamSpecSummary`；`@` 不再弹补全面板（候选在团队库，而 `Suggestions` 跑在 TUI `View()` 与 GUI 每次输入事件上——为它每次按键读盘不划算）；迁移提示少一条"召唤团队用 @xxx" |
| `gui/bridge.go` | 删 `AgentTeamPresets` / `AgentTeamMaterialize`（接口 + 实现 + 编译期断言自然收紧） |
| `gui/headless_team.go` | 删 `team.presets`；`team.materialize` **只接 `spec`**（`TeamKind` 字段一并删除） |
| `config/seelex.yaml` | 未动（团队相关配置只有 `limits.team.max_teammates`） |

### 3.1 观感与行为变化（用户可见）

- 面板「团队库」底下的 `内置 …` 小字消失；团队只能来自团队库条目（**另见面板那半的 devlog**）。
- `@` 不再能召唤"内置形态名"：`@goal-a2a` 只有在**团队库里有这支团队**时才装配（老用户
  的库里通常已经有，因为曾经点过「装配」/存过当前会话）；`@` 空参会把库里的名字列出来。
- **goal 上线不再自动拉起 `tl` 团队**。要 TL 角色会话时显式装配一支含 `tl`(techlead) 的团队；
  不装配也有 ADVISOR 裁决（治理循环自带的 supervisor 座位）。
- `@` 输入时不再弹团队候选（库空了会在召唤面明说下一步）。

### 3.2 没做（明确边界）

- **`team_kind` / `order_policy` / `order_roles` 的字段本体没删**：它们仍在 DTO 与落盘里，
  但已不是可配置项；退场条件见 M4 清单 #5（blocked，要先把顺序事实切到 team plan）。
- **`DefaultTeamID` 的字面量仍是 `"goal-a2a"`**：它是 `RoleSessionID(主会话, team_id, role)`
  的派生分量，历史会话用它算过号；换名会让同名员工被当成新员工（会话子树、权责反查、项目
  根绑定全部错位）。要改走一次显式迁移。
- 面板那半（chip/输入框/只读框/人工调序）在本轮的下一笔提交里，见面板 devlog。

## 4. 回归证据（Confirmed）

| 命令 | 结果 |
|---|---|
| `go build ./...` / `go build -tags "gui,desktop,production" ./...` | exit 0 / exit 0 |
| `go vet ./application/... ./gui/...` | exit 0 |
| `go test ./application/core/... ./gui/... ./tui/... -count=1` | 全绿（`application/core`、`agentteam`、`gui`、`tui` 分别 ok） |
| `python scripts/gen_core_readme_index.py` | 索引刷新（`agentteam/README.md`、`README-{agentteam,goal,input,misc,service}.md`）；生成器的"根包分卷覆盖自检"要求新测试文件归属一卷，故 application/core 的团队夹具命名为 `agentteam_fixture_test.go` |

**用例改动（口径：把"依赖内置形态"的用例改成"依赖数据"）**：

1. `agentteam/testspecs_test.go`（**新增**）：三支形态搬进测试作夹具（`testGoalSpec` /
   `testReviewSpec` / `testResearchSpec`）——AT8 证据"同一工厂换一份 TeamSpec 就能装配
   第二支团队"与"产品是否内置模板"无关，所以夹具留在测试侧。
2. `agentteam_test.go`：`TestNormalizeDerivesOrderAndExcludesScheduledRoles` 现在要求
   spec 带 `team_id` 并断言 `team_kind` 缺省 = `team_id`；**新增**
   `TestNormalizeRejectsNamelessSpec`（没有身份的 spec 是调用方错误，不再悄悄补形态名）；
   `TestGoalSpecMaterializeIsIdempotent` / `TestSecondTeamThroughSameFactory` 改用夹具。
3. `team_view_test.go`：`TestSecondAndThirdShapesDeclareNoExecutor`（原 `...Presets...`）
   改用夹具，仍钉住"只登记配置、没接执行者的团队必须在成员表里明说"。
4. `application/core/goal_team_wiring_test.go`：`TestGoalBeginMaterializesGoalAgentTeam`
   → **`TestGoalBeginLeavesSessionTeamAlone`**（会话里先有 tl+worker，`goal.begin` 后
   注册表、顺序、角色会话都必须一字未变）；`TestGoalBeginJoinsTeammatesAtGoalTurn` →
   `TestMaterializeJoinsTeammatesAtItsTurn`（入伙切点断言改打在装配入口上）。
5. `application/core/input_team_test.go`：新增 `summonFixture`（会话里有团队 + 已存进库），
   `TestSubmitTeamSummonsPresetTeam` → `TestSubmitTeamSummonsLibraryTeam`、
   `TestSubmitTeamWithoutNameDescribesPresets` → `TestSubmitTeamWithoutNameDescribesLibrary`
   （含"空库给行动指引"一例）；附言/通告用例改从库条目出发。
6. `application/core/agentteam_ring_user_projection_test.go` / `goal_ring_escape_test.go` /
   `goal_loop_turn_order_test.go` / `goal_directive_session_lock_test.go`：原先靠"goal 自动
   装配"现场成立的用例改为**显式装配** `goalTeamFixture()`（夹具见
   `application/core/agentteam_fixture_test.go`）。
7. `application/core/service_input_test.go`：`@` 的建议面断言改为"整列为空"（并写明理由）。
8. `application/core/employee_permission_assembly_test.go`：
   `TestGoalBeginAssemblyAssignsEmployeePermissions` → `TestMaterializeAssignsEmployeePermissions`
   （装配必须分配员工权限这条断言打在装配入口上）。
9. `gui/bridge_team_test.go` / `gui/headless_team_test.go`：删 `AgentTeamPresets` /
   `team.presets` 断言；`team.materialize` 改按 `spec` 转发；headless 是薄透传层，
   缺 `spec` 的校验留给 application（`Normalize`）。
10. `gui/team_live_probe_test.go`：环境门控的真实 API 冒烟夹具改为测试内联的
    `teamLiveGoalSpec()` / `teamLiveReviewSpec()`。
11. `gui/goal_team_wiring_live_probe_test.go`：**反转语义**——原先断言"只发 goal.begin
    就有 tl 团队"，现在断言"显式装配（含 worker）后发 goal.begin，`team.view` 一字未变"，
    即守"别再让 goal 上线冲掉会话团队"。

## 5. 待办 / 风险

- 面板那半未落地前，`app.js` 仍在调 `AgentTeamPresets`（前端已过时）——见下一笔提交。
- 新增用例覆盖：`TestNormalizeRejectsNamelessSpec`、`TestGoalBeginLeavesSessionTeamAlone`、
  `AgentTeamSaveCurrentTeam`+`@` 召唤的库路径；**未覆盖**："`DefaultTeamID` 改名会造成角色
  会话号分裂"这条只写在注释里（要验需构造历史会话夹具，属 M4 范围）。
- 判为**Hypothesis** 的一条：`@` 不弹补全是否会影响实际使用（团队名通常短且库里只有几支）
  ——没有用户实测数据；若反馈"记不住团队名"，正确做法是给补全面加会话上下文（而不是把
  形态目录加回来）。
