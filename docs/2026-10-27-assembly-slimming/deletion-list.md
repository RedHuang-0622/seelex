# Assembly 瘦身清单

## A1：Conversation turn

- [x] 删除 `conversationTurnCompletion` Service 字段
- [x] 删除 `conversationTurnOutcome` Service 字段
- [x] 删除 `conversationTurnPublisher` Service 字段
- [x] 删除 `conversationTurnTail` Service 字段
- [x] 删除 `conversationTurnCommit` Service 字段
- [x] 删除 `conversationTurnFinalize` Service 字段
- [x] 删除 `conversationTurnInput` Service 字段
- [x] 删除 `conversationTurnResponse` Service 字段
- [x] 删除 `conversationTurnRecovery` Service 字段
- [x] 删除 `conversationTurnCleanup` Service 字段
- [x] 删除 `conversationTurnExecution` Service 字段
- [x] 保留 `conversationTurnProcess` 与 `conversationTurnRunner`
- [x] 删除 turn stage 的字段存在性/反射测试
- [x] 行为测试迁 `application/conversation` 的 `conversation_test`

## A2：Edit fork

- [x] `editForkOperationRegistry`
- [x] `editForkValidator`
- [x] `editForkTargetResolver`
- [x] `editForkRollback`
- [x] `editForkProcess`
- [x] 保留 `editForkAnnotation`（运行期快照装配仍消费）
- [x] 删除对应字段存在性测试

## A3：Session cold

- [x] `sessionColdRead`
- [x] `sessionColdHistory`
- [x] `sessionColdRestore`
- [x] `sessionColdWorkspace`
- [x] `sessionColdActivation`
- [x] `sessionColdTask`
- [x] `sessionColdView`
- [x] `sessionColdPublish`
- [x] `sessionColdCommit`
- [x] 删除对应字段存在性测试

## A4：装配期外部端口与无状态 owner

- [x] `historyWindowAssembler` → builder 参数
- [x] `accountWindowPolicy` → builder 参数
- [x] `skillActivation` → builder 参数
- [x] `sessionRecords` → builder 参数
- [x] Draft Submission / Materialization / Entry 三字段合并为 `sessionDrafts`
- [x] `sessionForkLineage` → cold pipeline / cold snapshot 调用点自行构造
- [x] 删除只剩旧常量和旧注释的 `session_message_edit.go`
- [x] 删除无调用的 `editForkOperation` alias
- [x] conversation turn Process/Runner 合并为 `conversationTurn`
- [x] edit-fork Edit/Annotation 合并为 `editFork`
- [x] queue enqueue / durable mirror / recovery 迁 `conversation.QueueApplication`
- [x] 删除 `session_queue_recover.go`
- [x] scheduled workspace/plugin/run-params 判据迁 `application/admin/scheduler`
- [x] content reload 迁 `session/history.Runtime.ReloadContent`
- [x] 删除 `session_content_reload.go`
- [x] 删除测试专用 `asyncState*` 常量 alias

## T1：测试文件合并候选

- [x] 22 个分散的 `...IsAssembled` 反射断言合并为 owner graph 门禁
- [x] 删除 `goal_application_test.go`
- [x] 保留 `input_router_compat_test.go`（真实装配行为）
- [x] 删除 `service_admin_application_test.go`
- [x] 保留 `service_notice_test.go`（真实 AddNotice 行为）
- [x] 删除 `session_archive_component_test.go`
- [x] 删除 `session_delete_component_test.go`
- [x] 删除 `session_lifecycle_component_test.go`
- [x] 删除 `session_meta_application_test.go`
- [x] 删除 `subagent_application_test.go`
- [x] 删除 `work_table_application_test.go`

## T2：外部测试包

- [x] assembly 公开 API 测试迁 `package assembly_test`
- [x] application 根重复 boundary 测试合并
- [x] edit-fork owner 行为测试迁 `application/conversation/edit` 的 `edit_test`
- [x] session-cold 九个 stage 测试迁 `application/session/cold` 的 `cold_test`
- [x] draft submission / materialization / fork-lineage owner 测试迁 `application/session` 的 `session_test`
- [x] policy owner 行为测试迁 `application/policy` 的 `policy_test`
- [x] 删除纯字段存在性、反射和旧转发断言
- [ ] 其余 scheduler / review / completion 等 owner 行为测试继续迁 owner 包

## T4：默认套件分层

- [x] `//go:build extended`：上下文策略 A/B probe
- [x] `//go:build extended`：会话切换 A/B
- [x] `//go:build extended`：work table A/B
- [x] `//go:build extended`：会话恢复 benchmark
- [x] `realprobe` 继续作为真实会话说探针门
- [ ] 拆分 `context_cache_divergence_probe_test.go` 的共享 harness 与两个纯诊断用例

## Ratchet

- [x] `serviceComponents` 字段预算
- [x] assembly 测试文件数预算
- [x] assembly 测试代码行预算
- [x] assembly 生产文件数/代码行预算

