# Effort 系统完整设计方案

> **状态**: 历史设计稿（Effort Provider 扩展尚未实现；Skill 传输以 [`skill-effort-architecture.md`](skill-effort-architecture.md) 为准）
> **日期**: 2026-07-20
> **领域**: 配置层 / Provider 策略层 / 应用层 / 提示词组装
> **涉及改动**: `types/model.go` → `api/strategy.go` → `api/client.go` → `application/` → `seelebridge/`

---

## 1. 数据流总览

```
settings.json / YAML 配置
    │ effortLevel, 模型映射
    ▼
types.LLMConfig (新字段: ReasoningEffort, ThinkingBudget)
    │
    ▼
api.ChatClient.requestOpts() → RequestOptions (新字段: ReasoningEffort, ThinkingBudget)
    │
    ▼
ProviderStrategy.BuildRequest() → JSON body
    ├── Anthropic:   thinking.type + thinking.budget_tokens
    ├── OpenAI:      reasoning_effort
    └── DeepSeek:    thinking.type + thinking.budget_tokens (Anthropic 兼容)
    │
    ▼
application.EffortManager (应用层)
    ├── 读取 effort → 注入 Effort Prompt 层
    ├── 读取 effort → 选择模型（flash / pro）
    └── 读取 effort → 选择 Planning 策略
```

---

## 2. 配置层改动

### 2.1 types.LLMConfig 增加 Effort 字段

```go
// types/model.go

// LLMConfig 对应 YAML 配置的 agent 块或账号级配置。
type LLMConfig struct {
    BaseURL     string  `yaml:"ai_url"`
    APIKey      string  `yaml:"ai_api_key"`
    Model       string  `yaml:"ai_name"`
    MaxTokens   int     `yaml:"max_tokens"`
    Timeout     int     `yaml:"timeout"`
    Temperature float64 `yaml:"temperature"`

    // ── 新增 ──────────────────────────────────────────────

    // ReasoningEffort 控制模型的思考深度。
    // 有效值: "low" | "medium" | "high" | "max"
    // 空值 = 不设置（模型默认行为）
    // Provider 映射:
    //   Anthropic → thinking.type + thinking.budget_tokens
    //   OpenAI    → reasoning_effort
    //   DeepSeek  → thinking.type + thinking.budget_tokens
    ReasoningEffort string `yaml:"reasoning_effort,omitempty"`

    // ThinkingBudget 思考 token 预算（仅 Anthropic/DeepSeek）。
    // 0 = 使用 provider 默认值。
    // 各 effort 推荐值:
    //   low=1024, medium=4096, high=8192, max=16384
    ThinkingBudget int `yaml:"thinking_budget,omitempty"`
}
```

### 2.2 账号级配置支持 Effort

```yaml
# config/account.yaml
accounts:
  - name: deepseek-main
    model: deepseek-v4-flash
    priority: 1
    reasoning_effort: low       # ← 新增

  - name: deepseek-pro
    model: deepseek-v4-pro
    priority: 2
    reasoning_effort: high      # ← 新增
```

### 2.3 全局 Effort 配置

```yaml
# config/account.yaml 顶层
defaults:
  reasoning_effort: high
  thinking_budget: 8192
  # Effort → 模型映射（覆盖 seelebridge 的默认选择逻辑）
  effort_model_map:
    low:    deepseek-v4-flash
    medium: deepseek-v4-flash
    high:   deepseek-v4-pro
    max:    deepseek-v4-pro
```

### 2.4 settings.json 读取链

```go
// main.go 启动时
// 优先级: 命令行 --effort > 环境变量 > settings.json > YAML 默认
effortLevel := flag.Lookup("effort")
if effortLevel == "" {
    effortLevel = os.Getenv("CLAUDE_CODE_EFFORT_LEVEL")
}
if effortLevel == "" {
    // 从 C:\Users\redre\.claude\settings.json 读取 effortLevel
}
// 映射: "" → "high" (默认)
```

---

## 3. 传输层改动（ProviderStrategy）

### 3.1 RequestOptions 新增 Effort 字段

```go
// api/strategy.go
type RequestOptions struct {
    MaxTokens        int
    Temperature      float64
    ReasoningEffort  string // "low" | "medium" | "high" | "max" | ""
    ThinkingBudget   int    // 0 = provider default
}
```

### 3.2 requestOpts 传递 Effort

```go
// api/client.go
func requestOpts(cfg types.LLMConfig, acct *Account) RequestOptions {
    opts := RequestOptions{
        MaxTokens:        cfg.MaxTokens,
        Temperature:      cfg.Temperature,
        ReasoningEffort:  cfg.ReasoningEffort,  // ← 新增
        ThinkingBudget:   cfg.ThinkingBudget,    // ← 新增
    }
    if acct != nil {
        if acct.MaxTokens > 0    { opts.MaxTokens = acct.MaxTokens }
        if acct.Temperature > 0  { opts.Temperature = acct.Temperature }
        if acct.ReasoningEffort != "" { opts.ReasoningEffort = acct.ReasoningEffort } // ← 新增
    }
    return opts
}
```

### 3.3 AnthropicStrategy.BuildRequest 添加 thinking

```go
// strategy_anthropic.go
type anthropicRequest struct {
    Model       string             `json:"model"`
    Messages    []anthropicMessage `json:"messages"`
    MaxTokens   int                `json:"max_tokens"`
    System      string             `json:"system,omitempty"`
    Stream      bool               `json:"stream,omitempty"`
    Temperature float64            `json:"temperature,omitempty"`
    Tools       json.RawMessage    `json:"tools,omitempty"`
    ToolChoice  json.RawMessage    `json:"tool_choice,omitempty"`
    Thinking    *thinkingConfig    `json:"thinking,omitempty"` // ← 新增
}

type thinkingConfig struct {
    Type         string `json:"type"`                    // "enabled" | "disabled"
    BudgetTokens int    `json:"budget_tokens,omitempty"` // 思考 token 预算
}

// BuildRequest 中追加 thinking 块
func (s *AnthropicStrategy) BuildRequest(...) ([]byte, error) {
    // ... 现有逻辑 ...
    
    req := anthropicRequest{...}
    
    // ── 新增: Effort → Thinking ──
    if opts.ReasoningEffort != "" {
        budget := opts.ThinkingBudget
        if budget <= 0 {
            budget = effortThinkingBudget(opts.ReasoningEffort) // 映射表
        }
        req.Thinking = &thinkingConfig{
            Type:         "enabled",
            BudgetTokens: budget,
        }
        // 启用 thinking 时 MaxTokens 必须大于 BudgetTokens
        if req.MaxTokens <= budget {
            req.MaxTokens = budget + 4096
        }
    }
    
    return json.Marshal(req)
}

func effortThinkingBudget(level string) int {
    switch level {
    case "low":    return 1024
    case "medium": return 4096
    case "high":   return 8192
    case "max":    return 16384
    default:       return 4096
    }
}
```

### 3.4 OpenAIStrategy.BuildRequest 添加 reasoning_effort

```go
// strategy_openai.go
type openaiCompletionRequest struct {
    Model            string          `json:"model"`
    Messages         []types.Message `json:"messages"`
    Tools            []types.Tool    `json:"tools,omitempty"`
    MaxTokens        int             `json:"max_tokens,omitempty"`
    Temperature      float64         `json:"temperature,omitempty"`
    ReasoningEffort  string          `json:"reasoning_effort,omitempty"` // ← 新增
}

type openaiStreamRequest struct {
    // ... 同样加 ReasoningEffort ...
}

func (s *OpenAIStrategy) BuildRequest(...) ([]byte, error) {
    if opts.ReasoningEffort != "" {
        if stream {
            req.ReasoningEffort = opts.ReasoningEffort
        } else {
            req.ReasoningEffort = opts.ReasoningEffort
        }
    }
    // ...
}
```

---

## 4. 应用层改动

### 4.1 Effort 驱动模型选择

```go
// application/effort.go — 新增方法

// EffortModelMap 定义 effort → 模型名的映射。
// 从配置加载，有默认值。
var defaultEffortModelMap = map[string]string{
    "low":    "deepseek-v4-flash",
    "medium": "deepseek-v4-flash",
    "high":   "deepseek-v4-pro",
    "max":    "deepseek-v4-pro",
}

// SelectModel 根据 effort 返回对应模型。
// 如果映射表中没有该 effort 等级，返回当前模型不变。
func (m *EffortManager) SelectModel(effort string, currentModel string) string {
    if model, ok := m.modelMap[effort]; ok && model != "" {
        return model
    }
    return currentModel
}

// ApplyModel 如果 effort 映射到不同模型，切换账号/模型。
func (m *EffortManager) ApplyModel(effort string) {
    target := m.SelectModel(effort, m.currentModel)
    if target != m.currentModel && target != "" {
        // 调用 seelebridge.SelectAccount 切换账号
        // 或通过 client.SetProvider 切换
        m.modelSwitcher(target)
        m.currentModel = target
    }
}
```

### 4.2 Effort 驱动 Planning 策略

```go
// application/effort.go — Planning 策略

// PlanningStrategy 定义 effort 对应的计划风格。
type PlanningStrategy struct {
    AutoPlan   bool   // 是否自动提示使用 plan_load
    MaxSteps   int    // 计划的最大步骤数
    Style      string // "none" | "brief" | "structured" | "dag"
    SubAgents  bool   // 是否启用子 agent
}

var effortPlanning = map[string]PlanningStrategy{
    "low":    {AutoPlan: false, MaxSteps: 0, Style: "none",       SubAgents: false},
    "medium": {AutoPlan: false, MaxSteps: 3, Style: "brief",      SubAgents: false},
    "high":   {AutoPlan: true,  MaxSteps: 8, Style: "structured", SubAgents: false},
    "max":    {AutoPlan: true,  MaxSteps: 0, Style: "dag",        SubAgents: true},
}

// PlanningPrompt 返回 effort 对应的规划指令片段。
func (ps PlanningStrategy) Prompt() string {
    switch ps.Style {
    case "none":
        return ""
    case "brief":
        return "Briefly outline your approach before coding."
    case "structured":
        return "For complex tasks, use plan_load/plan_run. Structure your work into clear steps."
    case "dag":
        return "Always plan before acting. Use WorkPlan DAG for complex tasks. " +
               "Use Fork for parallel sub-agents when tasks are independent."
    }
    return ""
}
```

---

## 5. 提示词组装架构重构

### 5.1 提示词数据结构

```go
// application/prompt_assembly.go

// PromptPart 是系统提示词的一个组成部分。
type PromptPart struct {
    Kind    string // "identity" | "effort" | "plugins" | "skill" | "instructions"
    Content string
    Priority int   // 越小越靠近栈底
}

// PromptAssembly 负责组装最终的系统提示词。
// 替代当前的 PromptStack 的简单拼接。
type PromptAssembly struct {
    parts []PromptPart
}

// Assemble 按 priority 排序所有 part，用分隔符拼接。
// 固定顺序: identity → effort → plugins → instructions
//
// identity:     "You are Seelex, an intelligent coding assistant."
// effort:       effort 行为指令 + planning 策略 + 模型能力说明
// plugins:      当前插件描述 + 工具能力说明
// instructions: 插件和 Skill 上下文协议说明（始终存在）
// Skill 指令不属于 PromptAssembly；它随 user message 发送。
func (pa *PromptAssembly) Assemble() string {
    sort(pa.parts, by priority)
    // 固定层序渲染
}
```

### 5.2 提示词最终结构

```
─────────────────────────────────────────
[Identity] (固定)
  "You are Seelex, an intelligent coding assistant with plugin and skill system."
─────────────────────────────────────────
[Effort] (根据 /effort 动态变化)
  行为指令:
    "You are in high-effort mode. For multi-step tasks, use plan_load..."
  Planning 策略:
    "For complex tasks, use plan_load/plan_run..."
  （注：Effort 部分不重复说明可切换性）
─────────────────────────────────────────
[Plugins]（当前激活的 plugin.md body）
  "使用全部已注册工具与全局 Skill。"

  工具集:
    - grep_search: 搜索文件内容
    - read_file: 读取文件
    - plan_load: 定义 WorkPlan DAG
    - ...
─────────────────────────────────────────
[Instructions]（固定，说明切换能力）
  "## System Capabilities
   - You can switch plugins via switch_plugin tool.
   - Available plugins: default, read, write, plan
   - User-selected skills arrive as structured entries in user messages.
   - Current effort level: high"
─────────────────────────────────────────
```

### 5.3 PromptAssembly 实现

```go
// application/prompt_assembly.go

type PromptAssembly struct {
    mu     sync.Mutex
    parts  []PromptPart
}

func NewPromptAssembly() *PromptAssembly {
    return &PromptAssembly{parts: make([]PromptPart, 0, 8)}
}

const (
    PriorityIdentity     = 10
    PriorityEffort       = 20
    PriorityPlugins      = 30
    PriorityInstructions = 40
    PrioritySkill        = 50
)

func (pa *PromptAssembly) Set(part PromptPart) {
    pa.mu.Lock()
    defer pa.mu.Unlock()
    // 替换同 kind 的现有 part，否则追加
    for i, p := range pa.parts {
        if p.Kind == part.Kind {
            pa.parts[i] = part
            return
        }
    }
    pa.parts = append(pa.parts, part)
}

func (pa *PromptAssembly) Remove(kind string) {
    pa.mu.Lock()
    defer pa.mu.Unlock()
    filtered := make([]PromptPart, 0, len(pa.parts))
    for _, p := range pa.parts {
        if p.Kind != kind {
            filtered = append(filtered, p)
        }
    }
    pa.parts = filtered
}

func (pa *PromptAssembly) Assemble() string {
    pa.mu.Lock()
    sorted := append([]PromptPart(nil), pa.parts...)
    pa.mu.Unlock()
    
    sort.Slice(sorted, func(i, j int) bool {
        return sorted[i].Priority < sorted[j].Priority
    })
    
    parts := make([]string, 0, len(sorted))
    for _, p := range sorted {
        text := strings.TrimSpace(p.Content)
        if text != "" {
            parts = append(parts, text)
        }
    }
    return strings.Join(parts, "\n\n")
}
```

### 5.4 Service 集成

```go
// application/app.go — Service 持有 PromptAssembly

type Service struct {
    // ... 现有字段 ...
    promptAssembly *PromptAssembly  // 替代 PromptStack（或共存）
}

func (service *Service) refreshPrompt() {
    // 1. Identity
    service.promptAssembly.Set(PromptPart{
        Kind: "identity", Priority: PriorityIdentity,
        Content: "You are Seelex, an intelligent coding assistant..."})

    // 2. Effort (包括行为指令 + planning 策略)
    effortPart := service.buildEffortPart()
    service.promptAssembly.Set(effortPart)

    // 3. Plugin prompt
    if plugin, ok := service.deps.Plugins.Current(); ok {
        service.promptAssembly.Set(PromptPart{
            Kind: "plugins", Priority: PriorityPlugins,
            Content: plugin.Prompt})
    }

    // 4. Instructions (始终存在)
    service.promptAssembly.Set(PromptPart{
        Kind: "instructions", Priority: PriorityInstructions,
        Content: service.buildSwitchInstructions()})

    // Skill 不参与 system prompt 组装；由 chatRequest 的 modelInput 携带。

    // 渲染并写入 engine
    service.deps.Engine.SetSystemPrompt(service.promptAssembly.Assemble())
}

func (service *Service) buildEffortPart() PromptPart {
    level := service.effortManager.Current()
    planning := effortPlanning[level]
    
    var b strings.Builder
    b.WriteString(effortPrompts[level])
    if p := planning.Prompt(); p != "" {
        b.WriteString("\n\n")
        b.WriteString(p)
    }
    return PromptPart{
        Kind: "effort", Priority: PriorityEffort,
        Content: b.String()}
}

func (service *Service) buildSwitchInstructions() string {
    plugins := service.deps.Plugins.All()
    names := make([]string, 0, len(plugins))
    for _, p := range plugins {
        names = append(names, p.Name)
    }
    
    return fmt.Sprintf(`## System Capabilities
- You can switch plugins via switch_plugin tool.
- Available plugins: %s
- User-selected skills arrive as structured entries in user messages.
- Current effort level: %s`,
        strings.Join(names, ", "),
        service.effortManager.Current())
}
```

---

## 6. Effort × Planning 策略详细定义

| Effort | Planning 风格 | Prompt 指令 | 期待行为 |
|--------|-------------|-------------|---------|
| **low** | none | 无规划指令 | 直接回答，不调 plan_* 工具 |
| **medium** | brief | "Briefly outline your approach before coding." | 在回答中简述思路，不落地工具 |
| **high** | structured | "Use plan_load/plan_run. Structure work into steps." | 复杂任务主动用 WorkPlan |
| **max** | dag | "Use WorkPlan DAG + Fork for parallel sub-agents." | 主动多级编排 + 并行子 agent |

### Effort 切换时的 Planning 行为轨迹

```
用户: /effort low
  系统: SetMaxLoops(0), 隐藏 plan_* 工具
  LLM: 直接简短回答，不调工具

用户: /effort medium
  系统: SetMaxLoops(8), 显示 plan_*
  LLM: 必要时简述思路，偶尔调工具

用户: /effort high
  系统: SetMaxLoops(25), 显示全部工具
  LLM: 复杂任务 → plan_load → plan_run → 结果
       简单任务 → 直接回答

用户: /effort max
  系统: SetMaxLoops(50), 显示全部 + Fork 工具
  LLM: 任何非琐碎任务 → plan_load → Fork 并行 → plan_run
       自动创建子 agent 处理独立子任务
```

---

## 7. 影响范围总表

| 层 | 文件 | 改动 | 估算 |
|----|------|------|------|
| **types** | `types/model.go` | LLMConfig 加 ReasoningEffort + ThinkingBudget | ~10 行 |
| **api** | `api/strategy.go` | RequestOptions 加 2 字段 | ~5 行 |
| **api** | `api/client.go` | requestOpts 传递新字段 | ~5 行 |
| **api** | `strategy_anthropic.go` | BuildRequest 加 thinking 块 | ~30 行 |
| **api** | `strategy_openai.go` | BuildRequest 加 reasoning_effort | ~15 行 |
| **config** | YAML 解析层 | account 结构体加字段 | ~10 行 |
| **application** | `application/effort.go` | 加 PlanningStrategy + SelectModel + Prompt 生成 | ~50 行 |
| **application** | `application/prompt_assembly.go` | **新增** 结构化 PromptAssembly | ~80 行 |
| **application** | `application/app.go` | Service 集成 PromptAssembly | ~30 行 |
| **application** | `application/input.go` | Skill 激活后构造用户上下文 | ~10 行 |
| **seelebridge** | `seelebridge/runtime.go` | 暴露模型切换 | ~15 行 |
| **main** | `main.go` | settings.json effort 读取 | ~20 行 |
| **总计** | — | — | **~280 行** |

---

## 8. 向后兼容

| 场景 | 兼容性 |
|------|--------|
| 现有 YAML 无 reasoning_effort | ✅ 空值 = 不设置，模型默认行为 |
| 现有 YAML 无 thinking_budget | ✅ 0 = provider 默认 budget |
| 现有代码调用 RequestOptions{} | ✅ 零值字段被 strategy 忽略 |
| 现有 ProviderStrategy 未实现 thinking | ✅ 不设置 thinking 块，行为不变 |
| 现有 effort_dd 的四级定义 | ✅ 保持一致：low/medium/high/max |
