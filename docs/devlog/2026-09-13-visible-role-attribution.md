# 2026-09-13 可见会话的群聊角色归属（EXEC/ADVISOR 可辨）

## 范围

症状：AgentTeam 装配成功、goal 的 TL 回合也确实在跑，但聊天区里 teammates 之间
没有区分——所有 assistant 行都渲染成 `AGENT`，TL 的回合原文只以「系统」行出现。

根因（三层，全部命中）：

1. 可见会话的三个生产点都没有写群聊归属：
   `view_state.AppendMessageLockedFor`（实时 user/assistant/工具/流式续写）、
   `chat.go appendHistoryLockedFor`（引擎历史镜像）、
   `session_runtime conversationFromTranscriptLocked`（事件 → 可见消息重建，丢弃
   `RoleName`/`RoundID`）。
2. `injectGoalDirectivesFor` 把 TL 指令写成 `role=system` 的可见行：前端只能渲染
   成「系统」，不是 ADVISOR。
3. 前端不是问题：`gui/frontend/dist/components.js` 的 `roleIdentity` 一直按
   `message.role_name` 渲染 EXEC/ADVISOR，缺的是载荷字段。

## 变更

- `application/model/state.go`：新增 `MessageOrigin` 与角色名常量
  （`RoleNameUser`/`RoleNameMain`/`RoleNameTL`），归属字段沿用既有 JSON tag。
- `application/core/view_state/coordinator.go`：新增
  `AppendMessageWithOriginLockedFor`，归属随消息一起进前端载荷；原入口等价于
  零值 origin。
- `application/core/{chat.go,service_snapshot.go,tool_hooks.go}`：用户行 = `user`、
  EXEC 行（含工具调起/结果与工具轮后续写正文）= `main`，用户行开启的 round 同步
  盖到同轮可见行（`task_context.RoleRoundFor` 与 transcript 的 R4 编号同源）。
- `application/core/goal_service.go`：TL 指令回放改为 assistant 行 +
  `role_name=tl`；角色会话号按工厂口径 `(team_id, role_name)` 解析，未装配时为
  空（不伪造）。
- `application/core/session_runtime/archive.go`：事件 → 可见消息投影保留
  `role_name`/`role_session_id`/`round_id`/`unit_seq`（落盘 record 与冷恢复同源）。
- 文档：`docs/arch/a2a-agent-team-factory.md` 增加不变量 AT11；
  `application/model/README.md` 记录 `Message` 归属字段口径。

## 验证

红灯（复现，修复前）：

```text
go test ./application/core ./application/core/session_runtime -run "RoleAttribution|AdvisorIdentity" -count=1
→ 用户行 role_name = ""（want user）、EXEC 行 role_name = ""（want main）、
  TL 指令回放 role = "system"（want assistant）、事件→可见消息归属字段全空
```

绿灯（修复后）：

```text
go test ./application/core ./application/core/session_runtime -run "RoleAttribution|AdvisorIdentity" -count=1 -v
→ 4 个用例全 PASS
go test ./application/... ./sessionstore ./internal/adapters -count=1   → 全 ok
go test ./gui -count=1                                                  → ok
go test ./e2e -run DTO -count=1                                         → ok（新增 Message 归属字段契约）
node --test gui/frontend/dist/*.test.mjs                                → 251 pass / 0 fail
go build ./...                                                          → exit 0
```

回归用例：`application/core/visible_role_attribution_test.go`、
`application/core/session_runtime/visible_role_attribution_test.go`（断言对象就是
前端收到的 `Snapshot.Conversation`，即 `snapshot.conversation`）。

## 已知缺口（本次未动）

本次只修「区分」；「召唤出 teamwork」仍只有 goal-a2a 的 ADVISOR 一条真链路：
`TurnScheduler`（channel + 链表轮转）仍无生产调用点，`review-team`/`research-team`
装配后没有执行者，`order_policy` 的 `user_main_decided`/`scheduled_only` 也还没有
顺序函数实现。见 `docs/2026-09-10-a2a-agentteam-recovery/agentteam-management.md`
的 P0.3。

另一个未动的偏差：TL 指令注入 EXEC 引擎历史时仍是 provider `user` 消息
（`goal_service.go` 的 `injectGoalDirectivesForStart`/`GoalIterationCompleted`，
正文带 `〔[TL 指令 …]〕` 前缀），与 §2.1/AT9「编排态材料一律 system」不一致。
本页只改可见会话投影，provider 侧注入保持原行为，避免在修 UI 归属的同一提交里
改变模型输入。
