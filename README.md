# Seelex — Seele TUI 客户端

**Seelex** 是 [Seele](https://github.com/RedHuang-0622/Seele) 引擎的独立 TUI 客户端，使用 [Bubble Tea](https://github.com/charmbracelet/bubbletea) 框架构建。

与 Seele 是松耦合关系：Seele 负责提供 LLM 调用/工具调度/会话管理，Seelex 负责终端交互体验。

## 架构

```
Bubble Tea TUI ── AppController ──┐
                                  ├── application.Service
Electron/其他前端 ── IPC adapter ─┘      │
                                         ├── Chat / Command / Completion
                                         ├── Plugin / Skill / Session
                                         ├── EventHub / Snapshot
                                         └── ApprovalBroker
                                                  │ ports
                                                  ▼
                                  Engine / seelebridge / plugin / skill / session
```

`application` 是无界面的应用核心，持有业务状态、副作用和异步生命周期；`tui` 只负责 Bubble Tea 输入事件、终端尺寸、光标/选中项、滚动和 Lipgloss 渲染。Snapshot、Event 和 Interaction DTO 均不依赖 Bubble Tea，可供后续 Electron sidecar 直接序列化。

### 装配顺序

| 层 | 职责 | 对应函数 |
|----|------|---------|
| L1 | Runtime、Skill、Plugin、Store | `initRuntime()`, `initSkillSystem()`, `initPluginSystem()` |
| L2 | 事件与审批基础设施 | `application.NewEventHub()`, `application.NewApprovalBroker()` |
| L3 | Engine 与实例化 Tool Hooks | `initEngine()` |
| L4 | 产品工具与 Session | `registerProductTools()`, `initSessionManager()` |
| L5 | Headless Application Core | `initApplication()` |
| L6 | TUI Adapter | `initTUI()` |
| L7 | Bubble Tea Program | `startTUI()` |

### 设计模式

| 模式 | 用途 |
|------|------|
| 🏭 **工厂模式** | 创建 Runtime、Application、Session、Skill 等依赖 |
| 🔌 **端口适配器** | `application` 定义消费接口，`application_adapters.go` 转换具体实现 |
| ⚔️ **策略模式** | application 内的实例化 `CommandRegistry` |
| 📣 **发布订阅** | `EventHub` 向 TUI 或未来 IPC adapter 发布有序事件 |
| 🧊 **外观模式** | `session.Manager` 薄包装 `storage.Store` |
| 🚪 **审批 Broker** | 工具同步等待决议，前端仅展示并 Resolve |

## 快速开始

### 前提
- Go ≥ 1.25

### 安装

```bash
git clone https://github.com/RedHuang-0622/seelex.git
cd seelex
```

### 配置

```bash
cp config/account-openai.yaml config/account-openai.local.yaml
# 编辑 config/account-openai.local.yaml 填入 API Key
```

```yaml
defaults:
  provider: openai
  max_tokens: 4096
  timeout: 60
  temperature: 0.7

accounts:
  - name: main
    provider: openai
    model: gpt-4o
    base_url: https://api.openai.com/v1
    api_key: sk-...
```

> 支持多账号 round-robin 轮询，参见 `config/account-pool.yaml`

#### 从 Claude Code 同步 MiniMax 账号

如果 Claude Code 使用 `https://api.minimaxi.com/anthropic`，可将同一账号转换为 OpenAI 兼容配置：

```powershell
.\scripts\sync-claudecode-account.ps1
go run . -c config/account-claudecode.local.yaml
```

脚本读取 `$HOME/.claude/settings.json` 中的 `ANTHROPIC_AUTH_TOKEN`，使用 OpenAI 兼容端点 `https://api.minimaxi.com/v1` 和模型 `MiniMax-M3` 生成本地账号池。`config/*.local.yaml` 已加入 `.gitignore`，Token 不会进入版本库。MiniMax 的 OpenAI 兼容接口使用 Bearer API Key，并通过 `/v1/chat/completions` 提供文本生成能力。

### 运行

```bash
go run . -c config/account-openai.local.yaml
```

### CLI 标志

| 标志 | 默认值 | 说明 |
|------|--------|------|
| `-c` | `config/account-openai.yaml` | LLM 配置路径 |
| `-store` | `""`（当前目录） | 持久化存储路径 |
| `-skills` | `skills,cmd/repl/skills` | Skill 加载路径（逗号分隔） |

### 编译

```bash
go build -o seelex.exe .
```

## 功能

### 权限门控（Permission Gate）

所有工具调用自动检查权限规则（配置见 `seele.yaml`）。支持三级动作：

| 动作 | 行为 |
|------|------|
| `allow` | 直接执行，LLM 无感知 |
| `ask` | 弹出卡片式审批面板，等待用户确认 |
| `deny` | 返回 permission denied |

支持"始终允许"记忆：同一工具+同一参数不再询问。

### 智能提示（类似 Claude Code）

| 触发器 | 效果 |
|--------|------|
| 输入 `/` | 合并展示命令、可见工具和 Skill |
| 输入 `#` | 弹出已加载的 Skill 列表 |
| `Tab` | 接受高亮提示 |
| `↑` / `↓` | 在提示列表中导航 |

### 会话管理

| 命令 | 功能 |
|------|------|
| `/new` | 保存当前会话 → 清空 → 新建会话 |
| `/resume <id>` | 从磁盘恢复历史会话 |
| `/sessions` | 列出所有持久化会话 |

### Skill 系统

从 `skills/` 或 `cmd/repl/skills/` 目录加载 `.md` 文件作为 Skill：

```markdown
# translate
将中文翻译为英文，保留 Markdown 格式。
```

调用方式：
```
#translate 你好世界
```

### 审批面板（卡片式）

工具调用或 `ask_approve` 触发时弹出：

```
╭──────────────────────────────────────╮
│  ⚠ 高危操作                          │
│                                      │
│  bash(rm -rf /tmp/cache)             │
│                                      │
│  ▸ 1. 允许执行   放行此操作           │
│    2. 始终允许   记住此选择           │
│    3. 拒绝       禁止执行             │
│                                      │
│  ↑↓ 选择  Enter确认  Esc 取消  28s   │
╰──────────────────────────────────────╯
```

### 插件模式

LLM 可通过 `switch_mode` 工具自主切换工作模式：

| 模式 | 可用工具 |
|------|---------|
| `default` | 全部工具 |
| `read` | grep\*, read_file, glob, git 查询 |
| `write` | write\*, edit\*, read_file, bash |
| `git` | git_\*, bash |
| `shell` | bash |
| `plan` | plan_\* (WorkPlan) |

### 命令

| 命令 | 说明 |
|------|------|
| `/help` | 显示帮助 |
| `/model` | 显示当前模型和 Provider |
| `/pool` | 显示账号池状态 |
| `/history` | 显示历史消息统计 |
| `/trace` | 显示调用追踪树 |
| `/clear` | 清空对话历史 |
| `/new` | 新建会话 |
| `/resume <id>` | 恢复会话 |
| `/sessions` | 列出会话 |
| `/exit` | 退出程序 |

## 项目结构

```
seelex/
├── main.go                 # 生命周期与依赖装配
├── application_adapters.go # 具体领域对象到 application ports 的适配
├── application/            # 无界面的应用核心
│   ├── app.go              # Service、Snapshot 和公开用例
│   ├── ports.go            # Engine/Runtime/Plugin/Skill/Session 窄接口
│   ├── event.go            # EventHub、Subscription、Seq
│   ├── chat.go             # ChatCoordinator 与 Tool hooks
│   ├── command.go          # 实例化命令注册表
│   ├── completion.go       # 命令/工具/Skill 补全
│   ├── input.go            # 原始输入路由
│   └── approval.go         # ApprovalBroker
├── seele.yaml              # 权限规则配置
├── config/                 # LLM 账户配置
├── session/                # 会话管理器
├── skill/                  # Skill 加载器 + 注册表
├── tui/                    # Bubble Tea 前端适配器
│   ├── tui.go              # AppController + Model + 输入事件
│   ├── view.go             # Snapshot 到终端页面
│   ├── state.go            # application.Message 渲染器
│   ├── stream.go           # application.Event 到 tea.Msg
│   ├── styles.go           # 共享样式
│   ├── dialog.go           # Interaction 键盘适配
│   ├── suggest_view.go     # 建议面板渲染
│   ├── splash/             # 💡 启动画面子包
│   │   └── splash.go       #    Gradient + Render
│   └── tui_test.go         # 输入与交互适配测试
└── docs/                   # 架构文档
    ├── architecture-and-flaws.md
    ├── approve-research.md
    ├── context-improvement-plan.md
    └── cad-*.md            # CAD 自动化方案
```

## 改进路线图（来自 docs/）

### 已完成
- TUI 渲染与 application core 分离。
- 命令、补全、ChatStream、工具事件、Plugin/Skill/Session 和审批迁入无界面核心。
- 删除 TUI 包级 channel、registry 和 pending request。
- 增加 application 与 TUI adapter 单元测试。

### 后续
- 增加 JSON-RPC/stdio sidecar，供 Electron 调用 application core。
- 为 Snapshot 增加历史分页和协议版本。
- 提高 application 分支覆盖率并补充端到端审批测试。
- **CAD 自动化**: FreeCAD 执行底座 + MCP 桥接 + JSON 命令栈

## 开发

```bash
# 运行
go run . -c config/account-openai.local.yaml

# 编译
go build -o seelex.exe .

# 检查
go vet ./...

# 测试
go test ./... -v -count=1 -timeout=120s
```

### 编码规范

1. **接口先行** — interface 定义在使用方，不在实现方
2. **不重复造轮子** — 优先使用 Seele 内置能力
3. **依赖方向** — 高层策略 → interface → 低层实现，禁止倒挂
4. **子包原则** — 每个子包只暴露需要的能力，内部状态不导出
5. **设计模式** — 优先使用工厂/策略/装配件，避免单例/原型

## 配置参考

### 环境变量

| 变量 | 说明 |
|------|------|
| `$SEELE_CONFIG` | 配置文件路径（可替代 `-c` 参数） |

### 配置文件路径

默认查找顺序：
1. `-c` 命令行参数
2. `config/account-openai.yaml`（项目目录）

## 许可证

MIT
