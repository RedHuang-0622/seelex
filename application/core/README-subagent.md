# core/subagent

## 生态位

子代理投影集成测试

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### subagent_detail_test.go

- `func TestSubagentSessionDetailMissingNode(t *testing.T)` — TestAdaptSubagentConversation 验证会话记录适配：截断（evidence_chars）、
- `func TestSubagentSessionDetailCarriesContext(t *testing.T)` — TestAdaptSubagentContext 验证上下文快照适配：截断（evidence_chars）、
- `func TestSubagentSessionDetailCarriesWorktree(t *testing.T)` — TestSubagentSessionDetailCarriesWorktree 验证失败现场恢复入口：节点
- `func TestScheduledTasksProjectIntoRuntimeSnapshot(t *testing.T)` — TestScheduledTasksProjectIntoRuntimeSnapshot 验证周期任务与白名单命令经
- `func TestTodoItemsProjectIntoRuntimeSnapshot(t *testing.T)` — TestTodoItemsProjectIntoRuntimeSnapshot 验证 todolist 清单经运行时投影
- `func strPtr(value string) *string`

### subagent_tree_test.go

- `func TestHandlePlanNodeCompleteProjectsSubAgentTree(t *testing.T)` — TestHandlePlanNodeCompleteProjectsSubAgentTree 验证 plan 节点事件把
- `func TestHandlePlanBranchEventProjectsSubAgentTree(t *testing.T)` — TestHandlePlanBranchEventProjectsSubAgentTree 验证分支生命周期事件同样
- `func TestCollectRuntimeProjectionCarriesSubAgentTree(t *testing.T)` — TestCollectRuntimeProjectionCarriesSubAgentTree 验证权威 Snapshot 投影
