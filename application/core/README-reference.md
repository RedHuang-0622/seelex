# core/reference

## 生态位

read_tool_result / read_plan 引用工具

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### reference_tools.go

- `func (service *Service) ReadToolResultHandler(_ context.Context, argsJSON string) (string, error)` — 引用工具分页默认值收编进 seele.yaml limits 段
- `func (service *Service) resolveToolResultRefAlias(ref string) string` — resolveToolResultRefAlias 把模型常见的 result:call_<callID> 引用映射为
- `func nodeResultRef(ref string) (string, bool)` — nodeResultRef 解析 node:<nodeID>: 前缀的子代理结果引用；非节点引用 → false。
- `func (service *Service) nodeToolResult(nodeID, ref string) (string, bool)` — nodeToolResult 读回子代理工具结果（引擎桥；Engine 未装配 → 不可用）。
- `func (service *Service) hasToolResultRefLocked(resultRef string) bool`
- `func encodeToolResultPage(result StoredToolResult, offset, limit int, contains string) (string, error)`
- `func utf8Start(value string, index int) int`
- `func utf8End(value string, index int) int`
- `func (service *Service) ReadPlanHandler(_ context.Context, argsJSON string) (string, error)`

### reference_tools_test.go

- `func TestNodeResultRefParser(t *testing.T)`
- `func TestReadToolResultResolvesNodeRef(t *testing.T)` — TestReadToolResultResolvesNodeRef 验证 P1 桥：read_tool_result 对
- `func TestReadToolResultResolvesCallAlias(t *testing.T)` — TestReadToolResultResolvesCallAlias 验证 result:call_<callID> 别名映射：
- `func TestEncodeToolResultPageNodeContent(t *testing.T)`
