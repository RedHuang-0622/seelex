# Seelex — Seele TUI 客户端

**Seelex** 是 [Seele](https://github.com/RedHuang-0622/Seele) 引擎的独立 TUI 客户端，使用 [Bubble Tea](https://github.com/charmbracelet/bubbletea) 框架构建。

与 Seele 是松耦合关系：Seele 负责 LLM 调用/工具调度/会话管理，Seelex 负责终端交互体验。

## 架构

```
┌─────────────────────────────────┐
│          seelex TUI             │
│  ┌───────────────────────────┐  │
│  │  消息列表 (viewport)      │  │
│  │  You: ...                 │  │
│  │  Seele: ...               │  │
│  ├───────────────────────────┤  │
│  │ > 输入框                    │  │
│  │ ▸ /help    显示帮助信息    │  │  ← 智能提示
│  ├───────────────────────────┤  │
│  │ round-robin  tok:1234  3s │  │  ← 状态栏
│  └───────────────────────────┘  │
│           │ calls               │
│           ▼                     │
│  ┌───────────────────────────┐  │
│  │         Seele Engine       │  │
│  │  ChatStream / History /   │  │
│  │  Session / Storage / Tool │  │
│  └───────────────────────────┘  │
└─────────────────────────────────┘
```

### 设计模式

| 模式 | 用途 |
|------|------|
| 🏭 **工厂模式** | 创建 Session / Skill / Model 等依赖 |
| 🧩 **装配件模式** | Model 组装 viewport / messages / 提示引擎 |
| ⚔️ **策略模式** | Command 接口 + 10 个命令实现 |
| 🧊 **外观模式** | `session.Manager` 薄包装 `storage.Store` |
| ❌ 无单例/原型 | 所有依赖通过工厂 + 参数传递 |

## 快速开始

### 前提
- Go ≥ 1.25

### 安装

```bash
git clone https://github.com/RedHuang-0622/seelex.git
cd seelex
```

### 配置

复制示例配置并填入 API Key：

```bash
cp config/account-openai.yaml config/account-openai.local.yaml
# 编辑 config/account-openai.local.yaml 填入 API Key
```

配置文件格式：

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

### 编译

```bash
go build -o seelex.exe .
```

## 功能

### 智能提示（类似 Claude Code）

| 触发器 | 效果 |
|--------|------|
| 输入 `/` | 弹出命令补全列表 |
| 输入 `@` | 弹出当前可用工具列表 |
| 输入 `#` | 弹出已加载的 Skill 列表 |
| `Tab` | 接受高亮提示 |
| `↑` / `↓` | 在提示列表中导航 |

### 会话管理

| 命令 | 功能 |
|------|------|
| `/new` | 保存当前会话 → 清空 → 新建会话 |
| `/resume <id>` | 从磁盘恢复历史会话 |
| `/sessions` | 列出所有持久化会话 |

> 会话自动持久化到 `~/.seele/sessions/`，使用 Seele 内置分片存储引擎。

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

### 插件模式

LLM 可通过 `switch_mode` 工具自主切换工作模式，限制可用工具集：

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

### 状态栏

```
round-robin [read]  tok:1234  3s
├─ Provider          ├─ 插件   ├─ Token数 ├─ 耗时
```

## 键盘绑定

| 按键 | 行为 |
|------|------|
| 普通字符 | 输入 |
| `Enter` | 发送消息 |
| `Backspace` | 删除上一个字符 |
| `Tab` | 接受提示建议 |
| `↑` / `↓` | 提示列表导航 |
| `Ctrl+C` / `Ctrl+D` | 退出 |

## 项目结构

```
seelex/
├── main.go                 # 入口，装配件模式组装所有依赖
├── tui.go                  # 主模型（装配件）
├── tui_styles.go           # 样式定义
├── tui_commands.go         # 命令策略模式
├── tui_tools_panel.go      # 提示引擎
├── session/
│   └── manager.go          # 会话管理薄包装
├── skill/
│   └── skill.go            # Skill 加载器 + 注册表
├── config/
│   ├── account-openai.yaml # 单账号配置
│   └── account-pool.yaml   # 多账号池配置
└── .github/workflows/
    └── ci.yml              # GitHub Actions CI
```

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

# 清理模块缓存
go clean -modcache && go mod tidy
```

### 编码规范

1. **接口先行** — interface 定义在使用方，不在实现方
2. **不重复造轮子** — 优先使用 Seele 内置能力
3. **依赖方向** — 高层策略 → interface → 低层实现，禁止倒挂
4. **无全局状态** — 包级 var 仅允许 `regexp.MustCompile` / `sync.Pool`
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
