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

## A5.1：Conversation submission / turn

- [x] 旧位置 `application/assembly/service_input.go::submitConversation` 与
      `submitConversationFor` 中重复的 restoring / running / compaction / queue 编排 →
      新唯一位置 `application/conversation.TurnStartApplication.startLocked`
- [x] 旧位置 `application/assembly/chat.go::startChatFor` 的 admission、turn 起点投影和发布顺序 →
      新唯一位置同上；`chat.go` 只保留 `StartCurrent/StartFor` host wrapper
- [x] 旧位置 `submitConversation` 的 request/budget 构造与 draft materialization →
      新唯一位置 `application/conversation.SubmissionApplication`
- [x] 旧位置 `application/assembly/chat.go::reactBudgetFinalizationInput` 与
      `finalizeReActBudgetWithSink` 的 prepare/stream/cleanup 顺序 →
      新唯一位置 `application/conversation.TurnBudgetFinalizerApplication`
- [x] 旧位置 `application/assembly/chat.go::recordUnhandledTaskErrorLocked` 的
      cancel/provider/unverified Task 分类 → 新唯一位置
      `application/conversation.TurnErrorApplication`
- [x] assembly 的预算收尾提示测试迁移为 owner 测试
      `application/conversation/turn_budget_test.go`；保留 assembly 的真实引擎集成用例
- [x] 红灯证据：新增 owner 用例初次编译失败（`TurnStartApplication` /
      `TurnBudgetFinalizerApplication` 不存在），实现后定向用例转绿
- [x] 删除清单证据：`chat.go` 仅剩窄 wrapper 与历史/引擎 host helper；
      `service_input.go` 两条提交入口均只调用一个 owner 方法

## A5.2：Session transition / scope

- [x] 旧位置 `application/assembly/session_lifecycle.go::hotAttachSession` 的
      content/binding/tier/task/plan/approval/workspace/revision/publish 顺序 →
      新唯一位置 `application/session.AttachApplication`
- [x] `application/session/history/attach_host.go` 只把既有 coordinator/residency/
      workspace/task 机制实现成三组窄端口；锁、事务边界和恢复顺序留在 owner
- [x] 旧位置 `application/assembly/session_scope.go::openSessionEngine` 的
      session-routed prompt 能力分支 → `contract.SetSystemPromptFor`
- [x] `hotAttachSession`、cold restore、draft materialization 继续共享
      `Transition → ViewMu → Unit` 锁序；assembly 的 hot-attach 入口只剩一次委托

## A5.3：Draft / permission / interaction

- [x] 旧位置 `application/assembly/service_interaction.go::observeInteraction` 的
      open/close、会话归属、视图单槽和下一笔待批选择 →
      新唯一位置 `application/session.InteractionObserverApplication`
- [x] 旧位置 `service_interaction.go::mirrorPendingApprovalsLocked` 的首笔待批镜像 →
      新唯一位置 `InteractionObserverApplication.MirrorLocked`
- [x] assembly 的 ApprovalBroker observer 与 attach host 均直接绑定该 owner；
      Draft Entry/Materialization 和 permission tier 继续复用既有 owner，不新增第二份判据
- [x] owner 红灯用例覆盖后台审批不覆盖视图单槽、view slot 镜像与事件顺序

## A5.4：Execution / projection / scheduler

- [x] 旧位置 `application/assembly/tool_hooks.go::handleToolStart` 与
      `handleToolCompleteObserved` 的 active/background 分支、request fallback、
      消息投影、runtime 发布和副作用顺序 →
      新唯一位置 `application/execution/toolhooks.ProjectionApplication`
- [x] 旧位置 `application/assembly/snapshot_budget.go::boundToolResultForSnapshot` 的
      preview 判据消费、UTF-8 total/archive/tuple 规则 →
      新唯一位置 `application/session/viewstate.ToolResultBudgetApplication`
- [x] 删除 `application/assembly/plan_tools.go`；历史 Plan 的 current/background
      选择、fallback seed 与 Snapshot 镜像 →
      新唯一位置 `application/execution/taskcontext.PlanProjectionApplication`
- [x] scheduler workspace/plugin/run-params 判据继续由
      `application/admin/scheduler` 唯一持有；assembly 只注入既有依赖
- [x] tool-hooks owner 红灯用例钉住 session fallback、active revision、
      `tool.started`、worktable/runtime 发布顺序

## A6：Composition root 收口

- [x] 旧位置 assembly 内多处 `Engine`/`Runtime` 可选用法断言 →
      新唯一位置 `contract/engine_capabilities.go`
- [x] 收口 capability：routed/resident engine、session prompt、working-history release、
      activate/unload/bundle release、session-backed、per-session execution
- [x] assembly AST 类型断言预算 `41 → 23`；没有新增转发 getter/facade
- [x] 删除 `startChat*`、`submitConversation*`、`finalizeReActBudget`、
      `planProjectionLocked`、`observeInteraction`、tool hook 单调用转发 method；
      调用方直接消费 owner

## A7：测试与预算

- [x] assembly 的 completion 入口断言迁 `application/input/suggestions_test.go`；
      删除 assembly `completion_test.go`
- [x] 删除 assembly 中与 scheduler owner 重复的参数校验测试，保留真实装配集成用例
- [x] context cache 诊断拆为 `//go:build extended`；共享 harness 归
      `context_probe_harness_test.go`，默认 prefix invariant 留在
      `context_prefix_invariant_test.go` 并继续执行
- [x] 预算统计只计默认构建实际匹配的 assembly 测试文件；不把 extended/probe
      文件混入默认套件规模
- [x] 预算收紧：production 50 files / 6468 lines、test 168 files / 33697 lines、
      component fields 65、capability assertions 23
- [x] 测试迁移后预算继续收紧：production 49 files / 6468 lines、
      default tests 128 files / 28217 lines、all tests 132 files / 29993 lines
- [x] 可公开测试外移 `application/assembly/public_api_test.go` →
      `application/assemblytest/public_api_test.go`
- [x] 5 个 session 组件测试迁回 owner：resume、restoring shell、restore recovery、
      restore draft reset、draft entry behavior
- [x] 2 个 worktable 纯测试与 1 个 extended A/B 测试迁回 `kernel/worktable`
- [x] session switch A/B 微基准迁 `application/session/runtime`
- [x] 新增 all-tests ratchet（含 extended）：172 files / 35473 lines；默认套件
      ratchet 收紧到 168 files / 33697 lines
- [x] 3 个 AST 结构门禁迁 `application/assembly/architecture`，不再混在父包测试列表
- [x] 建立 `application/assembly/integration` 测试包并使用本地 fakes/ComponentsFor；
      account、account-window、history-search、notice、toolhook、task-skills、
      session-snapshot 共 7 个测试迁出父包
- [x] 旧位置 `application/assembly/message_seq_scope_test.go` →
      新唯一位置 `application/assembly/integration/message_seq_scope_test.go`；
      测试自持 fake Engine，并通过 `ComponentsFor` 访问 Submission、Snapshot、
      DraftEntry 与 Lifecycle owner
- [x] 旧位置 `application/assembly/role_tool_activity_test.go`、`visible_role_attribution_test.go` →
      新唯一位置 `application/assembly/integration/role_tool_activity_test.go`、
      `visible_role_attribution_test.go`；测试只通过 `ComponentsFor` 和本地 fakes
      访问 RoleActivity、View、Submission owner
- [x] 旧位置 `application/assembly/plugin_source_projection_test.go` →
      新唯一位置 `application/assembly/integration/plugin_source_projection_test.go`；
      真实 Plugin manager、loader 与 catalog 夹具随测试包自持，投影从
      `ComponentsFor(...).Session.View` 读取
- [x] 旧位置 `application/assembly/turn_status_single_word_test.go` →
      新唯一位置 `application/assembly/integration/turn_status_single_word_test.go`；
      快照/存档一致性经 `ComponentsFor`，落盘旧词判据直接依赖 `kernel/task`
      owner 的 `TurnStatusOfRecord`
- [x] B1 根批 20 文件迁移（每批 ≥20）：

| 旧位置 | 新唯一位置 |
|---|---|
| `application/assembly/review_files_test.go` | `application/assembly/integration/review_files_test.go` |
| `application/assembly/fault_guard_test.go` | `application/assembly/integration/fault_guard_test.go` |
| `application/assembly/agentteam_fixture_test.go` | `application/assembly/integration/agentteam_fixture_test.go` |
| `application/assembly/goal_team_wiring_test.go` | `application/assembly/integration/goal_team_wiring_test.go` |
| `application/assembly/fixture_concurrency_test.go` | `application/assembly/integration/fixture_concurrency_test.go` |
| `application/assembly/command_registry_test.go` | `wiring/adapters/command_registry_test.go` |
| `application/assembly/interrupted_continue_test.go` | `application/assembly/integration/interrupted_continue_test.go` |
| `application/assembly/subagent_envelope_store_test.go` | `application/assembly/integration/subagent_envelope_store_test.go` |
| `application/assembly/snapshot_budget_judgement_test.go` | duplicate deleted；owner 判据 `application/session/viewstate/tool_result_budget_test.go` |
| `application/assembly/snapshot_budget_test.go` | `application/assembly/integration/snapshot_budget_test.go` |
| `application/assembly/visible_output_test.go` | `application/assembly/integration/visible_output_test.go` |
| `application/assembly/chat_hot_attach_reasoning_repro_test.go` | `application/assembly/integration/chat_hot_attach_reasoning_repro_test.go` |
| `application/assembly/service_effort_wire_test.go` | `application/assembly/integration/service_effort_wire_test.go` |
| `application/assembly/environment_context_test.go` | `application/assembly/integration/environment_context_test.go` |
| `application/assembly/workspace_file_usecase_test.go` | `application/assembly/integration/workspace_file_usecase_test.go` |
| `application/assembly/workspace_tree_usecase_test.go` | `application/assembly/integration/workspace_tree_usecase_test.go` |
| `application/assembly/workspace_gitrepos_usecase_test.go` | `application/assembly/integration/workspace_gitrepos_usecase_test.go` |
| `application/assembly/close_semantics_test.go` | `application/assembly/integration/close_semantics_test.go` |
| `application/assembly/shutdown_concurrent_test.go` | `application/assembly/integration/shutdown_concurrent_test.go` |
| `application/assembly/history_safety_test.go` | `application/assembly/integration/history_safety_test.go` |
| `application/assembly/hot_attach_running_test.go` | `application/assembly/integration/hot_attach_running_test.go` |

- [x] B2 Context/Compaction 批 25 文件迁移（每批 ≥20）：

| 旧位置 | 新唯一位置 |
|---|---|
| `application/assembly/context_budget_frequency_test.go` | `application/assembly/integration/context_budget_frequency_test.go` |
| `application/assembly/context_budget_last_resort_test.go` | `application/assembly/integration/context_budget_last_resort_test.go` |
| `application/assembly/context_budget_margin_idempotency_test.go` | `application/assembly/integration/context_budget_margin_idempotency_test.go` |
| `application/assembly/context_cache_smoke_test.go` | `application/assembly/integration/context_cache_smoke_test.go` |
| `application/assembly/context_compact_across_rounds_repro_test.go` | `application/assembly/integration/context_compact_across_rounds_repro_test.go` |
| `application/assembly/context_compact_gate_test.go` | `application/assembly/integration/context_compact_gate_test.go` |
| `application/assembly/context_compact_index_test.go` | `application/assembly/integration/context_compact_index_test.go` |
| `application/assembly/context_compact_local_fallback_repro_test.go` | `application/assembly/integration/context_compact_local_fallback_repro_test.go` |
| `application/assembly/context_compact_no_summary_test.go` | `application/assembly/integration/context_compact_no_summary_test.go` |
| `application/assembly/context_compact_progress_test.go` | `application/assembly/integration/context_compact_progress_test.go` |
| `application/assembly/context_compact_selfdeadlock_repro_test.go` | `application/assembly/integration/context_compact_selfdeadlock_repro_test.go` |
| `application/assembly/context_compact_test.go` | `application/assembly/integration/context_compact_test.go` |
| `application/assembly/context_compact_viewmu_hold_repro_test.go` | `application/assembly/integration/context_compact_viewmu_hold_repro_test.go` |
| `application/assembly/context_compaction_body_missing_repro_test.go` | `application/assembly/integration/context_compaction_body_missing_repro_test.go` |
| `application/assembly/context_compaction_facts_restore_repro_test.go` | `application/assembly/integration/context_compaction_facts_restore_repro_test.go` |
| `application/assembly/context_compaction_restart_refill_repro_test.go` | `application/assembly/integration/context_compaction_restart_refill_repro_test.go` |
| `application/assembly/context_compactions_restore_repro_test.go` | `application/assembly/integration/context_compactions_restore_repro_test.go` |
| `application/assembly/context_controller_test.go` | `application/assembly/integration/context_controller_test.go` |
| `application/assembly/context_narration_attribution_test.go` | `application/assembly/integration/context_narration_attribution_test.go` |
| `application/assembly/context_prefix_invariant_test.go` | `application/assembly/integration/context_prefix_invariant_test.go` |
| `application/assembly/context_probe_harness_test.go` | `application/assembly/integration/context_probe_harness_test.go` |
| `application/assembly/context_restore_prefix_order_test.go` | `application/assembly/integration/context_restore_prefix_order_test.go` |
| `application/assembly/context_retain_floor_test.go` | `application/assembly/integration/context_retain_floor_test.go` |
| `application/assembly/context_strategy_ab_probe_test.go` | `application/assembly/integration/context_strategy_ab_probe_test.go` |
| `application/assembly/context_window_rule_test.go` | `application/assembly/integration/context_window_rule_test.go` |

- [x] B3 Service/Session 批 22 文件迁移（每批 ≥20）：

| 旧位置 | 新唯一位置 |
|---|---|
| `application/assembly/archive_reasoning_persist_test.go` | `application/assembly/integration/archive_reasoning_persist_test.go` |
| `application/assembly/archive_session_test.go` | `application/assembly/integration/archive_session_test.go` |
| `application/assembly/async_completion_trigger_test.go` | `application/assembly/integration/async_completion_trigger_test.go` |
| `application/assembly/chat_delegation_test.go` | `application/assembly/integration/chat_delegation_test.go` |
| `application/assembly/chat_state_event_test.go` | `application/assembly/integration/chat_state_event_test.go` |
| `application/assembly/compressed_turn_test.go` | `application/assembly/integration/compressed_turn_test.go` |
| `application/assembly/content_lru_test.go` | `application/assembly/integration/content_lru_test.go` |
| `application/assembly/durable_queue_wire_test.go` | `application/assembly/integration/durable_queue_wire_test.go` |
| `application/assembly/resident_lru_test.go` | `application/assembly/integration/resident_lru_test.go` |
| `application/assembly/service_input_fork_queue_test.go` | `application/assembly/integration/service_input_fork_queue_test.go` |
| `application/assembly/service_knobs_smoke_test.go` | `application/assembly/integration/service_knobs_smoke_test.go` |
| `application/assembly/service_stop_flush_test.go` | `application/assembly/integration/service_stop_flush_test.go` |
| `application/assembly/session_binding_regression_test.go` | `application/assembly/integration/session_binding_regression_test.go` |
| `application/assembly/session_input_index_test.go` | `application/assembly/integration/session_input_index_test.go` |
| `application/assembly/session_live_content_probe_test.go` | `application/assembly/integration/session_live_content_probe_test.go` |
| `application/assembly/session_runtime_slot_integration_test.go` | `application/assembly/integration/session_runtime_slot_integration_test.go` |
| `application/assembly/session_submit_restoring_test.go` | `application/assembly/integration/session_submit_restoring_test.go` |
| `application/assembly/session_switch_failed_resume_test.go` | `application/assembly/integration/session_switch_failed_resume_test.go` |
| `application/assembly/subagent_evidence_test.go` | `application/assembly/integration/subagent_evidence_test.go` |
| `application/assembly/teamwork_board_session_switch_test.go` | `application/assembly/integration/teamwork_board_session_switch_test.go` |
| `application/assembly/teamwork_completion_trigger_test.go` | `application/assembly/integration/teamwork_completion_trigger_test.go` |
| `application/assembly/session_history_pagination_test.go` | `application/assembly/integration/session_history_pagination_test.go` |

- [x] 最终归零批 20 文件迁移（根目录归零）：

| 旧位置 | 新唯一位置 |
|---|---|
| `application/assembly/input_unknown_command_notice_test.go` | `application/assembly/integration/input_unknown_command_notice_test.go` |
| `application/assembly/permission_command_test.go` | `application/assembly/integration/permission_command_test.go` |
| `application/assembly/service_interaction_guard_test.go` | `application/assembly/integration/service_interaction_guard_test.go` |
| `application/assembly/service_scheduler_test.go` | `application/assembly/integration/service_scheduler_test.go` |
| `application/assembly/session_archive_test.go` | `application/assembly/integration/session_archive_test.go` |
| `application/assembly/session_decoupling_test.go` | `application/assembly/architecture/session_decoupling_test.go` |
| `application/assembly/session_draft_shell_test.go` | `application/assembly/integration/session_draft_shell_test.go` |
| `application/assembly/session_draft_snapshot_test.go` | `application/assembly/integration/session_draft_snapshot_test.go` |
| `application/assembly/session_effort_test.go` | `application/assembly/integration/session_effort_test.go` |
| `application/assembly/session_pending_tail_test.go` | `application/assembly/integration/session_pending_tail_test.go` |
| `application/assembly/session_permission_tier_persist_test.go` | `application/assembly/integration/session_permission_tier_persist_test.go` |
| `application/assembly/session_running_not_rerooted_test.go` | `application/assembly/integration/session_running_not_rerooted_test.go` |
| `application/assembly/session_scope_test.go` | `application/assembly/integration/session_scope_test.go` |
| `application/assembly/session_state_display_test.go` | `application/assembly/integration/session_state_display_test.go` |
| `application/assembly/task_execution_test.go` | `application/assembly/integration/task_execution_test.go` |
| `application/assembly/tool_hooks_truncation_test.go` | `application/assembly/integration/tool_hooks_truncation_test.go` |
| `application/assembly/tool_record_isolation_test.go` | `application/assembly/integration/tool_record_isolation_test.go` |
| `application/assembly/tool_record_test.go` | `application/assembly/integration/tool_record_test.go` |
| `application/assembly/service_fakes_test.go` | duplicate deleted；依赖由 integration 包内 fakes 自持 |
| `application/assembly/service_helpers_test.go` | duplicate deleted；依赖由 integration 包内 helpers 自持 |

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
- [x] 其余 scheduler / review / completion 等 owner 行为测试继续迁 owner 包

## T4：默认套件分层

- [x] `//go:build extended`：上下文策略 A/B probe
- [x] `//go:build extended`：会话切换 A/B
- [x] `//go:build extended`：work table A/B
- [x] `//go:build extended`：会话恢复 benchmark
- [x] `realprobe` 继续作为真实会话说探针门
- [x] `context_cache_divergence_probe_test.go` 已随 Context 批迁出父包

## Ratchet

- [x] `serviceComponents` 字段预算
- [x] assembly 测试文件数预算
- [x] assembly 测试代码行预算
- [x] assembly 生产文件数/代码行预算
- [x] 可选类型断言数量预算（`assembly_capability_assertion_limit`，只许下调）

## R1：剩余判据归位（A6 可选项断言收缩）

### R1-a 任务快照作用域

- [x] 旧位置 `application/assembly/project_board_read.go::taskRecordsForSession` →
      新唯一位置 `application/session/viewstate.TaskScope(sessionID, viewSessionID)`
- [x] 删除 `application/assembly/project_board_read.go`（整文件；其
      `interface{ TaskSnapshotFor(string) }` 可选断言为死分支——`contract.RuntimePort`
      已声明 `TaskSnapshotFor`，见 `contract/ports.go:166`）
- [x] 删除 `application/execution/table.RuntimeDeps.TaskRecordsForSession` 端口
      （`runtime.go:26` 字段、`runtime.go:282` 调用点改用
      `runtime.deps.Core.Deps.Runtime.TaskSnapshotFor(viewstate.TaskScope(...))`）
- [x] 删除 `service_assembler.go:499` 的端口装配行
- [x] `service_assembler_applications.go:146` 改为直接调用
      `Deps.Runtime.TaskSnapshotFor(viewstate.TaskScope(sessionID, viewSessionID))`
- [x] 同步 `kernel/board/projection.go:31` 的「唯一实现」表行
- [x] 断言计数 42 → 41（AST 实测；编译器钉住：死分支删除后行为不变）

### R1-b wire 工具结果引用形状

- [x] 旧位置 `application/assembly/reference_tools.go::resolveToolResultRefAlias` +
      `kernel/task/task_context_state.go::wireToolResultRefPrefix` +
      `backend/engine/context/seelectx/processor.go`、`controller.go`、`gap.go` 与
      `seelectx/search/search.go` 的 6 处内联 `"result:"` 构造 + 1 处 `TrimPrefix` 解析 →
      新唯一位置 `contract/protocol.WireToolResultRef` / `WireToolResultCallID`
      （AST 实测：生产代码内 `"result:"` 字面量只剩 `tool_result_page.go:13` 一处；
      同轮新增 `tool_result_page_test.go` 钉住往返与 5 种非 wire 形状）
- [x] `application/reference.Deps.ResolveAlias func(ref string) string` →
      `CallIDAlias func(callID string) (string, bool)`；前缀判据移入 reference 自己的
      `toolResultContent`
- [x] 删除 `application/assembly/reference_tools.go::resolveToolResultRefAlias`，
      装配侧只保留按 callID 查表的窄端口（`CallIDAlias` 闭包 + `ToolResultRefByCallID`）
- [x] 旧位置 `application/assembly/reference_tools.go::hasToolResultRefLocked` 的线性扫描 →
      新唯一位置 `kernel/task.Coordinator.HasToolResultRef(resultRef string) bool`
      （同时补 `HasToolResultRefFor(sessionID, ref)`，与同文件既有 `*For` 命名对齐）
- [x] 同步 `application/reference/application_test.go`（原 `ResolveAlias` 全部改 `CallIDAlias`；
      两个用例拆成四个判据级用例）
- [x] 新增判据级用例：非 wire 形状不查别名、空 callID 不查别名、命中/未命中各一

### R1-c 预算

- [x] `application_budget.json` 新增 `assembly_capability_assertion_limit: 41`，
      `architecture_budget_test.go` 用 AST 统计 `application/assembly` 生产代码的
      `TypeAssertExpr` 数量钉住
- [x] 同步收紧文件/行预算到实测值：`assembly_production_file_limit` 51 → 50 → 49、
      `assembly_production_line_limit` 6891 → 6872 → 6845 → 6701 → 6468 → 6467；
      `assembly_test_file_limit` 192 → 168 → 82 → 0、
      `assembly_test_line_limit` 37382 → 33697 → 19124 → 0；
      `assembly_all_test_file_limit` 192 → 172 → 85 → 0、
      `assembly_all_test_line_limit` 37358 → 35473 → 20201 → 0；
      `assembly_capability_assertion_limit` 41 → 23

### 验收命令

```text
gofmt -l .
go build ./...
go build -tags "gui,desktop,production" ./...
go vet ./...
go test ./application/... ./contract/... ./kernel/task/... ./backend/engine/context/seelectx/... -count=1
go test ./application/ -run TestApplicationArchitectureBudget -count=1 -v
```

