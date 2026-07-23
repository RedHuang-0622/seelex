# 代码变更摘要

## 新增/修改/删除文件

| 文件 | 类型 | 说明 | 设计模式 |
|------|------|------|---------|
| `gui/bridge.go` | 新增 | Application Core 到桌面 GUI 的窄接口和事件桥 | Adapter / Observer |
| `gui/run_wails.go` | 新增 | `gui` build tag 下启动 Wails 窗口 | Factory |
| `gui/run_stub.go` | 新增 | 默认构建下提供明确的 GUI 未启用错误 | Null Object / Build Variant |
| `gui/assets.go`、`gui/frontend/dist/*` | 新增 | Codex 风格三栏界面；Runtime 弹窗、指令模式与原生 SVG 图标控件 | Embed / Component UI |
| `gui/frontend/dist/components.js` | 新增 | 消息/图标/资料源组件；工具 `IN`/`OUT` 双框；真实 ChatState 驱动运行指示和队列卡片 | Presentation Component / Lazy Expansion |
| `gui/frontend/dist/app.js` | 新增 | 运行时保持发送可用，后续输入进入后端队列；同步短状态、占位提示和活动组件 | State Projection |
| `gui/frontend/dist/markdown.js` | 新增 | 无外部依赖的安全 Markdown 渲染组件；支持常用语法与块级 `<think>` 折叠思考过程 | Pure Function / Sanitizing Renderer |
| `gui/frontend/dist/markdown.test.mjs`、`components.test.mjs` | 新增 | Markdown/思考块、状态动效、队列、安全转义和危险链接边界测试 | Contract Test |
| `gui/bridge_test.go` | 新增 | Bridge 转发、事件和资源契约测试 | Fake / Contract Test |
| `application/app.go`、`application/state.go` | 修改 | Snapshot 暴露持久化会话元数据，供 TUI/GUI 共用 | Snapshot / DTO |
| `application/ports.go`、`application_adapters.go` | 修改 | 新增会话历史替换/新建窄接口，完整保留 reasoning 和工具调用上下文 | Adapter / Transactional Replace |
| `application/chat.go`、`application/command.go` | 修改 | 恢复后按选中会话 ID 继续保存，新建会话切换独立 ID | Session Routing |
| `application/chat.go` | 修改 | 队列接续时不再额外插入临时用户消息，避免合并输入重复显示 | Single Source of Truth |
| `main.go` | 修改 | 增加 `-frontend`、`-version`，默认 manual 权限并校验输入 | Strategy |
| `version.go` | 修改 | 可由 linker 注入的版本和默认前端 | Single Source |
| `config/accounts.example.yaml` | 新增 | 唯一公开账户配置模板 | Configuration Template |
| `config/account-pool.yaml` | 删除 | 移除被 Git 跟踪、可能被误填密钥的真实配置文件名 | Security Hardening |
| 构建脚本 / Makefile | 修改/新增 | 配置白名单、正确版本注入、CLI/GUI 归档、附带许可证文档 | Allowlist Packaging |
| `.github/workflows/ci.yml` | 修改 | gofmt、配置白名单、发布安全、漏洞扫描 | Quality Gate |
| `.github/workflows/release.yml` | 新增 | tag 驱动 CLI/Windows GUI 构建、校验和和预发行 | Release Pipeline |
| `LICENSE`、`CHANGELOG.md` | 新增 | MIT 授权与 Alpha 变更记录 | Release Metadata |
| `README.md`、`docs/feature-instrumentation.md` | 修改 | 同步 GUI、权限、版本和已知限制 | Documentation |
| `smoke_test.go` | 修改 | 外部 LLM 测试改为显式 opt-in，缺配置时不再伪装通过 | Explicit Integration Gate |
| `go.mod`、`go.sum` | 修改 | 引入 Wails v2.13.0；升级 gRPC 到安全版本 v1.79.3 | Dependency Management |
| 多个现有 `.go` 文件 | 格式化 | 统一 gofmt，满足新 CI 门禁 | Mechanical Formatting |

## API 变更

| API | 变更 | 兼容性 |
|-----|------|-------|
| CLI `-permission` | 默认 `full_access` → `manual` | 有意的安全行为变化；可显式选择 `full_access` |
| CLI `-frontend` | 新增 `tui` / `gui` | 向后兼容，默认仍为 TUI |
| CLI `-version` | 新增 | 向后兼容 |
| `main.Version` | 常量改为 linker 可注入变量 | 构建脚本已同步 |
| `main.DefaultFrontend` | 新增 linker 可注入变量 | Windows GUI 包默认启动 GUI |
| `gui.Application` | 新增调用方窄接口 | `*application.Service` 直接满足 |
| `application.Snapshot.Sessions` | 新增持久化会话列表 | 向后兼容，新 JSON 字段 |
| `application.ChatEngine.ReplaceHistory` | 新增原子历史替换 | 内部窄接口，启用 TUI/GUI 会话恢复 |
| `application.ChatEngine.StartSession` | 新增独立会话 ID 生成 | 内部窄接口 |
| `gui.AppInfo.Project` | 新增工作区名称、根路径与资料源 | 向后兼容，本地 GUI 元数据 |
| `markdown(value)` | 从内联简易格式化升级为独立安全渲染器的兼容别名 | 保持原导出名与调用方式 |
| `renderMarkdown(value)` | 新增纯函数渲染接口 | 前端内部 API，不影响 Go/Wails Bridge |
| `renderConversationComponent(messages, chat)` | 可选接收 ChatState 并附加运行/队列组件 | 向后兼容，省略第二参数时行为不变 |
| `renderChatActivity(chat)` | 新增纯展示接口 | 只消费 `running/input_queue`，不维护前端队列副本 |

## 设计模式使用

| 模式 | 文件 | 效果 |
|------|------|------|
| Adapter | `gui/bridge.go` | GUI 不依赖 Seele/Engine 深层类型 |
| Observer | `gui/bridge.go` | Application Event 转发为桌面事件 |
| Strategy | `main.go` | 一次装配后选择 TUI 或 GUI |
| Build Variant | `gui/run_*.go` | 默认 CLI 不编译桌面容器，`-tags "gui,desktop,production"` 显式启用 |
| Allowlist | 构建脚本 | 发行包只复制公开模板，阻止本机账户文件泄露 |
| Sanitizing Renderer | `gui/frontend/dist/markdown.js` | 原始 HTML 先转义、危险 URL 协议拒绝，再生成受控标签 |
| State Projection | `gui/frontend/dist/components.js` | 将后端 ChatState 投影为加载动效和可见队列，不复制业务状态 |

## 接口抽象

| 接口 | 实现方 | 使用方 |
|------|-------|-------|
| `gui.Application` | `application.Service` | `gui.Bridge` |
| `emitter` | Wails runtime adapter / test fake | `gui.Bridge` 事件循环 |
| `renderMarkdown(value)` | `gui/frontend/dist/markdown.js` | GUI 消息组件 |
| `renderChatActivity(chat)` | `gui/frontend/dist/components.js` | GUI 会话尾部状态区 |

## 循环依赖检查

- [x] `application` 不依赖 `tui` 或 `gui`
- [x] `tui` 与 `gui` 互不依赖
- [x] `components.js` 单向依赖纯函数 `markdown.js`
- [x] 队列状态由 `application.ChatState` 单向流入 GUI，无前端双写状态
- [x] `go build ./...` 和 `go build -tags "gui,desktop,production" ./...` 均通过

## Commit 建议

未自动提交，建议消息：

```text
feat(gui): add safe alpha desktop frontend

- add Wails GUI bridge and embedded desktop interface
- render conversation Markdown with safe links and escaped raw HTML
- default permissions to manual and validate frontend modes
- harden release packaging against local account leakage
- add versioned prerelease CI and security scanning

Refs: release P0, GUI alpha
```
