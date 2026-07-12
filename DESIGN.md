# Seelex — Seele TUI 客户端设计文档

## 概述

Seelex 是 Seele 引擎的独立 TUI 客户端，使用 bubbletea 框架构建。
与 Seele 是松耦合关系：Seele 负责 LLM 调用/工具调度/会话管理，
Seelex 负责终端交互体验。

```
┌─────────────────────────────────┐
│          seelex TUI             │
│  ┌───────────────────────────┐  │
│  │  消息列表 (bubbles/viewport) │  │
│  │  You: ...                 │  │
│  │  Seele: ...               │  │
│  │                           │  │
│  ├───────────────────────────┤  │
│  │ > 输入框                   │  │
│  ├───────────────────────────┤  │
│  │ model  plugin  tok:123   │  │
│  └───────────────────────────┘  │
│           │ calls               │
│           ▼                     │
│  ┌───────────────────────────┐  │
│  │   Seele Engine             │  │
│  │   ChatStream / History     │  │
│  └───────────────────────────┘  │
└─────────────────────────────────┘
```

## 一、模块初始化

### go.mod

```go
module github.com/RedHuang-0622/seelex

go 1.25.5

// Seele 核心
require github.com/RedHuang-0622/Seele v0.0.1

// TUI 框架
require (
    github.com/charmbracelet/bubbletea v1.3.10
    github.com/charmbracelet/lipgloss v1.1.0
    github.com/charmbracelet/bubbles v1.0.0   // viewport
)

// 本地开发
replace github.com/RedHuang-0622/Seele => ../Seele
```

> 发布时去掉 `replace`，Seele 通过 GitHub tag 拉取。

### 完整导入路径

```go
import (
    "github.com/RedHuang-0622/Seele/agent"
    "github.com/RedHuang-0622/Seele/agent/core/api"
    "github.com/RedHuang-0622/Seele/agent/core/tool/builtin"
    "github.com/RedHuang-0622/Seele/agent/core/tool/holder"
    "github.com/RedHuang-0622/Seele/engine"
    "github.com/RedHuang-0622/Seele/seelectx/storage"
    "github.com/RedHuang-0622/Seele/seelectx/tracer"
    "github.com/RedHuang-0622/Seele/types"
)
```

## 二、核心类型定义

以下类型是 seelex 与 Seele 交互时涉及的完整类型签名，
**务必仔细阅读字段含义**，否则无法正确调 API。

### 2.1 LLM 配置

```go
// types.LLMConfig — Agent 的必填参数
type LLMConfig struct {
    BaseURL     string        // API 端点
    APIKey      string        // API Key
    Model       string        // 模型名（gpt-4o / claude-3-opus 等）
    MaxTokens   int           // 最大输出 token
    Timeout     time.Duration // HTTP 超时
    Temperature float64       // 采样温度（0 ~ 2）
}
```

### 2.2 消息（History / Stream 返回）

```go
// types.Message — 对话历史中的一条记录
type Message struct {
    Role             string      // "system" | "user" | "assistant" | "tool"
    Content          *string     // 文本内容（nil 表示纯 tool_call 消息）
    ReasoningContent string      // 思索文段（Claude 的 thinking）
    ToolCalls        []ToolCall  // assistant 发起的工具调用
    ToolCallID       string      // role="tool" 时对应的 call ID
    Name             string      // role="tool" 时的工具名
}

// types.ToolCall — 工具调用
type ToolCall struct {
    ID       string           // 唯一 ID
    Type     string           // "function"
    Function ToolCallFunction
}

type ToolCallFunction struct {
    Name      string // 工具名
    Arguments string // JSON 参数字符串
}
```

> 读取 History 时注意：`Content` 是 `*string`，需要判 nil：
> ```go
> if m.Content != nil {
>     fmt.Println(*m.Content)
> }
> ```

### 2.3 Agent 选项

```go
// agent.Options — Agent 的全部可选参数
type Options struct {
    LLMConfig       types.LLMConfig  // 必填：LLM 连接信息
    ToolCallTimeOut time.Duration    // 工具超时（推荐 120s）
    HubStartupDelay int              // MCP hub 启动等待秒数（推荐 10）

    // 以下为可选，通常不需要手动设置：
    ToolListTimeout   time.Duration
    Strategy          agent.Strategy     // 默认 Strategy
    WithManualStop    bool
}
```

### 2.4 Engine 选项

```go
// engine.Option — 全部配置引擎的函数式选项
func WithStore(s *storage.Store) Option           // 会话持久化
func WithCache(c cache.Provider) Option            // 缓存
func WithTracer(t tracer.Tracer) Option            // 追踪
func WithHooks(hooks *LoopHooks) Option            // 可视化回调（TUI 不需要）
func WithSystemPrompt(prompt string) Option        // 设置 system prompt
func WithSessionConfig(cfg SessionConfig) Option   // 自定义 session 参数
func WithLoop(l Loop) Option                       // 自定义 Loop 实现（通常不用）
func WithModelName(name string) Option             // 模型名
```

### 2.5 ChatClient 方法

```go
// api.ChatClient — Agent 内部的 LLM 客户端，强转获取
client := agt.LLM().(*api.ChatClient)

// 主要方法：
client.WithAccountPool(pool *api.AccountPool)    // 注入账号池（启动时调一次）
client.AccountPool() *api.AccountPool             // 获取账号池
client.SetProvider(pf api.ProviderType)           // 切换到指定 provider
client.SetProviderFilter(pf api.ProviderType)     // 同 SetProvider
client.ProviderFilter() api.ProviderType          // 当前 provider（""=round-robin）
```

### 2.6 存储

```go
// storage.Store — 会话持久化（由 Engine 内部管理，无需手动调用）
type Store struct { /* 私有字段 */ }

// 仅需初始化：
store, err := storage.NewStore(baseDir string) // "" → ".seele/sessions/"
```

## 三、初始化流程（main.go）

### 3.1 加载账号配置

```go
result, err := api.LoadFullAccountsConfig(*configPath)
// *configPath → "config/account-openai.yaml"

// 返回类型结构（无需 import，直接 . 访问）：
type configResult struct {
    LLMDefaults  *types.LLMDefaults  // 全局默认参数
    Pool         *api.AccountPool    // 账号池
}

ls  := result.LLMDefaults
pool := result.Pool
first := pool.All()[0]  // 取第一个账号

// pool.All() []api.AccountMeta
type AccountMeta struct {
    Name     string
    Provider string   // "openai" | "anthropic" 等
    Model    string
    BaseURL  string
    APIKey   string
    Disabled bool
}
```

### 3.2 构造 LLM 配置 + 创建 Agent

```go
llmCfg := types.LLMConfig{
    BaseURL:      first.BaseURL,
    APIKey:       first.APIKey,
    Model:        first.Model,
    MaxTokens:    ls.MaxTokens,
    Timeout:      ls.Timeout,
    Temperature:  ls.Temperature,
}

agt, err := agent.New(agent.Options{
    LLMConfig:        llmCfg,
    ToolCallTimeOut:  120 * time.Second,
    HubStartupDelay:  10,  // MCP hub 启动等待
})
if err != nil {
    // ✖ 最常见错误：API Key 无效、网络不通
    fmt.Fprintf(os.Stderr, "✖ Agent 初始化失败: %v\n", err)
    os.Exit(1)
}
defer agt.Shutdown()  // 重要：程序退出前必须调用

// 注入账号池
chatClient := agt.LLM().(*api.ChatClient)
chatClient.WithAccountPool(pool)
if ls.Provider != "" {
    chatClient.SetProvider(ls.Provider)
}
```

### 3.3 注册工具

#### 内置工具注册

```go
// 注册全部内置工具（grep / read_file / write_file / bash / git 等）
builtin.RegisterAll(agt.Tools())
```

#### 自定义工具注册

```go
// agt.RegisterTool 函数签名：
func (a *Agent) RegisterTool(
    name        string,                         // 工具名（LLM 可见）
    description string,                         // 描述（LLM 理解用途）
    schema      map[string]interface{},         // JSON Schema
    handler     func(ctx context.Context, argsJSON string) (string, error),
)

// 示例：get_time
agt.RegisterTool("get_time", "获取当前日期和时间",
    map[string]interface{}{
        "type":       "object",
        "properties": map[string]interface{}{},
    },
    func(_ context.Context, _ string) (string, error) {
        return fmt.Sprintf(`"%s"`, time.Now().Format("2006-01-02 15:04:05")), nil
    },
)
```

> **注意**：handler 接收 `argsJSON` 是 JSON 字符串，需要自行 `json.Unmarshal`。

#### WorkPlan 工具注册

```go
wpt := builtin.NewWorkPlanTool(
    builtin.NewChatAgentFactory(agt.LLM()),
    // ChatAgentFactory 让 WorkPlan 工具能自己调 LLM 做子任务
)
agt.Tools().Register(wpt)
```

### 3.4 插件系统

插件系统实现工具的分组访问控制。LLM 通过 `switch_mode` 自主切换插件。

```go
func initPlugins(agt *agent.Agent) {
    pm := holder.NewPluginManager()

    // holder.NewPlugin 函数签名：
    // func NewPlugin(name, desc string, include, exclude []string) *Plugin
    pm.Define(holder.NewPlugin("default", "所有工具可用", nil, nil))
    pm.Define(holder.NewPlugin("read", "阅读/搜索模式",
        []string{"switch_mode", "grep*", "read_file", "glob",
            "git_status", "git_log", "git_diff", "get_time"},
        nil,
    ))
    pm.Define(holder.NewPlugin("write", "编辑模式",
        []string{"switch_mode", "write*", "edit*", "read_file",
            "bash", "git_diff", "git_status", "get_time"},
        nil,
    ))
    pm.Define(holder.NewPlugin("git", "Git 模式",
        []string{"switch_mode", "git_*", "bash", "get_time"}, nil,
    ))
    pm.Define(holder.NewPlugin("shell", "Shell/DevOps 模式",
        []string{"switch_mode", "bash", "get_time"}, nil,
    ))
    pm.Define(holder.NewPlugin("plan", "WorkPlan 工作流模式",
        []string{"switch_mode", "plan_*", "get_time"}, nil,
    ))

    agt.Tools().WithPluginManager(pm)
}
```

**运行时 API：**

```go
// 切换插件（LLM 调用 switch_mode 时触发）
agt.Tools().ActivatePlugin("read")     // 成功 → nil，失败 → error
agt.Tools().DeactivatePlugin()          // 回到 default

// 查询插件状态
active := agt.Tools().ActivePlugin()   // "" | "read" | "write" 等
pm := agt.Tools().Plugin()             // *holder.PluginManager

// 遍历所有插件
if pm != nil {
    for _, name := range pm.AllPlugins() {
        p, _ := pm.Plugin(name)
        // p.Name, p.Description
    }
}
```

### 3.5 存储初始化

```go
store, err := storage.NewStore("")   // 默认路径 .seele/sessions/
if err != nil {
    log.Fatal(err)
}
// Engine 接管后自动管理 Save/Load，无需手动操作
```

### 3.6 Engine 创建

```go
eng := engine.New(agt,
    engine.WithStore(store),         // 会话持久化
    // 可选参数：
    // engine.WithSystemPrompt("You are ...")  // system prompt
    // engine.WithTracer(tr)                    // 追踪
    // engine.WithCache(cache)                  // 缓存
    // engine.WithHooks(hooks)                  // TUI 不需要
)

// Engine 核心方法：
eng.ChatStream(ctx, input, onChunk)  // 流式 Chat（最常用）
eng.Chat(ctx, input)                 // 同步 Chat
eng.History() []types.Message        // 历史
eng.ClearHistory()                   // 清空
eng.ExportTrace() *tracer.Tree       // Trace 树
eng.SessionID() string               // 当前 session
eng.SetMaxLoops(n int)              // 设置最大 tool 循环
eng.SetSystemPrompt(p string)        // 运行时替换 system prompt
```

### 3.7 switch_mode 工具注册

这是让 LLM 能自主切换插件的关键工具，**必须注册**否则 LLM 只能使用 default 插件。
从原 REPL 迁移此段：

```go
// 闭包捕获 eng / prompts / agt
agt.RegisterTool(
    "switch_mode",
    "切换工作模式以改变可用工具集。模式包括：default(全部), "+
        "read(搜索/读取), write(编辑), git(版本控制), shell(命令执行)。"+
        "切换后后续回合自动生效。",
    map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "mode": map[string]interface{}{
                "type": "string",
                "enum": []interface{}{"default", "read", "write", "git", "shell"},
                "description": "目标模式",
            },
        },
        "required": []string{"mode"},
    },
    func(_ context.Context, argsJSON string) (string, error) {
        var input struct {
            Mode string `json:"mode"`
        }
        if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
            return "", fmt.Errorf("switch_mode: %w", err)
        }
        mode := strings.ToLower(input.Mode)

        if mode == "" || mode == "default" {
            agt.Tools().DeactivatePlugin()
        } else {
            if err := agt.Tools().ActivatePlugin(mode); err != nil {
                return fmt.Sprintf(`{"error":"unknown mode: %s"}`, mode), nil
            }
        }

        // 如果后续接入了 prompt 系统，在此 eng.SetSystemPrompt(text)
        // if text, ok := prompts[mode]; ok { eng.SetSystemPrompt(text) }

        visible := agt.VisibleTools(context.Background())
        all := agt.Tools().Tools()
        return fmt.Sprintf(`{"mode":"%s","visible_tools":%d,"total_tools":%d}`,
            mode, len(visible), len(all)), nil
    },
)
```

### 3.8 Prompt 文件加载（可选）

```go
// 从 prompts/ 或 cmd/repl/prompts/ 目录加载 .md 文件
// 文件名（不含扩展名）为 prompt 名
func loadPrompts() map[string]string {
    prompts := make(map[string]string)
    for _, dir := range []string{"prompts", "cmd/repl/prompts"} {
        entries, err := os.ReadDir(dir)
        if err != nil {
            continue
        }
        for _, e := range entries {
            if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
                name := strings.TrimSuffix(e.Name(), ".md")
                data, err := os.ReadFile(filepath.Join(dir, e.Name()))
                if err == nil {
                    prompts[name] = string(data)
                }
            }
        }
        if len(prompts) > 0 {
            break
        }
    }
    return prompts
}

// 使用：
prompts := loadPrompts()
defaultPrompt := prompts["default"]
if defaultPrompt == "" {
    defaultPrompt = `You are Seele CLI, an intelligent coding assistant.
You can switch between specialized modes using the switch_mode tool.
Always respond in the user's language.`
}
eng = engine.New(agt, engine.WithSystemPrompt(defaultPrompt))

// 运行时替换：
eng.SetSystemPrompt(prompts["read"])  // 切换到 read 模式 prompt
```

### 3.9 启动 TUI

```go
p := tea.NewProgram(
    initialModel(eng, first.Model, chatClient, agt),
    tea.WithAltScreen(),  // alternate screen buffer
)
if _, err := p.Run(); err != nil {
    os.Exit(1)
}
// p.Run() 返回后，alternate screen 自动恢复
```

## 四、运行时 API（tui.go 核心）

### 4.1 流式 Chat

```go
// eng.ChatStream 函数签名：
func (e *Engine) ChatStream(
    ctx      context.Context,      // 上下文（取消可中断流）
    userInput string,              // 用户输入
    onChunk  func(string),         // 流式回调，每次收到一个 chunk
) (string, error)                  // 返回完整回复（通常忽略，用 onChunk）

// 流式调用会触发完整的 ReAct 循环（LLM → ToolCall → LLM → ...）
// onChunk 收到的可能是文本 chunk，也可能是空串
```

### 4.2 流式桥接模式

bubbletea 中处理异步 IO 的标准模式：goroutine → channel → tea.Msg。

```go
type streamChunk struct {
    text string           // chunk 内容
    done bool             // 流结束标记
    err  error            // 结束时如果有错误
}

// 后台 goroutine
func (m model) doStream(input string) {
    ctx := context.Background()
    _, err := m.eng.ChatStream(ctx, input, func(chunk string) {
        // 同步回调，channel 满时会阻塞 ChatStream
        m.streamCh <- streamChunk{text: chunk}
    })
    m.streamCh <- streamChunk{done: true, err: err}
}

// tea.Cmd 包装：等待下一个 streamChunk
func waitStream(ch chan streamChunk) tea.Cmd {
    return func() tea.Msg {
        return <-ch
    }
}

// Update 中的处理：
case streamChunk:
    if msg.done {
        m.streaming = false
        if msg.err != nil {
            // ChatStream 返回错误（网络断开 / API 拒绝 / 超时）
            addError(m, msg.err.Error())
            return m, nil
        }
        // 流正常结束，添加到对话列表
        m.messages = append(m.messages, messageView{
            role:    "assistant",
            content: m.streamBuf,
        })
        m.streamBuf = ""
        m.refreshView()
        return m, nil
    }
    // 累积 chunk
    m.streamBuf += msg.text
    m.refreshView()
    return m, waitStream(m.streamCh)  // 继续监听下一 chunk
```

#### 非阻塞发送（防止回调阻塞 LLM）

```go
// 如果 LLM 输出过快，channel 满可能导致回调阻塞。
// 改用 select + default 实现非阻塞：
func (m model) doStream(input string) {
    ctx := context.Background()
    _, err := m.eng.ChatStream(ctx, input, func(chunk string) {
        select {
        case m.streamCh <- streamChunk{text: chunk}:
        default:
            // 丢弃过快的 chunk（很少发生）
        }
    })
    m.streamCh <- streamChunk{done: true, err: err}
}
```

### 4.3 同步 Chat（备用）

```go
// 不需要流式时用同步版本：
reply, err := m.eng.Chat(ctx, userInput)
// reply 是完整回复文本
// err 可能值：API 错误、超时、网络断开
```

### 4.4 会话历史

```go
// 获取完整历史
hist := m.eng.History()   // []types.Message

// 每条 Message 的字段：
msg.Role      // "system" | "user" | "assistant" | "tool"
msg.Content   // *string（判 nil！）
msg.ToolCalls // []ToolCall（assistant 调工具时非空）
msg.ToolCallID  // tool 消息对应的 call ID
msg.Name        // tool 消息的工具名
msg.ReasoningContent // 思索文段

// 统计角色分布：
rc := make(map[string]int)
for _, h := range hist {
    rc[h.Role]++
}
// rc → {"user": 3, "assistant": 3, "tool": 5, "system": 1}

// 清空历史：
m.eng.ClearHistory()   // 保留 system 消息，清空其余
```

### 4.5 Token 统计

Token 统计通过 Trace 树提取，不是直接 API 返回。

```go
func tokensFromEngine(eng *engine.Engine) string {
    tree := eng.ExportTrace()
    if tree == nil || tree.Root == nil {
        return "?"
    }
    for _, c := range tree.Root.Children {
        if c.Kind == tracer.SpanLLMCall {
            if t, ok := c.Attrs["total_tokens"]; ok {
                return t
            }
        }
    }
    return "?"
}

// ExportTrace 返回值 *tracer.Tree 的结构：
type Tree struct {
    Root     *Span
    // ...
}
type Span struct {
    Name     string            // "LLM Call #1"
    Kind     SpanKind          // SpanLLMCall | SpanToolDispatch
    Attrs    map[string]string // "total_tokens" → "1234"
    Children []*Span           // 子 Span
    Duration time.Duration
    Status   SpanStatus        // SpanStatusOK | SpanStatusError
}
```

### 4.6 Trace 树

```go
tree := eng.ExportTrace()
if tree != nil && tree.Root != nil {
    text := tree.String()
    // 输出：
    // sess_1712345678
    //   ├── LLM Call #1 (1.2s)
    //   │   ├── grep (0.3s)
    //   │   └── read_file (0.1s)
    //   └── LLM Call #2 (0.8s)
}
```

用于 `/trace` 命令和调试。

### 4.7 Provider 信息

```go
pf := m.client.ProviderFilter()
// 返回值类型 api.ProviderType = string
// 值域：
//   "openai"    → 仅使用 OpenAI 账号
//   "anthropic" → 仅使用 Anthropic 账号
//   ""          → round-robin（所有账号轮询）

// 账号池信息（用于 /pool 命令）：
pool := m.client.AccountPool()
if pool != nil {
    for _, a := range pool.All() {
        a.Name     // 账号名
        a.Provider // "openai"
        a.Model    // "gpt-4o"
        a.BaseURL  // "https://api.openai.com/v1"
        a.Disabled // true=不可用
    }
}
```

### 4.8 插件状态

```go
plugin := m.agt.Tools().ActivePlugin()
// "" | "read" | "write" | "git" | "shell" | "plan"

// 插件列表：
pm := m.agt.Tools().Plugin()
if pm != nil {
    for _, name := range pm.AllPlugins() {
        p, _ := pm.Plugin(name)
        fmt.Printf("  %s: %s\n", name, p.Description)
    }
}
```

### 4.9 VisibleTools 查询

```go
// 当前插件可见的工具列表（用于状态栏展示）
tools := m.agt.VisibleTools(context.Background())
// []types.Tool，每个 Tool 有 Function.Name
for _, t := range tools {
    t.Function.Name  // 工具名
}

// 全部工具（无论插件过滤）
all := m.agt.Tools().Tools()
```

## 五、TUI 模型设计

### 5.1 Model 结构

```go
type model struct {
    // ── Seele 引用 ──
    eng       *engine.Engine    // LLM 调用入口
    client    *api.ChatClient   // Provider 信息
    agt       *agent.Agent      // 插件管理
    modelName string            // 当前模型名

    // ── 消息列表 ──
    messages []messageView      // 用户可见的消息历史
    input    string             // 当前输入缓冲区

    // ── 流式输出 ──
    streaming bool              // 是否正在流式输出
    streamBuf string            // 当前累积的流式内容
    streamCh  chan streamChunk  // go→bubbletea 管道
    lastInput string            // 最后一次用户输入
    lastStart time.Time         // 最后一次调用时间

    // ── 终端状态 ──
    viewport viewport.Model     // 可滚动消息区
    ready    bool
    width    int
    height   int
    quitting bool
}

type messageView struct {
    role    string  // "user" | "assistant" | "system" | "error"
    content string
}
```

### 5.2 Bubbletea 生命周期

```
tea.NewProgram(model)  ← main.go
        │
    model.Init()       ← 返回初始 Cmd（通常 nil）
        │
   ┌────┴────┐  (loop)
   │ Update  │  ← 处理 tea.Msg（键盘/窗口/自定义事件）
   └────┬────┘
        │
    model.View()  ← 返回渲染字符串
        │
    tea.Quit  ← 收到退出消息
        │
    p.Run() 返回  ← 程序退出
```

### 5.3 键盘映射

| 按键 | 行为 |
|---|---|
| 普通字符 | 追加到输入缓冲区 |
| Enter | 提交输入 → 触发流式 Chat |
| Backspace | 删除最后一个字符 |
| Ctrl+C / Ctrl+D | 退出 |
| /help | 显示帮助 |
| /clear | 清空历史 |
| /model | 显示模型 + Provider |
| /history | 显示历史统计 |
| /trace | 显示 Trace 树 |
| /exit | 退出 |

### 5.4 字符输入处理

```go
// 当前方案（简单但有问题）：
default:
    if !m.streaming && len(msg.String()) == 1 {
        m.input += msg.String()
    }

// 问题：组合键（Ctrl+A 等）也 len==1，会被当作字符
// 改进：过滤不可见字符
default:
    if !m.streaming {
        s := msg.String()
        if len(s) == 1 && s[0] >= 0x20 && s[0] != 0x7f {
            m.input += s
        }
    }
```

### 5.5 状态栏

```
  right-aligned:  round-robin  tok:1234  3s
  right-aligned:  openai [write]  tok:5678  5s
```

实现：在行首填充空格实现右对齐。

```go
right := fmt.Sprintf("%s%s  tok:%s  %s", pf, pluginStr, tokens, elapsed)
padding := m.width - lipgloss.Width(right) - 2
if padding < 0 {
    padding = 0
}
styleStatus.Render("  " + strings.Repeat(" ", padding) + right)
```

## 六、错误处理

### 6.1 初始化错误

| 错误 | 原因 | 处理 |
|---|---|---|
| `api.LoadFullAccountsConfig` 失败 | 配置文件路径错误 / YAML 格式错误 | stderr 输出后 exit(1) |
| `agent.New` 失败 | API Key 为空 / 网络不通 / 参数错误 | stderr 输出后 exit(1) |
| `storage.NewStore` 失败 | 目录无写入权限 | stderr 输出后 exit(1) |

### 6.2 运行时错误

```go
// ChatStream 可能的错误：
case streamChunk:
    if msg.done && msg.err != nil {
        m.streaming = false
        // 常见错误：
        //   1. API key 过期 / 额度用尽
        //   2. 网络断开
        //   3. 工具执行超时（ToolCallTimeOut）
        //   4. LLM 返回空内容
        // 处理：显示错误消息，允许继续输入
        m.messages = append(m.messages, messageView{
            role:    "error",
            content: msg.err.Error(),
        })
        m.refreshView()
        return m, nil
    }
```

## 七、配置文件格式

```yaml
# config/account-openai.yaml
defaults:
  provider: openai
  max_tokens: 4096
  timeout: 120s
  temperature: 0

accounts:
  - name: main
    provider: openai
    model: gpt-4o
    base_url: https://api.openai.com/v1
    api_key: sk-...

# 多 provider 配置示例（round-robin 自动轮询）：
---
defaults:
  provider: anthropic
  max_tokens: 8192

accounts:
  - name: claude
    provider: anthropic
    model: claude-sonnet-4-20250514
    base_url: https://api.anthropic.com/v1
    api_key: sk-ant-...
```

## 八、开发

```bash
# 运行
go run . -c config/account-openai.yaml

# 编译
go build -o seelex.exe .

# 测试
go test ./...

# 清理 go.sum + 重建
rm go.sum && go mod tidy
```

## 九、API 调用速查表

| 调用 | 包 | 用途 | 阶段 |
|---|---|---|---|
| `api.LoadFullAccountsConfig(path)` | `agent/core/api` | 加载 YAML 配置 | 启动 |
| `agent.New(opts)` | `agent` | 创建 Agent | 启动 |
| `agt.LLM().(*api.ChatClient)` | `agent/core/api` | 获取 ChatClient | 启动 |
| `client.WithAccountPool(pool)` | `agent/core/api` | 注入账号池 | 启动 |
| `client.SetProvider(pf)` | `agent/core/api` | 设置 provider | 启动 |
| `client.ProviderFilter()` | `agent/core/api` | 当前 provider | 运行时 |
| `builtin.RegisterAll(tools)` | `agent/core/tool/builtin` | 注册内置工具 | 启动 |
| `agt.RegisterTool(name, desc, schema, fn)` | `agent` | 注册自定义工具 | 启动 |
| `agt.Tools().Register(wpt)` | `agent` | 注册 WorkPlan | 启动 |
| `holder.NewPluginManager()` | `agent/core/tool/holder` | 创建插件管理器 | 启动 |
| `pm.Define(plugin)` | `agent/core/tool/holder` | 定义插件 | 启动 |
| `agt.Tools().WithPluginManager(pm)` | `agent` | 注入插件系统 | 启动 |
| `agt.Tools().ActivatePlugin(name)` | `agent` | 切换插件 | 运行时 |
| `agt.Tools().ActivePlugin()` | `agent` | 当前插件名 | 运行时 |
| `agt.Tools().Plugin()` | `agent` | 插件管理器 | 运行时 |
| `agt.VisibleTools(ctx)` | `agent` | 当前可见工具 | 运行时 |
| `storage.NewStore(dir)` | `seelectx/storage` | 存储初始化 | 启动 |
| `engine.New(agt, opts...)` | `engine` | 创建 Engine | 启动 |
| `eng.ChatStream(ctx, input, onChunk)` | `engine` | **流式 Chat** | **核心** |
| `eng.Chat(ctx, input)` | `engine` | 同步 Chat | 备用 |
| `eng.History()` | `engine` | 获取历史 | 运行时 |
| `eng.ClearHistory()` | `engine` | 清空历史 | 运行时 |
| `eng.ExportTrace()` | `engine` | 导出 Trace 树 | 运行时 |
| `eng.SetSystemPrompt(p)` | `engine` | 替换 system prompt | 运行时 |
| `eng.SetMaxLoops(n)` | `engine` | 设置最大循环 | 运行时 |
| `tokensFromEngine(eng)` | `tui.go` | 提取 token 数 | 运行时 |
