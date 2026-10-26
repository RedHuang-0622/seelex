# core/reference（根包分卷）

## 生态位

read_tool_result / read_plan / read_compressed_turn 的 core 兼容面与宿主适配。

覆盖：`reference*.go`；未归属文件由覆盖自检拦下。

引用工具与压缩轮次读取的纯应用编排已迁到
[`application/reference`](../reference/README.md)。本分卷只保留 `Service` 一行转发，
以及 Engine、Task、SessionStore 的窄回调装配。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### reference_tools.go

- `func (service *Service) ReadToolResultHandler(_ context.Context, argsJSON string) (string, error)` — 引用工具分页默认值收编进 seele.yaml limits 段
- `func (service *Service) ToolResultContent(_ context.Context, resultRef string, offset, limit int) (model.ToolResultPage, error)` — ToolResultContent 按 result_ref 分页读回完整工具输出（GUI 面：快照被
- `func (service *Service) nodeToolResult(nodeID, ref string) (string, bool)` — nodeToolResult 读回子代理工具结果（引擎桥；Engine 未装配 → 不可用）。
- `func (service *Service) hasToolResultRefLocked(resultRef string) bool` — hasToolResultRefLocked 判定该 ref 是否在当前会话的**内存态**归档里
- `func (service *Service) resolveToolResultRefAlias(ref string) string` — resolveToolResultRefAlias 把模型常见的 result:call_<callID> 引用映射为
- `func (service *Service) ReadPlanHandler(_ context.Context, argsJSON string) (string, error)`
- `func (service *Service) referenceApplicationComponent() *applicationreference.Application`
- `func newServiceReferenceApplication(service *Service) *applicationreference.Application`

### reference_tools_test.go

- `func TestReferenceApplicationIsAssembled(t *testing.T)`
- `func TestReferenceFacadeOnlyDelegatesToApplication(t *testing.T)`
- `func TestReadToolResultResolvesNodeRef(t *testing.T)` — TestReadToolResultResolvesNodeRef 验证 P1 桥：read_tool_result 对
- `func TestReadToolResultResolvesCallAlias(t *testing.T)` — TestReadToolResultResolvesCallAlias 验证 result:call_<callID> 别名映射：
