# 轨迹视图（Network 风格响应日志）模块详细设计

> 状态：已实现
> 总体架构：[`../architecture.md`](../architecture.md)
> 数据源：`Snapshot.conversation`（权威投影，见 `conversation-rendering.md`）

## 1. 职责与边界

该模块为对话区新增「轨迹」子页，提供类似浏览器 DevTools Network 面板的
响应日志体验：先按响应类型把会话消息分类，再按时间把请求/响应投影为
轨迹行（时间 / 类型 / 名称 / 状态 / 耗时 / 大小），点击行展开 IN/OUT 详情；
轨迹顶部另有一条多线谱式上下文轴，把不同类型（输入 / LLM / 工具 / 错误 /
通知）的上下文块分到各自的横轨上，对照查看上下文填充与先后关系。

它不调用 Bridge，不决定 Chat 业务状态，也不修改客户端 Snapshot。轨迹数据
完全从 `Snapshot.conversation` 派生（呈现层职责），本地 UI 状态（当前子页、
过滤类型、展开行、滚动位置）只存在前端。

## 2. 响应类型分类（先分类，再轨迹）

分类是纯函数（`buildTrajectory`），输入 conversation 消息数组，输出轨迹记录。

| 类型 | 来源 role | 说明（Network 类比） | 状态色 |
|---|---|---|---|
| `input` | `user` | 用户输入（请求发起 / Initiator） | info |
| `llm` | `assistant`（非空） | LLM 响应流（Response） | done |
| `tool` | `tool` + `tool_result` 配对 | 工具调用（请求 IN + 响应 OUT、耗时、大小） | done / failed / running |
| `error` | `error` 或 tool 错误 | 错误响应 | failed |
| `notice` | `system` | 系统通知（会话恢复、项目切换等） | idle |

规则：

- `tool` 请求与 `tool_result` 响应**只按 `tool.id`（框架 tool-call id）配对**为一条
  记录（`id` 缺失时不再按 `name` 猜，响应独立成行；与
  `components.buildConversationItems` 同一契约，阶段 B3 起生效）。服务端两处
  构造都携带同一 id，恢复历史亦取 `toolCall.ID`，因此按名回退是纯前端自造的
  猜测，会把同名并发工具错配成一行。响应把
  OUT / 状态 / 耗时 / `result_ref` / `truncated` / `total_chars` 合并进记录。
- 空 `assistant` 消息是工具回合后的占位，对轨迹无信息量，跳过。
- `role=system` 之外的未知 role 兜底为 `notice`。

## 3. 轨迹视图（Network 风格）

实现位置：`gui/frontend/dist/trajectory-view.js` + `gui/frontend/dist/trajectory.js`。

### 3.1 对话区子页（对话 / 轨迹）

`index.html` 在 workspace 顶部有主视图页签条 `.conversation-tabs`，与右栏
页签共享一套停靠布局（`dock-layout.js`）：对话/轨迹两页可留在主视图，也可
拖拽与右栏子页置换后停靠到右栏。激活切换是纯本地 DOM 状态，不进入 Snapshot；
切回对话子页时由 `chatView.renderConversation` 重新判定 empty-state 与加载
更早入口；聊天区 chip 点击经 `app.js` 的 `revealView("trajectory")` 让轨迹页
在它当前所在栏激活后再定位行。

### 3.2 面板结构

`createTrajectoryView` 在容器内建立五个固定子区：

1. **上下文轴**（`renderContextAxis`）：多线谱（分轨）布局，见下节。轴内除
   五条响应类型轨外，还内联两条元数据轨——「前缀注入」与「压缩」，替代旧
   独立「前缀注入」面板（`renderPromptInjection` 已删除）。
2. **轴详情**（`renderAxisDetail`）：点击轴上前缀层块 / 压缩刻度展开的详情
   区（前缀层全文或压缩公开元数据）；默认收起，再次点击同一块或「收起」关闭。
3. **过滤条**（`renderTrajectoryFilters`）：全部 / 输入 / LLM / 工具 / 错误 /
   通知，各带计数徽标（Network 面板 filter 语义）；当前过滤由
   `onFilterChange` 回传给 app.js（`state.trajectoryFilter`）。
4. **摘要条**（`renderTrajectorySummary`）：共 N 条 · 成功 X · 失败 Y · 运行 Z。
5. **表格区**（`renderTrajectoryTable`）：表头 + 记录行，keyed reconciliation。

### 3.2.1 上下文轴（多线谱分轨）

上下文轴把 `TRAJECTORY_KINDS` 的每种响应类型固定为一条横轨（输入 / LLM /
工具 / 错误 / 通知），五轨上下叠放，像总谱一样共用同一条横轴；横轴语义与
旧单轨一致，仍是「对话顺序 + 内容体量」，不是时间轴：

- 每个轨迹记录按 `contextAxisWeight` 占全局体量的比例获得一个块；块的起点
  是此前所有记录体量占比的累计（`--x`），块宽是自身体量占比（`--w`），
  落在本类型轨道上。
- 某类型在对应区段没有记录时，该轨留空（`.is-empty`，只显示一条细刻度线），
  因此一眼能看出「哪些类型在哪个阶段消耗了上下文」以及空窗。
- 每个块仍是 `<button class="axis-segment is-<kind>">`，带
  `data-trajectory-key`；点击后 `trajectory-view.js` 先切回全量过滤，再滚动
  定位并短暂高亮对应轨迹行。图例随分轨省略——轨道标签（图标 + 类型名）即图例。
- 空数据渲染 `.context-axis-empty` 引导文案。

### 3.2.2 元数据轨：前缀注入 + 压缩（并入轴，US-2）

轴把两类「模型上下文治理」事实内联为元数据轨，不再单独拎出来：

- **前缀注入轨**（`is-prefix`，轴顶部）：数据源 `Bridge.PromptLayers()`
  （`app.js` 打开轨迹子页时拉取；effort/技能/任务级变化在 `runtime.changed`/
  `task.changed` 时若轨迹页激活会刷新）。每条 system 前缀层
  （identity/base/effort/instructions/skill）是一个可点击块，**横跨整条轴**：
  段宽 = 该层文本在全部前缀层中的字符占比（`prefixLayerSegments`），因为它
  在每一轮请求都先于对话注入且字节稳定——轴表达它的组成，不假装它有逐消息
  坐标。可 trace 粒度 = 会话级当前层；`PromptStack` 不持久化每请求的层增量
  历史，这是当前实现的边界。点击段 → 轴详情显示该层全文（层文本不进入
  Snapshot，详情按需渲染且转义）。
- **压缩轨**（`is-compress`，轴底部）：数据源 `Snapshot.Task.ContextCompactions`
  （后端每次压缩发布的公开元数据，随 `snapshot.changed` 全量刷新到达）。
  每次压缩画一个刻度，x = 压缩发生时会话推进到的体量位置（锚定最后一条
  `started_at <= compacted_at` 的记录，`compactionMarks`）；压缩点早于已加载
  窗口时钳到轴起点并在详情注明。刻度只含公开字段（version/reason/
  messages_before/estimated_tokens/compacted_at），点击开详情并提示折叠原文
  可经 `read_compressed_turn` / `search_history` 读回。
- 元数据块不带 `data-trajectory-key`（没有可定位的轨迹行）；点击由
  `trajectory-view.js` 分派到轴详情开关，普通轨迹块仍走定位逻辑。

### 3.3 记录行

每行是 `<details class="trajectory-row">`：

- **主行**（`<summary>`）：时间 / 类型徽标 / 名称 / 状态（OK / ERR / RUN）/ 耗时 / 大小；
  行左侧 3px 语义色状态条（复用 status token，不新增同义色）。
- **详情**（展开）：工具记录为 IN/OUT 双栏，其余类型为 BODY 单栏；复用
  `.io-panel` 类名与交互契约（复制完整 payload、展开完整内容、`result_ref`
  分页读回），与对话工具卡片一致。

### 3.4 数据流

```text
Snapshot.conversation → buildTrajectory（分类+配对） → filterTrajectory（过滤）
  → renderTrajectoryTable → trajectory-view keyed reconcile → DOM
Snapshot.Task.ContextCompactions → compactionMarks → renderContextAxis（压缩轨）
Bridge.PromptLayers（会话级当前层） → prefixLayerSegments → renderContextAxis（前缀轨）
  → 点击元数据块 → renderAxisDetail（轴详情）
```

增量事件（`message.added` / `message.delta` / `tool.started` /
`tool.completed`）到达时 `app.js` 重新派生并渲染轨迹；轨迹子页在主视图与右栏
都未激活时只缓存数据面，不碰 DOM（懒渲染）。压缩发布 `snapshot.changed`
触发整份刷新，`task.context_compactions` 随快照进入轴数据面，无需新增后端契约。

## 4. 渲染与安全

- 所有模型/工具/用户文本经 `escapeHtml` 或进入 payload Map（`<pre>` 文本
  节点），不做未受控 HTML 注入；前缀层文本只出现在轴详情且转义。
- 大输出走既有截断链路：快照截断（`snapshot_tool_output_chars`）→ 预览 +
  `result_ref` → 前端「加载完整输出」经 `Bridge.ToolResultContent` 分页读回。
- 行内 `details`/`io-panel` 展开状态在 keyed 替换时捕获/恢复（与
  `conversation-view.js` 同一套局部 UI 状态规则）。
- 元数据轨只消费公开字段：轴不携带 checkpoint 正文、system prompt 之外的
  私有指令、工具结果或原始对话。

## 5. 自动化证据

- `trajectory.test.mjs`（24 例）：响应类型分类、空 assistant 跳过、tool 配对
  （按 id / 按 name / 错误状态）、result_ref 截断元数据、过滤与统计、表格行
  稳定 key、上下文轴分轨布局（五轨固定 + 空轨占位 + 共享横轴定位）、前缀注入
  轨（段序/占比/转义、轴内不出现层文本）、压缩刻度（时间锚定/窗口外钳制/
  公开元数据详情）、HTML 转义安全、过滤条计数与 active、摘要计数、时长/大小
  列、空状态。
- `node --test gui/frontend/dist/*.test.mjs` 全量通过（200 例）。

## 6. 审查清单

- 新 renderer 字段是否 escape 或进入 payload Map / 轴详情？
- 稳定 key 是否来自业务实体 ID（message.id / tool.id / 压缩 version）？
- 增量事件是否只在结构变化时重建，流式 delta 是否复用 keyed 节点？
- 过滤/展开/滚动是否只存在前端本地状态？
- 轨迹数据是否仍从权威 conversation 派生、元数据轨是否只消费已有公开信号
  （snapshot.task.context_compactions / Bridge.PromptLayers），未新增后端契约？
- 轴详情是否避免携带 checkpoint 正文 / 工具结果 / 原始对话？
