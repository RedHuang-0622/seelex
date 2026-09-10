# 分步骤提交打点表（2026-09-10 A2A AgentTeam / subagent 恢复）

> 原则：每批只包含一个可解释主题；每批提交前跑对应最小测试；不 push。
> R1 后端退役已单独提交：`1ce9969 refactor(sessionstore): 退役非 JSON 会话后端实现`。

## C1 存储侧角色会话 / draft / floor / team registry

- 状态：`[ ]`
- 主题：R2/R3/R4 存储基建。
- 文件：
  - `sessionstore/sessionstore.go`
  - `sessionstore/message_rows.go`
  - `sessionstore/lifecycle.go`
  - `sessionstore/session_context.go`
  - `sessionstore/stack_channel.go`
  - `sessionstore/structural_events.go`
  - `sessionstore/material_cache.go`
  - `sessionstore/role_session.go`
  - `sessionstore/role_session_router.go`
  - `sessionstore/role_session_test.go`
  - `sessionstore/schedule_events.go`
  - `sessionstore/team_registry.go`
  - `sessionstore/team_registry_test.go`
  - `sessionstore/README.md`
- 验证：`go test ./sessionstore -count=1`

## C2 Application / headless 角色与 AgentTeam 管理

- 状态：`[ ]`
- 主题：RoleSession 窄转发、AgentTeam factory/registry、headless `role.*`/`team.*`。
- 文件：
  - `application/contract/dto/agentteam.go`
  - `application/core/agentteam/`
  - `application/core/agentteam_service.go`
  - `application/core/role_session.go`
  - `internal/adapters/agentteam_ports.go`
  - `internal/adapters/session_workspace_ports.go`
  - `application/core/README.md`
  - `application/core/goal/README.md`
  - `gui/headless.go`
  - `gui/headless_role_test.go`
  - `gui/headless_team.go`
  - `gui/headless_team_test.go`
  - `gui/role_live_probe_test.go`
  - `gui/team_live_probe_test.go`
  - `gui/README.md`
  - `docs/arch/a2a-agent-team-factory.md`
  - `docs/arch/README.md`
  - `docs/2026-09-10-a2a-agentteam-recovery/agentteam-management.md`
- 验证：`go test ./application/core/... ./internal/adapters ./gui -count=1`

## C3 subagent 中断恢复续跑

- 状态：`[ ]`
- 主题：领域无关七步恢复模板 + subagent tool-call 适配 + headless `subagent.*`。
- 文件：
  - `application/contract/ports.go`
  - `application/contract/dto/subagent_recovery.go`
  - `application/core/resume/`
  - `application/core/service_subagent_resume.go`
  - `application/core/service_fakes_test.go`
  - `seelebridge/runtime_subagent_resume.go`
  - `seelebridge/runtime_subagent_resume_test.go`
  - `seelebridge/runtime_subagent_recovery.go`
  - `seelebridge/session/subagent_sessions.go`
  - `seelebridge/node/agent_node.go`
  - `seelebridge/node/coordinator.go`
  - `seelebridge/runtime.go`
  - `main.go`
  - `internal/adapters/runtime_port.go`
  - `e2e/scenario/harness.go`
  - `gui/headless_subagent.go`
  - `gui/headless_subagent_test.go`
  - `gui/subagent_live_probe_test.go`
  - `gui/tool_full_chain_test.go`
  - `docs/2026-09-10-a2a-agentteam-recovery/subagent-resume.md`
- 验证：`go test ./application/core/resume ./seelebridge ./gui -run 'Subagent|Resume' -count=1`

## C4 编排态材料统一 provider `system`

- 状态：`[ ]`
- 主题：只有真实用户输入是 `user`；task/goal/plan/subagent 状态材料 provider `system`。
- 文件：
  - `application/core/context_runtime/coordinator.go`
  - `application/core/history_safety.go`
  - `application/core/history_safety_test.go`
  - `application/core/task_context/plan_transcript.go`
  - `application/core/task_context/task_context_state.go`
  - `application/core/task_context/role_fields_test.go`
  - `application/core/task_context/provider_role_audit_test.go`
  - `application/core/service_input_test.go`
  - `application/core/session_runtime/fork.go`
  - `application/core/session_runtime/transcript_range.go`
  - `application/model/context.go`
  - `internal/adapters/role_fields_test.go`
  - `sessionstore/durable_history.go`
  - `sessionstore/wire_assembler.go`
  - `seelebridge/custom_role_live_probe_test.go`
  - `seelebridge/system_position_live_probe_test.go`
  - `docs/2026-09-10-a2a-agentteam-recovery/provider-role-audit.md`
- 验证：`go test ./application/core/... ./sessionstore ./seelebridge -run 'Provider|Role|Transcript|Recovery' -count=1`

## C5 验证、文档与构建钩子

- 状态：`[ ]`
- 主题：设计工作包、符合度记录、pprof 钩子与构建缓存忽略。
- 文件：
  - `.gitignore`
  - `MEMORY.md`
  - `main_pprof.go`
  - `docs/2026-09-08-session-storage-architecture/conformance-checklist.md`
  - `docs/2026-09-08-session-storage-architecture/my_design.md`
  - `docs/2026-09-10-a2a-agentteam-recovery/README.md`
  - `docs/2026-09-10-backend-cache-goal-session/`
  - `docs/research/2026-09-10-collaborative-doc-session-model.md`
- 验证：`go test ./e2e/ -run 'TestRepositoryModulesHaveReadmes|TestRepositoryModuleReadmeLinks|TestRepositoryAgentDocumentationRules'`

## 最终门禁

- `[ ] go test ./application/... ./sessionstore ./internal/adapters ./gui -count=1`
- `[ ] go test ./seelebridge/... ./session/... ./workspace/... -count=1`（非沙箱，项目根链接解析）
- `[ ] go build ./...`
- `[ ] go build -tags "gui,desktop,production" ./...`
- `[ ] gofmt -l` / `git diff --check`
