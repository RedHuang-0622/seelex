# Skill 用户上下文最终代码审查

> 日期：2026-07-24
> 详设：[`../../arch/skill-effort-architecture.md`](../../arch/skill-effort-architecture.md)
> 测试：[`test-report.md`](test-report.md)

## 变更结论

`#skillname 需求` 现在会把 Skill 名称、Skill 指令和完整原始问题作为同一轮条目化 user message 发给 Engine；Skill 清单和指令不再进入 system prompt。UI/Queue/History 继续只显示原始输入。

## 五轴审查

| 维度 | 状态 | 评分 | 证据 |
|------|:----:|:----:|------|
| 正确性 | ✅ | A | hash/slash、空需求、后续普通输入、未知 Skill、`#end`、队列与 History 全有测试 |
| 可读性 | ✅ | A | display/model 以 `chatRequest` 明确命名；格式化/解析为小型纯函数 |
| 架构 | ✅ | A | 保持 Application 单一业务入口；未修改 Bridge/DTO/Engine schema；角色边界清晰 |
| 安全性 | ✅ | A | Skill 不提升为 system；非法 envelope 原样保留；无密钥或危险返回模式 |
| 性能 | ✅ | A | 无 Skill 零额外格式；有 Skill 为 O(输入+指令)；队列无重复嵌套 header |
| Go 专项 | ✅ | A | gofmt/build/vet/test 通过；无非测试 `return nil, nil`、无新增全局可变状态、无 range 地址错误 |

## 详设功能打点与代码位置

| # | 详设功能点 | 实现代码段落位置 | 测试位置 | 审查结果 |
|---|------------|------------------|----------|:--------:|
| 1 | system prompt 只含四类系统层 | `application/app.go:62-93` `buildSystemPrompt`；`application/prompt_stack.go:89-125` `Render/systemPromptPriority` | `application/prompt_stack_test.go:7-44`；`application/command_test.go:405-429` | ✅ Skill 名称、描述、指令、需求均被反向断言排除 |
| 2 | `#skillname 需求` 激活并立即发送原文 | `application/app.go:104-129` `Submit`；`application/input.go:44-91` | `application/command_test.go:405-429` | ✅ |
| 3 | slash Skill 别名使用相同契约 | `application/input.go:9-17` `submitCommand` | `application/application_test.go:252-290` | ✅ |
| 4 | 空需求只激活，后续普通输入携带 Skill | `application/input.go:78-84`；`application/app.go:131-148` | `application/command_test.go:431-468` | ✅ |
| 5 | Skill 条目包含名称、指令和完整 User Request | `application/skill_context.go:32-60` | `application/skill_context_test.go:15-37` | ✅ |
| 6 | display/model 双通道隔离 UI 与 Engine | `application/skill_context.go:14-30`；`application/chat.go:13-35` | `application/command_test.go:405-429` | ✅ |
| 7 | 排队时固化 Skill，批量时消除嵌套 envelope | `application/app.go:131-148`；`application/chat.go:60-81`；`application/skill_context.go:75-95` | `application/command_test.go:470-510`；`application/skill_context_test.go:55-75` | ✅ |
| 8 | Chat、resume、分页 History 恢复原始显示 | `application/chat.go:103-119`；`application/app.go:479-491` `adaptEngineMessage` | `application/skill_context_test.go:46-53` | ✅ |
| 9 | `#end` LIFO 且 Goal MaxLoops 正确 | `application/input.go:59-76` | `application/command_test.go:527-594` | ✅ 审查中补修上层退栈边界 |
| 10 | 未知 Skill 不发送问题 | `application/input.go:44-57` | `application/command_test.go:512-525` | ✅ |

## 审查发现与处理

### 已修复

1. Goal Skill 位于栈底时，退栈上层 Skill 会错误恢复 Effort MaxLoops。已改为检查剩余活动 Skill，Goal 存在时保持 9999。
2. Effort layer 清除重加后可能位于 instructions 之后。已让 `Render` 在副本上按固定优先级稳定排序，同时保留 Skill 活动顺序。

### 已知权衡

1. 持续活动 Skill 会在每轮重复发送指令，增加 token；这是“不进入 system prompt”且保持持续 Skill 语义的必要成本，可用 `#end` 停止。
2. v1 envelope 写入 Engine History。旧版本客户端不会解包但仍能加载消息；当前版本覆盖 Chat、resume 和分页三条展示路径。
3. 本地无 CGO 工具链，race 由远端 Ubuntu CI 承接，不作为未验证的“通过”记录。

## 最终判断

- [x] 本地审查通过，可提交并推送
- [ ] 本次远端 CI 通过后完成最终交付闭环
