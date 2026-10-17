# core/plan（根包分卷）

## 生态位

Plan 打点/分支事件/重规划

覆盖：`plan*.go`；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### plan_tools.go

- `func (service *Service) planProjectionLocked(sessionID string) *model.PlanState` — planProjectionLocked 返回指定会话的历史 Plan 显示投影（调用方持有 Core.ViewMu）。
- `func (service *Service) mirrorPlanProjectionForSessionLocked(sessionID string, fallback func() *model.PlanState)` — mirrorPlanProjectionForSessionLocked 在会话成为视图会话时刷新
