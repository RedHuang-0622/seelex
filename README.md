# Seelex — Seele TUI 客户端

**Seelex** 是 [Seele](https://github.com/RedHuang-0622/Seele) 引擎的独立 TUI 客户端，使用 [Bubble Tea](https://github.com/charmbracelet/bubbletea) 框架构建。

与 Seele 是松耦合关系：Seele 负责提供 LLM 调用/工具调度/会话管理，Seelex 负责终端交互体验。

## 架构

```
┌──────────────────────────────────────────┐
│            seelex TUI                     │
│  ┌────────────────────────────────────┐  │
│  │          主 Model (tui.go)          │  │
│  │  ┌──────┐ ┌──────┐ ┌───────────┐  │  │
│  │  │conv  │ │stream│ │suggest.go │  │  │
│  │  │渲染  │ │流处理 │ │建议面板   │  │  │
│  │  └──────┘ └──────┘ └───────────┘  │  │
│  │                                    │  │
│  │  子包模块:                          │  │
│  │  approve/  卡片式审批面板           │  │
│  │  commands/ 命令系统（策略模式）      │  │
│  │  sugg/     智能补全引擎              │  │
│  │  splash/   启动画面艺术字            │  │
│  └────────────┬───────────────────────┘  │
│               │                           │
│               ▼                           │
│  ┌────────────────────────────────────┐  │
│  │         Seele Engine                │  │
│  │  ChatStream / History / Session    │  │
│  │  Storage / Tool / Permission Gate  │  │
│  └────────────────────────────────────┘  │
└──────────────────────────────────────────┘
```

### 架构分层（main.go 8 层装配）

| 层 | 职责 | 对应函数 |
|----|------|---------|
| L1 | LLM 配置加载 + Agent 创建 | `initAgent()` |
| L2 | 工具注册（builtin + switch_mode + ask_approve） | `initTools()`, `registerSwitchMode()` |
| L3 | 存储 + Engine | `initStore()`, `initEngine()` |
| L4 | 会话管理 | `initSessionManager()` |
| L5 | Skill 系统 | `initSkillSystem()` |
| L6 | TUI Model 装配 | `initTUI()` |
| L7 | 命令注册 + 建议同步 | `initCommands()` |
| L8 | Bubble Tea Program 启动 | `startTUI()` |

### 设计模式

| 模式 | 用途 |
|------|------|
| 🏭 **工厂模式** | 创建 Session / Skill / Model 等依赖 |
| 🧩 **装配件模式** | Model 组装 viewport / messages / 提示引擎 |
| ⚔️ **策略模式** | `Command` 接口 + 子包 `commands/` |
| 🧊 **外观模式** | `session.Manager` 薄包装 `storage.Store` |
| 🚪 **权限门控** | `permission.PermissionChecker` + `TUIApprovalGate` |

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
| 输入 `/` | 弹出命令补全列表 |
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
├── main.go                 # 8 层装配入口
├── go.work                 # IDE 跨模块解析
├── seele.yaml              # 权限规则配置
├── config/                 # LLM 账户配置
├── session/                # 会话管理器
├── skill/                  # Skill 加载器 + 注册表
├── tui/                    # TUI 核心
│   ├── tui.go              # 主 Model + Update
│   ├── view.go             # 主 View 组合
│   ├── state.go            # Cell / Conversation / AppState
│   ├── stream.go           # 流式处理 + Engine hooks
│   ├── styles.go           # 共享样式
│   ├── types.go            # 共享类型
│   ├── dialog.go           # 旧版审批 + 选择器
│   ├── command.go          # 命令注册入口
│   ├── suggest_view.go     # 建议面板渲染
│   ├── approve/            # 💡 审批面板子包
│   │   ├── approve.go      #    Manager + Ask + 桥接
│   │   ├── state.go        #    State 结构体
│   │   ├── view.go         #    卡片式渲染
│   │   └── styles.go       #    风险等级配色
│   ├── commands/           # 💡 命令系统子包
│   │   ├── commands.go     #    Command 接口 + 注册表
│   │   └── builtin.go      #    10 个内置命令
│   ├── sugg/               # 💡 建议引擎子包
│   │   └── engine.go       #    Engine (补全/过滤)
│   ├── splash/             # 💡 启动画面子包
│   │   └── splash.go       #    Gradient + Render
│   └── stream/             # 💡 工具事件类型
│       └── event.go        #    Event 类型
└── docs/                   # 架构文档
    ├── architecture-and-flaws.md
    ├── approve-research.md
    ├── context-improvement-plan.md
    └── cad-*.md            # CAD 自动化方案
```

## 改进路线图（来自 docs/）

### Phase 2 — 提升可测试性（待做）
- **ARC-014**: 为 Model 依赖引入接口（EngineInterface / ChatClientInterface / AgentInterface）
- **ARC-006 + ARC-017**: 补充单元测试（工具注册 + TUI model）

### Phase 3 — 代码组织优化 ✅
- `main()` 拆分为独立 init 函数
- Model 子包拆分（approve/splash/sugg/commands）

### Phase 4 — 代码质量（进行中）
- **ARC-020/021**: 简化命令注册，解耦 Model
- **Context 改进**: 上下文压缩、双向合并、快照验证
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
