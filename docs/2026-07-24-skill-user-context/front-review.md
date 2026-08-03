# Skill 用户上下文前置审查

## 需求摘要

`#skillname 需求` 必须在激活 Skill 后立即把 Skill 名称、Skill 指令和完整原始问题作为同一轮用户上下文发给模型；Skill 指令采用条目化内容，不进入 system prompt。

## 现状结论

- `Service.Submit` 在识别 `#` 后直接返回 `submitSkill`，不会进入 `startChat`，所以 `#review focused` 中的 `focused` 没有成为用户消息。
- `applySkill` 把 `skill.Prompt + args` 压入 `PromptStack`，随后调用 `SetSystemPrompt`；当前 Skill 指令和需求都位于 system prompt。
- `buildSystemPrompt` 还会把全部可用 Skill 名称与描述列入 system prompt。
- Chat 完成后 UI 从 Engine History 重建 Conversation；如果直接把结构化 Skill 上下文作为 Engine input，必须在 UI 展示时还原用户原始输入，避免把整段 Skill 指令显示在聊天气泡里。
- 运行中输入队列目前只保存一份字符串，同时承担 UI 展示和真实模型输入；新方案需要分离 display input 与 model input，才能既显示原问题又发送 Skill 上下文。

## 建议行为契约

| 输入 | 行为 |
|------|------|
| `#review 检查并修复问题` | 激活 `review`，立即发起/排队 Chat；模型收到条目化 Skill + 原始输入 |
| `#review` | 只激活 Skill，不创建空 Chat；下一条普通问题自动携带已激活 Skill |
| 激活后输入普通问题 | 每轮用户消息都携带当前活动 Skill 条目，保持持续 Skill 语义 |
| `#end` | 退栈最后一个 Skill，不发起 Chat |
| `#unknown 问题` | 只显示未知 Skill notice，不发送问题，避免无意绕过选择错误 |
| `/review 问题` | 保留现有 slash Skill 别名，并使用相同的新发送契约 |

模型输入建议格式：

```markdown
<!-- seelex:skill-context:v1 -->
## Selected Skills
- name: review
  instructions: |
    [Skill 原始指令，逐行缩进]

## User Request
#review 检查并修复问题
```

内部版本标记只用于从 Engine History 恢复 UI 显示；模型仍能同时看到 Skill 名称、指令和用户原始问题。

## 影响文件清单

| 文件路径 | 修改类型 | 具体位置 | 修改原因 |
|---------|---------|---------|---------|
| `application/app.go` | 修改 | `Service` 队列字段、`buildSystemPrompt`、`Submit` | 移除 system 中的 Skill 清单，区分 display/model input，令 `#skill 需求` 进入 Chat |
| `application/input.go` | 修改 | `submitCommand`、`submitSkill`、`applySkill`、新增格式化函数 | 激活 Skill、构建条目化用户上下文，不再刷新 system prompt |
| `application/chat.go` | 修改 | `startChat`、`runChat`、队列接续、history adaptation | Engine 收到 model input，UI 使用 display input；批量队列保持两者一一对应 |
| `application/prompt_stack.go` | 修改 | `Render`、注释 | Skill layer 保留为活动状态，但从 system prompt 渲染中排除 |
| `application/prompt_stack_test.go` | 修改 | Render/Skill tests | 固定 Skill 不进入 system prompt、仍可枚举/退栈 |
| `application/application_test.go` | 修改 | fake Engine、Skill routing tests | 证明原始问题与条目化 Skill 一起进入 Chat，system prompt 不含 Skill |
| `application/command_test.go` | 修改 | Skill Submit/#end/queue tests | 覆盖空需求、未知 Skill、持续激活和 UI 展示 |
| `application/race_test.go` | 可能修改 | 输入队列竞态断言 | 若内部队列结构调整，保持 race 测试覆盖 |
| `docs/feature-instrumentation.md` | 修改 | Skill 功能打点 | 记录新提示词边界和源码/测试证据 |

## 依赖分析

- 上游：`SkillPort.Get/All`、`SkillInfo`、`PromptStack`、Engine `ChatStream/History`。
- 下游：TUI/GUI 共用 `Service.Submit`，因此两个前端都会获得一致行为；Session History 会保存结构化模型输入，但 UI adaptation 只展示原始输入。
- 不修改 `ChatEngine`、Snapshot/Event protocol 或 Wails Bridge 公共接口。
- Skill 仍由 PromptStack 保存活动顺序和 LIFO `#end`，但 `Render` 只输出 identity/base/effort/instructions。

## 循环依赖检查

- [x] 只在 `application` 包内部调整，不新增 import 边。
- [x] Skill loader/registry 不反向依赖 Application。
- [x] 不新增全局可变状态；队列继续受 `Service.mu` 保护。

## 风险预估

- Engine History 泄露 Skill 内容到 UI：中概率、高影响；使用版本化 envelope，并在所有 user history adaptation 中恢复原始输入。
- 运行中切换 Skill 导致排队请求使用错误 Skill：中概率、中影响；提交时固化 model input，队列分别保存 display/model 两份数据。
- 每轮重复 Skill 指令增加 token：高概率、中影响；这是“不放 system prompt且持续激活”的直接代价，保持格式紧凑并在 `#end` 后停止附加。
- 旧测试依赖 Skill 位于 system prompt：高概率、低影响；改为断言 system 隔离与 Chat input 契约。
- 用户自然输入伪造 envelope：低概率、中影响；只有固定前缀开头才做 UI 还原，模型输入仍完整保留，不影响权限边界。

## 建议方案

保留 PromptStack 作为活动 Skill 注册表，但让 `Render` 排除 `kind=skill`；新增纯函数构造/解析版本化 Skill 用户上下文；重构 Chat 入口接受 display/model 两份输入，并为队列建立一一对应的双通道。`#skillname` 无需求时只激活，有需求时立即 Chat；普通输入自动携带所有活动 Skill。
