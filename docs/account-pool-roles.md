# 号池角色分组方案 v3

> 状态：讨论稿  
> 日期：2026-07-26

## 1. v2 的问题

v2 的 Think/Move/Scan 三级制有一个致命缺陷：**上下文切换**。

当 Agent 在 Think 模型下聊了 50 轮，然后遇到跨文件操作切换到 Scan 模型——Scan 模型没有前 50 轮的历史。要么截断上下文（丢信息），要么把全部历史塞进新模型（超窗口 + 高成本）。

**结论：不在会话中途切换模型。角色的分配时机是任务启动时，而非运行时。**

## 2. 用户对大模型的需求维度

调研后的三个独立维度：

| 维度 | 问题 | 典型场景 |
|------|------|---------|
| **深度** | 这个任务需要多强的推理？ | 简单问答 vs 架构设计 vs 代码审查 |
| **广度** | 这个任务涉及多少上下文？ | 单文件修改 vs 跨模块重构 vs 全仓库分析 |
| **并行度** | 这个任务有多少独立子任务？ | 单步执行 vs `plan_run` Fork 子代理 |

三个维度**正交**——一个任务可以同时要求高深度、大广度、高并行度。不能用一个"等级"概括。

## 3. Seelex 三级任务角色

按**任务启动时的类型**分配模型，而非运行时切换：

| 角色 | 启动条件 | 模型要求 | 上下文 | 成本 |
|------|---------|---------|--------|------|
| **SubAgent** | `plan_run` Fork 子节点 | 快、便宜、够用即可 | 子任务隔离上下文（只传 node input） | 低 |
| **Agent** | 默认对话 | 平衡推理/速度/成本 | 当前会话历史 | 中 |
| **GoalPlan** | `#goal` 激活 或 `/plan` 命令 | 深度推理 + 大窗口 | 全量上下文（不截断） | 高 |

### 3.1 为什么这样分

**SubAgent（子代理）**

Fork 子代理天然有**上下文隔离**——每个子节点只接收 `node.input`（一段 Prompt），不需要父会话的完整历史。所以子代理可以用小模型、低上下文窗口，成本低、速度快。

- 不需要大窗口（node input 通常 < 10K tokens）
- 不需要强推理（明确的任务描述 + 工具调用）
- 需要低延迟（用户等待所有子节点完成）

**Agent（默认）**

用户日常对话——问问题、改 bug、写功能。需要平衡的推理能力和上下文窗口。

- 默认走这个
- 用户手动 `SelectAccount` 可以覆盖

**GoalPlan（目标规划）**

当用户说 `#goal 重构整个认证模块` 或用 `/plan` 启动 WorkPlan——这时候需要：
- 深度推理（理解复杂需求、设计 DAG）
- 大上下文（阅读大量相关文件）
- 不在意成本（goal 任务是用户明确的"大活儿"）

`#goal` Skill 已经设了 `SetMaxLoops(9999)`——GoalPlan 角色的账号也同样要配大窗口 + 强推理。

### 3.2 上下文承接

关键设计：**同一角色内不切换模型**。GoalPlan 从 `plan_load` 到 `plan_run` 的**整个生命周期**用同一个账号。

```
#goal 重构认证模块
  │
  ├─ plan_load  → GoalPlan 账号 (Claude Opus 200K)
  │    读取 15 个相关文件（大上下文）
  │    设计 6 节点 DAG
  │
  ├─ plan_run
  │   ├─ node-1 (分析)     → SubAgent (Haiku, 独立上下文)
  │   ├─ node-2 (重构)     → SubAgent (Haiku, 独立上下文)
  │   ├─ node-3 (测试)     → SubAgent (Haiku, 独立上下文)
  │   └─ node-4 (审查)     → GoalPlan 账号 (需要完整上下文来审查)
  │
  └─ 结果汇总              → GoalPlan 账号 (一直持有完整上下文)
```

有 `_` 前缀的节点在 GoalPlan 上下文中执行（持有完整历史），普通节点 Fork 到 SubAgent（隔离上下文）。

## 4. 账号配置

```yaml
# accounts.yaml
accounts:
  # ── SubAgent：快速执行，便宜 ──
  - name: haiku
    provider: anthropic
    model: claude-haiku-4-5-20251001
    base_url: https://api.anthropic.com/v1
    api_key: sk-ant-xxx
    role: subagent

  - name: deepseek-flash
    provider: deepseek
    model: deepseek-v4-flash
    base_url: https://api.deepseek.com
    api_key: sk-xxx
    role: subagent

  # ── Agent：默认，均衡 ──
  - name: deepseek-pro
    provider: deepseek
    model: deepseek-v4-pro
    base_url: https://api.deepseek.com
    api_key: sk-xxx
    role: agent

  # ── GoalPlan：深度推理 + 大窗口 ──
  - name: claude-opus
    provider: anthropic
    model: claude-opus-4-20250514
    base_url: https://api.anthropic.com/v1
    api_key: sk-ant-xxx
    role: goalplan
```

### 4.1 回退策略

```
GoalPlan 没配账号 → 用 Agent 账号（可能窗口不够，但不静默降级）
Agent    没配账号 → 用 priority 最高的任意账号
SubAgent 没配账号 → 用 Agent 账号（比没有好）
```

### 4.2 用户手动切换

用户通过 `SelectAccount` 手动选账号后，**当前会话后续所有 Chat 都用所选账号**。这会覆盖角色路由——因为用户明确说了"我要用这个模型"。

## 5. 与现有架构的关系

| 现有能力 | 影响 |
|---------|------|
| `#goal` Skill | `#goal` 激活时自动路由到 GoalPlan 账号 |
| `plan_run` Fork | 子节点自动用 SubAgent 账号 |
| `SelectAccount` | 手动选择后覆盖所有角色路由 |
| `EffortManager` | Effort 只影响行为 Prompt 和 MaxLoops，不影响账号选择 |
| Plugin 切换 | 不触发账号切换 |

## 6. 实现拆解

| 阶段 | 内容 | 文件 |
|------|------|------|
| Phase 1 | `accounts.yaml` 加 `role: subagent\|agent\|goalplan` | `seelebridge/runtime.go`, `AccountInfo` |
| Phase 2 | `RoleRouter.Resolve(ctx)` — 根据会话状态选 role | 新增 `seelebridge/router.go` |
| Phase 3 | `#goal` 激活 → GoalPlan 账号 | `application/input.go` |
| Phase 4 | `plan_run` Fork → SubAgent 账号 | `application/chat.go` `handleToolStart` |
| Phase 5 | 同角色回退 + 日志 | router |

## 7. 不做什么

- **不自动检测模型能力**：不通过 API 推断窗口大小或推理能力。由用户配置 role。
- **不在单次 Chat 中切换模型**：plan_load 用 GoalPlan → individual node fork 用 SubAgent 是独立的 Chat 会话。
- **不限制一个账号只能一个 role**：一个强力模型可以同时配 `agent` 和 `goalplan`。
