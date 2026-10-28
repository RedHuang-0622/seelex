# textsanitize

## 生态位

`store/internal/textsanitize` 是 store 派生视图共享的文本清洗机制，唯一调用方是根
`store` 的项目摘要渲染与 `store/board` 的看板 Markdown 渲染。它不拥有项目、看板或
会话业务状态。

## 职责与非职责

- 职责：剔除机器本地 `ws-<nano>`、绝对路径，并校验仓库相对路径。
- 非职责：读写存储、判断看板条目、渲染 Markdown、决定哪些字段可展示。

## 文件索引

- `sanitize.go`：`KnownText` 与 `RepoRelativePath` 的唯一实现。

## 依赖方向

只依赖标准库，不由任何领域包反向依赖；领域包可向下依赖本包。

## 扩展与审查

新增清洗规则时修改本包单一实现，并同时覆盖项目摘要与看板渲染。审查重点是不要把
领域判定塞进这里，也不要为同一规则复制正则。

## 验证

```text
go test ./store/... -run 'ProjectKnown|ProjectBoard' -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### sanitize.go

- `func KnownText(value string) string` — KnownText removes machine-local workspace IDs and absolute paths from prose.
- `func RepoRelativePath(value string) (string, bool)` — RepoRelativePath validates and normalizes a repository-relative path.
