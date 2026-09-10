# 调研：在线文档协作维护模型 → Seelex 群聊会话（2026-09-10）

> 用途：为 R3（会话中间缓存）与 R4（群聊角色 draft/sequencer）选择可借鉴的
> 在线文档协作模型。结论已并入 `docs/2026-09-10-backend-cache-goal-session/prompt.md`
> 与 `docs/2026-09-08-session-storage-architecture/my_design.md` §8.3。
> 来源为公开资料（2026-09-10 检索），不复制第三方文档原文。

## 1. 问题定义

Seelex 群聊会话要满足：

- `message` 是 **append-only 在线文档**（唯一正文权威）；
- 多角色（user/main/tl/agent-team）**并发产出**事件行，各自先写 role draft；
- 需要一个**确定的全局顺序**（message `seq`）与“当前发言人”（floor）；
- 支持桌游式顺序（goal 固定循环 / user+main 编排）与**定时任务式插话**；
- 压缩（compact）是文档的**快照**；角色恢复要能从快照 + 未同步草稿 + `join_seq_id`
  正确重建各自上下文；
- 前端需要**在线状态/成员面板/顺序编排**的协作体验。

## 2. 公开方案与可借鉴点

### 2.1 服务端权威 + Operational Transformation（OT）

- 代表：Google Docs 类协作文档、ShareDB（其首页自述为 “Realtime database
  backend based on Operational Transformation (OT)”）。
- 做法：客户端提交操作，服务端做变换并广播；服务端最终决定顺序与结果。
- 可借鉴：**顺序权威在服务端**；客户端只提交操作、不直接改权威文档；
  连接/提交存在 ack 与重试语义。
- 不直接采用：Seelex 的文档是 append-only（没有任意位置文本编辑/变换需求），
  OT 的 transform 机制属于过度设计。

### 2.2 CRDT（Yjs / Automerge 等）

- 做法：操作可交换/可合并，离线编辑也能收敛；无需中心化排序。
- 可借鉴：**presence/awareness**（Yjs Awareness 文档：成员在线状态、光标等
  轻量、易失信息与文档本身分离）；离线/断连后的重连合并思路。
- 不直接采用：聊天/事件流需要**单一可读的全局顺序**（UI 行序、compact 区间、
  LRU watermark 都以它为准），CRDT 的偏序合并会引入“同序不同视图”的复杂度；
  且 CRDT 元数据会放大存储体积，与“减少存储消耗”目标冲突。

### 2.3 追加日志 + 快照（event sourcing / log compaction / streams）

- 代表思想：Kafka 的 log + compaction、Redis Streams 的 XADD + 消费位点、
  事件溯源里的 snapshot + replay。
- 可借鉴：**op-log 追加 + 周期快照**；消费者/读取方用位点（applied_seq）恢复；
  快照可由日志重放重建；日志只追加、不原地改。
- 与 Seelex 对应：message 行 = op-log；compact 帧 = 快照；
  `join_seq_id`/`compact_ref.applied_seq` = 读取位点；LRU watermark = 可回收前缀。

### 2.4 Presence / Awareness（在线状态）

- 代表：Yjs Awareness 一类“易失状态”通道（在线成员、光标、当前编辑者）。
- 关键原则：**presence 与文档正文分离**、可丢失、不参与文档一致性判定。
- 与 Seelex 对应：Agent Team 面板的 online/offline、当前发言高亮属于运行态；
  不写 message 正文；冷启动由“角色会话是否存在 + 运行期 actor 状态 + floor”重建。

## 3. 方案对比

| 维度 | OT（服务端权威） | CRDT | 追加日志 + 快照 |
|---|---|---|---|
| 顺序权威 | 服务端 | 无中心（偏序） | 单写者/日志序 |
| 适合 append-only 文档 | 一般（变换多余） | 过度 | **最贴合** |
| 恢复方式 | 服务端重放/快照 | 合并状态 | **快照 + 位点重放** |
| 元数据/存储放大 | 中 | 高 | 低 |
| 与 row 级 seq/LRU 兼容 | 需额外映射 | 复杂 | **天然兼容** |
| 定时插话/角色顺序 | 服务端可编排 | 需额外协调 | **sequencer 可编排** |

## 4. 推荐模型（映射到 Seelex）

采用“**服务端权威 sequencer + 角色 append-only op-log + 周期快照 + 易失 presence**”：

1. **角色 op-log**：`role_session/draft/<role_name>.jsonl`（每角色独立锁/actor，
   未同步 WAL；提交成功即删，等价于客户端 op 的 ack 清理）；
2. **sequencer = 服务端权威**：按 `round_id → role → unit_seq` 排序，
   分配 message 全局 `seq`，发布 message head（含 floor），回写锚；
3. **快照 = compact**：main 产生帧（快照），角色只同步 `compact_ref`
   （frame_id + applied_seq），不产生第二份快照；
4. **位点恢复**：角色恢复 = 快照 + `applied_seq` 之后的已发布行 + 未同步 draft；
   `join_seq_id` 决定角色可见起点（等价于读取方从某个位点开始订阅）；
5. **presence 易失**：Agent Team 在线状态、当前发言高亮、顺序编排面板，
   与 message 正文分离；不写 message、不参与一致性判定；
6. **定时插话 = 调度器注入 op**：`schedule.*` EVENT + 运行期 timer；
   到点产生 op（role draft），由 sequencer 在下一个单元边界插入；
7. **R3 缓存 = 物化视图**：缓存已 sync 的 wire 形态（等价于编辑器本地视图状态），
   以 message head revision/applied_seq 失效；未命中可从 op-log + snapshot 重建。

## 5. 对 R3 缓存的具体含义

- 缓存粒度建议 = **每角色会话一个物化视图**（wire 形态：compact 摘要 + tail 行 +
  自身未同步 draft 的 pending 呈现）；
- 失效键 = `message head.commit_id/revision`、`compact frame_id/applied_seq`、
  `floor/round_id`；
- 写路径：sync 成功后更新缓存并删除 draft；compact 后丢弃旧视图；
- 不采用 CRDT 的“每个字符/操作都带元数据”方案；R3 缓存只是读取加速，不是第二事实源。

## 6. 风险与待决

- presence 的在线判定（进程内 actor 状态 vs 心跳/租约）与 §9 单写者前提的交互；
- 定时任务的时间源与漂移（本机时钟/重启补发策略）；
- 角色数量上限与缓存内存上限（§11）；
- 多进程只读共享仍未表态（当前 I10 单写者前提）。

## 7. 参考（公开资料，2026-09-10 检索）

- Yjs Docs — Awareness（易失 presence/awareness 与文档分离）：
  https://docs.yjs.dev/api/about-awareness
- ShareDB — 首页自述 “Realtime database backend based on Operational
  Transformation (OT)”：https://share.github.io/sharedb/
- Automerge Docs（CRDT）：https://automerge.org/docs/
- 通用工程模式：event sourcing（append-only log + snapshot + consumer offset）、
  Kafka log compaction、Redis Streams（XADD + consumer ack）——作为“日志+快照+位点”
  的公开做法参照，不在本仓库复制其实现。
