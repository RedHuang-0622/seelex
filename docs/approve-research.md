# Approve 节点选型调研报告

> 调研日期：2026-07-16
> 调研对象：OpenCode、Claude Code
> 目标：为 Seele + seelex 的 approve/choice 交互机制选型

---

## 一、核心发现：三种实现模式对比

| 维度 | OpenCode | Claude Code | Seele 当前 |
|------|----------|-------------|-----------|
| **调用形式** | 权限门控拦截工具调用 | 权限门控拦截工具调用 | WorkPlan DAG 中的 `KindApprove` 节点 |
| **是否暴露为 Tool** | ❌ 不是 | ❌ 不是 | 半是（plan_load 可构建 approve 节点） |
| **配置来源** | `opencode.json` 声明式 | `settings.json` permissions.allow | 代码中注入 `ApprovalGate` 实例 |
| **TUI 实现** | Bubble Tea pub-sub overlay | 内置模态框 overlay | seelex 已有 `renderPrompt()` 简易面板 |
| **选项粒度** | once / always / reject | Allow / Deny / Always allow | execute / skip / abort（可自定义） |
| **异步支持** | 事件总线 + Promise | 内部 channel | NetworkApprovalGate（channel 阻塞） |
| **插件扩展** | `permission.asked` 事件 | hooks 机制 | ApprovalGate 接口（策略模式） |

---

## 二、OpenCode 详细分析

### 2.1 架构模型：三级权限策略

```
LLM 决定调用工具
  → 权限系统评估 (allow / ask / deny)
  → tool.execute.before hook (治理插件)
  → 若 "ask" → 用户审批弹窗 (once / always / reject)
  → 工具执行 (内置或 MCP)
  → tool.execute.after hook (审计/密文脱敏)
  → 结果返回 LLM
```

**核心原则：审批是基础设施横切关注点，不是工具。**

### 2.2 配置：声明式权限规则

```jsonc
// opencode.json
{
  "permission": {
    "bash": "ask",           // 简单模式：工具名 → 动作
    "edit": "allow",
    "webfetch": "deny",
    // 对象模式：glob 匹配命令模式
    "bash": {
      "git *": "allow",      // git 命令自动允许
      "npm *": "allow",
      "rm *": "deny",        // 危险命令拒绝
      "*": "ask"             // 其他 shell 命令询问
    },
    "edit": {
      "*.md": "allow",
      "*.ts": "ask",
      "*": "deny"
    }
  }
}
```

**规则评估：** 顺序匹配，最后一条胜出。支持 glob 模式（`*` 单段，`**` 递归）。

### 2.3 TUI 渲染：Bubble Tea 居中 Overlay

```
触发流程：
1. 工具需要审批 → 发布 PermissionRequest 事件到 pub-sub bus
2. appModel.Update() 捕获事件 → showPermissions = true
3. View() 用 layout.PlaceOverlay() 居中渲染半透明遮罩 + 选项卡片
4. 用户选择 once / always / reject
5. 发布 PermissionResponseMsg → Promise resolve → overlay 关闭
```

### 2.4 插件体系：事件总线

| 事件 | 用途 |
|------|------|
| `permission.asked` | 审批请求发出时触发，含 `{id, sessionID, permission, patterns[], tool}` |
| `permission.replied` | 用户回复后触发，含 `{sessionID, requestID, reply}` |
| `tool.execute.before` | 工具执行前，治理插件可阻断 |
| `tool.execute.after` | 工具执行后，用于审计/脱敏 |

**关键设计：** 插件可通过 Client API 编程式回复审批（`POST /permission/:id/reply`），实现远程审批（手机/Apple Watch 等）。

---

## 三、Claude Code 详细分析

### 3.1 架构模型：Harness 层拦截

```
用户输入 prompt
  → LLM 生成回复 + tool_calls
  → Harness 拦截每个 tool_call
  → 检查 settings.json permissions.allow 模式列表
  → 匹配 → 直接执行
  → 不匹配 → 渲染 TUI 审批弹窗
      → Allow → 执行
      → Deny → 返回拒绝消息给 LLM
      → Always allow → 写入 settings.json + 执行
  → 结果返回 LLM
```

**核心原则：模型完全不知道权限系统的存在。** 从模型视角看，工具调用要么成功，要么返回 "permission denied"。

### 3.2 配置：三级作用域 + 模式匹配

```jsonc
// ~/.claude/settings.json (全局)
{
  "permissions": {
    "allow": [
      "Bash(go build *)",          // 精确匹配命令
      "Bash(git *)",               // 通配符匹配
      "Read(//d/goproject/**)",    // 路径递归匹配
      "Edit(*.md)"                 // 文件扩展名匹配
    ],
    "additionalDirectories": ["g:\\Program\\go\\other_project"]
  }
}
```

| 作用域 | 路径 | 用途 |
|--------|------|------|
| Global (User) | `~/.claude/settings.json` | 跨项目通用规则 |
| Project | `<project>/.claude/settings.json` | 项目级提交共享 |
| Project Local | `<project>/.claude/settings.local.json` | 本地覆盖，不提交 |

### 3.3 模式语法

```
ToolName(glob_pattern)
```

- `Bash(go build *)` —— 任意 `go build` 参数
- `Bash(echo "EXIT:$?")` —— 精确字面匹配
- `Read(//path/**)` —— 递归路径访问
- `Edit(*.go)` —— 特定文件类型编辑

**没有正则，只支持 glob。** 简单 = 安全 = 可审计。

### 3.4 TUI：终端内联模态框

Claude Code 的审批弹窗直接嵌入终端流中，不是独立窗口：
- 显示被拦截的完整命令/工具调用
- 选项：Allow / Deny / Always allow / Modify（修改后执行）
- 支持键盘快捷键（数字键 + 首字母）
- 状态栏指示等待审批状态

---

## 四、Seele 当前状态分析

### 4.1 已有的 Approve 基础设施

**WorkPlan 层（`workplan/sugar/approve/`）：**

```go
// 数据模型
type Question struct {
    ID      string
    Content string
    Options []ChoiceOption
    KVS     map[string]any
    Timeout time.Duration
}

type ChoiceOption struct {
    Key, Label, Description, Style string
}

// 策略接口
type ApprovalGate interface {
    Ask(ctx context.Context, q Question) (any, error)
}

// 内置选项
Choices("execute", "skip", "abort", "confirm", "retry")
```

**三种 Gate 实现（`workplan/gate.go`）：**

| Gate | 实现方式 | 适用场景 |
|------|----------|----------|
| `CLIApprovalGate` | `fmt.Scanln` 阻塞读 stdin | 终端 REPL |
| `NetworkApprovalGate` | channel 阻塞 + `OnQuestion` 回调 | HTTP/WebSocket 远程审批 |
| `AutoApproveGate` | 静默返回首选项 | 自动化/CI |

### 4.2 seelex 已有的 Prompt 面板

**类型定义（`tui/types.go`）：**
```go
type promptRequest struct {
    question string
    choices  []string
    ch       chan string
}
```

**外部调用入口（`tui/dialog.go`）：**
```go
func HandleApproval(question string, choices []string) string {
    ch := initApproval()
    pendingPrompt = promptRequest{question: question, choices: choices, ch: ch}
    return <-ch  // 阻塞等待用户选择
}
```

**渲染（`tui/view.go`）：**
```go
func (m Model) renderPrompt() string {
    // 标题行 + 选项列表（↑↓ 导航，Enter/数字键选择）
}
```

**键盘处理（`tui/dialog.go`）：**
- `↑↓` 导航选项
- `Enter` 确认选择
- `1-9` 数字键快捷选择
- `Ctrl+C` 取消

### 4.3 当前问题与缺口

1. **Approve 节点未集成到 ReAct 循环** — `ApproveNode` 只在 WorkPlan DAG 中使用，LLM 在普通对话中无法触发审批
2. **缺少权限配置层** — 没有类似 `settings.json` 的声明式权限规则
3. **seelex prompt 面板简陋** — 只支持简单文本+选项列表，无样式化的选项卡片，无超时倒计时
4. **无 "Always allow" 记忆能力** — 用户每次都要重复选择，无法自动记住偏好
5. **审批与工具系统脱节** — 审批是 WorkPlan 概念，不是跨横切面的基础设施
6. **无模型可调用的审批机制** — LLM 无法在需要时主动请求用户输入（只能通过 plan_load → approve node）

---

## 五、方案建议

### 5.1 推荐方案：混合模式（Harness 拦截 + 模型可请求）

综合 OpenCode 和 Claude Code 的实践，建议采用两层架构：

```
┌─────────────────────────────────────────────────────┐
│  Layer 1: Permission Gate (Harness 拦截)             │
│  ───────────────────────────────────────────────     │
│  所有工具调用前自动检查权限配置                        │
│  配置驱动：settings.json / seele.json                │
│  对 LLM 透明（模型不知道权限检查存在）                 │
├─────────────────────────────────────────────────────┤
│  Layer 2: Approve Tool (模型可主动请求)               │
│  ───────────────────────────────────────────────     │
│  暴露 ask_user / request_approval 工具给 LLM          │
│  用于需要用户判断的语义场景（方案选择、内容审核）       │
│  与 WorkPlan 的 ApproveNode 复用同一 TUI 组件         │
└─────────────────────────────────────────────────────┘
```

### 5.2 调用形式

**方式一：Permission Gate（推荐作为主要机制）**

```
Engine Loop → agent.Dispatch(toolName, args)
  → ToolGateway 检查权限配置
  → allow → 执行
  → ask  → 发布 ApprovalRequest 事件 → TUI 渲染弹窗 → 用户响应
  → deny → 返回 permission denied
```

**方式二：Approve Tool（辅助机制）**

```json
// LLM 可主动调用的工具
{
  "name": "ask_user",
  "description": "当需要用户确认或选择时调用。例如方案选择、危险操作确认、歧义澄清。",
  "parameters": {
    "type": "object",
    "properties": {
      "question": {"type": "string", "description": "向用户展示的问题"},
      "options": {
        "type": "array",
        "items": {
          "type": "object",
          "properties": {
            "key": {"type": "string"},
            "label": {"type": "string"},
            "description": {"type": "string"}
          }
        }
      }
    }
  }
}
```

### 5.3 推荐选择：以 Layer 1 (Permission Gate) 优先

**理由：**
1. **安全性** — 权限检查在 harness 层，不依赖模型行为（Claude Code 和 OpenCode 的共同选择）
2. **声明式配置** — 用户可通过配置文件控制所有工具权限，可版本控制、可审计
3. **渐进式** — 先实现 Permission Gate，后续再按需添加 Approve Tool
4. **与现有架构契合** — Seele 已有 `ToolGateway` 和插件过滤机制，Permission Gate 自然扩展此层

### 5.4 TUI 方案：Bubble Tea 独立 Model

```go
// 在 seelex/tui/ 中新增 approve.go
type ApproveModel struct {
    question   string
    options    []ApproveOption
    selected   int
    timeout    time.Duration
    startTime  time.Time
    width      int
    height     int
    resolved   bool
    result     string
}

type ApproveOption struct {
    Key         string
    Label       string
    Description string
    Style       string  // "primary" | "secondary" | "danger" | "warning"
}
```

**渲染效果预览：**

```
╭──────────────────────────────────────────────────────────────╮
│                                                              │
│  ⚠ 需要确认：deploy-production                               │
│                                                              │
│  即将部署到生产环境，涉及 12 个服务，预计影响 5000 在线用户。    │
│  变更内容：                                                   │
│    • 数据库迁移 v3.2.1                                       │
│    • API 网关配置更新                                         │
│    • 缓存集群重启                                             │
│                                                              │
│  ──────────────────────────────────────────────────────────  │
│                                                              │
│  ▸ 1. 执行部署    按计划执行生产部署                           │
│    2. 跳过         跳过本次部署，保留变更                      │
│    3. 终止         终止整个部署流程                            │
│    4. 始终允许     记住此选择，后续自动执行                     │
│                                                              │
│  ↑↓ 选择  Enter 确认  Esc 取消    剩余 28s                    │
│                                                              │
╰──────────────────────────────────────────────────────────────╯
```

### 5.5 入参出参定义

**Permission Gate 入参（配置层）：**

```go
// 权限规则（加载自配置文件）
type PermissionRule struct {
    ToolName    string   `json:"tool"`      // "bash", "edit", "read" 等
    Patterns    []string `json:"patterns"`  // glob 模式列表
    Action      string   `json:"action"`    // "allow" | "ask" | "deny"
}

// 审批请求（运行时）
type ApprovalRequest struct {
    ID          string           // 唯一请求 ID
    ToolName    string           // 工具名
    Arguments   string           // JSON 参数字符串
    Preview     string           // 人类可读的预览（如 "rm -rf /tmp/cache"）
    Risk        string           // "low" | "medium" | "high"
    Options     []ApproveOption  // 用户选项
    Timeout     time.Duration    // 超时（0 = 无超时）
    SessionID   string           // 所属会话
}
```

**Permission Gate 出参：**

```go
type ApprovalResponse struct {
    RequestID   string    // 对应的请求 ID
    Choice      string    // 用户选择的 key
    Remember    bool      // 是否记住此选择（"始终允许"）
    Timestamp   time.Time // 响应时间
}
```

**Approve Tool 入参（模型视角）：**

```jsonc
{
  "question": "请选择部署策略",
  "options": [
    {"key": "rolling", "label": "滚动更新", "description": "逐个替换实例，零停机"},
    {"key": "blue_green", "label": "蓝绿部署", "description": "切换流量，快速回滚"},
    {"key": "canary", "label": "金丝雀发布", "description": "逐步放量，风险最低"}
  ],
  "multi_select": false,
  "timeout_seconds": 120
}
```

**Approve Tool 出参（返回给模型）：**

```jsonc
{
  "choice": "canary",
  "label": "金丝雀发布",
  "elapsed_ms": 4500
}
```

### 5.6 实现路线图

```
Phase 1: 权限配置层（Seele core）
├── 新增 config/permission.go — 权限规则加载
├── 扩展 agent/gateway/tool/default.go — 权限检查拦截点
├── 新增 agent/core/tool/permission/ — ApprovalRequest/Response 通道
└── 配置文件 schema（seele.json）

Phase 2: TUI 审批组件（seelex）
├── 新增 tui/approve.go — ApproveModel（独立 bubbletea Model）
├── 新增 tui/approve_view.go — 渲染：卡片式选项 + 超时倒计时
├── 增强 tui/tui.go — 嵌入 ApproveModel + 审批事件处理
├── 增强 tui/dialog.go — HandleApproval 升级为异步非阻塞
└── 新增 tui/approve_styles.go — 风险等级配色

Phase 3: Approve Tool（可选）
├── 新增 agent/core/tool/builtin/approve.go — ask_user 工具
├── Tool handler 发布 ApprovalRequest 到 TUI channel
└── 与 WorkPlan ApproveNode 共享 ApprovalGate 实现

Phase 4: 高级特性
├── "始终允许" 模式记忆（写入配置）
├── 远程审批适配（复用 NetworkApprovalGate）
├── 审批历史审计日志
└── 多选（multi_select）支持
```

### 5.7 关键设计决策

| 决策点 | 选择 | 依据 |
|--------|------|------|
| 审批是否暴露为 Tool | 不暴露（Harness 拦截） | Claude Code/OpenCode 共识；安全优先 |
| Approve Tool 是否保留 | 保留（辅助场景） | 语义审批（方案选择）不同于权限审批 |
| TUI 实现方式 | 独立 tea.Model | 解耦；可复用；支持超时动画 |
| 配置格式 | JSON（settings.json 风格） | 与 Claude Code 兼容；glob 简单可读 |
| 权限检查位置 | ToolGateway.Dispatch 之前 | 单一切入点；不依赖模型行为 |
| 模式匹配语法 | `Tool(args_pattern)` | Claude Code 已验证的简洁方案 |

---

## 六、参考链接

- OpenCode 权限系统 PR: `thdxr/opencode#6319` — PermissionNext 模块
- OpenCode 文档: https://opencode.ai/docs/permissions
- Claude Code 文档: https://code.claude.com/docs (permissions 章节)
- Seele WorkPlan Approve: `workplan/sugar/approve/approve.go`
- Seele Gate 实现: `workplan/gate.go`
- seelex 现有 Prompt: `seelex/tui/dialog.go`

---

> **结论：采用 Harness 拦截为主 + Approve Tool 为辅的混合模式，在 seelex 中实现独立 Bubble Tea ApproveModel 组件，配置采用 JSON + glob 模式匹配。**
