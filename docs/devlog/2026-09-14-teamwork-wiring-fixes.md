# 2026-09-14 AgentTeam team work 接线修复（review 最小修复建议 1~4）

> 范围: `application/core/goal`（工作进展帧）、`application/core`（工作摘要生产者）、
> `application/core/agentteam` + `sessionstore` + `internal/adapters`（floor 出口、
> 无执行者声明、接线状态守卫）、`gui/team_workcontent_live_probe_test.go`（回归哨兵）、
> `scripts/gen_core_readme_index.py`（散文保留缺陷）。
> 前置 review：`2026-09-14` 的 headless 全链路冒烟（结论：三处独立缺口，前端无责）。

## 一、背景

前一轮 review 的结论是「team work 没有接线到工作内容」= 后端三处独立缺口，并给出
4 条最小修复建议（未实施）。本次按建议 1~4 全部落地，并各补用例；改动全部有可复现
证据（包内用例 + 真机 API 冒烟）。

## 二、修复 ①：EXEC 的工作内容进入 ADVISOR 输入

问题：`turn_completed` 只推进水位、不抽帧，生产侧 `Detail` 恒为空，ADVISOR 的输入
= goal 锚点 + 打点帧 + 自身记忆——它"评审"的是自己的账本，不是 EXEC 干的活。

改动（EXEC → ADVISOR 的载荷边）：

1. **生产者带工作正文摘要**
   - 新增 `application/core/goal_work_summary.go`：`summarizeTurnWork` 只取「最后一条
     `user` 行之后」的内容（跨轮不串），正文取本轮终稿、换行压单行、工具名按出现顺序
     去重追加，按 `goaldomain.MaxSignalDetailRunes`（400 rune）截断；
     `goalTurnWorkSummary` 读会话可见投影（`View.mu` 叶子锁，Session 锁内外都可调）。
   - `GoalIterationCompleted`（`iteration_complete`）与 `AdvanceAfterChat`（`chat_end`）
     两个 `turn_completed` 生产者都携带 `Detail`（后者签名加 `detail` 形参）。
2. **治理域抽帧**
   - 新帧类型 `FrameWorkProgress = "work.progress"`（`application/core/goal/advisor.go`）。
   - `Supervisor` 增加待抽帧缓冲：`Notify` 收到带 `Detail` 的 `turn_completed` 时入缓冲
     （内容级去重：同一轮被两个生产者各报一次只留一条；超 `MaxWorkFrames`＝6 丢最旧），
     `runRoundLocked` 在**触发帧之前**一次性抽成 `work.progress` 帧（`ref_seq` 按水位单调，
     合法性与跳帧语义不变）。
   - `turn_completed` 仍不触发评估（跳帧策略不变）：回合由 goal 治理的 Governor 座位
     驱动，现在它带给 ADVISOR 的输入里有 EXEC 的真实产出。

证据：`application/core/goal/work_progress_test.go`（帧下发/正文含 marker/无 detail 不产帧/
去重/有界/帧单调）、`application/core/goal_work_summary_test.go`（摘要口径 + 端到端：
会话里的一轮 EXEC 产出 → ADVISOR 回合输入 `RenderText()` 里出现 marker 与工具名）。

## 三、修复 ②：`TeamView.floor_role` 有出口

问题：`dto.TeamView.FloorRole` 有字段、前端渲染 `floor ${floorRole || "—"}`，但
`assembleView` 从不赋值——数据一直在 `message head.floor` 里，缺的是出口。

改动：`sessionstore.Router.ReadMessageFloorWorkspace`（读 message head 并克隆返回；
未 sync 过的会话返回 `nil` 而非错误）→ `internal/adapters.SessionPort.ReadFloorRole`
→ 可选 `agentteam.FloorPort` → `Registry.View`/`SetOrder`/`Factory.Materialize` 每次返回
视图时重新填充（运行态值不缓存、不落盘）。读失败只进 `DesignNotice`，不阻断成员表；
宿主未实现该读面时留空（可选而非塞进 `Port`，不给既有装配桩加编译期义务）。

证据：`sessionstore/team_registry_test.go`（sync 前 nil / sync 后 = 当前发言角色 /
按值返回不被改写 / 未开始会话不报错）、`application/core/agentteam/team_view_test.go`
（可选端口填充、未实现留空、读失败进 notice）、`application/core/agentteam_floor_wiring_test.go`
（应用层端口接线与降级）。

## 四、修复 ③：`TurnScheduler` 的接线状态显式化（选择「标注 + 守卫」分支）

问题：`TurnScheduler`（channel + 链表 + `SetPrefix`「team work 起点→当前位置前缀」）
全仓只有 `scheduler_test.go` 一个调用点；`order_policy/order_roles` 只被 registry/factory
与 draft 排序消费，真正驱动轮次的是 goal 治理里硬编码的 Governor 座位。

**决策：本轮不接线，改为显式标注 + 机器守卫**。理由：接上它需要为每个角色配独立
agent loop（`Requests()` 的投递方）与桌面/会话级互斥，属于新能力；而在已有装配面里
"半接"（用链表顺序去 gate Governor 座位）会制造第二份顺序事实，比不接更危险。
"team work 前缀"的**语义**已由修复 ① 承担（工作进展帧就是 ADVISOR 拿到的"起点→当前"）。

改动：`application/core/agentteam/README.md` 增「接线现状（2026-09-14 复核）」表（逐条列出
能力/现状/证据，含 `TurnScheduler` 尚未接线与 `order_roles` 的真实生效面），`scheduler.go`
文件头写明未接线；新增守卫用例 `scheduler_wiring_test.go`：
- 扫描全仓源码，若出现 `NewTurnScheduler` 的**生产**调用点即失败（提示同步 README 与判据）；
- 断言 README 里必须写着「尚未接线」「接线现状」。

## 五、修复 ④：`review-team` / `research-team` 声明"暂无可执行者"

问题：`reviewer`/`researcher` 除 `presets.go` 外零引用——装配得出来，但没有执行者，
UI 会让人以为装配完就有人干活。

改动：`factory.go` 增执行者事实表 `RolesWithExecutor`（`user`/`main` 由宿主驱动、
`tl` 由 goal 治理执行）与 `unexecutedRoles`；顺序里存在无执行者角色时，
`TeamView.DesignNotice` 明说「本团队（<team_kind>）暂无可执行者：<角色> 目前只有注册
配置与角色会话，装配后不会自动产生回合」。goal-a2a 不产生该条目（其 `tl` 有执行者）。

证据：`application/core/agentteam/team_view_test.go`（review/research 两个 preset 的
装配回执与读视图都必须声明；goal-a2a 不得出现该条目）。

## 六、真机验证（真实 API，全链路 headless）

```text
go build -o tmp/bin/seelex-headless.exe .
$env:SMOKE_TEAM_WORK_LIVE='1'; $env:SMOKE_TEAM_WORK_LIVE_EXPECT_WIRED='1'
go test ./gui -run TestRealAPITeamWorkContentLiveProbe -v -count=1 -timeout 20m
```

`PASS`（25.5s），判据全绿：

```text
verdict = {team_assembled:true, tl_round_ran:true,
           tl_input_has_work_content:true, tl_input_has_work_frame:true,
           view_floor_role_filled:true, conversation_has_tl_rows:true}
```

ADVISOR 那一轮的输入原文（节选，`work.progress` 帧即修复 ① 的交付物）：

```text
[advisor] 你是评审者(ADVISOR)，上下文仅来自下列帧账本与你的回合记忆。
[goal-anchor]
id=g-1 title=team work 接线冒烟 status=active
statement: 验证 teammate 是否收到 EXEC 的真实工作内容
[frames]
- [work.progress] ref=3 src=chat_end : WORK-CONTENT-MARKER-74174
- [tool.checkpoint] ref=4 src=manual_eval
[trigger] govern:advisor-b
```

同一时刻 `team.view.floor_role="tl"`（此前恒为空）；报告落在
`tmp/headless-smoke/reports/team-work-20260914-162319.json`。

探针的缺口红灯模式（`SMOKE_TEAM_WORK_LIVE_EXPECT_UNWIRED`）随缺口关闭退役，改为
`SMOKE_TEAM_WORK_LIVE_EXPECT_WIRED=1`：把「工作正文 + `work.progress` 帧进 ADVISOR 输入」
与「floor 已填」钉成硬断言，缺口复活即红灯。

**观察（未在本次范围内）**：ADVISOR 收到工作正文后仍以 `escalate_human` 收口，理由是
"缺少成员表快照与 tl 回合输入原文"。这是它的证据标准/提示词口径问题（它现在确实看到了
`work.progress` 帧），不是接线缺口；是否给它更多上下文属于下一步的 prompt/协议取舍。

## 七、顺带修复：README 索引生成器丢散文

跑必做的 `python scripts/gen_core_readme_index.py` 时发现 `extra_prose` 会静默丢掉
「紧贴 `覆盖：` 行的手写散文」（`application/core/README-work-table.md` 的
`worktable.changed` 段落就是这样被删的）。已修：散文起点增加「`覆盖：` 行之后」，
并去掉首尾空行使其幂等（连跑两次不再改写文件）。`README-work-table.md` 已复原。

## 八、验证命令与结果

```text
gofmt -l application sessionstore internal gui      # 空
go build ./...
go test ./application/core/... ./sessionstore/ ./internal/adapters/ -count=1   # ok
go test ./gui -count=1                                                          # ok
go vet ./gui/                                                                   # ok
```

## 九、未做 / 下一步

- `TurnScheduler` 正式接线（独立 agent loop + 顺序权威化）：需要先落"每角色 loop"的
  设计与并发护栏（桌面/会话互斥），否则会把单一顺序事实变成两份。
- ADVISOR 的证据标准（第六节观察）：是否放宽/补上下文。
- computer use 工具族（`seelebridge/tools/computer`、`runtime_computer.go`、媒体分区
  `sessionstore/media.go`、`config/seele.yaml` 权限规则等）的工作区改动不在本次提交内，
  待框架侧补充内容落地后在另一个会话提交。
