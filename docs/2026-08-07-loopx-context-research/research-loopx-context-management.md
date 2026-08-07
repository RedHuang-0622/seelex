# LoopX 长任务上下文管理调研与 Seelex 借鉴点

> 调研日期：2026-08-07
> 对象：LoopX（github.com/huangruiteng/loopx，commit 4452d53，0.4.x）——字节跳动工程师黄瑞腾开源的"Loop Engineering"本地控制面，MIT，Python 3.11+ 零运行时依赖，已有两条 200+ 小时真实长任务轨迹（OpenViking issue-fix、AutoML 实验）。
> 方法：git clone 源码 + 三路并行深读（状态内核/goals-todos、turn/quota/handoff、设计文档与事故档案）；Seelex 侧以 2026-08-05 前代码为准（seelexctx 五包 + application/core/context_controller.go + sessionstore）。
> 配套：本文聚焦"长任务上下文管理"（跨轮次/跨会话的上下文持久化、注入、恢复与漂移治理），非全功能对比；与 `docs/2026-08-06-agent-landscape-research/` 的自迭代试点规划（D1–D5）直接衔接。

---

## 一、一句话结论

**LoopX 把长任务的全部控制状态（目标/gate/todo/claim/证据/quota/handoff）外置成"模型上下文之外、事件溯源、只读投影"的状态内核，让每轮 agent 都变成可替换的"有界执行 worker"；Seelex 已有同样思路的骨架（窗口外压缩可逆归档、跨会话快照承袭、Plan DAG 终态协议），缺的是 LoopX 用真实事故打磨出的三件东西：① 机器可判定的"下一步/阻塞/验证了什么"（goal frontier + typed todo），② 保护用户注意力的 quota 闸门（只对已验证的状态转移记账），③ "重规划必须改变机器可见前沿"的契约纪律。**

---

## 二、LoopX 长任务上下文管理的机制拆解

### 2.1 状态内核：事件溯源 + 多层投影，"聊天记忆不是事实源"

**真相层是 append-only JSONL 事件流**（`loopx/event_sourced_state.py:537-580`，`events.jsonl`，schema `loopx_state_event_v0`）。11 种事件类型（todo_added/claimed/updated/blocked/deferred/completed、refresh_recorded、run_recorded、quota_spent、evidence_attached、supervisor_proposed…），追加时持文件锁、序号单调、event_id 指纹冲突检测、显式禁止 `mutates_prior_event_id`——**事件不可变、不可篡改历史**。

**工作台层是 Markdown**（ACTIVE_GOAL_STATE.md）：人类/模型可编辑的投影面，backfill 幂等双向同步（`:229-408`）。前端 dashboard 是只读投影，写操作必须显式 capability + preview 握手，默认 `control_plane_write_enabled: false`（state-interaction-model.md）——"前端不造第二事实源"是硬契约。

**写回权限受 mutation authority 约束**（`mutation_authority.py:176-299`）：多 agent 注册时写 todo 必须带 `--agent-id`；todo 已 `claimed_by` 他人时需 `todo_lifecycle_authority` 显式授权；唯一豁免是精确链接的 user_gate decision_scope。**agent 无法直接写控制自身的状态。**

**注入是分级压缩的投影链**：events.jsonl → `build_state_projection` → `render_active_state_sections`（markdown）→ `compact_todo_group` → `build_goal_frontier_projection`。每层限长（todo text 220/500、goal channel 180-260、vision 总预算 1200 字符）、run history 只注入最近 5 条、隐私分层（public/private-pointer）。**模型每 turn 只见"决策载荷"（frontier + channel + summaries），永远不读事件流本体。**

### 2.2 有界 turn 事务：效果所有权分离

一个 turn 是 `sha256` 锚定的原子事务，相位固定：

```
host_execute → typed_result → validation → durable_writeback → quota_spend → scheduler_apply → scheduler_ack
```

commit 策略硬编码 `result<validate<writeback<spend; apply<ack; cadence:no-spend`（`turn_driver/transaction.py:18-26,156`）。

- **宿主 prompt 第一行**："Execute exactly one bounded LoopX Turn… Do not write LoopX state, spend quota, or apply scheduler changes; the adapter owns those effects"（`turn_driver/codex_cli.py:250-253`）——**副作用由控制面独占**，host 只产出 typed_result。
- **TurnEnvelope ≤8KB**（`quota/turn_envelope.py:744-800`）：把 quota should-run 全决策投影成模型可读视图，附 `action_signature`（source_hash/envelope_hash）——模型看到的判定不可篡改，签名不匹配直接 CONTRACT_ERROR。
- **独立后置条件验证**（`executor.py:389-521`）：material 结果必须过 trusted argv-only 的 validator（不用 LLM 当裁判），返回 passed/progress/failed/inconclusive；failed/inconclusive 必须带 `recovery_kind`（repair_required/replan_required）。
- **幂等 = turn_key**：lineage（goal/agent/todo/action_hash）+ 执行上下文的 canonical hash。resume journal 按相位推进，重启/换模型后按 turn_key 精确续跑或重放；失败可定点重试（validation 失败时保留 host_result 只重跑 validator）。
- **失败类型化**：HOST_FAILURE/VALIDATION_FAILED/WRITEBACK_FAILED/QUOTA_SPEND_FAILED 各绑 failed_phase。

### 2.3 Quota：保护的不是算力，是用户注意力

`loopx quota should-run` 是无人值守循环的第一道闸，状态机 `blocked_health → operator_gate → focus_wait → eligible → waiting → throttled → paused`，最终 decision 是严格优先级链：replan → run → observe → safe_bypass → self_repair → repair_bridge → workspace_guard → 升级 → agent_scope → **skip**。

**记账语义是核心创新**：quota spend 不是数 token/调用次数，而是对**"未计费的 accountable delivery run"**记账（`quota/slot_accounting.py:131-169`）——只有 `should-run=true → 有界 turn → 验证+写回 → refresh-state(带 delivery_outcome) → 恰好一次 spend-slot` 才算一档；monitor poll、scheduler ack、state refresh 全部 quota-neutral（不花钱）。安静 skip、preflight 失败、dry-run、重复记账都拒绝 spend；滚动窗口内可 void、可过期。

**安静是一等公民**：monitor-only lane 只允许 `monitor_due` 跑，否则 `effective_action="monitor_quiet_skip"`、`notify=DONT_NOTIFY`、`spend_policy="unchanged monitor polls are no-spend"`（`quota.py:1178-1388`）。配通知冷却（focus_wait 本身就是"用户专注期别打扰"）。"monitor-only 或 status-only 的 turn，除非发现状态变化，否则应该保持安静。"

### 2.4 Gate：人类判断是具体决策，不是打扰

Human-in-the-loop 不等于频繁确认。gate 是一等对象：**具体可回答的问题 + 被阻塞的路线 + 用户不回答时的安全默认 + 可以独立推进的旁路**（`loop-engineering-principles-and-pitfalls.zh.md`）。

- **Safe fallback 保持诚实**：P0 路线被阻塞时，审计过的 P1/P2 旁路可以推进，但 blocked 主线必须始终可见，fallback 必须显式标记——"不能把'旁路有进展'写成'gate 已解决'"。
- **五问可答**（最小可用 Loop）：当前目标与边界？哪个人类决策阻塞哪条路线？上一轮 agent 验证了什么？用户什么都不做时下一步是什么？哪类反馈会改变计划？——五问全部从状态内核可答，不从聊天猜。

### 2.5 typed todo / claim / handoff：交接不走对话走状态

- todo 是 **40+ 字段的 typed 对象**（`todos/contract.py`）：role(user/agent)、status(open/done/blocked/deferred)、task_class(advancement_task/continuous_monitor/user_gate/user_action/blocker)、claimed_by/bound_agent/blocks_agent/excluded_agents、required_write_scopes/required_capabilities、unblocks_todo_id/successor_todo_ids、resume_when(todo_done:/pr_merged:)、decision_scope、no_followup、monitor 元数据（next_due_at/result_hash/consecutive_no_change/material_change）……
- **"完成必须有 successor 或显式 no-followup"**（`todo_summary.py:1031-1052`）：completed_without_successor 会触发 TODO_SUCCESSION_GAP 重规划——机器可判定"下一步"不依赖模型自觉。
- **跨 agent 交接载体是 todo + public-safe evidence**（`multi_agent/contract.py:262-266`），不传对话；typed continuation 渲染成类型化新 todo（fresh/resume + child operations：codex-cli spawn_agent / resume_agent、claude-code Task）。
- **目标完成是机器判定**（`goal_frontier.py:169-189`）：validated_goal_closure + 空 frontier + source completeness，不依赖模型自评。

### 2.6 目标漂移治理：goal frontier 把"该做什么"变成可计算归约

`derive_goal_frontier_replan_obligation_from_summaries`（`goal_frontier.py:1502-1840`）按 9 条规则顺序决策（EXISTING_OBLIGATION → BLOCKING_HANDOFF_GATE → READY_DEFERRED_SUCCESSOR → OPEN_USER_TODO → TODO_SUCCESSION_GAP → VISION_ACCEPTANCE_GAP → LONG_TODO_CHAIN → MONITOR_NO_CHANGE_STREAK → MONITOR_FRONTIER_EXHAUSTED），漂移触发器共 8 类。**"该做什么"从模型判断降维成机器可计算的前沿归约**；目标变更必须携带 `path_delta` 且 `outcome=replan`（`goal_vision.py:437-442`）。

### 2.7 事故教训：长任务失败模式与修复（最有借鉴价值）

`docs/archive/incidents/` 记录了真实运行教训，三起事故的共同模式：

| 事故 | 失败模式 | 修复 |
|---|---|---|
| monitor-only-replan-stall (20260621) | 控制面反复记录 monitor/replan run，但 work frontier 从未改变；agent"看到像 action 的语言继续尝试，却得不到机器可验证的状态变化" | **"repair/replan 只有当下一个 agent 能看到不同的、可执行的 control-plane 状态才算完成"**——`repair_delta_contract_v0` 带 `delta_present=true` 才能 ACK；连续 6 次同目标无变化 poll → `dead_monitor_repeat` 强制修复 |
| agent-scoped-replan-precedence (20260703) | replan 压力与 scoped 无工作信号矛盾，agent 诚实保持安静而 goal 仍需要路由修复 | `autonomous_replan_obligation.required=true` 时 interaction mode 必须 `autonomous_replan_required`，安静等待不是 harder gate |
| agent-scoped-user-gate-overreach (20260624) | `blocks_agent=B` 的 gate 被 A 的 quota 当成 owner gate，A 有可执行工作却被卡死 | blocks_agent 指向他人的 todos 滤出当前 agent 的 blocking summary，保留为诊断项 |

**共同教训：长任务失败多半不是缺 prompt 细节，而是"下一个迁移归谁拥有"的边界模糊 + 机器投影与可执行真相脱节；修复一律是固化成机器可读的 typed contract（interaction_contract / repair delta / blocks_agent scope），而不是加提示语。**

### 2.8 无人值守 heartbeat 循环（长任务运行形态）

每轮固定 19 步 checklist：preflight → 拷贝 `LOOPX_TURN` 稳定 turn id → `quota should-run` → 读 `interaction_contract` 只执行 `primary_action` → 按 should_run 分支（operator_gate 发具体 NOTIFY、"Never say only 'owner gate'"；monitor_quiet 安静 DONT_NOTIFY 不 spend；safe_bypass 至多一个有界只读旁路步）→ steering audit → 选一个有界可验证段 → 最小验证 → 写回 → accountable refresh-state → **恰好一次** spend-slot。退避 `[30,60]` 分钟，`reset_token` 变化（用户回复/新 todo/门解开/证据变化）回到初始 cadence；unchanged poll 3 次 → final check → 仍不变才允许停。**cadence 变化与 final check 从不 spend quota。**

---

## 三、Seelex 长任务上下文管理现状

### 3.1 会话内：预算驱动的窗口外压缩（已实现，较强）

`seelexctx/controller.go` + `application/core/context_controller.go`：

- 软阈值 75% / 硬阈值 90% / 压缩目标 60% 的 token 预算策略（`ContextWindowPolicy`）；after_assistant/after_tool 钩子触发（`seelectx.ContextController` 接口，ReAct 循环唯一自动钩子）。
- **只压缩滑动窗口外轮次**，窗口内保留；压缩产物为 CompactFrame（SegmentID/From/To/Summary/Evidence）push 会话级 CompactStack，栈顶自足。
- **压缩可逆**：溢出轮次原文经 TurnArchiver 持久化，帧 Evidence 携带读回句柄，模型可经 `read_compressed_turn(segment_id=…)` 读回原文——"压缩丢失可逆"（`controller.go:396-408`）。
- 超大工具输出先归档为 result_ref（`ToolResultArchiver`，按调用 ID 幂等），仍超限才收缩窗口。
- 应用侧每请求前重建 provider 缓存：TaskCheckpoint（私有化有界检查点：决策/变更文件/产物/完成工作）+ planContextMessage（活动 Plan 投影：当前节点/已完成/失败/待办）+ fitExecutionHistory（transcriptTail 按目标预算裁剪），超预算直接拒绝发送（`context_controller.go:42-123`）。
- 巨型用户输入 out-of-band 存储 + contentReferenceWarning；history_safety 配对修复 tool-call/result。

### 3.2 跨会话/跨 agent：快照承袭（已实现，但只是"单次继承"）

`seelexctx` 五包：snapshot（ContextSnapshot：Goal/Decisions/Findings/Progress/Constraints/PendingWork/Escape）、provider（Provider/Mergable/Compactable 接口 + EngineProvider/HistoryProvider/TraceProvider）、compactor（三级压缩，token 预算驱动）、merger（MergeBack 双向合并，copy-on-write）、bridge（Export/Import 兼容 API，Import 前预算检查 TrimHistory）。

### 3.3 持久化

`sessionstore.SessionContextRecord`：SystemPrompt / PlanStack / TaskStack / SkillStack / CompactStack；ChatQueue append-only 单元索引作为压缩帧坐标空间（From/To 累计索引）。

### 3.4 与既有调研的衔接

`docs/2026-08-06-agent-landscape-research/` 已识别 seelex 缺口：N1 无失败回环、N2 无独立评审上下文、N3 无交付物协议、N5 headless 缺位、N6 跨会话记忆未落地；推荐 D1 自迭代试点（需求→coding→test→product）+ D2 verify 验证门 + D3 独立评审 + D5 失败分类护栏。**LoopX 的借鉴点正好是这些缺口的现成答案**（见第五节）。

---

## 四、对照表：同一问题，两种解法

| 问题 | LoopX 解法 | Seelex 现状 | 差距 |
|---|---|---|---|
| 长任务真相源 | append-only 事件流 + active goal state + registry；聊天只是执行上下文 | 会话历史 + SessionContextRecord（栈）+ TaskCheckpoint | **状态分散在"历史+栈+检查点"三层，无统一事件账本；"上一轮验证了什么"不可机器查询** |
| 上下文注入 | 分级投影决策载荷（frontier+channel+summaries），每层限长，模型不读事件流 | 窗口内保留 + 压缩帧前置 + checkpoint 注入 | 机制等价（都是"投影注入"）；缺 frontier 语义（下一步/阻塞/前沿计数） |
| 上下文丢失可恢复 | turn journal + turn_key 幂等续跑/重放；换模型/换会话不丢 | 压缩原文可读回（read_compressed_turn）+ 会话持久化 | **压缩内可逆；但无"跨 turn 幂等续跑"（没有 turn 身份与相位重放）** |
| 验证"上轮做了什么" | typed todo evidence/reason + run history latest_runs + vision checkpoint | task_check_node/task_complete 打点 + TaskCheckpoint.CompletedWork | 有打点协议；todo 是自由文本 PendingWork，无 typed 字段/claim/evidence 关联 |
| 该做什么/何时停 | goal frontier 机器归约（8 类触发器）+ 完成=机器判定 | Plan DAG 状态 + 逃逸信息（Escape），主要靠模型自评 | **"目标完成/漂移"无机器可判定归约**（Escape 是事后记录不是事前判定） |
| 人何时介入 | gate 一等对象（具体问题+阻塞路线+safe default+可独立推进） | 审批节点（approve）+ Escape 记录 | gate 是"等待"而非"具体决策+默认行为"；无 safe fallback 诚实性建模 |
| 算力/注意力成本 | quota 对已验证状态转移记账；monitor 安静 DONT_NOTIFY | Effort 预算（token 预算）+ 压缩阈值 | **无"验证过的转移"记账；无人值守时无静默/停止条件** |
| 无人值守循环 | heartbeat 19 步 + 退避 + reset_token + 3 次 unchanged 才停 | 无 headless/-p 模式（N5 缺口） | 未落地（已知缺口） |
| 失败处理 | 失败类型化 + 定点重试 + recovery_kind | verify 节点失败 → 无自动回环（N1 缺口） | 未落地（已知缺口） |
| 上下文漂移治理 | goal frontier + replan delta 契约（delta_present 才 ACK） | 窗口压缩（token 漂移）治理有；**目标漂移治理无** | 目标/前沿漂移完全靠模型自觉 |
| 重规划纪律 | "重规划必须改变机器可见前沿"（事故教训固化） | Plan 节点可改（/replan 文档） | 无"变更必须产生可验证差异"契约 |

---

## 五、值得借鉴的点（按优先级，标注与既有试点的衔接）

### 高优先级（试点 D1/D2 直接受益，改动面小到中）

**L1. gate 升级为一等对象：具体问题 + 阻塞路线 + 安全默认 + 可独立推进的旁路**
- 现状：seelex 审批节点只有"等待确认"语义；EscapeInfo 只有事后逃逸记录。
- 落地：approve 节点/人工门扩展字段——`{question, blocked_route, safe_default, allowed_parallel}`；Plan DAG 中 blocked 路线持续可见（GUI 标记），旁路节点显式标 `fallback_of=<route>`。
- 对齐：D1 试点的人工门（需求验收标准确认）；原则文档 `loop-engineering-principles-and-pitfalls.zh.md` 的 Human gate 与 Safe fallback 两节可直接引用为设计规范。
- 改动面：Plan DAG 节点 schema + GUI 显示，S–M。

**L2. "五问"可见性契约：把 check 点的必答字段固化**
- 现状：TaskCheckpoint 有 Goal/Decisions/Progress/CompletedWork，但缺"被阻塞的路线""用户什么都不做时的下一步""什么反馈会改变计划"。
- 落地：TaskCheckpoint/Task 终态协议（task_check_node/task_complete）增加可选字段 `blocked_routes`、`next_if_idle`、`feedback_changes_plan`；压缩帧 Summary 模板同步（controller.go summarizeOverflow 加段）。
- 改动面：snapshot/checkpoint schema + 渲染，S。这是"长任务可管理性"的零成本起点。

**L3. verify 门独立验证器 + 失败类型化（recovery_kind）**
- 现状：D2 已规划 verify 接真实测试命令（exit code 为唯一权威）；LoopX 给出完整形态：trusted argv-only validator、结果分 passed/progress/failed/inconclusive、failed 必须带 recovery_kind（repair_required/replan_required）、失败绑相位可定点重试。
- 落地：verify 节点输出协议增加 `{verdict, recovery_kind}`；失败回环先重跑确认（D5 分类护栏）再按 recovery_kind 路由（修复节点 or 重规划节点）。
- 改动面：verify 节点协议 + 回环路由，S–M；与 D2/D5 合并实现。

**L4. "重规划必须改变机器可见前沿"纪律（事故教训直接复用）**
- 现状：/replan 文档存在（2026-07-29-replan），但无"变更必须产生可验证差异"契约。
- 落地：replan 提交时校验 Plan 状态/节点集合确实变化（delta_present 语义）；未变化的重规划打回。顺带解决"模型假装重规划实际空转"的循环（对照 monitor-only-replan-stall 事故）。
- 改动面：replan 校验钩子，S。

### 中优先级（工作台/无人值守阶段）

**L5. quota 语义：对"已验证的状态转移"记账，保护用户注意力**
- 现状：Effort 预算管 token；无"注意力预算"概念。无人值守试点（C1 headless）一旦落地，没有停止条件会无限消耗用户信任。
- 落地：headless 模式加 `--max-deliveries`（完成并验证的节点数上限）+ 静默轮规则（monitor 轮无状态变化不 NOTIFY 不计数）。先对齐 Claude `-p --max-turns` 形态，quota 记账语义作为第二步。
- 改动面：headless 参数 + 计数，M；与 C1 合并。

**L6. typed todo：claim/ownership/unblocks/successor 字段**
- 现状：PendingWork 是自由文本；workbench 子代理有任务卡片但无所有权协议。
- 落地：TaskFrame/PendingWork 升级为 typed 结构（status/claimed_by/unblocks/successor/no_followup）；"完成必须有 successor 或显式 no_followup"作为 workbench 校验。
- 改动面：sessionstore TaskFrame schema + workbench 校验，M。解决"子代理完成但无人接手"（对齐 TODO_SUCCESSION_GAP）。

**L7. goal frontier 最小版：Plan 状态的机器可判定归约**
- 现状：Plan DAG 有 completed/failed/pending 状态——**机器归约的输入已经存在**，缺的是"当前前沿是什么、有没有漂移触发器"的判定层。
- 落地：新增 plan_frontier 判定：pending 节点数 + verify 失败重试次数 + 长链阈值（对齐 LONG_TODO_CHAIN 15/20）+ 无 successor 完成 → 输出"需要重规划/需要人介入"信号到 GUI/checkpoint。**这是纯增量（读现有 Plan 状态即可），是 L1/L2/L4 的承接层。**
- 改动面：application/core 判定层 + GUI 徽标，M。

### 低优先级 / 暂不借鉴

**L8. 事件溯源 JSONL**——seelex 已有 sessionstore + ChatQueue append-only，收益有限，成本高（迁移全部存储）。**不借鉴**，但可以借鉴"事件不可变 + 冲突检测"的思想于现有 ChatQueue（已有 append-only 语义，够用）。

**L9. TurnEnvelope 签名防篡改**——seelex 无"控制面判定注入模型"的场景（判定在模型上下文内），不适用。等 headless/quota 落地后若判定外置再考虑。

**L10. 多 agent mutation authority**——seelex 单用户本地 harness，注册表授权属于过度设计；workbench 子代理用 `claimed_by` 字段即可（L6 已含）。

### 工作方式借鉴

**L11. incident 文档文化**：loopx 的 `docs/archive/incidents/` 每起事故 = 失败模式 + 机器可验证修复契约。seelex 的 devlog 是流程记录，缺"失败模式→契约修复"结构。建议自迭代试点开始后，把每次"试点失败的运行"写成 incident（格式：现象/失败模式/修复契约/验证），比技术博客更沉淀长期价值。S，纯文档。

---

## 六、不建议照搬的部分（成本/收益分析）

1. **完整事件溯源层**：seelex 是单进程本地 harness，聊天 + sessionstore 已提供足够一致性；事件溯源是为"多 agent 并发写控制状态"服务的，seelex 的并发写场景（workbench 子代理）可用 claim 字段解决。
2. **quota 24h 分钟槽账本**：本地无人值守（C1 headless）只需要简单计数上限；分钟槽账本是为云端/长周期调度设计的。
3. **heartbeat 19 步 checklist**：是给"不可信外部 runtime 当 worker"设计的防御形态；seelex 是自托管 harness，控制面与执行体同进程，可以简化。

## 七、来源索引

- LoopX 源码（gitcode 镜像 clone，commit 4452d53）：`loopx/event_sourced_state.py`、`loopx/control_plane/goals/goal_frontier.py`、`loopx/control_plane/todos/contract.py`、`loopx/control_plane/todos/mutation_authority.py`、`loopx/control_plane/turn_driver/{executor,driver,transaction,codex_cli}.py`、`loopx/quota.py`、`loopx/control_plane/quota/{turn_envelope,slot_accounting,states,decision_summary}.py`、`loopx/control_plane/handoff/*`、`loopx/control_plane/agents/*`
- LoopX 文档：`README.zh-CN.md`、`docs/product/foundations/loop-engineering-principles-and-pitfalls.zh.md`、`docs/state-interaction-model.md`、`docs/quota-allocation.md`、`docs/integration.md`、`docs/status-data-contract.md`、`docs/heartbeat-automation-prompt.md`、`docs/product/core-control-plane/state-definitions.md`、`docs/operations/long-task-cadence-policy.md`、`docs/archive/incidents/{20260621,20260703,20260624}.md`、`docs/capabilities/{decision-context,agent-turn-recall}/README.md`
- 公开报道：[dev.to 介绍](https://dev.to/arshtechpro/loopx-a-control-plane-for-ai-agents-that-have-to-keep-working-for-days-47n)、[53ai 报道](https://www.53ai.com/news/OpenSourceLLM/2026080547891.html)（200 小时长任务）、[KuCoin 报道](https://www.kucoin.com/zh-hant/news/flash/bytedance-engineer-open-sources-loopx-for-long-running-ai-agents)
- Seelex 现状证据：`seelexctx/{controller,snapshot,compactor,merger,bridge}.go`、`seelexctx/provider/*.go`、`application/core/context_controller.go`、`sessionstore/session_context.go`、`docs/2026-07-30-context-summary/code-changes.md`、`docs/2026-08-06-agent-landscape-research/research-agent-landscape-2026-08.md`

> 说明：LoopX 版本为 0.4.x 早期软件，作者自述"不是生产自动化控制器，危险权限/生产写入/公开发布/最终 ownership 仍由人类负责"；本文借鉴的是其**控制面设计**而非其产品完整度。厂商自报数据（200+ 小时轨迹等）标注为公开报道，未独立验证。
