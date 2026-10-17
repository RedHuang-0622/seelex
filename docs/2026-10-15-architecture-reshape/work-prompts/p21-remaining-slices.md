# 任务书：P21 剩余装配片

> **状态：已完成并提交 `03efb22d`（代码）+ `3e93940c`（记录）。请勿重复认领。**
> 完成时基线：`fb8e625a`（SceneScope 已退役、NodeScope 已删除）；本文件保留为验收记录。
>
> **任务打点**
>
> - [x] 优先级 1 `runtime_teamwork.go`：判据已无剩余（作业记录上限 P21-29、入参口径 P21-37、
>   系统提示 P21-38 均已归位）；本轮把九处“会话 → 团队作用域键”接线收成
>   `resolveTeamworkScope` 一处。
> - [x] 优先级 2 `workunit_team_records.go`：存/删/回灌仍是宿主 I/O；记录归属过滤改走域包
>   `SceneOwnsNode`，作用域键改走同一份解析。
> - [x] 优先级 3 `runtime_teamwork_items.go` / `workunit_assembly.go`：逐项审计后按设计文档
>   留在装配处，本片未改（`workunit_assembly.go` 的 SceneScope 改造属另一会话的 `2c043125`）。
> - [x] 优先级 4 `runtime_context.go` / `runtime_tools.go`：逐项审计后按 §38 表判为装配面，
>   本片未改。
> - [x] README / `m5-record` §62 / 验证矩阵更新。
>
> **与原任务书的偏差（更新说明）**：原判断“剩余的是宿主动作与端口接线”成立，但端口接线里
> 仍有**同一判据的第二份实现**：会话作用域键解析写了九遍且已漂移（两处只看 `KeyFor` 的 ok、
> 半截键可进存储；一处漏 trim）。按 AGENTS §8.3 先收成一份，而不是只做“保持不动”的审计。

## 你的任务

在 `refactor/architecture-reshape` 分支上，继续 `m5-record.md` §45–§51 的 P21
“域方法随类型走”收尾。**剩余的是宿主动作与端口接线，不是判据**；判据已经下沉，
不要把它们搬回装配层，也不要在装配层复制第二份。

## 当前事实

- 最近提交：`3d2a9ab7`（Plan 引擎已删除，fork 直连 AgentNode）。
- P21-1…P21-38 已完成；装配层剩下的主要是：
  - `backend/engine/assemble/runtime_teamwork.go`：
    `RunWorker` / `workerRoleRoundSpec` / `bindWorkerProjectRoot` / `writeWorkerOutput` /
    `teamworkJobOutputs` / `SetTeamworkBackend` 的装配；
  - `backend/engine/assemble/workunit_team_records.go`：存/删/回灌的 I/O；
  - `backend/engine/assemble/runtime_teamwork_items.go` / `workunit_assembly.go`：
    设计文档明确留在装配处的端口实现与三格适配器；
  - `runtime_context.go` 组件工厂、`runtime_tools.go` 注册面。
- `runtime_plan.go` 已不存在；不要按旧文件路径改。

## 工作方式

1. 每次只取一个文件/一组同域函数，先读目标模块 README 的“文件与函数索引”。
2. 判断它属于哪一类：
   - **宿主动作**（开角色会话、绑项目根、写输出、回收现场）→ 留在装配层，但压成薄函数；
   - **判据/词表/状态判定** → 必须在拥有该规则的域包，装配层只转发；
   - **端口接线** → 用窄接口/函数值，不把跨域具体类型带进域包。
3. 如果发现“像判据但不在域包”，先停下来写审计结论，不要顺手复制。
4. 每片更新对应 README；删除的文件/函数在交付里列“文件:行 → 新唯一位置”。

## 优先顺序（按价值）

1. `runtime_teamwork.go` 的判据（作业记录上限、场景清单、派发/汇合入参口径）。
2. `workunit_team_records.go` 的 I/O（记录形状已在 `store`/`contract`）。
3. `runtime_teamwork_items.go` / `workunit_assembly.go` 的端口实现与三格适配器。
4. `runtime_context.go` 组件工厂、`runtime_tools.go` 注册面。

## 验证

```text
go build ./...
go vet ./...
go test ./backend/engine/assemble/ ./backend/multiagent/teamwork/ ./backend/workunit/ -count=1 -timeout 1200s
python scripts/gen_core_readme_index.py --check
python scripts/check_readme_refs.py --strict
```

## 约束

- 不要动 NodeScope/SceneScope 的改名工作（另一个会话负责）。
- 不要恢复 `plan_*` 工具、Plan DAG、Plan 审批门或 replan。
- 一个可解释切片一个 commit；提交到 `refactor/architecture-reshape`，不要 push。

交付时按片列出：搬走了什么、删掉了什么重复实现、哪些仍是装配层宿主动作、测试证据。

---

## 完成记录（2026-10-17）

### 实际落地的两片

| 片 | 内容 | 证据 |
|---|---|---|
| P21-39 | 会话作用域键解析收成一份：`store.Key.Complete` + 装配层 `resolveTeamworkScope` / `teamworkKeyFor`；看板域改问 `Key.Complete` | `03efb22d`；`TestResolveTeamworkScopeJudgment`、`TestTeamworkKeyJudgmentMatchesBoardDomain`（宿主与看板域同解） |
| P21-40 | 现场名单投影/归属过滤收成一份：恢复分派改走 `teamSceneNodeIDs`（域包 `SceneNodeIDs`）；记录归属过滤改走域包新增的 `SceneOwnsNode` | `03efb22d`；`TestSceneOwnsNode` |

### 删除清单（文件:行为 → 新唯一位置）

- `runtime_teamwork.go` `coordinatorFor` 的 trim / 后端 / `KeyFor` / 键校验 → `runtime_teamwork_scope.go:resolveTeamworkScope`
- `runtime_teamwork_items.go` `coordinatorForSession` 的同一段（此前只查 ok）→ 同上
- `runtime_teamwork_scene.go` `teamSceneIndex` 的同一段 → 同上
- `workunit_team_records.go` `teamUnitScope` 的同一段 → 同上（经 `teamworkKeyFor` 的 ok 形态）
- `runtime_teamwork_context.go` `teamworkReadScope` 的同一段 → 同上
- `runtime_teamwork_board_archive.go` `archiveTeamBoard` 的同一段 → 同上
- `runtime_teamwork_board.go` `TeamworkBoardSnapshot` 的同一段 → 同上
- `runtime_teamwork_board_close.go` `CloseTeamBoard` 的同一段 → 同上
- `runtime_project_board.go` 旧 `teamworkKeyFor`（漏 trim）→ `runtime_teamwork_scope.go:teamworkKeyFor`
- `middle/view/board/teamwork_plan.go` 内联的两个分量判空 → `store/sessionstore.go:Key.Complete`
- `runtime_unit_recovery.go` `restoreUnitRecords` 手写 `SceneEntry → nodeID` 循环 → `teamSceneNodeIDs`（域包 `SceneNodeIDs`）
- `workunit_team_records.go` `teamUnitReader` 手建 `wanted` 集合 → `teamwork.SceneOwnsNode`

### 仍是装配层宿主动作（按设计保留）

`runtime_teamwork.go` 的 `RunWorker` / `workerRoleRoundSpec` / `bindWorkerProjectRoot` /
`writeWorkerOutput` / `teamworkJobOutputs`；`workunit_team_records.go` 的存 / 删 / 回灌 I/O；
`runtime_teamwork_items.go` 的 `BindWorkspace` / `MergeWorkspace` / `ReleaseWorkspaceItem` /
`EnqueueTeammateMessage`；`workunit_assembly.go` 的 `hostPorts` 三格适配器；
`runtime_context.go` 组件工厂与 `runtime_tools.go` 注册面。

### 审计结论：像判据但不在域包的项

- `AssignEmployeePermissions`（`runtime_tools.go`）：逐条判定 `ToolsPolicy` / 显式权限格子 /
  继承，形态像判据；但 §38 表已把它判为装配期权限表装配（输入 `dto.RoleSpec`、输出
  `toolreg.EmployeePermission`，没有第二个调用方）。本片未搬；若后续要下沉，落点应是
  `backend/tool/registry`，并先补跨实现一致性用例。
- `teamUnitSurfaceAlive`（`workunit_team_records.go`）：只回答“这个单元在本进程还有没有
  执行面”（角色会话句柄 / 作业句柄两格读数）；“记录说在跑且执行面不在 ⇒ 中断”的判据在
  `teamwork.InterruptedUnits`（P21-36），本层不复制。
- `ReleaseWorkspace` / `ResetSession`：宿主回收动作；脏判定走 `worktree.PathDirty`，
  本层不另写。

### 验证

```text
go build ./...                                                          0
go build -tags "gui,desktop,production" ./...                           0
go vet ./...                                                            0
go test ./store/ ./middle/view/board/ ./backend/multiagent/teamwork/ ./backend/engine/assemble/ -count=1 -timeout 1200s
                                                                        全部 ok
go test ./backend/workunit/ -count=1 -timeout 600s                      ok
python scripts/gen_core_readme_index.py --check                         90 个 README，无漂移
python scripts/check_readme_refs.py --strict                            180 个 README，0 未解析
```

未跑 `go test ./...`：完成时另有 worktree 会话的未提交改动；`application/core` 3 例基线红为
存量（见 [baseline-context-tests.md](baseline-context-tests.md)）。
