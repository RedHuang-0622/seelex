---
schema_version: 1
name: default
description: 所有已注册工具与全局 Skill
include: []
exclude: []
---

# Default

使用全部已注册工具与全局 Skill（`$` 是 Skill 前缀，`#` 是切换 Plugin）。**规划/编排不另设 Skill**：原 `$plan`（`plan_load` DAG）已退场 —— 本插件工具面里没有 `plan_load`/`plan_run`/`plan_status`/`plan_clear`，那份 SKILL.md 要求"首步必须 `plan_load`"已无法执行；作业编排走 `$goal` + `$teamwork`（leader 按里程碑 + Work Item 派活，重活交给 subagent / teammate）。

团队作业面由 `$goal` + `$teamwork` 承载：`$goal` 是 agent 自总结的目标看板（阶段打点、可改写正文/完成条件），`$teamwork` 是主代理（leader）的工具面——按里程碑屏障 + 里程碑内 Work Item 派活（`milestones[].depends_on` 是里程碑之间的屏障、`work_items[].depends_on` 是里程碑内的 DAG，两者是顺序的唯一事实；阶段口径已整条退场）、契约先行、以证据收尾。团队**没有席位轮转**。

**桌面与图层纪律**见 `$desktop-discipline`（本插件的工程纪律）：桌面是用户正在用的工作台，不是画布——禁止置顶/覆盖/最大化窗口，禁止改用户窗口状态与抢前台，禁止未经请求的合成鼠标键盘输入，禁止跨轮留窗；需要"看图/读字"时按 `read_file` → OCR（Windows.Media.Ocr）→ PIL/numpy 量测 → ffmpeg 解码的顺序走**无窗口路径**，`computer_*` 当破坏性能力对待。leader 每份派活都要带这一条。
