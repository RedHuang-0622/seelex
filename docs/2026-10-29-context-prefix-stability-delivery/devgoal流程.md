# Workflow: 上下文前缀稳定性 M2–M5

## 元信息

- 日期：2026-10-29
- 规模：深度
- 需求：完成开发文档剩余全部里程碑，同时收口交接单点名的高风险接缝。
- 非目标：重做 M0/M1；修改 Seele 依赖源码；把 L1 持久化为事实源。

## G: Goal

主目标：让同一会话的健康轮次严格保持 append-only 字节前缀，所有失效都由显式纪元事件解释，并能从持久 truth 逐字节恢复。

成功标准：

- M2–M5 验收用例均先红后绿。
- 同一键下 `UnexpectedMiss == 0`。
- 压缩是唯一允许整段前缀替换的热路径事件。
- 冷加载重建与热前缀逐字节一致。
- draft/并发代码跑 `-race`。
- 不新增 Store 上帝方法，不修改 vendor，不越装配预算。

## O: Options

采用 owner 内快照 + 显式 actor（见 `plan.md` 方案 A）。新能力分别归 `kernel/context`、`kernel/task`，装配层只做窄端口接线。

## A: Action

按 M2、M3、M4、M5、旁支顺序实施；每个里程碑独立验证，不自动 commit/push。

## 当前 Todolist

> 口径：以当前工作树为准。旧交接里的 `integ/pause-continue` / `keep/*` 只作来源参考；
> 已在当前实现中覆盖的能力不重复合并，禁止未验收的 `team_accept`。

### A. 已在当前实现中落定，待最终回归

- [x] M0 字符探针与 M1 user/plan/task 三帧字节冻结保持不动。
- [x] M2 L1 派生快照：六格 `prefix_key`、连续 Seq 增量复用、缺口/校准/压缩显式重建。
- [x] M2 删除回合收尾 `ReleaseWorkingHistory` 热路径，保留会话卸载生命周期。
- [x] M2 正常装配从 `truth[retained_from:]` 直取；有界窗口只用于确实装不下与压缩边界决策。
- [x] M2 冷加载丢弃 L1 后从 truth 重建，现有用例断言重建历史逐字节一致。
- [x] M7 子代理稳定块与 plan/task 尾部块拆开，尾部块追加在 WorkingHistory 之后。
- [x] 续跑持久化字段映射到 `SessionRecord.Execution` 并从冷加载恢复。
- [x] `goal.pause/continue/resume` 审计接线到 goal append-only 账本。
- [x] M3 独立 draft actor：调用方分配 Seq、单写者、FIFO、有界背压、幂等、ack 水位。
- [x] M3 `ContextDraftState` 随会话记录持久化字段与冷加载恢复面。
- [x] M4 memory append-only 行、Digest 校验、epoch fold、同一 epoch 只选一次的 Runtime 快照。
- [x] M5 session catalog 冻结、稳定排序、`tools_epoch`、可用性变化不改变前缀键。
- [x] M5 `⟦toolset⟧` 渲染/解析、截断报错、system 固定“最后一块为准”规则。

### B. 当前里程碑收口项

- [x] M2 冷加载：L1 从持久 wire material 重建的逐字节用例已落地。
- [x] M2 性能：N=10/50/200 热路径用例记录 hit、delta 与 rebuilt-message 证据。
- [x] M2 文档：`kernel/context/README.md`、`kernel/task/README.md`、主设计 §8 与 devlog 已同步。
- [x] 续跑行为门禁：重建后暂停仍不续、同一中断点累计仍只续一次已由行为用例钉住。
- [x] 暂停/继续 E2E 六条：push 子会话即时中断、pull 轮边界不续、Resume 恰发一次、
      重建后仍暂停、goal done 后不续、新会话不读脏暂停位。
- [x] M3 接线：draft durable 适配器接到回合尾提交与真实持久化，提交失败保留 `uncommitted`。
- [x] M3 验收：并发无丢失/无乱序、重复 Seq、队列满背压、故障后下一轮合并提交、按水位重放。
- [x] M4 压缩滚动：epoch 从 c1→c2 时写 retire/add；跨压缩 memory 字节一致用例已落地。
- [x] M4 用例：同 epoch 换 query 前缀不变；手改 Bytes/Digest 必须拒绝。
- [x] M5 运行时接线：可用性变化只追加哨兵，不改变冻结 schema/`tools_digest`。
- [x] M5 catalog 变更：显式 catalog 变化才 bump epoch，并刷新冻结 schema。
- [x] M5 工具门：被当前门移除的工具返回含 tool epoch 的结构化拒绝。
- [x] M5 用例：哨兵“最后一块为准”；截断/重复声明解析失败。
- [x] 契约/README：`SessionRecord.Execution` 新字段、ContextDraft/epoch 表、恢复语义已同步。

### C. 子代理会话治理（新增）

> DDD 复用门禁：先认领既有 owner，再补缺口。当前工作树里已有用户未提交的
> `agent-conversation-view.js`、`composer-shell.js`、`contract.AgentConversationPort`、
> `application/conversation/unified.go`、`RoleSessionsPort` 与 subagent live/session 读面。
> 这些文件保持用户改动不动；后续只在对应 Application API / DTO 上扩展，禁止新建第二套视图或状态机。

- [x] 既有实现盘点：subagent 走 `AgentConversationPort` / `ConversationView`，
      teammate 走同一端口 + `RoleSessionsPort`，实时流复用 `SubscribeSubagentLive` /
      `SubagentLiveHistoryPage`，历史复用 `NodeSessionConversation` / `SessionRecordPage`。
- [x] 输入框复用：`composer-shell.js` 已把主 composer 模板克隆到当前 agent 会话，
      `agent-conversation-view.js` 已按 agent ref 固定提交目标；不再复制第二套 composer。
- [x] 子代理会话探针验收：补齐 subagent/teammate 两种 ref 的会话作用域测试，
      断言主/子 composer、提交目标、live→cold 切换和错误恢复；不重复实现 UI。
- [x] DDD owner 复核：确认 `ConversationSessionRef` 是唯一路由键，Application 只消费
      DTO/Event，Bridge 不持有 subagent/teammate 会话状态；缺少的只是端口方法时优先扩现有端口。
- [ ] 子会话提交权限与锁序：主会话运行中不阻塞子会话输入；子会话停止/重跑/关闭状态可判定。
- [ ] 子代理“天生 goal”：创建 subagent/teammate 即建立 goal，不以普通回合完成作为终点；
      只有 `goal_done` 或显式 abort/失败策略才能退出。
- [ ] 子代理 goal 生命周期：未完成自动续跑沿用同一两层状态与 pull/push 语义；
      需要独立 `PausedGoalID`、attempt ledger 与 stalled 读面。
- [ ] 主会话 goal 对齐：主会话现有 goal 收口后，将既定工作目标统一投影/迁移到对应子代理会话，
      避免主 goal 与子 goal 各自维护不同真相。
- [ ] 子代理 goal 完成自动提交：`goal_done` 后由子会话收尾事务持久化，
      再触发父会话/团队作业完成通知；重复通知幂等。
- [ ] 团队边界用例：主 goal done、子 goal 未完；子 goal done、主会话尚未收尾；
      暂停后重启；子代理与 teammate 并发完成等交叉状态。

### D. 危险操作/交付纪律

- [ ] 不执行 `team_accept`，直到对应改动已合入或已打 `keep/*` 保护分支。
- [ ] 不碰用户现有 GUI 改动；本轮 GUI/协议改动单独列文件面。
- [ ] 每个里程碑先红后绿，交付写删除/合并清单和结构性证据。
- [ ] 只跑直接相关包；全量回归只在收口时跑一次。
- [ ] 不在共享账号上无限重试；无产出且 loop 持续增长时按失败处理，不伪装成长跑。

## L: Learning

最终回填各阶段实测、删除清单、验证结果与遗留风险。
