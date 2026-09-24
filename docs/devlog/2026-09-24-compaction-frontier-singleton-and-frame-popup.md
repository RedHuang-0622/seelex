# 压缩分界是会话单例 + 折叠帧正文弹框（前端可见面） — 2026-09-24（1 轮）

> 触发：用户在压缩过的会话里看到**多条**「以上 … 已被折叠」虚线，读数与实际不符（像压缩了很多次、
> 折掉了一大片）；右栏「上下文压缩」在「状态/概要」折叠起来时整块消失；想看折叠帧正文只能在窄栏里读；
> 按完回车再抬头，进度条已撤条、回执只说"已压缩"，看不出这一轮到底走了哪几关。
> 诉求：分界只该有一条、右栏那块要一直看得见、帧正文要能弹框查看且可调大小、回执要拿得住门禁事实。

## 1. 根因（全部可复现）

### 1.1 分界被当成"每条记录一条线"

- 轨迹侧 `renderCompressionCutRow` 对**每个锚定在本页的压缩刻度**各画一条竖虚线（`is-cut` 轨的
  `×${cuts.length}`）；对话侧同理按"每条记录"插一行。历次记录各画一条 → 两条线之间那段看起来还发给模型，
  其实早已折出 provider 历史。
- 各线各报自己那条记录的区间 → 数字互相矛盾（一条说 `message-1..message-9`，另一条说 `message-10..message-20`），
  读者无法判断"以上"指哪一段。根子在口径：分界是会话当前的**一个**事实，不是每条记录一份文案。

### 1.2 右栏压缩块挂在折叠区里

`index.html` 里 `#context-compactions` 是 `#status-panel`（`<details>` 状态/概要）的子节点：折叠起来时
压缩进度条与历次记录一起消失——而"按下回车到底动没动"正靠这块回答。

### 1.3 帧正文只有一种读法

只有右栏内联展开（`data-compact-open`）。右栏夹在 状态/账户/团队 的折叠区之间、正文又最长，一屏只见几行。

## 2. 改法

### 2.1 口径单点：`compactionFrontier`（compaction-format.js）

新增纯函数返回**会话单例**分界：终点取全部记录里 `message_to` 序号最大者（被折出保留窗口的最后一条消息），
起点取全部记录里最早的一端（"以上"指整段已折出的上下文，不是最后一次折叠那一段）；沿用一个记录的字段名
回流，因此分界文案与条目文案共用 `compactionRangeText` / `compactionCutLabel` / `compactionCutTitle`，
不可能分叉。旁路函数 `messageOrdinal`（`message-663-1` → 663）解决"不能按字符串比大小"（`message-9 > message-663`）。

### 2.2 对话区分界：一页最多一行（components.js + chat-view.js）

- `conversationCompactionAnchor(messages, compactions)`（compaction-format.js）算出落点：本页最后一条
  "消息号 ≤ 前沿消息号"的消息之后；整页都在前沿之后返回 `null`——分界不在本页就不画，凭空插一行是假线。
- `insertCompactionFrontier` 在那一行后插入单行 `chat:compaction-frontier`（`kind: "frontier"`，自成一组，
  不并进"工具过程"），行内是虚分隔 + 说明 + （有帧时）「查看折叠帧正文」按钮。
- `ChatView.renderConversation` 新增 `compactions` 入参；app.js 在整页渲染（`runViewActivation`）与增量
  渲染（`renderIncremental`）两处都传入 `snapshot.task.context_compactions`，落点判定只在
  compaction-format.js 一处。

### 2.3 轨迹「分界」轨：从 `×N` 改成 `×1`

`compactionMarks` 只给**前沿**那个刻度打 `isFrontier` 标记（与右栏、对话区共用同一份口径），
`renderCompressionCutRow` 只画那一条虚线。分界不在本页时不画线，并在轨内写明原因，三种情况分别可读：
没折叠过 / 分界早于已加载窗口 / 分界在第 N 页（压缩轨上该刻度可点击跳页）。历次记录仍逐条可查：
「压缩」轨刻度点击开详情、右栏「上下文压缩」列表。

### 2.4 右栏压缩块移出折叠区（index.html）

`#context-compactions` 移到 `#status-panel` 的 `<details>` **之外**（紧跟概要之后）：状态折叠不再连带
藏掉进度条与记录。

### 2.5 折叠帧正文弹框（index.html + app.js + context-summary.js）

- 记录条目新增「弹框查看」按钮（`data-compact-frame-ref`），与内联展开并存。
- 弹框 `#compaction-frame-modal`（`data-resizable`，走既有 `initModalResize` 右下角手柄，可拖拽调宽高；
  也可用 CSS `resize` 手柄）里的正文区由 `renderCompactionFrameModal` → `renderFrameDetail` 渲染：
  与右栏内联展开**同一段 HTML**（同一份正文、同一套 `data-compact-frame-load` 分页标记），不存在同一 ref 两种读法。
- 入口事件用 `document` 级委托接住 `[data-compact-frame-ref]`：组件只携带 ref，不持有 invoke 依赖
  （同排队卡片的动作按钮）。弹框状态只在视图侧（不进快照）：快照刷新重建记录数组，不影响已读回的正文。
- 读取按 `ToolResultContent(ref, offset, 12000)` 分页；`compactionFrameLoadToken` 丢弃过期响应
  （期间又开了另一条记录 / 点了重试 / 关掉弹框）。
- 没有 `frame_ref` 的记录：右栏写明"本次没有可回读正文"，弹框也明说而不是给一个空壳 viewer。

### 2.6 回执自带门禁清单（context_compact.go + context_runtime/coordinator.go）

进度事件是瞬态的（`revision=0`、不进快照、终局约 2.5s 撤条），而门禁耗时是**回执**该拿住的事实。
`CompactResult` / `ContextCompactionResult` 因此新增 `Gates []CompactionGateTiming`（门禁 id + 本关毫秒，
从 `decision.Gates` 原样带出，不重算、不补零），`compactionRecordNote` 把它渲染成一行事实，
例如 `门禁 reached=6/6 judge<1ms assemble 4ms replace<1ms frame 33ms store<1ms record<1ms`。

只带门禁 id 与毫秒：中文关卡名只在前端 `compaction-format.js` 的 `compactionGateLabels`（同一处只放一种语言），
两份表一定会漂移，因此键序由后端测试 `TestFrontendGateLabelsMatchBackendOrder` 直接读前端文件互钉。
毫秒整数里的 `0` 含义是"不到一毫秒"，两处都渲染成 `<1ms`（不凑成 1ms、也不写成 `0ms`）。

## 3. 验证证据

- `node --test`（`gui/frontend/dist`，含全部前端单测）：**461 pass / 0 fail**（本轮新增 13 条）。
- `go build ./...` 通过；`go test ./...`：**84 个包 0 FAIL**（`application/core` 13.4s 含压缩链路测试）。
- 新增测试（口径都是"渲染/落点"，与实现同源）：
  - `compaction-format.test.mjs`：`messageOrdinal` 读事件号、`compactionFrontier` 取最新终点 + 最早起点、
    无消息号时拒绝猜测、`conversationCompactionAnchor` 三种落点（页内 / 整页更早钳到页尾 / 页上没有已折叠内容→null）。
  - `components.test.mjs`：分界行插在被折出的最后一条消息之后（一行、带 `data-compact-frame-ref`、不带轨迹 key）、
    锚点不在本页绝不插行、说明文字与 ref 全部转义。
  - `trajectory.test.mjs`：分界轨对会话只画一条线（不是每条记录一条）。
  - `context-summary.test.mjs`：每条记录同时给内联与弹框两个入口、弹框正文与右栏展开是同一段 HTML、
    没有 ref 的弹框明说没有正文。
  - `chat-view-input-lock.test.mjs`：`renderConversation` 透传压缩记录（分界的输入）。
- 后端（`application/core`）：`TestExplicitCompactEmitsOrderedProgressGates`、
  `TestAutoCompactionEmitsProgressGates`、`TestExplicitCompactGateTimeline`、
  `TestMaintenanceCompactEmitsOrderedProgressGates`、`TestCompactReceiptCarriesGateChecklist`、
  `TestFrontendGateLabelsMatchBackendOrder`（跨语言钉键序）。

## 4. 已知边界

- 分界只标**前沿**（最新一次折叠的终点），起点取历次里最早的一端；"压缩过几次"由右栏列表与轨迹刻度回答
  （对话区分界行在有多次折叠时附一行"会话共折叠 N 次，这里只标最新一次的终点"）。
- 分界不在当前页时对话区与轨迹都不画线：回翻的历史页上没有任何"还发给模型"的内容，插入假线比少一行更糟。
- 帧正文弹框、右栏、对话区分界都是前端产物：运行中的 GUI 需重载/重启才会生效。
