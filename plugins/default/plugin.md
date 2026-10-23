---
schema_version: 1
name: default
description: 所有已注册工具与全局 Skill
include: []
exclude: []
---

# Default

使用全部已注册工具与全局 Skill（`$` 是 Skill 前缀，`#` 是切换 Plugin）。**规划/编排不另设 Skill**：原 `$plan` **Skill** 已退场（`plan/` 目录与其 SKILL.md 已删，不再有"首步必须 `plan_load`"这类要求）；但 plan DAG **工具**仍在生产注册（`plan_load`/`plan_run`/`plan_status`/`plan_clear`/`plan_export`/`plan_validate`，由 `seelex-workplan` provider 随 `planExecutor` 装配），仅由 `$goal` 门控可见——主代理在 goal Skill 激活时可见、子代理不可见；作业编排走 `$goal` + `$teamwork`（leader 按里程碑 + Work Item 派活，重活交给 subagent / teammate）。

团队作业面由 `$goal` + `$teamwork` 承载：`$goal` 是 agent 自总结的目标看板（阶段打点、可改写正文/完成条件），`$teamwork` 是主代理（leader）的工具面——按里程碑屏障 + 里程碑内 Work Item 派活（`milestones[].depends_on` 是里程碑之间的屏障、`work_items[].depends_on` 是里程碑内的 DAG，两者是顺序的唯一事实；阶段口径已整条退场）、契约先行、以证据收尾。团队**没有席位轮转**。

**桌面与图层纪律**见 `$desktop-discipline`（本插件的工程纪律）：桌面是用户正在用的工作台，不是画布——禁止置顶/覆盖/最大化窗口，禁止改用户窗口状态与抢前台，禁止未经请求的合成鼠标键盘输入，禁止跨轮留窗；需要"看图/读字"时按 `read_file` → OCR（Windows.Media.Ocr）→ PIL/numpy 量测 → ffmpeg 解码的顺序走**无窗口路径**，`computer_*` 当破坏性能力对待。leader 每份派活都要带这一条。

**内置 SSH 工具族**是本插件的远端执行面：`read_file` / `write_file` / `edit_file` / `bash` / `bash_bg` / `bg_manage` 在远端环境上同名可用——同一套回执与句柄契约，只是执行面在远端，证据要标清是本机还是远端。远端环境**自命名**（`dev`、`test` 等），按 `name` 指名、`id` 才是唯一键（改名不改身份）。权限分两类且不可互推：`ssh` 是**通道能力**（能不能建连、能不能在该环境上跑东西），`ssh.userspace` 是**用户空间访问面**（哪些远端路径算你的活）；`ssh` **不隐含任何操作权限**——能建连不等于能读、能写、能删，具体动作仍按本机同一份权限判据裁定。三条纪律：口令只走凭据存储，不进命令行、脚本与日志；未经显式信任不得接受主机指纹（首次见到的新指纹先问，不静默记下）；远端命令的执行面要**如实上报**——退出码、被截断的输出、仍在跑的作业、超时段落，都不许说成成功。
