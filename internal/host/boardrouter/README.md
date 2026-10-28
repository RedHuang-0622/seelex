# Board Router Adapter

## 生态位

`internal/host/boardrouter` 是组合根使用的无状态适配器，把会话生命周期所需的 Board
读写端口连接到 `store/board`。它不拥有看板语义。

## 边界

- 负责向调用方暴露 `AppendProjectBoardRows`、`ProjectBoardWatermark`、
  `ReadProjectBoardRows`、`ReadProjectBoard` 和 `ReadProjectBoardMarkdown`。
- 不负责目录派生、折叠、压实、Markdown 渲染或项目根解析。
- 不读写会话正文，不导入 Application、Middle、Backend 或 Kernel。

## 验证

```text
go test ./internal/host/boardrouter ./store/board ./kernel/board ./boot -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### router.go

- `func New() *Adapter` — New constructs the stateless adapter.
- `func (a *Adapter) AppendProjectBoardRows(projectRoot, sessionID, file string, rows []dto.ProjectBoardRow) (uint64, error)`
- `func (a *Adapter) ProjectBoardWatermark(projectRoot, sessionID, file string) (uint64, error)`
- `func (a *Adapter) ReadProjectBoardRows(projectRoot, sessionID, file string) ([]dto.ProjectBoardRow, error)`
- `func (a *Adapter) ReadProjectBoard(projectRoot string, options dto.ProjectBoardReadOptions) ([]dto.ProjectBoardRow, bool, error)`
- `func (a *Adapter) ReadProjectBoardMarkdown(projectRoot string) ([]byte, error)`

### router_test.go

- `func TestAdapterForwardsToBoardOwner(t *testing.T)`
