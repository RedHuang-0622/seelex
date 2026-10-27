# application/history

## 生态位

`application/history` 承载可见对话历史窗口的前置/回到最新分页决策。真实调用方是
`application/assembly.Service` 的 `LoadMoreHistory` / `LoadLatestHistory` 兼容入口。

## 职责与非职责

职责：

- 计算一页的窗口大小与起始偏移；
- 内容 LRU 卸载后先回读再分页；
- 决定何时短路“加载更早”与“回到最新”；
- 固定 `InstallPrepend` / `InstallReplace` 两种窗口安装模式。

非职责：

- 不直接读存储、不执行 Git/Runtime I/O；
- 不拥有可见窗口、Snapshot 或内容 LRU；
- 不导入 `application/assembly`。

## 文件结构

- `application.go`：`Page`、`InstallMode`、`Deps`、`Application` 与构造器。
- `application_test.go`：窗口计算、回读、短路、安装模式和错误传播测试。

## 核心实现

`Page` 同时保留可见空间和持久化未过滤空间：`Rows`/`Start` 用于可见窗口，
`RawRows`/`DiskTotal` 用于判断发布点。`Application` 只负责分页顺序，所有磁盘读取
与窗口安装均由 narrow deps 执行。

`WindowAssembler` 是本包定义的消费方端口，包含 `ReplaceTail`、`Prepend`、
`DurableRows`、`IndexOfSameRow`、`ContainsSameRow`、`HasRowIdentity`。后两项来自
真实冷读扩窗调用，不能在 core 重写行身份判断。实现由
`middle/switch/history.Assembler` 唯一提供，Wiring 在构造 core 时注入。

## 数据流

兼容 `Service` 入口 → `history.Application` → `ReadViewState` → 外部冷读回调 →
`LoadEarlierVisiblePage` / `LoadConversationTail` → `InstallVisibleHistory`。

## 依赖方向

只依赖 `contract/protocol`、自身端口和标准库；禁止依赖
`application/assembly`、`store`、`backend` 或 GUI/TUI。

## 并发与错误

本包不持有锁；`ReadViewState` 与冷读回调由宿主决定锁边界。查询路径不获取共享写锁，
安装模式由宿主在短临界区内完成。所有读取错误原样返回。

## 扩展方式

新增分页能力时应扩展 `Deps` 的真实读回调，不得把持久化实现搬入本包，也不得在 core
复制第二个窗口决策函数。

## Review 指南

- 一页大小是否仍由 `HistoryWindow` 钳制；
- 内容卸载后是否先回读再取 offset；
- 历史贴尾时是否跳过不必要的冷读；
- `Page` 的未过滤与可见坐标是否仍分开；
- 是否 import `application/assembly`。

## 测试与验证

```text
go test ./application/history -count=1
go test ./application/assembly -run 'LoadMoreHistory|LoadLatestHistory|History' -count=1
go test ./application/... -count=1
```

## 文件与函数索引

### `application.go`

- `type WindowAssembler interface` — 历史窗口装配的消费方端口（六方法，仅纯消息值）。
- `func (page Page) ReachesPublishedTail() bool` — 判断页是否到达持久化发布尾。
- `func New(deps Deps) *Application` — 构造历史分页应用。
- `func (application *Application) LoadMoreHistory(limit int) error` — 前置更早一页。
- `func (application *Application) LoadLatestHistory() error` — 回到最新窗口。

### `application_test.go`

测试文件按仓库 README 规范不展开函数索引。
