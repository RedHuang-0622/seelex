# Application 模块化实施进度

日期：2026-10-25

本文是
[`plan.md`](plan.md)
的执行记录。代码与测试仍是“已实现”能力的最终事实源；本文只记录批次状态、
结构指标和跨会话门禁。

依赖方向与三个裁决项的冻结结论见
[`docs/arch/application-middle-backend-boundaries.md`](../../docs/arch/application-middle-backend-boundaries.md)。

下一阶段可直接使用
[`next-session-prompt.md`](next-session-prompt.md)
启动 Conversation 聚合的设计门禁。

## 批次进度

| 批次 | 状态 | 本轮改动 | 退出证据 |
|---|---|---|---|
| AP-001 | 已完成 | 171 方法冻结、直接内部依赖白名单、兼容层台账 | `application/architecture_budget_test.go` |
| AP-002 | 已完成 | 新增 13 个能力型入站接口；旧四组接口与 171 方法 `Service` 保持兼容 | `application/api_partition_test.go` |
| AP-003 | 已完成 | 入站 API 去具体 backend/store 类型；Goal/Command/Search/Config 契约化 | `application/api_contract_test.go` |
| AP-004 | 已完成 | 抽只读 `SessionQuery`：目录、项目根、元数据、目录刷新等待 | `application/query_faces_test.go`、`application/core/session_query.go` |
| AP-005 | 已完成 | 抽 `ConversationHistoryQuery`；冷读移出共享写锁 | 历史分页回归、慢 I/O 锁行为测试 |
| AP-006 | 已完成 | 将 `ExecutionQueryAPI` 拆成 5 个只读能力切片，旧组合面保持兼容 | `application/api_partition_test.go` |
| AP-007 | 首个切片完成 | 设计门禁 + Conversation Queue 应用组件 | `ap-007-conversation-design.md`、`service_queue_component_test.go` |
| AP-008 | 首个切片完成 | `ChatState` 单一写入点组件；主循环仍在后续批次迁移 | `chat_turn_state_test.go` |
| AP-009 | 首个切片完成 | ChatTurn 请求工厂、队列批次合并与引用提取 | `chat_turn_request.go`、结构门禁 |
| AP-010 | 首个切片完成 | Session unload 应用编排组件 | `session_lifecycle_component_test.go` |
| AP-011 | 首个切片完成 | Session archive 宿主适配组件 | `session_archive_component_test.go` |
| AP-012 | 首个切片完成 | Session delete 应用编排组件 | `session_delete_component_test.go` |
| AP-013 | 首个切片完成 | SessionQuery 覆盖 snapshot/transcript/input-index durable 读取面 | `query_faces_test.go`、查询回归 |
| AP-014 | 首个切片完成 | ChatTurnProcess 起点事务组件 | `chat_turn_process_test.go` |
| AP-015 | 首个切片完成 | Goal Application 用例组件 | `goal_application_test.go` |
| AP-016 | 首个切片完成 | Subagent Application 用例组件 | `subagent_application_test.go` |
| AP-017 | 首个切片完成 | Execution Application 用例组件 | `work_table_application_test.go` |
| AP-018 | 首个切片完成 | ChatTurn 持久化完成与 durable queue 结算事务组件 | `chat_turn_completion_test.go`、结构门禁 |
| AP-019 | 首个切片完成 | ChatTurn 收尾后的队列提升决策组件 | `chat_turn_process_test.go`、结构门禁 |
| AP-020 | 首个切片完成 | ChatTurn stale、错误与 runtime projection 结果组件 | `chat_turn_outcome_test.go`、结构门禁 |
| AP-021 | 首个切片完成 | ChatTurn 终态事件发布组件 | `chat_turn_publication_test.go`、结构门禁 |
| AP-022 | 首个切片完成 | ChatTurn ChatState/Task/revision 终态镜像组件 | `chat_turn_outcome_test.go`、结构门禁 |
| AP-023 | 首个切片完成 | ChatTurn 解锁后异步尾段组件 | `chat_turn_tail_test.go`、结构门禁 |
| AP-024 | 首个切片完成 | 显式草稿提交激活组件 | `session_draft_submission_component_test.go`、结构门禁 |
| AP-025 | 首个切片完成 | 草稿首请求物化编排组件 | `session_draft_materialization_component_test.go`、结构门禁 |
| AP-026 | 首个切片完成 | 新建会话进入草稿编排组件 | `session_draft_entry_component_test.go`、结构门禁 |
| AP-027 | 首个切片完成 | Resume hot/restoring/cold 顶层编排组件 | `session_resume_component_test.go`、结构门禁 |
| AP-028 | 首个切片完成 | Edit/Fork 幂等操作注册表 | `edit_fork_operation_registry_test.go`、结构门禁 |
| AP-029 | 首个切片完成 | Edit/Fork 父会话准入组件 | `edit_fork_validator_test.go`、结构门禁 |
| AP-030 | 首个切片完成 | Edit/Fork 目标事件与段落 cut 解析组件 | `edit_fork_target_resolver_test.go`、结构门禁 |
| AP-031 | 首个切片完成 | Edit/Fork child 原子编排组件 | `edit_fork_process_test.go`、结构门禁 |
| AP-032 | 首个切片完成 | Edit/Fork 回滚错误合成组件 | `edit_fork_rollback_test.go`、结构门禁 |
| AP-033 | 首个切片完成 | Edit/Fork 可见消息编辑判据组件 | `edit_fork_annotation_test.go`、结构门禁 |
| AP-034 | 首个切片完成 | Fork lineage 只读解析组件 | `session_fork_lineage_test.go`、结构门禁 |
| AP-035 | 首个切片完成 | Restoring 空壳共享投影组件 | `session_restoring_shell_test.go`、结构门禁 |
| AP-036 | 首个切片完成 | 冷加载失败恢复组件 | `session_restore_recovery_test.go`、结构门禁 |
| AP-037 | 首个切片完成 | 冷恢复 draft reset 组件 | `session_restore_draft_reset_test.go`、结构门禁 |
| AP-038 | 首个切片完成 | 冷加载持久化数据读取组件 | `session_cold_load_reader_test.go`、结构门禁 |
| AP-039 | 首个切片完成 | 冷加载 history 装配组件 | `session_cold_history_test.go`、结构门禁 |
| AP-040 | 首个切片完成 | 冷加载 Engine 恢复与队列回填组件 | `session_cold_restore_test.go`、结构门禁 |
| AP-041 | 首个切片完成 | 冷加载 workspace 绑定组件 | `session_cold_workspace_test.go`、结构门禁 |
| AP-042 | 首个切片完成 | 冷加载锁内激活准备组件 | `session_cold_activation_test.go`、结构门禁 |
| AP-043 | 首个切片完成 | 冷加载 Task 恢复组件 | `session_cold_task_test.go`、结构门禁 |
| AP-044 | 首个切片完成 | 冷加载 View 构造与安装组件 | `session_cold_view_test.go`、结构门禁 |
| AP-045 | 首个切片完成 | 冷加载解锁后发布组件 | `session_cold_publish_test.go`、结构门禁 |
| AP-046 | 首个切片完成 | 冷加载锁内投影收口组件 | `session_cold_commit_test.go`、结构门禁 |
| AP-047 | 首个切片完成 | ChatTurn Engine 前输入准备组件 | `chat_turn_input_test.go`、结构门禁 |
| AP-048 | 首个切片完成 | ChatTurn Engine 返回事务组件 | `chat_turn_response_test.go`、结构门禁 |
| AP-049 | 首个切片完成 | ChatTurn 恢复与终态组件 | `chat_turn_recovery_test.go`、结构门禁 |
| AP-050 | 首个切片完成 | ChatTurn 清理与持久化前准备组件 | `chat_turn_cleanup_test.go`、结构门禁 |
| AP-051 | 首个切片完成 | ChatTurn Commit 组件 | `chat_turn_commit_test.go`、结构门禁 |
| AP-052 | 首个切片完成 | ChatTurn Finalize 组件 | `chat_turn_finalize_test.go`、结构门禁 |
| AP-053 | 首个切片完成 | ChatTurn 执行策略同步组件 | `chat_turn_execution_test.go`、结构门禁 |
| AP-054 | 首个切片完成 | Policy Application 首批状态/操作收口 | `service_policy_application_test.go`、结构门禁 |
| AP-055 | 首个切片完成 | PermissionTier 用例组件化 | `service_policy_application_test.go`、结构门禁 |
| AP-056 | 首个切片完成 | Effort 切换用例组件化 | `service_policy_application_test.go`、结构门禁 |
| AP-057 | 首个切片完成 | Plugin 切换用例组件化 | `service_policy_application_test.go`、结构门禁 |
| AP-058 | 首个切片完成 | Admin 账号管理用例组件化 | `service_admin_application_test.go`、结构门禁 |
| AP-059 | 首个切片完成 | Admin 账号选择用例组件化 | `service_admin_application_test.go`、结构门禁 |
| AP-060 | 首个切片完成 | Workspace Environment 用例组件化 | `session_ssh_test.go`、结构门禁 |
| AP-061 | 首个切片完成 | Admin 账号窗口用例组件化 | `service_admin_application_test.go`、结构门禁 |
| AP-062 | 首个切片完成 | Workspace Git 查询用例组件化 | `workspace_gitrepos_usecase_test.go`、结构门禁 |
| AP-063 | 首个切片完成 | Workspace 批量登记用例组件化 | `workspace_tree_usecase_test.go`、结构门禁 |
| AP-064 | 首个切片完成 | Session SSH 用例组件化 | `session_ssh_test.go`、结构门禁 |
| AP-065 | 首个切片完成 | Role Account 用例组件化 | `role_permissions_test.go`、结构门禁 |
| AP-066 | 首个切片完成 | Role Permission Vector 用例组件化 | `role_permissions_test.go`、结构门禁 |
| AP-067 | 首个切片完成 | Role Inheritance / SSH Paths 用例组件化 | `role_permissions_test.go`、结构门禁 |
| AP-068 | 首个切片完成 | Scheduled Task CRUD 用例组件化 | `service_scheduler_test.go`、结构门禁 |
| AP-069 | 首个切片完成 | Runtime Refresh 用例组件化 | `service_scheduler_test.go`、结构门禁 |
| AP-070 | 首个切片完成 | Scheduled Run 装配用例组件化 | `service_scheduler_test.go`、结构门禁 |
| AP-071 | 首个切片完成 | Scheduled Session 顶层用例组件化 | `service_scheduler_test.go`、结构门禁 |
| AP-072 | 首个切片完成 | File Review Host 用例组件化 | `review_files_test.go`、状态归属门禁 |
| AP-073 | 首个切片完成 | Lifecycle drain/cancel 用例组件化 | `service_input_test.go`、结构门禁 |
| AP-074 | 首个切片完成 | Lifecycle Shutdown 用例组件化 | `service_input_test.go`、幂等/顺序门禁 |
| AP-075 | 已完成 | History Search 契约化与依赖下降 | 39 → 38 依赖预算红/绿 |
| AP-076 | 首个切片完成 | History Search Application 组件化 | `history_search_test.go`、结构门禁 |
| AP-077 | 首个切片完成 | Input Completion 用例组件化 | `completion_test.go`、结构门禁 |
| AP-078 | 首个切片完成 | Reference Tools 用例组件化 | `reference_tools_test.go`、结构门禁 |
| AP-079 | 首个切片完成 | Session Metadata 用例组件化 | `session_meta_application_test.go`、结构门禁 |
| AP-080 | 首个切片完成 | Project Board 只读查询组件化 | `project_board_read_test.go`、结构门禁 |
| AP-081 | 首个切片完成 | Session Release 编排组件化 | `session_release_test.go`、结构门禁 |
| AP-082 | 首个切片完成 | Context Compaction 用例组件化 | `context_compact_test.go`、结构门禁 |
| AP-083 | 首个切片完成 | Fault Guard 用例组件化 | `fault_guard_test.go`、状态归属门禁 |
| AP-084 | 首个切片完成 | Performance Stats 用例组件化 | `service_snapshot_test.go`、结构门禁 |
| AP-085 | 已完成 | Reference 用例迁出 core 到 `application/reference` | `application/reference_package_test.go`、`application/reference` parity、core 净减 |
| AP-086 | 已完成 | Session Meta/SSH/Release 迁出 core 到 `application/session` | 包内测试、SessionMeta/SessionSSH/Release 回归 |
| AP-087 | 已完成 | Input Completion 迁出 core 到 `application/input` | `application/input` 测试、Suggestions 回归 |
| AP-088 | 已完成 | History Search 迁出 core 到 `application/conversation/search` | 包内测试、SearchHistory 回归 |
| AP-089 | 已完成 | PerfStats 迁出 core 到 `application/observability` | 包内测试、PerfStats 回归 |
| AP-090 | 组件完成，待迁包 | Async/Teamwork Completion 编排组件化 | `async_completion_trigger_test.go`、Teamwork 回归 |
| AP-091 | 已完成 | Lifecycle/Fault Guard 迁出 core 到 `application/lifecycle` | 包内测试、drain/shutdown/fault parity |
| AP-092 | 已完成 | Role Account/Permission/Inheritance 迁出 core 到 `application/policy` | 包内测试、Role 回归 |
| AP-093 | 已完成 | `read_compressed_turn` 迁出 core 到 `application/reference` | 包内 parity、结构门禁、core 文件/行数净减 |
| AP-094 | 已完成 | Admin 账号用例迁出 core 到 `application/admin` | 包内测试、单行兼容门禁、facade closure |
| AP-095 | 已完成 | Workspace 环境/Git/批量登记迁出 core 到 `application/workspace` | 包内 containment/顺序测试、core 文件/行数净减 |
| AP-096 | 已完成 | 只读 `SessionQuery` 迁出 core 到 `application/session/query` | 包内读锁/委托测试、结构门禁、core 行数净减 |
| AP-097 | 已完成 | ConversationHistoryQuery 迁出 core 到 `application/history` | 分页决策测试、共享写锁门禁、core 行数净减 |
| AP-098 | 已完成 | ProjectBoardQuery 迁出 core 到 `application/execution` | WorkTable/Team Plan 查询测试、core 行数净减 |
| AP-099 | 已完成 | WorkItem/teammate 与 Subagent 应用迁出 core 到 `application/execution` | Runtime 委托/状态测试、core 行数净减 |
| AP-100 | 已完成 | Async/Teamwork Completion 迁出 core 到 `application/execution` | 终态扫描/忙会话/幂等测试、core 行数净减 |
| AP-101 | 已完成 | File Review 应用迁出 core 到 `application/review` | request ID/会话投影测试、core 行数净减 |
| AP-102 | 已完成 | Runtime Projection 应用迁出 core 到 `application/admin` | 锁/发布顺序测试、core 行数净减 |
| AP-103 | 已完成 | Goal Application 迁出 core 到 `application/goal` | Coordinator port/刷新测试、core 行数净减 |
| AP-104 | 已完成 | Session unload/archive/delete 迁出 core 到 `application/session` | 顺序/错误短路测试、core 行数净减 |
| AP-105 | 已完成 | Draft submit/Resume/Restore 编排迁出 core 到 `application/session` | 分支/锁/回退测试、core 行数净减 |
| AP-106 | 已完成 | Draft Entry/Materialization 迁出 core 到 `application/session` | 顺序/运行中旧会话测试、core 行数净减 |
| AP-107A | 已完成 | Cold Read/History/Restore/Workspace/Activation 迁出 core | 包内读取/装配测试、core 行数净减 |
| AP-107B | 已完成 | Cold Task/View/Commit/Publish 迁出 core | 包内组件测试、core 行数净减 |
| AP-108A | 已完成 | Fork lineage / Restoring shell 迁出 core | 解码/投影测试、core 行数净减 |
| AP-109 | 已完成 | Scheduled Task CRUD/Run/Session 迁出 core 到 `application/admin/scheduler` | 默认参数/校验/发布测试、core 行数净减 |
| AP-110 | 已完成 | Policy switching 迁出 core 到 `application/policy` | full path/错误短路测试、core 行数净减 |
| AP-111 | 已完成 | Context Compaction 迁出 core 到 `application/conversation/context` | outcome/回执/门禁测试、core 行数净减 |
| AP-112A | 已完成 | Conversation Queue 迁出 core 到 `application/conversation` | 队列顺序/事件/状态测试、core 行数净减 |
| AP-112B | 已完成 | ChatState transitions 迁出 core 到 `application/conversation` | ChatState 转换测试、core 行数净减 |
| AP-113A | 已完成 | ChatRequest value/factory 迁出 core 到 `application/conversation` | 请求工厂/批次合并测试、core 行数净减 |
| AP-113B | 已完成 | Turn Input/Response/Recovery/Cleanup 迁出 core | 流水线组件测试、core 行数净减 |
| AP-113C | 已完成 | Turn Process/Outcome/Completion/Commit/Finalize/Publication/Tail/Execution 迁出 core | 原子流水线测试、core 行数净减 |
| AP-113D+ | 已完成 runner 切片 | reasoning 与 `runChat` application 编排已迁出；Engine loop 仍归 Seele | AP-113D1/D2 |
| AP-113D1 | 已完成 | Chat reasoning 步骤提取/配对/fallback 迁出 core | 包内 reasoning parity、core 行数净减 |
| AP-113D2 | 已完成 | `runChat` assembly/runner 迁入 conversation 包 | 组件顺序 parity、core 一行 wrapper |
| AP-114A | 已完成 | Edit/Fork registry/validator/resolver/process/rollback/annotation 迁出 core | 组件 parity、core 行数净减 |
| AP-114B | 已完成 | EditUserMessageAndFork 顶层输入/幂等编排迁出 core | operation waiter/校验测试、core 行数净减 |
| AP-115A | 已完成 | ToolHookBridge 的 Seele hook 适配、工具 ID 配对与诊断迁出 core | 包内 parity、结构门禁、core 行数净减 |
| AP-115B | 已完成 | Tool Record 累计/暂存/批量落盘迁出 core | 包内 parity、core 回归、core 行数净减 |
| AP-115C1 | 已完成 | WorkTable publisher、latest-wins 与 payload 去重迁出 core | publisher parity、结构门禁、core 行数净减 |
| AP-115C2a | 已完成 | WorkTable TraceBlock 输入投影迁出 core | 行预算/会话过滤测试、core 行数净减 |
| AP-115C2b1 | 已完成 | async/teamwork WorkTable consumer 迁出 core | signal/retry parity、core 行数净减 |
| AP-115C2b2 | 已完成 | subagent/task consumer 与三路 start-once 迁出 core | lifecycle dispatch/stop、core 行数净减 |
| AP-115C2c | 已完成 | WorkItem/批次输入投影进入 table 包 | row/batch parity、core 行数净减 |
| EP-060 | 已完成 | WorkTable runtime、源同步、生命周期消费者与 Todo 刷新迁入 table | 结构门禁、core WorkTable 全量回归 |
| EP-061 | 已完成 | Session restore/view-intent 与目录富化状态迁入 viewstate | 结构门禁、viewstate/core 回归 |
| EP-062 | 已完成 | resident/content LRU 与释放保护状态迁入 session/residency | 结构门禁、residency/core 回归 |
| EP-063 | 已完成 | ToolRecord teammate/subagent host adapter 迁入 toolrecord | 结构门禁、toolrecord/core 回归 |
| EP-064 | 已完成 | 环境尾部渲染与 Session/Workspace 取数适配迁入 prompt | 结构门禁、prompt/core 回归 |
| EP-065 | 已完成 | explicit compaction gate 与 deferred submit 迁入 conversation/context | 结构门禁、context/core 回归 |
| EP-066 | 已完成 | SessionInputIndex 读取编排迁入 session/query | 结构门禁、query/core 回归 |
| EP-067 | 已完成 | provider context recovery/cleanup 迁入 conversation/context | 结构门禁、recovery/core 回归 |
| AP-115D1 | 已完成 | 基础内置命令注册迁入 `application/input/command` | 包内命令 parity、core 行数净减 |
| AP-115D2 | 已完成 | compact/session/plugin/effort/permission 命令编排迁出 core | mutation parity、core 行数净减 |
| AP-108B1 | 已完成 | Session Runtime 只读投影迁入 `application/session/query` | 字段归属/深拷贝测试、core 行数净减 |
| AP-108B2a | 已完成 | Snapshot 冷/热路径选择迁入 session/query | 分支 parity、core 一行 wrapper |
| AP-108B2b | 已完成 | 驻留 Snapshot 组装与 enrichment 迁入 session/query | snapshot/alias parity、core 行数净减 |
| AP-116A | 已完成 | 7 个无外部调用的 Service 辅助方法收回包内 | 调用方审计、164 方法预算绿 |
| AP-116B | 已完成 | Goal/ActiveSkill/lifecycle 辅助入口收回包内 | 接口收窄、158 方法预算绿 |
| AP-116C | 已完成 | TeamworkBoardViewFor 改为内部 view-port 适配器 | 接口收窄、157 方法预算绿 |
| AP-116D | 已完成 | ClearReviewRequest 改为 review 组件内部调用 | 调用方审计、156 方法预算绿 |
| AP-116E | 已完成 | 8 个前端共用入口先迁依赖方，再由 `Frontend` 直接承接 | GUI/TUI/headless/wiring 迁移、148 方法预算绿 |
| AP-116F1 | 已完成 | Admin/Role/Workspace 50 个纯转发入口迁入 `application/compat` | core 删除 3 个转发文件、98 方法预算绿 |
| AP-116F2a | 已完成 | 15 个只读查询入口迁入 compat；history 端口落地 | core 直接依赖 34 → 33、83 方法预算绿 |
| AP-116F2b | 已完成 | Context/Reference/Review/Goal/Scheduler/Lifecycle 19 个入口迁入 compat | core 依赖 33 → 32、64 方法预算绿 |
| AP-116F2c | 已完成 | Session/Subagent 16 个入口迁入 compat | core 依赖 32 → 28、48 方法预算绿 |
| AP-116F2d | 已完成 | Policy/Lifecycle/State 14 个入口迁入 compat | 34 方法预算、core 12,302 行 |
| AP-116F2e | 已完成 | ProjectBoard 装配读面改端口，Resume/Activate 迁入 compat | 30 方法预算、core 12,282 行 |
| AP-116F2f | 已完成 | Goal tool handlers 迁入 application/goal；ProjectBoard/Resume 壳收口 | 26 方法预算、core 11,846 行 |
| AP-116F2g | 已完成 | Notice/Runtime/ToolRecord 最后 3 个公开壳迁入 compat | 23 个状态方法预算、core 11,843 行 |
| EP-001 | 已完成 | History Window 外部端口 | core import 归零、端口/adapter parity、依赖预算 33 |
| EP-002A | 已完成 | Residency 外部端口 | core residency import 归零、adapter parity、依赖预算 32 |
| EP-002B | 受阻 | SessionUseCase 端口 | 需完整 SessionUnit/View 状态，等待聚合归属裁决 |
| EP-003 | 已完成 | Conversation Stream 外部端口 | core engine/react import 归零、依赖预算 31 |
| EP-004 | 已完成 | Account Window 外部端口 | core accountwindow import 归零、依赖预算 30 |
| EP-005 | 已完成 | Skill Activation 外部端口 | core engine/skill import 归零、依赖预算 29 |
| EP-006 | 已完成 | Session Records 外部端口 | core sessionaccess import 归零、adapter parity、依赖预算 28 |
| EP-007 | 已完成 | Error Presentation 外部端口 | core middle/view/errors import 归零、adapter parity、依赖预算 27 |
| EP-008 | 已完成 | Context Retain Floor 契约化 | core seelectx import 归零、契约/Engine parity、依赖预算 26 |
| EP-009 | 已完成 | Prompt Assets 入口收口 | core prompt/assets import 归零、Engine prompt façade、依赖预算 25 |
| EP-010 | 已完成 | Approval Broker 工厂外置 | core limit/approval import 归零、factory port、依赖预算 24 |
| EP-011 | 已完成 | Input Router 外部端口 | core backend/engine/input import 归零、factory port、依赖预算 23 |
| EP-012 | 已完成 | Subagent Tool Event DTO 收口 | core subagent domain import 归零、依赖预算 22 |
| EP-013 | 已完成 | Team Role Sessions 端口 | core multiagent/team import 归零、删除 core 适配文件、依赖预算 21 |
| EP-014 | 已完成 | Teamwork Board Reader 端口 | core multiagent/teamwork import 归零、reader parity、依赖预算 20 |
| EP-015 | 已完成 | Assemble DTO / Host Session 收口 | core engine/assemble import 归零、删除死读取文件、依赖预算 19 |
| EP-016 | 已完成 | Goal Session Coordinator 迁入 Application | core multiagent/goal import 归零、core 净减 290 行、依赖预算 18 |
| EP-017 | 已完成 | Middle Model alias 退场 | core 直连 `contract/protocol`、Middle alias 保留、依赖预算 17 |
| EP-018 | 已完成 | Middle View 只读模型归位 | Diag/投影迁入 Application、core `middle/view` import 归零、依赖预算 16 |
| EP-019 | 已完成 | Engine Prompt Coordinator 入口归位 | core import 归零、Application identity alias、依赖预算 15 |
| EP-020 | 已完成 | Context Window Policy 入口归位 | core contextcontrol import 归零、Application window alias、依赖预算 14 |
| EP-021 | 已完成 | Event Protocol 入口归位 | core Middle event import 归零、Application identity facade、依赖预算 13 |
| EP-022 | 已完成 | Board Contract 入口归位 | core middle/view/board import 归零、Application identity alias、依赖预算 12 |
| EP-023 | 已完成 | Subagent View 入口归位 | core middle/view/subagent import 归零、Application identity alias、依赖预算 11 |
| EP-024 | 已完成 | WorkTable Projection 入口归位 | core middle/view/worktable import 归零、Application table facade、依赖预算 10 |
| EP-025 | 已完成 | View State 入口归位 | core middle/view/state import 归零、Application identity facade、依赖预算 9 |
| EP-026 | 已完成 | Switch Workspace 入口归位 | core middle/switch/workspace import 归零、Application workspace alias、依赖预算 8 |
| EP-027 | 已完成 | Switch Policy 入口归位 | core middle/switch import 归零、Application switchpolicy facade、依赖预算 7 |
| EP-028 | 已完成 | Limit Tier 入口归位 | core backend/limit/tier import 归零、Application policy alias、依赖预算 6 |
| EP-029 | 已完成 | Limit Base 入口归位 | core backend/limit import 归零、Application policy alias、依赖预算 5 |
| EP-030 | 已完成 | Engine Context 入口归位 | core backend/engine/context import 归零、conversation/context facade、依赖预算 4 |
| EP-031 | 已完成 | Engine Task Context 入口归位 | core backend/engine/task import 归零、execution/taskcontext facade、依赖预算 3 |
| EP-032 | 已完成 | Core assembly helper 转发退场 | 删除 11 个纯转发 helper、core 净减 52 行、结构门禁覆盖 |
| EP-033 | 已完成 | Engine Session Runtime 入口归位 | core backend/engine/session import 归零、session/runtime facade、依赖预算 2 |
| EP-034 | 已完成 | Core 剩余纯 helper 转发退场 | teamwork/sigil/fork/window 6 个 wrapper 删除、core 净减 28 行 |
| EP-035 | 已完成 | Session Context helper 转发退场 | 删除 session_ctx.go；测试专用 shim 不进入生产面 |
| EP-036 | 已完成 | ApplyLimits 兼容入口退场 | 组合根与测试直接调用 application/policy.Apply |
| EP-037 | 已完成 | Limits 读取与 ReleaseWindow 写入归位 | 删除 core Limits；ApplySessionReleaseWindow 迁入 policy |
| EP-038 | 已完成 | CompressedTurn Archiver 与 Goal Store 解析归位 | Store commit port、Event DTO alias、core 净减 104 行 |
| EP-039 | 已完成 | Event 段落与 Queue 恢复 DTO 契约化 | `contract/protocol` 唯一段落实现、`contract/dto` queue DTO、core Store import 7 → 1 |
| EP-040 | 已完成 | ProjectBoard Store 类型边归位 | execution 类型门面、core Store import 归零、直接依赖 2 → 1 |
| EP-041 | 已完成 | 层边界裁决与 Application 全局依赖预算 | Session 聚合归 session、全局 concrete dependency 30 冻结 |
| EP-042 | 已完成 | Conversation window pure helper 归位 | history 不再依赖 view_state，为 SessionState 迁移清障 |
| EP-043 | 已完成 | SessionState / ViewState 物理迁入 Application | core 直接依赖归零、middle sessionusecase/viewstate 删除 |
| EP-044 | 已完成 | TeamworkPlan 五类实体完整 DTO 化 | contract 唯一定义、Store/backend alias、execution 迁移桥删除 |
| EP-045 | 已完成 | Fork lineage 改用窄 JSON 投影 | `application/session` 退出 Store 载荷类型依赖 |

## 基线指标

| 指标 | 2026-10-25 计划值 | 本轮起点 |
|---|---:|---:|
| `Service` 公开方法 | 171 | 64 |
| `application/core` 直接导入 backend/middle/store 包 | 38 | 37 |
| `application/core` 直接导入 backend/middle/store 包（本轮终点） | 38 | 37 |
| `application/core` 生产 Go 文件 | 76 | 126 |

AP-075 移除 `seelectx/search` 后降至 38；Reference 包提取后
`backend/tool/reference` 也退出 core，当前终点为 37。曾尝试新增
`application/conversation/query` 适配层来把 `middle/switch/history` 移出
`application/core`，但 `e2e/facade_boundary_test.go` 的 I1 门禁禁止门面依赖闭包
新增包；为保持既有边界且不复制历史算法，本轮撤回该适配层并保留直接依赖。
`middle/switch/history` 直接依赖仍存在，后续下降需要 Middle 会话提供闭包内端口。

## 高冲突文件

以下文件在迁移期间单车道修改：

- `application/application.go`
- `application/faces.go`
- `application/core/service_api_faces.go`
- `application/core/service.go`
- `application/core/service_components.go`
- `application/core/service_snapshot.go`

## 兼容层台账

兼容层必须记录旧接口、新接口、当前调用方、删除里程碑、归零验证和回退方式。
兼容实现只允许一行转发；任何状态、分支、写盘或事件发布都表示迁移尚未完成。

| 旧接口 | 新接口 | 当前调用方 | 删除里程碑 | 归零验证 | 回退方式 |
|---|---|---|---|---|---|
| `ViewFace` | 按能力拆分的只读接口 | GUI/TUI/Bridge 待迁 | M8/AP-008 | `rg '\\bViewFace\\b' gui tui wiring e2e` | 调用方继续持 `ViewFace` |
| `CommandFace` | 按能力拆分的命令接口 | GUI/TUI/Bridge 待迁 | M8/AP-008 | `rg '\\bCommandFace\\b' gui tui wiring e2e` | 调用方继续持 `CommandFace` |
| `PolicyFace` | `PolicyAPI` / `AdminAPI` | GUI/TUI/Bridge 待迁 | M8/AP-008 | 旧接口生产调用为零 | 恢复 `PolicyFace` 消费 |
| `QueueFace` | `QueueAPI` | GUI/TUI/Bridge 待迁 | M8/AP-008 | 旧接口生产调用为零 | 恢复 `QueueFace` 消费 |
| 171 方法 `application.Service` | 能力型小接口组合 | 门面与旧宿主 | M8/M9 | `application/testdata/service_methods.txt` 递减 | 恢复完整 `Service` 消费 |
| `Service.ListSessions` / `ProjectRootFor` / `SessionMeta` / `WaitCatalogRefresh` | `SessionQuery` | GUI/TUI/headless 待迁 | M8/AP-008 | 旧方法只一行转发，生产可直接消费 `SessionQuery` | 恢复查询方法体 |
| `Frontend.LoadMoreHistory` / `LoadLatestHistory` | `ConversationHistoryQuery` | GUI、TUI、headless | AP-116F | `Service` 已无同名公开方法，前端直接调用历史组件 | 用 `Frontend` 包住 `Service` 恢复兼容入口 |
| `application/core -> middle/switch/history` | `application/history.WindowAssembler` + Wiring 注入 `history.Assembler` | History 查询宿主 | EP-001（已完成） | core import 为零；依赖预算 33 | 恢复 core 直接 import 与旧构造签名 |
| `application/core -> middle/residency` | `application/session.ResidencyPort` + Wiring 注入 Middle 判据 | core Session/Residency 宿主 | EP-002A（已完成） | core import 为零；依赖预算 32 | 恢复 core 直接 import 与旧 deps |
| `application/core -> middle/switch/sessionusecase` | `application/session/state` 的状态与测试 | core Chat/Queue/Session 生命周期 | EP-043（已完成） | core import 为零；旧 Middle 包已删除 | 将状态包移回 Middle，恢复 core 直接 import |
| `application/core -> store` | Session/Event/ToolResult/Queue/Teamwork repository ports + pure DTO | compressed turn、edit/fork、project board、goal store、queue recovery | EP-038/039/040（已完成） | `rg 'github.com/RedHuang-0622/seelex/store' application/core` 归零 | 恢复 core 直接 import 与 store payload |
| `application/core -> backend/engine/react stream primitives` | `application/conversation.StreamOutputPort` + Wiring adapter | Chat 流与冷读正文适配 | EP-003（已完成） | core import 为零 | 恢复 engine/react 直接 import |
| `application/core -> backend/limit/accountwindow` | `application/admin.AccountWindowPolicy` + Wiring adapter | 账号窗口设置 | EP-004（已完成） | core import 为零 | 恢复直接构造 Setter |
| `application/core -> backend/engine/skill` | `application/policy.SkillActivationPort` + Wiring adapter | skill_activate | EP-005（已完成） | core import 为零 | 恢复直接构造 Activator |
| `application/core -> middle/view/sessionaccess` | `application/conversation.SessionRecordsPort` + Wiring adapter | 主会话 ConversationView 分页 | EP-006（已完成） | core import 为零；adapter 与原分页函数 parity | 恢复直接调用 `PaginateRecords` |
| `application/core -> middle/view/errors` | `contract.ErrorPresentationPort` + Wiring adapter | Chat 预算错误、Tool Hook 错误出口 | EP-007（已完成） | core import 为零；adapter 与原呈现函数 parity | 恢复直接调用 `viewerrors.Wrap/PresentToolError` |
| `application/core -> backend/engine/context/seelectx` | `contract.RetainFloorTokens` + Engine 兼容转发 | Retain floor 与启动期窗口校验 | EP-008（已完成） | core import 为零；数学实现只有 Contract 一份 | 恢复 core 调用 `seelexctx.RetainFloorTokens` |
| `application/core -> backend/engine/prompt/assets` | `backend/engine/prompt.ValidateAssets` | Application 启动期提示词资产校验 | EP-009（已完成） | core import 为零；资产解析/校验仍只有 assets 一份 | 恢复 core 直接调用 `promptassets.Validate` |
| `application/core -> backend/limit/approval` | `ExternalPorts.ApprovalFactory` + Wiring adapter | `Dependencies.Approval` 缺省装配 | EP-010（已完成） | core import 为零；Broker 状态机仍只有 Backend 一份 | 恢复 core 直接构造 `limitapproval.ApprovalBroker` |
| `application/core -> backend/engine/input` | `contract.CommandRegistryPort` / `InputDispatcher` + Wiring factories | 内置命令注册与规范化输入分派 | EP-011（已完成） | core import 为零；前缀路由与注册表仍只有 Backend 一份 | 恢复 core 直接持有 `CommandRegistry` / `Router` |
| `application/core -> backend/multiagent/subagent` | `dto.SubagentToolEvent` | 子代理工具事件兼容入口 | EP-012（已完成） | core import 为零；Backend 类型继续是同身份 alias | 恢复 core 使用 Backend alias |
| `application/core -> backend/multiagent/team` | `contract.RoleSessionsPort` + Wiring factory | GUI compat 的 RoleSession/Schedule 入口 | EP-013（已完成） | core import 为零；可选能力解析与错误文案仍只有 Team 一份 | 恢复 core `roleSessions()` 直接构造 `team.RoleSessions` |
| `application/core -> backend/multiagent/teamwork` | `application/execution.BoardReader` + Wiring factory | WorkItem 的团队看板/teammate 只读投影 | EP-014（已完成） | core import 为零；reader parity 覆盖三方法 | 恢复 core 直接构造 `teamwork.NewBoardReader` |
| `application/core -> backend/engine/assemble` | `dto.ScheduledTaskSpec` + `ExternalPorts.HostSessionForToolCall` | 调度参数与 goal 只读会话归属 | EP-015（已完成） | core import 为零；Context 读取实现仍只有 Assemble 一份 | 恢复 core 直接 import `assemble` |
| `application/core -> backend/multiagent/goal` | `application/goal.Coordinator` | 会话级 Controller 缓存与治理投影 | EP-016（已完成） | core import 为零；Goal 状态机仍只在 Backend | 恢复 core `goal_coordinator.go` |
| `application/core -> middle/protocol/model` | `contract/protocol` | Snapshot/Message/Plan/View 等全部 wire 形状 | EP-017（已完成） | core import 为零；Middle 路径继续同身份 alias | 恢复 core 使用 Middle alias |
| `application/core -> middle/view` | `application/observability.RenderDiag` + `application/admin` runtime projections | `/diag` 与回合尾 Runtime 投影 | EP-018（已完成） | core import 为零；纯投影测试随能力迁入 Application | 恢复 core 调用 `middle/view` |
| `application/core -> backend/engine/prompt` | `application/prompt.Coordinator` identity alias + `ValidateAssets` | system prompt 组装与同步 | EP-019（已完成） | core import 为零；实现与缓存仍只有 Engine prompt 一份 | 恢复 core 直接 import Engine prompt |
| `application/core -> backend/engine/input/contextcontrol` | `application/window` identity alias + one-line forwarders | window 配置加载、应用与策略构造 | EP-020（已完成） | core import 为零；配置/策略实现仍只有 Engine contextcontrol 一份 | 恢复 core 直接 import contextcontrol |
| `application/core -> middle/protocol/event` | `application/event` identity facade | Event/Hub/Subscription/kind 与 NewEventHub | EP-021（已完成） | core import 为零；投递、序号和过滤仍只有 Middle event 一份 | 恢复 core 直接 import Middle event |
| `application/core -> middle/view/board` | `application/execution` identity aliases | ProjectBoardSink / TeamworkPlanSource | EP-022（已完成） | core import 为零；同步器与可选计划读面仍只有 Middle board 一份 | 恢复 core 直接 import board |
| `application/core -> middle/view/subagent` | `application/execution.SubagentViewCoordinator` identity alias | 子代理详情与实时事件投影 | EP-023（已完成） | core import 为零；投影状态与工具记录判据仍只有 Middle subagent view 一份 | 恢复 core 直接 import Middle subagent view |
| `application/core -> middle/view/worktable` | `application/execution/table` identity aliases + forwarders | WorkTable rows/batches、clone/status/evidence 纯函数 | EP-024（已完成） | core import 为零；投影算法仍只有 Middle worktable 一份 | 恢复 core 直接 import Middle worktable |
| `application/core -> middle/view/state` | `application/session/viewstate` 唯一 Coordinator | Snapshot/message/revision 编排与 projection helper | EP-043（已完成） | core import 为零；Middle view state 已删除，状态与 revision 只在 Application | 将 Coordinator 移回 Middle，恢复 core 直接 import |
| `application/core -> middle/switch/workspace` | `application/workspace` identity aliases + `NewSwitch` | workspace create/bind/draft/projection | EP-026（已完成） | core import 为零；workspace 用例与 projection 仍只有 Middle 一份 | 恢复 core 直接 import switch workspace |
| `application/core -> middle/switch` | `application/session/switchpolicy` identity facade | queue/draft/interaction/release/fork/pending-tail 纯判据 | EP-027（已完成） | core import 为零；词表、错误和判据仍只有 Middle switch 一份 | 恢复 core 直接 import Middle switch |
| `application/core -> backend/limit/tier` | `application/policy.TierDeps/TierGate` identity aliases | 主会话权限档位持久化与生效编排 | EP-028（已完成） | core import 为零；档位判据与持久化仍只有 Backend tier 一份 | 恢复 core 直接 import limit/tier |
| `application/core -> backend/limit` | `application/policy` identity aliases + forwarders | 进程级 limits、Knobs、release window | EP-029（已完成） | core import 为零；默认值、原子快照和窗口判据仍只有 Backend limit 一份 | 恢复 core 直接 import backend/limit |
| `application/core -> backend/engine/context` | `application/conversation/context` Engine aliases + forwarders | compaction/history coordinator、predicate、warning | EP-030（已完成） | core import 为零；压缩、历史安全与分类判据仍只有 Engine context 一份 | 恢复 core 直接 import Engine context |
| `application/core -> backend/engine/task` | `application/execution/taskcontext` identity aliases + forwarders | Task coordinator、transcript tail、plan/tool/context helpers | EP-031（已完成） | core import 为零；Task 状态、ledger 与 transcript 判据仍只有 Engine task 一份 | 恢复 core 直接 import Engine task |
| `application/core -> backend/engine/session` | `application/session/runtime` identity aliases + forwarders | Session Coordinator、Location、title/workspace/fork helpers | EP-033（已完成） | core import 为零；durable session 状态、binding 与恢复策略仍只有 Engine session 一份 | 恢复 core 直接 import Engine session |
| `application.Command/CommandResult` | `contract.Command/CommandResult` | GUI/TUI/命令注册 | M8 | 根门面不再 import backend input | 恢复 concrete alias |
| `goaldomain.*GoalRecord/Request/StatusView` | `dto.GoalRecord/BeginGoalRequest/UpdateGoalRequest/FinishGoalRequest/GoalStatusView` | GUI/headless/Application GoalAPI | M8 | 类型 identity 由 goal domain alias 保持 | 恢复 goal 本地定义 |
| `assemble.ScheduledTaskSpec/Status` | `dto.ScheduledTaskSpec/Status` | GUI/调度注册 | M8 | 入站 API import 扫描通过 | 恢复 assemble alias |
| `seelexctxsearch.Result` | `dto.SearchResult` | GUI 历史检索 | M8 | 入站 API import 扫描通过 | 恢复搜索包 alias |
| `ProjectBoardTeamPlanFor/StateFor` 入站方法 | `Service` 装配面方法 | `wiring/bootstrap` 项目看板装配 | M8 | GUI/TUI 生产调用为零 | 恢复旧接口声明 |
| `compat.Frontend.AddAccount/AccountDetails/UpdateAccount/DeleteAccount/AccountBalance` | `adminApplication`；入站仍由 `AdminAPI` 暴露 | GUI/Bridge 现有 `AdminAPI` 路径 | AP-116F2 | `Service` 已无同名入口，compat 只一行转发 | 用 `Frontend` 包住 `Service` 恢复兼容入口 |
| `Frontend.SelectAccount` | `adminApplication.SelectAccount`；入站仍由 `AdminAPI` 暴露 | GUI Bridge、TUI | AP-116F | `Service` 已无同名公开方法，前端直达管理组件 | 用 `Frontend` 包住 `Service` 恢复兼容入口 |
| `compat.Frontend.WorkspaceEnvironments/SetWorkspaceEnvironment/DeleteWorkspaceEnvironment/SetDefaultWorkspaceEnvironment` | `workspaceEnvironmentApplication`；入站仍由 `WorkspaceAPI` 暴露 | GUI Bridge | AP-116F2 | `Service` 已无同名入口，compat 只一行转发 | 用 `Frontend` 包住 `Service` 恢复兼容入口 |
| `compat.Frontend.SetAccountWindow` | `adminApplication.SetAccountWindow`；入站仍由 `AdminAPI` 暴露 | GUI Bridge | AP-116F2 | `Service` 已无同名入口，compat 只一行转发 | 用 `Frontend` 包住 `Service` 恢复兼容入口 |
| `compat.Frontend.WorkspaceGitRepos/WorkspaceGitLog/WorkspaceChanges/WorkspaceGitCommitDetail/WorkspaceGitCommitFileContent` | `workspaceGitApplication`；入站仍由 `WorkspaceAPI` 暴露 | GUI Bridge、工作区面板 | AP-116F2 | `Service` 已无同名入口，compat 只一行转发 | 用 `Frontend` 包住 `Service` 恢复兼容入口 |
| `compat.Frontend.CreateWorkspaces` | `workspaceApplication.CreateWorkspaces`；入站仍由 `WorkspaceAPI` 暴露 | GUI Bridge | AP-116F2 | `Service` 已无同名入口，compat 只一行转发 | 用 `Frontend` 包住 `Service` 恢复兼容入口 |
| `Service.SessionSSHState/SetSessionSSHAllowed/SetSessionSSHEnv` | `sessionSSHApplication`；入站仍由 `WorkspaceAPI` 暴露 | GUI Bridge | M8 | 三个旧方法只一行转发，生产调用迁到窄 API | 恢复 `session_ssh.go` 原应用判据 |
| `compat.Frontend.RoleAccount/SetRoleAccount` | `roleAccountApplication`；入站仍由 `RoleAPI` 暴露 | GUI 角色权限板、会话加载 | AP-116F2 | `Service` 已无同名入口，compat 只一行转发 | 用 `Frontend` 包住 `Service` 恢复兼容入口 |
| `compat.Frontend.RoleDefaultVector/SetRoleDefaultVector/RolePermissionVector/SetRolePermissionVector` | `rolePermissionApplication`；入站仍由 `RoleAPI` 暴露 | GUI 角色权限板、工具权限判定 | AP-116F2 | `Service` 已无同名入口，compat 只一行转发 | 用 `Frontend` 包住 `Service` 恢复兼容入口 |
| `compat.Frontend.RoleInheritMainAgent/SetRoleInheritMainAgent/RoleSSHUserSpaces/RoleDefaultSSHUserSpaces/SetRoleSSHUserSpaces/SetRoleSessionSSHUserSpaces` | `roleInheritanceApplication`；入站仍由 `RoleAPI` 暴露 | GUI 角色权限板、工具路径门 | AP-116F2 | `Service` 已无同名入口，compat 只一行转发 | 用 `Frontend` 包住 `Service` 恢复兼容入口 |
| `Service.ScheduleTask/UpdateScheduledTask/CancelScheduledTask` | `schedulerApplication`；入站仍由 `AdminAPI` 暴露 | GUI 定时任务面板 | M8 | 三个旧方法只一行转发，生产调用迁到窄 API | 恢复 `service_scheduler.go` 原变更编排 |
| `Service.RefreshRuntimeSnapshot` | `runtimeApplication.Refresh` | Scheduler observer、Subagent 恢复 | M8 | 旧方法只一行转发 | 恢复 `service_scheduler.go` 原投影刷新 |
| `Service.AssembleScheduledRun` | `scheduledRunApplication` | Wiring Registry、定时会话 | M8 | 旧方法只一行转发 | 恢复提交前装配编排 |
| `Service.StartScheduledSession` | `scheduledSessionApplication` | Wiring Registry | M8 | 旧方法只一行转发 | 恢复新会话顶层编排 |
| `Service.RequestFileReview/ClearReviewRequest` | `reviewApplication`；逐文件裁决仍由 `backend/tool/review` 持有 | Workspace/Review tool host | M8 | 两个旧方法只一行转发；`serviceState.reviewSeq` 已移除 | 恢复 `review_files.go` 原 Service Host |
| `Service.BeginGracefulShutdown/WaitForIdle/AnyChatRunning/CancelAllChats/CancelChat/Shutdown` | `lifecycleApplication` | Wiring/Bridge/TUI/headless | M8 | 六个旧方法只一行转发 | 恢复 `service_input.go` 原生命周期编排 |
| `seelexctxsearch.Result` in core History Search | `dto.SearchResult` | GUI Bridge、search_history tool | 已到 D4（core 调用面） | core 不再 import `seelectx/search`；预算 38 | 无回退需要；类型 identity 已由契约层固定 |
| `Service.SearchHistory/SearchHistoryHandler` | `historySearchApplication` | GUI Bridge、search_history tool | M8 | 两个旧方法只一行转发 | 恢复 `history_search.go` 原 Service 适配 |
| `Frontend.Suggestions` | `completionApplication` | GUI/TUI 输入建议 | AP-116F | `Service` 已无同名公开方法，前端直达输入建议组件 | 用 `Frontend` 包住 `Service` 恢复兼容入口 |
| `Frontend.SwitchEffort` / `SwitchPlugin` | `policyApplication` | GUI Bridge、TUI、命令与 e2e harness | AP-116F | `Service` 已无同名公开方法，前端直达策略组件 | 用 `Frontend` 包住 `Service` 恢复兼容入口 |
| `Frontend.ReorderQueuedInput` / `RecallQueuedInput` | `conversationQueue` | GUI Bridge、TUI | AP-116F | `Service` 已无同名公开方法，前端直达队列组件 | 用 `Frontend` 包住 `Service` 恢复兼容入口 |
| `Service.ReadToolResultHandler/ToolResultContent/ReadPlanHandler` | `application/reference.Application` | Wiring Registry、GUI 工具结果详情 | M8 | 三个旧方法只一行转发；core 不再定义引用用例实现 | 恢复 `reference_tools.go` 原内部组件 |
| `Service.SetSessionMeta/SessionMeta` | `sessionMetaApplication` | GUI 会话树 | M8 | 两个旧方法只一行转发 | 恢复 `session_meta.go` 原端口装配 |
| `Service.WorkTableRowsFor/ProjectRootFor/ProjectBoardTeamPlanFor/ProjectBoardTeamStateFor` | `projectBoardQuery` | Wiring 项目看板同步器 | M8 | 四个旧方法只一行转发 | 恢复 `project_board_read.go` 原只读装配 |
| `Service.releaseSessionMemory/reconcileSessionRelease` | `sessionReleaseApplication` | 驻留/正文 LRU、unload/archive/delete、chat tail | M8 | 两个旧方法只一行转发 | 恢复 `session_release.go` 原释放编排 |
| `Service.CompactContextNow/CompactContextHandler` | `contextCompactionApplication` | `/compact`、compact_context tool | M8 | 两个旧方法只一行转发 | 恢复 `context_compact.go` 原 Service 编排 |
| `Service.degradeAndRequestExit/handleLifecycleFault` | `faultGuardApplication` | lifecycle consumers、background probes | M8 | 两个旧方法只一行转发；`serviceState.degradeOnce` 已移除 | 恢复 `fault_guard.go` 原 Service 状态与顺序 |
| `Service.PerfStats` | `perfStatsApplication` | GUI/headless 性能面板 | M8 | 旧方法只一行转发 | 恢复 `perf_stats.go` 原锁内取数 |

## AP-002 实施记录

能力型接口都由 `Service` 结构满足，单接口方法数 1–19。GUI/TUI 本轮
不迁移，因此旧 `ViewFace`/`CommandFace`/`PolicyFace`/`QueueFace` 继续作为
兼容入口，方法集和行为均未改变。`api_partition_test.go` 钉住两条不变量：

1. 任一能力 API 不得超过 20 个方法；
2. 四个旧兼容接口的每个方法都至少被一个新能力接口覆盖。

## AP-003 实施记录

红测：`application/api_contract_test.go` 扫描根门面与能力接口文件，初始发现
`assemble`、搜索、goal 和 store 四类具体实现 import。

实施：

- `contract/command.go` 承接命令接口与结果 DTO，`backend/engine/input` 保留 alias；
- `contract/dto/goal.go` 承接 Goal 数据形状；`backend/multiagent/goal` 用 alias
  保持旧名称，`Clone` / `ChangesDefinition` 改为域自由函数；
- `contract/dto/websearch_config.go` 与 `account_window.go` 承接配置/挡位纯数据；
- `ScheduledTaskSpec/Status` 和 `SearchResult` 的入站签名改用 `dto`；
- `ApprovalBroker` 改为 `contract.ApprovalBroker` 接口；
- 项目看板 teamwork plan/state 方法移出入站接口，登记为装配面能力。

结果：

- 入站 API 文件不再 import 具体 backend/store/middle-switch/view 包；
- JSON 字段、事件和持久化形状未变；
- `Service` 公开方法总数仍为 171；
- `application/core` 内部直接依赖仍为 39；该数字不再用于冒充入站 API 指标。

验证证据：

- `application/api_contract_test.go` 对根门面与能力接口文件的具体 import 扫描通过；
- `contract/dto/goal_test.go` 钉住 GoalRecord / Goal Update 的 JSON 字段与状态词；
- Application、contract、e2e、Goal、Input、Search、AccountWindow、Wiring、GUI 测试通过；
- `go build ./...` 与 `go vet ./...` 通过。

## AP-004 / AP-005 实施记录

纯搬运语义：

- `Service.ListSessions` 的组合顺序移入 `sessionQuery.ListSessions`；
- `ProjectRootFor`、`SessionMeta`、`WaitCatalogRefresh` 移入 `sessionQuery`；
- `LoadMoreHistory`、`LoadLatestHistory` 的编排移入
  `conversationHistoryQuery`，既有分页 helper 保持原位置和唯一实现。

删除/合并的重复位置：

- `session_history.go` 原方法体 → `session_conversation_history_query.go`；
- `service_snapshot.go` 原 `ListSessions` 方法体 → `session_query.go`；
- `project_board_read.go`、`session_meta.go`、`session_catalog.go` 的查询方法体
  → `session_query.go`；
- 历史 helper 仍直接调用 `middle/switch/history`；尝试新增 Application 适配包会
  违反 I1 门面闭包，因此没有引入第二份实现或未登记依赖。

门禁：

- 查询对象结构不持有 `*Service`；
- 查询实现文件禁止 `ViewMu.Lock`；
- 慢历史存储读取期间 `Snapshot` 必须可读，证明查询不持共享写锁；
- 171 方法总数不变，旧入口全部保留；
- 直接内部依赖保持 39，38 目标因 I1 闭包约束仍未完成。

## AP-006 实施记录

`ExecutionQueryAPI` 的 13 个方法按消费能力拆为五个只读接口：

- `WorkItemQueryAPI`：2 个方法；
- `TeammateQueryAPI`：3 个方法；
- `TeamworkQueryAPI`：3 个方法；
- `SubagentQueryAPI`：2 个方法；
- `TranscriptQueryAPI`：3 个方法。

旧 `ExecutionQueryAPI` 通过嵌入五个接口保留，方法集与行为不变；根
`application/faces_api.go` 已是入站接口事实源。门禁同时钉住三件事：

1. 每个切片最多 4 个方法；
2. 同一个方法不得落入两个切片；
3. 旧组合面的每个方法都必须被某个切片覆盖。

## AP-007 设计门禁与首个切片

设计结论见
[`ap-007-conversation-design.md`](ap-007-conversation-design.md)。已冻结：

- `Conversation`、`ChatTurn`、`Queue`、`HistoryWindow`、`Session` 的边界；
- `SessionUnit`、`View`、`Snapshot`、恢复记录的状态所有权与锁顺序；
- 单活动回合、队列下标、回合提升、历史窗口、释放与 revision 不变量；
- AP-003 拆成“入站 API 具体实现类型零泄漏”和
  “`application/core` 内部依赖预算”两条独立验收指标。

首个切片“Queue application ownership”已实施：

- 新增消费方私有 `conversationQueueUnit` 端口，`SessionUnit` 结构满足；
- 新增 `conversationQueue` 组件，承接 `ReorderQueuedInput` /
  `RecallQueuedInput` 的锁内编排与锁外发布；
- `Service` 两个旧方法改为一行转发，`QueueAPI` / `QueueFace` 签名不变；
- 不进入 `chat.go` 主循环，不改 durable queue、revision、恢复和 wire。

删除/合并：

- `service_queue.go:83 queueEditGuardLocked` → `chat_queue.go:94 conversationQueue.guardLocked`
- `service_queue.go:98 queuedInputUnitLocked` → `chat_queue.go:123 newServiceConversationQueue.ResolveUnit`
- `service_queue.go:112 applyQueueEditLocked` → `chat_queue.go:106 conversationQueue.applyEditLocked`

结构门禁：

- `conversationQueue` 结构不持有 `*Service`、`InputQueue` 或队列副本；
- 两个旧公开方法必须只含一行 `conversationQueue` 转发；
- `conversationQueueUnit` 由 `SessionUnit` 编译期结构满足。

## AP-008 首个切片实施记录

新增 `conversationTurnState`，集中以下 ChatState 转换：

- 开始回合：写入 `Running/RequestID/StartedAt`；
- 队列投影：从 `SessionUnit.QueueProjection` 更新输入队列和计数；
- 当前回合错误清理/写入；
- 回合收尾：`Running=false`，保留 request 元数据；
- cold/draft reset：恢复空 ChatState。

`SessionUnit.Chat` 仍是唯一状态所有者；组件不缓存 ChatState。生产文件直接
`SetChatState/UpdateChat` 的写入点已归零，主循环、队列和持久化顺序未改。

验证：

- `chat_turn_state_test.go` 结构门禁要求生产文件只在
  `chat_turn_state.go` 调用 `SetChatState/UpdateChat`；
- 既有 `chat.changed`、队列调序/撤回、错误与恢复行为用例通过；
- `go test ./application/... ./contract/... ./e2e -count=1` 通过；
- `go test -race ./application/... -count=1` 通过。

## AP-009 首个切片实施记录

新增 `chat_turn_request.go`，集中：

- `chatRequest` 的构造与提交时 Skill 冻结；
- SessionUnit 不透明队列 payload 的还原；
- 提升批次的 `\n---\n` 文本合并与 Skill 去重；
- 排队输入引用提取。

`chat.go`、`service_input.go`、`session_queue_recover.go` 和装配端口只调用
`conversationTurnState` 的请求工厂方法。旧自由函数已删除，批次格式、顺序和
Skill 冻结语义不变。

## AP-010 首个切片实施记录

新增 `sessionLifecycle`，集中 `UnloadSession` 的应用编排：

- 按目标会话取过渡锁；
- 组装 `middle/switch.UnloadDeps`；
- 非视图会话持久化、完整内存释放；
- 视图会话续接到 fresh draft；
- ViewMu 内 bump revision，锁外发布 `snapshot.changed`；
- 刷新会话目录。

删除/合并：

- `session_lifecycle.go:unloadDeps` → `session_lifecycle_use_case.go:newServiceSessionLifecycle`
- `session_lifecycle.go:unloadGate` → `session_lifecycle_use_case.go:newServiceSessionLifecycle`

`Service.UnloadSession` 现在只做一行组件转发；unload 准入判据仍只有
`middle/switch.Unload` 一份。

## AP-011 首个切片实施记录

新增 `sessionArchive`，集中 `ArchiveSession` 的宿主适配：

- 按会话获取过渡锁；
- 在 ViewMu 下采集 closed/draining/busy/restoring 门控；
- 按会话定位 record，执行 flush、mark archived 与项目目录刷新；
- 探测引擎驻留、卸载 bundle、摘除驻留序；
- 释放该会话后台命令。

删除/合并：

- `archive_session.go:43 archiveDeps` → `session_archive_use_case.go:newServiceSessionArchive`
- `archive_session.go:88 archiveGate` → `session_archive_use_case.go:newServiceSessionArchive`

`Service.ArchiveSession` 只做一行转发；归档判据和动作顺序仍只有
`middle/residency.Archive` 一份。

## AP-012 首个切片实施记录

新增 `sessionDelete`，集中 `DeleteSession` 的应用顺序：

- 优先删除 session record，失败立即返回；
- 终止该会话名下后台命令；
- 走完整内存释放面；
- workspace 存在时解绑并重应用 workspace 投影；
- 被删的是视图会话时切到 fresh draft；
- ViewMu 内 bump revision，锁外发布 snapshot.changed 并刷新目录。

删除/合并：

- `workspace_usecase.go:23 DeleteSession` 方法体 → `session_delete_use_case.go:sessionDelete.Delete`

`Service.DeleteSession` 只做一行组件转发；内存释放失败不反向覆盖已成功的记录删除。

## AP-013 首个切片实施记录

`SessionQuery` 增加三个 durable 只读方法：

- `SnapshotOf`；
- `GetSessionTranscript`；
- `SessionInputIndex`。

`Service` 三个旧方法改为一行 `sessionQuery` 转发；原实现改名为查询组件注入的
私有读函数。冷快照保持“宿主读面先采样、ViewMu 只保护内存组合”的顺序；
transcript 和 input index 直接走存储读面，不进入共享写锁。

门禁：

- `SessionQuery` 方法上限仍为 8；
- 方法集反射测试要求三个读取面存在；
- 冷快照、transcript、input index 的既有行为用例全部通过。

## AP-014 首个切片实施记录

新增 `conversationTurnProcess`，集中两个原本重复的 turn 起点：

- 正常 `startChatFor`；
- `runChat` 尾部的队列批次提升。

统一规则：

- requestID 生成；
- Effort 与 ReAct budget 回退；
- context cancel 与 visible stream；
- Task budget、checkpoint、BeginTask 与 Skill 激活；
- user transcript 事件；
- ChatState 与任务进度镜像顺序。

`chat.go` 不再直接调用 `StartReActBudgetForLocked`、`BeginTaskFor`、
`ActivateTaskSkillsLocked` 或 `AppendTranscriptEventLocked`。Engine `ChatStream`、
可见消息插入、持久化和 goroutine 启动仍由主循环拥有。

## AP-015 首个切片实施记录

新增 `goalApplication`，集中以下 Goal 用例：

- 会话 ID 与协调器可用性检查；
- Begin / Update / Done / Status；
- GoalGovernanceView；
- 成功迁移后的运行投影刷新。

`Service` 对应方法只做一行组件转发；`goalCoordinator` 和
`backend/multiagent/goal.Controller` 的状态所有权不变。

## AP-016 首个切片实施记录

新增 `subagentApplication`，集中：

- recovery list；
- 全部中断子代理恢复；
- 单节点恢复；
- 子代理 fork 派发；
- 子代理树清理；
- 子代理详情读取。

Runtime 继续拥有执行/恢复能力，`middle/view/subagent` 继续拥有详情投影。
Service 六个公开入口只保留一行组件转发，原有 nil runtime、ctx 补全和错误文案
保持不变。

## AP-017 首个切片实施记录

新增 `executionApplication`，集中：

- WorkItem execution 查询；
- teammate inspection；
- teammate session live 与分页；
- teamwork board view；
- todo 三态状态更新。

Runtime 的类型断言与 Teamwork BoardReader 仍是具体能力提供方。Service 入口
改为一行组件转发；原有 port 缺失错误、空视图和 todo 校验文案保持不变。

## AP-018 首个切片实施记录

新增 `conversationTurnCompletion`，集中 ChatTurn 收尾的 durable 事务：

- 按 sessionID 定位会话；
- 原子持久化当前 Session；
- 持久化失败时把本轮消费项退回草稿；
- 持久化成功后确认消费项；
- 成功后释放该会话工作历史；
- 保留“原回合错误 + 持久化失败”与“纯持久化失败”两类错误包装。

删除/合并：

- `chat.go` 原持久化、queue fail/confirm、working-history release 方法体
  → `chat_turn_completion.go:conversationTurnCompletion.Persist`

`chat.go` 只保留一次组件调用；Engine 流、可见消息投影、任务终态、队列提升后的
消息/事件/启动编排、会话释放和工具记录批次落盘仍由 `runChat` 负责。以下规则未改变：

- `PersistCurrentSession` 是“该轮已发布”的判定点；
- queue 失败回草稿、成功确认的顺序；
- 原回合错误作为主错误身份，持久化错误作为附加正文；
- `session.persistence_failed` 结构化错误码。

结构门禁要求 `chat.go` 不再直接调用 `PersistCurrentSession`、
`QueueFailConsumedInput`、`QueueConfirmConsumedInput` 或
`ReleaseWorkingHistoryFor`。行为测试钉住成功、失败两条路径的调用集合和错误语义。

## AP-019 首个切片实施记录

`conversationTurnProcess.PromoteLocked` 成为 ChatTurn 收尾后的唯一队列决策点：

- 无排队输入：结束当前 `ChatState`、清空 cancel，并在没有其它聊天运行时标记 idle；
- 有排队输入：从 `SessionUnit` 取原始批次，合并 request，清空单元队列，刷新队列
  投影并复用 `BeginLocked`；
- 返回下一回合的 context、requestID、合并 request 和 user transcript event。

删除/合并：

- `chat.go` 原 `QueuedRequests` / `Combine` / `SetRequests(nil)` /
  `ProjectQueueLocked` / 空队列 `FinishLocked` + idle 分支
  → `chat_turn_process.go:conversationTurnProcess.PromoteLocked`

以下顺序保持不变：

- 当前 turn 持久化完成后才决定队列提升；
- 队列批次仍是单一合并请求，分隔符、Skill 合并和顺序不变；
- 下一回合 user / assistant 可见消息仍先于 goroutine 启动；
- `message.added`、runtime projection、durable queue consumed 标记和下一轮启动
  仍由 `runChat` 编排。

结构门禁禁止 `chat.go` 直接调用 `QueuedRequests`、`Combine` 或
`ProjectQueueLocked`。行为测试覆盖空队列收尾、全局 idle 判定和“其它会话仍运行
时不得 idle”。

## AP-020 首个切片实施记录

新增 `conversationTurnOutcome`，集中回合持久化后的终态结果应用：

- 按 `requestID` 判断请求是否已被取代；
- stale request 立即退出，不修改当前会话任何状态；
- 当前请求先清理旧错误；
- 有错误时保留原始错误正文，写入 ChatState 并追加 `error` 可见行；
- 最后应用当次采集的 runtime projection。

删除/合并：

- `chat.go` 原 stale 检查、`ClearErrorLocked`、`SetErrorLocked`、error 行追加和
  `applyRuntimeProjectionForLocked` → `chat_turn_outcome.go:conversationTurnOutcome.ApplyLocked`

以下内容未迁移：

- 队列提升及其下一回合可见消息；
- Task 镜像；
- revision bump；
- `EventError` / `EventSnapshotChanged` 发布；
- 会话释放和工具记录落盘。

结构门禁禁止 `chat.go` 直接调用 `ClearErrorLocked`、`SetErrorLocked` 或
`applyRuntimeProjectionForLocked`。行为测试覆盖成功、失败和 stale request 三条路径。

## AP-021 首个切片实施记录

新增 `conversationTurnPublisher`，集中 `Core.ViewMu` 释放后的终态事件发布：

1. 发布 `chat.changed`；
2. 成功时发布 `snapshot.changed`，失败时发布 `EventError`；
3. 存在队列提升时发布 user / assistant 两条 `message.added`；
4. 设置下一回合 task batch；
5. 发布 runtime projections。

删除/合并：

- `runChat` 原 `publishChatStateFor`、终态 error/snapshot 分支、提升批次
  `publishSessionEvent`、`SetCurrentTaskBatch` 和 `publishRuntimeProjections`
  → `chat_turn_publication.go:conversationTurnPublisher.Publish`

以下内容未迁移：

- 下一轮 `runChat` goroutine 的启动；
- session release；
- 工具记录批次落盘。

结构门禁在 `runChat` 函数体内禁止上述四类发布调用。行为测试钉住无队列提升和
“失败 + 提升批次”两条事件顺序。

## AP-022 首个切片实施记录

`conversationTurnOutcome.MirrorLocked` 集中 `Core.ViewMu` 解锁前的最终镜像：

- 所有会话把当前 `ChatState` 写回自身槽位；
- 仅活跃会话读取权威 `VisibleTaskState` 并镜像到共享 Snapshot；
- 仅活跃会话推进进程 revision；
- 后台会话返回 revision 0，不覆盖共享 Task。

删除/合并：

- `runChat` 原 `setSessionChatLockedFor`、活跃 Task 镜像和 active `bumpLocked`
  → `chat_turn_outcome.go:conversationTurnOutcome.MirrorLocked`

结构门禁在 `runChat` 函数体内禁止直接调用 `setSessionChatLockedFor`、
`VisibleTaskStateFor` 或 `bumpLocked`。行为测试覆盖活跃与后台 Session 两条路径。

## AP-023 首个切片实施记录

新增 `conversationTurnTail`，集中 `Core.ViewMu` 释放后的异步收尾：

1. 完全空闲时重绑当前会话工作区；
2. 对提升批次执行 durable queue consumed 标记；
3. 发布终态事件；
4. 启动下一轮 `runChat`；
5. 收敛会话释放候选；
6. 批次落盘工具记录。

删除/合并：

- `runChat` 原 `rebindViewWorkspaceWhenIdle`、`QueueMarkConsumedInput`、
  终态发布、下一轮 goroutine、`reconcileSessionRelease` 和 `FlushToolRecord`
  → `chat_turn_tail.go:conversationTurnTail.Complete`

语义保持不变：

- 所有慢 I/O 都在 `ViewMu` 之外；
- consumed 标记仍在下一轮启动前执行；
- consumed 标记失败只记诊断日志，不阻止后续发布与启动；
- 无论是否有提升批次，都会执行 session release 与 tool record flush。

结构门禁在 `runChat` 函数体内禁止直接调用上述尾段入口或递归启动
`runChat`。行为测试覆盖无提升与“提升 + consumed 标记失败”两条路径。

## AP-024 首个切片实施记录

新增 `sessionDraftSubmission`，集中显式提交命中未物化草稿时的激活编排：

- 先用草稿目标判据做廉价预判；
- 命中后获取视图过渡锁；
- 锁内重新确认草稿仍属于当前视图；
- 视图已离开草稿时返回 `ErrDraftNotInView`；
- 归属成立后裁剪 `firstInput` 并委托现有 `materializeDraftSession`。

删除/合并：

- `session_draft.go:materializeDraftForSubmit` 原目标预判、过渡锁、锁内复判和
  输入裁剪 → `session_draft_submission.go:sessionDraftSubmission.Materialize`

`Service.materializeDraftForSubmit` 只保留一行组件转发。草稿槽、引擎激活、
Session 记录写入、workspace 绑定和快照发布仍由现有物化实现拥有。

结构门禁要求旧方法保持单行 return 转发；行为测试覆盖非草稿目标、过期视图和
当前草稿三条路径。

## AP-025 首个切片实施记录

新增 `sessionDraftMaterialization`，集中首请求草稿物化：

- 读取草稿标记、早分配 SessionID 和 workspace 快照；
- 非草稿直接返回，缺失早分配 ID 立即报错；
- 绑定 workspace 项目根和写作用域；
- 打开目标 Session 引擎；
- 生成 `first_request` 标题；
- 提交 Session、Plan 和 workspace 镜像并 bump revision；
- 发布 runtime/snapshot；
- 刷新目录、落盘工具记录并登记驻留。

删除/合并：

- `session_draft.go:materializeDraftSession` 原物化编排
  → `session_draft_materialization.go:sessionDraftMaterialization.Materialize`

`Service.materializeDraftSession` 只保留一行组件转发。以下所有权不变：

- 草稿槽与共享 Snapshot 仍由 `serviceState`/kernel 持有；
- SessionUnit/title 仍由 Session 域协调器持有；
- 引擎 bundle 与 workspace binding 仍由 Engine/Runtime/Workspace 宿主持有；
- 目录缓存、工具记录和驻留 LRU 仍复用既有协调器。

结构门禁要求旧方法单行 return 转发。行为测试覆盖非草稿、缺失 ID、workspace
绑定失败和完整成功顺序。

## AP-026 首个切片实施记录

新增 `sessionDraftEntry`，集中 `BeginNewSession` 的应用编排：

- 视图过渡锁内读取 closed/draining/draft、当前 Session、运行态和 workspace；
- 空闲旧会话有 history 时先保存，失败立即返回；
- 空闲旧会话随后清引擎历史并解绑 context，运行中会话保持资源不动；
- 所有路径清理 Skill 层；
- 复用或创建草稿槽，锁外读取持久权限档位；
- 无 workspace 草稿清理继承的全局项目根与写作用域；
- 锁内切换草稿壳并 bump revision；
- 锁外发布 runtime/snapshot、切换 task、清子代理树并刷新工作表格。

删除/合并：

- `session_draft.go:BeginNewSession` 原方法体
  → `session_draft_entry.go:sessionDraftEntry.Begin`

`Service.BeginNewSession` 只保留一行组件转发。草稿槽、SessionUnit、权限档位、
引擎历史/context 和 workspace binding 仍由各自宿主拥有。

结构门禁要求旧方法单行 return 转发。行为测试覆盖关闭、排空、已处于草稿、
空闲保存失败和运行中旧会话保持五条路径。

## AP-027 首个切片实施记录

新增 `sessionResume`，集中会话选择的应用顶层分支：

- 空 SessionID 在获取锁前失败；
- 先获取目标会话的过渡锁，再采样 hot/running/restoring/previous；
- restoring 分支不重复装载：优先把视图归位到在途空壳，装载已收口则改走热挂载；
- hot 分支先推进视图意图，再释放过渡锁并热挂载；
- cold 分支先激活 restoring 空壳并释放过渡锁；
- 其它聊天运行中时把冷加载转后台，空闲时同步等待；
- 同步冷加载失败调用既有失败恢复编排。

删除/合并：

- `session_history.go:resumeSession` 原顶层分支
  → `session_resume.go:sessionResume.Resume`

`Service.ResumeSession` 与内部 `resumeSession` 都只保留组件转发。
`resumeSessionCold`、`beginAsyncRestore`、hot attach、view intent 与 epoch
发布实现均保持原位置和唯一实现。

结构门禁要求内部 `resumeSession` 单行 return 转发。行为测试覆盖空 ID、
restoring 在途/已收口、hot、后台 cold 和同步 cold 失败五类分支。

## AP-028 首个切片实施记录

新增 `editForkOperationRegistry`，独立拥有编辑/分叉操作的幂等状态：

- `operation_id` → 在飞或短期完成结果；
- 自己的 mutex，不再使用 `Core.ViewMu` 保护 bookkeeping；
- 相同 parent/request 复用原 operation；
- 相同 ID 但不同 parent/request 明确拒绝；
- 成功项保留 5 分钟 TTL，失败项立即删除；
- `Finish` 写结果后关闭 `done`，等待方获得同一结果。

删除/合并：

- `serviceState.editForkOps`
  → `service_components.editForkOperationRegistry`
- `session_message_edit.go` 原 `beginEditForkOperation` / `finishEditForkOperation`
  方法体 → `edit_fork_operation_registry.go`

旧两个 Service 私有入口只保留一行组件转发。Edit/fork 的 CAS、目标 resolution、
child fork、切换、提交和回滚逻辑均未迁移。

结构门禁同时要求 `serviceState` 不再包含 `editForkOps`。行为测试覆盖请求复用、
冲突 ID、TTL、失败删除和等待方结果可见性。

## AP-029 首个切片实施记录

新增 `editForkValidator`，集中 Edit/Fork 的父会话准入：

- 父会话必须是当前视图会话；
- 父会话不得运行中；
- 父会话不得存在排队输入；
- 非零 expected revision 必须等于当前父事件 revision；
- 必须存在可编辑保留窗口；
- 目标事件不得落在已压缩前缀。

删除/合并：

- `session_message_edit.go:validateEditForkParent` 原判据
  → `edit_fork_validator.go:editForkValidator.Validate`

旧 Service 私有入口只保留一行组件转发。目标事件解析、段落边界、child fork、
切换、提交和失败回滚均保持原位置。

结构门禁要求旧方法单行 return 转发。行为测试逐项覆盖非当前会话、运行中、
排队、revision 漂移、无窗口、压缩前缀和正常路径。

## AP-030 首个切片实施记录

新增 `editForkTargetResolver`，集中 Edit/Fork 的目标事件解析：

- 通过 `SessionForkPort` 加载父会话完整事件范围；
- 缺失 fork 存储与读取失败保持原错误语义；
- 以最大 EventSeq 计算父 revision；
- 按 EventSeq 定位目标事件；
- 拒绝非 user、wire-only、internal 和空可见正文事件；
- 复用 `store.EventParagraphs` 推导完整段落；
- 要求目标位于段落起点，fork cut 取目标之前最后一段的 EventTo。

删除/合并：

- `session_message_edit.go:resolveEditForkTarget` 原事件加载与段落解析
  → `edit_fork_target_resolver.go:editForkTargetResolver.Resolve`

旧 Service 私有入口只保留一行组件转发。目标事件段落算法的唯一实现仍是
`store.EventParagraphs`，没有在 Application 复制第二套段落判据。

结构门禁要求旧方法单行 return 转发。行为测试覆盖缺失存储、加载失败、正常
revision/cut 以及缺失、assistant、internal 三类非法目标。

## AP-031 首个切片实施记录

新增 `editForkProcess`，固定 Edit/Fork 的 child 原子顺序：

1. 解析目标用户事件与 fork cut；
2. 校验父会话准入；
3. 获取父会话过渡锁；
4. 锁内调用 `forkSessionLocked`；
5. 释放过渡锁后恢复 child；
6. 向 child 提交编辑后的文本；
7. resume/submit 失败时统一调用既有 child 删除与父会话恢复回滚。

删除/合并：

- `session_message_edit.go:runEditUserMessageFork` 原原子编排
  → `edit_fork_process.go:editForkProcess.Run`

旧 Service 私有入口只保留一行组件转发。`forkSessionLocked`、resume、submit
和 `rollbackEditFork` 本体仍由既有 Session 能力唯一拥有。

结构门禁要求旧方法单行 return 转发。行为测试覆盖解析/校验短路、锁范围和
resume/submit 两类回滚。

## AP-032 首个切片实施记录

新增 `editForkRollback`，集中 child 清理和 parent 恢复：

- 先删除 child；
- 再恢复 parent；
- 两侧成功时原样返回编辑/提交失败 cause；
- child 删除失败、parent 恢复失败或双失败时，在原 cause 后追加对应诊断；
- 始终用 `%w` 保留原 cause 的错误链。

删除/合并：

- `session_message_edit.go:rollbackEditFork` 原删除/恢复与错误合成
  → `edit_fork_rollback.go:editForkRollback.Rollback`

`editForkProcess` 的回滚端口已指向新组件。Session 删除与 resume 本体仍由既有
Session 能力提供。旧 Service 私有入口只保留一行组件转发。

结构门禁要求旧方法单行 return 转发。行为测试覆盖双成功、child 失败、parent
失败和双失败四类组合。

## AP-033 首个切片实施记录

新增 `editForkAnnotation`，集中可见消息的编辑判据：

- 复制输入消息，禁止原地修改；
- 为没有 EventSeq 的用户消息按 MessageID 回填权威 transcript 序号；
- assistant、空可见用户行保持不可编辑；
- 按 running → 缺 EventSeq → 无保留窗口 → 压缩前缀的顺序给出唯一禁编原因；
- 满足条件时设置 `Editable=true` 并清除禁编原因。

删除/合并：

- `session_message_edit.go:annotateConversationForEditLocked` 原投影逻辑
  → `edit_fork_annotation.go:editForkAnnotation.Annotate`

可见正文、窗口、transcript 和保留窗口仍由现有 View/Task 组件拥有。调用方继续
持有 `Core.ViewMu`；组件只持有读入窄回调。

结构门禁要求旧方法单行 return 转发。行为测试覆盖 EventSeq 回填、用户/assistant
区分和四类禁编原因。

## AP-034 首个切片实施记录

新增 `sessionForkLineage`，集中 fork lineage 只读解析：

- 仅当 `SessionForkPort` 可用时读取 workspace/session context state；
- 空 payload、读取失败或未装配端口返回 nil；
- 解码 `store.SessionContextRecord` 并要求存在 `forked_from`；
- 解码 `model.SessionForkRef` 并要求 parent session ID 非空；
- 成功时返回 fork ref，否则安全降级为无 lineage。

删除/合并：

- `session_message_edit.go:loadSessionForkRef` 原 payload 读取与 JSON 解码
  → `session_fork_lineage.go:sessionForkLineage.Load`

旧 Service 私有入口只保留一行组件转发。Session context state 的写入与 fork
创建仍由 Store/Session 域拥有。

结构门禁要求旧方法单行 return 转发。行为测试覆盖读取失败、空 payload、损坏
record、缺失 parent、非法 ref 和有效引用。

## AP-035 首个切片实施记录

新增 `sessionRestoringShell`，统一 restoring 空壳的共享 Snapshot 投影：

- Session 切到目标 ID、标题和 `restoring` 状态；
- 切换 Active session；
- 清空上一会话的 Task、Plan、Todo、SubAgent、WorkTable/Batches、
  TeamworkBoard 与 Interaction；
- 镜像目标 SessionUnit 的 ChatState；
- 同步可见 View 并 bump revision。

删除/合并：

- `beginAsyncRestore` 中的重复 Snapshot 字段清理
- `pointViewAtRestoringShell` 中的重复 Snapshot 字段清理
  → `session_restoring_shell.go:sessionRestoringShell.PointLocked`

两条路径的特有逻辑仍保留：`beginAsyncRestore` 额外做 closed 检查、view intent、
restoring 标记和会话单元空壳重置；重复 Resume 路径保留幂等归位与“装载已收口则
改走 hot attach”的判定。冷加载主体、wire 装配和 epoch 发布继续在现有实现中。

行为测试钉住 Session 状态、Active 切换、ChatState 镜像、revision，以及所有上一
会话事实字段的清空。

## AP-036 首个切片实施记录

新增 `sessionRestoreRecovery`，集中冷加载失败的视图恢复：

- 在同一 `Core.ViewMu` 临界区内清除目标 restoring 标记并读取 active/epoch 状态；
- 视图已切走或 epoch 已被更新切换取代时静默结束；
- 仍有前一会话时尝试 hot attach；
- hot attach 成功则发布 Bridge 视图会话变更通知；
- 无前一会话或 hot attach 失败时重置 fresh draft，再发布通知。

删除/合并：

- `session_history.go:handleColdRestoreFailure` 原恢复分支
  → `session_restore_recovery.go:sessionRestoreRecovery.Handle`

旧 Service 私有入口只保留一行组件转发。冷加载主体、hot attach、draft reset 和
Bridge 通知实现仍由既有能力提供。

结构门禁要求旧方法单行调用转发。行为测试覆盖非 active、旧 epoch、成功 hot
回退、无 fallback 和 hot 失败五类分支。

## AP-037 首个切片实施记录

新增 `sessionRestoreDraftReset`，集中冷恢复失败的最后草稿兜底：

- 获取 `Core.ViewMu`；
- 复用草稿槽或创建新的早分配 draft ID；
- 更新槽位时间；
- 锁内切换 fresh draft shell、重算档位并 bump revision；
- 释放锁后发布 runtime projections 与 snapshot.changed。

删除/合并：

- `session_history.go:resetViewToDraftAfterRestoreFailure` 原锁定与发布编排
  → `session_restore_draft_reset.go:sessionRestoreDraftReset.Reset`

旧 Service 私有入口只保留一行组件调用。`enterDraftShellLocked`、草稿槽所有权、
权限档位重算和事件实现均复用既有唯一实现。

结构门禁要求旧方法单行调用转发。行为测试钉住锁内草稿准备/草稿壳、锁外 runtime
与 snapshot 发布顺序。

## AP-038 首个切片实施记录

新增 `sessionColdRead`，集中冷加载的持久化读取阶段：

- pending-tail 必须在三读前恢复；
- 定位目标会话；
- SessionRecord 与 transcript 并行读取；
- 无 record 时读取 legacy history tail；
- record 读取错误优先返回；
- 无 record 时 legacy history/transcript 错误阻断恢复；
- record 存在时旧 history/transcript 缓存错误不阻断恢复。

删除/合并：

- `session_history.go:resumeSessionCold` 原 pending-tail、并发三读、legacy
  history 回退与错误优先级
  → `session_cold_load_reader.go:sessionColdRead.Read`

`resumeSessionCold` 仍拥有 engine history 装配、Engine resume、队列回填、
Snapshot/View 安装、workspace/context 挂接和 epoch 发布。

结构门禁要求 `resumeSessionCold` 不再直接调用三个底层读取方法。行为测试覆盖
record 正常路径、legacy fallback、错误优先级和 record 权威降级。

实施中曾误用 `session_cold_read.go` 文件名覆盖已有冷快照/transcript 实现；
已恢复该文件并补回 AP-013 的 `readSessionTranscript` 注入面，新组件改名为
`session_cold_load_reader.go`。恢复后的 transcript/冷快照测试随本批全量验证。

## AP-039 首个切片实施记录

新增 `sessionColdHistory`，把冷读结果装配为 Engine 恢复历史：

- 无 record 时直接使用 legacy history；
- record transcript 为空或不含最新用户输入时，从 Conversation 重建并标记
  `Renumbered`；
- 用 `TranscriptTailHistory` 生成基础尾窗；
- 优先采用 v8 wire history；
- 缺失最新用户输入时回退 record resume history；
- 最终仍为空时回退 `session_runtime.RecordResumeHistory`；
- wire 装配错误保留原错误链。

删除/合并：

- `session_history.go:resumeSessionCold` 原 transcript 重建、wire 装配和
  history 回退
  → `session_cold_history.go:sessionColdHistory.Assemble`

`resumeSessionCold` 现在只消费装配结果，仍拥有 Engine resume/replace、队列回填、
Snapshot/View 安装、workspace/context 挂接和 epoch 发布。

结构门禁要求 `resumeSessionCold` 不再直接调用 wire 装配或 record transcript/
history 方法。行为测试覆盖 legacy、renumber+wire、record fallback、final resume
fallback 和 wire error。

## AP-040 首个切片实施记录

新增 `sessionColdRestore`，集中 history 装配之后的宿主恢复：

- 在 Engine 恢复前登记 session workspace 显式键；
- 优先调用 Engine `ResumeSession`；
- Engine 无 resume 能力时回退 `ReplaceHistory`；
- 恢复 durable queue，并把已发送未确认输入回填内存队列；
- 最后写目标会话 system prompt；
- Engine/queue 错误保留原提示并在错误处停止。

删除/合并：

- `session_history.go:resumeSessionCold` 原 workspace、Engine resume/replace、
  queue recover/re-enqueue 和 prompt 写入
  → `session_cold_restore.go:sessionColdRestore.Restore`

`resumeSessionCold` 仍拥有随后 Snapshot/View 安装、task/workspace 投影、
context 挂接、epoch 判定和事件发布。底层 Engine/Queue 实现保持不变。

结构门禁要求 `resumeSessionCold` 不再直接调用上述宿主步骤。行为测试覆盖正常
resume、replace fallback、resume/replace/queue 错误及 prompt 短路。

## AP-041 首个切片实施记录

新增 `sessionColdWorkspace`，集中冷加载锁外的 workspace 处理：

- workspace 元数据存在时，安全条件下重绑全局根与写作用域；
- 无论全局根是否可切，都写 per-session workspace binding；
- 有 workspace ID 但元数据缺失时保持现有绑定不动，避免永久解绑造成键漂移；
- 只有 workspace ID 确实为空时，才按全局忙闲解绑全局根；
- 同时解绑 session binding 和 runtime per-session project root。

删除/合并：

- `session_history.go:resumeSessionCold` 原 workspace 绑定/保留/解绑分支
  → `session_cold_workspace.go:sessionColdWorkspace.Bind`

`resumeSessionCold` 仍拥有 workspace projection 采样、权限档位读取和后续
Snapshot 安装。底层 Workspace/Runtime 端口实现不变。

结构门禁要求 `resumeSessionCold` 不再直接调用 workspace 绑定/解绑方法。行为
测试覆盖正常绑定、运行中跳过全局根、真正无 workspace 解绑和元数据缺失保留。

## AP-042 首个切片实施记录

新增 `sessionColdActivation`，集中 `Core.ViewMu` 内的激活准备：

- 用 view intent epoch 和当前 Snapshot Session 判定 `mayActivate`；
- 无论是否激活都清除 restoring；
- 标题优先使用 record title，否则由 legacy history 推导；
- 仅最新且仍 active 时切换 Snapshot Session 与 Active session；
- 获取/创建 SessionUnit 后清除 cancel；
- 应用持久权限档位；
- 写 legacy title，并在有 record 时覆盖为权威 title。

删除/合并：

- `session_history.go:resumeSessionCold` 原 epoch/active、restoring、标题、
  Active、cancel、tier 和 title 准备
  → `session_cold_activation.go:sessionColdActivation.PrepareLocked`

Task restore、View 构造/安装、Snapshot 字段合并和发布仍留在冷加载主流程。
SessionUnit 创建时机仍保持原顺序。

实现中修正了 sessions coordinator 在装配早期为 nil 的方法值捕获：title 写入改为
调用时动态解析。

结构门禁要求 `resumeSessionCold` 不再直接调用 clear-restoring/tier/title 方法。
行为测试覆盖 active 与 superseded 两类 epoch/视图分支。

## AP-043 首个切片实施记录

新增 `sessionColdTask`，集中冷加载 Task 域恢复：

- 用 transcript 尾 seq 和 projection checkpoint 上限计算 transcriptSeq；
- 优先使用 record 旧址 Task.ContextCompactions；
- 旧址为空时读取新 compaction 记录通道；
- 只对未 renumber 的 transcript 推导 retainedFrom；
- 按目标 Session 恢复 PlanStack、ActivePlan、checkpoint、tool results、
  fallback objective 和 context compaction 事实；
- 无 record 时执行原有任务状态 reset。

删除/合并：

- `session_history.go:resumeSessionCold` 原 transcriptSeq、compaction、retainedFrom、
  RestoreSessionTaskLockedFor 与 ResetForNewSessionLocked 分支
  → `session_cold_task.go:sessionColdTask.RestoreLocked`

View 构造、可见消息安装、ReadFiles、ForkedFrom、Snapshot Task 镜像和发布仍在
冷加载主流程。

结构门禁要求 `resumeSessionCold` 不再直接调用 Task restore/reset、compaction
读取或 retainedFrom 推导。行为测试覆盖旧址、新址、renumber 和无 record。

## AP-044 首个切片实施记录

新增 `sessionColdView`，集中冷加载可见 View：

- 构造 total/offset/has-more/window/ForkedFrom；
- record 路径推进 message seq，插入恢复 system 行，安装 record tail 和 ReadFiles；
- 目标可见会话已有更新内容时不覆盖；
- active 装载镜像 ForkedFrom 和 Task context compactions；
- legacy 路径追加历史并修正总数、offset、has-more，再镜像 active view。

删除/合并：

- `session_history.go:resumeSessionCold` 原 View 构造、record/legacy 分支、
  message seq、ReadFiles/ForkedFrom 和 active Task 镜像
  → `session_cold_view.go:sessionColdView.InstallLocked`

pending-tail notice、ChatState 镜像、Plan、workspace 投影、context 挂接、runtime
projection 与事件发布仍在冷加载主流程。

结构门禁要求 `resumeSessionCold` 不再直接调用 view set/mutate、legacy append 或
message seq。行为测试覆盖 record 安装、已有更新视图保护和 legacy 分页修正。

## AP-045 首个切片实施记录

新增 `sessionColdPublish`，集中冷加载解锁后的运行面与发布：

- active 装载在非路由 Engine 上回退写全局 system prompt；
- 任何装载都挂接目标 Session context 四栈；
- active 装载恢复 task registry、清子代理树、恢复 anchors；
- active 装载刷新并发布 runtime/snapshot；
- 所有装载刷新 catalog，并触碰 resident/content LRU；
- context 挂接失败在运行面切换和发布前返回。

删除/合并：

- `session_history.go:resumeSessionCold` 原 global prompt、context attach、
  active task/subagent 切换、runtime 发布、catalog 和 LRU
  → `session_cold_publish.go:sessionColdPublish.Publish`

`resumeSessionCold` 仍连接锁内 plan/workspace 收口、revision 和解锁边界。
底层 context、Runtime、catalog 与 LRU 实现不变。

结构门禁要求 `resumeSessionCold` 不再直接调用上述运行面/发布方法。行为测试
覆盖 active、非 active 和 context 失败三条路径及完整调用顺序。

## AP-046 首个切片实施记录

新增 `sessionColdCommit`，集中 `resumeSessionCold` 锁内最后收口：

- 追加 pending-tail system notice；
- 把目标 SessionUnit ChatState 镜像进快照；
- active 装载镜像当前 Plan，并清空 Interaction；
- 在锁内取得 active task system prompt 文本；
- active 且 workspace 可用时应用当前 workspace 与 workspace projection；
- active 装载 bump revision；
- 返回 prompt/revision 给锁外发布组件。

删除/合并：

- `session_history.go:resumeSessionCold` 原 tail/chat/plan/prompt/workspace/
  revision 收口
  → `session_cold_commit.go:sessionColdCommit.CommitLocked`

至此 `resumeSessionCold` 已按数据读取、history 装配、Engine/Queue 恢复、
workspace、锁内激活、Task、View、锁内 commit、锁外 publish 分解为组件串联。
剩余代码只负责各组件接线和错误传播。

结构门禁要求 `resumeSessionCold` 不再直接调用 tail/chat/plan/prompt/workspace/
revision 方法。行为测试覆盖 active 与非 active 两条路径及调用顺序。

## AP-047 首个切片实施记录

新增 `conversationTurnInput`，集中 `runChat` 进入 Engine 前的输入准备：

- 激活当前 task 的 system prompt；
- 调用 provider context 装配；
- 对装配成功结果执行原有非空输入归一化；
- context 失败时直接返回，不再归一化或进入 Engine 流。

删除/合并：

- `chat.go:runChat` 原 `ApplyActiveTaskSystemPromptFor`、
  `PrepareExecutionContextFor` 和 `nonEmptyProviderInput`
  → `chat_turn_input.go:conversationTurnInput.Prepare`

Engine `ChatStream`、输出观测、context recovery、budget/task finalization、
持久化和发布仍留在 `runChat`。

结构门禁在 `runChat` 内禁止直接调用 prompt/context 装配方法。行为测试覆盖成功
顺序和 context 失败短路。

## AP-048 首个切片实施记录

新增 `conversationTurnResponse`，集中 Engine `ChatStream` 返回后的即时处理：

- 仅在回合无错误时 attach 最新 reasoning；
- 始终把 reply/error 送入模型输出观测，保持观测错误忽略语义；
- reply 非空时补最终 assistant transcript；
- 用目标 Session 的 engine history 回填所有 assistant reasoning。

删除/合并：

- `chat.go:runChat` 原 `attachLatestReasoning`、`ObserveModelOutput`、
  `EnsureFinalAssistantTranscript`、`BackfillAssistantReasoning`
  → `chat_turn_response.go:conversationTurnResponse.Finalize`

context control recovery、bounded retry、ReAct budget finalization、Task finalize、
batcher flush、route stamp、持久化和发布仍在 `runChat`。

结构门禁在 `runChat` 内禁止直接调用上述四类响应处理。行为测试覆盖成功回复、
错误回合和空回复三条路径。

## AP-049 首个切片实施记录

新增 `conversationTurnRecovery`，集中 Engine 首轮返回后的恢复/终态链：

- 读取并覆盖 context control failure；
- 调用 provider failure recovery；
- 保持 recovery 错误组合文案；
- recovery 成功且错误可重试时执行有界 context recovery turn；
- 成功清空错误，失败追加 bounded retry 文案；
- 只有无错误时执行 ReAct budget finalization；
- budget 成功后执行 Task finalize。

删除/合并：

- `chat.go:runChat` 原 `TakeContextControlFailure`、
  `recoverProviderFailureFor`、`retryableAfterRecovery`、
  `retryContextRecovery`、`finalizeReActBudgetWithSink`、`FinalizeTask`
  → `chat_turn_recovery.go:conversationTurnRecovery.Finalize`

stream batcher flush、served route、subagent mailbox drain、runtime projection、
context checkpoint cleanup、持久化、视图收口和事件发布仍在 `runChat`。

结构门禁在 `runChat` 内禁止直接调用上述恢复/终态方法。行为测试覆盖成功 retry、
recovery/retry 错误和 budget 失败短路。

## AP-050 首个切片实施记录

新增 `conversationTurnCleanup`，集中 Engine 返回后的持久化前清理：

- batcher flush，失败时仅在原错误为空时写成 `flush streamed response`；
- served route stamp；
- 丢弃 subagent mailbox；
- 采集目标 Session runtime projection；
- 清理 task context checkpoints；
- 原错误为空时清理 provider context recovery；
- 有错误时在 ViewMu 内记录未处理 Task 错误；
- 停止 draft snapshot loop。

删除/合并：

- `chat.go:runChat` 原 batcher flush、route、mailbox、projection、checkpoint/
  recovery cleanup、error record 和 `stopDraftSnapshot`
  → `chat_turn_cleanup.go:conversationTurnCleanup.Cleanup`

持久化、queue 结算、View/ChatState 收口、队列提升、publication 和 tail 仍在
后续批次。

结构门禁在 `runChat` 函数体内禁止直接调用上述清理方法或实际执行
`batcher.Flush`；方法值传入组件属于允许路径。行为测试覆盖成功、原错误优先、
flush/recovery 错误和清理顺序。

## AP-051 首个切片实施记录

新增 `conversationTurnCommit`，串联既有子组件：

- 调用 `conversationTurnCompletion.Persist` 完成 durable queue 结算；
- 取 `Core.ViewMu`，判定 active 和目标 SessionUnit；
- 调用 `conversationTurnOutcome.ApplyLocked` 处理 stale/error/runtime projection；
- stale 请求记录诊断后短路，不再提升队列或修改状态；
- 调用 `conversationTurnProcess.PromoteLocked`；
- 有提升批次时追加 user/assistant 可见消息；
- 调用 `conversationTurnOutcome.MirrorLocked` 完成 ChatState/Task/revision 镜像；
- 返回 location、settled error、visible error、promotion、消息和 revision。

删除/合并：

- `chat.go:runChat` 原 Persist、ViewMu 收口、Outcome、队列提升、消息追加和 Mirror
  → `chat_turn_commit.go:conversationTurnCommit.Commit`

publication 与 tail 组件调用仍在 `runChat`，现在只消费 commit result。

结构门禁要求 `runChat` 不再直接调用 Persist/Apply/Promote/Mirror 或消息追加方法。
行为测试覆盖完整提交顺序和 stale 短路/锁释放。

## AP-052 首个切片实施记录

新增 `conversationTurnFinalize`，完成 ChatTurn 输出链最后连接：

- 从 `chatTurnCommitResult` 构造 `chatTurnPublication`；
- 将 location、revision、visible error、turn error、promotion 和下一批消息完整映射；
- 调用 `conversationTurnTail.Complete`，由其内部执行 publication、队列 consumed
  标记、下一轮启动、会话释放和工具记录落盘；
- stale commit 直接返回，不发布、不启动下一轮。

配套将 `SessionID` / `RequestID` 加入 commit result，确保 finalize 不依赖
`runChat` 局部变量。

删除/合并：

- `chat.go:runChat` 原 publication 构造、stale check 和 tail 直接调用
  → `chat_turn_finalize.go:conversationTurnFinalize.Finalize`

至此 `runChat` 只保留 Engine 输入准备、ChatStream、响应/恢复/清理调用、
commit 和 finalize 组件串联。

结构门禁要求 `runChat` 不构造 `chatTurnPublication`、不直接访问
`conversationTurnTail`。行为测试覆盖完整映射和 stale 忽略。

## AP-053 首个切片实施记录

新增 `conversationTurnExecution`，集中每回合起点的执行面同步：

- 绑定目标 Session 的 project root；
- 下发目标 Session 的 subagent policy；
- 下发目标 Session 的 reasoning effort；
- 下发目标 Session 的 full access 门。

删除/合并：

- `chat.go:runChat` 原 `bindSessionProjectRoot`、`syncSubagentPolicyFor`、
  `syncSessionReasoningEffortFor`、`syncFullAccessFor`
  → `chat_turn_execution.go:conversationTurnExecution.Prepare`

执行策略实现仍由现有 Session/Runtime/Engine 方法拥有。`runChat` 现在只保留
draft snapshot loop、执行策略组件、Engine 输入/流、response/recovery/cleanup/
commit/finalize 组件串联。

结构门禁要求 `runChat` 不再直接调用四个执行策略方法。行为测试钉住同步顺序。

## AP-054 首个切片实施记录

新增 `policyApplication`，收拢低风险 Policy 用例：

- 技能激活入口与 nil guard；
- reasoning effort 独立拉条；
- loop limit 独立拉条；
- Session release window 读写；
- FullAccess 到 full/manual permission tier 的兼容映射。

删除/合并：

- `skill_activate.go:ActivateSkill` 原 activator 构造与 nil guard
- `service_knobs.go:SetReasoningEffort / SetLoopLimit`
- `service_release_window.go:SetSessionReleaseWindow / SessionReleaseWindow`
- `service_interaction.go:SetFullAccess`
  → `service_policy_application.go:policyApplication`

底层 `backend/engine/skill.Activator`、`backend/limit.Knobs`、
`middle/switch` release window 和权限档位状态机仍保持唯一实现。

Effort 切换、插件切换和 `SetPermissionTier` 本体仍留在 `service_interaction.go`。
对应 `Service` 方法只保留组件转发；`ActivateSkill` 保留 nil receiver guard。

## AP-055 首个切片实施记录

扩展 `policyApplication`，迁入 `SetPermissionTier` 本体：

- 标准化档位；
- 取当前视图 SessionID；
- 先持久化档位，失败即返回且不改内存；
- 更新 SessionUnit 档位；
- 同步本会话审批自动门和 Runtime 权限门；
- 计算真正生效档位；
- full 档仅自动放行本会话待批；
- 锁内写 Snapshot PermissionTier/FullAccess、bump revision，锁外发布 runtime。

删除/合并：

- `service_interaction.go:SetPermissionTier` 原完整状态编排
  → `service_policy_application.go:policyApplication.SetPermissionTier`

`Service.SetPermissionTier` 保留 nil receiver guard 后只做组件转发。底层权限档位、
审批 broker、Runtime 和 SessionUnit 状态不变。Effort/Plugin 切换仍未迁移。

行为测试覆盖完整 full 路径、持久化/Runtime 失败和 non-full 不自动放行。

## AP-056 首个切片实施记录

扩展 `policyApplication.SwitchEffort`：

- 通过 Knobs 守卫确认视图会话空闲；
- 空值或 `cycle` 时循环到下一 Effort；
- 应用 Effort 到 prompt/预算管理；
- 下发进程级 subagent policy；
- 下发账号侧 reasoning effort；
- 渲染并写入目标视图 Session 的 system prompt；
- 锁内写 SessionUnit Effort 读数、Snapshot Effort/Reasoning/Loop；
- 锁外发布完整 runtime.changed。

删除/合并：

- `service_interaction.go:SwitchEffort` 原完整编排
  → `service_policy_application.go:policyApplication.SwitchEffort`

`Service.SwitchEffort` 只保留组件转发。Plugin 切换仍未迁移。

实施中修正 effortManager 的装配期方法值捕获：改为调用时动态解析，避免单测
构造路径 nil。

行为测试覆盖完整路径和 idle/cycle/apply 三类失败短路。

## AP-057 首个切片实施记录

扩展 `policyApplication.SwitchPlugin`：

- 先检查是否存在任何运行中会话，存在则拒绝；
- off/none/空名称走 deactivate，否则走 activate；
- 清空引擎历史；
- deactivate 重置空 prompt 与 system prompt；
- activate 读取当前插件 prompt 并重设 prompt 前缀；
- 重新应用用户当前 Effort 与 loop limit；
- active 切换写入渲染后的 system prompt；
- 重置会话可见内容并给出稳定提示；
- 采集 runtime projection，锁内应用并 bump revision；
- 锁外发布 runtime.changed 与完整 runtime projections。

删除/合并：

- `service_interaction.go:SwitchPlugin` 原完整编排
  → `service_policy_application.go:policyApplication.SwitchPlugin`

`Service.SwitchPlugin` 只保留组件转发。底层 Plugin、Engine、Prompt、Session
reset 实现不变。

行为测试覆盖 activate、off、运行中拒绝和 activate 失败短路。

验证结果：

- `go test ./application/... ./contract/... ./e2e -count=1`：通过；
- `go test -race ./application/... -count=1`：通过；
- `go build ./...`、`go vet ./...`：通过；
- `gofmt -l application contract`：无输出；
- `python scripts/check_readme_refs.py --strict`：186 个 README、0 个失效引用；
- 本轮范围 `git diff --check -- application docs/2026-10-25-application-modularization`：
  通过；全仓 `git diff --check` 仍受工作树中其它并行改动的既有行尾空白影响。

## AP-058 首个切片实施记录

新增 `adminApplication`，先以红测钉住以下缺口：

- `serviceComponents` 尚未装配 `adminApplication`；
- 账号用例仍分散在五个 `Service` 方法中；
- 缺少组件级行为测试与“旧入口只一行转发”的 AST 门禁。

红测结果：`go test ./application/core -run 'AdminApplication|AccountFacade' -count=1`
因 `adminApplication`、`newAdminApplication`、`adminApplicationDeps` 未定义而失败。

实施后，`service_admin_application.go` 集中：

- `AddAccount` 的草稿原样转发；
- `AccountDetails` / `DeleteAccount` 的名称裁剪；
- `UpdateAccount` 的名称裁剪与原位更新；
- `AccountBalance` 的 Runtime 可选端口解析与 context 透传；
- nil 组件、缺失宿主端口和宿主错误三类语义。

删除/合并：

- `service_account.go:AddAccount` 原端口解析与缺失错误
  → `service_admin_application.go:adminApplication.AddAccount`
- `service_account.go:AccountDetails` 原端口解析、名称裁剪与缺失错误
  → `service_admin_application.go:adminApplication.AccountDetails`
- `service_account.go:UpdateAccount` 原端口解析、名称裁剪与缺失错误
  → `service_admin_application.go:adminApplication.UpdateAccount`
- `service_account.go:DeleteAccount` 原端口解析、名称裁剪与缺失错误
  → `service_admin_application.go:adminApplication.DeleteAccount`
- `service_account.go:AccountBalance` 原 Runtime 能力断言与缺失错误
  → `service_admin_application.go:adminApplication.AccountBalance`

`service_account.go` 的五个旧入口均只保留一行组件转发。底层账号档写入、
热注册和余额探测仍由 `AccountAdminPort` 与 `AccountBalancePort` 的唯一实现拥有。

绿测：

- `go test ./application/core -run 'AdminApplication|AccountFacade|Account' -count=1`：
  通过；
- 组件行为覆盖添加、详情、更新、删除、余额、名称裁剪、宿主错误透传和端口缺失；
- AST 门禁覆盖五个旧 `Service` 方法，均只有一条 return。

结构指标：

- `Service` 公开方法：171 → 171（五个兼容入口保留，已全部降为一行转发）；
- `application/core` 直接内部依赖：39 → 39（只使用既有 `contract` 端口）；
- `application/core` 生产 Go 文件：124 → 125；
- `application/core` 生产代码：18,461 → 18,538 行。

验证：

- `go test ./application/... -count=1`：通过；
- `go test ./application/... -count=1` 的 race 版本：通过；
- `go test ./contract/... -count=1`：通过；
- `go test ./e2e -run 'Facade|Document' -count=1`：通过；
- `go build ./...`、`go vet ./...`：通过；
- `python scripts/check_readme_refs.py --strict`：186 个 README、0 个失效引用；
- `gofmt -l application contract`：无输出；
- `git diff --check`：通过，仅提示并行 Backend 文档 CRLF 将被 Git 转为 LF。

## AP-059 首个切片实施记录

扩展 `adminApplication.SelectAccount`，先以红测钉住缺失的依赖：

- 账号选择组件尚未集中 Runtime 准入、projection、锁内状态、revision、发布与通知；
- `Service.SelectAccount` 仍包含完整编排；
- 缺少“账号不可用时不采集 projection”的短路用例；
- 缺少旧入口只允许一行转发的 AST 门禁。

红测结果：`go test ./application/core -run 'AdminApplicationSelect|SelectAccountFacade' -count=1`
因 `adminApplicationDeps` 缺少 `SelectAccount`、projection、锁、发布等字段而失败。

实施后保持以下顺序：

1. `Runtime.SelectAccount(name)` 判定账号可用性；
2. 用 `context.Background()` 采集 runtime projection；
3. 取得 `ViewMu`；
4. 写入 Snapshot 账户名并应用 projection；
5. 锁内 bump revision；
6. 解锁后读取当前视图 SessionID 与 Runtime Snapshot；
7. 发布 `runtime.changed`；
8. 添加“已切换账号”通知。

删除/合并：

- `service_interaction.go:SelectAccount` 原完整状态编排
  → `service_admin_application.go:adminApplication.SelectAccount`

`Service.SelectAccount` 只保留一行组件转发。账号可用性判据仍只在
`Runtime.SelectAccount`，Application 没有复制账号路由规则。

绿测：

- `go test ./application/core -run 'AdminApplicationSelect|SelectAccountFacade|Account' -count=1`：
  通过；
- 行为测试覆盖完整调用顺序与账号不可用短路；
- AST 门禁钉住 `Service.SelectAccount` 只有一个 return。

结构指标：

- `Service` 公开方法：171 → 171（兼容入口保留并降为一行转发）；
- `application/core` 直接内部依赖：39 → 39；
- `application/core` 生产 Go 文件：125 → 125；
- `application/core` 生产代码：18,538 → 18,580 行。

## AP-060 首个切片实施记录

新增 `workspaceEnvironmentApplication`，先以红测钉住：

- `serviceComponents` 尚未装配项目环境应用组件；
- 四条 Workspace Environment 方法仍各自执行可选端口解析与输入裁剪；
- 缺少项目 ID、环境表能力缺失和环境名必填的组件用例；
- 缺少旧入口只允许一行转发的 AST 门禁。

红测结果：`go test ./application/core -run 'WorkspaceEnvironmentApplication|WorkspaceEnvironmentFacade' -count=1`
因 `workspaceEnvironmentApplication`、构造器和 deps 未定义而失败。

实施后，组件集中：

- 调用时解析 `contract.WorkspacePort` 并断言可选
  `contract.WorkspaceEnvironmentPort`；
- 保留 workspace 未装配、项目 ID 为空、环境表能力缺失三条错误语义；
- `WorkspaceEnvironments` 读取环境表与默认环境；
- `SetWorkspaceEnvironment` 裁剪项目 ID/环境名并原样传递口令三态；
- `DeleteWorkspaceEnvironment` 与 `SetDefaultWorkspaceEnvironment` 裁剪输入并转发。

删除/合并：

- `workspace_environments.go:workspaceEnvironmentPort` 原端口解析与输入校验
  → `workspace_environments.go:workspaceEnvironmentApplication.environmentPort`
- 四个 `Service` 方法中的独立解析分支
  → `workspaceEnvironmentApplication` 的四个同名方法

四条旧 `Service` 入口均只保留一行组件转发。连接信息、凭证、默认环境和落盘规则
仍由 `WorkspaceEnvironmentPort` 的唯一实现拥有；应用组件不持锁、不执行慢 I/O。

绿测：

- `go test ./application/core -run 'WorkspaceEnvironmentApplication|WorkspaceEnvironmentFacade|SessionEnvironment' -count=1`：
  通过；
- 行为覆盖四条转发、输入裁剪、workspace 未装配、环境表能力缺失、空项目 ID
  和空环境名；
- AST 门禁钉住四条旧 `Service` 方法只有一个 return。

结构指标：

- `Service` 公开方法：171 → 171（兼容入口保留并降为一行转发）；
- `application/core` 直接内部依赖：39 → 39；
- `application/core` 生产 Go 文件：125 → 125；
- `application/core` 生产代码：18,580 → 18,629 行。

## AP-061 首个切片实施记录

扩展 `adminApplication.SetAccountWindow`，先以红测钉住：

- 组件缺少 `SetAccountWindow` 与对应依赖；
- `Service.SetAccountWindow` 仍直接执行 nil 守卫、Runtime 类型断言和 Setter 构造；
- 缺少名称/整数原样转发与 nil/缺失组件错误用例；
- 缺少旧入口只允许一行转发的 AST 门禁。

红测结果：`go test ./application/core -run 'AdminApplicationSetAccountWindow|SetAccountWindowFacade' -count=1`
因 `adminApplicationDeps.SetAccountWindow` 和组件方法不存在而失败。

实施后，`adminApplication` 集中 nil 守卫和 Setter 适配；装配处仍将
`service.Deps.Runtime` 断言为 `contract.AccountWindowPort`，并传入
`service.Deps.AccountWindowStore`。挡位校验、先写盘再改内存和返回生效值仍由
`backend/limit/accountwindow.Setter` 唯一实现。

删除/合并：

- `service_account_window.go:SetAccountWindow` 原 nil 守卫、类型断言和 Setter 构造
  → `service_admin_application.go:adminApplication.SetAccountWindow`

`Service.SetAccountWindow` 只保留一行组件转发。

绿测：

- `go test ./application/core -run 'AdminApplicationSetAccountWindow|SetAccountWindowFacade|SetAccountWindow' -count=1`：
  通过；
- 行为覆盖转发值、nil 组件、缺失依赖和 Setter 结果；
- AST 门禁钉住旧入口只有一个 return。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 直接内部依赖：39 → 39；
- `application/core` 生产 Go 文件：125 → 125；
- `application/core` 生产代码：18,629 → 18,637 行。

## AP-062 首个切片实施记录

新增 `workspaceGitApplication`，先以红测钉住：

- `serviceComponents` 尚未装配 Workspace Git 查询组件；
- 仓库发现与四条 git 查询分散在 `Service`；
- `WorkspaceTreePort` 能力判定在三条查询中重复；
- 缺少组件级端口分派、缺失能力降级和单行门面门禁。

红测结果：`go test ./application/core -run 'WorkspaceGitApplication|WorkspaceGitFacade' -count=1`
因 `workspaceGitApplication`、构造器和 deps 未定义而失败。

实施后，组件集中：

- `WorkspaceGitRepos` 的工作区 root 获取、可选 `WorkspaceGitReposPort` 分派和
  “不可用”结果；
- `WorkspaceGitLog`、`WorkspaceChanges`、`WorkspaceGitCommitDetail` 的仓库根解析、
  `WorkspaceTreePort` 判定和请求转发；
- `WorkspaceGitCommitFileContent` 的 `WorkspaceFilePort` 判定和请求转发。

删除/合并：

- `service.workspaceRepoPort` → `workspaceGitApplication.workspaceRepoPort`
- `WorkspaceGitLog` / `WorkspaceChanges` / `WorkspaceGitCommitDetail` 三份
  `WorkspaceTreePort` 类型断言 → `workspaceTreePort` 单一函数
- `WorkspaceGitRepos` 与四条 git 查询的 Service 编排
  → `workspaceGitApplication` 同名用例

`resolveWorkspaceRepo` / `repoWithinRoot` 的 containment 判据未复制。五条旧
`Service` 入口均只保留一行组件转发，查询路径不进入共享写锁。

绿测：

- `go test ./application/core -run 'WorkspaceGitApplication|WorkspaceGitFacade|WorkspaceGitQueries|WorkspaceGitCommitFile|WorkspaceGitRepos' -count=1`：
  通过；
- 行为覆盖仓库发现、三条 tree 查询、提交文件读取、缺少能力降级和 repository
  root 转发；
- AST 门禁钉住五条旧 `Service` 方法只有一个 return。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 直接内部依赖：39 → 39；
- `application/core` 生产 Go 文件：125 → 125；
- `application/core` 生产代码：18,637 → 18,703 行。

## AP-063 首个切片实施记录

新增 `workspaceApplication`，先以红测钉住：

- `serviceComponents` 尚未装配 Workspace 应用组件；
- `Service.CreateWorkspaces` 仍直接执行创建、投影、锁、revision 和发布；
- 缺少创建失败后不读取投影的短路用例；
- 缺少旧入口只允许一行转发的 AST 门禁。

红测结果：`go test ./application/core -run 'WorkspaceApplication|CreateWorkspacesFacade' -count=1`
因 `workspaceApplication`、构造器和 deps 未定义而失败。

实施后保持以下顺序：

1. `middle/switch/workspace.UseCase.CreateWorkspaces` 创建并持久化项目；
2. 创建成功后才读取 workspace projection；
3. 取得 `ViewMu`，应用目录投影并在锁内 bump revision；
4. 解锁后读取当前视图 SessionID；
5. 发布 `snapshot.changed`。

删除/合并：

- `workspace_usecase.go:CreateWorkspaces` 原完整投影与发布编排
  → `workspace_usecase.go:workspaceApplication.CreateWorkspaces`

`Service.CreateWorkspaces` 只保留一行组件转发。当前会话、workspace binding 和
Runtime 根不变；项目创建仍由 `middle/switch/workspace` 唯一实现。

绿测：

- `go test ./application/core -run 'WorkspaceApplication|CreateWorkspacesFacade|CreateWorkspaces' -count=1`：
  通过；
- 行为覆盖完整调用顺序和创建失败短路；
- AST 门禁钉住旧入口只有一个 return。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 直接内部依赖：39 → 39；
- `application/core` 生产 Go 文件：125 → 125；
- `application/core` 生产代码：18,703 → 18,759 行。

## AP-064 首个切片实施记录

新增 `sessionSSHApplication`，先以红测钉住：

- `serviceComponents` 尚未装配会话 SSH 应用组件；
- 三条公开方法仍包含会话/工作区可选端口解析和固定环境状态机；
- 缺少旧入口只允许一行转发的 AST 门禁。

红测结果：`go test ./application/core -run 'SessionSSHApplication|SessionSSHFacade' -count=1`
因 `sessionSSHApplication` 未定义而失败。

实施后，组件集中：

- `SessionSSHState` 的可选会话状态端口解析；
- `SetSessionSSHAllowed` 的项目环境表读取、`validateAllowedEnvs` 调用、固定环境
  撤销保护和落盘后读回；
- `SetSessionSSHEnv` 的 SessionID/项目 ID/环境名裁剪、首次绑定、重复绑定幂等、
  固定环境不可变和环境存在性校验；
- 会话所属项目与指定项目两种环境表读取路径。

删除/合并：

- `Service.sessionSSHStore` → `sessionSSHApplication.sessionStore`
- `Service.projectEnvironments` / `projectEnvironmentsFor`
  → `sessionSSHApplication` 同名私有方法
- 三条公开方法中的完整状态机 → `sessionSSHApplication.State/SetAllowed/SetEnv`

`validateAllowedEnvs` 仍是唯一纯判据。`environmentTraceBlockFor` 直接消费同一组件
读取项目环境表，没有第二份缓存。三个旧 `Service` 方法均只保留一行转发。

绿测：

- `go test ./application/core -run 'SessionSSHApplication|SessionSSHFacade|SessionEnvironment|ValidateAllowedEnvs|EnvironmentTrace' -count=1`：
  通过；
- 既有固定环境、项目环境子集、环境 Trace 用例保持通过；
- AST 门禁钉住三个旧方法只有一个 return。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 直接内部依赖：39 → 39；
- `application/core` 生产 Go 文件：125 → 125；
- `application/core` 生产代码：18,759 → 18,820 行。

## AP-065 首个切片实施记录

新增 `roleAccountApplication`，先以红测钉住：

- `serviceComponents` 尚未装配角色账号应用组件；
- `RoleAccount`、`SetRoleAccount`、`RestoreRoleAccounts` 仍直接持有端口解析；
- 缺少 store 失败后 Runtime 回滚的组件用例；
- 缺少恢复时跳过未显式设置账号的用例和单行门面门禁。

红测结果：`go test ./application/core -run 'RoleAccountApplication|RoleAccountFacade' -count=1`
因 `roleAccountApplication`、构造器和 deps 未定义而失败。

实施后，组件集中：

- `RoleAccount` 的 SessionID/role 裁剪与显式设置读取；
- `SetRoleAccount` 的 role/account 必填校验、Runtime 先行热推、store 写入失败后
  用空账号回滚 Runtime；
- `RestoreRoleAccounts` 的可选 Runtime、`dto.AccountRoleVocabulary()` 遍历、
  未设置账号跳过和首个错误短路。

删除/合并：

- `role_permissions.go:RoleAccount` 原端口解析与裁剪
  → `roleAccountApplication.RoleAccount`
- `SetRoleAccount` 原 Runtime/store 编排与回滚
  → `roleAccountApplication.SetRoleAccount`
- `RestoreRoleAccounts` 原恢复循环
  → `roleAccountApplication.RestoreRoleAccounts`

三个旧 `Service` 方法均只保留一行转发。账号归属校验仍在 Bridge/运行时，持久化仍在
`roleDefaultPort`；应用层没有缓存第二份账号状态。

绿测：

- `go test ./application/core -run 'RoleAccountApplication|RoleAccountFacade|RoleAccount' -count=1`：
  通过；
- 行为覆盖 Runtime 回滚、恢复跳过未设置项和既有读写 round-trip；
- AST 门禁钉住三个旧方法只有一个 return。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 直接内部依赖：39 → 39；
- `application/core` 生产 Go 文件：125 → 125；
- `application/core` 生产代码：18,820 → 18,883 行。

## AP-066 首个切片实施记录

新增 `rolePermissionApplication`，先以红测钉住：

- `serviceComponents` 尚未装配角色权限向量应用组件；
- 五条默认/会话向量路径仍直接持有端口解析；
- 整会话恢复与单角色读取各自复制了一份“登记优先/五层解析”判据；
- 缺少单行门面门禁。

红测结果：`go test ./application/core -run 'RolePermissionApplication|RolePermissionFacade' -count=1`
因 `rolePermissionApplication` 未定义而失败。

实施后，组件集中：

- `RoleDefaultVector` / `SetRoleDefaultVector` 的默认向量读写；
- `RestoreRolePermissions` 的批量角色恢复；
- `RolePermissionVector` / `SetRolePermissionVector` 的单角色读写；
- 可选 registry/layers/default 端口解析；
- `resolveRoleSettings` 单一 helper：登记存在时登记优先，显式空集不回退，登记缺失
  才按五层来源解析；批量恢复仍保留层来源提供的标量字段。

删除/合并：

- `RestoreRolePermissions` 与 `RolePermissionVector` 中各一份登记/五层合成逻辑
  → `rolePermissionApplication.resolveRoleSettings`
- 五个公开方法中的端口解析与输入校验
  → `rolePermissionApplication` 五个同名方法

五个旧 `Service` 方法均只保留一行转发。Application 仍不缓存权限向量，存储登记和
`dto.ResolveRoleSettings` 继续是唯一事实源/解析实现。

绿测：

- `go test ./application/core -run 'RolePermissionApplication|RolePermissionFacade|RestoreRolePermissions|RolePermissionVector|RoleDefaultVector|SetRolePermissionVector|SetRoleDefaultVector' -count=1`：
  通过；
- 既有登记优先、显式空集、五层冲突、并发角色读取用例保持通过；
- AST 门禁钉住五个旧方法只有一个 return。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 直接内部依赖：39 → 39；
- `application/core` 生产 Go 文件：125 → 125；
- `application/core` 生产代码：18,883 → 18,959 行。

## AP-067 首个切片实施记录

新增 `roleInheritanceApplication`，先以红测钉住：

- `serviceComponents` 尚未装配继承/SSH 路径应用组件；
- 六条继承与路径白名单方法仍直接持有 `roleDefaultPort` 解析；
- 缺少单行门面门禁。

红测结果：`go test ./application/core -run 'RoleInheritanceApplication|RoleInheritanceFacade' -count=1`
因 `roleInheritanceApplication` 未定义而失败。

实施后，组件集中：

- `RoleInheritMainAgent` / `SetRoleInheritMainAgent` 的默认继承与角色准入；
- `RoleSSHUserSpaces` 的 subagent/teammate 继承回落；
- `RoleDefaultSSHUserSpaces` 的跨会话默认读取；
- `SetRoleSSHUserSpaces` / `SetRoleSessionSSHUserSpaces` 的输入裁剪、规范化与写入；
- 远端 POSIX 绝对路径、根目录拒绝、64 项上限、去重和非空校验。

删除/合并：

- 六条公开方法中的 `roleDefaultStore` 解析与输入校验
  → `roleInheritanceApplication` 六个同名方法

`normalizeRemoteUserSpaces` 仍是唯一路径判据，存储仍由 `roleDefaultPort` 唯一持有。
六个旧 `Service` 方法均只保留一行转发；Role Permissions 的 14 个公开入口至此全部
单行化。

绿测：

- `go test ./application/core -run 'RoleInheritance|SSHUserSpaces|RoleSSHUserSpaces' -count=1`：
  通过；
- 既有默认继承、继承态 mainagent 路径、关闭继承后角色覆盖和路径规范化用例保持通过；
- AST 门禁钉住六个旧方法只有一个 return。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 直接内部依赖：39 → 39；
- `application/core` 生产 Go 文件：125 → 125；
- `application/core` 生产代码：18,959 → 19,019 行。

## AP-068 首个切片实施记录

新增 `schedulerApplication`，先以红测钉住：

- `serviceComponents` 尚未装配定时任务应用组件；
- 创建、编辑、取消三条入口仍由 `Service` 直接编排；
- 缺少参数校验失败后不继续 workspace/plugin/runtime 的短路用例；
- 缺少单行门面门禁。

红测结果：`go test ./application/core -run 'SchedulerApplication|SchedulerFacade' -count=1`
因 `schedulerApplication`、构造器和 deps 未定义而失败。

实施后，组件集中：

- `ScheduleTask` 的默认执行参数、参数/workspace/plugin 校验顺序、Runtime 调用和
  成功后的 runtime 投影刷新；
- `UpdateScheduledTask` 的非空 ID、与创建一致的校验顺序、Runtime 调用和刷新；
- `CancelScheduledTask` 的 Runtime 取消和刷新。

删除/合并：

- `service_scheduler.go:ScheduleTask` 原完整创建编排
  → `schedulerApplication.ScheduleTask`
- `UpdateScheduledTask` 原完整编辑编排
  → `schedulerApplication.UpdateScheduledTask`
- `CancelScheduledTask` 原取消/刷新编排
  → `schedulerApplication.CancelScheduledTask`

默认参数仍由 `withScheduledRunDefaults`、workspace/plugin/执行参数校验仍由原 Service
helper、调度器定义与执行仍由 Runtime 唯一持有。三个旧入口均只保留一行转发。

绿测：

- `go test ./application/core -run 'SchedulerApplication|SchedulerFacade|Scheduled' -count=1`：
  通过；
- 既有默认参数、workspace/plugin 校验、创建/编辑/取消用例保持通过；
- AST 门禁钉住三个旧方法只有一个 return。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 直接内部依赖：39 → 39；
- `application/core` 生产 Go 文件：125 → 125；
- `application/core` 生产代码：19,019 → 19,076 行。

## AP-069～071 实施记录

三个连续切片分别完成：

- AP-069：新增 `runtimeApplication`，迁移 `RefreshRuntimeSnapshot`；
- AP-070：新增 `scheduledRunApplication`，迁移 `AssembleScheduledRun`；
- AP-071：新增 `scheduledSessionApplication`，迁移 `StartScheduledSession`。

红测先依次钉住 `serviceComponents` 缺少三个组件字段，以及三个旧 `Service` 入口仍为
多语句实现。实现后旧入口均只保留一行转发。

AP-069 保持：

1. 锁外采集 runtime projection；
2. 锁内应用 projection 并 bump revision；
3. 解锁后发布 `runtime.changed`；
4. 发布 task deltas。

AP-070 保持：

1. 会话 ID 裁剪与空值拒绝；
2. tier 空值默认 full 并规范化；
3. 注册 SessionUnit；
4. 持久化并应用会话档位；
5. 下发思考强度/循环次数；
6. 归一插件声明并按需把插件装配写入执行 ctx。

AP-071 保持：

1. 提示词非空和 closed/draining 守卫；
2. workspace 解析、`sched` SessionID 分配和引擎打开；
3. workspace projection 锁内应用、revision、解锁后 snapshot 发布；
4. resident/catalog 刷新；
5. 提交前运行装配；
6. 后台提交到新会话，不切用户视图。

删除/合并：

- `service_scheduler.go:RefreshRuntimeSnapshot`
  → `runtime_projection.go:runtimeApplication.Refresh`
- `service_scheduler.go:AssembleScheduledRun`
  → `scheduledRunApplication.AssembleScheduledRun`
- `service_scheduler.go:StartScheduledSession`
  → `scheduledSessionApplication.StartScheduledSession`

底层档位持久化、执行参数、workspace、session engine、runtime 和提交实现均未复制。

绿测：

- `go test ./application/core -run 'RuntimeApplication|Scheduler|Scheduled' -count=1`：
  通过；
- 既有 runtime 刷新、档位/插件装配、无 workspace 不继承绑定和后台新会话用例保持通过；
- AST 门禁钉住三个旧入口均只有一个转发语句。
- 一次组合回归中出现 `TestConversationQueuePreservesPresentationEventOrder`
  偶发事件顺序失败；随后该用例单独连续 10 次通过、完整 core 连续 3 次通过、
  race 通过，完整 Application/contract/e2e 重跑通过。断言未修改，事件时序抖动
  仍作为后续并发专项调查项保留。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 直接内部依赖：39 → 39；
- `application/core` 生产 Go 文件：125 → 125；
- `application/core` 生产代码：19,076 → 19,251 行。

## AP-072 首个切片实施记录

新增 `reviewApplication`，先以红测钉住：

- `serviceComponents` 尚未装配审阅应用组件；
- 文件审阅 Host 仍持有完整 `*Service`；
- `reviewSeq` 仍位于 `serviceState`；
- 两个旧入口仍直接构造 Review Host。

红测结果：`go test ./application/core -run 'ReviewApplication|ReviewFacade' -count=1`
因 `reviewApplication` 未定义而失败。

实施后：

- `reviewApplication` 集中 Workspace FilePort、召唤 ID、ctx/视图会话解析、
  视图锁内请求写入、清空和 snapshot 发布；
- `reviewApplicationHost` 只持有应用组件，不再持有完整 `Service`；
- `reviewSeq` 从 `serviceState` 移入 `reviewApplication`，仍是唯一原子序号；
- 逐文件 containment/敏感/二进制/重复/上限裁决继续只由
  `backend/tool/review.Requests` 实现。

删除/合并：

- `serviceReviewHost{service *Service}` → `reviewApplicationHost{application *reviewApplication}`
- `Service.reviewRequests` → `reviewApplication.reviewRequests`
- `serviceState.reviewSeq` → `reviewApplication.reviewSeq`
- `Service.RequestFileReview` / `ClearReviewRequest` → 一行组件转发

绿测：

- `go test ./application/core -run 'ReviewApplication|ReviewFacade|RequestFileReview' -count=1`：
  通过；
- 既有 workspace 边界、二进制/敏感/重复过滤、快照补放和全部跳过不覆盖请求用例保持通过；
- AST 门禁钉住两个旧入口只有一条转发语句。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 直接内部依赖：39 → 39；
- `application/core` 生产 Go 文件：125 → 125；
- `application/core` 生产代码：19,251 → 19,301 行。

## AP-073～075 实施记录

### AP-073：Lifecycle drain/cancel

新增 `lifecycleApplication`，迁移：

- `BeginGracefulShutdown`
- `WaitForIdle`
- `AnyChatRunning`
- `CancelAllChats`
- `CancelChat`

组件只通过窄回调读取 idle/running、收集 cancel 并在锁外调用；SessionUnit 仍是
cancel 的唯一所有者。五个旧入口均只保留一行转发。

### AP-074：Lifecycle Shutdown

扩展 `lifecycleApplication.Shutdown`：

- `BeginShutdown` 负责 closed 幂等门与取消全部会话；
- 首个调用按固定顺序执行 flush → catalog stop → session close → lifecycle consumer
  stop → worktable close → approval shutdown；
- 后续调用直接返回，不重复清理。

`Service.Shutdown` 只保留一行转发。专项测试钉住清理只执行一次及固定顺序。

### AP-075：History Search 契约化

先收紧预算红测：

- `application/testdata/application_budget.json` 从 39 收紧到 38；
- 从允许依赖中移除 `backend/engine/context/seelectx/search`。

红测结果：`go test ./application -run 'TestApplicationArchitectureBudget' -count=1`
按预期指出唯一超预算依赖是 `seelectx/search`。

实施后：

- `Service.SearchHistory` 返回类型改为 `dto.SearchResult`；
- `SearchHistoryHandler` 省略 limit 时传 0，由 Backend search 的唯一默认/clamp
  规则解析，不在 Application 重写默认值；
- core 删除 `seelectx/search` import。

绿测：

- `go test ./application -run 'TestApplicationArchitectureBudget' -count=1`：通过；
- `go test ./application/core -run 'SearchHistory' -count=1`：通过；
- History Search handler 的 JSON、空查询、Runtime 错误和权威命中回归保持通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 直接内部依赖：39 → 38；
- `application/core` 生产 Go 文件：125 → 125；
- `application/core` 生产代码：19,301 → 19,392 行。

## AP-076 首个切片实施记录

新增 `historySearchApplication`，先以红测钉住组件未装配、两个旧入口仍直接适配。

实施后：

- `SearchHistory` 集中空查询校验、Runtime 可用性和查询转发；
- `SearchHistoryHandler` 集中 JSON 解码、空查询、Runtime 调用和 JSON 编码；
- 省略 limit 时保持传 0，不在 Application 重建默认值；
- 检索、默认 limit、clamp、压缩栈和事件库兜底仍由 Runtime/search 唯一实现。

删除/合并：

- `Service.SearchHistory` / `SearchHistoryHandler` 原适配
  → `historySearchApplication` 同名方法

两个旧入口均只保留一行转发。

绿测：

- `go test ./application/core -run 'HistorySearchApplication|HistorySearchFacade|SearchHistory' -count=1`：
  通过；
- 既有结构化命中、空查询和 Runtime 错误用例保持通过；
- 依赖预算仍为 38，没有重新引入 `seelectx/search`。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 直接内部依赖：38 → 38；
- `application/core` 生产 Go 文件：125 → 125；
- `application/core` 生产代码：19,392 → 19,435 行。

## AP-077 首个切片实施记录

新增 `completionApplication`，先以红测钉住组件未装配和 `Suggestions` 仍为直接实现。

实施后，组件集中：

- sigil 解析后的参数区拒绝；
- `/` 的命令 + Skill、`#` 的 Plugin、`$` 的 Skill 候选合成；
- 大小写不敏感前缀过滤；
- command → skill → plugin 的跨域稳定排序；
- 输出切片的独立拷贝。

命令、Skill、Plugin 目录仍由 `commandSuggestions` / `skillSuggestions` /
`pluginSuggestions` 从各自宿主端口读取。sigil 常量与 `splitSigil` 仍是唯一词法判据。

删除/合并：

- `Service.Suggestions` 的候选合成、过滤和排序
  → `completionApplication.Suggestions`

旧入口只保留一行转发。

绿测：

- `go test ./application/core -run 'CompletionApplication|CompletionFacade|Suggestions' -count=1`：
  通过；
- 既有工具排除、命令/Skill/Plugin 候选、前缀过滤和排序用例保持通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 直接内部依赖：38 → 38；
- `application/core` 生产 Go 文件：125 → 125；
- `application/core` 生产代码：19,435 → 19,472 行。

## AP-078 首个切片实施记录

新增 `referenceApplication`，先以红测钉住组件未装配和三个旧入口仍为 Service 实现。

实施后，组件集中：

- `read_tool_result` JSON 参数校验、默认/最大分页值；
- `node:<nodeID>:` 子代理归档读取；
- `result:call_...` → 真实 `tr-...` alias 解析；
- pending 内存归档与 durable SessionStore 读取的优先级；
- `dto.ToolResultPage` 构造与 JSON 编码；
- `read_plan` 的 PlanRef 默认、PlanFrame 查找、节点筛选和 JSON 编码。

删除/合并：

- `Service.ReadToolResultHandler` / `ToolResultContent` / `ReadPlanHandler` 原适配
  → `referenceApplication` 同名用例
- 引用解析、分页和 Plan 筛选顺序从 Service 移到组件；

三个旧入口均只保留一行转发。Engine、TaskStack、Session transcript store 和
`backend/tool/reference` 仍是唯一底层实现。

绿测：

- `go test ./application/core -run 'ReferenceApplication|ReferenceFacade|read_tool_result|ReadToolResult|ReadPlan|ToolResultContent' -count=1`：
  通过；
- 节点引用、别名、pending/durable 分页、Plan 节点筛选和错误语义回归保持通过；
- 依赖预算仍为 38。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 直接内部依赖：38 → 38；
- `application/core` 生产 Go 文件：125 → 125；
- `application/core` 生产代码：19,472 → 19,547 行。

## AP-079 首个切片实施记录

新增 `sessionMetaApplication`，先以红测钉住组件未装配和两个旧入口仍由 Service
直接装配。

实施后：

- `SetSessionMeta` 经 `MetaDeps` 调用 `middle/switch.SetMeta`，保持校验、持久化和
  目录刷新唯一实现；
- `SessionMeta` 委托 `SessionQuery`，保持未装配错误语义；
- `Service` 两个入口均只保留一行转发。

绿测：

- `go test ./application/core -run 'SessionMetaApplication|SessionMetaFacade|SessionMeta' -count=1`：
  通过；
- 既有 GUI Bridge 与 Session Meta 端口用例保持通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 直接内部依赖：38 → 38；
- `application/core` 生产 Go 文件：125 → 125；
- `application/core` 生产代码：19,547 → 19,588 行。

## AP-080 首个切片实施记录

新增 `projectBoardQuery`，先以红测钉住组件未装配和四个只读入口仍由 Service 实现。

实施后，组件集中：

- `WorkTableRowsFor` 的 Plan/Task/SubAgentTree/AsyncRun 输入合成和
  `worktable.BuildWorkTable` 调用；
- `ProjectRootFor` 的 SessionQuery 委托；
- `ProjectBoardTeamPlanFor` 的 Runtime `TeamworkPlanSource` 窄能力探测；
- `ProjectBoardTeamStateFor` 从同一 Plan.State 提取运行态，不新增第二份事实。

四个旧 `Service` 方法均只保留一行转发。锁边界、WorkTable 构建、SessionQuery 和
Runtime Team Plan 仍是唯一实现。

绿测：

- `go test ./application/core -run 'ProjectBoardQuery|ProjectBoardReadFacade|ProjectBoard|WorkTableRowsFor|ProjectRootFor' -count=1`：
  通过；
- 既有活体投影一致性、项目根绑定、Team Plan/State 和 Plain host 降级用例保持通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 直接内部依赖：38 → 38；
- `application/core` 生产 Go 文件：125 → 125；
- `application/core` 生产代码：19,588 → 19,636 行。

## AP-081 首个切片实施记录

新增 `sessionReleaseApplication`，先以红测钉住组件未装配和
`releaseSessionMemory` / `reconcileSessionRelease` 仍由 Service 直接编排。

实施后，组件集中：

- releaseIdle 的二次受保护集检查；
- 释放前 Session flush 和工具台账 flush；
- Engine unload、桥侧 Session bundle release；
- Goal 会话句柄清理；
- 可见正文释放；
- releaseFull 的 SessionUnit、Task/Plan、标题移除；
- resident/content LRU 次序清理；
- releaseFull 工具记录台账回收；
- 目录刷新，以及 resident/content 两层 reconcile。

删除/合并：

- `releaseSessionMemory` 原完整释放顺序
  → `sessionReleaseApplication.Release`
- `reconcileSessionRelease` 两层收敛
  → `sessionReleaseApplication.Reconcile`

两个旧 Service 方法只保留一行转发。`protectedSessionLocked`、`releaseBudget`、正文 LRU、
Engine/Runtime/Store 的具体释放动作仍保持原唯一实现。

绿测：

- `go test ./application/core -run 'SessionReleaseApplication|SessionReleaseFacade|SessionRelease|releaseSession' -count=1`：
  通过；
- 既有受保护集、统一窗口、完整释放、工具记录隔离和视图会话释放用例保持通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 直接内部依赖：38 → 38；
- `application/core` 生产 Go 文件：125 → 125；
- `application/core` 生产代码：19,636 → 19,715 行。

## AP-082 首个切片实施记录

新增 `contextCompactionApplication`，先以红测钉住组件未装配和两个旧入口仍由 Service
直接编排。

实施后，组件集中：

- 从工具 ctx 或视图快照解析目标 SessionID；
- 同会话压缩 gate 的 acquire/release；
- `context_runtime.Coordinator.CompactContextNow` 调用；
- CompactDone / Unrecorded / Failed / Scheduled / NotNeeded 五类结果与回执映射；
- `compact_context` 工具的参数解析、模型自述 reason 和 JSON 编码。

删除/合并：

- `Service.CompactContextNow` 原完整编排
  → `contextCompactionApplication.CompactContextNow`
- `Service.CompactContextHandler` 原工具适配
  → `contextCompactionApplication.CompactContextHandler`

两个旧 Service 方法只保留一行转发。压缩算法、持久化、失败痕、恢复与门禁判据仍只在
`context_runtime`。

绿测：

- `go test ./application/core -run 'ContextCompactionApplication|ContextCompactionFacade|CompactContext' -count=1`：
  通过；
- 既有压缩成功、失败、局部 fallback、进度门禁、索引、跨轮恢复和 ViewMu 不持锁用例
  保持通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 直接内部依赖：38 → 38；
- `application/core` 生产 Go 文件：125 → 125；
- `application/core` 生产代码：19,715 → 19,763 行。

## AP-083 首个切片实施记录

新增 `faultGuardApplication`，先以红测钉住组件未装配、`degradeOnce` 仍在
`serviceState`，以及两个旧入口仍直接实现。

实施后，组件集中：

- 降级退出 `sync.Once` 幂等门；
- closed/draining 标记与全部运行 Session 的 cancel；
- catalog stop、session close、lifecycle consumer stop、worktable close、
  approval shutdown；
- `EventExitRequested` 进程级事件发布；
- `handleLifecycleFault` 的降级后 panic 重抛。

删除/合并：

- `serviceState.degradeOnce` → `faultGuardApplication.degradeOnce`
- `degradeAndRequestExit` 原清理顺序 → `faultGuardApplication.DegradeAndRequestExit`
- `handleLifecycleFault` 原 fail-fast → `faultGuardApplication.HandleLifecycleFault`

两个旧 Service 方法均只保留一行转发；清理动作仍调用原唯一实现。

绿测：

- `go test ./application/core -run 'FaultGuardApplication|FaultGuardFacade|DegradeAndRequestExit|LifecycleFault' -count=1`：
  通过；
- 既有幂等、closed/draining、ExitRequested 和 panic 重抛用例保持通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 直接内部依赖：38 → 38；
- `application/core` 生产 Go 文件：125 → 125；
- `application/core` 生产代码：19,763 → 19,822 行。

## AP-084 首个切片实施记录

新增 `perfStatsApplication`，先以红测钉住组件未装配和 `PerfStats` 仍直接取数。

实施后，组件集中：

- `Core.ViewMu` 读锁；
- Core Snapshot 输入；
- `Limits().HistoryWindow`；
- Task 协调器的 ToolResultRefs；
- 调用 `middle/view.PerfStats` 计算无内容指标。

`Service.PerfStats` 只保留一行转发。性能指标的字段、预算和计算仍由 middle/view 唯一拥有。

绿测：

- `go test ./application/core -run 'PerfStatsApplication|PerfStatsFacade|PerfStats' -count=1`：
  通过；
- Bridge/headless 的 PerfStats wire 形状不变。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 直接内部依赖：38 → 38；
- `application/core` 生产 Go 文件：125 → 125；
- `application/core` 生产代码：19,822 → 19,872 行。

## AP-085 首个包提取实施记录

Reference 用例不再继续作为 `application/core` 内部组件扩张，而是迁入独立
`application/reference` 包。先新增会红的结构门禁，要求：

- `application/reference/application.go` 存在；
- 导出 `Deps`、`Application` 与 `New`；
- `application/core/reference_tools.go` 不再定义 `referenceApplication` /
  `referenceApplicationDeps`。

红测结果：

```text
go test ./application -run TestReferenceApplicationIsExtractedFromCore -count=1
```

按预期因 `application/reference/application.go` 不存在而失败。

实施后：

- 参数校验、默认/最大分页、节点引用、`result:call_*` 别名、pending/durable
  读取委托、contains 过滤和 Plan 节点筛选迁入 `application/reference`；
- `application/core/reference_tools.go` 只保留三个一行 Service 转发和
  Engine/Task/Session 的窄回调适配；
- `serviceComponents.referenceApplication` 改为
  `*applicationreference.Application`；
- facade 依赖闭包登记 `application/reference`，上限按新领域落点从 90 调到 91。

删除/合并：

- `application/core/reference_tools.go:referenceApplication.Deps`
  → `application/reference.Deps`
- `referenceApplication.ReadToolResultHandler`
  → `application/reference.Application.ReadToolResultHandler`
- `referenceApplication.ToolResultContent/toolResultContent`
  → `application/reference.Application.ToolResultContent/toolResultContent`
- `referenceApplication.ReadPlanHandler`
  → `application/reference.Application.ReadPlanHandler`

Engine 节点归档、Task pending/Plan 栈、Session durable 和
`backend/tool/reference` 分页判据仍是各自唯一实现。

绿测：

- `go test ./application/reference -count=1`：通过；
- `go test ./application/core -run 'Reference|ReadToolResult|ReadPlan|ToolResultContent' -count=1`：
  通过；
- `go test ./application -run TestReferenceApplicationIsExtractedFromCore -count=1`：
  通过；
- `go test ./e2e -run TestFacadeDependencyClosureRatchet -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 直接内部依赖：38 → 38；
- `application/core` 生产 Go 文件：126 → 126（本批只缩短既有文件）；
- `application/core` 生产代码：19,872 → 19,819 行；
- `application/reference` 生产代码：139 行。

## AP-086～089 包提取实施记录

本批把四个已具有窄 deps 的应用组件从 core 文件中移出，建立首批领域包：

| 批次 | 新包 | 迁出能力 | 新包生产行数 |
|---|---|---|---:|
| AP-086 | `application/session` | Session Meta | 38 |
| AP-087 | `application/input` | sigil/Suggestions | 99 |
| AP-088 | `application/conversation/search` | History Search 工具适配 | 70 |
| AP-089 | `application/observability` | PerfStats | 39 |

共同约束：

- 新包不得导入 `application/core`；
- 新包不持完整 `Service`；
- core 只保留构造调用与兼容 Service 转发；
- facade closure 逐包登记，未放宽 backend/middle 边界；
- 包内测试覆盖各自行为，core 保留 parity/结构门禁。

删除/合并：

- core `sessionMetaApplicationDeps` / `sessionMetaApplication`
  → `application/session.MetaDeps` / `MetaApplication`
- core `completionApplicationDeps` / `completionApplication`
  → `application/input.Deps` / `Application`
- core `historySearchApplicationDeps` / `historySearchApplication`
  → `application/conversation/search.Deps` / `Application`
- core `perfStatsApplicationDeps` / `perfStatsApplication`
  → `application/observability.PerfDeps` / `PerfApplication`

结构指标（相对 AP-084）：

- `application/core` 生产代码：19,872 → 19,680 行（净减 192）；
- `application/core` 生产文件：126 → 126（包含同期新增的 `compressed_turn`
  内部组件；后续应迁包后删除该文件）；
- 新包生产行合计：139 + 38 + 99 + 70 + 39 = 385 行；
- `Service` 公开方法：171 → 171；
- core 直接内部依赖：38 → 38。

验证：

- `go test ./application/session/... ./application/input/... ./application/observability/... ./application/reference/... ./application/conversation/search/... -count=1`：通过；
- `go test ./application/... ./contract/... ./e2e -count=1`：通过；
- `go build ./...`、`go vet ./...`：通过。

## AP-090 Async Completion 组件化

新增 `asyncCompletionApplication`，把两张作业表的终态扫描、幂等账、忙会话保护与
提交入口从 `Service` 编排中收拢到组件：

- `triggerAsyncCompletions`
- `triggerTeamworkJobCompletions`
- 共用 `SessionIdle`、`SubmitConversation` 窄 deps
- 作业表仍由 Runtime 唯一提供
- prompt 内容仍复用既有自由函数

两个旧 Service 方法只保留一行转发。该组件后续与 WorkTable consumer 一起迁入
`application/execution` 或独立 `application/completion` 包，当前不直接迁包。

绿测：

- `go test ./application/core -run 'AsyncCompletion|AsyncCompletions|TeamworkJobCompletion|TeamworkCompletion' -count=1`：
  通过。

## AP-091/092 领域包提取

### AP-091 `application/lifecycle`

把 Lifecycle 与 Fault Guard 从 core 私有组件迁入新包：

- `Application`：drain、WaitForIdle、AnyChatRunning、CancelAllChats、CancelChat、
  幂等 Shutdown。
- `FaultGuardApplication`：degrade once、清理顺序、ExitRequested 和 panic 重抛。
- 包只依赖标准库，所有 SessionUnit、Session 域、EventHub 和 worker 动作通过 deps
  注入。

core `service_input.go` / `fault_guard.go` 删除状态和编排，仅保留 Service 一行转发与
装配适配。

### AP-092 `application/policy`

把 Role Account、Permission Vector、Inheritance/SSH Path 应用组件迁入
`application/policy`：

- `RoleDefaultsPort`、`PermissionRegistryPort`、`PermissionLayerPort` 由调用方端口提供。
- 登记优先、显式空集不回退、五层解析只在登记缺失时发生。
- runtime 账号先行与 store 失败回滚保持在 Application。
- SSH 路径规范化通过注入 validation，避免复制唯一判据。

core `role_permissions.go` 从 447 行缩短到约 123 行，只保留兼容入口和适配器。

### 合并后指标

- `application/core` 生产代码：19,680 → 18,723 行；
- `application/core` 直接内部依赖：38 → 37（`backend/tool/reference` 退出）；
- 新包生产代码：
  - `application/session`：370 行；
  - `application/lifecycle`：139 行；
  - `application/policy`：412 行；
  - `application/reference`：139 行；
  - `application/input`：99 行；
  - `application/observability`：39 行；
  - `application/conversation/search`：70 行。
- `Service` 公开方法：171 → 171（兼容期尚未退场）。

验证：

- `go test ./application/lifecycle/... ./application/policy/... ./application/session/... -count=1`：通过；
- `go test ./application/core -run 'Lifecycle|FaultGuard|Shutdown|Role' -count=1`：通过；
- `go test ./application/... ./contract/... ./e2e -count=1`：通过；
- `go build ./...`、`go vet ./...`：通过。

## AP-092.1 core 生产规模 ratchet

[`application/architecture_budget_test.go`](../../application/architecture_budget_test.go)
原预算只冻结 `Service` 方法数和 core 直接内部依赖，不能阻止“把组件搬进 core 后
依赖数不变、生产代码继续膨胀”。本批新增两个结构预算：

- `core_production_file_limit`：`application/core` 非测试生产 Go 文件数；
- `core_production_line_limit`：同一文件集的物理行数。

当前基线写入
[`application_budget.json`](../../application/testdata/application_budget.json)：
126 个文件、18,723 行。预算只允许下降；任何上升都必须显式修改 JSON 并在
实施记录说明原因。

红测证据：先把两个预算设为 `0`，运行
`go test ./application -run TestApplicationArchitectureBudget -count=1`，
按预期失败并报告 `application/core 生产 Go 文件 = 126，预算 = 0`。

转绿：写入真实基线后同一命令通过。该门禁与 facade closure、core 直接内部依赖
三项一起构成包提取的硬预算，不再只靠人工报告行数。

## AP-092.2 包边界与兼容转发 ratchet

在继续派发 ADM / WS / REF 包提取前，补充三类机械门禁：

1. [`application/architecture_budget_allowlist_test.go`](../../application/architecture_budget_allowlist_test.go)
   要求 core 直接内部依赖白名单与实测集合双向完全一致，不能只检查“实测 ⊆ 白名单”
   而让已迁出的许可永久残留；
2. [`application/package_boundaries_test.go`](../../application/package_boundaries_test.go)
   要求已提取领域包不得导入 `application/core`，生产 struct 不得持有完整
   `Service`；
3. [`application/forwarding_boundary_test.go`](../../application/forwarding_boundary_test.go)
   对 Session、Policy、Lifecycle、Admin、Workspace 与
   `read_compressed_turn` 的已迁移兼容入口做 AST 断言：方法体只能有一个
   call/return 转发语句，不能重新长出第二份状态机。

审计结论：

- `application/session` 已包含 Meta、SSH、Release，README 曾停留在只有 Meta 的首批
  状态，已按事实更新；
- `application/policy` 的 14 个 Service 入口都只转发到 `RoleApplication`；
  五层解析、显式空集优先、Runtime 账号回滚与 SSH 路径规范化只有包内一份；
- `application/lifecycle` 的 Shutdown 与 FaultGuard once/清理顺序只有包内一份，
  core 只保留 SessionUnit、worker、Session 域和 EventHub 适配；
- `application/core/README.md` 中 Session Metadata、Session Release、Lifecycle、
  Fault Guard、PerfStats 与角色策略的“core 私有组件”描述已更正为领域包事实，
  core 直接内部依赖终点明确为 37。

专项绿测：

- `go test ./application/session/... ./application/policy/... ./application/lifecycle/... -count=1`
- `go test ./application/core -run 'SessionMeta|SessionSSH|Release|FaultGuard|Lifecycle|Shutdown|WaitForIdle|Cancel|Role' -count=1`
- `go test ./application -run TestMigratedServiceMethodsRemainOneLineForwards -count=1`
- `go test ./application -run TestExtractedApplicationPackagesDoNotImportCore -count=1`

### AP-093 `read_compressed_turn` 包提取（已完成）

`application/core/compressed_turn_application.go` 仍是 AP-085 之后的额外 core
内部组件，并且每次调用重新构造组件图。先新增
[`application/reference_package_test.go`](../../application/reference_package_test.go)
中的 `TestCompressedTurnApplicationIsExtractedFromCore`：

```text
go test ./application -run TestCompressedTurnApplicationIsExtractedFromCore -count=1
```

红灯时 `application/reference/compressed_turn.go` 尚不存在，且 core 仍定义
`compressedTurnApplication` / `compressedTurnApplicationDeps`。AP-094 完成后收口本批：

- 参数校验、segment 归档读取、轮次渲染、contains 过滤和字符分页进入
  `application/reference/compressed_turn.go`；
- `Deps` 新增当前视图作用域、压缩轮次读取和 ref 前缀三个真实端口；
- core `reference_tools.go` 只装配 SessionStore 显式 workspace 读取；
- `Service.ReadCompressedTurnHandler` 只保留一行转发；
- `application/core/compressed_turn_application.go` 及其 core 内部组件测试删除；
- core `compressed_turn.go` 继续只拥有写侧 `CompressedTurnArchiver`。

绿测：

- `go test ./application/reference -count=1`：通过；
- `go test ./application/core -run 'CompressedTurn|Reference|ReadToolResult|ReadPlan|ToolResultContent' -count=1`：通过；
- `go test ./application -run 'CompressedTurnApplicationIsExtractedFromCore|ExtractedApplicationPackagesDoNotImportCore|MigratedServiceMethodsRemainOneLineForwards|ApplicationArchitectureBudget' -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：126 → 125；
- `application/core` 生产代码：18,634 → 18,481 行；
- `application/reference` 生产代码：139 → 265 行（新增压缩轮读取与渲染）；
- core 生产规模预算同步收紧到 125 文件、18,481 行。

### AP-094 `application/admin` 包提取

账号 Admin 的 7 个用例从 core 私有组件迁入
[`application/admin`](../../application/admin/README.md)：

- `AddAccount`、`AccountDetails`、`UpdateAccount`、`DeleteAccount`；
- `AccountBalance`；
- `SelectAccount`；
- `SetAccountWindow`。

包内 `AccountDeps` 只持有账号管理、余额、选择、窗口与 projection 窄端口；
`ProjectionPort` 固定“锁外采集 → 锁内账号/投影/revision → 解锁后发布/通知”
顺序。账号档校验与写入、Runtime 热注册、余额探测、accountwindow 落盘仍由原
唯一实现拥有。

删除/合并：

- `application/core/service_admin_application.go:adminApplicationDeps`
  → `application/admin.AccountDeps`
- `application/core/service_admin_application.go:adminApplication`
  → `application/admin.AccountApplication`
- `adminApplication` 7 个用例体
  → `application/admin.AccountApplication` 同名方法
- core 仅保留 `service_admin_component.go` 的宿主适配器和
  `newServiceAdminApplication`

红测：包测试先因 `NewAccountApplication` / `AccountDeps` /
`AccountApplication` 未定义失败；实现后包测试通过。

绿测：

- `go test ./application/admin -count=1`：通过；
- `go test ./application/core -run 'Admin|Account|SelectAccount|SetAccountWindow' -count=1`：通过；
- `go test ./application -run TestApplicationArchitectureBudget -count=1`：通过；
- `go test ./e2e -run TestFacadeDependencyClosureRatchet -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171（兼容期保留）；
- `application/core` 生产文件：126 → 126（旧文件由适配器替代）；
- `application/core` 生产代码：18,723 → 18,634 行；
- core 生产规模预算同步收紧到 18,634；
- `application/admin` 生产代码：159 行；
- facade closure 新增 `application/admin`，上限 96 → 97。

### AP-095 `application/workspace` 包提取

Workspace 的项目环境、Git 查询和批量登记投影编排从 core 私有组件迁入
[`application/workspace`](../../application/workspace/README.md)：

- 环境表读取、写入、删除与默认环境四条用例；
- Git 仓库发现、日志、工作区改动、提交详情和提交内文件读取；
- `CreateWorkspaces` 成功后的 projection、revision 与 snapshot 发布顺序。

包内 `Deps` 分开接收项目环境端口、当前视图根端口、批量创建回调和
`ProjectionPort`。路径 containment 的绝对路径、`..`、软链真实路径、非目录与
未绑定根判据只在新包 `resolveRepo` / `withinRoot` 中保留一份。

删除/合并：

- `application/core/workspace_environments.go:workspaceEnvironmentApplication`
  → `application/workspace.Application`
- `application/core/workspace_gitrepos.go:workspaceGitApplication`
  → `application/workspace.Application`
- `application/core/workspace_usecase.go:workspaceApplication`
  → `application/workspace.Application`
- core 只保留 `newServiceWorkspaceApplication` 宿主适配、projection port 和
  三个兼容转发文件中的 12 个 Service 入口一行转发

行为/结构测试：

- `go test ./application/workspace -count=1`：通过；覆盖环境 ID trim、缺失端口、
  Git 相对/绝对/`..`/软链逃逸、仓库发现降级、文件能力缺失和 CreateWorkspaces
  成功/失败顺序。
- `go test ./application/core -run 'Workspace|Environment|Git|CreateWorkspaces|SessionSSH' -count=1`：通过；
- `go test ./application -run 'WorkspaceApplicationIsExtractedFromCore|MigratedServiceMethodsRemainOneLineForwards|ExtractedApplicationPackagesDoNotImportCore|ApplicationArchitectureBudget|CoreInternalDependencyAllowList' -count=1`：通过；
- `go test ./e2e -run TestFacadeDependencyClosureRatchet -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- core 直接内部依赖：37 → 37；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：18,481 → 18,156 行；
- `application/workspace` 生产代码：282 行；
- core 生产规模预算同步收紧到 125 文件、18,156 行；
- facade closure 新增 `application/workspace`，上限 97 → 98。

### AP-096 `application/session/query` 包提取

只读 `SessionQuery` 从 core 私有类型迁入
[`application/session/query`](../../application/session/query/README.md)：

- `ListSessions` 的目录组合与共享读锁；
- `ProjectRootFor` 的会话绑定事实读取；
- `SessionMeta`、`WaitCatalogRefresh`；
- `SnapshotOf`、`GetSessionTranscript`、`SessionInputIndex`。

包内 `Deps` 只持有目录、metadata、workspace 和 durable read 回调；不持有
`Service`，不导入 `application/core`。core `session_query.go` 只保留
`newServiceSessionQuery` 适配和 `waitCatalogRefresh` 宿主回调。

删除/合并：

- `application/core/session_query.go:sessionQueryDeps`
  → `applicationsessionquery.Deps`
- `application/core/session_query.go:sessionQuery`
  → `applicationsessionquery.Application`
- `SessionQuery` 的 7 个查询方法体
  → `application/session/query.Application` 同名方法

红测：

- `go test ./application -run TestSessionQueryApplicationIsExtractedFromCore -count=1`
  先因 `application/session/query/application.go` 不存在失败。

绿测：

- `go test ./application/session/query -count=1`：通过；
- `go test ./application/core -run 'SessionQuery|ListSessions|SnapshotOf|Transcript|InputIndex|WaitCatalogRefresh|SessionMeta' -count=1`：通过；
- `go test ./application -run 'SessionQueryApplicationIsExtractedFromCore|QueryImplementationsAvoidSharedWriteLockAndServiceField|ExtractedApplicationPackagesDoNotImportCore|MigratedServiceMethodsRemainOneLineForwards|ApplicationArchitectureBudget' -count=1`：通过；
- `go test ./e2e -run TestFacadeDependencyClosureRatchet -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- core 直接内部依赖：37 → 37；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：18,156 → 18,069 行；
- `application/session/query` 生产代码：100 行；
- core 生产规模预算同步收紧到 18,069 行；
- facade closure 新增 `application/session/query`，上限 98 → 99。

### AP-097 `application/history` 包提取

历史窗口分页从 core 私有类型迁入
[`application/history`](../../application/history/README.md)：

- `LoadMoreHistory` 的窗口钳制、内容卸载回读与前置安装；
- `LoadLatestHistory` 的热尾短路、尾部读取与替换安装；
- `Page` 的可见/未过滤坐标分离；
- `InstallPrepend` / `InstallReplace` 两种安装模式。

core `session_history.go` 仍拥有磁盘冷读、可见窗口安装和内容 LRU；新包只做分页
排序与模式决策。`application/history` 不导入 `application/core`。

删除/合并：

- `application/core/session_conversation_history_query.go:conversationHistoryQueryDeps`
  → `application/history.Deps`
- `application/core/session_conversation_history_query.go:conversationHistoryQuery`
  → `application/history.Application`
- `application/core/session_history.go:conversationPage`
  → `application/history.Page`
- `application/core/session_history.go:historyPageInstall` 与两个常量
  → `application/history.InstallMode` / `InstallPrepend` / `InstallReplace`

红测：

- `go test ./application -run TestConversationHistoryApplicationIsExtractedFromCore -count=1`
  先因 `application/history/application.go` 不存在失败。

绿测：

- `go test ./application/history -count=1`：通过；
- `go test ./application/core -run 'LoadMoreHistory|LoadLatestHistory|History|reloadSessionContent' -count=1`：通过；
- `go test ./application -run 'ConversationHistoryApplicationIsExtractedFromCore|QueryImplementationsAvoidSharedWriteLockAndServiceField|ExtractedApplicationPackagesDoNotImportCore|MigratedServiceMethodsRemainOneLineForwards|ApplicationArchitectureBudget' -count=1`：通过；
- `go test ./e2e -run TestFacadeDependencyClosureRatchet -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- core 直接内部依赖：37 → 37；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：18,069 → 17,962 行；
- `application/history` 生产代码：116 行；
- core 生产规模预算同步收紧到 17,962 行；
- facade closure 新增 `application/history`，上限 99 → 100。

### AP-098 `application/execution` 项目看板查询提取

项目看板只读查询从 core 私有类型迁入
[`application/execution`](../../application/execution/README.md)：

- `WorkTableRowsFor` 的会话输入组合与 WorkTable 构建；
- `ProjectRootFor` 的绑定根读取；
- `ProjectBoardTeamPlanFor` / `ProjectBoardTeamStateFor` 的窄端口探测与状态提取。

core `project_board_read.go` 只保留 `newServiceProjectBoardQuery` 宿主适配、
`taskRecordsForSession` 唯一会话作用域判据和四个一行转发。

删除/合并：

- `application/core/project_board_read.go:projectBoardQueryDeps`
  → `application/execution.ProjectBoardDeps`
- `application/core/project_board_read.go:projectBoardQuery`
  → `application/execution.ProjectBoardApplication`
- 四个只读方法体 → `ProjectBoardApplication` 同名方法

红测：

- `go test ./application -run TestProjectBoardQueryIsExtractedFromCore -count=1`
  先因 `application/execution/project_board.go` 不存在失败。

绿测：

- `go test ./application/execution -count=1`：通过；
- `go test ./application/core -run 'ProjectBoard|WorkTableRowsFor|ProjectRootFor' -count=1`：通过；
- `go test ./application -run 'ProjectBoardQueryIsExtractedFromCore|ExtractedApplicationPackagesDoNotImportCore|MigratedServiceMethodsRemainOneLineForwards|ApplicationArchitectureBudget' -count=1`：通过；
- `go test ./e2e -run TestFacadeDependencyClosureRatchet -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- core 直接内部依赖：37 → 37；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：17,962 → 17,890 行；
- `application/execution` 生产代码：58 行（本切片）；
- core 生产规模预算同步收紧到 17,890 行；
- facade closure 新增 `application/execution`，上限 100 → 101。

### AP-099 WorkItem/Subagent 执行应用提取

WorkItem、teammate 与 Subagent 应用组件迁入
[`application/execution`](../../application/execution/README.md)：

- `ExecutionApplication` 的 WorkItem execution、teammate inspection、团队看板/实时读取；
- WorkItem ID 解析与唯一手动可变的 todo 状态入口；
- Subagent recovery、恢复、fork、详情与树清理。

core `work_table_application.go` / `subagent_application.go` 只保留 Runtime 与
subagent view coordinator 的装配 adapter；旧 Service 入口保持一行转发。

删除/合并：

- `application/core/work_table_application.go:executionApplicationDeps`
  → `application/execution.WorkItemDeps`
- `application/core/work_table_application.go:executionApplication`
  → `application/execution.WorkItemApplication`
- `application/core/subagent_application.go:subagentApplicationDeps`
  → `application/execution.SubagentDeps`
- `application/core/subagent_application.go:subagentApplication`
  → `application/execution.SubagentApplication`

红测：

- `go test ./application -run TestExecutionApplicationsAreExtractedFromCore -count=1`
  先因 `application/execution/work_item.go` 不存在失败。

绿测：

- `go test ./application/execution -count=1`：通过；
- `go test ./application -run 'ExecutionApplicationsAreExtractedFromCore|ExtractedApplicationPackagesDoNotImportCore|MigratedServiceMethodsRemainOneLineForwards|ApplicationArchitectureBudget' -count=1`：通过；
- `go test ./application/core -run 'Execution|Subagent|WorkItem|Teammate|TeamworkBoard|ClearSubagent' -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- core 直接内部依赖：37 → 37；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：17,890 → 17,720 行；
- `application/execution` 生产代码：58 → 252 行；
- core 生产规模预算同步收紧到 17,720 行。

### AP-100 Async Completion 包提取

后台作业与 teammate 作业终态触发迁入
[`application/execution/completion.go`](../../application/execution/completion.go)：

- `TriggerAsyncCompletions` 与 `TriggerTeamworkJobCompletions` 的扫描、幂等账与忙会话保护；
- 终态完成正文组装与证据字符上限；
- 空句柄、孤儿作业、非终态和句柄裁剪语义。

core `async_completion.go` 只保留 Runtime 作业表 / 事件宿主适配、
`sessionIdleForAsyncTrigger` 和两个一行触发入口。

删除/合并：

- `application/core/async_completion.go:asyncCompletionApplicationDeps`
  → `application/execution.CompletionDeps`
- `application/core/async_completion.go:asyncCompletionApplication`
  → `application/execution.CompletionApplication`
- 两张作业表的扫描/正文函数 → `application/execution` 唯一实现

红测：

- 扩展 `TestExecutionApplicationsAreExtractedFromCore` 后，先因
  `application/execution/completion.go` 不存在失败。

绿测：

- `go test ./application/execution -count=1`：通过；
- `go test ./application/core -run 'AsyncCompletion|AsyncCompletions|TeamworkJobCompletion|TeamworkCompletion' -count=1`：通过；
- `go test ./application -run 'ExecutionApplicationsAreExtractedFromCore|ExtractedApplicationPackagesDoNotImportCore|ApplicationArchitectureBudget' -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：17,720 → 17,469 行；
- `application/execution` 生产代码：252 → 436 行；
- core 生产规模预算同步收紧到 17,469 行。

### AP-101 File Review 包提取

文件审阅请求应用迁入 [`application/review`](../../application/review/README.md)：

- workspace 文件端口解析与请求 ID 生成；
- ctx 会话归属优先、视图会话兜底的顺序；
- 接受请求写入会话投影并发布 snapshot；
- 清空指定/当前会话请求。

逐文件 containment、敏感、二进制与去重裁决仍只在
`backend/tool/review.Requests`。core `review_files.go` 只保留宿主 adapter 和两个 Service
一行转发。

删除/合并：

- `application/core/review_files.go:reviewApplicationDeps`
  → `application/review.Deps`
- `application/core/review_files.go:reviewApplication`
  → `application/review.Application`
- `reviewApplicationHost` 及其 Host 方法 → `application/review` 内部 adapter

红测：

- `go test ./application -run TestReviewApplicationIsExtractedFromCore -count=1`
  先因 `application/review/application.go` 不存在失败。

绿测：

- `go test ./application/review -count=1`：通过；
- `go test ./application/core -run 'Review|FileReview' -count=1`：通过；
- `go test ./application -run 'ReviewApplicationIsExtractedFromCore|ExtractedApplicationPackagesDoNotImportCore|MigratedServiceMethodsRemainOneLineForwards|ApplicationArchitectureBudget' -count=1`：通过；
- `go test ./e2e -run TestFacadeDependencyClosureRatchet -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：17,469 → 17,391 行；
- `application/review` 生产代码：79 行；
- core 生产规模预算同步收紧到 17,391 行；
- facade closure 新增 `application/review`，上限 101 → 102。

### AP-102 Runtime Projection 包提取

Runtime 投影刷新迁入 [`application/admin/runtime.go`](../../application/admin/runtime.go)：

- 锁外采集 projection；
- 锁内应用并 bump revision；
- 解锁后发布 runtime.changed 与 task delta。

core `runtime_projection.go` 只保留 `serviceRuntimeProjectionPort`、构造 adapter 和
应用自有 runtime visibility/evidence 发布函数。

删除/合并：

- `application/core/runtime_projection.go:runtimeApplicationDeps`
  → `application/admin.RuntimeDeps`
- `application/core/runtime_projection.go:runtimeApplication`
  → `application/admin.RuntimeApplication`

红测：

- `go test ./application -run TestRuntimeApplicationIsExtractedFromCore -count=1`
  先因 `application/admin/runtime.go` 不存在失败。

绿测：

- `go test ./application/admin -count=1`：通过；
- `go test ./application -run 'RuntimeApplicationIsExtractedFromCore|ExtractedApplicationPackagesDoNotImportCore|MigratedServiceMethodsRemainOneLineForwards|ApplicationArchitectureBudget' -count=1`：通过；
- `go test ./application/core -run 'RuntimeApplication|RefreshRuntimeSnapshot|Scheduler' -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：17,391 → 17,382 行；
- `application/admin` 生产代码：159 → 209 行；
- core 生产规模预算同步收紧到 17,382 行。

### AP-103 Goal Application 包提取

Goal 用例路由迁入 [`application/goal`](../../application/goal/README.md)：

- `CoordinatorPort` 的五个真实方法；
- session 必填校验与协调器缺失错误；
- Begin/Update/Done 成功后 Runtime 投影刷新；
- GovernanceView/Status 只读路由。

Goal 栈状态机继续只在 `backend/multiagent/goal`；core `goal_application.go` 只保留
协调器 port 适配和构造器。

删除/合并：

- `application/core/goal_application.go:goalApplicationDeps`
  → `application/goal.Deps`
- `application/core/goal_application.go:goalApplication`
  → `application/goal.Application`
- `errGoalCoordinatorUnavailable` → `application/goal.ErrCoordinatorUnavailable`

红测：

- `go test ./application -run TestGoalApplicationIsExtractedFromCore -count=1`
  先因 `application/goal/application.go` 不存在失败。

绿测：

- `go test ./application/goal -count=1`：通过；
- `go test ./application/core -run 'Goal' -count=1`：通过；
- `go test ./application -run 'GoalApplicationIsExtractedFromCore|ExtractedApplicationPackagesDoNotImportCore|MigratedServiceMethodsRemainOneLineForwards|ApplicationArchitectureBudget' -count=1`：通过；
- `go test ./e2e -run TestFacadeDependencyClosureRatchet -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：17,382 → 17,293 行；
- `application/goal` 生产代码：118 行；
- core 生产规模预算同步收紧到 17,293 行；
- facade closure 新增 `application/goal`，上限 102 → 103。

### AP-104 Session Lifecycle 包提取

Session unload/archive/delete 的应用编排迁入
[`application/session`](../../application/session/README.md)：

- `LifecycleApplication.Unload`；
- `ArchiveApplication.Archive`；
- `DeleteApplication.Delete`。

middle/switch 与 middle/residency 继续拥有准入和归档判据；core 只保留三份宿主
adapter。删除路径的记录失败优先、内存释放失败记日志继续、工作区投影与快照发布顺序
保持不变。

删除/合并：

- `application/core/session_lifecycle_use_case.go:sessionLifecycle*`
  → `application/session.LifecycleApplication`
- `application/core/session_archive_use_case.go:sessionArchive*`
  → `application/session.ArchiveApplication`
- `application/core/session_delete_use_case.go:sessionDelete*`
  → `application/session.DeleteApplication`

红测：

- `go test ./application -run TestSessionLifecycleOperationsAreExtractedFromCore -count=1`
  先因 `application/session/lifecycle.go` 不存在失败。

绿测：

- `go test ./application/session -count=1`：通过；
- `go test ./application/core -run 'SessionLifecycle|SessionArchive|SessionDelete|UnloadSession|ArchiveSession|DeleteSession' -count=1`：通过；
- `go test ./application -run 'SessionLifecycleOperationsAreExtractedFromCore|ExtractedApplicationPackagesDoNotImportCore|MigratedServiceMethodsRemainOneLineForwards|ApplicationArchitectureBudget' -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：17,293 → 17,129 行；
- `application/session` 生产代码：370 → 553 行；
- core 生产规模预算同步收紧到 17,129 行。

### AP-105 Session Draft/Resume/Restore 包提取

四个会话选择/草稿/恢复应用组件迁入
[`application/session`](../../application/session/README.md)：

- `DraftSubmissionApplication`；
- `ResumeApplication`；
- `RestoreRecoveryApplication`；
- `RestoreDraftResetApplication`。

core 只保留 transition、draft slot、cold restore 和 publication 的 Host adapter；
`ErrDraftNotInView` 由 core 注入，保持错误身份。包内不导入 `application/core`。

删除/合并：

- `sessionDraftSubmission*` → `application/session.DraftSubmissionApplication`
- `sessionResumeState/Deps/Resume` → `application/session.ResumeState/ResumeApplication`
- `sessionRestoreRecovery*` → `application/session.RestoreRecoveryApplication`
- `sessionRestoreDraftReset*` → `application/session.RestoreDraftResetApplication`

红测：

- `go test ./application -run TestSessionLifecycleOperationsAreExtractedFromCore -count=1`
  在扩展结构门禁后先因 `application/session/draft_submission.go` 不存在失败。

绿测：

- `go test ./application/session -count=1`：通过；
- `go test ./application/core -run 'SessionDraftSubmission|SessionResume|SessionRestore' -count=1`：通过；
- `go test ./application -run 'SessionLifecycleOperationsAreExtractedFromCore|ExtractedApplicationPackagesDoNotImportCore|ApplicationArchitectureBudget' -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：17,129 → 16,959 行；
- `application/session` 生产代码：553 → 723 行；
- core 生产规模预算同步收紧到 16,959 行。

### AP-106 Session Draft Entry / Materialization 包提取

草稿入口和首请求物化迁入 [`application/session`](../../application/session/README.md)：

- `DraftEntryApplication` 的 `/new` 门控、旧会话保存、Skill/context 清理、draft shell；
- `DraftMaterializationApplication` 的 workspace 绑定、Engine 打开、标题、commit、
  runtime/snapshot 发布、catalog、tool-record flush 与 resident touch。

core 只保留 draft slot、Engine、Snapshot 和 publication 的 Host adapter。

删除/合并：

- `sessionDraftEntry*` → `application/session.DraftEntryApplication`
- `sessionDraftSlot` / state → `application/session.DraftSlot` / `DraftEntryState`
- `sessionDraftMaterialization*` → `application/session.DraftMaterializationApplication`

红测：

- 扩展 `TestSessionLifecycleOperationsAreExtractedFromCore` 后先因
  `application/session/draft_entry.go` 不存在失败。

绿测：

- `go test ./application/session -count=1`：通过；
- `go test ./application/core -run 'SessionDraftEntry|SessionDraftMaterialization' -count=1`：通过；
- `go test ./application -run 'SessionLifecycleOperationsAreExtractedFromCore|ExtractedApplicationPackagesDoNotImportCore|ApplicationArchitectureBudget' -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：16,959 → 16,821 行；
- `application/session` 生产代码：723 → 914 行；
- core 生产规模预算同步收紧到 16,821 行。

### AP-107A Cold Restore 半链包提取

冷恢复的前五个组件迁入
[`application/session/cold`](../../application/session/cold/README.md)：

- concurrent record/transcript/history read；
- transcript/provider history assembly；
- Engine resume/replace、durable queue recovery；
- workspace 绑定/保留/解绑；
- locked activation 的 epoch、active、tier 与 title。

core 只保留 session coordinator、Engine、Runtime、View 与 publication 的 Host adapter。

删除/合并：

- `sessionColdRead*` → `cold.ReadApplication`
- `sessionColdHistory*` → `cold.HistoryApplication`
- `sessionColdRestore*` → `cold.RestoreApplication`
- `sessionColdWorkspace*` → `cold.WorkspaceApplication`
- `sessionColdActivation*` → `cold.ActivationApplication`

红测：

- `go test ./application -run TestSessionColdRestoreComponentsAreExtractedFromCore -count=1`
  先因 `application/session/cold/read.go` 不存在失败。

绿测：

- `go test ./application/session/cold -count=1`：通过；
- `go test ./application/core -run 'SessionCold(Read|History|Restore|Workspace|Activation)' -count=1`：通过；
- `go test ./application -run 'SessionColdRestoreComponentsAreExtractedFromCore|ExtractedApplicationPackagesDoNotImportCore|ApplicationArchitectureBudget' -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：16,821 → 16,546 行；
- `application/session/cold` 生产代码：316 行；
- core 生产规模预算同步收紧到 16,546 行；
- facade closure 新增 `application/session/cold`，上限 103 → 104。

### AP-107B Cold Restore 后半链包提取

Cold Task、View、Commit、Publish 四个组件迁入
[`application/session/cold`](../../application/session/cold/README.md)：

- Task/Plan/checkpoint/compaction 恢复；
- record/legacy View 构造与安装；
- pending-tail、ChatState、Plan、workspace 与 revision 的锁内 commit；
- context 挂接、Task/Subagent/Runtime 发布与 resident/content LRU。

core 只保留 Task coordinator、View coordinator、Runtime 和 EventHub 的 Host adapter。

删除/合并：

- `sessionColdTask*` → `cold.TaskApplication`
- `sessionColdView*` → `cold.ViewApplication`
- `sessionColdCommit*` → `cold.CommitApplication`
- `sessionColdPublish*` → `cold.PublishApplication`

红测：

- 扩展 `TestSessionColdRestoreComponentsAreExtractedFromCore` 后先因
  `application/session/cold/view.go` 不存在失败。

绿测：

- `go test ./application/session/cold -count=1`：通过；
- `go test ./application/core -run 'SessionCold(Task|View|Commit|Publish|Read|History|Restore|Workspace|Activation)' -count=1`：通过；
- `go test ./application -run 'SessionColdRestoreComponentsAreExtractedFromCore|ExtractedApplicationPackagesDoNotImportCore|ApplicationArchitectureBudget' -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：16,546 → 16,298 行；
- `application/session/cold` 生产代码：316 → 608 行；
- core 生产规模预算同步收紧到 16,298 行。

### AP-108A Fork / Restoring Shell 包提取

Fork lineage 与 restoring shell 迁入
[`application/session`](../../application/session/README.md)：

- fork payload 的安全解码与空/损坏降级；
- restoring 状态的 Snapshot 投影、上一会话事实清理、Chat 镜像和 revision。

core `session_fork_lineage.go` / `session_restoring_shell.go` 只保留 session port 与
Snapshot Host adapter。

删除/合并：

- `sessionForkLineage*` → `application/session.ForkLineageApplication`
- `sessionRestoringShell*` → `application/session.RestoringShellApplication`

红测：

- 扩展 `TestSessionLifecycleOperationsAreExtractedFromCore` 后先因
  `application/session/fork_lineage.go` 不存在失败。

绿测：

- `go test ./application/session -count=1`：通过；
- `go test ./application/core -run 'SessionForkLineage|SessionRestoringShell' -count=1`：通过；
- `go test ./application -run 'SessionLifecycleOperationsAreExtractedFromCore|ExtractedApplicationPackagesDoNotImportCore|ApplicationArchitectureBudget' -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：16,298 → 16,220 行；
- `application/session` 生产代码：914 → 1,009 行；
- core 生产规模预算同步收紧到 16,220 行。

### AP-109 Scheduled Task 包提取

定时任务 CRUD、运行装配与触发新会话迁入
[`application/admin/scheduler`](../../application/admin/scheduler/README.md)：

- create/update/cancel 的默认参数、统一校验和 projection refresh；
- permission tier、run params、plugin carrier 的提交前装配；
- 后台新会话创建、workspace projection、revision、发布、catalog、resident touch 与提交。

core `service_scheduler.go` 只保留 workspace/plugin 校验宿主、Runtime/Session 适配器和
Service 一行转发；run-param 默认/校验委托给 scheduler 包唯一实现。

删除/合并：

- `schedulerApplication*` → `scheduler.SchedulerApplication`
- `scheduledRunApplication*` → `scheduler.RunApplication`
- `scheduledSessionApplication*` → `scheduler.SessionApplication`
- `withScheduledRunDefaults` / `scheduledRunParams` / run-param validation
  → `scheduler.WithRunDefaults` / `RunParams` / `ValidateRunParams`

红测：

- `go test ./application -run TestSchedulerApplicationsAreExtractedFromCore -count=1`
  先因 `application/admin/scheduler/application.go` 不存在失败。

绿测：

- `go test ./application/admin/scheduler -count=1`：通过；
- `go test ./application/core -run 'Scheduler|Scheduled' -count=1`：通过；
- `go test ./application -run 'SchedulerApplicationsAreExtractedFromCore|ExtractedApplicationPackagesDoNotImportCore|MigratedServiceMethodsRemainOneLineForwards|ApplicationArchitectureBudget' -count=1`：通过；
- `go test ./e2e -run TestFacadeDependencyClosureRatchet -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：16,220 → 15,931 行；
- `application/admin/scheduler` 生产代码：254 行；
- core 生产规模预算同步收紧到 15,931 行；
- facade closure 新增 `application/admin/scheduler`，上限 104 → 105。

### AP-110 Policy Switching 包提取

剩余 Policy 应用组件迁入 [`application/policy`](../../application/policy/README.md)：

- Skill activation；
- reasoning effort / loop limit / release window；
- permission tier / full access；
- effort/plugin 切换与 runtime projection 提交。

core `service_policy_application.go` 只保留 skill activator、knobs、Engine、Plugin、
Approval、Snapshot 和 Runtime 的 Host adapter。

删除/合并：

- `policyApplicationDeps` → `policy.SwitchDeps`
- `policyApplication` → `policy.SwitchApplication`
- 9 个策略切换方法体 → `application/policy` 唯一实现

红测：

- `go test ./application -run TestPolicySwitchApplicationIsExtractedFromCore -count=1`
  先因 `application/policy/switch.go` 不存在失败。

绿测：

- `go test ./application/policy -count=1`：通过；
- `go test ./application/core -run 'Policy|PermissionTier|FullAccess|SwitchEffort|SwitchPlugin|ReleaseWindow|ReasoningEffort|LoopLimit|Skill' -count=1`：通过；
- `go test ./application -run 'PolicySwitchApplicationIsExtractedFromCore|ExtractedApplicationPackagesDoNotImportCore|MigratedServiceMethodsRemainOneLineForwards|ApplicationArchitectureBudget' -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：15,931 → 15,776 行；
- `application/policy` 生产代码：412 → 591 行；
- core 生产规模预算同步收紧到 15,776 行。

### AP-111 Context Compaction 包提取

显式上下文压缩迁入
[`application/conversation/context`](../../application/conversation/context/README.md)：

- Session 解析与同会话 compression gate；
- Runtime `CompactContextNow` 委托；
- Done/Unrecorded/Failed/Scheduled/NotNeeded 回执映射；
- `compact_context` JSON 适配、note、gate checklist、range 与 failure label。

core 保留 `ContextCompactionResult` / `CompactionGateTiming` 类型别名、两个一行 Service
入口和 Runtime/Gate Host adapter，公开 API 身份不变。

删除/合并：

- `contextCompactionApplicationDeps` → `conversationcontext.Deps`
- `contextCompactionApplication` → `conversationcontext.Application`
- 压缩 outcome/回执/label 函数 → `application/conversation/context` 唯一实现

红测：

- `go test ./application -run TestContextCompactionApplicationIsExtractedFromCore -count=1`
  先因 `application/conversation/context/application.go` 不存在失败。

绿测：

- `go test ./application/conversation/context -count=1`：编译通过；
- `go test ./application/core -run 'CompactContext' -count=1`：通过；
- `go test ./application -run 'ContextCompactionApplicationIsExtractedFromCore|ExtractedApplicationPackagesDoNotImportCore|MigratedServiceMethodsRemainOneLineForwards|ApplicationArchitectureBudget' -count=1`：通过；
- `go test ./e2e -run TestFacadeDependencyClosureRatchet -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：15,776 → 15,398 行；
- `application/conversation/context` 生产代码：303 行；
- core 生产规模预算同步收紧到 15,398 行；
- facade closure 新增 `application/conversation/context`，上限 105 → 106。

### AP-112A Conversation Queue 包提取

排队输入应用编排迁入 [`application/conversation/queue.go`](../../application/conversation/queue.go)：

- `QueueUnit` 最小 SessionUnit 面；
- `ReorderQueuedInput` / `RecallQueuedInput`；
- 关闭/排空 gate、队列投影、活跃 ChatState 镜像、revision 和锁外发布。

`SessionUnit.Queue` 仍是唯一状态所有者。core `chat_queue.go` 只保留 session resolver、
ViewMu、ChatState 和 EventHub 的 Host adapter。

删除/合并：

- `conversationQueueDeps` → `conversation.QueueDeps`
- `conversationQueue` → `conversation.QueueApplication`
- `conversationQueueUnit` → `conversation.QueueUnit`

红测：

- `go test ./application -run TestConversationQueueApplicationIsExtractedFromCore -count=1`
  先因 `application/conversation/queue.go` 不存在失败。

绿测：

- `go test ./application/conversation -count=1`：编译通过；
- `go test ./application/core -run 'Queue|QueueComponent|ReorderQueuedInput|RecallQueuedInput|ChatState' -count=1`：通过；
- `go test ./application -run 'ConversationQueueApplicationIsExtractedFromCore|ExtractedApplicationPackagesDoNotImportCore|MigratedServiceMethodsRemainOneLineForwards|ApplicationArchitectureBudget' -count=1`：通过；
- `go test ./e2e -run TestFacadeDependencyClosureRatchet -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：15,398 → 15,284 行；
- `application/conversation` 根包生产代码：131 行；
- core 生产规模预算同步收紧到 15,284 行；
- facade closure 新增 `application/conversation`，上限 106 → 107。

### AP-112B ChatState Transition 包提取

ChatState 转换规则迁入
[`application/conversation/turn_state.go`](../../application/conversation/turn_state.go)：

- Begin / ProjectQueue / ClearError / SetError / Finish / Reset；
- `ChatStateUnit` 与 Queue projection 窄接口。

SessionUnit 仍是唯一状态存储。core `conversationTurnState` 仅嵌入提取后的
`TurnStateApplication`，并暂时保留 chatRequest factory，等待 AP-113 迁移。

红测：

- `go test ./application -run TestConversationTurnStateApplicationIsExtractedFromCore -count=1`
  先因 `application/conversation/turn_state.go` 不存在失败。

绿测：

- `go test ./application/conversation -count=1`：编译通过；
- `go test ./application/core -run 'ChatState|Queue' -count=1`：通过；
- `go test ./application -run 'ConversationTurnStateApplicationIsExtractedFromCore|ApplicationArchitectureBudget|ExtractedApplicationPackagesDoNotImportCore' -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：15,284 → 15,233 行；
- `application/conversation` 根包生产代码：131 → 192 行；
- core 生产规模预算同步收紧到 15,233 行。

### AP-113A ChatRequest 包提取

ChatRequest 值、Skill 过滤/合并、队列恢复和批次合成迁入
[`application/conversation/request.go`](../../application/conversation/request.go)：

- `Request` 的 display/model/skills/budget；
- `NewRequest`、`QueuedRequests`、`Combine`；
- `ChatRequestDisplays`、`QueuedInputRefs`、`SelectedSkillLayers`；
- model envelope parse/wrap 由 core Skill context 回调注入。

core 保留 `chatRequest` 类型别名和两个自由函数兼容转发；`request.displayInput`
等主链接口统一改为导出字段访问，不保留第二套 request 逻辑。

验证：

- `go test ./application/core -run 'ChatState|Queue|ChatRequest|Skill' -count=1`：通过；
- `go test ./application -run 'ConversationTurnStateApplicationIsExtractedFromCore|ApplicationArchitectureBudget|ExtractedApplicationPackagesDoNotImportCore' -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：15,233 → 15,145 行；
- `application/conversation` 根包生产代码：192 → 324 行；
- core 生产规模预算同步收紧到 15,145 行。

### AP-113B Turn Input / Response / Recovery / Cleanup 包提取

ChatTurn 前后处理组件迁入 `application/conversation`：

- `TurnInputApplication`；
- `TurnResponseApplication`；
- `TurnRecoveryApplication`；
- `TurnCleanupApplication`。

Engine 流、Context Controller、Task transcript、Runtime projection 与 EventHub 仍通过
core Host adapter 提供。直接组件测试保留并改用包类型。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：15,145 → 14,999 行；
- `application/conversation` 根包生产代码：324 → 506 行；
- core 生产规模预算同步收紧到 14,999 行。

### AP-113C ChatTurn 后半链原子提取

ChatTurn 的后半链整体迁入 `application/conversation`：

- `TurnProcessApplication` / `Promotion` / `StartOptions`；
- `TurnOutcomeApplication`；
- `TurnCompletionApplication`；
- `TurnCommitApplication` / `CommitResult`；
- `TurnFinalizeApplication`；
- `TurnPublisherApplication` / `Publication`；
- `TurnTailApplication`；
- `TurnExecutionApplication`。

core 只保留 `Service` 组件字段、Host adapter、类型别名与兼容构造函数。Persist、
outcome、queue promotion、tail、event publication、runtime projection 和 session
policy 的实际提供方仍由既有模块唯一拥有。

验证：

- `go test ./application/conversation -count=1`：编译通过；
- `go test ./application/core -run 'ChatTurn|ConversationTurn' -count=1`：通过；
- `go test ./application -run 'ChatTurnPipelineComponentsAreExtractedFromCore|ApplicationArchitectureBudget|ExtractedApplicationPackagesDoNotImportCore' -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：14,999 → 14,514 行；
- `application/conversation` 根包生产代码：506 → 1,025 行；
- core 生产规模预算同步收紧到 14,514 行。

### AP-114A Edit/Fork 组件提取

Edit/Fork 组件迁入 [`application/conversation/edit`](../../application/conversation/edit/README.md)：

- operation TTL / 幂等账；
- parent validator；
- target event / fork cut resolver；
- child fork/resume/submit/rollback process；
- visible-message editability annotation。

core `session_message_edit.go` 暂时保留顶层 `EditUserMessageAndFork` 参数校验与 operation
owner/waiter 编排，只调用提取后的组件；这避免同批改动公开入口语义。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：14,514 → 14,179 行；
- `application/conversation/edit` 生产代码：394 行；
- core 生产规模预算同步收紧到 14,179 行；
- facade closure 新增 `application/conversation/edit`，上限 107 → 108。

### AP-114B EditUserMessageAndFork 顶层编排提取

`EditApplication` 迁入 `application/conversation/edit/application.go`，集中：

- parent/NewText/OperationID/TargetEventSeq 输入校验；
- operation Begin/waiter/Finish；
- 拓扑流程委托给提取后的 Process。

core `Service.EditUserMessageAndFork` 只保留一行转发；`begin/finish` 与 resolver/validator
兼容内部方法仍为一行 adapter，供现有测试与 process 装配使用。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：14,179 → 14,160 行；
- `application/conversation/edit` 生产代码：394 → 460 行；
- core 生产规模预算同步收紧到 14,160 行。

### AP-115A Tool Hook Bridge 包提取

Seele loop hook 适配迁入
[`application/execution/toolhooks`](../../application/execution/toolhooks/README.md)：

- LLM complete、tool start/complete、iteration complete 回调编排；
- 并发工具调用的稳定唯一 ID 与 `(turn, name, arguments)` FIFO 配对；
- metadata-only 诊断观察者及其 best-effort 容错；
- session-backed 排队输入闸门与非 session-backed history repair/失败回执。

core 的 `tool_hooks.go` 只保留 `handleToolStart` /
`handleToolCompleteObserved` 等工具投影宿主，以及 `ToolHookBridge` 的
`Bind`/`Hooks`/诊断类型适配；`Bridge` 的配对状态、ID 生成和 hook 状态机不再存在
第二份实现。

删除/合并：

- `ToolHookBridge.mu/service/toolSeq/pending/diagnostic` → `toolhooks.Bridge`
- `beginTool` / `completeTool` / `nextToolIDLocked` / `toolHookKey` /
  `observeDiagnostic` → `application/execution/toolhooks`
- `application/core/tool_hooks.go` 保留的工具投影函数不变，事件顺序与 wire 不变。

红测：

- `go test ./application -run TestToolHookBridgeIsExtractedFromCore -count=1`
  在 `application/execution/toolhooks/bridge.go` 尚不存在时失败；该测试现在同时扫描
  core，禁止重复出现 `beginTool`、`completeTool`、`nextToolIDLocked`、
  `toolHookKey`、`observeDiagnostic`。

绿测：

- `go test ./application/execution/toolhooks -count=1`：通过；包含并发 FIFO 配对、
  迭代闸门各分支和诊断观察者 panic 容错用例；
- `go test ./application/core -run 'ToolHook|HandleTool|IterationComplete' -count=1`：通过；
- `go test ./application -run 'ToolHookBridgeIsExtractedFromCore|ApplicationArchitectureBudget|ExtractedApplicationPackagesDoNotImportCore' -count=1`：通过；
- `go test ./e2e -run TestFacadeDependencyClosureRatchet -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：14,160 → 14,056 行；
- `application/execution/toolhooks` 生产代码：177 行；
- core 生产规模预算同步收紧到 14,056 行；
- facade closure 新增 `application/execution/toolhooks`，上限 108 → 109。

### AP-115B Tool Record 包提取

工具记录状态机迁入
[`application/execution/toolrecord`](../../application/execution/toolrecord/README.md)：

- 每会话累计、开关槽、最近 flush 错误与快照端口；
- 叶子 stage、drain、`lastSeq` 幂等、release 回收与 dropped diagnostics；
- 成功/失败终态判据与失败原因唯一词表；
- 命令退出码结果、批量 flush、每会话懒加载和 Board 投影。

core 的 `tool_record.go` 与 `tool_record_stage.go` 只保留 Service 一行转发、
`WritableSession` 宿主判据和提交编排水位；`store/record` 继续是
`Accumulator` / `Stage` 的唯一累加实现。

删除/合并：

- `toolRecordAccumulators` / `toolRecordStages` → `toolrecord.Application`；
- `toolRecordPort` 的开关、错误和 snapshotStore 槽位 → 同一 Application；
- `toolStatusFailed` / `toolRecordReason` / `containsAny` → `toolrecord.Failed` /
  `toolrecord.Reason`；
- `subagentToolRecordUnitID` / `roleToolRecordUnitID` → `toolrecord.SubagentUnitID` /
  `toolrecord.RoleUnitID`；
- `loadToolRecord` / `flushAllToolRecords` / `releaseToolRecordSession` 状态机
  → `Load`/`FlushAll`/`ReleaseSession`。

红测：

- `go test ./application -run TestToolRecordApplicationIsExtractedFromCore -count=1`
  先因 `application/execution/toolrecord/application.go` 不存在失败。

绿测：

- `go test ./application/execution/toolrecord -count=1`：通过；
- `go test ./application/core -run 'ToolRecord|ToolCommand|RoleTool|SubagentTool' -count=1`：通过；
- `go test ./application/core -count=1`：通过；
- `go test ./application -run 'ToolRecord|ApplicationArchitectureBudget|ExtractedApplicationPackagesDoNotImportCore' -count=1`：通过；
- `go test ./e2e -run TestFacadeDependencyClosureRatchet -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：14,056 → 13,402 行；
- `application/core` 直接内部依赖：35 → 34（`store/record` 退出 core 导入面）；
- `application/execution/toolrecord` 生产代码：525 行；
- core 生产规模预算同步收紧到 13,402 行；
- facade closure 新增 `application/execution/toolrecord`，上限 109 → 110。

### AP-115C1 WorkTable Publisher 包提取

`worktable.changed` 的 publisher 应用状态迁入
[`application/execution/table`](../../application/execution/table/README.md)：

- latest-wins CSP 汇聚与关闭尾态；
- 子代理树 payload 签名去重；
- 表格/批次与树变化后的 `WorkTableEvent` 组装。

core 的 `publishWorkTable` 只调用 `Application.Send`；assembler 注入树快照和
EventHub 发布回调，`CloseWorkTable` 只调用 `Application.Close`。
WorkItem 行、批次、打点与异步行判据仍归 `middle/view/worktable`，没有复制。

删除/合并：

- `workTablePublisher` 与 `lastPublishedSubagentTreeSig` 字段 →
  `table.Application`；
- `workTableEventPayload` 的树签名/空树/items 组装 → `table.Application`；
- core 保留锁内树克隆、session ID 读取与事件发布三个 host 动作。

红测：

- `go test ./application -run TestWorkTablePublisherIsExtractedFromCore -count=1`
  先因 `application/execution/table/application.go` 不存在失败。

绿测：

- `go test ./application/execution/table -count=1`：通过；
- `go test ./application/core -run 'WorkTable|RefreshWorkTable|Todo|Shutdown|Fault' -count=1`：通过；
- `go test ./application/core -run 'WorkTable' -count=1`：通过；
- `go test ./application -run TestWorkTablePublisherIsExtractedFromCore -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：13,402 → 13,376 行；
- `application/execution/table` 生产代码：64 行；
- core 生产规模预算同步收紧到 13,376 行；
- facade closure 新增 `application/execution/table`，上限 110 → 111。

### AP-115C2a WorkTable TraceBlock 输入投影提取

请求尾部 WorkTable TraceBlock 的输入投影迁入同一
[`application/execution/table`](../../application/execution/table/README.md)：

- 未终态 task 过滤、稳定 ID 排序与 retry 文案；
- async/teamwork 行装配与读法提示；
- 整块 30 行预算、截断提示与显式空块；
- session/view 作用域归一。

core 的 `workTableTraceBlockFor` 只负责读取 view session、按会话取 task 记录、
读取 async/teamwork 运行投影并传递 limits；marker 常量保留为兼容 alias。

删除/合并：

- `workTableTraceMaxLines` → `table.TraceMaxLines`；
- TraceBlock 行过滤/排序/预算/hint 逻辑 → `table.BuildTraceBlock`；
- 现有 async/teamwork/task 行为测试继续钉住 core wrapper，包内新增纯输入测试。

红测：

- `go test ./application -run TestWorkTablePublisherIsExtractedFromCore -count=1`
  先因 `application/execution/table/trace.go` 不存在失败。

绿测：

- `go test ./application/execution/table -count=1`：通过；
- `go test ./application/core -run 'WorkTableTrace|WorkTableAsync|WorkTable' -count=1`：通过；
- `go test ./application -run TestWorkTablePublisherIsExtractedFromCore -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：13,376 → 13,309 行；
- `application/execution/table` 生产代码：64 → 164 行；
- core 生产规模预算同步收紧到 13,309 行。

### AP-115C2b1 async/teamwork consumer 提取

async 与 teamwork 的后台作业生命周期 consumer 迁入
[`application/execution/table`](../../application/execution/table/README.md)：

- 单消费者同时等待两个容量 1 信号口；
- pending completion 的 500ms 补扫；
- refresh 与 async/teamwork trigger 的 safe-call 顺序；
- stop 时停止 timer 并退出，不泄漏 goroutine。

core 的 `consumeAsyncRuns` 只注入通道、refresh、completion trigger、safe-call
和 stop 信号；`startLifecycleConsumers` 仍拥有启动时机。

红测：

- `go test ./application -run TestWorkTablePublisherIsExtractedFromCore -count=1`
  先因 `application/execution/table/consumer.go` 不存在失败。

绿测：

- `go test ./application/execution/table -count=1`：通过，包含 signal dispatch、retry
  和无信号返回用例；
- `go test ./application/core -run 'WorkTable|Async|Teamwork' -count=1`：通过；
- `go test ./application -run TestWorkTablePublisherIsExtractedFromCore -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：13,309 → 13,232 行；
- `application/execution/table` 生产代码：164 → 274 行；
- core 生产规模预算同步收紧到 13,232 行。

### AP-115C2b2 subagent/task consumer 提取

subagent tree 与 task change 的 consumer 迁入
[`application/execution/table/lifecycle.go`](../../application/execution/table/lifecycle.go)：

- subagent signal → refresh callback；
- task change → task context 采样与 `publishTaskChanged`；
- `sync.Once` 保证三路 consumer 只启动一次；
- stop 信号统一结束 subagent/task/async 三类 goroutine。

core 的 `startLifecycleConsumers` 只构造 channels、refresh/publish/context/safe-call
回调并调用 `Lifecycle.Start`。

红测：

- `go test ./application -run TestWorkTablePublisherIsExtractedFromCore -count=1`
  先因 `application/execution/table/lifecycle.go` 不存在失败。

绿测：

- `go test ./application/execution/table -count=1`：通过，包含 dispatch 与 start-once；
- `go test ./application/core -run 'WorkTable|Async|Teamwork|Lifecycle|Shutdown' -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：13,232 → 13,173 行；
- `application/execution/table` 生产代码：274 → 373 行；
- core 生产规模预算同步收紧到 13,173 行。

### AP-115C2c WorkItem/批次输入投影提取

WorkItem 行、批次与 task-change 依赖补齐进入
[`application/execution/table/projection.go`](../../application/execution/table/projection.go)：

- `BuildRows` 统一去重 Plan/Task/SubAgent/Async 输入并返回行与批次；
- `WorkItemForRecord` 统一补齐 plan adjacency 依赖；
- 实际行、批次、截断和身份算法仍在 `middle/view/worktable`。

红测：

- `go test ./application -run TestWorkTablePublisherIsExtractedFromCore -count=1`
  先因 `application/execution/table/projection.go` 不存在失败。

绿测：

- `go test ./application/execution/table -count=1`：通过；
- `go test ./application/core -run 'WorkTable|Task|Plan' -count=1`：通过；
- `go test ./application -run TestWorkTablePublisherIsExtractedFromCore -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：13,173 → 13,165 行；
- `application/execution/table` 生产代码：373 → 414 行；
- core 生产规模预算同步收紧到 13,165 行。

### AP-115D1 基础内置命令包提取

help、clear、model、history、trace、sessions、diag、exit 迁入
[`application/input/command`](../../application/input/command/README.md)：

- `contract.Command` / `CommandResult` 注册契约；
- 历史角色统计、会话列表、诊断文本与 sigil 帮助；
- clear 通过宿主 callback 调用既有 reset；
- 重复注册错误保持 `register command ...` 包装。

core 只保留 compact/resume/fork/plugin/effort/permission 等会话 mutation 命令；
`backend/engine/input` 的 CommandRegistry 与 Router 未改。

红测：

- `go test ./application -run TestBuiltinCommandApplicationIsExtractedFromCore -count=1`
  先因 `application/input/command/command.go` 不存在失败。

绿测：

- `go test ./application/input/command -count=1`：通过；
- `go test ./application/input/command ./application/core -run 'Command|Suggestions|Input' -count=1`：通过；
- `go test ./application -run 'BuiltinCommandApplicationIsExtractedFromCore|ApplicationArchitectureBudget|ExtractedApplicationPackagesDoNotImportCore' -count=1`：通过；
- `go test ./e2e -run TestFacadeDependencyClosureRatchet -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：13,165 → 13,116 行；
- `application/input/command` 生产代码：177 行；
- core 生产规模预算同步收紧到 13,116 行；
- facade closure 新增 `application/input/command`，上限 111 → 112。

### AP-115D2 mutation 命令编排包提取

剩余 mutation 命令迁入 [`application/input/command`](../../application/input/command/README.md)：

- compact/new/resume/fork/pool；
- plugins/plugin、effort、permission；
- 参数裁剪、能力判断和 `contract.CommandResult` 组装；
- 具体执行仍通过 `MutationDeps` 回调进入既有 Application/Session/Policy 用例。

core 的 `command.go` 只保留 host callbacks 装配；命令 Router、权限与 Skill/Plugin
能力判定仍由既有模块拥有。

绿测：

- `go test ./application/input/command -count=1`：通过，包含 mutation 分支；
- `go test ./application/core -run 'Command|Suggestions|Input|ContextCompact|Permission' -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：13,116 → 13,022 行；
- `application/input/command` 生产代码：177 → 395 行；
- core 生产规模预算同步收紧到 13,022 行。

### AP-108B1 Session Runtime 只读投影提取

SessionSnapshot 的 Runtime 字段归属与深拷贝进入
[`application/session/query/runtime.go`](../../application/session/query/runtime.go)：

- 只保留 Effort/FullAccess/Permission/Tokens/Replan/Plan/Todo/Subagent/Goal/Skill/
  WorkTable 等会话字段；
- 去除 model/provider/plugin/account 等进程级字段；
- Plan、Goal 历史、ActiveSkills、WorkTable 与 batches 均断开共享；
- `SnapshotOf` 的冷/热读取主链与锁序保持不变。

红测：

- `go test ./application -run TestSessionScopeRuntimeProjectionIsExtractedFromCore -count=1`
  先因 `application/session/query/runtime.go` 不存在失败。

绿测：

- `go test ./application/session/query -count=1`：通过；
- `go test ./application/session/query ./application/core -run 'SessionScope|Snapshot|SessionRuntime|Goal' -count=1`：通过；
- `go test ./application -run TestSessionScopeRuntimeProjectionIsExtractedFromCore -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：13,022 → 12,991 行；
- `application/session/query` 生产代码：135 行；
- core 生产规模预算同步收紧到 12,991 行。

### AP-108B2a Snapshot 冷/热路径选择提取

Snapshot 冷读、驻留和内容 LRU 回退选择进入
[`application/session/query/snapshot_read.go`](../../application/session/query/snapshot_read.go)：

- 空 session ID 拒绝；
- 未驻留 → 持久化冷读；
- 非视图会话且内容已卸载 → 冷读成功则返回，失败才回退驻留内存；
- 其它情况走驻留 Snapshot builder。

core 的 `readSnapshotOf` 只注入 ViewMu 状态采样和两个既有 builder。

红测：

- `go test ./application -run TestSessionScopeRuntimeProjectionIsExtractedFromCore -count=1`
  先因 `application/session/query/snapshot_read.go` 不存在失败。

绿测：

- `go test ./application/session/query -count=1`：通过；
- `go test ./application/session/query ./application/core -run 'Snapshot|SessionScope|SessionRuntime' -count=1`：通过。

结构指标：

- core 生产代码：12,861 → 12,854 行；
- `application/session/query` 生产代码：135 → 176 行；
- core 生产规模预算同步收紧到 12,854 行。

### AP-108B2b 驻留 Snapshot 组装提取

驻留 Snapshot 字段组装进入
[`application/session/query/resident_snapshot.go`](../../application/session/query/resident_snapshot.go)：

- Session/Conversation/Chat/HistoryWindow/ReadFiles/ReviewRequest 的防御性拷贝；
- Conversation edit annotation callback；
- Plan、Task context-compactions 深拷贝；
- task records + async runs 的 WorkTable row/batch enrichment。

core 的 `snapshotOfResident` 只做锁外 host sampling、锁内 View/Unit 读取和纯 builder
调用，不改变锁序。

绿测：

- `go test ./application/session/query -count=1`：通过；
- `go test ./application/session/query ./application/core -run 'Snapshot|SessionScope|SessionRuntime' -count=1`：通过。

结构指标：

- core 生产代码：12,854 → 12,837 行；
- `application/session/query` 生产代码：176 → 251 行；
- core 生产规模预算同步收紧到 12,837 行。

### AP-113D1 Chat Reasoning 投影提取

`chat.go` 的 assistant reasoning 投影进入
[`application/conversation/reasoning.go`](../../application/conversation/reasoning.go)：

- `ReasoningStepsFromHistory` 提取带推理的 assistant 步骤；
- 工具调用 ID 集合与归一化正文的 step matching；
- `message.delta` 生成与 legacy last-reasoning fallback。

core 的 `attachLatestReasoning` 只读取 history/view 并调用该包；
`runChat` Engine 主循环、队列提升和持久化顺序不变。

红测：

- `go test ./application -run TestChatReasoningProjectionIsExtractedFromCore -count=1`
  先因 `application/conversation/reasoning.go` 不存在失败。

绿测：

- `go test ./application/conversation -count=1`：通过；
- `go test ./application/conversation ./application/core -run 'Reasoning|Chat|SessionBacked' -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：12,991 → 12,865 行；
- `application/conversation` 生产代码：1,155 行；
- core 生产规模预算同步收紧到 12,865 行。

### AP-113D2 ChatTurn Runner 提取

`runChat` 的 application 组件链编排迁入
[`application/conversation/turn_runner.go`](../../application/conversation/turn_runner.go)：

- session context 注入与 draft snapshot 生命周期；
- execution prepare、ReAct budget 清理、stream batcher；
- input → stream → response → recovery → cleanup → commit → finalize 顺序。

core 的 `runChat` 只剩一条 runner 转发；Engine ReAct loop 本身、SessionUnit 状态所有权
和持久化实现未改。

红测：

- `go test ./application -run TestChatTurnRunnerIsExtractedFromCore -count=1`
  先因 `application/conversation/turn_runner.go` 不存在失败。

绿测：

- `go test ./application/conversation -count=1`：通过；
- `go test ./application/conversation ./application/core -run 'Chat|Reasoning|Turn|SessionBacked|Tail' -count=1`：通过；
- `go test ./application/core -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 171；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：12,865 → 12,861 行；
- core 生产规模预算同步收紧到 12,861 行。

### AP-116A 零外部调用方法收回

通过全仓调用方审计，确认以下方法只在 core 内部或 core 测试使用：

- `StageUnitToolOutcome`
- `CommitUnitToolRecord`
- `CloseUnitToolRecord`
- `CloseSubagentToolRecordUnit`
- `ToolRecordDroppedReadings`
- `RestoreRoleAccounts`
- `RestoreRolePermissions`

已改为包内方法，并同步 `service_methods.txt`、`service_method_count` 和 forwarding
allowlist。`CloseRoleToolRecordUnit` 仍由 wiring 使用，继续保留公开。

红测：

- `go test ./application -run TestRetiredServiceMethodsAreNotPublic -count=1`
  先列出 7 个仍公开的方法。

绿测：

- `go test ./application -run 'RetiredServiceMethodsAreNotPublic|ApplicationArchitectureBudget|Forwarding' -count=1`：通过；
- `go test ./application/core -count=1`：通过。

结构指标：

- `Service` 公开方法：171 → 164；
- core 直接内部依赖：34 → 34；
- `service_methods.txt` 与 `application_budget.json` 已同步。

### AP-116B Goal/State 内部入口收回

继续审计后收回：

- `ActiveSkillIDs`（无生产调用方）；
- `GoalBegin` / `GoalUpdate` / `GoalDone`（工具 handler 内部路径）；
- `HandleSubagentToolEvent`（生产使用 `HandleSubagentToolEventFor`）；
- `RefreshWorkTableSnapshot`（CSP lifecycle 内部回调）。

headless 继续使用 `Goal*For`，wiring 继续使用 `Goal*Handler`、
`HandleSubagentToolEventFor` 和 ToolHook 入口。`TeamworkBoardViewFor` 因中间层 view port
仍要求该结构方法而保留。

绿测：

- `go test ./application -run 'RetiredServiceMethodsAreNotPublic|ApplicationArchitectureBudget|Forwarding' -count=1`：通过；
- `go test ./application/core -count=1`：通过。

结构指标：

- `Service` 公开方法：164 → 158；
- core 生产代码：12,837 → 12,823 行；
- e2e face 清单、`service_methods.txt` 与预算同步。

### AP-116C Teamwork read-port adapter

`TeamworkBoardViewFor` 不再直接暴露在 `Service` 上；core 内部
`teamworkBoardViewPort` 满足 `middle/view/state` 的结构接口，并转发到
`application/execution` 的只读团队看板组件。ViewFace、TeamworkQueryAPI 与
ExecutionQueryAPI 的组合面同步收窄。

绿测：

- `go test ./application ./application/core ./e2e -run 'RetiredServiceMethodsAreNotPublic|ApplicationArchitectureBudget|SmallApplicationAPIs|Facade|TeamworkBoard' -count=1`：通过。

结构指标：

- `Service` 公开方法：158 → 157；
- core 生产代码保持 12,823 行。

### AP-116D ClearReviewRequest 收回

`ClearReviewRequest` 没有 GUI/TUI/wiring 生产调用，core 的 workspace host 已改为
直接调用 `reviewApplicationComponent().ClearReviewRequest("")`；Service 方法、
ViewFace assembly 清单和 service-method 预算同步删除。`RequestFileReview` 仍被
backend review port 结构满足，继续保留。

绿测：

- `go test ./application ./application/core ./e2e -run 'RetiredServiceMethodsAreNotPublic|ApplicationArchitectureBudget|Review|Facade|Forwarding' -count=1`：通过。

结构指标：

- `Service` 公开方法：157 → 156；
- core 生产代码：12,823 → 12,818 行。

### AP-116E Frontend 迁移与首批 Service 退场

本批先迁移依赖方，再删除旧 `Service` 公开方法，避免先删入口造成 GUI/TUI
编译中断：

- 新增 `core.Frontend` 过渡适配器，并由 `wiring.NewFrontend` 统一构造；
- GUI Bridge、TUI、headless、wiring bootstrap 和 e2e harness 改用
  `*application.Frontend`；
- `Suggestions`、`SelectAccount`、`SwitchEffort`、`SwitchPlugin`、
  `LoadMoreHistory`、`LoadLatestHistory`、`ReorderQueuedInput`、
  `RecallQueuedInput` 的公开签名从 `Service` 移到 `Frontend`；
- `Frontend` 直接调用唯一领域组件；`Service` 内层兼容壳在调用方归零后删除，
  没有保留第二份状态机；
- core 测试通过测试专用 `frontendOfTestService` 包装，不向生产 `Service`
  恢复兼容方法。

红测：

- `go test ./application ./application/core -count=1` 首先因
  `query_faces_test.go` 仍把 `*Service` 断言为 `ConversationHistoryQuery` 失败；
- 随后列出 16 个 core 测试文件仍调用 8 个迁移前公开入口；
- 删除依赖方后，`completion_test.go` 与 `service_queue_component_test.go`
  继续钉住旧 receiver/转发链，按预期转红。

绿测：

- `go test ./application/... ./contract/... ./e2e -count=1`：通过；
- `go test ./gui ./tui ./headless ./wiring/... -count=1`：通过；
- `go test -race ./application/... -count=1`：通过；
- `go build ./...`、`go vet ./...`：通过；
- `python scripts/check_readme_refs.py --strict`：209 个 README、0 个失效引用；
- `gofmt -l application contract e2e gui tui headless wiring`：无输出；
- `git diff --check`：通过（仅 CRLF 转 LF 提示）；
- 全仓检索 8 个入口的 core 测试直接 `service.<method>` 调用为空；
- `git status --short` 保留既有工作树改动，未执行 reset/clean/checkout。

前端测试为 874 项中 873 项通过；唯一失败仍是既有的
`account-window.test.mjs` 共享 DTO 事实源断言，与本批依赖迁移无关。

结构指标：

- `Service` 公开方法：156 → 148；
- `application/core` 直接内部依赖：34 → 34；
- `application/core` 生产文件：125 → 125；
- `application/core` 生产代码：12,818 → 12,810 行；
- `Frontend` 增加 8 个同签名兼容入口，但删除 8 个 `Service` 内层壳后
  core 仍净减 8 行。

### AP-116F1 纯转发壳迁出 core

后续 34 个直接内部依赖的拆除设计见
[`port-migration-plan.md`](port-migration-plan.md)。

AST 审计确认 148 个 Service 公开方法中有 125 个是单行组件转发。本批先把
账号、角色、RoleSessions 与 Workspace 的 50 个纯转发入口迁入
`application/compat`：

- 账号：`AddAccount`、`AccountDetails`、`UpdateAccount`、`DeleteAccount`、
  `AccountBalance`、`SetAccountWindow`；
- 角色权限 12 个、角色会话/调度 14 个；
- Workspace 环境、Git 与文件用例 18 个。

同时把 AP-116E 的 8 个入口也收口到 `application/compat`。core 现在只提供
`NewFrontendPorts` 装配快照，不再保存这些公开兼容方法。

删除文件：

- `application/core/service_account_window.go`；
- `application/core/workspace_environments.go`；
- `application/core/workspace_gitrepos.go`。

删减代码：

- `service_account.go` 只保留宿主组件读取；
- `role_permissions.go` 只保留内部 restore 用例与组件构造；
- `role_session.go` 只保留 `team.RoleSessions` 的端口解析；
- `workspace_usecase.go` 只保留 workspace host adapter 与 projection port。

结构指标：

- `Service` 公开方法：148 → 98；
- `application/core` 生产文件：125 → 123；
- `application/core` 生产代码：12,810 → 12,538 行；
- `application/core` 直接内部依赖：34 → 34。

仍未迁移的 98 个方法中，75 个仍是单行转发壳，23 个包含真实状态编排或
host callback；后续必须继续先迁兼容壳，不能把 `compat` 变成新的 core。

### AP-116F2a 只读查询壳与 History 端口

新增 `application/history.WindowAssembler`，由 `middle/switch/history`
提供唯一 adapter。`application/core/session_history.go` 不再直接 import
`middle/switch/history`，窗口行身份、durable 行和接缝判据仍只在
`middle/switch/history`。

同时把 15 个只读/查询入口迁入 `application/compat`：

- 会话：`ListSessions`、`WaitCatalogRefresh`、`GetSessionTranscript`、
  `SessionInputIndex`、`SnapshotOf`、`SessionMeta`；
- 执行投影：`WorkTableRowsFor`、`ProjectRootFor`、
  `TeammateSessionLiveFor`、`TeammateSessionLivePageFor`、
  `WorkItemExecutionFor`、`TeammateInspectionFor`；
- 工具/观测：`PerfStats`、`SearchHistory`、`ToolResultContent`。

wiring、GUI、TUI 与 test harness 已通过 compat 或窄组件入口读取，不恢复
`Service` 同名方法。

结构指标：

- `Service` 公开方法：98 → 83；
- core 直接内部依赖：34 → 33（`middle/switch/history` 退出）；
- core 生产文件：123 → 121；
- core 生产代码：12,538 → 12,463 行。

### AP-116F2b Context/Reference/Review/Goal/Scheduler/Lifecycle 壳迁移

新增 compat 端口快照：ContextCompaction、Goal、Review、Scheduler、
ScheduledRun、ScheduledSession、Lifecycle、Runtime，并把 19 个单行入口从
`Service` 移除：

- Context/Reference/Review 6 个；
- Goal 状态机公开入口 5 个；
- Scheduler/Runtime 6 个；
- Lifecycle 查询与等待 2 个。

内部调用改为直接调用唯一组件：命令压缩、goal handler、scheduler observer、
subagent refresh 和 view 的 goal governance adapter 都只保留一个入口。

结构指标：

- `Service` 公开方法：83 → 64；
- core 直接内部依赖：33 → 32；
- core 生产文件：121 → 121；
- core 生产代码：12,463 → 12,387 行。

### AP-116F2c Session/Subagent 兼容面迁移

把 16 个公开入口迁入 `application/compat`：session archive/draft/unload/meta、
edit/fork、Session SSH、Skill 激活、WorkItem 状态以及子代理恢复/详情/清空。

内部命令、environment trace、edit rollback、subagent refresh 全部改为直接调用
唯一组件，不恢复 `Service` 公共壳。

结构指标：

- `Service` 公开方法：64 → 48；
- core 直接内部依赖：32 → 28；
- core 生产代码：12,387 → 12,335 行。

### AP-116F2d Policy/Lifecycle/State 兼容面迁移

把 14 个公开入口迁入 compat：history search handler、生命周期
begin/cancel/shutdown、Effort/Loop/Release/Permission、process Subscribe、
GoalSkillActive 与 subagent tool-event 投影。

`SetFullAccess` 的 tier 回调改为调用同一个 `SwitchApplication.SetPermissionTier`
组件，不经过已删除的 Service 壳。

结构指标：

- `Service` 公开方法：48 → 34；
- core 直接内部依赖：28 → 28；
- core 生产文件：121 → 121；
- core 生产代码：12,335 → 12,302 行。

### AP-116F2e ProjectBoard 与 SessionResume 收口

- `ProjectBoardTeamPlanFor` / `ProjectBoardTeamStateFor` 不再出现在 Service；
  project-board 装配直接读取 `core.NewFrontendPorts(app).ProjectBoard`；
- `ResumeSession` / `ActivateSession` 迁入 `application/compat`，直接调用
  `application/session.ResumeApplication`；
- 核心私有 `resumeSession` 仍是内部状态链，不复制恢复逻辑；
- GUI、TUI、console、scheduled 与 e2e 全部改走 Frontend 或组件端口。

结构指标：

- `Service` 公开方法：34 → 30；
- core 直接内部依赖：25 → 23（并行外部端口批次同时收口 Input/Prompt/Approval 等）；
- core 生产文件：121 → 121；
- core 生产代码：12,302 → 12,282 行。

### AP-116F2f Goal handlers 与最后 assembly 壳

- Goal JSON handler 的解析、`finish|abort` 词表校验和 JSON 回执迁入
  `application/goal.Application`，通过 `WriteSessionID` / `ReadSessionID`
  两个纯回调解析目标会话；
- `wiring/registry.RegisterGoalTools` 改为接收 Goal handler 窄接口，不再接
  `*application.Service`；
- `ProjectBoardTeamPlanFor` / `ProjectBoardTeamStateFor` 从 Service 删除，
  bootstrap 和测试直接使用 `FrontendPorts.ProjectBoard`；
- `ResumeSession` / `ActivateSession` 由 compat 直接调用 SessionResume；
- goal coordinator 宿主实现同步收口到外部端口，删除 core coordinator 文件。

结构指标：

- `Service` 公开方法：30 → 26；
- core 直接内部依赖：19 → 18；
- core 生产文件：119 → 118；
- core 生产代码：12,136 → 11,846 行。

### AP-116F2g 状态 Host 回调外置

最后 3 个公开转发壳迁入 compat，实际状态动作仍由 core 持有：

- `AddNotice → FrontendPorts.AddNotice → service.addNotice`；
- `PublishRuntimeProjections → FrontendPorts.PublishRuntime`；
- `CloseRoleToolRecordUnit → FrontendPorts.CloseRoleToolRecord`。

结构门禁 `TestServiceHasNoCompatibilityForwarders` 改为扫描所有导出的
`Service` 方法；core 中不得再存在新的单行公开兼容转发。

结构指标：

- `Service` 公开方法：26 → 23；
- core 直接内部依赖：18 → 18；
- core 生产文件：118 → 118；
- core 生产代码：11,846 → 11,843 行。

## 跨会话阻塞项

- AP-003 的 DTO 迁移已完成；后续若新接口需要改 `contract/**`，仍须登记契约、
  消费方、wire 兼容策略与 Backend/Middle 责任批次。
- 前端 `account-window.test.mjs` 仍要求
  `backend/limit/accountwindow/accountwindow.go` 内联挡位表；并行 Backend/Middle
  改动已把唯一事实源迁到 `contract/dto/account_window.go`，需要共享 DTO 会话同步
  更新该断言。当前前端测试 874 项中 873 项通过、1 项因此失败。
- EP-002B：`middle/switch/sessionusecase` 仍要求完整 `SessionUnit`/`View`、
  Domain actor、Chat/Queue/Approval/Residency/Runtime slot；直接端口化会镜像完整会话状态。
  需要裁决会话聚合终态归属后才能继续。
- Store 端口：剩余直接用点跨 compressed turn、edit/fork event、project board plan、
  goal fifth-stack、queue recovery 五类 payload。必须先决定哪些 store payload 升为
  `contract` DTO、哪些由窄 repository port 映射；本轮不以 Application alias 掩盖具体
  `store.*` 类型。

## 本轮验证记录

已通过：

- `go test ./... -count=1 -timeout=120s`
- `go test ./application/... ./contract/... -count=1`
- `go test ./backend/... ./middle/... -count=1`
- `go test ./e2e -run 'Facade|Document' -count=1`
- `go test ./test -count=1`
- `go test -race ./backend/multiagent/teamwork ./backend/engine/assemble ./middle/view/... -count=1`
- `go vet ./...`
- `go build ./...`
- `go build -tags "gui,desktop,production" ./...`
- `python scripts/check_readme_refs.py --strict`（216 个 README、0 个失效引用）
- `gofmt -l application middle contract wiring test`（无输出）
- `git diff --check`（通过）

跨会话环境项：

- `node --test gui/frontend/dist/*.test.mjs`：874 项中 873 项通过，唯一失败仍是
  `account-window.test.mjs` 的 Backend/共享 DTO 事实源迁移不同步。
- `python scripts/gen_core_readme_index.py --only application/core` 当前仍拒绝刷新：
  `conversation_session.go`、`edit_fork_*`、`environment_context.go`、
  `workitem_execution.go` 等既有工作树文件尚未登记到 `ROOT_GROUPS`。本轮新增文件
  使用 `service_` 前缀避免增加新的未归属项，并在 `README-service.md` 手工补齐索引。

## EP-001：History Window 外部端口

设计门禁见
[`history-window-front-review.md`](history-window-front-review.md) 与
[`history-window-plan.md`](history-window-plan.md)。

- 新端口：`application/history.WindowAssembler`，六方法；比提示词多出的
  `ContainsSameRow` / `HasRowIdentity` 来自真实冷读扩窗调用，避免 core 复制行身份判据；
- 唯一实现：`middle/switch/history.Assembler`，逐项转发既有纯函数；
- 注入：`wiring/adapters.NewHistoryWindowAssembler` 返回结构满足端口的 Middle 实现，
  `wiring.New` 传给 `core.New`；
- core：删除 `middle/switch/history` 直接 import，锁内安装与锁外冷读顺序不变；
- 删除重复：core 内两处 `DurableConversationCount(view.Conversation)` 改用端口的
  `len(DurableRows(...))`，行身份/计行判据继续只在 Middle 一份；
- 红测：`TestCoreDoesNotImportHistoryWindowPackage` 初始报
  `session_history.go` 仍 import；`Assembler` 一致性测试初始因类型不存在编译失败；
- 绿测：core import 门禁、Middle assembler parity、Extension history/core 分页回归。

本批直接内部依赖 **34 → 33**；core 生产文件 121、生产代码 12,463 行（工作树并行
提取后的当前值）。GUI/TUI wire、事件 revision/seq、持久化格式均未改变。

## EP-002A：Residency 外部端口

设计门禁与停止条件见
[`session-residency-front-review.md`](session-residency-front-review.md) 与
[`session-residency-plan.md`](session-residency-plan.md)。

- 新端口：`application/session.ResidencyPort`；
- 纯值 deps：`EvictionSnapshot/Deps`、`ArchiveGate/Store/Engine/UseCaseDeps`；
- LRU 序在端口边界使用 `[]string`，`middle/residency` 仍是 Order、Budget、
  Prune、Eviction、InputIndex、Archive 的唯一判据；
- archive not-found identity 下沉 `contract.ErrSessionNotFoundArchive`；
- 唯一 adapter：`wiring/adapters.NewResidencyPort`；
- core 的 9 个 residency import 文件全部改为端口调用，GUI/TUI wire 与持久化不变。

EP-002B（SessionUseCase）在设计门禁停止：`SessionUnit`/`View` 同时承载 Chat、
Queue、审批、驻留、Runtime、窗口锁与 Domain actor；直接端口化会镜像完整 session state，
违反停止条件。候选项见设计报告，等待聚合归属裁决。

## EP-003：Conversation Stream 外部端口

- 新端口：`application/conversation.StreamOutputPort`；
- 纯值类型：`VisibleOutput`、`DeltaBatcher`、`StreamBatcherOptions`；
- 唯一实现：`wiring/adapters.NewConversationStreamPort` 转发
  `backend/engine/react`；
- core 的 `chat.go` 与 `session_history.go` 不再 import engine/react；
- think-block 过滤、可见输出流和 batcher 生命周期仍只有 Engine 一份实现；
- GUI/TUI wire、事件 revision/seq 与持久化格式不变。

## EP-004：Account Window 外部端口

- 新端口：`application/admin.AccountWindowPolicy`；
- 唯一实现：`wiring/adapters.NewAccountWindowPolicy` 转发
  `backend/limit/accountwindow.Setter`；
- core 不再 import `backend/limit/accountwindow`；
- 窗口校验、先写盘后改运行面、错误文案与生效值仍是 Middle 唯一实现；
- 本次增加少量构造注入代码，core 行数因此小幅上升，但直接内部依赖从 31 降到 30。

## EP-005：Skill Activation 外部端口

- 新端口：`application/policy.SkillActivationPort`；
- 唯一实现：`wiring/adapters.NewSkillActivationPort` 转发
  `backend/engine/skill.Activator`；
- core 不再 import `backend/engine/skill`；
- 空名、`goal` 禁激活、未知技能错误与落地动作顺序保持原实现；
- 直接内部依赖从 30 降到 29。

## EP-006：Session Records 外部端口

- 新端口：`application/conversation.SessionRecordsPort`；
- 消费面：主会话 `ConversationView` 的 record 分页；
- 唯一实现：`wiring/adapters.NewSessionRecordsPort` 逐项转发
  `middle/view/sessionaccess.PaginateRecords`；
- core 不再 import `middle/view/sessionaccess`，分页边界与 UTF-8 裁剪判据仍只有
  Middle 一份实现；
- 红测：`TestCoreDoesNotImportSessionAccessView` 在迁移前点名
  `conversation_session.go`；
- adapter parity：`TestSessionRecordsPortMatchesMiddlePaginator` 用同一输入逐字段
  比较端口与 Middle 原函数输出；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：恢复 core 直接 import 与对 `PaginateRecords` 的调用，删除端口注入字段。

## EP-007：Error Presentation 外部端口

- 新端口：`contract.ErrorPresentationPort`，只含 `Wrap` 与 `PresentToolError` 两个纯函数面；
- 消费方：Chat 的 ReAct 预算错误包装与 Tool Hook 的错误呈现；
- 唯一实现：`wiring/adapters.NewErrorPresentationPort` 转发 `middle/view/errors`；
- core 不再 import `middle/view/errors`；错误分类优先级、用户可见文案和 provider
  细节隐藏规则仍只有 Middle 一份实现；
- 红测：`TestCoreDoesNotImportViewErrorPresentation` 初始点名
  `chat.go`、`service_assembler.go`、`tool_hooks.go`；
- adapter parity：`TestErrorPresentationPortMatchesMiddleRules` 对同一错误比较包装类型、
  原始正文、结构化错误码与工具错误文本；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：恢复 core 对 `middle/view/errors` 的直接引用并删除端口字段。

## EP-008：Context Retain Floor 契约化

- 这是纯数学判据而不是可替换服务，因此不新增无状态 adapter/interface；
- 唯一实现迁到 `contract.RetainFloorTokens`，`seelectx.RetainFloorTokens` 保留同语义
  兼容转发供 Engine 现有调用方使用；
- core 的 retain floor 与启动期窗口校验改调 Contract，退出
  `backend/engine/context/seelectx` 直接依赖；
- 红测：`TestCoreDoesNotImportContextWindowMath` 初始点名 `window.go`；
- 契约测试：`TestRetainFloorTokens` 覆盖禁用比例、禁用预算与正常比例；
- 绿测：`application/core` 保留窗回归与 `seelectx` 原窗口测试继续通过；
- 回退方式：恢复 `seelectx` 函数体并让 core 直接调用；GUI/TUI wire 与持久化不变。

## EP-009：Prompt Assets 入口收口

- core 已依赖 Engine prompt 域，但不应再穿透到其资源子包；
- `backend/engine/prompt.ValidateAssets` 成为 Application 启动期校验的窄入口，
  内部仍唯一调用 `promptassets.Validate`；
- core 不再 import `backend/engine/prompt/assets`，资产覆盖、逐文件回退与模板校验
  规则没有第二份；
- 红测：`TestCoreDoesNotImportPromptAssetsPackage` 初始点名 `service_assembler.go`；
- 绿测：`backend/engine/prompt/...`、Prompt/Identity/Instructions core 回归与
  Application 预算通过；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：恢复 core 对 assets 子包的 import 与直接 `Validate` 调用。

## EP-010：Approval Broker 工厂外置

- 新装配端口：`core.ExternalPorts.ApprovalFactory func(event.Hub) contract.ApprovalBroker`；
- 唯一实现：`wiring/adapters.NewApprovalBrokerFactory` 转发
  `backend/limit/approval.NewApprovalBroker`；
- core 只在 `Dependencies.Approval` 缺省时调用工厂，审批超时、会话归属、决议幂等与
  观察者语义仍由 Backend Broker 唯一实现；
- 删除 core 的 `NewApprovalBroker` 构造转发；core 测试改用同一 adapter；
- 红测：`TestCoreDoesNotImportApprovalImplementation` 初始点名 `assembly_helpers.go`；
- 绿测：Approval/Interaction/Permission core 回归、Backend approval 包测试与局部 build；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：恢复 core 构造 helper，并让缺省装配直接调用 Backend Broker。

## EP-011：Input Router 外部端口

- 新端口：`contract.CommandRegistryPort` 与 `contract.InputDispatcher`，路由处理闭包用
  `contract.InputRouteHandlers`；
- 唯一实现：`wiring/adapters.NewCommandRegistryFactory` 与
  `NewInputRouterFactory` 转发 `backend/engine/input`；
- core 的 `serviceState.commands` 与 `serviceComponents.input` 改持接口，启动时由
  ExternalPorts 注入；
- 命令注册表的去重、大小写查找、排序与 `/ # $` 前缀分派仍只有 Backend 一份；
- 删除 core 的 `NewCommandRegistry` 与 `newInputRouter` 构造转发；core 测试改用 adapter；
- 红测：`TestCoreDoesNotImportInputRouterImplementation` 初始点名
  `assembly_helpers.go`、`service_state.go`；
- 绿测：Command/Input/Sigil/Suggestion core 回归、adapter factory 用例与局部 build；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：恢复 core 具体类型与构造 helper，并删除两个 factory 端口。

## EP-012：Subagent Tool Event DTO 收口

- `service.handleSubagentToolEvent` 的唯一入参类型本来就是
  `dto.SubagentToolEvent`；核心入口改为直接使用 DTO；
- `backend/multiagent/subagent.SubagentToolEvent` 继续保留同身份 alias，调用方与 wire
  字段不变；
- 删除 `application/core -> backend/multiagent/subagent` 这一条仅为类型别名存在的边；
- 红测：`TestCoreDoesNotImportSubagentDomainForDto` 初始点名 `service.go`；
- 绿测：Subagent/ToolHook/ToolRecord core 回归与局部 build；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：恢复 core 的 Backend alias import；无状态迁移或数据回退。

## EP-013：Team Role Sessions 端口

- 新端口：`contract.RoleSessionsPort`，覆盖 GUI compat 实际消费的 RoleSession 与
  Schedule 方法；
- 唯一可选能力解析：`wiring/adapters.NewRoleSessionsFactory` 对 `SessionPort` 做
  `RoleSessionPort` / `SchedulePort` 断言并构造 `team.RoleSessions`；
- `core.FrontendPorts.RoleSessions` 改持端口；删除 `application/core/role_session.go`
  及其 Backend 构造逻辑；
- “未装配会话端口”“未暴露角色会话”“未暴露定时插话”三类错误仍只有 Team 一份；
- 红测：`TestCoreDoesNotImportTeamRoleSessionsImplementation` 初始点名
  `frontend_ports.go`、`role_session.go`；
- 绿测：adapter 未装配错误 parity、Role/Schedule/Frontend core/compat 回归与局部 build；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：恢复 core `role_session.go` 与 `FrontendPorts` 的具体字段类型。

## EP-014：Teamwork Board Reader 端口

- 新端口：`application/execution.BoardReader`，只含 WorkItem 真实消费的
  BoardView/SessionLive/SessionLivePage 三个只读方法；
- 唯一实现：`wiring/adapters.NewTeamworkBoardReaderFactory` 构造
  `teamwork.BoardReader`，输入是现有 `contract.RuntimePort`；
- core 只把 reader 方法接到 `WorkItemDeps`，不再 import Teamwork 实现包；
- 红测：`TestCoreDoesNotImportTeamworkBoardReader` 初始点名
  `work_table_application.go`；
- adapter parity：`TestTeamworkBoardReaderFactoryMatchesBackendReader` 对相同
  Runtime/分页输入比较三个投影；
- 绿测：WorkItem/Teamwork/Teammate/Board core+execution 回归与局部 build；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：恢复 core 内 `teamwork.NewBoardReader(service.Deps.Runtime)`。

## EP-015：Assemble DTO / Host Session 收口

- 调度参数直接使用既有的 `dto.ScheduledTaskSpec`，不再穿到 Assemble 的兼容 alias；
- goal 读会话归属拆成两个纯输入：core 自己的 `sessionIDFromContext` 与
  `ExternalPorts.HostSessionForToolCall`；Wiring adapter 转发
  `assemble.HostSessionForToolCall`；
- 删除只为一次纯转发存在的 `application/core/goal_read_session.go`，组合逻辑内联在
  Goal Application 构造处；
- 红测：`TestCoreDoesNotImportAssembleForDtoOrContextRead` 初始点名
  `goal_read_session.go`、`service_scheduler.go`；
- adapter parity：`TestHostSessionForToolCallMatchesAssemble` 对同一 ctx 比较结果；
- 绿测：Goal/Scheduled/Schedule/Runtime core 回归与局部 build；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：恢复 Assemble import、`goal_read_session.go` 与 Assemble alias 签名。

## EP-016：Goal Session Coordinator 迁入 Application

- 领域归属：按会话缓存 `goal.Controller`、第五栈重载与看板恢复投影属于 Application
  编排；Goal 状态机与持久化语义仍归 `backend/multiagent/goal`；
- `application/goal.Coordinator` 承接原 core 协调器，`CoordinatorDeps.StoreFor` 保持
  “未装配 → 内存态”的既有降级；
- `CoordinatorPort` 继续作为用例面；core 组合根只构造 Coordinator，不再 import
  Goal 领域包；
- `StackFrames` 成为 Application 层投影函数，相关 stack/done/archive 测试随能力迁入
  `application/goal`；core 只保留发布与释放编排；
- 依赖门禁：`TestCoreDoesNotImportGoalDomain` 钉住 core import 归零；
- 绿测：Goal/Governance/Archive/Release core+goal 回归与局部 build；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：将 `application/goal/coordinator.go` 移回 core，并恢复
  `serviceComponents.goal` 的具体类型。

## EP-017：Middle Model alias 退场

- `contract/protocol` 已是 Snapshot、Message、Plan、Runtime、WorkTable 等 wire 形状的
  唯一声明源；`middle/protocol/model` 只保留同身份 alias；
- core 的 50 个生产文件批量改用 `contract/protocol`，类型身份与 JSON 字段不变；
- Middle/frontend 现有 import 仍可通过 alias 路径编译，删除里程碑随 BM-002 后续消费方迁移；
- 红测：`TestCoreUsesContractProtocolModel` 初始列出 50 个仍 import Middle alias 的文件；
- 绿测：Snapshot/Message/History/Session/Goal/Tool core 回归、协议双向 build 与预算；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：批量恢复 `middle/protocol/model` import；无数据迁移。

## EP-018：Middle View 只读模型归位

- 领域归属：`RenderDiag` 是从权威 Snapshot 派生的无状态观测文本，归入
  `application/observability`；RuntimeVisibility / ParentEvidence /
  LatestVisibleUserGoal 是 Runtime 发布前的纯投影构造，归入 `application/admin`；
- 两个文件从 `middle/view` 物理迁出，不保留反向 wrapper；
- `application.RenderDiag` 公开门面与 `/diag` 命令签名不变，core 不再 import
  `middle/view`；
- 红测：`TestCoreDoesNotImportMiddleViewReadModels` 初始点名
  `diagnostics.go`、`runtime_projection.go`；
- 绿测：`RenderDiag`、RuntimeProjection、LatestVisibleUserGoal、PerfStats 及
  core Snapshot/Runtime 回归；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：把 diagnostics/projection 文件移回 `middle/view` 并恢复 core 调用。

## EP-019：Engine Prompt Coordinator 入口归位

- `application/prompt.Coordinator` 是 `backend/engine/prompt.Coordinator` 的同身份 alias；
- `application/prompt.NewCoordinator` 只做装配参数形状转换，system prompt 层组装、
  前缀缓存、任务层同步与 Engine `SetSystemPrompt` 仍只有 Engine 一份实现；
- `ValidateAssets` 也经 Application prompt 包转发，core 不再直接 import Engine prompt；
- 红测：`TestCoreDoesNotImportEnginePromptCoordinator` 初始点名
  `service_assembler.go`、`service_components.go`；
- 绿测：Application prompt、Engine prompt/assets、core Prompt/System/Component 回归；
- identity parity：`TestCoordinatorIsEngineIdentity` 钉住类型 alias；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：恢复 core 直接构造 `prompt_layer.Coordinator`。

## EP-020：Context Window Policy 入口归位

- `application/window` 以同身份 alias 暴露 `WindowConfig` / `DefaultWindowPolicy`，
  并提供 Apply/Current/Load/Default/New 的逐项转发；
- 配置默认值、YAML 加载、provider 轮数推导与保留窗口校验仍只有 Engine
  contextcontrol/seelectx 一份实现；
- core 的 window helper 与启动期校验改 import Application window，不再直连 contextcontrol；
- 红测：`TestCoreDoesNotImportContextControl` 初始点名 `assembly_helpers.go`、`window.go`；
- 绿测：Application window、Engine contextcontrol、core Window/Retain 回归与局部 build；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：恢复 core 直接 import contextcontrol，删除 Application alias。

## EP-021：Event Protocol 入口归位

- `application/event` 对 Event、EventKind、Hub、SessionAwareHub、Subscription、
  MessageDelta、CompactionProgress 与 EventHub 使用 identity alias；
- 全部 wire kind 常量保持同一值，`NewEventHub` 只转发 Middle 唯一实现；
- core 改走 Application facade，不再直接 import Middle event；
- 红测：`TestCoreUsesApplicationEventFacade` 初始点名 7 个 core 生产文件；
- identity parity：`TestEventTypesAreMiddleIdentity` 钉住 Event/EventHub 类型身份；
- 绿测：Application event、Middle event、core Event/Subscribe/Snapshot 回归与局部 build；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：恢复 core 直接 import Middle event，删除 Application facade。

## EP-022：Board Contract 入口归位

- `application/execution.ProjectBoardSink` 与 `TeamworkPlanSource` 是
  `middle/view/board` 的同身份 alias；
- core 的 `SetProjectBoardSink`、project-board plan source 类型断言改走 Application；
- board syncer、trigger、flush、tool-complete 写盘与 Runtime 可选计划读面仍只有 Middle 一份；
- 红测：`TestCoreDoesNotImportBoardFacadePackage` 初始点名
  `project_board_read.go`、`service.go`、`service_components.go`；
- identity parity：`TestBoardFacadeTypesAreMiddleIdentity`；
- 绿测：Application execution、Middle board、core Board/Project/Teamwork 回归与局部 build；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：恢复 core 直接 import `middle/view/board`。

## EP-023：Subagent View 入口归位

- `application/execution.SubagentViewCoordinator` / `SubagentViewDeps` 是 Middle 类型的
  identity alias，`NewSubagentView` 只转发构造；
- 子代理节点详情、live/tree 投影、工具活动 observer 与 record 判据仍只有 Middle 一份；
- core 的 component、FrontendPorts 与 assembler 改走 Application execution；
- 红测：`TestCoreDoesNotImportMiddleSubagentView` 初始点名
  `frontend_ports.go`、`service_assembler.go`、`service_components.go`；
- identity parity：`TestSubagentViewTypesAreMiddleIdentity`；
- 绿测：Application execution、Middle subagent view、core Subagent/Component/Frontend；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：恢复 core 直接 import `middle/view/subagent`。

## EP-024：WorkTable Projection 入口归位

- `application/execution/table` 以 identity alias 暴露 `Limits` / `WorkTableUpdate`，
  并对 clone、merge、build rows/batches、状态与 evidence 截断做逐项转发；
- WorkTable 行/批次构建、task/subagent 状态映射、Plan/树深拷贝和 evidence 截断仍只有
  Middle worktable 一份；
- core 的 worktable/publisher/cold snapshot/project board 改走 Application table；
- 红测：`TestCoreDoesNotImportMiddleWorkTable` 初始点名 6 个 core 生产文件；
- identity parity：`TestWorkTableFacadeTypesAreMiddleIdentity`；
- 绿测：Application table、Middle worktable、core WorkTable/Cold/ProjectBoard/Limits；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：恢复 core 直接 import `middle/view/worktable`。

## EP-025：View State 入口归位

- `application/session/viewstate` 对 Coordinator、Deps、RuntimeStateProjection 使用
  identity alias，并转发 NewCoordinator / DurableConversationCount；
- Snapshot、message seq、revision、投影采集/应用与 event 发布仍只有 Middle view state 一份；
- core 的 chat/commit/cleanup/snapshot/frontend/policy/input-index 改走 Application facade；
- 红测：`TestCoreDoesNotImportMiddleViewState` 初始点名 11 个 core 生产文件；
- identity parity：`TestViewStateTypesAreMiddleIdentity`；
- 绿测：Application viewstate、Middle state、core Snapshot/Message/Chat/Runtime/Session；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：恢复 core 直接 import `middle/view/state`。

## EP-026：Switch Workspace 入口归位

- `application/workspace` 暴露 `SwitchUseCase` / `SwitchDeps` / `SwitchProjection` /
  `SwitchViewState` identity alias 与 `NewSwitch` 转发；
- workspace create/bind/unbind、draft binding、projection 构造与应用仍只有 Middle 一份；
- core 的 assembler、frontend ports、scheduler、cold commit 与 workspace host 改走 Application；
- 红测：`TestCoreDoesNotImportSwitchWorkspace` 初始点名 7 个 core 生产文件；
- identity parity：`TestSwitchWorkspaceTypesAreMiddleIdentity`；
- 绿测：Application workspace、Middle workspace、core Workspace/Session/Scheduled；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：恢复 core 直接 import `middle/switch/workspace`。

## EP-027：Switch Policy 入口归位

- `application/session/switchpolicy` 以 `switching` 包名 re-export Middle 纯策略类型、
  interaction/draft 常量与队列错误值，并逐项转发纯函数；
- queue target、draft identity、interaction choice、release window、fork gate、
  pending-tail、unload gate 等判据仍只有 Middle 一份；
- core 只更换 import，类型/错误/函数身份不变；
- 红测：`TestCoreDoesNotImportMiddleSwitchPolicy` 钉住 core import 为零；
- identity parity：`TestSwitchPolicyTypesAndErrorsAreMiddleIdentity`；
- 绿测：Application switchpolicy、Middle switch、core Queue/Draft/Interaction/Release/Fork/
  Unload/Meta/PendingTail；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：恢复 core 直接 import `middle/switch`。

## EP-028：Limit Tier 入口归位

- `application/policy.TierDeps` / `TierGate` 是 Backend tier 类型的 identity alias；
- session tier 的生效档位解析、持久化、restore、gate 与快照同步仍只有 Backend tier 一份；
- core 的 tier host 只替换 import，不改变任何判据或锁语义；
- 红测：`TestCoreDoesNotImportLimitTier` 初始点名 `session_permission_tier.go`；
- identity parity：`TestTierFacadeTypesAreBackendIdentity`；
- 绿测：Application policy、Backend tier、core PermissionTier/Permission；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：恢复 core 直接 import `backend/limit/tier`。

## EP-029：Limit Base 入口归位

- `application/policy` identity alias 暴露 `Limits` / `Knobs` / `KnobsHost`，并逐项转发
  Apply/Get/Validate/SetSessionReleaseWindow/NewKnobs；
- 默认 limits、原子快照、窗口边界、Knobs 状态机与快照语义仍只有 Backend limit 一份；
- core 的 limits helper、release window、snapshot budget、knobs 与 window 校验只更换 import；
- 红测：`TestCoreDoesNotImportLimitBasePackage` 初始点名 6 个 core 生产文件；
- identity parity：`TestLimitsFacadeTypesAreBackendIdentity`；
- 绿测：Application policy、Backend limit、core Limit/Knobs/ReleaseWindow/SnapshotBudget/Retain；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：恢复 core 直接 import `backend/limit`。

## EP-030：Engine Context 入口归位

- `application/conversation/context` 以 `Engine*` identity alias 暴露 Coordinator/History/
  Deps/CompactResult/CompactionIndexPort，并逐项转发构造函数、内容分类与 warning；
- 压缩、history safety、provider-only/checkpoint/skill 分类与 oversized result 判据仍
  只有 Engine context 一份；
- core 的 chat/history/compact/input-index/skill context 只更换 import；
- 红测：`TestCoreDoesNotImportEngineContextPackage` 初始点名 8 个 core 生产文件；
- identity parity：`TestEngineFacadeTypesAreBackendIdentity`；
- 绿测：Application conversation context、Backend context、core Context/Compact/History/
  Skill/Snapshot；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：恢复 core 直接 import `backend/engine/context`。

## EP-031：Engine Task Context 入口归位

- `application/execution/taskcontext` identity alias 暴露 Coordinator/Deps/Prompt/任务状态类型、
  Tool constants，并转发 plan、session context、transcript tail、ledger identity 与 budget helper；
- Task execution、ledger、transcript 裁剪、checkpoint 和 tool observation 判据仍只有 Engine task 一份；
- core 的 chat/plan/reference/review/cold/history/tool/worktable 只更换 import；
- 红测：`TestCoreDoesNotImportEngineTaskContext` 初始点名 17 个 core 生产文件；
- identity parity：`TestTaskContextTypesAreBackendIdentity`；
- 绿测：Application taskcontext、Backend task、core Task/Plan/Tool/History/Session；
- 修复并行写窗口造成的 `assembly_helpers.go` 空文件，恢复其现有公开 helper 并改用
  Application prompt/event/window facade；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：恢复 core 直接 import `backend/engine/task`。

## EP-032：Core assembly helper 转发退场

- 删除 `application/core/assembly_helpers.go`，不再在 core 复制 Application
  Event、Prompt、Window 与 Protocol 的构造/helper 入口；
- 退场的 11 个纯转发为 `NewDefaultWindowPolicy`、`DefaultWindowConfig`、
  `LoadWindowConfig`、`NewEventHub`、`NewPromptStack`、`NewEffortManager`、
  `cloneSnapshot`、`cloneRuntimeState`、`maxLoopsFor`、`reasoningEffortFor`
  与 `reactBudgetFor`；
- 直接消费方改调 `application/event`、`application/prompt`、
  `application/window` 与 `contract/protocol`；`wiring/bootstrap/runtime.go`
  的窗口配置加载不再经过 core；
- 红测：`TestCoreHasNoPackageFunctionForwarders` 初始逐项点名上述 11 个 wrapper；
- 结构门禁同时保留 `TestServiceHasNoCompatibilityForwarders` 与
  `TestCoreHasNoPackageFunctionForwarders`，阻止 core 再长出同类转发；
- 绿测：Application 结构门禁、core、wiring/bootstrap 与 prompt/event/window
  受影响编译；
- core 直接内部依赖保持 3；生产文件 117 → 116，生产代码 11,816 → 11,764 行；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：恢复 `assembly_helpers.go` 并让调用方改回 core helper。

## EP-033：Engine Session Runtime 入口归位

- `application/session/runtime` 以 identity alias 暴露 Engine session 的
  Coordinator/Deps、会话定位、记录、transcript、fork 与 restore 类型；
- 记录加载、workspace scope、transcript 拼接、fork 深拷贝和 Engine session
  bundle 仍只有 Backend Engine 一份实现；
- core 的 chat/session/fork/cold/reference/input-index 改走 Application
  session runtime facade，不再直接 import `backend/engine/session`；
- 红测：`TestCoreDoesNotImportEngineSessionPackage` 初始点名 core 生产文件；
- 绿测：Application session/runtime、Backend Engine session、core Session/
  Fork/Cold/History；
- core 直接内部依赖 3 → 2，生产文件与行数不变；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：恢复 core 直接 import `backend/engine/session`。

## EP-034：Core 剩余纯 helper 转发退场

- 删除 `teamworkJobCompletions`、`teamworkJobEvents`、`splitSigil`、
  `deepCopyForkRecord`、`ApplyWindowConfig` 与 `CurrentWindowConfig` 六个
  core 纯转发 helper；
- WorkTable completion 信号直接调用 `application/session/switchpolicy`，
  sigil 解析直接调用 `application/input.Parse`，fork 深拷贝直接调用
  switchpolicy；窗口配置的写/读调用方直接调用 `application/window`；
- 红测：`TestCoreHasNoPackageFunctionForwarders` 逐项点名六个 wrapper；
- 绿测：Application、Contract、Wiring 与 E2E Facade/Document 全绿；
- core 直接内部依赖保持 2，生产文件保持 116，生产代码 11,764 → 11,736 行；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：恢复对应 core helper，并把调用方改回 core。

## EP-035：Session Context helper 转发退场

- 删除 `application/core/session_ctx.go` 与 `withSessionID` /
  `sessionIDFromContext` 两个纯转发；
- chat、compact、goal、history safety、tool hooks 与 turn runner 直接使用
  `application/execution/taskcontext` 的会话 ID context helper；
- 旧的 core 测试调用保留一个 test-only shim，只存在于 `_test.go`，不进入生产面；
- 红测：`TestCoreHasNoPackageFunctionForwarders` 初始点名 `session_ctx.go`；
- 绿测：Application、Contract 与 E2E Facade/Document 全绿；
- core 直接内部依赖保持 2，生产文件 116 → 115，生产代码 11,736 → 11,721 行；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：恢复 `session_ctx.go` 并让生产调用方改回 core helper。

## EP-036：ApplyLimits 兼容入口退场

- 删除 `application/core.ApplyLimits` 纯转发；组合根直接调用
  `application/policy.Apply`；
- core 测试仅保留 `_test.go` 中的同名测试 shim，生产面不再提供该入口；
- 红测：`TestCoreHasNoPackageFunctionForwarders` 点名 `limits.go.ApplyLimits`；
- 绿测：Application、Contract 与 E2E Facade/Document 全绿；
- core 直接内部依赖保持 2，生产文件保持 115，生产代码 11,721 → 11,715 行；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：恢复 core `ApplyLimits`，并把组合根改回调用。

## EP-037：Limits 读取与 ReleaseWindow 写入归位

- 删除 `application/core.Limits` 纯转发；core 生产消费点直接读取
  `application/policy.Get()`；
- 将 `ApplySessionReleaseWindow` 的“先校验、再改进程快照、返回生效值”顺序迁入
  `application/policy`，避免 core 保留同一个 limit 写 helper；
- core 测试只保留 `_test.go` 的 `Limits` shim；新增 policy 行为测试覆盖非法值零改动
  与合法值即时生效；
- 红测：`TestCoreHasNoPackageFunctionForwarders` 点名 core `Limits`；
- 绿测：Application、Contract 与目标包测试通过；
- core 直接内部依赖保持 2，生产文件保持 115，生产代码 11,723 → 11,709 行；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：恢复 core `Limits` / `ApplySessionReleaseWindow`，并让调用方回到 core。

## EP-038：CompressedTurn Archiver 与 Goal Store 解析归位

- `CompressedTurnArchiver` 从 core 迁入 `application/reference`；Application 只持
  `CompressedTurnStore.SaveCompressedTurn(projectID, sessionID, ref, content)` 窄端口；
- Wiring adapter 把端口转换为既有 `store.Commit/ToolResult`，继续复用唯一
  `SaveCommitWorkspace` 写路径；显式 project/session scope、ctx 会话优先级与
  `compressed:<segment>` ref 不变；
- `CompressedTurnRefPrefix` 下沉 `contract`，Store 与 Application 共用同一常量；
- `store.Event/EventToolCall` 改为 `contract/protocol.TranscriptEvent/ToolCall`
  同身份 alias；JSON 字段与持久化格式不变；
- Goal 的 Runtime `SessionContextStoreFor` 可选类型断言移入
  `application/goal.NewCoordinatorForRuntime`，core 不再直接识别 Store 类型；
- 红测：core 文件结构门禁在迁移前拒绝 `compressed_turn.go` 与 core alias；
- 绿测：Reference/Goal/Core/Wiring adapter、Application、Contract 与 E2E
  Facade/Document 全绿；
- core 直接内部依赖保持 2，直接 Store import 文件 7 → 5，生产文件 115 → 114，
  生产代码 11,709 → 11,605 行；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化格式均未改变；
- 回退方式：恢复 core Archiver/Goal type assertion 与 Store 本地 Event 定义。

## EP-039：Event 段落与 Queue 恢复 DTO 契约化

- `CompleteEventUnits`、`EventParagraphs`、`ParagraphEnd` 与
  `IsParagraphBoundary` 的唯一实现迁入 `contract/protocol/event_units.go`；
  Store 保留同名函数与 `EventParagraph` 同身份 alias，旧 backend/session 调用不变；
- edit/fork 的 resolver、process 与 core host adapter 改用
  `protocol.TranscriptEvent` / `protocol.Message` / `protocol.ForkRequest`，删除
  Application 对 Store Event 与 Middle model alias 的直接依赖；
- 新增 `contract/dto.QueueItem` / `QueueRecoveryReport`；Store 与 backend session
  运行时保留同身份 alias，core durable queue 回填只消费 Contract DTO；
- 红测：`TestEditForkProductionFilesUseProtocolEvents` 初始点名 5 个仍 import
  Store/Middle model 的文件；Queue 恢复 import 门禁同步钉住 core 不直连 Store；
- 绿测：Contract BM 门禁、Store queue/paragraph、backend session fork/queue、
  Application edit/fork、architecture budget 与 wiring adapters 定向通过；
- core 直接内部依赖保持 2；直接 Store import 文件 5 → 1，生产文件保持 114，
  生产代码 11,605 → 11,603 行；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化 JSON 字段均未改变；
- 回退方式：恢复 Store 内段落函数本体与 Queue 本地类型，并把 edit/fork 消费点
  改回 Store Event；Contract alias 可整体删除。

## EP-040：ProjectBoard Store 类型边归位

- `application/execution` 暴露 `TeamworkPlan/TeamworkState` 迁移期同身份类型，
  ProjectBoard Application 与 core 不再直接 import Store；
- ProjectBoard 的 WorkTable、Team Plan、Team State 与 ProjectRoot 读面仍保持原签名；
  Store 继续唯一拥有 TeamworkPlan 状态机、校验和持久化载荷；
- 红测：`TestCoreProjectBoardDoesNotImportStore` 初始点名 `project_board_read.go`；
- 绿测：Application ProjectBoard、Execution、Core、Wiring registry 与架构预算；
- core 直接内部依赖 2 → 1；直接 Store import 1 → 0；生产文件保持 114，
  生产代码 11,603 → 11,602 行；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化 JSON 字段均未改变；
- 回退方式：恢复 core 直接 import Store，并删除 execution 的 Teamwork 类型门面。

## EP-041：层边界裁决与 Application 全局依赖预算

- 新增长期契约
  [`docs/arch/application-middle-backend-boundaries.md`](../../docs/arch/application-middle-backend-boundaries.md)；
- 裁决 Session 聚合最终归 `application/session`；Middle 只保留纯策略/读投影，
  Backend Engine 保留执行运行时；
- 裁决整个 `application/**` 的具体 Backend/Middle/Store 依赖最终归零；
  迁移期 identity facade 与 host adapter 必须登记，不得持状态或第二份判据；
- 裁决 `TeamworkPlan` 系列完整 DTO 化，当前 `application/execution` alias
  仅为迁移桥；
- 新增 `application_wide_dependency_budget.json` 与结构测试，当前冻结 30 个
  唯一具体依赖包，只许下降；
- core 直接依赖保持 1，`Service` 方法保持 23；
- 回退方式：删除全局预算测试与架构文档；`core` 预算不受影响。

## EP-042：Conversation window pure helper 归位

- `DurableConversationCount`、`BoundConversationTail`、`BoundConversationHead`
  的唯一实现迁入 `contract/protocol/conversation_window.go`；
- `middle/switch/history` 改调 Contract，不再依赖 `middle/view/state`；
- `middle/view/state` 保留同身份兼容转发，后续随 ViewState 一起迁入
  `application/session/viewstate`；
- 红测：Contract helper 不存在导致编译红；History import 门禁点名
  `middle/view/state`；
- 绿测：Contract window、History、ViewState 与 core History 回归；
- 该批不改变 Application 预算与 wire；
- 回退方式：恢复 ViewState 内函数体并让 History 重新调用该包。

## EP-043：SessionState / ViewState 物理迁入 Application

- `middle/switch/sessionusecase` 全部状态与测试迁入
  `application/session/state`，Go 包名改为 `sessionstate`；
- `middle/view/state` Coordinator 迁入 `application/session/viewstate`，
  删除 Middle facade；
- 所有 Application/Core/Conversation/Session 调用方改走新路径；
- `middle/switch/history` 已在前置切片退出 ViewState 依赖，因此 Middle
  没有形成 Application 反向依赖；
- 红测：`TestSessionAggregateLivesInApplicationSession` 在迁移前点名缺失的
  state/viewstate 文件；
- 绿测：SessionState、ViewState、Conversation、Core、Middle History/View、
  E2E Facade/Layout；
- `application/core` 直接内部依赖 1 → 0；`Service` 保持 23；生产文件保持 114，
  行数保持 11,602；
- Application 全局具体依赖包 30 → 28；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化 JSON 字段均未改变；
- 回退方式：将两包移回 Middle，恢复 core 直接 import；状态与算法不复制。

## EP-044：TeamworkPlan 完整 DTO 化

- `TeamworkPlan`、`TeamworkMember`、`TeamworkMilestone`、`TeamworkWorkItem`、
  `TeamworkState` 及里程碑/工作项状态常量迁入 `contract/dto/teamwork_plan.go`；
- `TeamworkPlanIsEmpty`、`TeamworkWorkItemStatusOrPending`、
  `TeamworkWorkItemTerminal` 是 Contract 自由函数；原类型方法退出，JSON tag 不变；
- Store 保留五类同身份 alias 与一行兼容自由函数，持久化校验仍只在 Store；
- Middle 看板的可选计划端口改为直接消费 `contract/dto`；
  Backend teamwork 调用点改用自由函数，不复制状态判据；
- `application/execution.TeamworkPlan/TeamworkState` 迁移 alias 已删除，
  ProjectBoard 与 Core 直接使用 Contract 类型；
- 红测：`TeamworkPlan` Contract 类型尚不存在时，
  `TestTeamworkPlanContractWireShapeAndFreeFunctions` 与
  `TestTeamworkPlanUsesContractIdentity` 编译失败并逐项点名缺口；
- 绿测：Contract DTO、Application/Core/Execution、Store、Middle board、
  Backend teamwork 与 Engine assemble 全部通过；
- Application 全局具体依赖包保持 28：本批删除 `application/execution -> store`
  类型边，但 `goal`、`session` 与 `toolrecord` 仍有后续 Store 迁移；
- `Service` 保持 23，core 生产文件/行数保持 114 / 11,602；
- GUI/TUI wire、Snapshot revision/seq 与 `plan.json` 持久化 JSON 均未改变；
- 回退方式：恢复 Store 本地类型与方法，让 Middle/Application 回退到 Store alias。

## EP-045：Fork lineage 窄载荷投影

- `application/session.ForkLineageApplication` 不再解码完整
  `store.SessionContextRecord`，只定义 `forked_from` 私有窄投影；
- 持久化 JSON 字段、损坏数据降级为 nil、空 parent 拒绝等语义保持不变；
- 新增结构门禁 `TestForkLineageApplicationDoesNotImportStore` 与
  `application/session` 行为测试；
- 红测：结构门禁点名 `fork_lineage.go` 仍 import Store；
- 绿测：Application、Session 与 Core fork lineage 定向测试；
- `application/**` 具体依赖包总数暂保持 28，因为 Goal/ToolRecord 仍有
  `store`、`store/record` 类型边；本批消除 `application/session -> store`；
- `Service` 保持 23，core 文件/行数不变；GUI/TUI wire 与持久化格式不变；
- 回退方式：恢复完整 Store 载荷解码；旧数据无需迁移。

## EP-046A：Core 组件 getter 与输入 facade 退场

- 删除 26 个无参、单返回值的 `*ApplicationComponent` getter；core 包内调用点
  直接读取 `service.components`，跨包调用走 `FrontendPorts` 或领域包；
- 删除只承载 `adminApplicationComponent` 的 `service_account.go`；
- `HasSigilPrefix`、`SigilOf`、sigil 常量和 `Suggestion` 类型身份回归
  `application/input`，根门面直接引用该包；
- 新增 `TestCoreDoesNotExposeComponentGetters`、
  `TestCoreDoesNotOwnInputFacade` 与 `TestCoreDoesNotOwnInputVocabulary`；
- 红测先点名旧 getter/输入 facade，迁移后结构门禁与 Application 全包测试通过；
- core 生产文件 114 → 111，生产代码 11,602 → 10,634；
- GUI/TUI wire、事件 revision/seq 与持久化格式不变。

## EP-046C：入站接口定义回到 Application

- 删除 `application/core/service_api_faces.go` 和仅做 `core` 再导出的
  `application/faces.go`；
- `ViewFace`、`CommandFace`、`PolicyFace`、`QueueFace` 与 AP-002 能力接口的
  事实源改为 `application/faces_api.go`；
- 编译期结构断言继续固定在 `application/frontend_assertions.go`；
- `api_contract_test.go` 直接扫描根 Application 入站文件，E2E facade 门禁
  继续冻结方法集，并明确允许这些入站契约类型由根包定义；
- 本切片保持 `Service` 23 个方法；接口签名、GUI/TUI 调用面与 wire 未改变；
- 回退方式：恢复 core 接口文件与 faces.go alias 再导出层。

## EP-046D：Frontend 状态壳与 Session 订阅归位

- `SubscribeSession`、`SubscribeSessionWithReplay` 的唯一过滤与 replay
  判据迁入 `application/session/viewstate.Coordinator`；
- `PrepareProviderHistory`、`PromptLayers`、`SetProjectBoardSink` 从
  `core.Service` 移到 `compat.Frontend`，分别直达 history、prompt stack 和
  project-board 装配端口；
- ToolRecord 的启用、snapshot 端口、flush、看板与命令退出码入口从
  `core.Service` 移到 `compat.Frontend`，直达
  `application/execution/toolrecord.Application`；
- `wiring/bootstrap` 与项目看板测试改用 `Frontend`；core 旧集成测试只在
  `_test.go` 保留 shim，生产包不再暴露同名方法；
- 红测：`viewstate` 订阅方法不存在导致编译失败，随后两个过滤/隔离用例转绿；
- `Service` 23 → 11；core 生产文件 114 → 111、生产代码
  11,602 → 10,634；
- GUI/TUI wire、事件 revision/seq 与持久化格式不变；
- 回退方式：恢复 core Service 订阅与装配入口，并把 `Frontend` 调用改回。

## EP-046E：Core 组件单表达式转发退场

- 删除 40 个 `Service -> service.components.*` 单表达式转发方法；
- 内部调用点直接持有组件，或在构造期用闭包延迟解析组件，
  避免捕获尚未初始化的 coordinator；
- 新增 `TestCoreHasNoComponentMethodForwarders`，禁止同类转发重新出现；
- 修正组合顺序问题：TurnProcess、RestoreRecovery、DraftSubmission 和
  view/session 组件在目标组件就绪后解析；
- 测试门禁更新为“旧转发不存在”，不再要求兼容 wrapper 存在；
- `Service` 保持 11，core 生产文件保持 111，生产代码收紧到 10,634；
- Application 全包、E2E Facade/Document/Layout、race、build、vet、
  README 与 diff check 全部通过；
- 回退方式：恢复单表达式方法并把调用点改回 `Service`。

## EP-046B：自由构造器退场与组合阶段收拢

- 删除 68 个自由 `newService*` 构造器；构造闭包按能力归入
  `service_assembler_{conversation_turn,session,session_draft,session_query,edit_fork,applications,capabilities,scheduler_lifecycle}.go`；
- Conversation Turn、Session lifecycle/draft/restore/query、Edit/Fork、
  Cold Restore、Admin、Policy、Goal、Scheduler、Workspace、Reference、
  Context Compaction、Async Completion、Lifecycle 与 FaultGuard 的初始化
  顺序保持逐项不变；
- 构造器只在 `Service` 组合阶段读取 core 私有状态，没有把这些状态复制进
  领域包，也没有新增万能 Host 接口；
- 新增 `TestNoFreeServiceConstructors`，红测初始点名 68 个构造器，迁移后
  要求自由 `newService*` 构造器为 0；
- 58 个只承载构造器的文件退出；core 生产文件 111 → 68，生产代码
  10,634 → 10,243 行；
- `Service` 公开方法保持 11；GUI/TUI wire、事件 revision/seq、snapshot 与
  持久化 JSON 均未改变；
- 回退方式：从对应 `service_assembler_*.go` 恢复单个构造器文件；业务状态与
  判据无需迁移。

## EP-047：ToolRecord 内存台账归位

- `Accumulator`、`Stage`、原因归一和叶子暂存测试从 `store/record` 迁入
  `application/execution/toolrecord`；
- ToolRecord 应用不再 import `store/record`，Store 只通过
  `contract.ToolRecordSnapshotStore` 接收整份快照；
- `store/record` 整包退出，E2E facade/layout 白名单与 Contract DTO 注释同步；
- 红测：`TestToolRecordOwnsItsInMemoryLedger` 先点名缺失的
  `application/execution/toolrecord/accumulator.go`；依赖预算随后点名
  `store/record` 仍被允许；
- 绿测：ToolRecord 累加/暂存 parity、Store ToolRecord 落盘、Application
  全局依赖预算与 E2E facade/layout；
- Application 全局具体依赖包 28 → 27；
- GUI/TUI wire、事件 revision/seq、`tool-record.jsonl` JSON 与 `Service` 方法数不变；
- 回退方式：恢复 `store/record` 四个源码/测试文件，并让 ToolRecord 重新 import。

## EP-048：Goal Store 适配退出 Application

- `application/goal.CoordinatorDeps` 改为接收 Goal 域自己的
  `Store` / `AuditAccount` 与已投影的治理视图回调，不再出现
  `*store.SessionContextStore`、`SessionBoards` 或 `GoalBoardMeta`；
- 删除 `NewCoordinatorForRuntime` 的 Runtime 类型探测；Core 通过
  `ExternalPorts.Goal` 接收组合根构造好的 Coordinator；
- 新增 `wiring/adapters.NewGoalCoordinator`，唯一负责把
  `Runtime.SessionContextStoreFor`、第五栈适配器和看板存档读面接到 Application；
- 看板存档到 `dto.GoalGovernanceView` 的投影归
  `backend/multiagent/goal.GoalGovernanceViewFromBoardArchive`；
- 红测：`TestGoalApplicationDoesNotImportStore` 先点名
  `application/goal/coordinator.go`；依赖预算随后点名 `store` 仍被允许；
- 绿测：Goal 活栈/存档恢复 parity、Wiring narrow-port 门禁、Backend Goal
  存档边界与 Application 全局依赖预算；
- Application 全局具体依赖包 27 → 26，具体 `store` 依赖归零；
- GUI/TUI wire、Goal Stack 与 `board_goal.json` 持久化格式不变；
- 回退方式：恢复 Application 对 Store 类型的直接依赖和 Runtime 探测，删除
  Wiring Goal 适配。

## EP-049：统一 Conversation 适配迁入 conversation 包

- `ConversationView`、`ConversationChat`、`RecoverConversation` 的唯一实现从
  `application/core/conversation_session.go` 迁入
  `application/conversation.UnifiedApplication`；
- 主会话使用 `SnapshotOf`、`SessionRecordsPort` 与 `SubmitToSession` 三个窄
  回调；subagent/teammate 继续委托 `contract.AgentConversationPort`；
- `FrontendPorts.ConversationSession` 注入 compat；GUI/headless 原方法签名与
  wire 不变，旧 `Service` 同名方法退出；
- 红测：`NewUnified` 尚不存在时
  `TestUnifiedApplicationRoutesMainAndAgentConversations` 编译失败；迁移后
  主会话、缺失快照、teammate 路由和 Submit 参数全部通过；
- 记录投影、恢复分类测试随 owner 迁入 `application/conversation`；
- `Service` 公开方法 11 → 8；core 生产文件 68 → 67，生产代码
  10,243 → 10,065 行；
- GUI/TUI wire、事件 revision/seq、Snapshot 与持久化 JSON 均未改变；
- 回退方式：恢复 core 统一适配文件，并让 compat 调用 `Service` 原方法。

## EP-050：Frontend Snapshot 组合迁入 viewstate

- `Snapshot` 的状态富化、草稿行注入、编辑标注、revision 和目录 row enrichment
  唯一实现迁入 `application/session/viewstate.SnapshotApplication`；
- core 只注入既有 `*Locked` 回调；生产内部消费点改读
  `service.components.frontendSnapshot.Snapshot()`，不再经过公开 `Service` 方法；
- `application/compat.Frontend.Snapshot` 直接消费同一组件；
- 红测：`NewSnapshotApplication` 尚不存在时
  `TestFrontendSnapshotProjectsDraftRowsAndHostEnrichment` 编译失败；迁移后
  revision、draft row、annotation、live rows 与 enrich 顺序通过；
- 现有 core 集成测试通过 `_test.go` 单行 shim 保留 `Snapshot` 调用面；
- `Service` 公开方法 8 → 7；core 生产代码 10,065 → 10,044 行；
- GUI/TUI wire、revision/seq 与 Snapshot JSON 字段不变；
- 回退方式：恢复 core `Snapshot` 方法并让 compat 调回。

## EP-051：输入受理门禁迁入 input.Application

- `Submit` 的 closed/draining 门禁、空白裁剪和 dispatcher 委托迁入
  `application/input.SubmissionApplication`；
- core 组合根只注入生命周期读回调与已装配的 `InputDispatcher`；
- GUI/TUI/headless 的 `Submit` 签名不变，由 `compat.Frontend` 直接消费新组件；
- 红测：`NewSubmission` 尚不存在时
  `TestSubmissionApplicationPreservesLifecycleAndDispatch` 编译失败；迁移后
  closed、draining、空白与正常分发语义通过；
- core 集成测试通过 `_test.go` 单行 shim 保留 `Submit`；
- `Service` 公开方法 7 → 6；core 生产代码 10,044 → 10,043 行；
- GUI/TUI wire、事件 revision/seq 与持久化格式不变；
- 回退方式：恢复 core `Submit` 并让 compat 调回。

## EP-052：Task 终态 handler 迁入 taskcontext

- `TaskTerminalHandler` 的 Plan 基线补种、`VerifyAndApply`、Task 镜像及
  check/complete 后的 Plan 镜像顺序迁入
  `application/execution/taskcontext.TerminalHandlerApplication`；
- `ToolFailed`、`ToolNeedsUserDecision` 词表补入 Application identity facade；
- core 仅注入已有 `*AfterTerminal` host 回调；wiring registry 继续通过
  `Frontend.TaskTerminalHandler` 注册同名工具；
- 红测：`NewTerminalHandler` 尚不存在时
  `TestTerminalHandlerPreservesProjectionOrder` 编译失败；迁移后成功/失败两条
  镜像顺序通过；
- core 集成测试通过 `_test.go` 单行 shim 保留同名方法；
- `Service` 公开方法 6 → 5；core 生产代码 10,043 → 10,039 行；
- GUI/TUI wire、事件 revision/seq 与 Task/Plan 持久化不变；
- 回退方式：恢复 core handler 并让 compat 调回。

## EP-053：Role Tool Activity 迁入 execution

- `HandleRoleToolActivity` 的归属校验、EvidenceChars 截断、started/completed
  分类、ToolRecord stage 和 session event 发布唯一实现迁入
  `application/execution.RoleActivityApplication`；
- core 只注入请求 ID 读锁、截断、ToolRecord stage、诊断计数与事件发布回调；
- 红测：`NewRoleActivity` 尚不存在时
  `TestRoleActivityApplicationRoutesStagesAndPublishes` 编译失败；迁移后
  drop、running/failed 分类、stage 参数、截断与事件路由通过；
- core 原有真实事件链测试通过 `_test.go` 单行 shim 保留入口；
- `Service` 公开方法 5 → 4；core 生产代码 10,039 → 10,015 行；
- GUI/TUI wire、revision=0 语义、ToolRecord 持久化 JSON 不变；
- 回退方式：恢复 core 方法并让 compat 调回。

## EP-054：Interaction 决议迁入 session

- `ResolveInteraction` 的视图单格校验、后台 approval 反查、cancel、
  Session restore 与 Account switch 决策树迁入
  `application/session.InteractionApplication`；
- Session 恢复失败、账号切换失败的 notice + close 顺序保持不变；
- core 只注入当前交互读锁、approval broker、resume/account/notice/close 回调；
- 红测：`NewInteraction` 尚不存在时 `TestInteractionApplication...` 编译失败；
  迁移后 view approval、后台 approval、missing、Session 失败路径通过；
- core 集成测试通过 `_test.go` 单行 shim 保留入口；
- `Service` 公开方法 4 → 3；core 生产代码 10,015 → 9,997 行；
- GUI/TUI wire、approval 事件 revision/seq 与持久化不变；
- 回退方式：恢复 core 方法并让 compat 调回。

## EP-055：Session Fork 与显式提交退场

- `ForkSession`、`ForkSessionLatest` 的父 ID 校验、per-session 过渡锁、
  latest cut、commit 后 resume 顺序迁入 `application/session.ForkApplication`；
- `SubmitToSession` 的草稿物化、loaded/content-unloaded 判定、恢复与显式
  session submit 顺序迁入 `application/session.SubmitApplication`；
- core 只保留真实 fork snapshot/copy/persist 事务、加载判定和
  `submitConversationFor`；command 的 fork 入口直接调用唯一组件；
- 红测：`NewFork` / `NewSubmit` 尚不存在时对应测试编译失败；迁移后 fork
  request、latest cut、switch failure、materialize/load/resume/submit 顺序通过；
- core 集成测试通过 `_test.go` 单行 shim 保留旧调用面；
- `Service` 公开业务方法 3 → 0；core 生产代码 9,997 → 9,947 行；
- GUI/TUI wire、fork snapshot、queue、事件 revision/seq 与持久化 JSON 不变；
- 回退方式：恢复两个 core 方法并把 compat 调回。

## EP-056：Middle model alias 退出 Application

- Application 生产代码的 `middle/protocol/model` 引用全部改为
  `contract/protocol`；142 个 core/domain 测试的 model 引用同步切换；
- `TestCoreUsesContractProtocolModel` 与 Edit/Fork 协议门禁继续禁止旧
  `middle/protocol/model`，并允许新的 `contract/protocol` 唯一身份；
- 红测：全量结构门禁先点名仍读取 Middle model 的 Application 文件，
  同时暴露机械替换把门禁目标误改成 `contract/protocol` 的反向断言；
- 绿测：`go test ./application/... -count=1` 全包通过；
- Application 具体 Backend/Middle/Store 依赖包 26 → 25，Store 保持 0；
- `Service` 公开方法保持 0，core 生产文件 67、生产代码 9,947 行；
- GUI/TUI wire、Event、Snapshot 与持久化 JSON 不变；
- 回退方式：恢复 `middle/protocol/model` 引用及其门禁目标。

## EP-057：Wiring 调用方迁入 Frontend

- `Service` 公开业务方法归零后，`wiring/registry` 的 Snapshot、
  SubmitToSession、TaskTerminalHandler 与 `wiring/bootstrap` 的
  Snapshot、HandleRoleToolActivity 调用改走 `compat.Frontend`；
- 工具注册名、定时任务投递顺序、角色工具事件载荷和 GUI/TUI wire 均未改变；
- 红测：`go test ./wiring/...` 点名 `app.Snapshot`、`app.SubmitToSession`、
  `app.TaskTerminalHandler`、`app.HandleRoleToolActivity` 在 `Service` 上不存在；
- 绿测：`go test ./wiring/...`、`go build ./...`、E2E Facade/Document/Layout；
- 指标保持 `Service=0`、core 67 文件/9,947 行、Application 具体依赖 25；
- 回退方式：恢复 Service 业务门面并把 Wiring 调用点改回。

## EP-058：Review/Reference 后端实现退出 Application

- Reference 的 ref 分类与分页算法直接使用 `contract/protocol` 的唯一实现，
  `application/reference` 不再 import `backend/tool/reference`；
- Review 在 Application 侧定义 `RequestProcessor` 与 `ProcessorFactory`，
  具体策略由 `wiring/adapters.NewReviewProcessorFactory` 注入，返回
  `backend/tool/review` 的唯一实现；
- 红测：Application 全局依赖预算点名 `backend/tool/reference` 与
  `backend/tool/review` 仍被允许；
- 绿测：Reference/Review、Wiring adapters、Application 全包、`go build` 与
  `go vet`；
- Application 具体 Backend/Middle/Store 依赖包 25 → 23，Store 保持 0；
- core 生产代码经统一格式化后为 9,953 行；
- ref wire、Review JSON、事件 revision 与前端调用签名不变；
- 回退方式：恢复 Application 对两个 backend 工具包的直接 import，删除 Review
  Processor 端口。

## EP-059：输入发现策略归位

- 把 `unknownCommandNotice`、`sigilMigrationHint`、命令近似匹配和有界编辑距离从
  `application/core/completion.go` 迁入 `application/input`；
- `input.Application` 新增 `SigilMigrationHint`、`UnknownCommandNotice` 与三个目录
  窄查询 `HasPlugin`、`HasSkill`、`HasVisibleTool`，调用方直接使用唯一实现；
- 删除 `application/core/completion.go`，core assembler 只负责把命令、Skill、
  Plugin 与可见工具目录装配成回调；
- 红测：input 新接口不存在导致编译失败，`TestInputCompletionPolicyLivesOutsideCore`
  点名旧 core 文件仍存在；
- 绿测：input 提示/迁移/近似匹配、core 未知命令集成与 Suggestions 回归通过；
- core 生产文件 67 → 66，生产代码 9,953 → 9,788 行；`Service` 保持 0，
  Application 具体 Backend/Middle/Store 依赖保持 23，Store 保持 0；
- 未知命令文案、sigil、建议排序、GUI/TUI wire、事件 revision/seq 与持久化格式
  均未改变；
- 回退方式：恢复 `application/core/completion.go` 并让 input 删除对应方法。

## EP-060：WorkTable Runtime 归属迁移

- `application/core/work_table.go` 的生产实现迁入
  `application/execution/table/runtime.go`；新 owner 同时持有 publisher、
  Plan/子代理源同步、task 行投影编排、生命周期消费者、Todo 刷新与 TraceBlock。
- `application/execution/table.RuntimeDeps` 只接收共享状态内核、Task/View 协调器、
  session/task 读面、发布回调、故障守护和 async trigger；不 import
  `application/core`，也不持有完整 `Service`。
- core assembler 改为构造 `table.NewRuntime`，其余回合、冷读与 Draft 路径直接
  调用 owner；生产 core 不再保留 WorkTable 实现或消费者停止状态。
- 现存 core 集成测试通过 `_test.go` shim 继续覆盖既有锁序与行为；测试迁移随后续
  owner 化批次继续，不在本批复制生产判据。
- 红测：`TestWorkTableRuntimeLivesInExecutionTable` 初始同时点名 legacy
  `application/core/work_table.go` 与新 runtime 缺失；迁移后结构门禁通过。
- 绿测：`go test ./application/execution/table ./application/core -count=1`，
  Application 架构预算通过。
- core 生产文件 66 → 65，生产代码 9,788 → 9,409 行；`Service` 公开方法保持 0，
  core 直接内部依赖与 Application 具体依赖包数不变。
- WorkTable JSON、GUI/TUI wire、事件 revision/seq、任务同步顺序与持久化格式均未改变；
- 回退方式：恢复 `application/core/work_table.go`，把 assembler 与调用点切回 core，
  再删除 `table.Runtime`。

## EP-061：Session View Runtime 状态归属迁移

- `restoring` 集合、restore 广播信号、view epoch、per-session view intent、
  Session 状态/目录富化、读文件引用与工具后 assistant 占位从
  `application/core/service_snapshot.go` 迁入
  `application/session/viewstate/session_runtime.go`；
- `viewstate.Coordinator` 新增 `DraftView`、`TitleForSession` 与 Task
  `RoleRoundFor` 窄回调；draft、会话目录和 task 事实仍由原 owner 提供；
- core 删除四个视图状态字段与 `service_snapshot.go`，assembler 和回合/冷恢复/
  Resume/Release 路径直接调用 viewstate owner；既有 core 集成测试只保留
  `_test.go` shim；
- 红测：`TestSessionViewRuntimeLivesInViewState` 初始同时点名 legacy
  `service_snapshot.go` 与新 owner 文件缺失；
- 绿测：`go test ./application/session/viewstate ./application/core -count=1`；
  viewstate 新增 restore/epoch/intent 状态所有权用例；
- core 生产文件 65 → 64，生产代码 9,409 → 9,069 行；`Service` 公开方法保持 0，
  core 直接内部依赖和 Application 具体依赖包数不变；
- Snapshot、目录行状态、GUI/TUI wire、事件 revision/seq 与持久化格式不变；
- 回退方式：恢复 `service_snapshot.go` 与 core 字段，把 assembler/调用点切回 core。

## EP-062：Session Residency Runtime 归属迁移

- resident engine 使用序、loaded content 使用序、受保护集、两层驱逐、
  正文卸载和自动回读入口从
  `application/core/{resident_lru,content_lru,session_release}.go` 迁入
  `application/session/residency/runtime.go`；
- 新 owner 只接收共享状态内核、Session/View、`ResidencyPort`、上限及
  flush/release/reload/catalog 回调，不 import `application/core`，不持有 `Service`；
- core assembler 构造唯一 `residency.Runtime`，Session lifecycle/draft/cold/query、
  Turn tail 和历史安装均直接调用 owner；core 只保留 history-page 的
  `reloadSessionContent` 读回适配；
- 红测：`TestSessionResidencyRuntimeLivesInSessionPackage` 初始点名三个 legacy
  core 文件和缺失的新 runtime；
- 绿测：`go test ./application/session/residency ./application/core -count=1`；
  snapshot 不别名底层顺序的双层回归随 owner 迁入新包；
- core 生产文件 64 → 62，生产代码 9,069 → 8,573 行；`Service` 公开方法保持 0，
  core 直接内部依赖和 Application 具体依赖包数不变；
- LRU 顺序、释放窗口、内容卸载、Snapshot 目录刷新、GUI/TUI wire 与持久化格式不变；
- 回退方式：恢复三个 core 文件与顺序字段，把 assembler/调用点切回 core。

## EP-063：ToolRecord Host Adapter 归属迁移

- teammate 状态帧解析、subagent event 解析、稳定 subagent unit key 与终态节点
  递归收口从 `application/core/{tool_record,tool_record_stage}.go` 迁入
  `application/execution/toolrecord/host.go`；
- core 的 assemble、RoleActivity、Subagent view、WorkTable runtime、turn tail、
  release 和 shutdown 路径直接调用 ToolRecord 组件；core 只保留 draft 可写判据与
  view session 两个装配回调；
- 红测：`TestToolRecordHostAdaptersLiveInToolRecord` 点名两个 legacy core 文件和
 缺失的新 owner；
- 绿测：`go test ./application/execution/toolrecord ./application/core -count=1`；
- core 生产文件 63 → 61，生产代码 8,655 → 8,568 行；`Service` 公开方法保持 0，
  core 直接内部依赖和 Application 具体依赖包数不变；
- ToolRecord 判据、unit key、flush 顺序、JSONL wire、GUI/TUI 行为与持久化格式不变；
- 回退方式：恢复两个 core host adapter 文件，把装配/调用点切回 core。

## EP-064：Environment Trace 归属迁移

- 环境尾部标记、稳定排序、SSH/local 地址投影、名称/地址单行截断与空表退场
  从 `application/core/environment_context.go` 迁入
  `application/prompt/environment.go`；
- `prompt.EnvironmentTraceDeps` 只接收 Session SSH 环境表、当前固定环境与项目根
  三个读回调；core assembler 提供适配，不复制渲染判据；
- core 集成测试通过 `_test.go` alias 保留既有 fixture；纯渲染与稳定顺序测试已归
  prompt owner；
- 红测：`TestEnvironmentContextLivesInPromptPackage` 点名 legacy core 文件与缺失
  prompt owner；
- 绿测：`go test ./application/prompt ./application/core -run 'Environment' -count=1`；
- core 生产文件 61 → 60，生产代码 8,568 → 8,465 行；`Service` 公开方法保持 0，
  core 直接内部依赖和 Application 具体依赖包数不变；
- 环境标记、行顺序、SSH 地址、GUI/TUI wire 与持久化格式不变；
- 回退方式：恢复 `application/core/environment_context.go` 并删除 prompt owner。

## EP-065：Compaction Gate 归属迁移

- per-session compacting 集合与广播信号、领轮、等待、释放和 deferred submit
  从 `application/core/context_compact_gate.go` 迁入
  `application/conversation/context/compaction_gate.go`；
- gate 只接收共享状态内核与 deferred submit 回调；提交入口、显式压缩和
  capabilities 装配直接消费 owner，core 不再保存 compacting 状态；
- 红测：`TestCompactionGateLivesInConversationContext` 点名 legacy core 文件与
  缺失的新 owner；
- 绿测：`go test ./application/conversation/context ./application/core -run
  'Compaction' -count=1`；context 新增串行/取消用例；
- core 生产文件 60 → 59，生产代码 8,465 → 8,330 行；`Service` 公开方法保持 0，
  core 直接内部依赖和 Application 具体依赖包数不变；
- 同会话压缩串行、deferred submit 目标会话、上下文取消和 note/事件语义不变；
- 回退方式：恢复 core gate 文件与两个状态字段，把调用点切回 core。

## EP-066：SessionInputIndex 读取编排归位

- 把全量用户输入索引从 `application/core/session_input_index.go` 迁入
  `application/session/query`：durable range、record fallback、已加载窗口、
  摘要构建与空会话降级由 `InputIndexApplication.Read` 统一编排；
- durable 读取保持在共享写锁之外，已加载窗口只取 `ViewMu.RLock`；查询不持完整
  `Service`，也不拥有 Session 生命周期或持久化；
- `inputIndexUserText` 移入既有 `core/skill_context.go`，因为 Edit/Fork 与冷读
  仍共用同一用户正文判据；
- 红测：`NewInputIndex` 不存在导致 query 编译失败，
  `TestSessionInputIndexRuntimeLivesInSessionQuery` 点名 legacy core 文件仍存在；
- 绿测：query 的 full-range、record fallback、空 session 门禁，以及原有
  SessionInputIndex 集成回归；
- core 生产文件 59 → 58，生产代码 8,330 → 8,248 行；`Service` 保持 0，
  core 直接依赖、Application 全局具体依赖与 Store 依赖均未改变；
- snapshot/wire 字段、durable range 读取次数、事件 revision/seq 与持久化格式
  均未改变；
- 回退方式：恢复 `application/core/session_input_index.go`，把
  `SessionQuery.InputIndex` 回调切回旧方法。

## EP-067：Provider Recovery 归属迁移

- provider context/history 拒绝后的有界 checkpoint、恢复/重放、可重放判据和恢复
  信封清理从 `application/core/history_safety.go` 迁入
  `application/conversation/context/recovery.go`；
- `RecoveryDeps` 只接收 Core/Task/View/Context、engine history replace、stream 和
  event 发布回调；core turn assembly 直接消费 owner；
- display 层通过 context owner 的公开恢复标记判断内部行；core 只保留测试 shim；
- 红测：`TestProviderRecoveryLivesInConversationContext` 点名 legacy core 文件与
  缺失 owner；
- 绿测：`go test ./application/conversation/context ./application/core -run
  'Provider|Recovery|Context' -count=1`；context 新增 retryable/non-empty parity；
- core 生产文件 58 → 57，生产代码 8,248 → 8,061 行；`Service` 公开方法保持 0，
  core 直接内部依赖和 Application 具体依赖包数不变；
- provider 错误分类、checkpoint、超时不自动重放、恢复信封和事件顺序不变；
- 回退方式：恢复 `history_safety.go`，把 turn assembly 切回 core。

## 本轮收口指标

| 指标 | 本轮起点 | 当前 |
|---|---:|---:|
| `Service` 公开方法 | 171 | 0 |
| core 直接内部依赖 | 37 | 0 |
| core 生产文件 | 126 | 57 |
| core 生产代码 | 18,723 行 | 8,061 行 |

core 生产代码净减 10,662 行，直接内部依赖从 37 降到 0；
AP-116A/B/C/D/E/F1/F2a/F2b/F2c/F2d/F2e/F2f/F2g 共收回或迁出 148 个 `Service` 公开入口。
AP-093～AP-115D2、AP-108B1/B2a/B2b、AP-113D1/D2
已把 Reference、Admin、Workspace、Queries、Execution、Review、Goal、Scheduler、
Policy、Session Lifecycle/Draft/Cold、Context Compaction 与 Conversation Queue/ChatState
与 Tool Hook / Tool Record / WorkTable publisher+consumer 迁出 core；尚未完成调用方迁移的
Service 入口继续保留一行兼容转发。
