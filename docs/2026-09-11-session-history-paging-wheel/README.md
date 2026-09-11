# 会话历史分页修复 + 对话时间线轮轴重做

> 状态：已实现（代码与测试是事实来源）
> 日期：2026-09-11
> 涉及模块：`application/core`（会话历史分页与可见窗口）、`gui`（Bridge/无头命令面）、
> `gui/frontend/dist`（分页前端接线 + 轮轴）、`tui`（回到最新出口）
>
> 后续：本文第 4 节的「轮轴」与恢复顺序已于同日二次改版——轮轴改为
> **一条刻度 = 一问一答**，长会话改为**按 message 顺序恢复**。现状以
> [`docs/devlog/2026-09-11-conversation-wheel-and-restore-order.md`](../devlog/2026-09-11-conversation-wheel-and-restore-order.md)
> 与模块 README 为准；本文其余分页修复仍然有效。

## 1. 问题与现场

用户报告：`前端加载会话的逻辑有问题，尤其是长会话加载早期会话时候`，怀疑分页算法；
右侧时间线线条与内容无关（「啥也不是」）。

代码事实（修复前）：

- `application/core/session_history.go` 的 `LoadMoreHistory` **只写
  `service.Core.Snapshot`**（活跃会话的只读镜像），不写会话自己的可见投影
  （`session.View`）。任何一次镜像（新消息、工具事件、会话切换、热挂载）都会
  用 view 的内容整体覆盖 Snapshot，把刚取回的一页抹掉，`HistoryOffset` 退回
  尾部窗口。
- 追加新消息时 `boundViewTailLocked` 无条件把窗口重新贴尾，即使窗口正锚定在
  更早的历史位置——正在翻旧账的会话一旦有事件就跳回尾部。
- 无 record 的旧格式会话冷加载用 `appendHistoryLockedFor` 逐条追加可见消息，
  `TotalMessages` 只等于已装载的尾窗条数：`HistoryOffset` 恒为 0、
  `HasMoreHistory` 恒为 false，长会话的「加载更早」永远点不出来。
- 前端固定请求 `LoadMoreHistory(50)`，而后端窗口是 `limits.history_window`
  （200）：「一页」既不是旧的整窗也不是新的整窗，翻一次只多出半屏又丢掉半屏；
  滚动恢复用 `scrollHeight` 增量，而窗口整体后退时前后高度几乎不变，增量恒为
  0，锚点等于失效。
- 前端 reducer 对 `message.added`/`tool.*` 无条件 upsert；窗口锚定在更早位置时
  尾部新消息会被追加到列表末尾，在窗口与尾巴之间插出断层。
- 右侧「时间线拨轮」按「条目权重」均分轨道（用户输入 3、思考 2…），与内容真实
  位置/高度无关；视口游标只画一条 `--wheel-progress` 横线，点击线条跳转与线
  位置不一致。

## 2. 红灯复现（先红后绿）

新增 `application/core/session_history_pagination_test.go`，用仿生产端口
（record + conversation 区间读同一份 durable 消息）复现长会话分页：

| 用例 | 修复前现场（红灯证据） |
|---|---|
| `TestLoadMoreHistoryPrependsPageAndSurvivesMirror` | `history_offset after mirror = 15, want 8`（取回的一页被镜像抹掉） |
| `TestLoadMoreHistoryPagesToBeginning` | `duplicate message "durable-2" across pages`（offset 未推进，反复取同一页） |
| `TestAppendWhileBrowsingHistoryKeepsWindow` | `history_offset = 17, want 8`（新消息把窗口拽回尾部） |
| `TestLegacySessionWithoutRecordExposesEarlierHistory` | 冷加载 `total=6`（尾窗条数被当成总数）、`has_more_history=false` |
| `TestLoadLatestHistoryReturnsToTail` | `LoadLatestHistory undefined`（缺少回到最新的应用边界） |

（前三条的红灯输出是在同一测试夹具上直接跑出来的；第四条来自同一场景的临时
探针输出 `offset=0 total=6 more=false`，随后固化为用例。）

## 3. 修复

后端（`application/core`）：

1. `LoadMoreHistory(limit)`：一页 = 一整窗（`limit<=0 || limit>window` 时取
   `window`）；分页态写进**会话可见投影**（`SessionViewMutateLocked`）后再
   镜像 Snapshot，窗口收敛为 `[HistoryOffset, HistoryOffset + window)`。
2. 新增 `LoadLatestHistory()`：重读尾部窗口并贴尾，回看期间的新消息一并带回。
3. `view_state`：新增 `viewWindowTailAligned`，
   `SessionViewBrowsingHistoryLocked`，`DurableConversationCount`；`AppendMessageLockedFor`
   在「回看历史」（窗口未贴尾）时只推进 `TotalMessages`、不把新消息写进窗口
   （否则会在窗口尾部插洞并把用户拽回尾部）。
4. `chat.go`：流式增量（活跃/后台）与推理挂接在回看期间跳过——此时窗口最后
   一条不是本轮消息，写它会串写到旧消息上。
5. 旧格式会话冷加载补写 `TotalMessages/HistoryOffset/HasMoreHistory`（总数来自
   provider 历史）。

前端（`gui/frontend/dist`）：

6. `protocol.js`：新增 `historyWindowed(snapshot)`（口径与后端一致，不计 system
   引导消息），窗口离开尾部时 `message.added`/`tool.*` 不再向列表插入。
7. `conversation-view.js`：滚动恢复改为**按消息 key 锚定**（`captureScrollAnchor`/
   `restoreScrollAnchor`），锚点被截掉时退回增量算法；程序化定位统一走
   `setScrollInstantly`（`.conversation` 的 `scroll-behavior` 改回 `auto`）。
8. `app.js`/`chat-view.js`/`index.html`：`LoadMoreHistory(0)`（一整窗）、历史栏
   常驻「加载更早」+「回到最新」（`LoadLatestHistory` + 滚动到底）。
9. `conversation-wheel.js`（新）+ `conversation-wheel.test.mjs`：轮轴重做，见下。

桥与宿主：

10. `gui/bridge.go`、`gui/headless.go`、`tui/tui.go`/`stream.go`：新增
    `LoadLatestHistory` 透传；TUI `end` 键在回看历史时回到最新。

## 4. 轮轴重做（conversation-wheel.js）

旧实现按「条目权重」均分轨道、游标只画进度线，与内容无对应。新实现：

- 从容器直接子项测量**真实几何**（`offsetTop`/`height`）→ 一条线/条目，线高 =
  条目占内容高度的比例（钳制 `[2, 48]px`，上限随轨道高度自适应），位置顶对齐；
- 类型来自渲染层写入的 `data-wheel-kind`（user/agent/think/tools/system/other）
  与摘要 `data-wheel-label`（`components.js` 新增），悬停出标签；
- 视口滑柄反映可视区间，拖拽 1:1 跟手、轨道空白处点击把视口中心对到点击位置、
  点击线条跳转并闪烁定位、键盘 ↑/↓/PgUp/PgDn/Home/End（`role=scrollbar`）；
- 每次渲染与容器缩放重新测量，几何指纹相同则不重写 DOM——加载更早历史、增量
  新消息、换行重排后线条自动与内容一致；
- 空态只做视觉隐藏（`display:none` 会把轨道高度量成 0，轮轴再也出不来）。

纯几何函数（`buildWheelLines`/`wheelThumb`/`scrollTopForThumbTop`/
`scrollTopForFraction`/`lineAtOffset`/`wheelSignature`/`normalizeWheelKind`）离线单测。

## 5. 验证

```text
gofmt -l .                                   # 本次改动文件无输出（其余为存量）
go build ./...                               # OK
go build -tags "gui,desktop,production" ./... # OK
go vet ./application/core/... ./gui/... ./tui/...   # OK
go test ./... -count=1                       # 全绿（含 application/core、gui、tui）
node --test gui/frontend/dist/*.test.mjs     # 227 tests / 227 pass / 0 fail
```

关键新增用例：

- `application/core/session_history_pagination_test.go`
  （分页持久、翻到最早、回到最新带回新消息、回看期间不回卷、旧格式会话总数）。
- `gui/frontend/dist/conversation-wheel.test.mjs`（几何映射、钳制、滑柄反解、
  命中测试、指纹）。
- `gui/frontend/dist/protocol.test.mjs`（窗口判定 + 窗口外消息不入列表）。
- `gui/bridge_test.go`、`gui/headless_test.go`、`tui/tui_test.go`（命令面透传）。

## 6. 未做 / 风险

- 可见窗口仍是有界窗口（上限 `limits.history_window`）：翻回更早历史时窗口整体
  后退，视口下方（更新的内容）被截掉。DOM 虚拟滚动（
  `docs/2026-07-26-gui-conversation-windowing/plan.md`）未实现，这是当前
  「不无限加长可见列表」的代价。
- 本机未跑 `-race`（Windows 本地 `CGO_ENABLED=0`），并发语义仅由既有测试覆盖。
- 未做真机 GUI 目视验证（Wails WebView 未在本环境启动）：轮轴的交互（拖拽手感、
  标签位置、空态隐藏）只经过单元测试与几何演算，建议在真机过一遍。
