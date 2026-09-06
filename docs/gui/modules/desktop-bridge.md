# Desktop Bridge 与生命周期模块详细设计

> 状态：已实现（Wails 进程内 adapter）
> 总体架构：[`../architecture.md`](../architecture.md)

## 1. 职责与边界

Bridge 是 Application Core 与 Wails/WebView 的唯一桌面适配层。它负责：

- 定义 GUI 需要的最窄 Application 接口；
- 把 Go 方法绑定为前端可调用 API；
- 把 EventHub subscription 转发为 Wails events；
- 管理订阅 goroutine 的启动、取消和关闭；
- 发现只读项目资料元数据；
- 根据 build tags 选择真实 Wails 或 stub。

Bridge 不解释 Chat、Plugin、Session 或审批业务，不缓存业务 Snapshot。

## 2. 调用方接口

实现位置：`gui/bridge.go:19-31`。

接口定义在使用方 `gui` 包，使 tests 可以注入 fake application，而不构造 Seele runtime。公开能力分为：

| 类别 | 方法 |
|------|------|
| 状态 | `Snapshot`、`Subscribe`、`Info` |
| 对话 | `Submit`、`CancelChat`、`LoadMoreHistory` |
| 交互 | `ResolveInteraction` |
| Runtime | `SelectAccount`、`SwitchEffort`、`SwitchPlugin` |
| 指令 | `Suggestions` |

新增 GUI 功能时只有 Core 已存在稳定业务动作才扩展此接口。

## 3. 生命周期

实现位置：`gui/bridge.go:109-162`。

### start

1. Bridge mutex 防止重复启动；
2. 从 Wails startup context 派生 cancel context；
3. 以 256 buffer 订阅 EventHub；
4. goroutine 首先发送 `seelex:ready` Snapshot；
5. 循环转发 `seelex:event`，直到 context 或 subscription 关闭。

### stop

1. 持锁交换 running/cancel/context 状态；
2. 锁外 cancel context；
3. 关闭 subscription；
4. 等待 goroutine 结束。

`stop` 必须幂等，且不能在持 Bridge mutex 时等待 goroutine。

## 4. Wails 组装

实现位置：`gui/run_wails.go:15-55`。

- `//go:build gui` 选择真实桌面入口；
- embedded FS 子目录 `frontend/dist` 作为 AssetServer；
- 默认窗口 1440×900，最小 980×640；
- `OnStartup` 启动事件泵；
- `OnShutdown` 停止并等待；
- `Bind` 只暴露 Bridge。

无 `gui` tag 时，`gui/run_stub.go:7-12` 返回明确的 tags 构建提示。这样默认 TUI 构建不需要桌面 WebView 链接环境。

### 4.1 Headless 冒烟接口（headlessUI 设计）

实现位置：`gui/headless.go`；装配点 `gui/run_wails.go` 的 `Run` 开头。

桌面 GUI 在真实窗口之外提供一个**默认关闭、仅回环**的本地控制面，让外部驱动
像前端一样驱动同一份 Application Core，用于多会话/长会话冒烟与时间线热力
分析：

- 开关：环境变量 `SEELEX_HEADLESS_PORT=<port>`；未设置时零开销（生产双击
  路径不受影响）。服务只绑定 `127.0.0.1`。
- 独立调试入口：`-frontend headless` 无窗口装配同一 Application 并启动该
  控制面（不依赖 WebView2/Wails，桌面进程/CI 均可驱动；需要
  `SEELEX_HEADLESS_PORT`）。
- `/rpc`：JSON-RPC 风格方法面，映射到 Bridge 同源的窄契约子集——
  `Snapshot` / `PerfStats` / `Submit` / `BeginNewSession` / `ResumeSession` /
  `ForkSessionLatest` / `CancelChat` / `LoadMoreHistory` /
  `ResolveInteraction` / `WaitIdle` / `WaitCatalogRefresh`，以及会话级扩展
  `ListSessions` / `SnapshotOf` / `ActivateSession`（宿主具备时）。命令全部
  走 application core，不新增业务状态机。`WaitIdle` 与 `WaitCatalogRefresh`
  是异步冒烟驱动的确定性等待口（可传超时秒数；缺参/0 回退内置护栏），分别
  等待全部已接受 chat 收敛与会话目录 worker 覆盖本次命令变更。
- `/events`：SSE 全量会话事件流（含 payload 体积等公开元数据，不含会话
  正文），驱动侧按到达时刻打点即可得到事件速率/阶段耗时热力图。
- `/healthz`：进程存活探针。

安全边界：回环绑定 + 环境变量显式开关；不暴露账号配置、命令行、工具输出等
私有内容；事件流不重复携带对话正文。

## 5. 项目资料发现

实现位置：`gui/bridge.go:72-107`。

`discoverProject` 只检查固定候选项：README、CHANGELOG、seele.yaml、账户模板、plugins 和 docs。它不会读取文件内容，不遍历用户目录，也不会把真实账户配置列为资料源。

输出路径统一为 slash，供前端展示；路径仍以传入 ProjectRoot 为边界。

## 6. 请求 context

实现位置：`gui/bridge.go:164-207`。

绑定方法使用 Wails 生命周期 context。Bridge 尚未 start 时回退 `context.Background()`，保证单元测试和启动边界调用不会 panic。Shutdown 后新请求不应由 UI 发出；Core 自身仍负责 closed 状态保护。

## 7. 错误策略

| 场景 | 行为 |
|------|------|
| app 为 nil | `NewBridge` 返回错误 |
| title 为空 | 使用 `Seelex` |
| project root 无法绝对化 | 使用 clean 后的输入路径 |
| 重复 start/stop | 幂等返回 |
| embedded FS 子目录失败 | `Run` 返回错误，不启动空窗口 |
| 无 gui build tag | stub 返回包含正确 tags 的错误 |

## 8. 自动化证据

- `gui/bridge_test.go:47-94`：构造、Info、项目资料发现。
- `gui/bridge_test.go:96-189`：绑定委托、事件转发、生命周期。
- `gui/bridge_test.go:191-205`：所有嵌入式前端模块存在。
- `gui/headless_test.go`：healthz、RPC 方法面（含参数/未知方法错误）与
  SSE 事件流契约。
- `.github/workflows/ci.yml:50-52`：Windows production tags 编译。
- `.github/workflows/ci.yml:83-110`：Application/Bridge contract job。

## 9. 审查清单

- Bridge 方法是否只是薄适配，没有复制业务判断？
- 新方法是否加入调用方接口并提供 fake test？
- goroutine 是否始终受 context、subscription 和 WaitGroup 管理？
- 资料发现是否可能暴露本地真实配置或越过 ProjectRoot？
- build tag 错误是否仍给出可执行的构建命令？
