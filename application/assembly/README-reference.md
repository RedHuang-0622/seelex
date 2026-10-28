# assembly/reference（根包分卷）

## 生态位

read_tool_result / read_plan 引用工具

覆盖：`reference*.go`；未归属文件由覆盖自检拦下。

引用工具与压缩轮次读取的纯应用编排已迁到
[`application/reference`](../reference/README.md)。本分卷只保留 `Service` 一行转发，
以及 Engine、Task、SessionStore 的窄回调装配。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### reference_tools.go

- `func (service *Service) nodeToolResult(nodeID, ref string) (string, bool)` — nodeToolResult reads a subagent tool result through the Engine bridge.
