> 历史记录，不描述当前实现。以下清单记录已执行的兼容层退场过程。

# 删除与重定向清单

本清单是退场 ratchet。每项只有在调用方归零、结构门禁为绿后才能勾选。

## D1：删除兼容包

- [ ] `application/compat/frontend.go`
- [ ] `application/compat/frontend_test.go`
- [ ] `application/compat/README.md`
- [ ] 目录 `application/compat/`

归零验证：全仓 `.go` 与 `.md` 不再引用 `application/compat`、`applicationcompat`、`compat.Frontend`。

## D2：删除根门面兼容契约

- [ ] `application/frontend_assertions.go`
- [ ] `application/faces_api.go`
- [ ] `application/query_faces_test.go`
- [ ] `application/api_partition_test.go`
- [ ] `application/application.go` 中 `Frontend` alias 与 compat import
- [ ] `application/faces_api.go` 中的 `ViewFace/CommandFace/PolicyFace/QueueFace`
- [ ] `ConversationAPI/SessionAPI/ExecutionQueryAPI/...` 等已无真实调用方的宽接口

保留：`ContextCompactionResult` 等稳定公开类型 alias 移到 `application/application.go`，只保留一个身份。

## D3：删除 assembly 兼容类型与 test-only 转发

- [ ] `application/assembly/service.go` 的 `type Frontend struct{ *Service }`
- [ ] `application/assembly/frontend_ports.go` 中的 49 字段扁平兼容快照与 `NewFrontendPorts`
- [ ] `application/assembly/frontend_migration_test.go`（403 行、128 个方法）
- [ ] `application/assembly/frontend_shell_legacy_test.go`
- [ ] `application/assembly/snapshot_legacy_test.go`
- [ ] `application/assembly/submission_legacy_test.go`
- [ ] `application/assembly/subscription_legacy_test.go`
- [ ] `application/assembly/service_fork_legacy_test.go`
- [ ] `application/assembly/service_interaction_legacy_test.go`
- [ ] `application/assembly/service_submit_session_legacy_test.go`
- [ ] `application/assembly/service_task_terminal_legacy_test.go`
- [ ] `application/assembly/service_role_activity_legacy_test.go`
- [ ] `application/assembly/service_helpers_test.go` 的 `frontendOfTestService`

## D4：删除 15 个测试别名补丁

- [ ] `chat_turn_pipeline_test_aliases_test.go`
- [ ] `chat_turn_pipeline_test_aliases2_test.go`
- [ ] `chat_stream_test_aliases_test.go`
- [ ] `context_compaction_test_alias_test.go`
- [ ] `context_compaction_gate_test_aliases_test.go`
- [ ] `edit_fork_test_aliases_test.go`
- [ ] `policy_test_aliases_test.go`
- [ ] `scheduler_test_aliases_test.go`
- [ ] `session_cold_test_aliases_test.go`
- [ ] `session_fork_restoring_test_aliases_test.go`
- [ ] `session_history_test_aliases_test.go`
- [ ] `session_residency_runtime_test_aliases_test.go`
- [ ] `session_view_runtime_test_aliases_test.go`
- [ ] `toolrecord_host_test_aliases_test.go`
- [ ] `work_table_runtime_test_aliases_test.go`

处理原则：先确认 alias 背后的行为测试是否已在 owner 包有等价覆盖。没有覆盖的，用 owner API 改写测试；仅旧名字/旧接收者本身没有行为价值的，删除。

## D5：删除“facade 只转发”测试

在以下文件删除只做源码字符串/AST 转发表断言的测试函数；保留真实行为测试并重定向：

- `completion_test.go`
- `context_compact_test.go`
- `fault_guard_test.go`
- `history_search_test.go`
- `project_board_read_test.go`
- `reference_tools_test.go`
- `review_files_test.go`
- `role_permissions_test.go`
- `service_admin_application_test.go`
- `service_input_test.go`
- `service_queue_component_test.go`
- `service_scheduler_test.go`
- `service_snapshot_test.go`
- `session_archive_component_test.go`
- `session_delete_component_test.go`
- `session_lifecycle_component_test.go`
- `session_meta_application_test.go`
- `session_ssh_test.go`
- `subscription_legacy_test.go`
- `workspace_gitrepos_usecase_test.go`
- `workspace_tree_usecase_test.go`

具体旧测试名以 `Test.*Facade.*|.*OnlyDelegates.*` 扫描结果为准；不要按文件名整批删除行为用例。

## D6：迁移构造与注册

- [ ] `wiring/constructors.go`：删除 `NewFrontend`
- [ ] `wiring/registry/registry.go`：删除 `applicationcompat` import 与 6 处 `compat.New`
- [ ] `wiring/bootstrap/bootstrap.go`：删除 `wiring.NewFrontend`、`frontendApp.*` 聚合调用
- [ ] `wiring/bootstrap/application.go`：直接使用 RoleApplication
- [ ] `e2e/scenario/harness.go`：组件直连
- [ ] `test/*` harness：组件直连；project board sink 走真实 owner

## D7：迁移前端

- [ ] `gui/bridge.go`：宽 `Application` 拆为 consumer-owned domain dependencies
- [ ] `gui/shutdown.go`：只依赖 snapshot/activity/lifecycle
- [ ] `gui/terminal_bridge.go`：只依赖 snapshot
- [ ] `tui/ui.go`、`tui/stream.go`、`tui/dialog.go`、panel 文件：使用 TUI dependencies
- [ ] `headless/headless.go`、`headless/headless_subagent.go`：使用 headless dependencies
- [ ] `console/backend_console.go`：使用 console dependencies

## D8：结构门禁

- [ ] `application/compat` 目录必须不存在
- [ ] 无 `.go` import `application/compat`
- [ ] `application.Frontend` 不存在
- [ ] `assembly.Frontend`、`assembly.FrontendPorts` 不存在
- [ ] application 根包无旧宽接口
- [ ] assembly 的 `.go` 和 `_test.go` 均无 `Frontend` 接收者方法
- [ ] assembly 的 `.go` 和 `_test.go` 均无只转发 `service.components` 的兼容方法
- [ ] 删除所有 `frontendOfTestService`
- [ ] 前端不直接 import assembly/backend/store

## D9：文档

- [ ] `application/README.md`
- [ ] `application/assembly/README.md` 与相关分卷
- [ ] `application/assembly/README-service.md`
- [ ] `application/assembly/README-session.md`
- [ ] `application/conversation/README.md`
- [ ] `application/input/README.md`
- [ ] `application/execution/README.md`
- [ ] `application/conversation/search/README.md`
- [ ] `docs/2026-10-25-application-modularization/plan.md` 的 M8 状态
- [ ] `docs/2026-10-25-application-modularization/implementation.md` 的退场记录

## 不得删除

- 真实状态机、锁、恢复、事件 revision/seq、项目作用域和失败语义
- 失败路径、并发、恢复、wire 和 UI 行为测试；它们应迁到 owner 或直接调用 owner
- `MEMORY.md` 的安全规则和用户数据
