# Effort 与 Skill 上下文架构

> 状态：已实现
> 更新日期：2026-07-24
> 适用范围：`application`、TUI、Wails GUI、Engine History

## 1. 目标与边界

Effort 和 Skill 都会影响模型行为，但二者属于不同信任与生命周期边界：

| 内容 | 传输位置 | 生命周期 |
|------|----------|----------|
| Identity、Plugin prompt、Effort、系统能力说明 | system prompt | 运行时配置，切换时重建 |
| 当前活动 Skill 名称与指令 | user message | 每次提交时固化 |
| 用户问题 | user message | 单轮输入 |

核心不变量：

1. Skill 名称、描述和指令不写入 system prompt。
2. `#skillname 需求` 中的完整原始输入必须真正发送给 Engine。
3. UI、输入队列和恢复后的会话只展示用户原文，不展示内部 Skill envelope。
4. Skill 仍然支持多层活动状态和 LIFO `#end`。
5. 排队请求使用提交时的 Skill 快照，后续激活或退栈不改写它。

## 2. 总体数据流

```text
TUI / GUI Submit("#review 检查并修复")
                 │
                 ▼
Service.Submit ──识别 #/slash Skill──► activateSkillAndSubmit
                 │                          │
                 │                          ├─ PromptStack.Push(kind=skill)
                 │                          └─ 有需求时继续 submitConversation
                 ▼
newChatRequest(original, PromptStack.Layers())
                 │
                 ├─ displayInput = 原始输入 ──► Snapshot / Event / GUI
                 │
                 └─ modelInput = Skill 条目 + 原始输入 ──► Engine.ChatStream
                                                        │
                                                        ▼
                                                 Session History
                                                        │
                                  displayUserInput ◄────┘
                                        │
                                        └─► UI 原始输入
```

## 3. System prompt 组装

`application/app.go` 的 `buildSystemPrompt` 建立以下固定顺序：

```text
identity → plugin/base → effort → instructions
```

`PromptStack` 也保存 `kind=skill` 的层，用于活动状态、状态栏描述和退栈。但 `application/prompt_stack.go` 的 `Render` 明确跳过所有 Skill layer。

系统能力说明只描述协议：模型会在用户消息中收到已选择 Skill。它不枚举 Skill Registry，也不注入 `Available Skills`。这避免了未选择 Skill 污染全局行为、system prompt 随注册表增长，以及用户需求被错误提升为系统指令。

## 4. 输入行为契约

| 输入 | Application 行为 | 是否启动 Chat |
|------|------------------|:-------------:|
| `#review 检查并修复问题` | 激活 `review`，发送完整原始输入 | 是，或进入队列 |
| `#review` | 仅激活 `review` | 否 |
| 活动后输入 `检查问题` | 携带所有活动 Skill | 是 |
| `#end` | 退栈最后一个 Skill，恢复 Effort MaxLoops | 否 |
| `#unknown 问题` | 添加未知 Skill notice | 否 |
| `/review 检查问题` | slash Skill 别名，契约与 `#review` 相同 | 是，或进入队列 |

判断“是否包含需求”使用解析后的参数数量，但发送给模型的是 `strings.TrimSpace` 后的完整原始输入，不用参数重组问题，因此引号、标点和多空格之外的语义不会丢失。

## 5. 模型输入格式

`application/skill_context.go` 生成版本化 Markdown envelope：

```markdown
<!-- seelex:skill-context:v1 display=I3JldmlldyDmo4Dmn6Xpl67popg -->
## Selected Skills
- name: review
  instructions: |
    检查正确性并给出证据。

## User Request
#review 检查这个实现
```

规则：

- `display` 使用 base64url 编码，只用于从 Engine History 恢复 UI 原文；
- 每个 Skill 是独立条目，指令逐行缩进为 block scalar；
- Skill 按 PromptStack 顺序发送，保留叠加顺序；
- 无活动 Skill 时不增加 envelope，Engine 继续收到原始文本；
- marker 带版本号，未知或非法 marker 按普通用户文本保留。

## 6. Chat 请求与队列

内部值对象 `chatRequest` 同时保存：

```go
type chatRequest struct {
    displayInput string
    modelInput   string
}
```

`submitConversation` 在检查 Chat 是否运行之前构造该对象，从 PromptStack 获取线程安全副本。运行中请求进入 `[]chatRequest`，Snapshot 只暴露由 `chatRequestDisplays` 提取的原文。

当前 Chat 完成后，`combineChatRequests` 把所有等待项合并为一轮：

1. display 以 `\n---\n` 合并；
2. 已装饰 model input 去除各自 envelope header，保留每项的 Skill 条目与问题；
3. 只生成一个外层 envelope，供最终历史恢复组合后的 display；
4. 任何一个队列项携带 Skill 时，组合 model input 都被 envelope 包装。

这样既保持原有“排队消息批量接续”语义，也不会因 `#end` 或新 Skill 改变排队期间已固化的请求。

## 7. History 与前端适配

Engine 和 Session 需要保存真实模型输入，才能保证会话上下文一致；前端只应看到用户输入。因此所有 Engine → UI 路径统一调用 `displayUserInput`：

- Chat 完成后的 `appendHistoryLocked`；
- Session 分页的 `adaptEngineMessage`；
- Session resume 间接使用 `appendHistoryLocked`。

只有 role 为 `user` 的消息会解包，assistant/tool 内容保持不变。无法识别的 envelope 原样展示，避免静默丢失历史数据。

## 8. Effort 与 Goal Skill

Effort 继续通过 `application/effort.go` 管理 system prompt 行为层和 Engine MaxLoops：

| 等级 | MaxLoops | system prompt 行为层 |
|------|---------:|:--------------------:|
| lite | 20 | 无 |
| medium | 64 | 有 |
| high | 512 | 有 |
| max | 1024 | 有 |

`goal` Skill 激活时临时设置 `MaxLoops=9999`。`#end` 会重新应用当前 Effort，从而恢复其 MaxLoops。Skill 指令仍走用户消息，Goal 的循环上限特例与提示词传输位置相互独立。

## 9. 实现决策

| 决策 | 选择 | 原因 |
|------|------|------|
| 活动 Skill 存储 | 复用 PromptStack layer | 保留既有 LIFO、Describe 和线程安全能力 |
| Skill 传输 | 结构化 user message | 满足角色边界，问题与 Skill 同轮到达 |
| UI 隐藏方式 | 版本化可逆 envelope | 不修改 Engine/Session schema，历史可恢复 |
| 队列结构 | 单个双字段值对象 | display/model 同生命周期，避免双 slice 漂移 |
| 无 Skill 输入 | 原文直传 | 不增加普通对话 token 和存储开销 |
| 未知 Skill | notice 后停止 | 防止拼写错误时绕过用户选择直接发送问题 |

未采用的方案：

- 把 Skill 写入 system prompt：不满足角色隔离要求，并会把 `#skill 需求` 中的问题提升为系统内容；
- 只发送需求、不发送 Skill 指令：模型无法执行被选择的 Skill；
- UI 直接显示 Engine input：会泄漏长 Skill 指令并破坏聊天可读性；
- 在 Chat 执行时再读取活动 Skill：排队期间的 `#end` 会改变用户原先提交的语义。

## 10. 并发、错误与性能

- PromptStack 自身加锁；`Layers` 返回副本，格式化阶段不持锁。
- `inputQueue` 和 ChatState 由 `Service.mu` 保护。
- envelope 编解码是线性字符串处理，不引入 IO 或外部依赖。
- Skill 指令每轮重复发送会增加 token，这是角色隔离和持续激活语义的直接成本；`#end` 后立即停止附加。
- 非法 base64、未知版本或缺少换行的 marker 不解包。
- 该协议只做展示还原，不作为权限或安全认证边界。

## 11. 源码与测试打点

| 功能点 | 实现位置 | 自动化证据 |
|--------|----------|------------|
| system prompt 排除 Skill | `application/prompt_stack.go: PromptStack.Render`、`application/app.go: buildSystemPrompt` | `TestPromptStack_PushAndRender`、`TestSkillLoadViaSubmit` |
| `#` 与 slash Skill 路由 | `application/app.go: Service.Submit`、`application/input.go: submitCommand/submitSkill` | `TestSuggestionsAndSkillRouting`、`TestSkillLoadViaSubmit` |
| 空需求/后续携带/退栈 | `application/input.go: activateSkillAndSubmit/endSkill` | `TestSkillWithoutRequirementAppliesToNextInput` |
| 条目化 Skill + 原问题 | `application/skill_context.go: formatSkillUserInput` | `TestFormatSkillUserInputCreatesItemizedContext` |
| display/model 双通道 | `application/skill_context.go: chatRequest`、`application/chat.go: startChat` | `TestSkillLoadViaSubmit` |
| 排队固化与批量合并 | `application/app.go: submitConversation`、`application/skill_context.go: combineChatRequests` | `TestQueuedSkillRequestFreezesDisplayAndModelInput`、`TestCombineChatRequestsPreservesDisplayAndSkillBodies` |
| History 原文恢复 | `application/chat.go: appendHistoryLocked`、`application/app.go: adaptEngineMessage` | `TestAdaptEngineMessageRestoresOriginalUserInput` |
| 未知 Skill 不发送 | `application/input.go: submitSkill` | `TestSkillUnknown` |

## 12. 维护清单

- 新增 Engine History → UI 路径时，对 user role 调用 `displayUserInput`。
- 修改 envelope 时新增版本，不改变 v1 的解析语义。
- 修改队列策略时保持 display/model 一一对应和提交时固化。
- system prompt 测试应使用反向断言，确保 Skill 名称、描述、指令和用户需求都不存在。
- Skill 输入协议变更时同步 README、功能打点表和 GUI Application 协议文档。
