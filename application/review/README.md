# application/review

## 生态位

`application/review` 承载「请 GUI 打开待审阅文件」的应用适配。真实调用方是
`application/assembly.Service` 的审阅兼容入口；逐文件 containment、敏感、二进制和
去重裁决仍由 `backend/tool/review` 唯一拥有。

## 职责与非职责

职责：

- 提供 workspace 文件读取和会话归属窄端口；
- 生成跨调用唯一的 review request ID；
- 把接受的文件请求写入宿主会话投影并发布快照；
- 清空当前或指定会话的审阅请求。

非职责：

- 不重新实现文件沙盒或敏感文件判据；
- 不直接写 Snapshot、EventHub 或工作区文件；
- 不导入 `application/assembly`，不持完整 `Service`。

## 文件结构

- `application.go`：`Deps`、`Application`、Host adapter 与两个用例。
- `application_test.go`：接受请求、会话回退和请求 ID 唯一性测试。

## 核心实现

`Application` 只持真实宿主回调和 request 序号；每次调用构造
`backend/tool/review.Requests`，把裁决交给既有唯一实现。

## 数据流

`Service` 兼容入口 → `review.Application` → `review.Requests` → Workspace 文件
端口 → 会话视图请求写入 → 解锁后快照事件。

## 依赖方向

允许依赖 `backend/tool/review` 的纯应用落点、`contract` 与 `contract/dto`。禁止依赖
`application/assembly`、GUI/TUI 或直接存储实现。

## 并发与错误

request ID 序号使用原子计数；视图锁和 Snapshot 写入由宿主注入回调负责。文件读取错误
仍由 `backend/tool/review` 分类，全部跳过时不改写已有请求。

## 扩展方式

新增审阅能力先判断是否属于同一请求落点；不得在本包复制 workspace containment 或敏感
文件词表。兼容 Service 入口必须保留一行转发。

## Review 指南

- 是否仍只调用 `backend/tool/review.Requests`；
- request ID 是否避免同 tick 碰撞；
- ctx 会话优先、视图会话兜底的顺序是否保持；
- 发布是否在视图锁外；
- 是否 import `application/assembly`。

## 测试与验证

```text
go test ./application/review -count=1
go test ./application/assembly -run 'Review|FileReview' -count=1
go test ./application/... -count=1
```

## 文件与函数索引

### `application.go`

- `func New(deps Deps) *Application` — 构造审阅请求应用。
- `func (application *Application) RequestFileReview(ctx context.Context, files []dto.ReviewFileRef) (dto.ReviewRequest, error)` — 生成并投影文件审阅请求。
- `func (application *Application) ClearReviewRequest(sessionID string)` — 清空会话审阅请求。

### `application_test.go`

测试文件按仓库 README 规范不展开函数索引。
