# assembly/goal（根包分卷）

## 生态位

goal 域协调器/门面用例与「goal 上线不覆盖会话团队」接线回归

覆盖：`goal*.go`；未归属文件由覆盖自检拦下。

## Goal Application 用例（AP-015 首个切片）

`Goal application` 集中会话目标用例：协调器查找、Begin/Update/Done/Status、
治理视图和运行投影刷新。`goalCoordinator` 继续持有按会话 bundle，
`kernel/goal.Controller` 继续唯一拥有目标状态机。

> **2026-10-03（阶段三 W3）**：goal 的**席位轮转已整条退场**。回合尾不再自动跑
> "exec 让位 → advisor 评审"的座位循环（`govern` 包、`Adapter`/`NewAdvisorSeat`、
> 座位作业面 `jobs.KindSeat`、headless `goal_gov_*`、视图的 Round/座次/断环 一并删除）。
> goal 的驱动改为**提示词驱动的 leader 派活**（见 `plugins/default/goal/SKILL.md`），
> 终态判定只在显式入口：`goal_propose_finish` 的终态 gate 与审批预筛。保留不变的是
> `Controller` + `Supervisor` + 终态 gate + 逃生。详见
> [`docs/devlog/2026-10-03-seat-rotation-retired.md`](../../docs/devlog/2026-10-03-seat-rotation-retired.md)。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### goal_service.go

- `func (service *Service) refreshGoalRuntimeProjection(sessionID string)` — refreshGoalRuntimeProjection 在 goal 状态迁移后刷新目标会话的 runtime
- `func (port goalGovernanceViewPort) GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView`
