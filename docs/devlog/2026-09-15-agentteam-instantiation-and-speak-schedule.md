# 2026-09-15 Agent Team 员工一步实例化 + 发言调度运行态（+ 治理轮次上限）

> 日期: 2026-09-15 | 范围: `application/core/agentteam/factory.go`、
> `application/core/agentteam/runtime.go`、`application/core/agentteam_runtime.go`、
> `application/core/agentteam_service.go`、`application/contract/dto/agentteam.go`、
> `application/contract/dto/projection.go`、`gui/frontend/dist/agent-team-view.js`
> | commit: `fb48f08`

## 一、问题：能配角色，不能“招人”；没有可观测的“下一个谁发言”

Agent Team（goal-a2a：EXEC(a) + ADVISOR(b)）上线后只有“配角色 / 按 preset
装配”两条路，缺口有两处：

1. **不能一步“招人”**：`team.materialize` 按整份 `TeamSpec` 装配；临时想加一个
   `reviewer`，没有“建一个角色会话 + 落配置 + 决定是否进顺序”的原子入口。
2. **发言顺序不可观测**：链表顺序只存在于装配时的 `TeamSpec` 里，运行态没有
   投影——“下一个该谁发言”“轮到第几轮/上限多少”“user 席位在不在”在数据面
   上看不到，前端只能自己猜。

## 二、改法

### 1. 一步实例化（员工入职）

新增 `team.instantiate_role` RPC 与 `Service`/`Bridge.AgentTeamInstantiateRole`
（`dto.RoleInstantiation` 回报建会话结果 + 执行者绑定）。物化路径抽到
`agentteam/factory.go`：

- **幂等建角色会话**：`(team_id, role_name)` 派生同一 `role_session_id`，重复
  调用不重复建会话；
- **落配置**：把角色写进团队注册表；
- **`join_policy` 决定是否进顺序**：`immediate` 直接入环，`deferred`/`timer`
  延后（见测试 `TestInstantiateRoleRespectsJoinPolicyAndTimer`）；
- **内置角色拒绝**：`user`/`main` 与空名字/空会话在入口被拒
  （`TestInstantiateRoleRejectsBuiltinAndBadInput`）。

### 2. 发言调度运行态（`TeamView.schedule`）

`application/core/agentteam/runtime.go` 把“链表顺序”投影成运行态：

- **下一个该发言的角色** + **轮次 / 上限**；
- **user 席位**：`queued` / `member` / `absent`；
- **逃生状态**：`round_limit` / `no_progress` / `no_executor` / `empty_ring` /
  `external_break`。

`application/core/agentteam_runtime.go` 按**主会话**持有该运行态
（`teamRuntimeStore` → `serviceState.teamRuntimes`）：装配路径与读路径都建环，
chat 起点、队列编辑、插话统一同步 user 席位（`NoteTeamUserQueued` /
`noteTeamUserSeat`），`teamScheduleFor` 只读投影。前端只渲染后端事实，不下发也
不缓存顺序。

### 3. 治理轮次上限（`GoalGovernanceView.RoundLimit`）

`dto.GoalGovernanceView.RoundLimit`（`0` = 显式不设上限）暴露给前端
（“轮次 n/limit”），goal 协调器按本会话上限收束，团队运行态的轮次与治理口径
同源。

### 4. 前端：Agent Team 面板拆成两栏条目化表格

`gui/frontend/dist/agent-team-view.js` 把面板拆成：

- **员工栏**：谁在岗 / 类型 / 独立会话 / 入职时机 / 工具策略 + 一步实例化表单；
- **Team 栏**：装配形态 / 顺序策略 / 工作顺序 / 发言调度 / 定时 agent。

## 三、证据（测试）

```text
go test ./application/core/agentteam -count=1 \
  -run "InstantiateRole|Schedule|TeamView|Runtime"      # 物化 / 调度投影
go test ./application/core -run "Team|Goal" -count=1     # 服务门面与 goal 收束
go test ./gui -run "Team" -count=1                       # bridge/headless team.* 转发
node --test gui/frontend/dist/agent-team-view.test.mjs   # 两栏渲染 + 状态投影
```

关键用例：`application/core/agentteam/instantiate_role_test.go`、`runtime_test.go`、
`team_view_test.go`、`scheduler_wiring_test.go`、`application/core/agentteam_floor_wiring_test.go`、
`gui/bridge_team_test.go`、`gui/headless_team_test.go`。

真实 API 面：`gui/team_workcontent_live_probe_test.go`（`SMOKE_TEAM_WORK_LIVE=1`）
与 `gui/team_work_computer_use_live_probe_test.go`（`SMOKE_TEAM_WORK_COMPUTER_LIVE=1`）
为 opt-in 真机探针；证据面见
[`2026-09-15-team-work-computer-use.md`](2026-09-15-team-work-computer-use.md)。

## 四、边界与未做

- “像素未贯通”：ADVISOR 拿到的是媒体句柄 + 元数据（宽高/前台窗口标题），不是
  像素；给 TL 回合挂图或让角色带独立工具循环都尚未接线（同上 devlog 第四节）。
- 发言调度是**投影**，不是调度执行器：真正“让某人发言”仍由 goal-a2a 的
  order_policy 驱动；`TeamSchedule` 只读事实。
