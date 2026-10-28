# application/conversation/search

## 生态位

`application/conversation/search` 是会话历史检索的应用适配层，供 core 兼容门面和
`search_history` 模型工具共用。检索索引、默认 limit、token 预算与 clamp 仍由 Runtime
的 search 实现唯一拥有。

## 职责

- 拒绝空查询。
- 检查 Runtime search 能力是否装配。
- 保持 GUI 查询与模型工具走同一查询入口。
- 把权威 `dto.SearchResult` 编码为工具 JSON。

## 非职责

- 不读取历史存储、不选择压缩栈、不实现相关性排序。
- 不重写默认 limit 或 token 预算。
- 不导入 `application/assembly`，不持有完整 `Service`。

## 文件结构

- `application.go`：窄 deps、查询校验和工具适配。
- `application_test.go`：空查询、参数透传、JSON 与错误语义。

## 核心实现

`Application` 只接收 `Available` 与 `Search` 两个窄函数。两者在调用时解析，保证宿主
替换 Runtime 后下一次查询立即生效。

## 数据流

`application/assembly` 一行兼容转发 → `Application.SearchHistory` → Runtime search →
权威 `dto.SearchResult`。

## 依赖方向

允许依赖 `contract/dto` 与标准库。禁止依赖 `application/assembly`、具体 backend/middle
实现或前端包。

## 并发与错误

本包不缓存状态、不持锁。空查询返回 `ErrEmptyQuery`；Runtime 未装配和宿主错误保持
显式失败，不返回伪造空结果。

## 扩展方式

新增检索能力时扩展 `Deps` 的窄函数，不在本包复制 Runtime 的默认值、预算或检索算法。

## Review 指南

- GUI 与工具入口是否仍共用 `SearchHistory`。
- `limit == 0` 是否仍原样交给 Runtime。
- 是否误把结果缓存或索引状态放进 Application。

## 测试与验证

```text
go test ./application/conversation/search -count=1
go test ./application/... -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### application.go

- `func New(deps Deps) *Application` — New constructs the application from its consumer-defined dependencies.
- `func (application *Application) SearchHistory(ctx context.Context, query string, limit int) (dto.SearchResult, error)` — SearchHistory returns the authoritative runtime result for a non-empty query.
- `func (application *Application) SearchHistoryHandler(argsJSON string) (string, error)` — SearchHistoryHandler adapts the search_history model tool to SearchHistory.

### application_test.go

- `func TestSearchHistoryRejectsEmptyQuery(t *testing.T)`
- `func TestSearchHistoryPreservesQueryAndLimit(t *testing.T)`
- `func TestSearchHistoryHandlerUsesSameApplicationPath(t *testing.T)`
- `func TestSearchHistoryHandlerWrapsRuntimeFailure(t *testing.T)`
