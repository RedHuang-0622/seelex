# Seelex 预发行 P0 与 GUI 实现方案

## 设计目标

1. 产出可公开分发的 `v0.1.0-alpha.1`，发行包不包含任何本机密钥。
2. 默认权限改为 `manual`，危险工具必须显式审批，`full_access` 仅由用户主动开启。
3. 建立单一、可注入、可显示的版本源和 tag 驱动发布流程。
4. 新增与 `tui/` 同级的 `gui/`，复用 `application.Service` 的 Snapshot、Event 和 Interaction，不在前端复制业务状态机。
5. TUI 继续作为默认和回退入口；GUI 通过显式启动参数开启。

## 设计模式选择

| 模式 | Go 实现 | 应用位置 | 理由 |
|------|---------|---------|------|
| Adapter | `gui.Bridge` 包装窄接口 | `gui/bridge.go` | 将 context、channel event 转换为 GUI 可调用方法和事件 |
| Strategy | `Frontend`/启动函数选择 | `main.go` | 同一 application core 在 TUI/GUI 间切换 |
| Observer | 订阅 `application.EventHub` | `gui/bridge.go` | 将流式消息、工具调用和审批推送给 Web UI |
| Embed | `embed.FS` | `gui/assets.go` | 将构建后的 Web 资源打进桌面程序 |
| Factory | GUI 配置构造 | `gui/gui.go` | 集中窗口、资源和生命周期配置 |

## 方案对比

| 维度 | 方案 A：同一程序 `-frontend tui/gui` | 方案 B：抽取 bootstrap，生成两个独立程序 |
|------|-----------------------------------|----------------------------------------|
| 耦合度 | 中低；入口知道两个前端 | 最低；两个 cmd 只依赖共享装配包 |
| 内聚性 | GUI 包职责清楚，入口略重 | 最好，但需要迁移 400+ 行装配代码 |
| 可测试性 | Bridge 可独立测试 | Bridge 与 bootstrap 都可独立测试 |
| 实现成本 | 低，适合 Alpha | 高，涉及较大入口重构 |
| 改动面 | 小 | 大 |
| 可回滚性 | 高，删除 GUI 分支即可 | 中，回滚涉及装配迁移 |
| CLI 体积 | 会包含 GUI 容器依赖 | TUI/GUI 可分别控制体积 |

## 推荐：方案 A

Alpha 阶段采用同一程序显式选择前端，优先验证 GUI 业务闭环；不在本轮同时重构依赖装配。GUI 稳定后再把装配迁移到 `internal/bootstrap` 并拆成独立发行物。

最大风险是 GUI 依赖增加普通二进制体积。通过保持 TUI 默认入口、GUI 显式开启和后续可拆分的 Bridge 边界控制风险。

## 循环依赖检查

```text
main ──→ application
  ├────→ tui ──→ application
  └────→ gui ──→ application

application 不依赖 tui/gui
tui 与 gui 互不依赖
```

无新增循环依赖。

## 核心接口

接口定义在 GUI 调用方，便于 fake 测试：

```go
package gui

type Application interface {
    Snapshot() application.Snapshot
    Subscribe(buffer int) application.Subscription
    Submit(context.Context, string) error
    CancelChat(string) bool
    ResolveInteraction(context.Context, string, string) error
    SelectAccount(context.Context, string) error
    SwitchEffort(context.Context, string) error
    SwitchPlugin(context.Context, string) error
    LoadMoreHistory(int) error
}
```

`Bridge` 对 Web UI 暴露同步命令方法；异步状态统一通过 `seelex:event` 推送，前端收到事件后拉取 Snapshot，第一版优先保证一致性。

## GUI 最小产品范围

- 左栏：会话/Plugin/账户信息。
- 主区：用户、Assistant、工具调用消息流，支持 Markdown 和流式更新。
- 右栏：当前模型、Effort、Plan、可见工具与 Skill。
- 底栏：多行输入、发送、停止。
- 审批：Interaction 模态卡片，支持批准/拒绝/始终允许。
- 历史：加载更早消息。
- 状态：连接、运行、错误和重同步提示。

不在本轮实现内置终端、代码编辑器、完整 CAD 3D 工作台和远程 Web 服务。

## 实现步骤

| # | 步骤 | 文件 | 模式 |
|---|------|------|------|
| 1 | 修复配置模板与安全打包 | `config/`, scripts, Makefile | 安全白名单 |
| 2 | 修复权限默认值与模式校验 | `main.go`, tests | Strategy |
| 3 | 统一版本和发布元数据 | `version.go`, LICENSE, CHANGELOG, README | Single Source |
| 4 | 增加 CI/Release 门禁 | `.github/workflows/*` | Pipeline |
| 5 | 定义 GUI Application 窄接口与 Bridge | `gui/bridge.go` | Adapter/Observer |
| 6 | 增加窗口启动与资源嵌入 | `gui/gui.go`, `gui/assets.go` | Factory/Embed |
| 7 | 实现 Web UI | `gui/frontend/` | Component UI |
| 8 | 入口增加前端选择 | `main.go` | Strategy |
| 9 | 补 Bridge、权限、版本和打包测试 | `*_test.go`, scripts checks | Fake/Contract Test |
| 10 | 同步文档和功能打点 | README、docs | Documentation |

## 测试策略

- 单元：权限模式解析、版本输出、GUI Bridge 命令转发、Snapshot 读取。
- 集成：fake application 推送 Event，验证 GUI 订阅和 Shutdown 不泄漏。
- 静态：`gofmt`、`go vet`、`go build ./...`。
- 全量：`go test ./... -count=1`；CGO 可用环境执行 `-race`。
- 前端：无 Node 依赖的首版采用原生 ES modules；验证嵌入资源存在及关键 DOM 标识。
- 发布安全：构建临时归档后扫描，确保不存在 `*.local.yaml`、`api_key` 实值和账户池文件。

## 回滚方案

- GUI 可通过删除 `gui/` 和入口 GUI 分支回滚，不影响 application/TUI。
- 默认权限若造成紧急兼容问题，可由用户显式 `-permission full_access`，不回退安全默认值。
- Release workflow 独立于 CI，可单文件禁用，不影响日常构建。
- 发布脚本仅使用白名单复制；不恢复整目录复制。

## 验收标准

- 干净 clone 能看到唯一的公开配置示例，发布包不含开发机账户文件或可直接误提交的真实配置文件。
- 默认启动使用 manual 权限，非法权限值明确报错。
- `-version` 输出 tag 注入版本。
- TUI 行为保持可用；GUI 能完成输入→流式响应→工具事件→审批→停止的主链路。
- `go build ./...`、`go vet ./...`、`go test ./...` 和 gofmt 门禁通过。
