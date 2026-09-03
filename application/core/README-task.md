# core/task

## 生态位

任务执行集成测试

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### task_execution_test.go

- `func TestTaskTerminalHandlerRecordsBoundedCompletion(t *testing.T)`
- `func TestTaskFailedRequiresFailureType(t *testing.T)`
- `func TestTaskNeedsUserDecisionRecordsDistinctTerminalState(t *testing.T)`
- `func TestTaskCompleteRequiresAllAuthoritativePlanNodes(t *testing.T)`
- `func TestNaturalStopWithPendingAuthoritativePlanNeedsUserDecision(t *testing.T)`
- `func TestContextControllerCompactsAndCleansInternalCheckpoint(t *testing.T)`
- `func TestContextControllerRepeatedCompactionDoesNotAccumulateCheckpoints(t *testing.T)`
- `func TestTaskContextRecoveryHistoryKeepsOnlyProductSystemInstruction(t *testing.T)`
- `func TestContextControllerRejectsLargeToolOutputBeforeGlobalCompaction(t *testing.T)`
- `func TestTaskContextSummaryRetainsCompletedToolEvidence(t *testing.T)`
- `func TestTaskContextSummaryIgnoresMetadataOnlyCheckpoint(t *testing.T)`
- `func TestInterruptedTaskContinuationCarriesCheckpointAndSkills(t *testing.T)`
- `func TestTaskContextSummaryStaysWithinProviderToolBudget(t *testing.T)`
- `func TestNoProgressBudgetStopsRepeatedToolRounds(t *testing.T)`

### task_service_test.go

- `func TestTaskCompleteRejectedWhenProjectionNotConverged(t *testing.T)`
- `func TestTaskCompleteFlushConvergesProjectionBeforeVerdict(t *testing.T)`
- `func TestTaskCompleteRejectedWhenProjectionFlushFails(t *testing.T)`
- `func TestTerminalResumeRecordKeepsObjectiveAndQueuedInputs(t *testing.T)`
- `func TestOnChatEndKeepsResumeRecord(t *testing.T)`
- `func TestCheckNodeMarksNodeCompletedInTasklist(t *testing.T)`
- `func TestCheckNodeRejectsUnknownNode(t *testing.T)`
- `func TestCheckNodeRequiresLoadedPlanAndNodeID(t *testing.T)`
- `func TestCheckNodeIdempotentAndDoesNotReplayEpoch(t *testing.T)`
- `func TestTaskCompleteCoversAlreadyCheckedNodes(t *testing.T)` — TestTaskCompleteCoversAlreadyCheckedNodes 验证歧义消除：在途打点已完成的节点
- `func TestTaskCompleteStillRejectsUncheckedNodes(t *testing.T)` — TestTaskCompleteStillRejectsUncheckedNodes 打点流下缺节点仍拒绝：
- `func TestNoProgressBudgetReadsTaskServiceSemanticProgress(t *testing.T)`

### task_skills_for_test.go

- `func TestActiveSkillsProjectionForSession(t *testing.T)` — TestActiveSkillsProjectionForSession（G1-A）：skill/目标可见性按会话取
