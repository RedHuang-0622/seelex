# application/assembly/architecture

## 生态位

`application/assembly/architecture` 是 assembly 的结构门禁测试包。它读取父目录的
Go 源码并验证组件边界，不参与生产装配。

## 职责与非职责

- 做：禁止自由 `newService*` 构造器、组件转发 getter、单调用 facade 和退场宽接口；
- 做：验证 Conversation turn 运行时与 ChatState/request 规则没有复制回 assembly；
- 不做：启动 Service、访问运行态或复制业务实现。

## 目录结构

- `component_getter_boundary_test.go`：组件 getter、inbound contract、单调用转发门禁。
- `service_constructor_boundary_test.go`：组合根构造函数门禁。
- `conversation_turn_boundary_test.go`：Conversation turn 规则归属门禁。

## 核心实现

测试通过 `go/parser` 扫描父目录生产文件，路径固定为 `..`，不依赖测试进程的工作目录名称。

## 数据流与依赖

测试 → 父目录 `.go` AST → 结构断言。只依赖标准库和 `application/assembly` 源码。

## 并发与错误

只读扫描；解析或目录读取失败立即以测试错误返回。

## 扩展方式与 Review

新增结构门禁放本包；涉及运行态、锁序或真实端口交互的测试仍留父包集成测试。

## 测试与验证

```text
go test ./application/assembly/architecture -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### ast.go

- `func FunctionHasReceiver(function *ast.FuncDecl, name string) bool` — FunctionHasReceiver reports whether the declaration is a method on the named

### component_getter_boundary_test.go

- `func TestCoreDoesNotExposeComponentGetters(t *testing.T)` — TestCoreDoesNotExposeComponentGetters pins the composition boundary: once a
- `func TestCoreDoesNotOwnInboundAPIContracts(t *testing.T)` — TestCoreDoesNotOwnInboundAPIContracts keeps consumer-facing interfaces at
- `func TestCoreHasNoComponentMethodForwarders(t *testing.T)` — TestCoreHasNoComponentMethodForwarders removes the second forwarding layer:
- `func isComponentsForwardCall(body *ast.BlockStmt) bool`
- `func startsWithServiceComponents(expression ast.Expr) bool`
- `func hasServiceReceiver(function *ast.FuncDecl) bool`

### conversation_turn_boundary_test.go

- `func TestConversationTurnRuntimeHasNoAssemblyForwarder(t *testing.T)`
- `func TestChatStateWritesUseTurnStateApplication(t *testing.T)`
- `func TestChatTurnRequestConstructionIsCentralized(t *testing.T)`
- `func scanAssemblySources(t *testing.T, excluded string, visit func(string, *ast.File))`

### migration_context_batch_test.go

- `func TestContextMigrationBatchFilesAreGone(t *testing.T)`

### migration_final_batch_test.go

- `func TestAssemblyRootHasNoRemainingTests(t *testing.T)`

### migration_root_batch_test.go

- `func TestRootMigrationBatchFilesAreGone(t *testing.T)`

### migration_service_session_batch_test.go

- `func TestServiceSessionMigrationBatchFilesAreGone(t *testing.T)`

### migration_session_batch2_test.go

- `func TestSessionMigrationBatch2FilesAreGone(t *testing.T)`

### service_constructor_boundary_test.go

- `func TestConversationTurnConstructorsStayInCompositionRoot(t *testing.T)` — TestConversationTurnConstructorsStayInCompositionRoot prevents each
- `func TestSessionColdConstructorsStayInCompositionRoot(t *testing.T)`
- `func TestEditForkConstructorsStayInCompositionRoot(t *testing.T)`
- `func TestSessionConstructorsStayInCompositionRoot(t *testing.T)`
- `func TestConversationHistoryQueryConstructorStaysInCompositionRoot(t *testing.T)`
- `func TestExtractedApplicationConstructorsStayInCompositionRoot(t *testing.T)`
- `func TestNoFreeServiceConstructors(t *testing.T)`
- `func requireNoFreeConstructors(t *testing.T, prefix string)`

### session_decoupling_test.go

- `func TestCoreHoldsNoSessionContainerFields(t *testing.T)`

### wiring_facade_boundary_test.go

- `func repoRoot(t *testing.T) string`
- `func walkSources(t *testing.T, root string, visit func(path string) error)` — walkSources 遍历仓库源码树，跳过临时目录与 .git。
- `func TestNoSourceImportsRetiredWiring(t *testing.T)` — TestNoSourceImportsRetiredWiring 是本次迁移的第一条红灯：生产代码与测试代码
- `func TestNoWiringPackageOrDirectory(t *testing.T)` — TestNoWiringPackageOrDirectory 禁止以任何形式重建 `wiring` 包：目录名与包名都算。
- `func compositionSurfaceDirs(root string) []string` — compositionSurfaceDirs 是「装配与消费面」：这里只允许真实行为，不允许转发壳。
- `func TestNoConstructorAliasForwarders(t *testing.T)` — TestNoConstructorAliasForwarders 禁止「构造 alias」：函数体只有一句
- `func TestNoSingleMethodFacadeTypes(t *testing.T)` — TestNoSingleMethodFacadeTypes 禁止「单方法 facade」：类型只有 1 个方法，
- `func forEachProductionFile(t *testing.T, directory string, visit func(path string, file *ast.File))`
- `func importedPackageNames(file *ast.File) map[string]bool`
- `func receiverName(expression ast.Expr) string`
- `func singleForwardedCall(body *ast.BlockStmt) (alias string, called string, ok bool)` — singleForwardedCall 判断函数体是否「只有一句 return <pkg>.<name>(...)」，
- `func relativePath(root, path string) string`
