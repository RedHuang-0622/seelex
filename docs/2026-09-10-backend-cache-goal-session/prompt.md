# 提示词：会话存储下一阶段基建（R1 后端收口 / R2 goal 会话 / R3 中间缓存）

> 用法：把本文件作为下一阶段任务的开工提示词。开始实现前必须先按顺序读
> `AGENTS.md`、根 `MEMORY.md`、`docs/2026-09-08-session-storage-architecture/my_design.md`
> （v8.3 权威稿）与同目录 `conformance-checklist.md`，并以当前代码/测试为事实源。

## 0. 角色与总目标

你是 Seelex 仓库的执行 agent。当前会话存储的 v8.3 **存储侧收口已完成并提交**，
下一步只做**基建准备**，不接应用层业务接线：

1. **R1**：把多格式后端实现收口到接口（删除旧实现，保留新链路 interface，后续再实现）；
2. **R2**：goal 的 techleader 聊天记录像 subagent 一样独立存储，goal stack 记录
   role_name + session_id 锚；
3. **R3**：在会话生命周期引入中间缓存区（读 cache hit / 写 cache 与存储同步
   append-only，缓存内容 = 实际发送出去的上下文，含 compact 结果），storage 的
   jsonl 只 append，各 stack 即时落盘，解决会话后期加载时间随规模增长的问题。
4. **R4**：群聊模式与角色 draft 统一流程（user/main/tl/未来 agent-team 同路径；
   message = append-only 在线文档）；顺序支持 goal 固定循环与 agent-team
   user+main 决定（前端交互），并支持定时任务式角色插话；draft 装配/恢复按
   `join_seq_id` 与 `compact_ref` 收口（详见 §4）。

> 本阶段产出的是接口、存储形态、测试与设计登记；应用层接线（EVENT 生产者、
> 缓存命中接线）留到后续阶段。
> 应用层业务接线的独立提示词（按 R1→R4 顺序）见同目录
> [`app-layer-wiring-prompt.md`](./app-layer-wiring-prompt.md)。

### 0.1 术语与不歧义定义（本提示词唯一口径）

以下术语在 R1–R4 中一律按本表理解，不得混用：

| 术语 | 定义 |
|---|---|
| **message** | append-only 的**在线文档**：会话正文唯一事实源，一行 = 一个事件行，全局 `seq` 单调；任何角色的内容最终都必须以 message 行为准 |
| **role（角色）** | 参与者身份，当前 = `user` / `main`（main agent）/ `tl`（techleader）；后续 agent-team 角色只是新增 `role_name`，**不新增专属通道或专属提交流程** |
| **role_session_id** | 角色自己的会话号：`main` = 主会话号；`tl` = techleader 子会话号；未来角色同理 |
| **round（轮）** | 一次群聊因果轮次：从一条 user 输入开始，直到 TL 判定本轮群聊结束（见 R4 终止口径）；`round_id` 单调递增，是跨角色排序的第一键 |
| **draft（角色草稿）** | **未同步缓冲**：角色产出的、尚未进入 message 的事件行；**同步成功后立即删除**，不作为长期事实源、不做保留窗口 |
| **sync（同步）** | 由唯一 sequencer 把一批 draft 行 append 进 message 并发布 message head（提交点）；一次 sync = 一批行 = 一个 `commit_id` |
| **sequencer** | 会话级单写者：决定跨角色顺序、给 message 分配全局 `seq`、发布 head、回写锚；只有它触碰 message 写锁 |
| **unit（协议单元）** | 一次逻辑轮次（如 assistant tool_call 行 + 其 tool 结果行 + final LLM 行）；draft 内必须按 `CompleteEventUnits` 规则排好序，sync 时原样成单元落盘 |
| **compact 权威** | **main 会话是 compact 的唯一权威**：压缩区间、帧内容、frame_id 由 main 产生；tl 只保存同一帧的引用/备份用于冷恢复，**不得生成第二份帧** |
| **join_seq_id** | 角色加入群聊时 main message 的 `seq`；角色只能装配/恢复 `seq > join_seq_id` 的已发布内容；join 前历史由 main 决定是否摘要共享 |
| **order_policy / order_roles** | 群聊顺序策略：`goal_loop`（默认，固定 user→main↔tl 直到 TL 判定结束）或 `user_main_decided`（user+main 决定、前端交互）；存 `lifecycle.order_policy`/`order_roles` |
| **schedule（定时插话）** | EVENT `schedule.registered/cancelled/fired` + 运行期 timer；触发角色先写 role draft，再由 sequencer 在下一个可插入边界同步；不得绕过 sequencer |
| **compact_ref** | 角色会话对 main compact 帧的引用（frame_id + applied_seq）；角色不生成第二份帧 |
| **draft 装配分域** | `user` draft → 前端输入框；其他角色 draft → 各自上下文；未 sync 前都不进主文档 |
| **角色 draft 锁/actor** | 每角色一把独立 draft 锁 + actor 闭包（user/main/tl/agent-team 不共享）；写 draft 不触碰 message 锁；只有 sequencer 持 message 写锁 |
| **floor（发言权）** | 当前发言角色记录（role_name/role_session_id/round_id/seq/updated_at）；唯一写者 = sequencer；随 message head 发布（零额外 IO），不另开 metadata 文件 |

## 1. 现状与阶段判定

- **阶段**：v8.3「会话存储收口」已完成（S11–S24、S9、S13、S17 余量、S19/S20 已回填），
  最新提交 `45d35f6 feat(sessionstore): 按 v8.3 稿收口会话存储通道`，工作区干净。
- **JSON v8 布局是当前唯一在跑的会话存储实现**；SQLite/PostgreSQL/Redis 的
  v8 化此前被用户明确「搁置，随 message 读写热点专项」，本轮按 R1 直接删除实现。
- **仓库根 `go test .` 的 FAIL 归因**：根目录 `repro_*` 是诊断/复现用例，不是门禁。
  已用 `git worktree` 在 HEAD（`2b4535c`）复跑确认：
  `TestTwoRunningViewThirdThenSwitchedFinishesHot/Cold`、
  `TestBackgroundCompletionWhileSwitchingToC` 在 HEAD 同样失败（预存在红灯，非本轮回归）；
  同类诊断中 record/hot-attach 工具顺序问题已在本轮转为通过。
- **仍未闭合的挂牌项**（上一阶段遗留，不阻塞本提示词开工）：
  §4「EVENT 运行期写入（生产者）」——`checkpoint / compacted / interrupted /
  request·turn / token_usage / session_archived` 的应用层生产写入方尚未接线。

## 2. R1：多后端实现收口到接口（删除旧链路，防干扰）

### 目标

只保留新链路需要的接口契约，删除旧格式后端实现，避免后续实现被旧分支干扰。

### 要做

1. 保留的接口/契约（不得改动语义）：
   - `Repository`（JSON v8 单实现）、`stackJournal`（JSON 实现）、结构事件/模块 head
     与 commit_id 幂等契约；
   - 会话粒度端口（`SessionGranularStore` 暴露的 API）保持稳定。
2. 删除的旧实现（连同其专属测试/分支）：
   - `sqlRepository` / `redisRepository` 及其 `stack_journal_sql.go` /
     `stack_journal_redis.go`、manifest/generation/shard、SQL 表与 Redis 键等；
   - 与它们绑定的配置分支、后端矩阵测试（`forEachStackBackend` 的 sqlite 半边等）。
3. 后端枚举/配置处理口径（**已定：方案 A**，2026-09-10 用户裁决）：
   保留 `BackendSQLite/PostgreSQL/Redis` 枚举 + `Open` 显式返回
   “backend retired/unsupported”错误（不静默回退，配置层给出可操作提示）；
   不删除枚举，不做配置自动改写。
4. 明确「删除旧链路 = 删代码路径，不删磁盘上的用户数据」；`.seelex/`、`dist/`、
   `config/` 数据不清理。

### 危险操作铁律

执行删除前必须读根 `MEMORY.md`，并用中文向用户预警与备份（本操作只删代码，
但仍可能涉及构建物/测试夹具）；不得 `git clean/reset`；不得删除用户数据目录。

### 验收

- `go build ./...`、`go build -tags "gui,desktop,production" ./...`；
- `go test ./sessionstore -count=1` 与 `go test ./application/... ./internal/... ./seelebridge/...` 全绿；
- 已退役后端给出显式错误（或枚举已删），GUI/CLI 不会静默切换后端；
- README/打点表标注「后端实现已收口，新后端待按接口实现」。

## 3. R2：goal techleader 会话独立存储（仿 subagent）

### 目标

goal 治理里的 techleader 聊天记录独立存储，不再与主会话 message/context 混放；
goal stack 条目记录 `role_name`（如 techleader）与 `session_id` 锚。

### 已定形态（2026-09-10 用户裁决）

- **TL 会话定位 = TL 的备份 + 冷恢复来源**，不是主会话上下文的权威：
  - 主会话 message 文档仍是群聊正文的**唯一权威**（含各角色同步后的行）；
  - TL 子树（`session/goal_<hash>/`，message/event/metadata 同构，
    复用 `storeEngine` 与 subagent 子树机制）保存 TL 自己那份行，用于 TL
    冷恢复与审计；
  - TL 冷恢复 = 读自己的备份行 + 引用 main 的 compact 帧；不得从 TL 侧反推
    main 的 compact。
- **compact 以 main 为准**：压缩区间、帧内容、`frame_id` 全部由 main 产生；
  TL 会话同步同一 `frame_id` 的引用/摘要（只作冷恢复），**不得产生第二份帧**；
  主会话 compact 覆盖区间以 message 全局 `seq` 为准（跨角色行一视同仁）。
- **goal stack 锚**：条目 payload 增加 `role_name` + `role_session_id`
  （字段定名 `role_name` / `role_session_id`，语义见 §0.1），坐标沿用
  `item_message_id` / `item_message_seq`；head 仍只装水位。
- **生命周期**：TL 派发（spawn）→ TL 子会话建立并写入自己的备份行 → 参与群聊
  直到 TL 判定结束（R4 终止口径）→ 归档/保留为 TL 冷恢复来源；
  `fork session` 不入栈（沿用 D6），子会话不拷贝 goal 子树；
- **blob**：复用主会话 `big_tool_result`（I11），不建独立 blob 目录。

### 验收建议（新编号 T-GS-01…）

- 独立目录与项目枚举可见、与主会话 message 物理隔离；
- goal stack 条目含 role_name/session_id，锚点正确、`goal.*` EVENT 留档；
- 删除主会话后 TL 备份行仍可读；TL 冷恢复在当前数据根内可完成（compact 引用
  缺失时显式报错，不伪造帧）；
- head 只含水位（无条目正文）；
- 与 subagent 一致：无独立 blob 目录、大输出 result_ref 指主会话。

## 4. R4：A2A 群聊、角色 draft、桌游顺序与定时插话（统一流程）

### 目标

把 message 当作 **append-only 在线文档**：所有参与者（user、main、tl，以及
之后加入的 agent-team 角色）都走**同一条**「角色 draft → sequencer 同步 → message」
流程，不为任何角色开专属通道或专属写入器。

### 群聊时序（已定，2026-09-10）

- 顺序：`user → main → tl → main → tl → main → tl → …` 交替，直到 TL 判定群聊结束；
- `round_id` 从一条 user 输入开始，TL 结束判定收口本轮；下一条 user 输入开启新 round；
- TL 终止口径（默认）：TL 输出 `verdict_done` 或 `escalate_human`，或终态 gate
  通过；其它 directive 一律视为“继续交替”。具体 directive→终止映射在实现时
  落成 T-A2A 用例（见下）。
- **阶段一（goal 原型）固定循环**：`order_policy=goal_loop`，顺序不可被前端改；
- **阶段二（agent-team）顺序由 user + main 决定**：`order_policy=user_main_decided`，
  有序角色列表存 `lifecycle.order_roles`；前端必须有顺序编辑交互（拖拽/上下移/
  插入/跳过），后端只接受该字段更新，不得有第二份顺序事实；
- **角色区分**：前端必须按 `role_name`（user/main/tl/agent-team）区分展示
  （徽标/配色/分组/顺序面板）；message 行必须带角色归属，否则 UI/恢复不可辨。

### 统一角色 draft 流程（所有角色一致）

1. 角色产出的事件行（assistant tool_call、tool 结果、final LLM、自己的 user/摘要材料）
   先写进**自己的 draft**（append-only、崩溃可恢复）；
2. 角色**不得直接写 message**；只有 sequencer 能把 draft append 进 message；
3. sequencer 顺序键：`round_id` → role 顺序（当前 user→main→tl；未来角色按注册的
   调度策略）→ `unit_seq`；
4. 工具调用顺序在 draft 内按 `CompleteEventUnits` 规则排好（assistant tool_call →
   tool 结果 → final LLM），sync 时原样成单元；
5. **sync 成功（message head 发布）后立即删除该批 draft；失败保留，重启后重放**；
   draft 无保留窗口、不做长期归档；
6. 幂等：一次 sync 一个 `commit_id`（由 round/role/role_session/unit 集合确定性推导）；
   重放不重复行；读侧按条目键去重、高 revision 胜出。
   半同步窗口（head 已发布、draft 尚未删除时崩溃）：重启按 `commit_id` 判定该批
   已同步，**直接删除 draft，不重复 append**；判定不成立才重放。

### 落盘与准入门（实现前必须完成）

- 建议形态：`session/goal_<hash>/draft/<role>.jsonl`（每角色一个 append-only
  未同步缓冲；**文件存在 = 有待同步内容**，删除 = 已同步）；
- 该落盘产物必须先加入 `my_design` §2.0 清单并登记 §4 术语表（D-A2A），
  同步更新 I9/写锁与 T-A2A 用例；
- draft 不配“长期可见性 head”：它的提交点就是 message head；崩溃恢复按文件内容重放；
- message 行必须可识别角色归属（`role_name` + `role_session_id`，或等价映射），
  供 UI/审计/恢复使用；**不得为角色复制一份 message 事实**。

### seq 编排（唯一分配者）

- message 全局 `seq` **只由 sequencer 分配**；
- draft 行携带：`round_id`、`role_name`、`role_session_id`、`unit_seq`、`message_id`；
- 写入顺序 = 跨角色 round/role 顺序 + 角色内 `unit_seq`；LRU 空洞只允许出现在
  watermark 之前，不重编号；
- sync 后把 message 坐标回写：goal stack 条目 `item_message_id`/`item_message_seq`
  与 EVENT `goal.*` 审计。

### 角色 draft 锁与 actor（必须做，避免共享热点）

- **每角色一把独立 draft 锁**：user、main、tl、未来 agent-team 角色各持自己的
  `roleDraftMu`；**不共享 draft 锁**，角色并发写 draft 不互相阻塞；
- 推荐实现为**每角色一个 actor 闭包**（goroutine + mailbox）：角色 actor 只做
  “产出事件行 → append 自己的 draft → 通知 sequencer”，不触碰 message 锁；
- sequencer 是独立单写者 actor：只它持 message 写锁，负责排序、分配全局 seq、
  发布 message head、回写锚、更新 floor；
- 角色 actor 的“完成/可同步”通知只进 sequencer 排序队列，不进 message 临界区；
- 新角色只新增自己的锁/actor，不允许改动共享锁形态。

### floor：当前发言角色记录（必须做）

- floor 字段：`role_name`、`role_session_id`、`round_id`、`seq`、`updated_at`；
- **唯一写者 = sequencer**（本身就是串行点），其他角色只读；
- 落点固定：**message head 的 `floor` 字段**，随每次 sync 的 message head
  原子发布写入——不新增 metadata 文件、不额外 rename/fsync；
- 运行期以 sequencer 内存态为快（in-flight draft 未同步也能表示“正在谁发言”）；
  冷恢复 = message head.floor + 最后一行 role 归属 + `order_policy/order_roles`；
- 可选 EVENT `turn.floor_granted` 仅作审计，不参与装配；
- **反例（禁止）**：为 floor 单开 `metadata/floor.json` 或每轮额外替换 lifecycle
  head——那会新增每轮一次 fsync+rename（≈30–63 ms 量级），收益不抵成本。

### 定时任务式插话（与桌游式并列支持，必须做）

- 注册/取消/触发 = EVENT `schedule.registered` / `schedule.cancelled` /
  `schedule.fired`（payload：schedule_id、role_name、role_session_id、
  cron/interval、next_fire_at、payload 引用）；
- 冷启动按 EVENT 重放重建 timer；错过的触发默认“重启后立即补一次”
  （可配置为跳过）；
- 触发时角色与普通参与者同路径：先写 role draft，sequencer 在下一个 `unit_seq`
  边界插入；**不得阻塞其它角色 draft 写入，不得绕过 sequencer 直写 message**；
- 定时插话与桌游式顺序共用同一排序/插入机制，只改“触发来源”，不改 message 形态。

### draft 装配秩序与恢复（必须做）

- `user` draft → 前端输入框（`session/input/draft.json`）；未发送前不进群聊正文；
- 其他角色（main/tl/agent-team）draft → 各自上下文：作为 pending material 参与该角色
  下一次 wire 组装，**未 sync 前不进主文档、不进其他角色 wire**；
- 每个角色的可见区间（装配输入）：

```text
role_wire(role) =
    main compact_ref（frame_id + applied_seq）          # compact 唯一权威来自 main
  + main message 行 where seq > join_seq_id(role) 且已发布
  + 该角色自身未同步 role draft（按 round_id→role→unit_seq 排序）
```

- `join_seq_id` 存角色会话 `metadata/lifecycle.json`，并登记在 goal stack 条目锚上；
  角色加入时间不同 → 可见历史不同，是预期语义；
- **compact 同步到角色**：main 帧发布后把 `compact_ref`（frame_id + applied_seq）
  写入每个角色会话；角色**不得生成第二份帧**；
- **冷恢复顺序**：载入角色自身备份行（message/event） → 载入未同步 role draft WAL →
  按 round/role/unit_seq 装配 → 与 main `compact_ref` + `seq > join_seq_id` 的已发布行
  合并；已同步 draft 按 commit_id 幂等删除，不重放。

### 群聊扩展（agent-team / 桌游式调度）

- 新角色 = 注册 `role_name` + `role_session_id` + 顺序优先级 + `join_seq_id`；
  不得新增旁路通道；
- 桌游式调度只是替换 sequencer 的“role 顺序函数”（goal 固定循环 / user+main 决定 /
  定时插入），message/draft/compact 形态不变。

### 验收（T-A2A-01…）

- **T-A2A-01 顺序与单元**：同一 round 内 `user→main→tl→…` 行序稳定；
  tool_call→tool→final 单元顺序与 `CompleteEventUnits` 一致；
- **T-A2A-02 同步即删**：sync 成功后 draft 文件不存在；message head 未发布时 draft 保留；
- **T-A2A-03 幂等恢复**：崩溃重放同一 round 不重复行、`commit_id` 稳定；
  半同步（draft 已写/head 未发布）可恢复；
- **T-A2A-04 群聊统一**：user/main/tl/未来角色走同一 draft→sync 路径，无专属写入器；
  新增角色不改 message/compact 形态；
- **T-A2A-05 compact 权威**：main 生成帧，tl 只引用/备份；tl 冷恢复 = 备份行 +
  main 帧，引用缺失显式报错；
- **T-A2A-06 性能（同轮 A/B）**：每 round 主会话提交次数/fsync+rename/`messageMu`
  等待下降；draft 字节仅存在于未同步窗口，同步后归零。
- **T-A2A-07 顺序策略**：`goal_loop` 行序严格交替且 TL 终止停；`user_main_decided`
  按 `order_roles` 生效，前端只改 lifecycle 字段；
- **T-A2A-08 定时插话**：`schedule.*` EVENT 驱动 timer，触发角色经 draft→sequencer
  在下一可插入边界同步；不阻塞其它角色、不绕过 sequencer；
- **T-A2A-09 join_seq 语义**：不同 `join_seq_id` 角色装配出的历史不同，且只含
  `seq > join_seq_id` 的已发布行；join 前历史默认不出现（main 摘要共享除外）；
- **T-A2A-10 draft WAL 恢复**：冷启动从 role draft WAL 载入未同步批并按
  round/role/unit_seq 装配；半同步批按 commit_id 幂等删除；
- **T-A2A-11 compact_ref 同步**：main 帧发布后各角色收到同一 frame_id 引用；
  角色恢复不生成第二份帧，引用缺失显式报错；
- **T-A2A-12 角色区分/装配分域**：user draft 只进输入框；其他角色 draft 只进各自
  上下文；未 sync 前均不进主文档；UI 能按 role_name 区分展示。
- **T-A2A-13 角色锁/actor 独立**：user/main/tl/agent-team 各持独立 draft 锁/actor；
  并发写 draft 不互相阻塞、不触碰 message 锁；只有 sequencer 持 message 写锁。
- **T-A2A-14 floor 记录**：当前发言角色只由 sequencer 写、随 message head 发布
  （零额外 rename）；冷恢复由 floor + 最后一行 role + order_policy 重建。

## 5. R3：会话中间缓存层（cache hit + storage append-only）

### 目标

在会话生命周期里设置中间缓存区：

- 读：命中缓存（cache hit）直接返回，未命中回退存储派生；
- 写：cache 与存储**同时 append-only**；缓存内容 = 实际发送出去的上下文
  （wire/material，因此 compact/裁剪后的形态也要在缓存里维护好）；
- storage 的 jsonl 只 append；各 stack 即时落盘；
- 目标：避免会话越到后期加载时间越长（消除 O(history) 冷加载放大）。

### 参考模型（已调研，2026-09-10）

调研报告：[`docs/research/2026-09-10-collaborative-doc-session-model.md`](../../research/2026-09-10-collaborative-doc-session-model.md)。
结论：采用“**服务端权威 sequencer + 角色 append-only op-log + 周期快照 + 易失
presence**”，不采用纯 OT/CRDT：

- message 行 = op-log；role draft = 未确认 op（同步成功即删）；
- compact = 周期快照；`join_seq_id`/`compact_ref.applied_seq` = 读取位点；
  LRU watermark = 可回收前缀；
- presence（Agent Team 在线状态、当前发言高亮）与正文分离，易失、不入 message；
- R3 缓存 = 每个角色会话的**物化视图**（已 sync 的 wire 形态），
  失效键 = message head commit/revision + compact frame_id/applied_seq + floor；
- 不采用 CRDT 的每操作元数据方案（存储放大与总序复杂度不划算）。

### 边界与约束

- 只做**基建与接口**，应用层接线留到后续（但接口要预留 layer 间调用点）；
- 不改变 D12 两类通道语义（追加型 revision ≤ head_seq + 条目键最高 revision；
  整份替换型以文件原子替换为发布点）；
- head 发布点、commit_id 逐操作唯一、幂等与恢复语义不变；
- 缓存不得成为第二事实源：未命中必须能完全从存储派生；崩溃/重启语义要定义；
- 与 message 读写热点专项（H2/H3/H4，含发布形态）协同，不得各自为政；
- §9 单数据根单写者（I10）是缓存正确性前提；多进程只读共享仍未表态。
- **群聊模式下的缓存分域（与 R4 对齐）**：缓存按 `role_session_id` 分域——main
  缓存主文档（append-only 在线文档）的 wire 形态；tl 缓存 TL 自己的行（备份/
  冷恢复）；compact 帧只来自 main，tl 缓存只引用 main 的帧，不产生第二份帧。
  角色 draft 是未同步缓冲，不进长期缓存；sync 成功后缓存更新、draft 删除。

### R3 待定项（见 §6.2，未点前不得自行决定）

- 缓存粒度：会话级 / 通道级 / 分片级；
- 失效与一致性：head revision、checksum、seq 空洞、LRU watermark；
- compact 的缓存形态：只缓存最新帧 + 其后 tail，还是缓存整段 wire；
- 容量/淘汰：条数、字节、与 attempt cache / compact / retention 的关系；
- 与 R1 的先后：建议 R1 完成后再做 R3，避免旧后端干扰；
- 与“message 读写热点专项”的先后（用户已要求该专项最后做）。

### 验收建议（新编号 T-CACHE-01…）

- 冷启动后首请求 = 缓存命中路径与从存储派生路径输出逐字节一致；
- cache append 与 storage jsonl append 同轮可观测；崩溃后可从存储完整恢复；
- 长会话（≥500 轮、≥3 次压缩）加载时间不随历史线性增长（同轮 A/B，本机抖动不可比）；
- 各 stack 即时落盘断言（不依赖会话结束批量刷写）。

## 6. 已定口径与仍需裁决

### 6.1 已定（2026-09-10 用户裁决，直接执行，不再自行发挥）

1. **同步后即删 draft**：无保留窗口；message head 发布 = 同步成功 = 删除 draft；
   失败保留、崩溃重放、幂等去重（见 R4）。
2. **群聊顺序**：`user → main → tl → main → tl → …` 交替，直到 TL 判定结束；
   一条 user 输入 = 一个 `round_id`。
3. **compact 以 main 为准**：main 生成区间/帧/frame_id；tl 同步同一帧的引用/备份，
   不得产生第二份帧；**tl 会话只是 TL 的备份与冷恢复来源**（备份自己的行 +
   引用 main compact）。
4. **角色 draft 走统一流程**：user、main、tl 以及未来 agent-team 角色同一
   draft→sequencer→message 路径；message = append-only 在线文档；禁止角色专属旁路。
5. **字段定名**：`role_name`、`role_session_id`（不再用 `session_id` 简写指代角色会话）；
   goal stack 条目同时保留 `item_message_id`/`item_message_seq` 锚。
6. **顺序指定**：goal 阶段固定循环 `user→main↔tl` 直到 TL 判定结束；
   agent-team 阶段由 **user 与 main 共同决定**顺序（前端交互），
   只更新 `lifecycle.order_policy`/`order_roles`；前端必须做角色区分。
7. **定时任务式插话必须支持**：EVENT `schedule.*` + 运行期 timer，触发角色
   经 role draft→sequencer 在下一可插入边界同步，不绕过 sequencer。
8. **draft 装配分域**：user draft 只进输入框；其他角色 draft 只进各自上下文；
   未 sync 前不进主文档。
9. **恢复坐标**：每个角色记 `join_seq_id`；compact 权威在 main，帧发布后把
   `compact_ref` 同步到各角色会话；角色冷恢复 = 自身备份 + WAL + compact_ref +
   `seq > join_seq_id` 的已发布行。
10. **角色 draft 锁/actor 独立**：user/main/tl/agent-team 各持一把 draft 锁或
    actor 闭包，不共享；写 draft 不触碰 message 锁；只有 sequencer 持 message
    写锁。
11. **floor 记录**：当前发言角色由 sequencer 写，落 `metadata/message.json` 的
    `floor` 字段（随 message head 发布，零额外 IO）；不另开文件、不额外 rename。
12. **R1 后端方案 = A**：保留 `BackendSQLite/PostgreSQL/Redis` 枚举，
    `Open` 显式返回 retired/unsupported 错误，不静默回退、不自动改写配置。
13. **R3 参考模型**：采用“服务端权威 sequencer + 角色 append-only op-log +
    周期 compact 快照 + 易失 presence”（调研见
    `docs/research/2026-09-10-collaborative-doc-session-model.md`），
    不采用纯 OT/CRDT。
14. **前端交互已定框架**（字符画见 §7）：右侧栏「状态 → Agent Team」子页；
    成员在线状态 + 当前发言标识；新建 goal → 新建 TL → TL 上线进入工作顺序；
    工作顺序栏由 main agent 编排；定时任务 agent 单独分区、不参考工作顺序。
15. **装配流程以 my_design §5.7 为权威**（本提示词 §8 为执行摘要）：
    冷加载首步→末步、热切换、运行时会话切换、draft 装配顺序与 I22–I25 不变量
    全部照此实现，验收 T-ASM-01…06。

### 6.2 仍需用户裁决（未点前不得自行决定）

- **R3 缓存**：粒度（会话/通道/分片）、compact 缓存形态（最新帧+tail / 整段 wire）、
  容量与淘汰、与 message 读写热点专项的先后；多进程只读共享是否允许
  （当前 I10 单写者前提）。
- **前端交互细节（不阻塞存储实现，但阻塞 UI 接线）**：顺序编辑的具体交互
  （拖拽/跳过/插入位置）、角色徽标与配色规范、定时任务管理面板字段。

### 6.3 R4 实现时可采用的默认（若你无异议，按此推进；有异议请在开工前点）

- TL 终止映射：`verdict_done` / `escalate_human` / 终态 gate 通过 = 结束本轮群聊；
  其它 directive = 继续 `main→tl` 交替；
- 角色 draft 文件命名：`session/goal_<hash>/draft/<role_name>.jsonl`；
- 未来角色注册：`role_name` + `role_session_id` + 调度优先级（序号即可），
  调度策略只替换 sequencer 的 role 顺序函数；
- 定时任务默认：`schedule.enabled=false`、错过的触发默认重启后补一次
  （`catch_up=true`），插入点为下一个 `unit_seq` 边界；
- draft/角色会话目录：tl = `session/goal_<hash>/`，agent-team =
  `session/role_<hash>/`，draft = `<role_session>/draft/<role_name>.jsonl`；
- 角色冷恢复默认：自身备份 + WAL + main `compact_ref` + `seq > join_seq_id`；
- floor 落点默认：message head `floor` 字段（若未来需要独立审计可加 EVENT，
  但不额外写 metadata 文件）；
- 角色锁/actor 默认：每角色一把 draft 锁 + actor 闭包，sequencer 单写 message；
- Agent Team UI 默认：未建 goal 时只有 user/main（定时 agent 可选）；
  新建 goal 才创建 TL 并标记 online；工作顺序栏只由 main agent 写入
  `order_roles`（阶段二放开 user+main）；定时任务 agent 单独分区、不入 `order_roles`；
- presence 默认：在线状态/当前发言高亮是运行态，不写 message、不入 §2.0 metadata；
  冷启动由角色会话存在性 + 运行期 actor 状态 + floor 重建；
- draft 清理：sync 成功后按批删除；崩溃后只按 draft 文件重放，不读已删历史。

## 7. 前端交互字符画：右侧栏「状态 → Agent Team」

### 7.1 右侧栏状态子页（成员在线状态 + 工作顺序 + 定时任务）

```text
┌─ Seelex ─────────────────────────────────────────────┬──────────────────────────────────────────────┐
│ 主视图（群聊 / message = append-only 在线文档）        │ 右侧栏 · 状态子页                              │
│                                                      │                                                │
│  ┌────────────────────────────────────────────────┐  │  ┌ Agent Team ───────────────────────────────┐ │
│  │ user  │ 请开始这个 goal                        │  │  │ 成员            身份    在线   当前发言     │ │
│  │ main  │ 收到，拆解目标并派发 TL                │  │  │ ● main         主脑     online             │ │
│  │ tl-1  │ 评审：先做 A，再做 B；风险点 …         │  │  │ ● tl-1         TL       online   ◀ floor  │ │
│  │ main  │ 按 TL 指令执行 A…                      │  │  │ ○ tl-2         TL       offline            │ │
│  │ tl-1  │ verdict_not_done：继续执行 B           │  │  │ ○ agent-A      agent    offline            │ │
│  │ main  │ 执行 B…                                │  │  │ ● agent-B      定时     online  (已上线)   │ │
│  │ tl-1  │ verdict_done → 本轮群聊结束             │  │  └───────────────────────────────────────────┘ │
│  └────────────────────────────────────────────────┘  │                                                │
│                                                      │  ┌ 工作顺序（main agent 可编排）─────────────┐ │
│  floor: tl-1 (round 3, seq 128)                      │  │ 1. user                                   │ │
│                                                      │  │ 2. main                                   │ │
│  ┌ 新建 goal ─────────────────────────────────────┐  │  │ 3. tl-1                                   │ │
│  │ [ 新建 TL ] → tl 上线（online）→ 进入团队名单   │  │  │ 4. main ⇄ tl-1（循环直到 TL 判定结束）    │ │
│  └────────────────────────────────────────────────┘  │  │ [↑][↓][插入][跳过][拖拽]  main 编排        │ │
│                                                      │  └───────────────────────────────────────────┘ │
│                                                      │                                                │
│                                                      │  ┌ 定时任务 agent（不参考工作顺序）─────────┐ │
│                                                      │  │ ⏰ agent-B   每 10m       下一次 10:30    │ │
│                                                      │  │ ⏰ agent-C   cron 0 9 * * *  下一次 09:00 │ │
│                                                      │  │ 到点 → 写 role draft → sequencer 下一      │ │
│                                                      │  │       unit_seq 边界插入（不阻塞其它角色）  │ │
│                                                      │  └───────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────┴──────────────────────────────────────────────┘

要点：
- 未建 goal 时团队只有 user/main（+ 定时 agent 可选）；
- 新建 goal = 新建 TL 并让 TL“上线”（online），才进入工作顺序编排；
- 工作顺序栏 = `lifecycle.order_roles`，只有 main agent 可编排（阶段二可 user+main）；
- 定时任务 agent 单独分区，**不参与工作顺序**，到点经 draft→sequencer 插话；
- 角色区分：成员身份/徽标/配色 + message 行的 role_name/role_session_id 归属。
```

### 7.2 写入与顺序流水线（角色 actor → sequencer → 在线文档）

```text
        user actor            main actor             tl actor          agent-team / schedule actor
        ┌──────────┐          ┌──────────┐          ┌──────────┐          ┌──────────┐
        │ userDraft│          │ mainDraft│          │  tlDraft │          │ roleDraft│   ← 每角色独立锁/actor
        │   Mu     │          │   Mu     │          │   Mu     │          │   Mu     │
        └────┬─────┘          └────┬─────┘          └────┬─────┘          └────┬─────┘
             │ append              │ append              │ append              │ append
             ▼                     ▼                     ▼                     ▼
        draft/user.jsonl      draft/main.jsonl      draft/tl.jsonl       draft/<role>.jsonl
             │                     │                     │                     │
             └──────────┬──────────┴──────────┬──────────┴──────────┬──────────┘
                        ▼                                            ▼
                 ┌─────────────────────────────────────────────────────────────┐
                 │ sequencer（单写者 actor；只它持 message 写锁）                │
                 │ 排序: round_id → role（goal_loop / order_roles / schedule）   │
                 │       → unit_seq；分配全局 seq；更新 floor                    │
                 └──────────────────────────────┬──────────────────────────────┘
                                                │ sync（一批行 = 一个 commit_id）
                                                ▼
                 ┌─────────────────────────────────────────────────────────────┐
                 │ message 在线文档（append-only；唯一正文权威）                 │
                 │ 行字段: seq / role_name / role_session_id / round_id / unit_seq│
                 └───────────────┬──────────────────────────────┬──────────────┘
                                 │ compact（main 权威）          │ ack
                                 ▼                              ▼
                  ┌───────────────────────────┐        ┌───────────────────────────┐
                  │ compact 帧（frame_id）     │        │ 同步后即删 role draft      │
                  │ → compact_ref 同步各角色   │        │ （无保留窗口）             │
                  └───────────────────────────┘        └───────────────────────────┘

恢复：角色会话备份（message/event） + role draft WAL + main compact_ref
      + main 行 where seq > join_seq_id；半同步批按 commit_id 幂等处理。
```

## 8. 装配流程（冷加载 / 热切换 / 运行时切换 / draft 顺序）

> 本节与 `my_design.md` §5.7 同源；实现与测试以 my_design 为准，这里给执行摘要。
> 目标：任何角色、任何切换路径下，装配输入都只来自目标会话自身事实，且切换原子。

### 8.1 冷加载：首步 → 末步（逐步不可省）

```text
view.switch(sessionID, role_name)
 → resolve project binding（禁止回退 active scope）
 → guide(schema) → head(message/compact/floor) 校验/自愈
 → role 上下文（role_session_id / join_seq_id / compact_ref / order_policy）
 → main compact 帧（frame_id + applied_seq）
 → main message 行 where seq > max(applied_seq, join_seq_id)
 → role 备份（role session message/event）
 → role draft WAL（未同步 op，round→role→unit 排序）
 → pending 合并进 role wire（不进主文档）
 → 尝试缓存 K + 预算/完整单元 → role wire
 → 原子 view swap + 缓存(applied_seq/prefix_digest) + 注册 actor/presence/floor
```

1. 入口与绑定：显式 sessionID + role；项目归属用 workspace 绑定，禁止回退 Router
   活跃写作用域。
2. guide/head 校验：schema/checksum 失败自愈重读，二次仍不符按数据重建 head。
3. 角色上下文：`role_session_id`、`join_seq_id`、`compact_ref`、
   `order_policy/order_roles`、`floor`；user 这一支只服务输入框。
4. 快照：取 main 最新 compact 帧；角色不得使用自建帧；
   `applied_seq = max(compact_ref.applied_seq, join_seq_id)`。
5. 已同步正文：读 main message 行 `seq > applied_seq`（尾窗优先），
   按 `CompleteEventUnits` 收口，open 单元保留 + 修复占位。
6. 角色备份：`role != main` 时读该角色会话备份行（message/event），仅作备份/冷恢复。
7. role draft WAL：读 `<role_session>/draft/<role_name>.jsonl` 未同步 op，
   按 `round_id → role → unit_seq` 排序；文件存在 = 待同步。
8. pending 合并：只并入该角色自己的 wire；未 sync 不进主文档、不进其他角色 wire。
9. 尝试缓存与预算：锚点后拼最近 K 条尝试；软阈值（75%）停扩窗收口完整单元；
   仍超限 → `need_compact`。
10. 输出：`role_wire + need_compact + prefix_digest`；写回 R3 物化缓存
    （key = applied_seq/commit_id/floor）。
11. **最后一步才是原子 view swap**；随后注册 actor/锁、presence(online)、
    floor 高亮、订阅 head/event 变更。
12. 失败语义：compact_ref 缺失/draft 损坏显式报错；不伪造帧、不回退旧布局、
    不把别的会话内容当本会话历史。

### 8.2 热切换（目标 resident）与运行时切换（A 在跑切 B/C 再切回）

```text
target resident? ──yes──► 内存快照(floor/head/revision) 校验
        │                   → 缓存命中 role_wire → pending 合并 → 原子 swap
        └─no──────────────► 冷加载（8.1）；旧视图保持到新 wire ready

A running ──view.switch(B)──► B hot attach 或 cold load
   │  A 的 runChat/actor 继续：只写 A 的 draft/queue/EVENT，由 A 的 sequencer 同步
   └─view.switch(A)──► A resident? hot attach : cold load（A 的 WAL 按序载入）
```

- 热切换优先内存快照 + R3 缓存，不整文件重读；缓存失效才重建。
- 切走不写 message、不停止在跑会话；切回按 resident/cold 走对应路径。
- **禁止**在冷读时回退引擎共享 RawHistory 或全局历史；B/C 视图不允许出现 A 的内容。
- 视图加载期间保持旧内容，新 wire 就绪后原子替换（对应根目录 repro 诊断的失败点）。

### 8.3 draft 装配顺序（统一）

| draft | 装配目标 | 进主文档时机 | 排序键 |
|---|---|---|---|
| user draft（`input/draft.json`） | 前端输入框 | 发送并 sync 后 | 无（1:1 草稿） |
| main role draft | main wire（上下文） | sync 后 | `round_id → role → unit_seq` |
| tl / agent-team role draft | 各角色 wire | sync 后 | 同上（schedule 插话取下一 unit 边界） |

- unit 内顺序：`assistant tool_call → tool 结果 → final LLM`（`CompleteEventUnits`）；
- pending 段在 wire 中紧接该角色可见的最后一条已 sync 行之后，floor 标“正在谁发言”；
- 多角色同时 pending：主文档按 `order_policy` 依次 sync；定时插话不抢占当前 unit；
- sync 成功后立即删除对应 draft；半同步（head 已发布、draft 未删）按 commit_id
  幂等删除，不重复 append；随后更新 role 缓存、applied_seq、floor、presence。

### 8.4 装配不变量（与 my_design I22–I25 对齐）

```text
I22 装配输入只来自目标会话自身（main message/compact + 目标角色备份 + 目标角色 draft
    + join_seq_id/applied_seq 切点）；禁止跨会话或全局历史回退
I23 pending draft 未 sync 前不进主文档、不进其他角色 wire；user draft 只进输入框
I24 会话切换原子：新 wire 就绪前保持旧视图，不允许半成品/跨会话内容可见
I25 compact frame / compact_ref.applied_seq / join_seq_id 是仅有的历史切点；
    不得用引擎内存 RawHistory 充当装配输入
```

### 8.5 验收（T-ASM-01…06）

| 编号 | 断言 |
|---|---|
| T-ASM-01 | 冷加载按 8.1 逐步执行：首步绑定 → 末步原子 swap；中途失败不改变旧视图 |
| T-ASM-02 | 角色 wire 只含 `seq > max(applied_seq, join_seq_id)` 的已发布行 + 自身 pending draft |
| T-ASM-03 | user draft 只进输入框；其他角色 draft 只进各自 wire；未 sync 不进主文档 |
| T-ASM-04 | 热切换缓存命中不整文件重读；缓存失效（head/compact 变化）自动重建 |
| T-ASM-05 | A 在跑切 B/C 再切回：A 不中断、B/C 无 A 内容；切回 resident=hot、卸载=cold |
| T-ASM-06 | draft 顺序/幂等：round→role→unit 顺序稳定；sync 后即删；半同步按 commit_id 不重复 |

## 9. 每阶段施工纪律（不可省）

1. 先写失败测试 → 实现 → `gofmt`/`go build ./...`（含 gui tags）/`go vet`/目标包测试；
2. 每阶段 `go test -race` + mutexprofile，同轮 A/B 对比（跨会话数字不可比）；
3. 新语义先在 `my_design.md` §4 术语表登记，再补 `T-*` 用例，最后回填
   `conformance-checklist.md` §6/§7；
4. 退役不留兼容；发现字段无处安放 → 停下上报，不自行造落点；
5. 每阶段commit / 不 push，除非用户明确要求；危险操作前按 `MEMORY.md` 中文预警 + 备份。
