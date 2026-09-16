# 2026-09-16 员工「继承」口径的越权提权：接到现有审批面板

> 日期: 2026-09-16 | 范围: `seelebridge/tools/{registry_state,permission_inherit_test}.go`、
> `seelebridge/{runtime_tools}.go`、`main.go`、`permission_employee_panel_test.go`
> | 前置：同日 `2026-09-16-agentteam-ring-revive.md`（环复活 + tools_policy 写入侧枚举化）、
> `2026-09-16-ring-escape-permission-bearing/FOLLOWUP-role-turn-body.md`（角色回合执行体）

## 一、口径（用户原话 → 可执行断言）

> **如果是继承的话，在会话的工具中继承全量的工具，通过中间件的权限审查来获取他有没有
> 使用这个工具的权限，并且对于需要越权使用的工具提供人为审批面板（直接用现有的）。**

拆成三条：

1. **继承 → 工具面 = 全量**（不收窄成只读/读写簇）；
2. **用不用得了由中间件逐次判定**（位/组路由 + rules：allow 放行 / ask 问人 / deny 拒绝）；
3. **越权提权用现有审批面板**（`ApprovalBroker` → 视图单格 `Interaction` / 目录
   `awaiting_approval` / 会话快照 `Approvals`），不新建面板、不改 UI。

## 二、盘点：前两条已经成立，第三条断在"归属"上

`ToolsPolicy` 空（继承）在 `ClassForToolsPolicy` 落 `root`，于是：

- `ToolFaceForContext`（工具列表过滤，`ToolsPolicy` 里的 `PolicyDeps.ToolFace`）对 root
  一律返回 true → **继承员工的工具面就是宿主的全量面**（探针实测：抽样 20 个工具全部在面上）；
- 调用时由中间件的 `Gate` 判定（探针实测：`read_file` 直放、`write_file`/`switch_plugin`/
  `computer_click` 各走一次审批页、`bash "rm -rf /"` 直接拒绝）。

**断点在审批呈现**：权限门把审批请求的会话归属写成**调用会话**，而员工回合的调用会话是
角色会话（`goal-a2a-pm` / `advisor:<main>`）。`application/core` 的 `observeInteraction`
只把"视图会话（或空归属）"的待批镜像进单格 `Snapshot.Interaction`；角色会话不是视图会话，
`service.sessions.Unit(角色会话)` 又是 nil（没有会话单元）。两条读面都落空 →

> 员工越权提权对宿主**完全不可见**，只能等到 `limits.approval_timeout` 超时被拒。
> 即"继承全量工具 + 中间件审查"这套机制里，**唯一给人用的那一环事实上不存在**。

这是可复现的结构事实（不是猜测）：角色会话号既不是视图会话，也没有会话单元承载。

## 三、改动：审批归属按宿主主会话折算（只改"弹在哪"，不改"按谁判"）

- `seelebridge/tools.PermissionGate` 增 `RoleSessionOwner func(sessionID) (string, bool)` +
  `SetRoleSessionOwnerResolver`：组合根（`main.go`）注入 `app.RoleSessionOwner`——与会话权责
  读面同一份反查索引（`application/core/agentteam_role_index.go`）。
- `Middleware` 写 ctx 会话归属前先折算：
  `toolspermission.WithSessionID(ctx, approvalSessionFor(sessionID))`。折算只影响**审批
  呈现**：主体判定读的是 `SessionFromContext` 的原始会话（telemetry 键），两条路互不干扰，
  因此"折算了归属"不会把 readonly 员工的写调用放行（用例 `TestEmployeeApprovalOwnershipDoesNotChangeJudgement`）。
- `seelebridge.Runtime.SetRoleSessionOwnerResolver` 透传；`main.go` 在
  `SetRoleSessionPolicyResolver` 旁接线（同一反查索引，两处同一份事实）。
- 未注入 / 未命中 → 按调用会话原样归属（后台会话/旧宿主行为不变）。

折算之后的读面全部是**现成的**：视图单格弹窗、目录行 `awaiting_approval`、
`SessionSnapshot.Approvals`；连"会话全权"也一并生效（`ResolveAllFor(主会话)` 会结掉该会话
员工正在等待的提权）——这正是"直接用现有的"。

## 四、证据（红 → 绿）

```text
# 新增：继承口径三条断言 + 审批归属折算
go test ./seelebridge/tools/ -run 'TestInheritEmployee|TestEmployeeApproval' -count=1 -v   # PASS（5 条）
# 跨层：员工越权 → broker 待批 → 归属宿主主会话 → 放行后执行
go test . -run TestEmployeeEscalationLandsInOwnerSessionPanel -count=1 -v                  # PASS
```

红灯（把折算拆掉后，两处各自失败一次）：

```text
--- FAIL: TestEmployeeApprovalAttributedToOwnerSession/注入折算：角色会话_→_宿主主会话
    permission_inherit_test.go:141: 审批归属 = "goal-a2a-owner", want "sess-main"
--- FAIL: TestEmployeeEscalationLandsInOwnerSessionPanel
    permission_employee_panel_test.go:78: 审批归属 = "goal-a2a-owner", want "sess-main"
```

回归（本机 Windows，全部 exit 0）：

```text
gofmt -l seelebridge application main.go permission_employee_panel_test.go   # 无输出
go vet ./seelebridge/... ./application/... .                                 # 无输出
go build ./... ; go build -tags "gui,desktop,production" ./...               # ok
go test ./seelebridge/tools ./seelebridge ./application/... ./gui/ . -count=1 # 全 ok
```

## 五、边界与未做（诚实标注）

1. **ADVISOR(`tl`) 仍是构造式只读**（`runtime_goal_tl.go` 的 `ToolsPolicy: readonly`）：它的
   工具面按只读簇收窄，因此不会撞上"规则要求问人"的工具（ro 簇没有 ask 规则），本次的折算
   对它只是兜底。要让评审者"跑测试"（`bash` 属 rw）是另一个产品决定：改的是那个装配点的
   常量，不是本次的归属折算。
2. **角色身份未写进审批记录**：折算后审批归属是宿主主会话，人类在面板上看到的是
   `ToolName + Preview`（工具与参数），**看不出是哪个员工**。要做"员工 X 请求越权"的标注，
   得在 `ApprovalHandler` 外包一层改写 `Question`（`ApprovalContext.Request.Context` 里有
   主体 `emp_<角色>`），本次未做。
3. **全权语义外溢**：主会话开全权后，其员工的越权请求会被 broker 的会话级自动放行（因为
   归属折算到了主会话）。方向是"用户说了这一会话免审"，但等于对员工也免审；严格口径应把
   "员工不享会话全权"也钉住（子代理已经有这条断言）。
4. **归属反查的歧义未消除**：角色会话号不含主会话身份（`teamID-roleName`），两个主会话用
   同一 `team_id` 时 `RoleSessionOwner` 取"第一个读到的"——审批呈现因此可能归错会话。要根治
   得把主会话身份编进角色会话号（存储迁移），不在本次范围。
