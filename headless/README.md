# headless

## 生态位

`headless/` 是无窗口的本地控制面（headlessUI）：在与 GUI 相同的进程装配上，
只绑定 `127.0.0.1` 暴露 `/healthz`、`/rpc`、`/events`，让外部驱动像前端一样调用
owner 用例并观察全量事件流。它是 `-frontend headless` 的入口，也是 GUI 进程内
（`SEELEX_HEADLESS_PORT` 设置时）的冒烟/量测接口。

主要调用方：`wiring/bootstrap`（独立 headless 模式与 GUI 进程内启动）。

## 职责与非职责

- 做：RPC 方法名 → owner 端口的窄映射、参数解码、错误如实返回、事件流订阅与写回。
- 不做：不持有业务状态机、不复制 core 判定、不 import 存储/引擎实现包，
  也不再经由任何 application 级宽门面取能力。

## 目录与文件

| 文件 | 职责 |
|---|---|
| `headless.go` | HTTP 服务、`/rpc` 分派、`/events` 订阅、基础方法表、`Run` / `StartIfRequested` |
| `headless_subagent.go` | `subagent.*` 段（恢复清单 / 派发 / 重跑） |
| `dependencies.go` | 本包自己的 consumer-owned 窄端口（按 owner 切）与 `Dependencies` 装配面 |

## 核心实现

- `Dependencies`：控制面需要的全部应用面，一行一个 owner（`Events` / `Snapshot` /
  `Turn` / `Submission` / `Draft` / `Resume` / `Fork` / `Interaction` / `History` /
  `Catalog` / `Workspace` / `Policy` / `Perf` / `ToolRecords` / `Roles` / `Goal` /
  `Subagents`）。端口方法名逐字等于 owner 的方法名，owner 指针在装配处结构满足端口，
  因此本包既没有适配器，也没有 `_ X = (*impl)(nil)` 一类的编译期断言。
- 全部字段都是骨架：有无只由装配表说话，缺 owner 即装配错误。旧形态用 9 处运行时类型断言
  探测 5 个可选面（`server.app.(roleRPCApplication)`、`.(toolRecordBoardProvider)` 等）；
  生产路径上 `headless.Application` 的唯一实现者是兼容门面，它实现全部 5 个面（如
  `application/compat/frontend.go:750,775`），因此断言在生产中恒为真。false 分支只被测试里
  刻意「只实现一部分面」的假实现触达，且没有任何用例断言那条「未装配」错误 —— 所以这层按
  不可达错误面删除，不复制成新端口。
- `Server`：`NewServer(deps)` → `Serve(listener)` → `Shutdown(ctx)`；`StartIfRequested`
  只在环境变量设置时启动。
- `/rpc` 方法名是对外契约：Go 侧调用改落到哪个 owner 不改变线上名字，外部驱动无需改动。

## 数据流

外部驱动 → `POST /rpc`（`{method,args}`）→ 方法表 → owner 端口 → 结果/错误信封；
`GET /events` 订阅全局事件流（缓冲溢出由 hub 的 `resync.required` 兜底）。

## 依赖方向

`headless → application（Snapshot / Subscription / PerfStats 等 DTO 类型）
+ contract/dto + kernel/goal（goal 请求形状）`。owner 由 `wiring/bootstrap` 的装配表
注入，本包不 import `gui`、`application/assembly`、`store` 或引擎实现包。
goal 请求形状的契约化（P3）完成后，这里应改为只 import `contract`。

## 并发与安全

- 只监听回环地址；默认关闭，未设置 `SEELEX_HEADLESS_PORT` 时零开销。
- 每个请求独立解码；服务关闭走 `http.Server.Shutdown`。
- 事件流按订阅缓冲写出，慢消费者不会阻塞业务执行。

## 扩展方式

新增 RPC：在 `dispatch` 表加一行，落到已有 owner 端口的方法；需要新能力面时，
在 `dependencies.go` 按 owner 声明端口与 `Dependencies` 字段，并在装配表
`wiring/bootstrap/frontend_ports.go` 补一行交出该 owner 指针。RPC 线上名字一经发布
就是对外契约，只增不改；不在本包新造状态或第二份判据。

## Review 指南

- 方法表是否只调用 owner 端口，是否出现 `store.` / 引擎实现 / `assembly` 的直接 import；
- 是否重新长出运行时类型断言或判空分支：能力有无只在装配表表达，端口字段是骨架；
- 端口方法名是否仍逐字等于 owner 的方法名（出现改名就说明中间多了一层转发）；
  单个端口是否只对一个 owner（跨两个 owner 就需要拆分，否则得加缝合层）；
- 未实现的分派是否落到 `default` 分支显式报「未知 … 方法」，而不是返回零值冒充成功。

## 测试与验证

```text
go test ./gui/ -run Headless -count=1    # 测试留在 gui 包，直接驱动本包 Server
go test ./e2e/ -count=1                  # 布局/依赖边界
go build -tags "gui,desktop,production" ./...
```

用例的替身在 `gui/headless_ports_test.go`：一个类型只满足一个端口，用例装配哪几个
owner 就填 `headless.Dependencies` 的哪几个字段（没有用例驱动的端口不造 fake）。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### headless.go

- `func NewServer(deps Dependencies) *Server` — NewServer 构造一个尚未 Serve 的控制面（测试可直接在回环监听器上起它）。
- `func (server *Server) Shutdown(ctx context.Context) error` — Shutdown 关闭底层 HTTP 服务（未 Serve 时为空操作）。
- `func Run(deps Dependencies) error` — RunHeadless 是 headless 控制面的独立入口（调试/冒烟用，不启动桌面窗口）：
- `func StartIfRequested(deps Dependencies) (func(), error)` — StartIfRequested 在设置 SEELEX_HEADLESS_PORT 时启动回环控制面；
- `func (server *Server) Serve(listener net.Listener) error` — Serve 在给定监听器上提供 headless 冒烟接口（/healthz、/rpc、/events）。
- `func (server *Server) serveHealth(writer http.ResponseWriter, _ *http.Request)`
- `func (server *Server) serveRPC(writer http.ResponseWriter, request *http.Request)`
- `func writeRPCError(writer http.ResponseWriter, message string)`
- `func (server *Server) dispatch(method string, args []json.RawMessage) (any, error)` — dispatch 把冒烟驱动的方法调用映射到装配表交出的 owner 端口。
- `func decodeHeadlessObject(method string, args []json.RawMessage, destination any) error`
- `func (server *Server) dispatchRole(method string, args []json.RawMessage) (any, error)`
- `func (server *Server) dispatchSchedule(method string, args []json.RawMessage) (any, error)`
- `func (server *Server) dispatchGoal(method string, args []json.RawMessage) (any, error)` — dispatchGoal 把 goal.<method> 透传到 goal owner（当前视图会话）。
- `func waitContext(timeoutSeconds int, fallback time.Duration) (context.Context, context.CancelFunc)` — waitContext 为异步等待类 RPC 构造带护栏的 context：显式 timeoutSeconds
- `func (server *Server) serveEvents(writer http.ResponseWriter, request *http.Request)` — serveEvents 以 SSE 方式推送全量会话事件（含 payload 体积与到达时刻由

### headless_subagent.go

- `func (server *Server) dispatchSubagent(method string, args []json.RawMessage) (any, error)`

