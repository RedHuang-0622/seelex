---
schema_version: 1
name: default
description: 所有已注册工具与全局 Skill
include: []
exclude: []
---

# Default

使用全部已注册工具与全局 Skill。Plan 是启动即注册的基础工具，而不是独立 Plugin；通过 `#plan` 注入默认 Plan Skill，并使用 `plan_load`/`plan_run` 执行工作流。
