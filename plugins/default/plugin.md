---
schema_version: 1
name: default
description: 所有已注册工具与全局 Skill
include: []
exclude: []
---

# Default

使用全部已注册工具与全局 Skill。Plan 是启动即注册的基础工具，而不是独立 Plugin；通过 `$plan` 召回默认 Plan Skill（`$` 是 Skill 前缀，`#` 是切换 Plugin），并使用 `plan_load`/`plan_run` 执行工作流。

团队作业面由 `$goal` + `$teamwork` 承载：`$goal` 是 agent 自总结的目标看板（阶段打点、可改写正文/完成条件），`$teamwork` 是主代理（leader）的工具面——按里程碑屏障 + 里程碑内 Work Item 派活（`milestones[].depends_on` 是里程碑之间的屏障、`work_items[].depends_on` 是里程碑内的 DAG，两者是顺序的唯一事实；阶段口径已整条退场）、契约先行、以证据收尾。团队**没有席位轮转**。
