# Terminal UI

## 生态位

`tui` 是 Bubble Tea 终端前端。它通过 `AppController` 消费 Application Snapshot/Event 并提交用户动作，不直接调用 Seele Engine、Plugin Manager 或 Session Store。

主要调用方：组合根 `main.go`（`-frontend tui`，默认入口）。

## 架构图

```mermaid
flowchart TB
    subgraph TEA["Bubble Tea 主循环"]
        INIT["Init：读取 Snapshot + SubscribeSession(\"\")"]
        UPD["Update：应用 event / 用户按键"]
        VIEW["View：纯投影，不发起 IO"]
    end

    CTRL["AppController<br/>mutation 全部回调"]
    SVC["application.Service"]
    EV["Application event 流（含会话归属判定）"]

    subgraph PANELS["面板"]
        CONV["conversation / input / status"]
        PLANP["plan：按 Effort 与终端宽度渲染"]
        GT["goalteam：Alt+G 目标治理 / Alt+T 团队看板"]
        SUG["suggest：/ # $ 前缀建议"]
        SPL["splash：启动画面"]
    end

    INIT --> EV
    EV --> UPD
    UPD --> VIEW
    UPD --> CTRL
    CTRL --> SVC
    VIEW --> CONV
    VIEW --> PLANP
    VIEW --> GT
    VIEW --> SUG
    SPL --> INIT
```

本地状态只含光标、viewport、输入框、suggestion 与布局；业务事实一律来自
Snapshot/Event，会话归属由 application 投递端判定，TUI 不自行判断。

## 文件结构

| 文件 | 职责 |
|---|---|
| `tui.go` | `AppController`、Model、Init/Update 主循环。 |
| `view.go` | 总体布局、conversation/input/status 渲染。 |
| `stream.go` | Application event 到 Tea message 的桥接。 |
| `dialog.go` | Interaction/account/session 等选择面板。 |
| `plan.go` | 按 Effort 和终端宽度渲染 Plan 生命周期。 |
| `goalteam.go` | 目标治理（Alt+G）与团队看板（Alt+T）的只读面板：投影 + 面板键 + 团队读面的异步取值。Agent Team 成员表读面已随退场删除（2026-10-12 J-1a）。 |
| `tool_record_panel.go` | Alt+T 面板下半段的**工具记录看板**（2026-10-15）：读 `Application.ToolRecordBoard()`（可选读面 + 类型断言，未装配整块退场），与 GUI「状态 → 工具记录」同源；成功与失败都记（开关关着或没记过时整块退场）。 |
| `suggest_view.go` | `/`、`#`、`$` suggestions。 |
| `state.go` / `types.go` | UI cell、message 和内部状态。 |
| `styles.go` | Lipgloss 主题。 |
| [`splash/`](splash/README.md) | 启动画面。 |

## 状态流

Model 初始化读取 Snapshot 并以 `SubscribeSession("")`（跟随当前视图会话）订阅 Application events，与 GUI 共用 application 投递端的会话归属判定，TUI 自身不判定事件属于哪个会话。Update 根据 event 更新本地投影；提交、取消、选择、翻页等 mutation 全部回调 AppController。View 是纯投影，不发起 IO。

TUI local state 只包含光标、viewport、输入框、suggestion 和布局信息；conversation/runtime 等业务事实来自 Snapshot/Event。

待批计数承载面（2026-09-03 收口）：TUI 无会话侧栏，采用最小可用面——状态行
右侧显示跨会话待批计数（`待批:N`），正文区顶部在有待批会话时显示提示行
（计数 + 短 ID）。数据源是目录联合镜像 `Snapshot.Sessions` 的
`awaiting_approval` 行；单格 `Interaction` 仍只表达当前视图会话审批（切换
到目标会话后经既有 dialog + `ResolveInteraction` 处理）。不做平行会话管理。

## 交互和关闭

- Enter 提交原始输入。
- 输入前缀（sigil）与后端同一张表（`application.HasSigilPrefix` / `application.SigilOf`）：
`/` 可执行入口、`#` 切换 Plugin、`$` 召回 Skill；`@` 手动召唤团队**已退场**（2026-10-12 J-1a），不再是前缀。旧肌肉记忆打 `@` 时按普通文本下发，不再有"未知前缀"提示。
  进入 `suggMode` 并显示 suggestions 面板（数据源 = `Suggestions`，Tab 接受）。
  TUI 不自持字符表：**Suggestions 在 `View()` 渲染路径上**，所以后端那一侧必须保持
  零 I/O（`@` 因此只列内置团队形态，不读团队库）。
- 只读面板：`alt+g` 目标治理（数据源 = `Snapshot.Runtime.GoalGovernance`，与 GUI
  「目标」面板同源投影，无活跃 goal 时给上线入口提示）、`alt+t` 团队（成员/发言
  顺序/定时 agent/调度运行态，取值走一次 `tea.Cmd` 调
`AppController` 的**目标只读面**（Snapshot.Runtime.GoalGovernance）与**团队看板只读面**（Snapshot.Runtime.TeamworkBoard）：两者都在会话快照里，Alt+G / Alt+T 因此不排任何 RPC。原先 Alt+T 上半段的 Agent Team 成员表读面（`AgentTeamView(mainSessionID)`）已随 Agent Team 用户面退场（2026-10-12 J-1a）。
  已实现，与 GUI 经 Bridge 读的是同一个方法；读面缺失或后端报错时面板给出明确
  文案）。面板只读：不提交输入、不改后端状态、不新增只对终端生效的业务事实；
  有待批选择时不打开（键义不打架）。`esc` 关闭，`alt+t` 在团队面板上再按 = 刷新。
  `AppController` 主接口因此**不需要**新增方法：读面是可选接口，装配根不改也能跑。
- 分页：`pgup`（viewport 到顶且还有更早历史时取更早一页）、`home` 同上；
  `end` 在窗口已锚定在更早历史（回看中）时回到最新一页
  （`LoadLatestHistory`）——后端分页在回看期间不再把窗口拽回尾部，这是
  终端端的显式出口。`LoadMoreHistory(limit<=0)` 表示「一整窗」。
- Ctrl+C 在 running chat 时取消对应 request，否则按产品语义处理复制/退出。
- Interaction 键选择通过 `ResolveInteraction` 返回 Application。
- 退出由 composition root 协调 graceful shutdown。

## Review 指南

- Update 不应阻塞；IO 必须包装为 `tea.Cmd`。
- Event 丢失时是否能从 Snapshot 恢复，而不是永久错位。
- 宽度计算使用 display width，中文/emoji 不应破坏布局。
- Plan status icon、interaction option 和 session ID 是否与 Application model 一致。
- 不要在 TUI 新增只对终端生效的业务状态。

## 测试

```text
go test ./tui/... -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### dialog.go

- `func (model Model) handleInteractionKey(message tea.KeyMsg) (tea.Model, tea.Cmd)`
- `func resolveInteraction(interaction InteractionWriter, id, optionID string) tea.Cmd`

### goalteam.go

- `func (model Model) handlePanelKey(message tea.KeyMsg) (bool, tea.Model, tea.Cmd)` — handlePanelKey 处理目标/团队面板的按键：Alt+G 目标治理、Alt+T 团队看板、Esc 关闭。
- `func (model Model) panelHeight() int` — panelHeight 是面板占用的行数（0 = 未打开）；convHeight 用它扣减对话区高度。
- `func (model Model) renderPanel() string` — renderPanel 渲染面板正文（空串 = 未打开）。
- `func (model Model) panelLines() []string` — panelLines 是面板的唯一渲染源：高度与正文都由它给出，两者不可能不一致。
- `func (model Model) goalPanelLines() []string` — goalPanelLines 渲染目标治理只读面（数据源 = Snapshot.Runtime.GoalGovernance，
- `func (model Model) goalHistoryLines() []string` — goalHistoryLines 渲染「历史目标」一节：本会话已收口/中止目标的只读账本。
- `func formatGoalClosedAt(at int64) string` — formatGoalClosedAt 把存档里的 unix 秒压成 "YYYY-MM-DD HH:MM"（0/负数 → ""，
- `func (model Model) teamPanelLines() []string` — teamPanelLines 渲染团队看板（数据源 = Snapshot.Runtime.TeamworkBoard，与 GUI
- `func (model Model) teamBoardLines() []string` — teamBoardLines 渲染**团队看板**（数据源 = Snapshot.Runtime.TeamworkBoard，与 GUI
- `func rosterText(board *dto.TeamworkBoardView) string` — rosterText 是在编一行：有产品级上限时写「在编 n/max」（看板要能区分"正常"与"顶到上限"），
- `func formatByteSize(bytes int64) string` — formatByteSize 把字节数压成终端友好的短形态（0 → "0B"，不写"0.0KiB"）。
- `func clampLines(lines []string, limit int) []string` — clampLines 截断超长面板：多出的内容折叠成一行提示，避免挤掉对话区。
- `func (model Model) textLimit() int` — textLimit 是面板正文按终端宽度换算的展示上限（中文按 1 rune 计，宽度安全余量 6）。
- `func oneLine(text string, limit int) string` — oneLine 把多行文本压成一行并按 rune 截断（面板是单行块的堆叠，不能有裸换行）。
- `func fallback(value, placeholder string) string` — fallback 取非空值（面板上的空字段显示为占位符而不是空白）。
- `func goalBadge(snapshot application.Snapshot) string` — goalBadge 是状态行的 goal 短标记（与 GUI 的 GOAL badge 同口径：有活跃治理

### goalteam_test.go

- `func goalSnapshot() application.Snapshot`
- `func altRuneKey(r rune) tea.KeyMsg` — altKey 造 Alt+<rune> 按键（tea 的 Alt+字母 = 带 Alt 修饰的 rune 键）。
- `func escKey() tea.KeyMsg`
- `func press(t *testing.T, model Model, message tea.KeyMsg) (Model, tea.Cmd)` — press 走一次 Update，返回更新后的 Model（测试只关心面板状态与投影）。
- `func TestGoalPanelRendersGovernanceProjection(t *testing.T)`
- `func TestGoalPanelWithoutActiveGoalPointsAtEntry(t *testing.T)`
- `func TestGoalPanelShowsRecoveredFrameAndHistoryLedger(t *testing.T)` — TestGoalPanelShowsRecoveredFrameAndHistoryLedger：目标面板的两条新痕迹
- `func TestPanelHeightMatchesRenderedLines(t *testing.T)` — TestPanelHeightMatchesRenderedLines：convHeight 用 panelHeight 扣高度，两者
- `func TestPanelKeysDoNotHijackInteractionOrInput(t *testing.T)` — TestPanelKeysDoNotHijackInteractionOrInput：待批选择优先；普通按键进入输入框
- `func TestStatusBarShowsGoalBadge(t *testing.T)`
- `func TestPanelShortcutAvailableWhileRunning(t *testing.T)` — TestAltGAndAltTAreNotGlobalShortcutNoise：面板键在 running 回合里同样可用

### plan.go

- `func planNodeIcon(status application.NodeStatus) string`
- `func planNodeColor(status application.NodeStatus) lipgloss.Color`
- `func planStatusIcon(status application.PlanStatus) string`
- `func planModeLabel(plan *application.PlanState) string` — planModeLabel 区分 tasklist 门禁与 plan_run 执行：PlanRunning 时是 plan 模式
- `func progressBar(fraction float64, width int) string` — progressBar 渲染一个水平进度条 [████░░░░]。
- `func PlanPanel(plan *application.PlanState, effort string, termWidth int) string` — PlanPanel 根据 effort 渲染 Plan 面板。无活跃 Plan 时返回空字符串。
- `func PlanPanelHeight(plan *application.PlanState, effort string) int` — PlanPanelHeight 返回 plan 面板占用的行数（用于 convHeight 计算）。
- `func renderPlanLite(p *application.PlanState, _ int) string` — ── Lite ─────────────────────────────────────────────────────
- `func renderPlanMedium(p *application.PlanState, width int) string` — ── Medium ───────────────────────────────────────────────────
- `func renderPlanHigh(p *application.PlanState, width int) string` — ── High ─────────────────────────────────────────────────────
- `func renderPlanMax(p *application.PlanState, width int) string` — ── Max ──────────────────────────────────────────────────────
- `func styleStatusText(status application.NodeStatus) lipgloss.Style` — styleStatusText 给节点状态上色（参数是**契约枚举**：读状态词的地方只许引常量，
- `func completedCount(nodes []application.PlanNode) int`

### queue_edit_test.go

- `func newQueueFakeApp(queue []string) *queueFakeApp`
- `func (app *queueFakeApp) Snapshot() application.Snapshot`
- `func (app *queueFakeApp) Subscribe(buffer int) application.Subscription`
- `func (app *queueFakeApp) SubscribeSession(sessionID string, buffer int) (application.Subscription, error)`
- `func (*queueFakeApp) Submit(context.Context, string) error`
- `func (app *queueFakeApp) CancelChat(id string) bool`
- `func (*queueFakeApp) Suggestions(string) []application.Suggestion`
- `func (*queueFakeApp) Resolve(context.Context, string, string) error`
- `func (*queueFakeApp) SwitchEffort(string) error`
- `func (*queueFakeApp) LoadMoreHistory(int) error`
- `func (*queueFakeApp) LoadLatestHistory() error`
- `func (app *queueFakeApp) ReorderQueuedInput(_ string, from, to int) error`
- `func (app *queueFakeApp) RecallQueuedInput(_ string, index int) (string, error)`
- `func (app *queueFakeApp) deps() Dependencies` — deps 装配本假实现的端口：队列编辑面在这里，工具记录读面刻意留 nil。
- `func altKey(r rune) tea.KeyMsg`
- `func TestQueueFocusReorderRoutesThroughApplication(t *testing.T)` — TestQueueFocusReorderRoutesThroughApplication Alt+Q 进入队列焦点，Shift+↓
- `func TestQueueFocusRecallWritesComposer(t *testing.T)` — TestQueueFocusRecallWritesComposer Alt+R 撤回选中项：原文进输入框（不覆盖
- `func TestQueueFocusDoesNotSwallowGlobalKeys(t *testing.T)` — TestQueueFocusDoesNotSwallowGlobalKeys 队列焦点不吞全局键：Ctrl+C 仍是停止
- `func TestQueueFocusRejectedWhenQueueEmpty(t *testing.T)` — TestQueueFocusRejectedWhenQueueEmpty 没有排队输入时不进入焦点，给明确提示。
- `func TestRenderQueueHighlightsSelection(t *testing.T)` — TestRenderQueueHighlightsSelection 队列焦点下选中行高亮 + 快捷键提示；

### state.go

- `func (cell Cell) Render(_ int) string`
- `func renderConversation(messages []application.Message, width int) string`

### stream.go

- `func waitApplicationEvent(subscription application.Subscription) tea.Cmd`
- `func submitInput(submission Submitter, input string) tea.Cmd`
- `func loadMoreHistory(history HistoryPager, limit int) tea.Cmd`
- `func loadLatestHistory(history HistoryPager) tea.Cmd`
- `func tickEvery(d time.Duration) tea.Cmd` — tickEvery 每 d 间隔发送一次 tickMsg，用于刷新界面中的时间显示等。

### suggest_view.go

- `func renderSuggestions(suggestions []application.Suggestion, selected, offset, width int, input string) string`

### team_board_panel_test.go

- `func boardSnapshot() application.Snapshot` — boardSnapshot 在 goal 快照上挂一份团队看板投影（与后端下发的形状一致）。
- `func TestTeamPanelShowsTeamBoardProjection(t *testing.T)`
- `func TestTeamPanelOmitsTeamBoardWithoutPlan(t *testing.T)`
- `func TestTeamBoardLinesReturnNilWithoutPlan(t *testing.T)`

### tool_record_panel.go

- `func (model Model) toolRecordLines() []string` — toolRecordLines 渲染工具记录：一行表头（开关 + 合计成功/失败）+ 每工具一行
- `func topReason(reasons []application.ToolRecordReason) (string, int)` — topReason 取失败最多的原因（并列取词表序在前的那一个，读数确定）。
- `func toolRecordReasonLabel(reason string) string` — toolRecordReasonLabel 把原因标识翻成终端里的短标签。词表只有一份（契约
- `func shortToolRecordTime(at time.Time) string` — shortToolRecordTime 只保留日期与分钟（终端一行放不下秒）。
- `func toolRecordReasonSummary(reasons []application.ToolRecordReason) string` — toolRecordReasonSummary 把原因表折成一行（导出给将来的终端详表用；现在只在

### tool_record_panel_test.go

- `func (app *toolRecordFakeApp) Board() application.ToolRecordBoard`
- `func (app *toolRecordFakeApp) deps() Dependencies` — deps 在 fakeApp 的端口上再绑定工具记录读面。
- `func TestToolRecordPanelRendersSuccessFailureAndTopReason(t *testing.T)`
- `func TestToolRecordPanelStaysSilentWhenDisabledOrUnwired(t *testing.T)` — 关着计数 / 没记过 / 未装配读面 → 整块不出现。
- `func TestToolRecordPanelSurfacesLedgerError(t *testing.T)` — 台账落盘失败要显示成告警（读数照记，但"写不进去"必须看得见）。

### tui.go

- `func NewModel(deps Dependencies) Model`
- `func (model Model) Init() tea.Cmd`
- `func (model Model) Update(message tea.Msg) (tea.Model, tea.Cmd)`
- `func (model Model) windowed() bool`
- `func (model Model) handleKey(message tea.KeyMsg) (tea.Model, tea.Cmd)`
- `func (model Model) handleQueueKey(message tea.KeyMsg) (bool, tea.Model, tea.Cmd)` — handleQueueKey 处理「队列焦点」下的按键。handled=false 表示该键不属于队列
- `func (model *Model) useQueueEditor() (QueueEditor, bool)` — useQueueEditor 是「队列编辑面在不在」的唯一判据：未装配时给出退场提示，
- `func (model Model) reorderQueued(from, to int) (Model, tea.Cmd)` — reorderQueued 调换排队顺序（from → to）：顺序事实源在后端会话队列，TUI 不
- `func (model Model) recallQueued(index int) (Model, tea.Cmd)` — recallQueued 撤回排队输入到输入框（原文追加在当前草稿之后，不覆盖），并
- `func (model Model) queueSelection() int` — queueSelection 返回队列焦点下的有效选中行（钳到 [0, len-1]）。
- `func copyLastResponse(model Model) tea.Cmd` — copyLastResponse 复制最后一条 assistant 回复到系统剪贴板。
- `func (model Model) handleEnter() (tea.Model, tea.Cmd)`
- `func (model *Model) checkPaste() bool` — checkPaste 检测 textarea 是否包含多行粘贴内容，是则折叠为占位符。
- `func (model *Model) foldPaste(old, newVal string)` — foldPaste 检测单次按键中是否插入了大量文本（逐字符粘贴特征），是则折叠为占位符。
- `func (model *Model) afterInput()`
- `func (model Model) autoResizeTextarea() Model`
- `func (model Model) acceptSuggestion(suggestion application.Suggestion) Model`
- `func (model *Model) syncInteractionSelection()`

### tui_test.go

- `func newFakeApp() *fakeApp`
- `func (app *fakeApp) Snapshot() application.Snapshot`
- `func (app *fakeApp) Subscribe(buffer int) application.Subscription`
- `func (app *fakeApp) SubscribeSession(sessionID string, buffer int) (application.Subscription, error)`
- `func (app *fakeApp) Submit(_ context.Context, input string) error`
- `func (app *fakeApp) CancelChat(id string) bool`
- `func (*fakeApp) Suggestions(string) []application.Suggestion`
- `func (app *fakeApp) Resolve(_ context.Context, id, option string) error`
- `func (*fakeApp) SwitchEffort(string) error`
- `func (app *fakeApp) LoadMoreHistory(limit int) error`
- `func (app *fakeApp) LoadLatestHistory() error`
- `func (app *fakeApp) deps() Dependencies` — deps 装配本假实现真正提供的端口。队列编辑与工具记录读面刻意留 nil：
- `func TestStatusBarShowsCrossSessionPendingApprovalCount(t *testing.T)` — TestStatusBarShowsCrossSessionPendingApprovalCount E：TUI 最小承载面 =
- `func TestEnterSubmitsRawInput(t *testing.T)`
- `func TestInteractionKeyResolvesThroughApplication(t *testing.T)`
- `func TestCtrlCCancelsActiveChat(t *testing.T)`
- `func TestPlanNodeLifecycleIconsAreDefined(t *testing.T)`
- `func TestFoldedPasteSubmitsOriginalContent(t *testing.T)`
- `func TestCheckPasteFoldsLongSingleLine(t *testing.T)`
- `func TestEditingPastePlaceholderClearsHiddenBuffer(t *testing.T)`
- `func TestInputHistoryRestoresDraft(t *testing.T)`
- `func TestSuggestionNavigationAndAcceptance(t *testing.T)`
- `func TestSigilPrefixesDriveSuggestionMode(t *testing.T)` — TestSigilPrefixesDriveSuggestionMode 钉住四个输入前缀在 TUI 侧的同一口径：
- `func TestApplicationEventRefreshesSnapshot(t *testing.T)`
- `func TestExitEventQuits(t *testing.T)`
- `func TestSubmitErrorBecomesUIError(t *testing.T)`
- `func TestWindowSizeInitializesViewport(t *testing.T)`
- `func TestLoadMoreHistoryCommandUsesRequestedLimit(t *testing.T)`
- `func TestTickRefreshesRunningSnapshot(t *testing.T)`
- `func TestEndKeyReturnsToLatestWhileBrowsingEarlierHistory(t *testing.T)` — TestEndKeyReturnsToLatestWhileBrowsingEarlierHistory 回看更早历史（窗口未

### types.go

- `func currentSuggestions(model Model) []application.Suggestion`

### view.go

- `func (model Model) convHeight() int`
- `func (model Model) topPanelH() int`
- `func (model Model) planPanelH() int`
- `func (model Model) midPanelH() int`
- `func (model Model) bottomPanelH() int`
- `func (model Model) View() string`
- `func (model Model) pendingSessions() []application.SessionInfo` — pendingSessions 返回目录中处于 awaiting_approval 的会话行（跨会话待批
- `func (model Model) pendingApprovalCount() int` — pendingApprovalCount 返回跨会话待批总数（状态行计数；单格审批仍只表达
- `func (model Model) renderPendingApprovals() string` — renderPendingApprovals 渲染待批会话的最小提示行：跨会话计数 + 短 ID。
- `func (model Model) renderInputLine() string`
- `func (model Model) renderConversation() string`
- `func effortBadge(effort string) string`
- `func (model Model) renderStatusBar() string`
- `func (model Model) renderQueue() string`
- `func (model Model) renderInteraction() string`
- `func (model Model) renderShortcuts() string`
- `func (model *Model) syncView()`
- `func FormatInput(textareaView string) string` — FormatInput 格式化输入行。
- `func max(first, second int) int`
