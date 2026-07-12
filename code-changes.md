# 代码变更摘要

## 架构概览

```
seelex/
├── main.go                    ─── 入口，装配件模式组装所有依赖
├── tui.go                     ─── 主模型（装配件），组合各子组件
├── tui_styles.go              ─── 样式定义（集中管理）
├── tui_commands.go            ─── 命令策略模式（/help, /new, /resume 等）
├── tui_tools_panel.go         ─── 提示引擎（/ 命令提示, @ 工具提示, # Skill 提示）
├── session/
│   └── manager.go             ─── 会话管理薄包装（包装 Seele storage.Store）
├── skill/
│   └── skill.go               ─── Skill 加载器 + 注册表
├── config/
│   ├── account-openai.yaml    ─── 单账号配置
│   └── account-pool.yaml      ─── 多账号池配置
└── .github/workflows/
    └── ci.yml                 ─── GitHub Actions CI
```

## 新增/修改文件

| 文件 | 类型 | 说明 | 设计模式 |
|------|------|------|---------|
| main.go | 重写 | 7 层装配：依赖→工具→存储→会话→Skill→命令→TUI | 装配件 + 工厂 |
| tui.go | 重写 | 主模型含 `suggEng`/`sessionMgr`/`skillReg` 子组件 | 装配件模式 |
| tui_styles.go | 新增 | 全局样式常量 | — |
| tui_commands.go | 新增 | 命令接口 + 10 个具体命令 | 策略模式 |
| tui_tools_panel.go | 新增 | 提示引擎 + 提示面板渲染 | — |
| session/manager.go | 重写 | 薄包装 Seele `storage.Store` | 外观模式 |
| skill/skill.go | 新增 | Skill 类型 + Loader + Registry | 工厂 + 策略容器 |
| config/account-pool.yaml | 新增 | 多账号池示例 | 外部化配置 |
| .github/workflows/ci.yml | 新增 | 跨平台 CI（Linux/Win/Mac） | — |
| session/store.go | 删除 | 用 Seele 内置 `storage.Store` 替代 | 不重复造轮子 |

## 设计模式使用

| 模式 | 文件 | 效果 |
|------|------|------|
| 🏭 **工厂模式** | `main.go`, `skill/skill.go` | NewLoader/NewRegistry/NewManager 工厂方法创建依赖 |
| 🧩 **装配件模式** | `tui.go` | 主 model 组装 viewport/messages/suggEng/sessionMgr |
| ⚔️ **策略模式** | `tui_commands.go` | Command 接口 + 10 个策略实现 |
| 🎯 **策略容器** | `tui_commands.go` | globalCommands 注册表 |
| 🧊 **外观模式** | `session/manager.go` | 薄包装 Seele storage.Store |

## 已避免的模式
| 模式 | 原因 |
|------|------|
| ❌ 单例模式 | 所有依赖通过工厂创建 + 参数传递 |
| ❌ 原型模式 | 无克隆需求 |

## 用户交互增强

| 触发器 | 效果 |
|--------|------|
| 输入 `/` | 弹出命令补全列表（如 `/help`, `/new`, `/resume`） |
| 输入 `@` | 弹出可见工具列表（如 `@grep`, `@read_file`） |
| 输入 `#` | 弹出已加载 Skill 列表 |
| Tab | 接受当前高亮提示 |
| ↑/↓ | 在提示列表中导航 |

## 会话管理

| 命令 | 功能 |
|------|------|
| `/new` | 保存当前会话 → 清空历史 → 新建 |
| `/resume <id>` | 从磁盘加载历史会话 |
| `/sessions` | 列出所有持久化会话 |
| 自动保存 | 新建会话时异步持久化 |

## Skill 系统
- 从 `skills/` 或 `cmd/repl/skills/` 目录加载 `.md` 文件
- 注册表存储，`#skill_name` 触发调用
- 支持传递参数：`#skill_name arg1 arg2`

## 循环依赖检查
- [x] 确认无循环依赖（所有子包仅单向依赖根包）
- [x] session/ 仅依赖 Seele storage
- [x] skill/ 无外部依赖

## 编译验证
- [x] `go build ./...` — 通过
- [x] `go vet ./...` — 通过
- [ ] `go test ./...` — 待运行

## API 兼容性
| 变更 | 兼容性 |
|------|--------|
| `initialModel` 签名扩展（新增 sessionMgr, skillReg 参数） | 破坏性变更 ✅ main.go 已同步更新 |
| `executeCommand` 改为包级函数 | 兼容 |
| `messageView` / `streamChunk` 定义不变 | 兼容 |
