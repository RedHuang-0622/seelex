# Effort 架构设计与 Skill 系统改造方案

> **状态**: 设计稿（未实现）
> **日期**: 2026-07-20
> **领域**: 应用层 / System Prompt 栈 / WorkPlan 编排
> **来源**: 合并 `effort_dd.md`（引擎层参数控制）与 `skill-effort-architecture.md`（应用层提示词栈）

---

## 1. 背景与问题

### 当前架构缺陷

| 问题 | 位置 | 严重度 |
|------|------|--------|
| `applySkill()` 用 `SetSystemPrompt` 直接替换整个系统提示词，插件 prompt 丢失 | `application/input.go:61` | 🔴 |
| Effort 设置存在于 `settings.json` 但代码从不读取 | `application/` 无消费方 | 🟡 |
| Skill 只能单槽，不能叠加（`#goal` 后 `#code` 覆盖上一技能） | `application/input.go:61` | 🔴 |
| Skill 无退栈机制，加载后一直留在上下文 | 缺 `#end` 命令 | 🟢 |

### effort_dd 的设计前提

effort_dd.md 假设 effort 需要改动 `engine/` 层（loop.go / config.go / engine.go）的代码：
- MaxLoops 根据 effort 在引擎层硬编码（0 / 8 / 25 / 50）
- 压缩阈值、History 窗口、Tracer 级别都在引擎层开关
- 工具可见性通过引擎层过滤

**问题**：这违反了"不改 ReAct 循环"的约束，且引擎层改函数签名影响面大。

### 本方案的核心思路

> **Effort 的控制面在应用层，执行面通过已有 Engine API 下发。**
>
> 不新增 Engine 函数签名，通过 `SetSystemPrompt` + `SetMaxLoops` + Plugin 系统已有能力组合出四种 effort 行为。

---

## 2. 整体架构

```
┌── TUI / Settings ──────────────────────┐
│  /effort high                          │
│  settings.json: effortLevel: "xhigh"   │
└──────────────┬─────────────────────────┘
               │ effort level string
               ▼
┌── Application Layer ─────────────────────────────────────┐
│  EffortManager                                            │
│  ├── 读取 effort → 注入 Effort Prompt 层到 PromptStack   │
│  ├── 下发 engine.SetMaxLoops(n)   (已有 API)              │
│  └── 下发 Plugin 工具可见性         (插件系统已有)         │
│                                                           │
│  PromptStack                                              │
│  ├── base:    plugin.md prompt       ← 始终保留           │
│  ├── effort:  effort 行为指令        ← 可选层             │
│  ├── skill_1: goal SKILL.md          ← #goal 压栈         │
│  └── skill_2: code SKILL.md          ← #code 压栈         │
│                                                           │
│  Render() = join([base, effort, skill_1, skill_2], sep)   │
│           → engine.SetSystemPrompt(Render())              │
└──────────────────────────────────────┬────────────────────┘
               │ 只调已有 Engine API
               ▼
┌── Engine Layer (不改) ───────────────────────────────────┐
│  engine.SetSystemPrompt(string)   ← 始终看到完整拼好的提示词│
│  engine.SetMaxLoops(int)          ← effort 控制循环轮次   │
│  plugin.ActivatePlugin(name)      ← effort 控制工具可见性  │
│  ReActLoop.Run()                  ← 完全一致，不用分支     │
└───────────────────────────────────────────────────────────┘
```

---

## 3. PromptStack 详细设计

### 3.1 结构

```go
// application/prompt_stack.go
type PromptLayer struct {
    Kind string // "base" | "effort" | "skill"
    Name string // plugin name / effort level / skill name
    Text string // prompt 内容
}

type PromptStack struct {
    layers []PromptLayer
}

func NewPromptStack() *PromptStack
func (ps *PromptStack) Push(kind, name, text string)
func (ps *PromptStack) Pop(name string) bool         // 按 name 删除单层
func (ps *PromptStack) PopKind(kind string) string   // 删除最后一个指定 kind 的层，返回其 name
func (ps *PromptStack) ClearKind(kind string)        // 删除所有指定 kind 的层
func (ps *PromptStack) Reset(baseText string)        // 清空所有层，设置 base
func (ps *PromptStack) Render() string               // 所有层用分隔符拼接
func (ps *PromptStack) Has(kind string) bool
func (ps *PromptStack) Layers() []PromptLayer        // TUI 显示用
```

### 3.2 分层规则

```
Layer order (从底到顶):
  [base]          ← 插件激活时设置, 始终存在
  [effort]        ← /effort 设置, 最多一个, 可更新
  [skill_1]       ← #skillname 压栈
  [skill_2]       ← 再 #skillname 再压
  ...

Render() 拼接:
  base_text
  + "\n\n---\n\n" + effort_text   (if effort layer exists)
  + "\n\n---\n\n" + skill_1_text  (if skill_1 exists)
  + "\n\n---\n\n" + skill_2_text  (if skill_2 exists)
```

### 3.3 Effort 作为 Prompt 层

Effort 不控制引擎参数（不碰 MaxLoops），而是通过 **注入行为指令到 system prompt** 来影响 LLM：

```
Effort=low  → 不注入 effort 层
    PromptStack = [plugin.md]

Effort=medium → 注入 medium 指令:
    "You are in medium-effort mode. Keep responses concise. 
     Use tools only when necessary. No retry on tool failure."
    PromptStack = [plugin.md] + [effort: medium]

Effort=high → 注入 high 指令:
    "You are in high-effort mode. For multi-step tasks, use 
     plan_load/plan_run to create structured plans. Retry with 
     auto-fix on tool failure. Verify results after each change."
    PromptStack = [plugin.md] + [effort: high]

Effort=max → 注入 max 指令:
    "You are in max-effort mode. Always plan before acting using 
     WorkPlan. Use Fork for parallel sub-agents. Cross-verify 
     results. Use worktrees for isolation. Retry up to 5 times."
    PromptStack = [plugin.md] + [effort: max]
```

### 3.4 Effort 的辅助控制（通过已有 Engine API）

Effort 除了注入 prompt 层外，还通过 Engine 已有的 `SetMaxLoops` 做辅助控制：

```go
func (m *EffortManager) Apply(level string) {
    m.promptStack.ClearKind("effort")
    
    switch level {
    case "low":
        m.engine.SetMaxLoops(0)        // 已有 API
        // 不注入 effort 层
    case "medium":
        m.engine.SetMaxLoops(8)
        m.promptStack.Push("effort", "medium", mediumPrompt)
    case "high":
        m.engine.SetMaxLoops(25)
        m.promptStack.Push("effort", "high", highPrompt)
    case "max":
        m.engine.SetMaxLoops(50)
        m.promptStack.Push("effort", "max", maxPrompt)
    }
    m.engine.SetSystemPrompt(m.promptStack.Render())
}
```

**不需要改动 `engine/loop.go`** —— `SetMaxLoops` 在 `engine/engine.go:161-170` 已存在。

---

## 4. 四档 Effort 对比

### 4.1 对比表

| 维度 | Low | Medium | High (默认) | Max |
|------|-----|--------|-------------|-----|
| **Effort Prompt 层** | 无 | 简洁指令 | 完整行为指导 | 多 agent 编排 |
| **MaxLoops** | 0 | 8 | 25 | 50 |
| **工具箱** | 只读 + 时间 | default 插件 | 全部（可 switch） | 全部 + WorkPlan |
| **Retry 策略** | 0 次 | 1 次 | 3 次 | 5 次 + 降级 |
| **Skill 可用** | ✅ | ✅ | ✅ | ✅ |
| **PromptStack** | 完整支持 | 完整支持 | 完整支持 | 完整支持 |

### 4.2 典型 PromptStack 渲染效果

**Effort=high + #goal + #code**：
```
[plugin.md body]
"使用全部已注册工具与全局 Skill。"

---

"You are in high-effort mode. For multi-step tasks, use 
 plan_load/plan_run to create structured plans. Retry with 
 auto-fix on tool failure. Verify results after each change."

---

[goal SKILL.md content]
"# GOAL 方法论 + A2A 子代理调度..."

---

[code SKILL.md content]
"# 代码实现
 你是一个精通 Go/Python 的软件工程师..."
```

**Effort=low + 无 skill**：
```
[plugin.md body]
"使用全部已注册工具与全局 Skill。"
```

---

## 5. 实现方案

### 5.1 文件清单

| 文件 | 操作 | 改动量 |
|------|------|--------|
| `application/prompt_stack.go` | **新增** | ~70 行 |
| `application/effort.go` | **新增** | ~60 行 |
| `application/input.go` | 改 `applySkill` 和 `submitCommand` | ~10 行 |
| `application/app.go` | 插件激活时调用 `promptStack.Reset()` | ~5 行 |
| `application/command.go` | 新增 `/effort` 和 `#end` 命令 | ~20 行 |
| `application/service.go` | Service 结构体加 `promptStack` 和 `effortManager` 字段 | ~5 行 |
| **engine/ 下任一文件** | **不改** | 0 行 |

### 5.2 核心改动细节

#### 5.2.1 applySkill 改为 Push（替换 SetSystemPrompt）

```go
// input.go — 改前
func (service *Service) applySkill(skill SkillInfo, args []string) error {
    service.deps.Engine.SetSystemPrompt(skill.Prompt)  // 🔴 替换
    ...
}

// input.go — 改后
func (service *Service) applySkill(skill SkillInfo, args []string) error {
    prompt := skill.Prompt
    if len(args) > 0 {
        prompt += "\n\n" + strings.Join(args, " ")
    }
    service.promptStack.Push("skill", skill.Name, prompt)
    service.deps.Engine.SetSystemPrompt(service.promptStack.Render())
    service.addNotice("加载 Skill: " + skill.Name)
    return nil
}
```

#### 5.2.2 /command 也走技能查找

```go
// input.go — submitCommand 已有技能路由, 不需要改
func (service *Service) submitCommand(ctx context.Context, input string) error {
    if skill, ok := service.deps.Skills.Get(parts[0]); ok {
        return service.applySkill(skill, parts[1:])  // ✅ 已有
    }
    ...
}
```

#### 5.2.3 插件激活时重置栈

```go
// app.go — SwitchPlugin / activateDefaultPlugin
func (service *Service) SwitchPlugin(ctx context.Context, name string) error {
    ...
    if current, ok := service.deps.Plugins.Current(); ok {
        service.promptStack.Reset(strings.TrimSpace(current.Prompt))  // 清空 + 设 base
        applyEffortLayer(service)  // 如果有 effort 层, 重新压回去
        service.deps.Engine.SetSystemPrompt(service.promptStack.Render())
    }
    ...
}
```

#### 5.2.4 #end 退栈

```go
// command.go
service.commands.Register("end", "退栈最近一次的 Skill 加载", func(...) {
    name := service.promptStack.PopKind("skill")
    if name == "" {
        return "当前无 Skill 可退栈"
    }
    service.deps.Engine.SetSystemPrompt(service.promptStack.Render())
    return "已退栈 Skill: " + name
})
```

#### 5.2.5 /effort 命令

```go
// command.go
service.commands.Register("effort", "切换 Effort 等级: low / medium / high / max", func(ctx context.Context, args []string) {
    if len(args) == 0 {
        return "当前 Effort: " + service.effortManager.Current()
    }
    level := args[0]
    service.effortManager.Apply(level)
    return "Effort 已切换为: " + level
})
```

### 5.3 Effort 读取链

```
启动时:
  C:\Users\redre\.claude\settings.json
    → "effortLevel": "xhigh"
    → application.Service 初始化时读取
    → effortManager.Apply("xhigh")   (xhigh 映射到 max)

运行时:
  TUI 状态栏显示 [E:Max]
  /effort high  → effortManager.Apply("high")
  Ctrl+Shift+E  → 循环切换 (未来)
```

### 5.4 Effort 提示词映射表

```go
// application/effort.go
var effortPrompts = map[string]string{
    "low": `You are in low-effort mode.
- Answer directly. Do not use tools unless explicitly asked.
- No retry on tool failure. Report the error and move on.
- Keep responses concise. Skip analysis and planning.`,
    
    "medium": `You are in medium-effort mode.
- Keep responses concise. Use tools only when necessary.
- Retry once on tool failure.
- For multi-step tasks, briefly outline your approach first.`,
    
    "high": `You are in high-effort mode.
- For multi-step tasks, use plan_load to define a plan, then plan_run.
- On tool failure, attempt auto-fix and retry up to 3 times.
- Verify results after each change (compile/test).
- Use ask_approve for destructive operations.
- You can switch plugins via switch_plugin when needed.`,
    
    "max": `You are in max-effort mode.
- Always plan before acting. Use WorkPlan for complex tasks.
- Use Fork for parallel sub-agents when tasks are independent.
- On tool failure, retry with alternative approach up to 5 times.
- Cross-verify results with multiple methods.
- Use worktrees for isolated experiments.
- Record key decisions and findings for review.`,
}
```

### 5.5 Holder 层 Retry 策略（辅助改动）

当前 `holder.go` 的 retry 固定为 3 次。Effort 应能影响此值：

```go
// seelebridge/runtime.go — 新增方法
func (r *Runtime) ApplyEffort(level string) {
    retries := map[string]int{"low": 0, "medium": 1, "high": 3, "max": 5}
    r.agent.Tools().SetMaxRetries(retries[level])  // 需要在 holder 暴露此方法
}
```

**如果不想改 holder，也可以完全依赖 prompt 让 LLM 自决定重试次数**——低 effort 的 prompt 告诉它"不要重试"，高 effort 告诉它"重试 3 次"。这不精确但无需改 engine。

---

## 6. 状态栏显示

### 6.1 新增 Effort 状态

```
改前:  round-robin  tok:1234  3s
          ↑plugin     ↑tokens   ↑耗时

改后:  E:Max  round-robin  tok:1234  3s
          ↑effort ↑plugin     ↑tokens   ↑耗时
```

### 6.2 实现

```go
// tui/view.go — 状态栏渲染
func effortBadge(level string) string {
    colors := map[string]lipgloss.Color{
        "low":    lipgloss.Color("241"),  // 灰
        "medium": lipgloss.Color("220"),  // 金
        "high":   lipgloss.Color("75"),   // 蓝
        "max":    lipgloss.Color("198"),  // 紫红
    }
    c := colors[level]
    if c == 0 { c = lipgloss.Color("241") }
    return lipgloss.NewStyle().Foreground(c).Render("E:" + level)
}
```

---

## 7. 与 effort_dd.md 的关键差异

| 维度 | effort_dd.md (原方案) | 本方案 (合成) |
|------|----------------------|--------------|
| Effort 在哪层生效 | `engine/` (loop.go / config.go) | `application/` (prompt stack + 已有 engine API) |
| 控制手段 | 硬参数：MaxLoops / 压缩阈值 / 工具过滤 | 软注入：system prompt 分层 + SetMaxLoops + 插件系统 |
| 是否改引擎 | ✅ 改 loop.go / config.go / engine.go 函数签名 | ❌ 不动 engine/ |
| Skill 分层 | 没涉及 | PromptStack 核心能力 |
| Effort 切换力 | Lite 不允许工具/Skill 任何能力 | 低 effort 也有完整 PromptStack 支持 |
| 向后兼容 | Medium 以下改默认行为 | Max 作为默认，等价于当前行为 |
| Ultra 级别 | 独立的 runUltra() 方法 | Max + 更激进的 prompt（无新代码路径） |
| TUI 集成 | 简略 | 状态栏 + 快捷键 + /effort 命令完整 |
| 实施量 | 多个 engine 文件修改 | ~170 行纯新增，0 行 engine 改动 |

---

## 8. 验证方案

### 8.1 单元测试

```go
func TestPromptStack(t *testing.T) {
    ps := NewPromptStack()
    ps.Push("base", "default", "base prompt")
    assert.Equal("base prompt", ps.Render())

    ps.Push("effort", "high", "high instructions")
    assert.Contains(ps.Render(), "high instructions")
    assert.Contains(ps.Render(), "base prompt")

    ps.Push("skill", "goal", "goal prompt")
    assert.Contains(ps.Render(), "goal prompt")

    ps.Pop("goal")
    assert.NotContains(ps.Render(), "goal prompt")
    assert.Contains(ps.Render(), "high instructions")

    ps.Reset("new base")
    assert.Equal("new base", ps.Render())
}
```

```go
func TestEffortManager(t *testing.T) {
    em := NewEffortManager(ps, engine)
    em.Apply("high")
    assert.True(ps.Has("effort"))
    assert.Equal(25, engine.MaxLoops())

    em.Apply("low")
    assert.False(ps.Has("effort"))
    assert.Equal(0, engine.MaxLoops())
}
```

### 8.2 集成验证

| 步骤 | 预期 |
|------|------|
| 1. 启动 Seelex → 激活 default 插件 | system prompt = plugin.md body |
| 2. `#goal` | system prompt = plugin.md + goal 内容 |
| 3. `#code` | system prompt = plugin.md + goal + code |
| 4. `#end` | system prompt = plugin.md + goal 恢复 |
| 5. `/effort high` | system prompt = plugin.md + high 指令 + goal |
| 6. 切换插件 | PromptStack 重建，base 换为新 plugin.md |
| 7. `SetMaxLoops(0)` 生效 | 低 effort 下 LLM 不调工具 |

---

## 9. 实施路线

| 阶段 | 文件 | 内容 | 估算 |
|------|------|------|------|
| P1 | `application/prompt_stack.go` | PromptStack 实现 + 测试 | ~30min |
| P2 | `application/effort.go` | EffortManager + prompt 映射表 | ~20min |
| P3 | `application/input.go` + `app.go` | applySkill 改为 Push + 插件重置栈 | ~15min |
| P4 | `application/command.go` | /effort + #end 命令 | ~15min |
| P5 | `tui/view.go` | 状态栏 Effort 徽标 | ~10min |
| **总计** | — | **不改 engine/** | **~90min** |
