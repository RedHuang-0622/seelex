# Desktop Bridge 与生命周期模块详细设计

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
- `.github/workflows/ci.yml:50-52`：Windows production tags 编译。
- `.github/workflows/ci.yml:83-110`：Application/Bridge contract job。

## 9. 审查清单

- Bridge 方法是否只是薄适配，没有复制业务判断？
- 新方法是否加入调用方接口并提供 fake test？
- goroutine 是否始终受 context、subscription 和 WaitGroup 管理？
- 资料发现是否可能暴露本地真实配置或越过 ProjectRoot？
- build tag 错误是否仍给出可执行的构建命令？
