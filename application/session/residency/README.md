# application/session/residency

## 生态位

`application/session/residency` 拥有两层进程内驻留状态：引擎 bundle 的驻留顺序
和可见正文的加载顺序。主要调用方是 `application/core` 的 Session 生命周期、
冷恢复和查询装配；纯窗口/驱逐判据仍来自 `middle/residency`。

## 职责

- 维护 resident engine 与 loaded content 两个 LRU 顺序；
- 在 `Core.ViewMu` 内取一致的占用/受保护快照，锁外交给 Middle 判据；
- 执行内容 flush、LRU 驱逐、正文卸载与目录刷新；
- 维护唯一受保护集判据：当前视图、待审批、忙会话和 restoring；
- 在正文自动卸载后触发 cold reload。

## 非职责

- 不拥有 `SessionUnit`、Snapshot 或 View；
- 不实现 Middle 的窗口、排序和预算算法；
- 不打开 Store、Engine 或历史文件句柄；
- 不 import `application/core`，不持有完整 `Service`。

## 文件结构

- `runtime.go`：两层 LRU 状态、快照、驱逐、正文回读入口与受保护集；
- `snapshot_test.go`：快照不别名底层顺序的并发回归；

## 核心实现

`RuntimeDeps` 只接收共享状态内核、Session/View owner、Middle residency port、
上限读数和 flush/release/reload/catalog 回调。`Runtime` 自己拥有两个顺序切片；
所有读写与 core 的共享视图锁约定一致，Middle 判据在锁外运行。

## 数据流

Session 生命周期 → `TouchResident/TouchContent` → 锁内 LRU 更新 → 锁外
`ResidencyPort.Evict` → flush/卸载/回读回调 → 目录刷新或继续使用。

## 依赖方向

允许依赖 `application/session` 纯值、`session/state`、`session/viewstate`、
`contract`、`kernel/state` 与 `middle/residency`。禁止依赖 `application/core`、
`store`、具体 Backend 实现或 GUI/TUI。

## 并发与错误

`residentOrder` / `loadedContentOrder` 只由 `Core.ViewMu` 保护；驱逐判据拿到的
顺序必须复制。flush 失败不卸载内容；释放失败保持原状并等待下一轮收敛。

## 扩展与 Review

新增驻留层必须先证明它是独立资源，不得把 Session 状态或 Middle 判据复制进来。
Review 重点：锁外 I/O、顺序快照别名、受保护集漂移和回读失败语义。

## 测试与验证

```text
go test ./application/session/residency -count=1
go test ./application/core -run 'Resident|Content|Release|Cold|Session' -count=1
go test ./application/... -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。

### runtime.go

- `func NewRuntime(deps RuntimeDeps) *Runtime` — NewRuntime 构造 Session residency runtime。
- `func (runtime *Runtime) TouchResident(sessionID string)` — 记录引擎驻留使用并收敛。
- `func (runtime *Runtime) ReconcileResident()` — 按受保护集和窗口驱逐引擎。
- `func (runtime *Runtime) TouchContent(sessionID string)` — 记录正文使用并收敛。
- `func (runtime *Runtime) ReconcileContent(protect string)` — 按受保护集和窗口卸载正文。
- `func (runtime *Runtime) EnsureContent(sessionID string) error` — 自动卸载后回读正文。
- `func (runtime *Runtime) ProtectedSessionLocked(sessionID string) bool` — 唯一释放保护判据。
