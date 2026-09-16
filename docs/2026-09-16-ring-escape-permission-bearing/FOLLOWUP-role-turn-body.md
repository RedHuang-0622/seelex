# 追加交付：角色回合执行体（`seelebridge.RunRoleTurn`）

> 日期: 2026-09-16（承接同日 `CHANGES.md` 的"角色回合执行面未实现"）
> 范围: `application/contract/{ports.go,dto/agentteam.go}`、`application/core/{role_turn.go,role_turn_test.go,goal_coordinator.go,service_assembler.go}`、`seelebridge/{runtime_role_turn.go,runtime.go,runtime_role_turn_test.go}`、`main.go`

## 一、为什么补这一层

同日 `A2A-VALUE-REVIEW.md` §1 给出的结构判断是：当时的"团队"其实是
**单执行体 + 一个上下文隔离的评审者 + 一个确定性逃生天花板**——除 `tl` 的
ADVISOR 评审回合外，**没有任何角色会真的跑一轮**（`RolesWithExecutor` 事实表
只有 `user`/`main`/`tl`）。于是同日的权限整改只交出"判定链已就位"，没有"被拦的
对象"：按角色拦截没有承载体。

这一轮补的就是那个承载体：**员工座位真的带工具跑一轮，并且从起手就被权责管辖**。

## 二、改了什么

### 1. 跨层契约（application/contract）

- `dto.RoleTurnRequest` / `dto.RoleTurnOutcome`：角色回合的跨层形态（主会话、
  角色名、**角色会话**、**权责口径**、链上位次、本轮工作正文；回执 = 是否跑了 /
  是否推进 / 一句话摘要）。
- `contract.RoleTurnPort` + `Dependencies.RoleTurn`（未装配 = 试水形态）。
- `application/core/role_turn.go`：`contract.RoleTurnPort → core.RoleTurnRunner`
  的适配 + `roleTurnRunnerFor`；`service_assembler` 把它接进 `goalCoordinatorDeps.RoleTurnFor`。

### 2. 执行体（seelebridge/runtime_role_turn.go）

`Runtime.RunRoleTurn(ctx, dto.RoleTurnRequest)` 的顺序即语义：

1. **开角色会话**（没有就开）：`roleSessionFor`——**开的同时**把该员工的权限写进
   权责表（`AssignEmployeePermissions`，与装配期同一条路径），落成 `emp_<角色名>`
   主体条目；口径不可分配（空/`full`/未识别）时**不写条目**（"继承宿主默认"必须
   真的是继承）。
2. **绑项目根**：角色会话按主会话的项目根解析工具路径（`ProjectScope.RootFor`/
   `BindFor`）——不绑根，FileSystem 工具无根可解析（该 scope 刻意没有"回落进程工作
   目录"的兜底）。
3. **按构造带主体**：`tools.WithEmployeeSubject(ctx, 角色名, 权责口径)` +
   telemetry 会话标签写成角色会话。**这条不依赖任何反查**——即使"角色会话 → 权责"
   反向索引冷启动没查到，员工回合的工具调用照样按员工口径拦。
4. **跑一轮有界回合**：`roleTurnMaxLoops = 12`；系统提示优先取已登记的员工提示词，
   未登记给最小角色框架；本轮正文来自 `RoleTurnRequest.Input`。

角色会话引擎：默认建框架 `session.Session`（`own loop` + **节点级**上下文组件 +
独立 `SessionID`），可用 `SetRoleEngineFactory` 替换；`Shutdown` 释放
（`ReleaseRoleSessions`）。

### 3. 本轮正文的透传（application/core/goal_coordinator.go）

座位在装配期构造、不持有"这一轮发生了什么"。本轮 `detail` 经 ctx 透传
（`withRoleTurnInput` → `roleTurnSeat.Act` 读回 → 请求 `Input`）；请求里已显式给的
`Input` 优先于 ctx。

## 三、证据（本轮全绿）

```text
gofmt -l application seelebridge main.go gui        # 无输出
go build ./...                                      # ok
go build -tags "gui,desktop,production" ./...       # ok
go vet ./...                                        # 无输出
go test ./... -count=1                              # 全部 ok（无 FAIL）
go test -race ./seelebridge/ -run RoleTurn -count=3 # ok（无竞态）
go test -race ./application/core/ -run 'RoleTurn|AdvanceAfterChat' -count=3   # ok
```

新增用例（先跑红/再转绿）：

- `seelebridge/runtime_role_turn_test.go`（8 条）：
  - 回合真的落在**角色会话**上（系统提示点明角色、`maxLoops` 有界、引擎只造一次）；
  - **开角色会话即分配** `emp_pm` 主体条目（政策随请求落地）；
  - 该回合的 ctx 按员工口径收窄工具面：readonly 员工看不到 `write_file`、readwrite 看得到——
    **不依赖任何反查**；telemetry 标签 = 角色会话；
  - 同一角色会话跨回合复用引擎；执行面错误向上抛（可 `errors.Is` 归类）；
  - 缺角色名/角色会话显式失败且不开会话；继承口径不写条目；
  - `ReleaseRoleSessions` 后重建（且**不清引擎构造器**——构造器是配置不是派生状态）；
  - 默认引擎（未注入构造器）用角色会话号建框架 Session（不跑 ChatStream，不碰网络）。
- `application/core/role_turn_test.go`（4 条）：座位请求 ↔ 跨层 DTO **逐字段**对应
  （含 `ToolsPolicy`/`Input`）；本轮正文经 ctx 到执行面；显式输入优先；未装配端口
  → 执行面为 nil（试水形态的唯一入口）。

## 四、边界与未做（比改动本身更重要）

1. **真实 API 冒烟与 computer use 实机跑一轮**是**下一件事**，不在本轮：本轮把
   "有东西可冒烟"这件事做出来了（角色回合执行体存在、有真引擎、有权责落点），但
   本轮的验收用假引擎（角色回合的正确性不该依赖一次真实 API 调用）。
2. **ADVISOR（tl）仍然无工具**：`runtime_goal_tl.go` 的评审回合仍是一次无工具的
   completer 调用。`A2A-VALUE-REVIEW.md` §3.3 建议的"给 ADVISOR 上只读工具"因此
   还没兑现；本轮的 `RunRoleTurn` 是它的现成承载体（角色会话 + 员工主体 + 工具面），
   但 ADVISOR 的座位走的是 `TLEvaluator` 另一条路，接线是独立的一件事。
3. **`Progress` 是保守近似**：当前口径 = "本轮产出了非空结论"，不是"目标真的推进了"。
   它只用于喂环的 `no_progress` 逃生记账。
4. **角色会话的引擎是进程内的**：不接 `DurableHistory`，重启即失忆（角色 draft
   存储是另一件事，不是引擎的持久化面）。
5. **角色回合不绑 workspace，只绑项目根**：员工能用文件工具读写项目树，但没有独立的
   worktree 隔离。要更严的隔离面，改的是 `SetRoleEngineFactory` 这一个构造点。
6. **`no_progress` 的门限**与 A2A-VALUE-REVIEW §3.3 的"量起来再决定"仍未做：
   没有 A/B 数据，所以"团队更有用"仍然只是叙事。
