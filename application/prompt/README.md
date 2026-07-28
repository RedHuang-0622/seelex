# Application Prompt

## 定位

本包管理 system prompt 的分层组合和 Effort 行为策略，为 `core.Service` 提供线程安全、可解释的 prompt 状态。

## PromptStack

`PromptLayer` 以 `kind + name + text` 标识一层。固定 system 顺序为 identity、base/plugin、effort、instructions；Skill 请求内容不永久写入 system prompt，而随用户 turn 进入结构化上下文。

主要操作：

- `Push`/`Pop`/`PopKind`/`ClearKind`：维护层。
- `Reset`：重置基础 prompt。
- `Render`：按固定顺序生成最终 system prompt。
- `Layers`/`Describe`：供输入冻结和 UI 诊断。

## EffortManager

`lite`、`medium`、`high`、`max` 映射到行为提示和 Engine MaxLoops。`Apply` 同时更新 PromptStack 与 Engine；`Cycle` 为前端提供顺序切换。

## Review 指南

- 不要让 map 迭代顺序影响 prompt；渲染顺序必须确定。
- Effort 更新应同时刷新 max loops 和最终 system prompt。
- Skill prompt 与 system layers 的边界不能混淆。
- 所有读写都经过 mutex，返回 slice 时需复制。

## 测试

```text
go test ./application/prompt -count=1
go test ./application/core -run 'Effort|Skill' -count=1
```
