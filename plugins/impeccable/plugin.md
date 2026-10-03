---
schema_version: 1
name: impeccable
description: 前端设计纪律与确定性检测器（Impeccable 移植）
include: []
exclude: []
---

# Impeccable 前端设计纪律

`$impeccable` 是**设计垂直面**的专业能力：把「AI 生成前端」最容易犯的
模板化套路，变成**写在纪律里、可被 61 条确定性规则机械检测**的东西。

三件事：

1. **纪律**（`impeccable/SKILL.md` + `impeccable/reference/craft-floor.md`）——
   工艺底线（对比度/间距/字阶/动效/状态/浏览器原生表面）与「拒绝清单」
   （卡片套卡片、章节 eyebrow、渐变文字、侧条纹边框、emoji 当图标……）。
2. **命令路由**——24 条命令（`shape`/`critique`/`audit`/`polish`/`distill`/
   `harden`/`animate`/`typeset`/`layout`/`bolder`/`quieter`/`clarify`…），
   各自有 playbook，按需从上游取。
3. **确定性检测器**——`npx -y impeccable detect <path>`：零 LLM、零 API key，
   退出码可进 CI。它是**证据**，不是审美裁判：干净 ≠ 好看。

## 使用口径

- 入口：`$impeccable`（`$` = Skill 前缀）。首次使用先把当前 Plugin 切到本 plugin
  （`#` = 切 Plugin 的前缀 + plugin 名；`#` 与 `$` 一字符一含义，别混）。
- **设计任务必须先读 `impeccable/reference/craft-floor.md` 再动 UI**，并按其
  Verify 清单在**一轮批量检查**里核完，不做无限自 QA。
- 改动 UI 后按项目纪律取证：`npx -y impeccable detect --json <改动路径>`
  + 真实渲染截图（`computer_screenshot`）+ 关键路径点一遍。
- 不接后端与非 UI 任务；`live` 模式只对本地 checkout，不碰线上站点。

## 生态位

`plugins/default` 是「全工具入口」，`plugins/impeccable` 是**设计这一垂直面的
专业 plugin**：工具面不裁剪（视觉工作要用到 bash 跑检测器、computer use 取渲染
证据、plan/goal 做长任务），差别在**注入的纪律与 skill**。

出处与许可：上游 `pbakaus/impeccable`（Apache-2.0），详见 `README.md`。
