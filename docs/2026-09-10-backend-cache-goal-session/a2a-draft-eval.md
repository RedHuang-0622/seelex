# 评估：goal A2A「全员先写 draft、再由 sequencer 同步 message」是否划算

> 关联：[`prompt.md`](./prompt.md) 的 R2（techleader 独立存储）与 R3（中间缓存），
> [`review.md`](./review.md) 的性能判据。当前提交 `45d35f6`。
> 用户背景：goal A2A 是原型；设计空间预留给后续 agent-team 的“桌游式”调度。

## 1. 现状（代码事实）

- **b（TechLeader）目前无会话存储**：`seelebridge/runtime_goal_tl.go` 的
  `goalLLMEvaluator` 直接 `completer.Complete(...)` 做一次有界评审，产出
  `TLDirective`；没有自己的 session/engine/消息通道。
- **同步写主会话的串行点**：`application/core/chat.go` 在回合结束/开始时把 TL
  指令注入主会话受信区（`goal A2A：把上一回合 TL 产生的指令注入本回合受信区`），
  这些内容随主会话的 message/EVENT 提交落盘——即与主会话的 `messageMu`、
  head 发布（fsync + rename，≈40–64 ms/次）以及 EVENT 提交同步串行。
- **现有队列/草稿语义**：`session/input/draft.json` 是 1:1 整份替换型；
  `session/queue/queue.jsonl` 是 FIFO 整份替换型（§2.5.1/§2.5.2）。它们目前是
  “引擎待发送请求”的单通道，没有 role 维度，也不能表达多角色并发草稿。
- **栈与缓存现状**：goal stack 条目已带 `item_message_id/seq` 锚；message
  区间/单元语义（`CompleteEventUnits`）已固定“assistant tool_call + tool 结果 +
  final LLM”的顺序规则；R3 缓存尚未落地，且 S20 之后 record/翻页派生读存在
  全量扫行放大（见 review.md §2）。

## 2. 提案（把用户设想具体化）

**全员先写 draft，再由唯一 sequencer 同步到 message**：

1. user / main-agent / techleader 各自把本角色的输出按事件行写进**本角色的
   draft 通道**（append-only、role 隔离），包括 assistant 的 tool_call 行、
   tool 结果行、final LLM 行，以及 message_id / 单元序号；
2. 一个**单写者 sequencer**（会话级 actor）按确定性顺序把 draft 批量 append 到
   message 通道（一次 sync = 一批行 = 一个 commit_id）；
3. seq 编排：为 draft 行携带 `round_id`（因果轮次）+ `role_name`+
   `role_session_id`+`unit_seq`；sequencer 决定跨角色顺序并映射到 message 的全局
   `seq`，再回写 goal stack 的 message 锚。

新落盘形态建议（须先过 my_design §2.0 准入门并登记 §4 术语表）：

- 角色 draft 放 **goal 子树**（R2 形态）：
  `session/goal_<hash>/draft/<role>.jsonl`（append-only），或
  `session/goal_<hash>/message/...` + 角色 draft 仅作未同步缓冲；
- 主会话只保留同步后的 message 行 + `goal.*` EVENT 摘要 + goal stack 锚；
- **不要改 `input/draft.json`/`queue.jsonl` 的单通道语义**（它们是整份替换型，
  加 role 维度会改变既定通道形态）。

## 3. 收益：哪些热点会降

### 3.1 主会话锁与提交次数

- b（以及未来的其他角色）不再触碰主会话 `messageMu`/head 发布；每轮 A2A 对
  主会话的提交从“每个角色/每段一次”收敛为 **每个 round 一次 sync 批提交**。
  以当前 message 提交地板 ≈63.7 ms、head rename ≈30 ms/次计算，一次 round
  若由 2–3 次提交降为 1 次，主会话临界区内的提交次数与锁等待按比例下降。
- 代价是 sequencer 的批内 IO 字节数不变（仍要写同样的行），但
  **fsync/rename 次数减少**；这正是“减少存储 IO 热点”的主要来源。

### 3.2 并发与“桌游式”扩展

- 角色 draft 各自独立 append（独立锁/独立文件），main-agent 与 b 可
  并行产出；只有 sequencer 串行。后续 agent-team 只要扩充角色与调度策略，
  不需要改 message/EVEVT 的写形态。
- 顺序成为**数据属性**（round/role/unit_seq）而不是调用时序；为回放、
  重排、审计、fork 提供统一坐标。

### 3.3 与 R3 缓存协同

- 缓存可直接缓存“已 sync 的 wire 形态”（含 compact 结果）；draft 未同步部分
  由 sequencer 队列持有，避免了“读主会话全量行再组装”的放大（review.md §2）。
- 命中路径：cache → wire；未命中：draft+message+stack → 组装，语义可验证一致。

## 4. 代价与风险：哪些不会变快、哪些是新增成本

### 4.1 不能消除的

- **每批 sync 仍有一次 fsync 数据 + rename 发布 head**（message ≈63.7 ms 地板，
  EVENT ≈40 ms，head ≈30 ms）；除非同时做 group commit / 批量发布形态改造
  （H2/H3/H4 专项），否则单批提交的常数地板不降。
- **H2 的读写共用 `messageMu`**：sequencer 仍是该锁的唯一写者，读侧若仍全量读，
  91.7 ms 级等待仍在；必须与 message 读写专项一起改。

### 4.2 新增成本

- **写放大（字面 2 份）**：同一内容先写 draft 再写 message，磁盘字节约 ×2
  （除非 draft 仅在崩溃恢复窗口保留并同步后回收——需要定义保留与清理）。
- **seq 编排复杂度**：跨角色因果关系、同角色 unit 顺序、LRU/watermark 空洞、
  fork 截断、compact 覆盖区间都要在 sequencer 里正确映射；错误会直接破坏
  “message=正文事实源”。
- **恢复与幂等**：draft 未同步时崩溃的重放、半同步（draft 已写、message 未发布）、
  sequencer 重启后的续排，都要靠 commit_id/round 幂等与恢复扫描兜底。
- **新通道准入门**：角色 draft 是新增落盘产物，必须先改 §2.0 清单、登记 §4
  术语表与 I9/写锁，再补 T-A2A-*；不能靠 `input/draft.json` 蹭。
- **改造面大**：涉及 seelebridge（b 升级为真实角色会话）、application/core/goal
  （sequencer/调度）、sessionstore（draft 通道 + 同步提交 + 锚回写）三层；
  这是用户所说“大工程”的准确来源。

## 5. 划算判断（分场景）

| 场景 | 判断 |
|---|---|
| A2A 仍是“单 b、每 3–5 轮一次、小评审文本” | **暂不划算**：主热点是 message 提交地板与 H2 锁等待，draft+sequencer 只把提交次数从 1 降到 1（省不了），却引入新通道与 seq 编排；建议先做 R3 读侧与 H2/H3/H4 专项 |
| A2A 演进为“每轮多角色、b 有状态会话、可并行产出” | **划算**：主会话提交从每角色/每段一次收敛为每 round 一次；b 完全离开主锁；顺序数据化，直接支撑后续 agent-team 桌游式调度 |
| 要做 agent-team 原型/长期方向 | **先做基建**：R1（清旧后端）→ R2（b 独立会话与 role_name/session_id 锚）→ draft/sequencer 通道与 T-A2A 门禁 → R3 缓存；每步用同轮 A/B 证明提交次数/锁等待/加载时间下降，再扩角色 |

**一句话结论**：这个方向在“多角色、有状态、并发产出”的目标形态下是**划算**的，
它能把热点从“主会话被多角色同步提交串行占用”转为“单 sequencer 批提交 + 角色并行
draft”；但在当前单 b 原型流量下，复杂度/写放大/新通道准入的成本会超过收益。
建议按目标形态先做 R1/R2 与最小 draft+sequencer 骨架，并用量化门禁决定是否扩面。

## 6. 建议的最小落地顺序与验收（若决定做）

1. **R1 清后端**（降维护/构建成本，防干扰）。
2. **R2 b 独立会话**：techleader 从 stateless completer 升级为 role 会话
   （独立 engine/actor、独立 session 目录），goal stack 条目加
   `role_name` + `session_id` + 锚；主会话只留 `goal.*` EVENT。
3. **draft+sequencer 骨架**（新通道先过 §2.0 准入门）：
   - T-A2A-01：同一 round 内 user/main/TL 的 draft → message 行序稳定，
     assistant tool_call→tool 结果→final LLM 的单元顺序与 `CompleteEventUnits` 一致；
   - T-A2A-02：角色写 draft 不触碰主会话 `messageMu`；sync 单写者、批提交一个
     commit_id；崩溃重放不重复（head last_commit_id/读侧高 revision 胜出）；
   - T-A2A-03：seq 编排在 LRU 空洞、fork 截断、compact 覆盖下仍可重建；
   - T-A2A-04（性能，同轮 A/B、median）：每 round 主会话提交次数/fsync+rename
     次数、`messageMu` 等待、加载读取分片数全部下降且 race 全绿；
   - 指标：draft 保留字节、sync 批量、角色并发度、cache hit、内存上限。
4. **R3 缓存接线**：缓存已 sync 的 wire 形态，命中不走全量扫行；未命中可回退。

## 7. 2026-09-10 用户裁决（取代上一轮待定项）

1. **draft 同步后即删**：无保留窗口；message head 发布 = 同步成功 = 删除；
   失败保留、崩溃重放、commit_id 幂等（落地口径见 prompt.md §4/§6.1）。
2. **群聊顺序**：`user → main → tl → main → tl → …` 交替，直到 TL 判定结束
   （`verdict_done` / `escalate_human` / 终态 gate 通过）。
3. **compact 以 main 为准**：main 生成帧与 frame_id；tl 只做同一帧的引用/备份；
   tl 会话定位 = TL 的备份 + 冷恢复来源，不是主上下文权威。
4. **角色 draft 统一流程**：user/main/tl/未来 agent-team 角色同一 draft→sequencer
   →message 路径（群聊模式）；message 是 append-only 在线文档；
   **不得为任何角色开专属旁路**。
5. 字段定名：`role_name` + `role_session_id`；goal stack 保留
   `item_message_id`/`item_message_seq` 锚。
6. **顺序指定**：goal 阶段固定循环 `user→main↔tl` 直到 TL 判定结束；
   agent-team 阶段由 user 与 main 共同决定群聊顺序（前端交互，
   只更新 `lifecycle.order_policy`/`order_roles`）；前端必须做角色区分。
7. **定时任务式插话必须支持**：EVENT `schedule.*` + 运行期 timer；触发角色
   经 role draft→sequencer 在下一可插入边界同步，不绕过 sequencer。
8. **draft 装配分域**：user draft → 输入框；其他角色 draft → 各自上下文；
   未 sync 前不进主文档。
9. **恢复坐标**：角色记 `join_seq_id`（可见起点）；compact 权威在 main，
   帧发布后把 `compact_ref` 同步到各角色会话；角色冷恢复 = 自身备份 + WAL +
   `compact_ref` + `seq > join_seq_id` 的已发布行。
10. **角色 draft 锁/actor 独立**：user/main/tl/agent-team 各持一把 draft 锁或
    actor 闭包，不共享；写 draft 不触碰 message 锁；只有 sequencer 持 message
    写锁。
11. **floor（当前发言角色）**：唯一写者 = sequencer；落 `metadata/message.json`
    的 `floor` 字段（随 message head 原子发布，零额外 IO）；不另开 metadata
    文件、不额外 rename。反例：单开 floor.json / 每轮额外替换 lifecycle head
    会新增 ≈30–63 ms 的 rename 地板，不做。
12. **R1 = 方案 A**：保留后端枚举 + `Open` 显式 retired/unsupported 错误；
    旧实现删除，新实现待按接口重写。
13. **R3 参考在线文档协作模型**：采用“服务端权威 sequencer + 角色 op-log +
    周期快照（compact）+ 易失 presence”；调研见
    `docs/research/2026-09-10-collaborative-doc-session-model.md`。
14. **前端框架**：右侧栏「状态 → Agent Team」子页（成员在线状态、当前发言、
    工作顺序栏由 main 编排、定时任务 agent 单独分区不入顺序）；字符画见
    `prompt.md` §7。

仍待用户裁决的只剩 R1 后端枚举处理（A/B）与 R3 缓存粒度/淘汰/与读写专项先后
（见 prompt.md §6.2）；R4 已无阻塞性待决项，可直接按 §6.3 默认推进。

> 同步更新：以上 R4 约定已写入 `my_design.md` 的 §2.0 准入门、§3 ER 图、
> §3.1/§3.2、§4 术语表与不变量 I16–I19、§5.1/§5.3 装配契约、§8.3 新章与
> §11 配置键；实现与测试以该稿为准。
