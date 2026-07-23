# Skill 用户上下文实现方案

## 目标

1. `#skillname 需求` 激活 Skill 并立即发送需求。
2. Engine 用户输入同时包含活动 Skill 的名称、指令和完整原始问题。
3. Skill layer 不参与 system prompt 渲染，可用 Skill 清单也不进入 system prompt。
4. UI/Session 展示原始输入，不展示内部 Skill envelope。
5. 运行中排队时在提交时固化 Skill 上下文，避免后续 `#end`/切换影响已排队请求。

## 接口与数据结构

- `PromptStack.Render()`：只渲染非 Skill layer；`Layers()` 继续提供活动 Skill 副本。
- `formatSkillUserInput(layers, input)`：纯函数，把 Skill layer 与原始输入编码为版本化 Markdown envelope。
- `displayUserInput(input)`：纯函数，只对自有版本 marker 解包，供 Engine History → UI 转换。
- `Service.submitConversation(ctx, displayInput, modelInput)`：统一普通输入、`#skill` 和 slash Skill 别名的 Chat/queue 入口。
- `Service.startChat(ctx, displayInput, modelInput)`：UI 追加 display，Engine 收到 model。
- `Service.inputQueue []chatRequest`：每个排队项同时持有 display/model，两者同锁、同生命周期，不存在长度漂移。

## 实现步骤

| # | 步骤 | 验收 |
|---|------|------|
| 1 | PromptStack Render 排除 Skill | system prompt 无 Skill 指令，Describe/PopKind 仍可见 |
| 2 | 新增 envelope 编解码纯函数 | 多 Skill、换行缩进、普通输入、伪 marker 边界测试 |
| 3 | Chat display/model 双输入 | 流式 UI 显示原文，Engine History 重建后仍显示原文 |
| 4 | 双队列 | Snapshot 只暴露 display；Engine 批量收到提交时固化的 model input |
| 5 | 调整 #/slash Skill 路由 | 有需求立即 Chat，无需求只激活，未知/#end 不 Chat |
| 6 | 更新 system capabilities | 删除 Available Skills 条目，只保留通用协议说明 |
| 7 | 完整验证与审查 | build/vet/application tests/full tests/文档同步 |

## 测试策略

- 单元：PromptStack system 隔离；envelope format/strip；多 Skill 顺序。
- 集成：`#review focused` 的 Engine input、system prompt、UI Conversation、notice。
- 边界：空需求、未知 Skill、`#end`、slash 别名、后续普通问题。
- 队列：display/model 双队列长度与批量输入；Skill 在排队后退栈不改变已固化上下文。
- 并发：现有 Submit/InputQueue race tests 继续通过；远端 Ubuntu race 为最终门禁。

## 回滚

恢复 `PromptStack.Render` 对 Skill 的渲染、移除 model queue 和 envelope 函数，并让 `#skill` 重新只调用 `applySkill` 即可回滚；不涉及持久化 schema 迁移。
