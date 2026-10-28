# media（会话媒体与随图）

## 生态位

`backend/tool/io/media` 是**会话媒体面**：把「画面落进执行会话自己的媒体分区」与
「画面挂到下一次模型请求上且只挂一次」两件事收成一份实现。主要调用方是装配器
`backend/engine/assemble`（`Runtime` 持有并在拓扑序里包装 Completer）与 computer use
工具族（`backend/tool/computer` 经 `Deps.StoreMedia` / `Deps.AttachImage` 调它）；
GUI 的输入框图片附件（拖拽 / 粘贴）经 `gui.Bridge` → `Runtime.ComposerMedia()` 也用它
（`AttachUserImage` / `MediaFilePath`）。

物理位置：2026-10-16 按
[plan.md](../../../../docs/2026-10-15-architecture-reshape/plan.md) §2.6 从
`seelebridge/runtime_image.go` 下沉到这里（该路径已删除：`seelebridge` 根包先整体归位
`backend/engine/assemble`，随后这一域再下沉）；这是 M4-J2 的第一片"带宿主端口的域下沉"——
此前 J2a 下沉的
[`backend/engine/obs`](../../../engine/obs/README.md) 完全不碰宿主状态。

## 职责与非职责

- **做**：① 归属解析——把一次媒体读写判给"执行这次动作的那个会话"（`ProjectIDFor`）；
  ② 媒体分区分读写——`StoreMedia` / `LoadMedia`；③ 待随图队列——`Attach` / `AttachPending` /
  `PendingCount`，以及把它接上 Completer 链的 `Wrap`；④ 用户输入框附上来的图片
  ——`AttachUserImage` / `MediaFilePath`（GUI 拖拽 / 粘贴的唯一落点，2026-10-30）。
- **不做**：不决定包装挂在链的哪一层（那是装配器的拓扑判断）；不判断"该不该给模型看图"
  （授权在权限门）；不落非媒体数据；不缓存字节（字节在会话媒体分区，队列只存附件投影）；
  不启动系统查看器（那是宿主的 GUI 面：`gui.openWithSystemViewer`）。

## 核心实现

| 类型 / 方法 | 作用 |
|---|---|
| `Host` | 宿主端口，只有媒体读写三格（`WriteMediaWorkspace` / `ReadMediaWorkspace` / `MediaPathWorkspace`）与 `SessionProjectID(sessionID) string`。宿主是 `*assemble.Runtime`；本包不持 `store.Router` |
| `SessionMedia` | 本域唯一状态：`host` + 待随图队列 `pending *imageattach.Registry`。零值不可用，`New(host)` 构造 |
| `Attach` / `AttachPending` / `PendingCount` | 入队与读数。`AttachPending` 是工具侧入口：缺会话上下文**显式报错**（截了却送不到模型必须让人知道），`Attach` 则按队列语义静默丢弃；三者与 `AttachUserImage` 共用同一处 `attach`（唯一 `pending.Add`） |
| `StoreMedia` / `LoadMedia` | 会话媒体分区的写/读。两处都先解析归属再做 IO，因此读写同域、不会把别的会话/项目的画面取过来；`StoreMedia` 与 `AttachUserImage` 共用同一处 `storeMedia` |
| `AttachUserImage` | 用户从输入框附上来的图片（拖拽 / 粘贴）：按 MIME 白名单（png/jpeg/webp/gif/bmp）校验并把扩展名归一，落进该会话媒体分区后挂进随图队列。**会话由请求显式给出**（同一时刻可能有多个会话在跑，图属于用户正在编辑的那个） |
| `MediaFilePath` | 某条已落盘媒体在该会话分区里的真实文件位置——GUI「点开缩略图，用系统默认查看器看原图」用。路径只在宿主进程内消费，不回渲染层 |
| `ProjectIDFor` | 归属三级解析：会话绑定 → `model.SceneScope.WorkspaceID` → 默认项目 `""`。刻意不读 Router 的活跃写作用域、不用主会话兜底（并行子代理执行期间视图可能已切走） |
| `Wrap` | 把 `imageattach.Wrapper`（`Next` + `Pending` + `Loader: LoadMedia` + 可选 Stream/StreamEvents）**返回**给调用方装配，自身不持有 Completer |

## 数据流

```text
截屏工具（backend/tool/computer）
  └─ Deps.StoreMedia(ctx, asset)  → SessionMedia.StoreMedia → artifact.SessionStore.WriteMedia(归属项目, 执行会话)
  └─ Deps.AttachImage(ctx, image) → SessionMedia.AttachPending → pending.Add(执行会话)
输入框图片附件（frontend composer → gui.Bridge）
  └─ AttachComposerImage(sessionID, name, mime, w, h, base64)
       └─ SessionMedia.AttachUserImage → storeMedia(显式会话) + attach(显式会话)
  └─ OpenComposerImage(sessionID, ref) → SessionMedia.MediaFilePath → gui.openWithSystemViewer(默认查看器)
装配器（assemble.NewRuntime，拓扑序第 3.1 步）
  └─ SessionMedia.Wrap(completer, streamer) —— 挂在 request log 包装之内、agent.NewWithComponents 之前
下一次模型请求
  └─ imageattach.Wrapper.prepare：Take(会话) 取走待随图，ref 缺失时经 LoadMedia 读回字节
```

**「随下一次请求」的语义**（用户图片也一样）：图在附上的那一刻就进了该会话的随图队列，
下一次该会话的请求会把它送给模型并且只送一次。因此"附了图但没发消息"时，图仍会随下一次
请求出发；会话切换时前端只清掉缩略图条——图属于**上一个会话**，前端擦不掉。

## 依赖方向

- 允许：标准库、Seele（`agent` / `types`）、[`backend/tool/io/imageattach`](../imageattach/README.md)、
  [`backend/tool/computer`](../../computer/README.md)（只取 `MediaAsset` / `StoredMedia` / `PendingImage` 三个形状）、
  `contract/dto`（`ComposerImageRequest` / `ComposerImage` 两个形状）、
  `internal/model`、`internal/telemetry`、`store/artifact` 的形状类型。
- 禁止：`application/*`（上层）、`backend/engine/assemble`（装配器反过来 import 本包；
  宿主关系只能经 `Host` 端口表达），以及任何"再包一层会话状态"的重复实现。

## 并发、存储与错误语义

- 队列由 `imageattach.Registry` 自锁；本类型自身**无锁**（没有可变字段），可并发调用。
- 存储语义沿用 Artifact owner：媒体按 `(归属项目, 执行会话)` 分片，未绑定 = 默认项目。
- 错误语义：会话上下文缺失、宿主未注入 `artifact.SessionStore` 都是**显式错误**，不静默
  落进默认项目；零值 `*SessionMedia` 上 `Attach`/`PendingCount` 是空操作，`StoreMedia`/`LoadMedia`
  返回"存储未装配"错误。

## 扩展方式

- 新增媒体种类：加在 `backend/tool/computer` 的 `MediaAsset` 侧，本包只做搬运（不改判据）。
- 新增随图形态（例如"只给引用"之外的载入方式）：改 `Wrap` 的 `Options`，不要另起一条入队路径
  （`Attach` 必须是唯一一处 `pending.Add`）。
- 宿主需要更多事实时，先问"能不能由既有两格推出"；确需扩端口就在 `Host` 加一格并在装配处实现，
不要在本包 import `assemble`、根 `store` 或具体 Router 类型。

## Review 指南

- 归属解析是否仍然**只**按执行会话与节点作用域？（退回读 Router 活跃写作用域是历史坑）
- 入队是否仍然只有一条路径（工具侧 `AttachPending` 与输入框的 `AttachUserImage` 都不得绕过
  `attach` 直接写队列）？
- 落盘是否仍然只有一条路径（`AttachUserImage` 不得自己拼 `WriteMediaWorkspace`）？
- 包装位置是否仍在 request log 之内、`agent.NewWithComponents` 之前？（顺序错会让画像记录不到图片）
- 缺会话上下文是否仍然是硬错误，而不是静默丢弃？
- 用户图片的落盘名是否仍带与 MIME 一致的扩展名？（否则「用默认查看器打开」找不到接管程序）

## 测试与验证

```text
go test ./backend/tool/io/media/... -count=1
go test ./backend/engine/assemble/... ./backend/tool/computer/... -count=1
```

关键用例：`TestStoreSessionMediaWritesIntoExecutingSessionPartition`（会话隔离 + 节点作用域归属 +
缺会话报错）、`TestMediaProjectIDForResolution`（三级解析）、`TestAttachSessionImageQueuesForExecutingSession`
（队列不跨会话）、`TestWrapKeepsStreamChain`（流式链接线）、`TestNilMediaIsInert`（零值不 panic）、
`TestAttachUserImageStoresIntoSessionPartitionAndQueuesOnce`（用户图片：分区隔离 + 入队 + 扩展名归一 +
原图位置）、`TestAttachUserImageRejectsUnsupportedInput`（非图片/空内容/缺会话/白名单外一律显式失败
且不落盘）、`TestMediaFilePathRejectsMissingFacts`（点开原图的失败面）。

## 文件与函数索引

### media.go

| 函数 | 签名 | 摘要 |
|---|---|---|
| `New` | `func New(host Host) *SessionMedia` | 构造媒体面（待随图队列由本包自建） |
| `Attach` | `func (m *SessionMedia) Attach(ctx context.Context, attachment imageattach.Attachment)` | 把一张画面挂进下一次请求随图队列 |
| `PendingCount` | `func (m *SessionMedia) PendingCount(ctx context.Context) int` | 返回当前会话还有多少张待随图 |
| `StoreMedia` | `func (m *SessionMedia) StoreMedia(ctx context.Context, asset bridgecomputer.MediaAsset) (bridgecomputer.StoredMedia, error)` | 把截图落进执行会话自己的媒体分区 |
| `AttachPending` | `func (m *SessionMedia) AttachPending(ctx context.Context, image bridgecomputer.PendingImage) error` | 工具侧入队入口：缺会话上下文改为显式报错 |
| `LoadMedia` | `func (m *SessionMedia) LoadMedia(ctx context.Context, ref string) (types.FilePart, error)` | 按 `media:<hash>` 从执行会话的媒体分区读回图片 |
| `ProjectIDFor` | `func (m *SessionMedia) ProjectIDFor(ctx context.Context, sessionID string) string` | 解析媒体资产的归属项目（会话绑定 → 节点工作区 → 默认） |
| `Wrap` | `func (m *SessionMedia) Wrap(next agent.Completer, streamer agent.StreamCompleter) (agent.Completer, agent.StreamCompleter)` | 把待随图包装挂到给定的一对 Completer 上并返回 |

### user_image.go

| 函数 | 签名 | 摘要 |
|---|---|---|
| `AttachUserImage` | `func (m *SessionMedia) AttachUserImage(ctx context.Context, request dto.ComposerImageRequest) (dto.ComposerImage, error)` | 把用户从输入框附上来的图片落进该会话媒体分区并挂进随图队列 |
| `MediaFilePath` | `func (m *SessionMedia) MediaFilePath(ctx context.Context, sessionID, ref string) (string, error)` | 返回已落盘媒体的真实文件位置（系统默认查看器打开原图用） |

### media_test.go

| 测试 | 钉住什么 |
|---|---|
| `TestStoreSessionMediaWritesIntoExecutingSessionPartition` | 落在执行会话分区、ref 不跨会话、节点作用域归属、缺会话报错（随 M4-J2b 从 assemble 原样搬来） |
| `TestMediaProjectIDForResolution` | 归属三级解析（含无宿主回落默认项目） |
| `TestAttachSessionImageQueuesForExecutingSession` | 入队到执行会话、附件载体形状、不泄漏到别的会话 |
| `TestWrapKeepsStreamChain` | 包装保留 Stream/StreamEvents 链、返回同一个包装、未装配不造包装 |
| `TestNilMediaIsInert` | 零值上的四格行为（空操作 / 显式错误，不 panic） |

### user_image_test.go

| 测试 | 钉住什么 |
|---|---|
| `TestAttachUserImageStoresIntoSessionPartitionAndQueuesOnce` | 用户图片：落在该会话分区、入队只进本会话、扩展名按 MIME 归一、原图位置可交给系统查看器 |
| `TestAttachUserImageRejectsUnsupportedInput` | 非图片 / svg / 空内容 / 缺会话一律显式失败，且**不落盘** |
| `TestAttachUserImageKeepsJPEGNameAndQueuesBytePayload` | 第二个 MIME 走同一条路径；队列里的 `FilePart` 是 image 种类且能过发射前校验 |
| `TestMediaFilePathRejectsMissingFacts` | 缺会话 / 缺 ref / 未装配存储都报错（不编路径） |
