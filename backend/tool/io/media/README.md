# media（会话媒体与随图）

## 生态位

`backend/tool/io/media` 是**会话媒体面**：把「画面落进执行会话自己的媒体分区」与
「画面挂到下一次模型请求上且只挂一次」两件事收成一份实现。主要调用方是装配器
`backend/engine/assemble`（`Runtime` 持有并在拓扑序里包装 Completer）与 computer use
工具族（`backend/tool/computer` 经 `Deps.StoreMedia` / `Deps.AttachImage` 调它）。

物理位置：2026-10-16 按
[plan.md](../../../../docs/2026-10-15-architecture-reshape/plan.md) §2.6 从
`seelebridge/runtime_image.go` 下沉到这里（该路径已删除：`seelebridge` 根包先整体归位
`backend/engine/assemble`，随后这一域再下沉）；这是 M4-J2 的第一片"带宿主端口的域下沉"——
此前 J2a 下沉的
[`backend/engine/obs`](../../../engine/obs/README.md) 完全不碰宿主状态。

## 职责与非职责

- **做**：① 归属解析——把一次媒体读写判给"执行这次动作的那个会话"（`ProjectIDFor`）；
  ② 媒体分区分读写——`StoreMedia` / `LoadMedia`；③ 待随图队列——`Attach` / `AttachPending` /
  `PendingCount`，以及把它接上 Completer 链的 `Wrap`。
- **不做**：不决定包装挂在链的哪一层（那是装配器的拓扑判断）；不判断"该不该给模型看图"
  （授权在权限门）；不落非媒体数据；不缓存字节（字节在会话媒体分区，队列只存附件投影）。

## 核心实现

| 类型 / 方法 | 作用 |
|---|---|
| `Host` | 宿主端口，**只有两格**：`HistoryRouter() *store.Router`、`SessionProjectID(sessionID) string`。宿主是 `*assemble.Runtime`；本包不持宿主的任何写入口 |
| `SessionMedia` | 本域唯一状态：`host` + 待随图队列 `pending *imageattach.Registry`。零值不可用，`New(host)` 构造 |
| `Attach` / `AttachPending` / `PendingCount` | 入队与读数。`AttachPending` 是工具侧入口：缺会话上下文**显式报错**（截了却送不到模型必须让人知道），`Attach` 则按队列语义静默丢弃 |
| `StoreMedia` / `LoadMedia` | 会话媒体分区的写/读。两处都先解析归属再做 IO，因此读写同域、不会把别的会话/项目的画面取过来 |
| `ProjectIDFor` | 归属三级解析：会话绑定 → `model.SceneScope.WorkspaceID` → 默认项目 `""`。刻意不读 Router 的活跃写作用域、不用主会话兜底（并行子代理执行期间视图可能已切走） |
| `Wrap` | 把 `imageattach.Wrapper`（`Next` + `Pending` + `Loader: LoadMedia` + 可选 Stream/StreamEvents）**返回**给调用方装配，自身不持有 Completer |

## 数据流

```text
截屏工具（backend/tool/computer）
  └─ Deps.StoreMedia(ctx, asset)  → SessionMedia.StoreMedia → router.WriteMediaWorkspace(归属项目, 执行会话)
  └─ Deps.AttachImage(ctx, image) → SessionMedia.AttachPending → pending.Add(执行会话)
装配器（assemble.NewRuntime，拓扑序第 3.1 步）
  └─ SessionMedia.Wrap(completer, streamer) —— 挂在 request log 包装之内、agent.NewWithComponents 之前
下一次模型请求
  └─ imageattach.Wrapper.prepare：Take(会话) 取走待随图，ref 缺失时经 LoadMedia 读回字节
```

## 依赖方向

- 允许：标准库、Seele（`agent` / `types`）、[`backend/tool/io/imageattach`](../imageattach/README.md)、
  [`backend/tool/computer`](../../computer/README.md)（只取 `MediaAsset` / `StoredMedia` / `PendingImage` 三个形状）、
  `internal/model`、`internal/telemetry`、`store`。
- 禁止：`application/*`（上层）、`backend/engine/assemble`（装配器反过来 import 本包；
  宿主关系只能经 `Host` 端口表达），以及任何"再包一层会话状态"的重复实现。

## 并发、存储与错误语义

- 队列由 `imageattach.Registry` 自锁；本类型自身**无锁**（没有可变字段），可并发调用。
- 存储语义沿用 store：媒体按 `(归属项目, 执行会话)` 分片，未绑定 = 默认项目。
- 错误语义：会话上下文缺失、宿主未装配（`HistoryRouter() == nil`）都是**显式错误**，不静默
  落进默认项目；零值 `*SessionMedia` 上 `Attach`/`PendingCount` 是空操作，`StoreMedia`/`LoadMedia`
  返回"存储未装配"错误。

## 扩展方式

- 新增媒体种类：加在 `backend/tool/computer` 的 `MediaAsset` 侧，本包只做搬运（不改判据）。
- 新增随图形态（例如"只给引用"之外的载入方式）：改 `Wrap` 的 `Options`，不要另起一条入队路径
  （`Attach` 必须是唯一一处 `pending.Add`）。
- 宿主需要更多事实时，先问"能不能由既有两格推出"；确需扩端口就在 `Host` 加一格并在装配处实现，
  不要在本包 import `assemble` 或 `store.Router` 之外的具体宿主类型。

## Review 指南

- 归属解析是否仍然**只**按执行会话与节点作用域？（退回读 Router 活跃写作用域是历史坑）
- 入队是否仍然只有一条路径（工具侧 `AttachPending` 不得绕过 `Attach` 直接写队列）？
- 包装位置是否仍在 request log 之内、`agent.NewWithComponents` 之前？（顺序错会让画像记录不到图片）
- 缺会话上下文是否仍然是硬错误，而不是静默丢弃？

## 测试与验证

```text
go test ./backend/tool/io/media/... -count=1
go test ./backend/engine/assemble/... ./backend/tool/computer/... -count=1
```

关键用例：`TestStoreSessionMediaWritesIntoExecutingSessionPartition`（会话隔离 + 节点作用域归属 +
缺会话报错）、`TestMediaProjectIDForResolution`（三级解析）、`TestAttachSessionImageQueuesForExecutingSession`
（队列不跨会话）、`TestWrapKeepsStreamChain`（流式链接线）、`TestNilMediaIsInert`（零值不 panic）。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### media.go

- `func New(host Host) *SessionMedia` — New 构造媒体面。pending 队列由本包自建：它是进程内的一次性状态，没有第二个
- `func (m *SessionMedia) Attach(ctx context.Context, attachment imageattach.Attachment)` — Attach 把一张画面挂进「下一次模型请求随图」队列。
- `func (m *SessionMedia) PendingCount(ctx context.Context) int` — PendingCount 返回当前会话还有多少张待随图（诊断与断言用）。
- `func (m *SessionMedia) StoreMedia(ctx context.Context, asset bridgecomputer.MediaAsset) (bridgecomputer.StoredMedia, error)` — StoreMedia 把截图落进**执行会话自己**的媒体分区（截屏工具经
- `func (m *SessionMedia) AttachPending(ctx context.Context, image bridgecomputer.PendingImage) error` — AttachPending 把刚刚看到的画面挂进「下一次模型请求随图」队列
- `func (m *SessionMedia) LoadMedia(ctx context.Context, ref string) (types.FilePart, error)` — LoadMedia 按 `media:<hash>` 从执行会话的媒体分区读回图片：imageattach
- `func (m *SessionMedia) ProjectIDFor(ctx context.Context, sessionID string) string` — ProjectIDFor 解析媒体资产的归属项目（store 的 projectID 与
- `func (m *SessionMedia) Wrap(next agent.Completer, streamer agent.StreamCompleter) (agent.Completer, agent.StreamCompleter)` — Wrap 把待随图包装挂到给定的一对 Completer 上，**返回**挂好的一对。
- `func (m *SessionMedia) historyRouter() *store.Router` — historyRouter 是本包内部对 Host 的取值口：宿主未装配（nil）时返回 nil。

### media_test.go

- `func (h *stubHost) HistoryRouter() *store.Router`
- `func (h *stubHost) SessionProjectID(sessionID string) string`
- `func TestStoreSessionMediaWritesIntoExecutingSessionPartition(t *testing.T)` — TestStoreSessionMediaWritesIntoExecutingSessionPartition 验证截图落在**执行
- `func TestMediaProjectIDForResolution(t *testing.T)` — TestMediaProjectIDForResolution 验证媒体归属项目的三级解析：执行会话自己的
- `func TestAttachSessionImageQueuesForExecutingSession(t *testing.T)` — TestAttachSessionImageQueuesForExecutingSession 验证画面随图：入队到执行会话，
- `func (streamEventStub) CompleteStream( context.Context, []types.Message, []types.Tool, func(string), ) (string, string, []types.ToolCall, error)`
- `func (stubCompleter) Complete(context.Context, []types.Message, []types.Tool) (types.Message, error)`
- `func (streamEventStub) CompleteStreamEvents( context.Context, []types.Message, []types.Tool, func(types.StreamEvent), ) (string, string, []types.ToolCall, error)`
- `func TestWrapKeepsStreamChain(t *testing.T)` — TestWrapKeepsStreamChain 验证包装的链式接线：非 nil 的 streamer 进 wrapper.Stream
- `func TestNilMediaIsInert(t *testing.T)` — TestNilMediaIsInert 验证未装配（零值）时四格读写都是空操作/显式错误，不 panic。
