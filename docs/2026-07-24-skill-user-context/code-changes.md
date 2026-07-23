# Skill 用户上下文代码变更

## 变更概览

本轮把 Skill 从 system prompt 迁移到每轮 user message，并修复 `#skillname 需求` 只激活、不发送需求的问题。TUI 与 GUI 共用 `application.Service`，因此无需修改前端提交协议即可获得一致行为。

## 文件与职责

| 文件 | 变更 | 设计职责 |
|------|------|----------|
| `application/skill_context.go` | 新增 | `chatRequest` 双通道、Skill 条目化格式、版本化 envelope、队列合并、UI 解包 |
| `application/app.go` | 修改 | 删除 system prompt 中的 Skill 清单；统一普通/Skill Chat 提交与排队；分页历史恢复原文 |
| `application/input.go` | 修改 | `#`/slash Skill 激活后按是否含需求决定发送；`#end` 保持剩余 Goal 的循环上限 |
| `application/chat.go` | 修改 | UI 使用 display input，Engine 使用 model input；批量队列和完成历史走双通道 |
| `application/prompt_stack.go` | 修改 | Skill 保留为活动层但不参与 Render；system prompt 固定四层排序 |
| `application/*_test.go` | 修改/新增 | 单元、路由、History、队列、LIFO、Goal MaxLoops 和并发契约 |
| `README.md`、`docs/` | 修改/新增 | 当前事实、详细设计、功能打点、测试报告和最终审查 |

## 核心实现

### 1. system prompt 与 Skill 分离

`application/app.go:62` 的 `buildSystemPrompt` 不再遍历 `Skills.All()` 生成 `Available Skills`。`application/prompt_stack.go:89` 的 `Render` 跳过 `kind=skill`，并通过 `systemPromptPriority` 固定输出：

```text
identity → plugin/base → effort → instructions
```

PromptStack 仍保存 Skill layer，用于多 Skill 活动状态、状态栏和 `#end` LIFO。

### 2. 输入双通道

`application/skill_context.go:14` 定义：

```go
type chatRequest struct {
    displayInput string
    modelInput   string
}
```

- display input 始终是用户原文，供 Conversation、Event 和 Queue 展示；
- model input 在有活动 Skill 时包含 `Selected Skills` 条目和 `User Request` 原文；
- 无活动 Skill 时 model input 保持原样，避免普通对话额外开销。

### 3. Skill 路由

`application/input.go:44-91` 实现统一契约：

- `#review 需求` 与 `/review 需求`：激活并提交；
- `#review` 与 `/review`：只激活；
- 普通输入：携带当前全部活动 Skill；
- `#end`：只退栈，不发 Chat；
- 未知 Skill：只发 notice，不把后续问题发送给模型。

发送内容使用完整原始 input，参数切分只用于判断是否存在需求，不用 `strings.Join(args)` 重建问题。

### 4. Queue 与 History

`application/app.go:131` 在 Submit 时构造 `chatRequest`，从而固化当时的 Skill 快照。`application/skill_context.go:75` 在当前 Chat 结束时合并排队项，只保留一个外层 envelope。

`application/chat.go:103` 和 `application/app.go:480` 附近的 `adaptEngineMessage` 对 user role 调用 `displayUserInput`，覆盖 Chat 完成、Session resume 和分页历史三条 Engine → UI 路径。

### 5. 审查中追加修复

| 问题 | 修复 |
|------|------|
| Goal 位于 Skill 栈底时，退栈上层 Skill 会错误恢复普通 MaxLoops | `application/input.go:59` 根据剩余 Skill 判断；Goal 仍活动则保持 9999，Goal 退栈后才恢复当前 Effort |
| 重新应用 Effort 会把 layer 移到 instructions 之后 | `application/prompt_stack.go:89-125` 在 Render 副本上稳定排序，不改变活动 Skill 原始顺序 |

## 兼容性

- `Service.Submit`、Snapshot/Event DTO、Wails Bridge 和 TUI Controller 公共签名不变；
- Engine/Session schema 不变，Skill model input 作为普通 user message 持久化；
- 新客户端可从 v1 envelope 恢复 UI 原文；非法或未知格式保持原样；
- slash Skill 别名保留；无 Skill 的普通输入逐字节保持原行为。

## 回滚

如需回滚，只需恢复 PromptStack 对 Skill 的 Render、删除 `chatRequest`/envelope，并让 `#skill` 回到仅 `applySkill` 的旧路径；不涉及数据 schema 迁移。已经保存的 v1 envelope 在旧客户端会显示为普通文本，不会导致 Session 无法加载。
