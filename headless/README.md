# headless

## 生态位

`headless/` 是无窗口的本地控制面（headlessUI）：在与 GUI 相同的进程装配上，
只绑定 `127.0.0.1` 暴露 `/healthz`、`/rpc`、`/events`，让外部驱动像前端一样调用
门面能力并观察全量事件流。它是 `-frontend headless` 的入口，也是 GUI 进程内
（`SEELEX_HEADLESS_PORT` 设置时）的冒烟/量测接口。

主要调用方：`wiring/bootstrap`（独立 headless 模式）与 `gui`（桌面进程内启动）。

## 职责与非职责

- 做：方法名 → 门面能力的窄映射、参数解码、错误如实返回、事件流订阅与写回。
- 不做：不持有业务状态机、不复制 core 判定、不绕过门面直接 import 存储/引擎实现。

## 目录与文件

| 文件 | 职责 |
|---|---|
| `headless.go` | HTTP 服务、`/rpc` 分派、`/events` 订阅、基础方法表、`Run` / `StartIfRequested` |
| `headless_subagent.go` | `subagent.*` 段（恢复清单 / 派发 / 重跑） |

## 核心实现

- `Application`：本包声明的 17 方法窄契约（接口归调用方所有），由门面
  `application.Service` 结构满足；gui 反过来 import 本包，方向保持单向。
- 可选扩展面：`sessionAwareApplication`、`toolRecordBoardProvider`、
  `roleRPCApplication`、`goalRPCApplication`、`subagentRPCApplication`；
  宿主未实现时对应 RPC 显式报"未装配"，不静默降级。
- `Server`：`NewServer` → `Serve(listener)` → `Shutdown(ctx)`；`StartIfRequested`
  只在环境变量设置时启动。

## 数据流

外部驱动 → `POST /rpc`（`{method,args}`）→ 方法表 → 门面方法 → 结果/错误信封；
`GET /events` 订阅全局事件流（缓冲溢出由 hub 的 `resync.required` 兜底）。

## 依赖方向

`headless → application（门面）+ contract/dto + backend/multiagent/goal（goal 请求形状）`。
禁止 import `gui`、`store` 或引擎实现包。goal 请求形状的契约化（P3）完成后，
这里应改为只 import `contract`。

## 并发与安全

- 只监听回环地址；默认关闭，未设置 `SEELEX_HEADLESS_PORT` 时零开销。
- 每个请求独立解码；服务关闭走 `http.Server.Shutdown`。
- 事件流按订阅缓冲写出，慢消费者不会阻塞业务执行。

## 扩展方式

新增方法：在方法表加一项 + 在对应可选接口声明所需能力；若需要新能力面，
优先在门面已有窄接口上扩展，不在本包新造状态。

## Review 指南

- 方法表是否只调用门面、是否出现 `store.` / 引擎实现的直接 import；
- 未装配能力是否显式报错（而不是返回零值冒充成功）；
- `Application` 是否仍只列真正使用的方法（接口随调用方收窄）。

## 测试与验证

```text
go test ./gui/ -run Headless -count=1    # 测试留在 gui 包，直接驱动本包 Server
go test ./e2e/ -count=1                  # 布局/依赖边界
go build -tags "gui,desktop,production" ./...
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### headless.go

- `func NewServer(app Application) *Server` — NewServer 构造一个尚未 Serve 的控制面（测试可直接在回环监听器上起它）。
- `func (server *Server) Shutdown(ctx context.Context) error` — Shutdown 关闭底层 HTTP 服务（未 Serve 时为空操作）。
- `func Run(app Application) error` — RunHeadless 是 headless 控制面的独立入口（调试/冒烟用，不启动桌面窗口）：
- `func StartIfRequested(app Application) (func(), error)` — StartIfRequested 在设置 SEELEX_HEADLESS_PORT 时启动回环控制面；
- `func (server *Server) Serve(listener net.Listener) error` — Serve 在给定监听器上提供 headless 冒烟接口（/healthz、/rpc、/events）。
- `func (server *Server) serveHealth(writer http.ResponseWriter, _ *http.Request)`
- `func (server *Server) serveRPC(writer http.ResponseWriter, request *http.Request)`
- `func writeRPCError(writer http.ResponseWriter, message string)`
- `func (server *Server) dispatch(method string, args []json.RawMessage) (any, error)` — dispatch 把冒烟驱动的方法调用映射到 Application（含会话级扩展）契约。
- `func decodeHeadlessObject(method string, args []json.RawMessage, destination any) error`
- `func (server *Server) dispatchRole(method string, args []json.RawMessage) (any, error)`
- `func (server *Server) dispatchSchedule(method string, args []json.RawMessage) (any, error)`
- `func (server *Server) dispatchGoal(method string, args []json.RawMessage) (any, error)` — dispatchGoal 把 goal.<method> 透传到 application（当前视图会话）。
- `func waitContext(timeoutSeconds int, fallback time.Duration) (context.Context, context.CancelFunc)` — waitContext 为异步等待类 RPC 构造带护栏的 context：显式 timeoutSeconds
- `func (server *Server) serveEvents(writer http.ResponseWriter, request *http.Request)` — serveEvents 以 SSE 方式推送全量会话事件（含 payload 体积与到达时刻由

### headless_subagent.go

- `func (server *Server) dispatchSubagent(method string, args []json.RawMessage) (any, error)`

