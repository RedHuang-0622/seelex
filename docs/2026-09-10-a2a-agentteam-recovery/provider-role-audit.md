# provider role 口径审计（工作流 B / P2）

审计对象：task/goal/plan/subagent 状态材料进入 provider 历史时的 `role` 取值。
权威口径：[`docs/arch/a2a-agent-team-factory.md`](../../arch/a2a-agent-team-factory.md) §2.1 与 §9（AT2/AT9）。

## 1. 结论

- 生产者侧有**唯一收口点** `application/core/task_context/task_context_state.go`
  的 `applyTranscriptRoleFieldsLocked`：真实用户输入 → `role_name=user`；
  internal/context 行与 system 行 → `role_name=system`；assistant/tool → `role_name=main`
  （显式给出 `role_name` 时不覆盖，供 tl/agent-team 角色行使用）。
- provider 侧 `role` 在转换边界统一映射（`sessionstore/durable_history.go`
  `providerRoleForEvent`、`application/core/task_context/plan_transcript.go`
  `providerRoleForTranscriptEvent`、`sessionstore/wire_assembler.go`）：
  task/goal/plan/subagent 的 internal/context 状态材料 → `system`；真实用户输入
  保持 `user`；激活技能正文保留 internal user 轮次以维持稳定前缀缓存。
- **没有任何编排态材料以 `assistant` 身份进入 provider 历史**；回归测试
  [`application/core/task_context/provider_role_audit_test.go`](../../../application/core/task_context/provider_role_audit_test.go)
  以反伪装断言钉住这一点。

## 2. 落点 → 实际 role → 结论

| 落点 | 实际 provider role | 逻辑 `role_name` | 结论 |
|---|---|---|---|
| 真实用户输入（`appendTranscriptEventLocked`，`Role="user"` 且非 internal） | `user` | `user` | 符合：只有用户输入是 user |
| 任务/plan/goal/subagent 状态材料（`Kind=internal`，存储 `Role="user"`） | `system` | `system` | 符合：provider 映射为 system，存储行保留 internal 标记 |
| 遗留 `internal_user` / `context` 行 | `system` | `system` | 符合 |
| system 状态行（如恢复说明，`Role="system"`） | `system` | `system` | 符合（AT9） |
| 角色会话真实模型输出 | `assistant` | `main` / `tl` / agent-team 角色名 | 符合（AT2：角色名只在 metadata） |
| 工具结果（含中断占位） | `tool` | 调起方角色名 | 符合 |
| 中断工具链占位正文（`context_runtime.RepairInterruptedToolChains`） | `tool`（provider-only） | — | 符合：不成对即补占位，不伪装成 assistant |
| 群聊角色行（role draft → sequencer sync） | `assistant` | 该角色 `role_name` | 符合：真实发言才用 assistant |

## 3. 已收口的 provider role 口径

真实端点实验已确认历史中部的 `system` 可用：

- `seelebridge/system_position_live_probe_test.go` 在 user 历史之后插入一条
  `system` 状态材料，再继续 user 输入；真实 provider 返回 `"好的"`，未拒绝。

因此最终口径：

- 真实用户输入 → provider `user`；
- task/goal/plan/subagent internal/context 状态材料 → provider `system`；
- 只有激活技能正文（`active-skill` internal 轮次）保留内部 user 轮次，用于维持
  稳定前缀缓存；它不是 task/goal/plan/subagent 的 active 状态；
- system 行与恢复说明 → provider `system`；
- 角色真实发言 → `assistant`，工具结果 → `tool`。

存储行保留 `Kind=internal` / `wire_material` 与 UI 过滤语义，provider 映射只在
转换边界发生，不改变 message 事实。

## 4. 验证

```text
go test ./application/core/task_context -run 'TestProviderRoleAudit|TestTranscriptRoleFieldsDefaults' -count=1
go test ./application/... -count=1
$env:SEELEX_LIVE_SMOKE='1'; go test ./seelebridge -run TestSystemPositionLiveProbe -count=1
```

回归测试：`provider_role_audit_test.go`（本文件 §1/§2 的可执行版本）。
