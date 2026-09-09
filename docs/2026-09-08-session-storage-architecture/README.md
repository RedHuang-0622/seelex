# 会话存储架构（总览，2026-09-09 统一口径）

> 状态：**设计稿，未实现**。代码与测试是最终事实源。
>
> 本 README 是总览与入口；**权威明细 = [my_design.md](./my_design.md)（v8.1）**——
> 术语、端点约定、不变量与 R2 装配读取器契约都以 my_design 为准；若与本文冲突，
> 以 my_design 为准。实施将另开对话按契约进行，本文不再承担实现细节。

## 0. 结论先行

1. **message = 会话唯一正文事实源**：条目 = 事件行（当前代码约定），append-only；
   assistant 的 tool_call 与 result 同一逻辑轮次但仍是独立事件行。
2. **EVENT = 结构性操作摘要**：压缩/fork/subagent/中断等，**不参与模型装配**、
   不承担幂等计数；重试/失败由运行期尝试缓存承载。
3. **两种上下文**：前端/历史上下文 = message 全量；LLM wire 上下文 =
   compact 摘要 + 最新尾窗 + 同一操作最近 K 条尝试（默认 K=3）。
4. **compact = 派生裁剪视图**；LRU 淘汰需用户确认，删除只发生在 watermark 之前，
   message_id/seq 保持空洞不重编号。
5. **metadata 模块化**：`session/metadata/guide.json` 只做读索引/路由，各模块 head
   独立成 json、写锁按模块，避免单一热点文件与跨模块写锁。
6. **回滚走 fork**：不原地撤销；fork 起点必须 ≥ LRU watermark；
   fork session 与 fork subagent 是两种形态。
7. **队列 = 引擎待发送请求队列**：draft↔queue 在 lifecycle 模块内原子迁移；
   先队列后草稿；失败回草稿；已发送未确认重启后重发。
8. **subagent 复用主会话 big_tool_result**；检索为单会话关键词模糊索引（可重建）。
9. **GUI/CLI 各自数据根**；单数据根单进程写者（独占锁）。

## 1. 事实模型与记录分层

| 层 | 载体 | 角色 | 生命周期 |
|---|---|---|---|
| message | `session/message/message_{m}_{n}.jsonl` | 正文事实（最终成功 in/out） | append；LRU 用户确认行删除 |
| EVENT | `session/event/event_{1..100}.jsonl` | 结构性操作摘要 | append，不参与装配 |
| compact | `session/compact.jsonl` | 派生摘要帧 | append；与 retention 联动 |
| stack/history | `session/{plan,task,goal}/{active,history}.jsonl` | 批次事实 + 归档 | 批次弹栈 |
| metadata | `session/metadata/guide.json` + `<module>.json` | 读索引 + 模块 head | 模块原子发布 |
| draft / queue | `session/input/`、`session/queue/` | 草稿 + 待发送队列 | lifecycle 模块内原子迁移 |
| big_tool_result | `session/big_tool_result/<hash>.jsonl` | 旁路（主会话统一） | 配额 + GC |
| 尝试缓存 | 运行期内存 | 重试/失败说明 | 非持久，进程退出清空 |
| 会话检索索引 | `session/metadata-index/` | 关键词模糊索引 | 可重建、允许落后 |

## 2. 目录布局

```text
session-<hash>/
  metadata/
    guide.json          # 读索引/路由（低频）
    message.json        # message head（热）
    compact.json        # compact head（热）
    event.json          # event head（热）
    stack.json          # plan/task/goal head（热）
    lifecycle.json      # queue_head + draft_head（同模块，原子迁移）
    retention.json      # LRU 水位/淘汰状态
    subagent.json       # 子代理清单
    media.json          # 媒体索引（预留）
  message/message_{m}_{n}.jsonl
  compact.jsonl
  event/event_{1..100}.jsonl
  {plan,task,goal}/{active,history}.jsonl
  input/draft.json(l)
  queue/queue.json(l)
  big_tool_result/<hash>.jsonl
  subagent_<hash>/      # 同构子树（无独立 blob，复用主会话）
  meta/<hash>/<原名>    # 媒体（后续，无项目关联任务）
```

读写规则：先 append 数据 → 原子替换所属模块 json；guide 只做读索引，低频更新；
读者路径 = guide → 模块 json → 数据文件（校验不符则自愈重读）。

## 3. 关键约定（权威：my_design §4）

- message 区间 `[from, to]` **含端点**；EVENT.anchor_message_id = 事件发生在**该行之后**；
- commit_id = 一次持久提交（可含多条事件行）；
- 最终成功：无重试 = 工具正常返回；有重试 = 成功后再读确认（verify）才落 message；
- K = 同一操作的重复次数（默认 3）；
- 装配材料：AI 需要看内容，读操作内容不得用抽象索引替代；
- 锚 ≤ watermark 视为已淘汰区，不算损坏；
- 队列空才直发，否则入队尾；fork 起点 ≥ watermark。

## 4. 恢复与装配：R1 / R2 / R3

- R1（UI/历史）：读 message 分片，可展示压缩前内容；LRU 淘汰区显示摘要占位；
- R2（LLM 装配）：最新 compact 摘要 + frame.message_to 之后的事件行 + 最近 K 条尝试，
  纯函数、不读 EVENT；详细契约见 my_design §5（R2-STABLE/TEST-* 系列）；
- R3（断点续跑）：interrupted 锚点后的尾段（EVENT 只用于定位，不改变正确性）。

## 5. 生命周期与容量

- draft / queue：lifecycle 模块内原子迁移；失败回草稿；message.json 发布 = 发送成功判定；
- 尝试缓存：非持久、K 条装配、max_items/max_chars 超限淘汰最旧；
- LRU：用户确认 → watermark 前按行删除 → message_id 空洞；
- 魔法数字与 config：my_design §11（shard_rows=100、K=3、frame 阈值=30、256 MB 告警、
  soft 60000/hard 16 MB/配额 64 MB 等），并附 §11.1 长会话校准基准。

## 6. fork、subagent、检索与数据根

- fork session = 独立项目级会话；拷贝 [watermark, from] + 该点栈/上下文快照
  （history 锚重建）；队列与 draft 不拷贝；
- fork subagent = `session/subagent_<hash>/` 子树；复用主会话 big_tool_result；
- 检索 = 单会话关键词模糊索引（可重建）；
- 数据根：GUI/CLI 隔离；单数据根单进程（独占锁 + owner_process；陈旧锁自动恢复默认关）。

## 7. 迁移与旧布局

- 新会话按本布局写入；旧 rollout/manifest/generation/transcript 布局**只读兼容**，
  不自动改写存量数据；迁移单独方案 + 备份 + 中文预警确认；
- 布局判定：存在 `session/metadata/guide.json` = 新布局；存在 `manifest.json` = 旧布局；
  运维/枚举工具需同时支持两套。

## 8. 实施顺序建议（供新对话）

| 阶段 | 范围 | 先落契约/测试 |
|---|---|---|
| M1 | message 事件行存储 + metadata 模块化（guide + module head） | 提交语义、崩溃残尾、模块 head 原子性 |
| M2 | R1/R2/R3 读取器 | R2-STABLE-1、R2-TAIL-1、R2-FILTER-1、R2-CACHE-1、R2-ORPHAN-1、R2-OPEN-1、R2-BUDGET-1、R2-EVENT-1、R2-WM-1、R2-FORK-1 |
| M3 | lifecycle（draft/queue）、fork session/subagent | 原子迁移、失败回草稿、重发、fork 起点校验 |
| M4 | LRU 行删除、retention、单会话检索索引、GC | 删除一致性、索引重建、锚 ≤ watermark |
| M5 | 迁移工具、verify/reconcile、config 校准 | 新旧布局判定、11.1 基准 |

门禁规则：每阶段 PR 必须先交付并跑绿 my_design 附录 D 对应的功能用例
（T-M1 / T-R1 / T-R2 / T-R3 / T-LC / T-FK / T-WM / T-EV / T-SR / T-BL / T-CFG）；
headless 冒烟只在功能用例全绿后执行，负责跨模块装配，不作为发现功能 bug 的兜底。

## 9. 开放项

同 [my_design.md](./my_design.md) 附录 C：检索索引实现细节、memory 项目级形态、媒体细节、
subagent 嵌套、默认值校准、跨模块短窗口分离（已接受；如需强一致再引入跨模块提交账本）。
