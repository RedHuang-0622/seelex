# backend/tool/review

## 生态位

`backend/tool/review` 是「请 GUI 在侧边资源管理器打开待审阅文件」（`show_files`）的**落点**：
逐份过 workspace 域裁决 → 记为会话作用域的可重放请求 → 回契约收据。

主要调用方：`application/core`（门面实现 `ReviewFilesPort`，把 workspace 读面、召唤 ID、会话归属
与视图写面经 `Host` 装配进来）；工具注册那一半（名字/参数/返回值形状）在
`backend/engine/assemble`（`RegisterReviewFilesTool`）——它的签名绑在 `*assemble.Runtime` 上，
按计划随 R11（`Runtime` 按域下沉）一起搬。

落点沿革：2026-10-16 随 M5-R-tool-review **接收者转移**从 `application/core/review_files.go`
归位（plan §2.6）。

## 职责与非职责

**做**：

1. 形状校验（非空 / 单次上限 `dto.MaxReviewFilesPerRequest`，超限**显式报错**不静默截断）；
2. 同批次去重（`NormalizeKey`：分隔符归一，不做路径语义解析）；
3. 逐份走 workspace 域 `ReadFile` 做 containment / 敏感 / 二进制裁决，并把读失败**分类**成契约词表里的原因格；
4. 至少一份通过时写会话作用域请求 + 发布整份快照事件；全部被跳过时不碰既有请求。

**不做**：不实现沙盒边界（唯一实现在 workspace 域，本包只调用同一个 Port、同一处 root）；
不读磁盘旁路、不开"agent 专用"的更宽通道；不注册工具（见"落点现状"）。

## 目录与文件结构

| 文件 | 职责 |
|---|---|
| `review.go` | `Requests`（落点本体）+ `Host`（宿主面）+ `ProbeLimit` / `NormalizeKey` / `ClassifyReadError` |
| `review_test.go` | 读错误分类表、去重键、以及"全被跳过不写宿主 / 无会话不发布"等宿主契约用例 |

## 核心实现

- `Requests.Request(ctx, files)`：**沙盒边界的唯一裁决点**。它本身不实现边界——逐份文件走
  `Host.WorkspaceFile()` 解析出的 Port 与 root，与前端点开文件行是同一条通道，因此新增这个入口
  不可能放宽既有边界。返回的 `dto.ReviewRequest` 就是模型面收据（请求几份 / 进去几份 / 被跳过几份与原因）。
- `ClassifyReadError`：把 workspace 域的读取拒绝按**文案**归到 `dto.ReviewSkip*` 词表。
  刻意不新增一族类型化错误：边界的唯一实现在 workspace 域，其文案已是前端与文档依赖的既有契约；
  再引一族类型会让"边界一份"变成"边界一份、分类一份"。代价是依赖文案，因此判据集中在这一个函数里。
- `ProbeLimit`（64 KiB）：只用于探测，字节读完即弃。取 64 KiB 而不是 1 字节（二进制探测看分布，
  1 字节会把 PNG 误判成文本），也不是 4 MiB（裁决只需要"能不能给人看"，真读由前端按路径走预览通道）。

## 数据流

```text
工具调用 show_files(files[])
  → Host.WorkspaceFile()（当前会话绑定的 workspace 读面 + root）
  → 逐份 ReadFile(root, path, ProbeLimit) → containment/敏感/二进制裁决
  → 至少一份通过：Host.SetReviewRequest（视图锁内写请求 + bump）
  → Host.PublishSnapshotChanged（整份快照事件，前端补放/开 chip）
  → 返回契约收据
```

## 依赖方向

允许：标准库、`contract`（`WorkspaceFilePort`）、`contract/dto`（请求与跳过原因词表）。
禁止：`application/*`、`store`、workspace 实现包——读面由 `Host` 注入。

## 并发、存储与错误语义

- 本包**无状态**；并发安全由 `Host` 保证（请求写入在宿主视图锁内）。
- 不落盘：请求是**会话快照**的一部分（可重放），不是持久化事实。
- 错误语义：workspace 解析失败 → `show_files: <原因>` 冒泡；读失败 → 归入 `Skipped`（不中断整批）。

## 扩展方式

- 新增跳过原因：先加进 `contract/dto` 词表，再在 `ClassifyReadError` 的 switch 里加一条（判据一处）。
- 新增工具入口：注册侧（assemble）只做形状校验后交给本包，不要在注册侧另写一份裁决。

## Review 指南

- 裁决通道是否仍与前端预览**同一个 Port、同一处 root**（不得新增读盘旁路）？
- `ClassifyReadError` 的文案表是否与 workspace 域的实际报错漂移（新增文案时改这一处）？
- 超限是否仍**显式报错**（不得静默截断）？全部被跳过时是否仍**不碰**既有请求？
- 会话归属是否仍"ctx 里的会话优先、视图会话兜底"？

## 测试与验证

```text
go test ./backend/tool/review/ -count=1
go test ./application/core/ -run Review -count=1     # 门面级集成（裁决通道 / 快照投影 / 工作区切换清理）
go test ./backend/engine/assemble/ -run Review -count=1   # 工具注册面（形状与收据）
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### review.go

- `const ProbeLimit int64` — 裁决一份待审阅文件时实际读回的上限（64 KiB；只用于探测）。
- `type Host interface` — 落点需要的宿主面（workspace 读面、召唤 ID、会话归属、视图写面、快照发布）。
- `type Requests struct` — 落点本体（无状态）。
- `func NewRequests(host Host) *Requests` — 建落点。
- `func (r *Requests) Request(ctx context.Context, files []dto.ReviewFileRef) (dto.ReviewRequest, error)` — 沙盒边界的唯一裁决点：逐份裁决 → 写会话作用域请求 → 回收据。
- `func (r *Requests) Clear(sessionID string)` — 清空指定会话的待审阅请求（工作区切换/解绑的失效面）。
- `func NormalizeKey(path string) string` — 同批次去重用的路径键（分隔符归一）。
- `func ClassifyReadError(err error) string` — 把 workspace 域的读取拒绝归到契约词表里的原因格。
