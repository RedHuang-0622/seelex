package gui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge"
	seelexctxsearch "github.com/RedHuang-0622/seelex/seelexctx/search"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

const eventName = "seelex:event"

// Application is the narrow application-core contract consumed by the GUI.
// The interface belongs to the caller so the desktop bridge can be tested
// without constructing the Seele runtime.
type Application interface {
	Snapshot() application.Snapshot
	BeginGracefulShutdown()
	WaitForIdle(context.Context) error
	Subscribe(buffer int) application.Subscription
	Submit(context.Context, string) error
	BeginNewSession() error
	// SaveComposerDraft 持久化当前草稿会话的未发送输入（跨重启恢复）。
	SaveComposerDraft(string) error
	ResumeSession(string) error
	// ForkSessionLatest 从指定会话最新完整轮次分支出新会话并切换（fork
	// 一期 GUI 入口；返回子会话 ID）。
	ForkSessionLatest(string) (string, error)
	CancelChat(string) bool
	ResolveInteraction(context.Context, string, string) error
	SelectAccount(context.Context, string) error
	SwitchEffort(context.Context, string) error
	SwitchPlugin(context.Context, string) error
	LoadMoreHistory(int) error
	Suggestions(string) []application.Suggestion
	DeleteSession(string) error
	// SetSessionMeta 写会话展示元数据（置顶/别名/排序位），随会话目录下发。
	SetSessionMeta(string, application.SessionMeta) error
	// WaitCatalogRefresh 等待会话目录 worker 完成一轮覆盖本次变更的刷新，使随后
	// 的 Snapshot() 直接携带权威目录（前端因此不必回填上一次列表伪造状态）。
	WaitCatalogRefresh(context.Context) error
	CreateWorkspace(name, rootPath, gitRemote string) error
	BindWorkspace(workspaceID string) error
	UnbindWorkspace()
	SetFullAccess(bool)
	SessionStorageConfig() (sessionstore.Config, error)
	TestSessionStorage(context.Context, sessionstore.Config) error
	ConfigureSessionStorage(context.Context, sessionstore.Config) error
	SubagentSessionDetail(nodeID string) (*application.SubagentDetail, error)
	SubscribeSubagentLive(nodeID string) ([]dto.SubagentLiveEvent, <-chan dto.SubagentLiveEvent, func(), error)
	ClearSubagentTree() error
	// UpdateWorkItemStatus 更新工作表格任务状态（v1：仅 todo 的
	// pending/doing/done；plan/subagent 由执行器管理）。
	UpdateWorkItemStatus(id, status string) error
	ScheduleTask(context.Context, seelebridge.ScheduledTaskSpec) (*seelebridge.ScheduledTaskStatus, error)
	CancelScheduledTask(string) error
	// SearchHistory 检索会话历史聊天记录（压缩栈索引 → 真实记录；
	// GUI 历史检索面板数据源）。
	SearchHistory(context.Context, string, int) (seelexctxsearch.Result, error)
	// WorkspaceTree 列出当前工作区某目录的子条目（工作树数据源；只含
	// 元数据，不含文件内容）。
	WorkspaceTree(relPath string, depth int) (dto.TreeListing, error)
	// WorkspaceFileCount 统计当前工作区文件/目录数（工作树文件数 badge）。
	WorkspaceFileCount() (dto.TreeCount, error)
	// WorkspaceGitLog 返回当前工作区最近 limit 条提交的拓扑树（提交记录树
	// 数据源；只读元数据，不含 diff/文件内容；非 git 仓库返回 Result.Error）。
	WorkspaceGitLog(limit int) (dto.GitLogResult, error)
	// ToolResultContent 按 result_ref 分页读回完整工具输出（快照被截断的
	// 工具输出，前端"加载完整输出"数据源；复用 read_tool_result 通道）。
	ToolResultContent(context.Context, string, int, int) (application.ToolResultPage, error)
	// PerfStats 返回性能追踪钩子的后端数据面（无内容指标，供前端渲染
	// 进程对照 DOM/JS heap 与快照载荷体积）。
	PerfStats() application.PerfStats
	// PromptLayers 返回当前会话注入的 prompt 前缀层（轨迹视图"前缀注入"
	// 数据源；经桥接单独拉取，不进 Snapshot，避免私有指令泄漏）。
	PromptLayers() []application.PromptLayer
}

// sessionAwareApplication 是 Application 的可选会话级扩展（M1 显式
// sessionID API；旧方法继续委托活跃会话，桌面宿主可按能力渐进接入）。
type sessionAwareApplication interface {
	SubmitToSession(context.Context, string, string) error
	ActivateSession(string) error
	SnapshotOf(string) (application.Snapshot, error)
	SubscribeSession(string, int) (application.Subscription, error)
}

// replayAwareApplication 是 sessionAwareApplication 的再一层可选扩展：宿主能
// 给出带重放窗口的订阅（缓冲写满不丢事件，可按 delivery_seq 增量补取），
// Bridge 才会启用回执重推（C4）。未实现时退回无窗口订阅，溢出仍由 hub 的
// resync.required 兜底。
type replayAwareApplication interface {
	SubscribeSessionWithReplay(sessionID string, buffer, replayWindow int) (application.Subscription, error)
}

// EventEmitter receives Application events after the Bridge has adapted them
// to the stable desktop event names. Desktop hosts pass the function that
// forwards events into their renderer runtime.
type EventEmitter func(context.Context, string, any)

type Options struct {
	Title       string
	Version     string
	ProjectRoot string
	Width       int
	Height      int
	// StartupWarning 非空时，GUI 窗口就绪后会弹出原生错误对话框展示启动
	// 配置警告（配置损坏仍可启动，不闪退）。
	StartupWarning string
}

type AppInfo struct {
	Title   string      `json:"title"`
	Version string      `json:"version"`
	Project ProjectInfo `json:"project"`
}

type ProjectInfo struct {
	Name    string          `json:"name"`
	Root    string          `json:"root"`
	Sources []ProjectSource `json:"sources"`
}

type ProjectSource struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Path string `json:"path"`
}

// Bridge adapts the headless application service to desktop-safe methods.
type Bridge struct {
	app    Application
	info   AppInfo
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	sub    application.Subscription
	// subscribedSessionID 是当前订阅的事件会话键（mu 保护）：草稿早分配
	// SID 后，视图会话在 Submit 物化（或 legacy 引擎回退 StartSession 另发
	// ID）时可能变化；relay 发现事件 sid 与订阅键不一致即重订阅。
	subscribedSessionID string
	wg                  sync.WaitGroup
	running             bool
	emitFn              EventEmitter
	streams             map[string]func()
	// 事件投递回执（C4，均由 mu 保护）：Go→WebView 这条腿没有任何投递反馈
	// （EventEmitter 无返回值），事件"发过了"不等于"渲染层应用了"。ackedSeq 是
	// 渲染层回执的应用水位；落后于订阅水位时 Bridge 从重放窗口增量重推，
	// resendTimer/resendTries 是那次重推的节流与止损。
	ackedSeq    uint64
	resendTimer *time.Timer
	resendTries int
}

const (
	// 会话级事件订阅的缓冲与重放窗口：窗口 ≥ 缓冲，落后一档仍可增量补取，
	// 不必整份重拉快照。
	eventSubscriptionBuffer = 256
	eventReplayWindow       = 1024
	// eventResendDelay 必须显著大于渲染层回执节流（150ms），否则每次正常投递都
	// 会被误判为丢失而白发一遍重复事件（重复虽幂等，但白耗一次跨进程往返）。
	eventResendDelay = 500 * time.Millisecond
	// eventResendMaxTries 后停止重推：渲染层多半已经不可达，继续重推只会掩盖
	// 真问题；下一条事件或下一次回执会重新武装。
	eventResendMaxTries = 3
)

// subagentLiveEventName 是 node 第一视角实时流的前端事件名。
const subagentLiveEventName = "seelex:subagent_live"

func NewBridge(app Application, options Options) (*Bridge, error) {
	if app == nil {
		return nil, errors.New("gui: application is required")
	}
	title := strings.TrimSpace(options.Title)
	if title == "" {
		title = "Seelex"
	}
	return &Bridge{app: app, info: AppInfo{Title: title, Version: options.Version, Project: discoverProject(options.ProjectRoot)}}, nil
}

func discoverProject(root string) ProjectInfo {
	if strings.TrimSpace(root) == "" {
		return ProjectInfo{}
	}
	absRoot, err := filepath.Abs(root)
	if err == nil {
		root = absRoot
	}
	project := ProjectInfo{Name: filepath.Base(filepath.Clean(root)), Root: filepath.Clean(root)}
	candidates := []ProjectSource{
		{Name: "README", Kind: "documentation", Path: "README.md"},
		{Name: "Changelog", Kind: "documentation", Path: "CHANGELOG.md"},
		{Name: "Agent configuration", Kind: "configuration", Path: "seele.yaml"},
		{Name: "Account template", Kind: "configuration", Path: filepath.Join("config", "accounts.example.yaml")},
		{Name: "Plugins", Kind: "capability", Path: "plugins"},
		{Name: "Project docs", Kind: "documentation", Path: "docs"},
	}
	for _, source := range candidates {
		if _, statErr := os.Stat(filepath.Join(root, source.Path)); statErr == nil {
			source.Path = filepath.ToSlash(source.Path)
			project.Sources = append(project.Sources, source)
		}
	}
	return project
}

// Start begins relaying the initial snapshot and subsequent Application events
// to the desktop renderer. It is idempotent.
func (bridge *Bridge) Start(ctx context.Context, emit EventEmitter) {
	bridge.mu.Lock()
	if bridge.running {
		bridge.mu.Unlock()
		return
	}
	bridge.ctx, bridge.cancel = context.WithCancel(ctx)
	bridge.emitFn = emit
	bridge.streams = make(map[string]func())
	bridge.running = true
	loopContext := bridge.ctx
	subscription := bridge.subscribeViewLocked()
	bridge.sub = subscription
	bridge.mu.Unlock()
	bridge.startRelay(loopContext, emit, subscription)
}

// startRelay 启动一个订阅的转发 goroutine（relay 生命周期随 bridge.ctx
// 取消；resubscribe 会关闭旧订阅并另起新 relay）。
func (bridge *Bridge) startRelay(loopContext context.Context, emit EventEmitter, subscription application.Subscription) {
	bridge.wg.Add(1)
	go func() {
		defer bridge.wg.Done()
		if emit != nil {
			emit(loopContext, "seelex:ready", bridge.app.Snapshot())
		}
		for {
			select {
			case <-loopContext.Done():
				return
			case event, ok := <-subscription.Events:
				if !ok {
					return
				}
				if emit != nil {
					emit(loopContext, eventName, event)
				}
				// 会话键漂移（草稿物化 / legacy 引擎回退 StartSession 另发
				// ID）：视图会话已切到 event.SessionID，重建订阅后旧 relay
				// 退出，触发事件本身已先交给渲染层。
				if event.SessionID != "" && bridge.subscribedIDLocked() != event.SessionID {
					bridge.resubscribe()
					return
				}
				// 交给 renderer 只是"发过"；等它回执才算送达。收不到回执时
				// 由 catchUpRenderer 从重放窗口增量重推（C4）。
				bridge.armResend()
			}
		}
	}()
}

// subscribeViewLocked 按**显式当前视图 sid** 订阅事件流（G2：订阅键含
// sid；会话切换由 Bridge 重订阅，不再依赖 application 的"空 sid 跟随视图
// 指针"判定）。草稿期 sid 为空时保留跟随视图过渡口径（G4 早分配 SID 后
// 彻底消失）；调用方必须持有 bridge.mu。
//
// 优先申请带重放窗口的订阅：缓冲写满时事件不丢，落后的渲染层可以按
// delivery_seq 增量补取而不是整份重拉快照（C4）。宿主不支持窗口时退回
// 全局/无窗口订阅（此时溢出仍由 hub 的 resync.required 兜底）。
func (bridge *Bridge) subscribeViewLocked() application.Subscription {
	sessionID := bridge.app.Snapshot().Session.ID
	bridge.subscribedSessionID = sessionID
	if app, ok := bridge.app.(replayAwareApplication); ok {
		if subscription, err := app.SubscribeSessionWithReplay(sessionID, eventSubscriptionBuffer, eventReplayWindow); err == nil {
			return subscription
		}
	}
	if app, ok := bridge.app.(sessionAwareApplication); ok {
		if subscription, err := app.SubscribeSession(sessionID, eventSubscriptionBuffer); err == nil {
			return subscription
		}
	}
	return bridge.app.Subscribe(eventSubscriptionBuffer)
}

// subscribedIDLocked 返回当前订阅的会话键（无订阅时为空串）。
func (bridge *Bridge) subscribedIDLocked() string {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	return bridge.subscribedSessionID
}

// resubscribe 在视图会话切换后重建事件订阅（G2：切换即重订阅，新订阅从
// 当前权威快照基线开始，replay/ack 游标随新订阅重置）。旧订阅立即关闭，
// 渲染层按 delivery_seq 去重，因此切换瞬间可能重复/跳号由新订阅窗口承接。
func (bridge *Bridge) resubscribe() {
	bridge.mu.Lock()
	if !bridge.running {
		bridge.mu.Unlock()
		return
	}
	old := bridge.sub
	loopContext := bridge.ctx
	emit := bridge.emitFn
	subscription := bridge.subscribeViewLocked()
	bridge.sub = subscription
	bridge.ackedSeq = 0
	bridge.resendTries = 0
	bridge.stopResendLocked()
	bridge.mu.Unlock()
	if old.Events != nil {
		old.Close()
	}
	bridge.startRelay(loopContext, emit, subscription)
}

// Stop cancels the event relay and waits until its goroutine has exited. It is
// safe to call more than once.
func (bridge *Bridge) Stop() {
	bridge.mu.Lock()
	if !bridge.running {
		bridge.mu.Unlock()
		return
	}
	cancel := bridge.cancel
	subscription := bridge.sub
	bridge.running = false
	bridge.cancel = nil
	bridge.ctx = nil
	bridge.emitFn = nil
	bridge.stopResendLocked()
	for nodeID, cancel := range bridge.streams {
		cancel()
		delete(bridge.streams, nodeID)
	}
	bridge.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	subscription.Close()
	bridge.wg.Wait()
}

// AckEvents 是渲染层的应用回执（C4）：seq 是它已经应用过的最后一个
// delivery_seq。回执推进即说明事件真送达，重推止损计数随之清零；Bridge 立刻
// 尝试补齐缺口，不必等下一个定时器。
//
// 回执只允许单调推进：Wails 的 invoke 之间无顺序保证，旧回执直接忽略。
func (bridge *Bridge) AckEvents(seq uint64) {
	bridge.mu.Lock()
	if seq <= bridge.ackedSeq {
		bridge.mu.Unlock()
		return
	}
	bridge.ackedSeq = seq
	bridge.resendTries = 0
	bridge.mu.Unlock()
	bridge.catchUpRenderer()
}

// ReplayEvents 供渲染层在 delivery_seq 跳号时主动增量补取（C4）：返回窗口内
// 晚于 sinceSeq 的事件。Covered=false 表示窗口已不覆盖（或宿主订阅没有窗口），
// 调用方必须整份重拉快照。
func (bridge *Bridge) ReplayEvents(sinceSeq uint64) application.ReplayResult {
	bridge.mu.Lock()
	subscription, running := bridge.sub, bridge.running
	bridge.mu.Unlock()
	if !running {
		return application.ReplayResult{}
	}
	return subscription.ReplaySince(sinceSeq)
}

// armResend 武装一次"等回执"的重推：同一时刻至多一个待触发定时器，
// 事件密集期不会堆积定时器。
func (bridge *Bridge) armResend() {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	if !bridge.running || bridge.resendTimer != nil {
		return
	}
	bridge.resendTimer = time.AfterFunc(eventResendDelay, func() {
		bridge.mu.Lock()
		bridge.resendTimer = nil
		bridge.mu.Unlock()
		bridge.catchUpRenderer()
	})
}

// catchUpRenderer 比较渲染层回执水位与本订阅的投递水位：落后就把重放窗口里的
// 事件重推一遍（渲染层按 delivery_seq 去重，重复投递是幂等的）；窗口不再覆盖
// 时改为投递一条带当前水位的 resync.required，让渲染层整份重拉并把水位抬到该
// 处，避免对补不回来的区间无限重推。
func (bridge *Bridge) catchUpRenderer() {
	bridge.mu.Lock()
	if !bridge.running || bridge.emitFn == nil {
		bridge.mu.Unlock()
		return
	}
	subscription, emit, loopContext := bridge.sub, bridge.emitFn, bridge.ctx
	acked := bridge.ackedSeq
	watermark := subscription.DeliveryWatermark()
	if acked >= watermark {
		bridge.resendTries = 0
		bridge.mu.Unlock()
		return
	}
	if bridge.resendTries >= eventResendMaxTries {
		// 止损：渲染层大概率不可达。下一条事件或下一次回执会重新走这里。
		bridge.mu.Unlock()
		return
	}
	bridge.resendTries++
	replay := subscription.ReplaySince(acked)
	bridge.mu.Unlock()

	if replay.Covered && len(replay.Events) > 0 {
		for _, item := range replay.Events {
			emit(loopContext, eventName, item)
		}
		bridge.armResend()
		return
	}
	// 窗口补不齐：显式要求整份重拉，并把本订阅水位交给渲染层作为新基准
	// （渲染层应用后按该值回执，缺口到此收敛）。
	emit(loopContext, eventName, application.Event{
		ProtocolVersion: application.ProtocolVersion,
		DeliverySeq:     watermark,
		Kind:            application.EventResyncRequired,
	})
	bridge.armResend()
}

func (bridge *Bridge) stopResendLocked() {
	if bridge.resendTimer != nil {
		bridge.resendTimer.Stop()
		bridge.resendTimer = nil
	}
	bridge.resendTries = 0
}

func (bridge *Bridge) requestContext() context.Context {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	if bridge.ctx != nil {
		return bridge.ctx
	}
	return context.Background()
}

func (bridge *Bridge) Info() AppInfo { return bridge.info }

// sessionCatalogSettleTimeout 是"等目录收敛"的预算：超过它就直接返回，让
// renderer 拿到当时的快照，随后 worker 发布的 snapshot.changed 仍会到达。
const sessionCatalogSettleTimeout = 2 * time.Second

// settleCatalog 在会改变会话目录的命令返回前等一轮目录刷新，使前端紧接着
// 重拉的 Snapshot() 已含本次变更（新建/删除/分支/展示元数据）。超时按最佳
// 努力处理而不向上报错：命令本身已成功，未收敛的列表由异步
// snapshot.changed 补齐，前端不得为此回填旧列表。
func (bridge *Bridge) settleCatalog() {
	ctx, cancel := context.WithTimeout(bridge.requestContext(), sessionCatalogSettleTimeout)
	defer cancel()
	_ = bridge.app.WaitCatalogRefresh(ctx)
}

func (bridge *Bridge) Snapshot() application.Snapshot { return bridge.app.Snapshot() }

// SubagentSessionDetail 返回子代理节点详情（会话记录 + 状态/耗时/输出）。
func (bridge *Bridge) SubagentSessionDetail(nodeID string) (*application.SubagentDetail, error) {
	return bridge.app.SubagentSessionDetail(nodeID)
}

// SubagentDetailStreamStart 订阅 node 第一视角实时流并把事件推送到前端
// （seelex:subagent_live；阶段/工具事件到达即发，即时输出面）。返回
// **历史回放**（subagent start 以来的有界事件缓冲），前端打开即渲染滚动
// 上下文，之后实时事件继续追加。重复启动同 node 时先停旧流（幂等）。
func (bridge *Bridge) SubagentDetailStreamStart(nodeID string) ([]dto.SubagentLiveEvent, error) {
	if nodeID == "" {
		return nil, errors.New("gui: node id required")
	}
	history, ch, cancel, err := bridge.app.SubscribeSubagentLive(nodeID)
	if err != nil {
		return nil, err
	}
	bridge.mu.Lock()
	if bridge.emitFn == nil {
		bridge.mu.Unlock()
		cancel()
		return nil, errors.New("gui: bridge is not started")
	}
	if existing := bridge.streams[nodeID]; existing != nil {
		existing()
	}
	ctx := bridge.ctx
	emit := bridge.emitFn
	bridge.streams[nodeID] = cancel
	bridge.wg.Add(1)
	bridge.mu.Unlock()

	go func() {
		defer bridge.wg.Done()
		for {
			select {
			case event, ok := <-ch:
				if !ok {
					return
				}
				emit(ctx, subagentLiveEventName, event)
			case <-ctx.Done():
				return
			}
		}
	}()
	return history, nil
}

// SubagentDetailStreamStop 停止 node 第一视角实时流（幂等）。
func (bridge *Bridge) SubagentDetailStreamStop(nodeID string) {
	bridge.mu.Lock()
	cancel := bridge.streams[nodeID]
	delete(bridge.streams, nodeID)
	bridge.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// ClearSubagentTree 清空子代理树（工作区「子代理」分区清空按钮）。
func (bridge *Bridge) ClearSubagentTree() error {
	return bridge.app.ClearSubagentTree()
}

// UpdateWorkItemStatus 更新工作表格任务状态（参数透传；业务校验在
// application 层，Bridge 不维护状态）。
func (bridge *Bridge) UpdateWorkItemStatus(id, status string) error {
	return bridge.app.UpdateWorkItemStatus(id, status)
}

func (bridge *Bridge) Submit(text string) error {
	return bridge.app.Submit(bridge.requestContext(), text)
}

func (bridge *Bridge) BeginNewSession() error {
	before := bridge.app.Snapshot().Session.ID
	if err := bridge.app.BeginNewSession(); err != nil {
		return err
	}
	bridge.settleCatalog()
	if bridge.app.Snapshot().Session.ID != before {
		bridge.resubscribe()
	}
	return nil
}

// SaveComposerDraft 把渲染层输入框的未发送正文交给草稿会话持久化
// （渲染层在输入时防抖调用；重启后随 seelex:ready 的 Snapshot
// session.composer 恢复）。
func (bridge *Bridge) SaveComposerDraft(text string) error {
	return bridge.app.SaveComposerDraft(text)
}

func (bridge *Bridge) ResumeSession(sessionID string) error {
	before := bridge.app.Snapshot().Session.ID
	if err := bridge.app.ResumeSession(sessionID); err != nil {
		return err
	}
	if bridge.app.Snapshot().Session.ID != before {
		bridge.resubscribe()
	}
	return nil
}

// ForkSessionLatest 从会话最新完整轮次分支出新会话并切换（Wails 前端会话
// 树「分支」按钮数据源；返回子会话 ID）。
func (bridge *Bridge) ForkSessionLatest(sessionID string) (string, error) {
	before := bridge.app.Snapshot().Session.ID
	childID, err := bridge.app.ForkSessionLatest(sessionID)
	if err != nil {
		return "", err
	}
	bridge.settleCatalog()
	if bridge.app.Snapshot().Session.ID != before {
		bridge.resubscribe()
	}
	return childID, nil
}

// CancelChat 取消当前视图会话的运行中回合。request_id 可能滞后一个事件 tick，
// 归属判断（停掉本会话当前回合）在 application 层，Bridge 只做转发。
func (bridge *Bridge) CancelChat(requestID string) bool {
	return bridge.app.CancelChat(requestID)
}

// SubmitToSession 向指定会话提交输入（会话级保护 API；应用不支持时返回
// 明确错误，不影响既有 Submit 路径）。
func (bridge *Bridge) SubmitToSession(sessionID, text string) error {
	app, ok := bridge.app.(sessionAwareApplication)
	if !ok {
		return errors.New("session-scoped API is not supported by the application")
	}
	return app.SubmitToSession(bridge.requestContext(), sessionID, text)
}

// ActivateSession 切换指定会话为当前会话（M1：切换即恢复，运行中拒绝）。
func (bridge *Bridge) ActivateSession(sessionID string) error {
	app, ok := bridge.app.(sessionAwareApplication)
	if !ok {
		return errors.New("session-scoped API is not supported by the application")
	}
	before := bridge.app.Snapshot().Session.ID
	if err := app.ActivateSession(sessionID); err != nil {
		return err
	}
	if bridge.app.Snapshot().Session.ID != before {
		bridge.resubscribe()
	}
	return nil
}

// SnapshotOf 返回指定会话的权威快照（M1：仅活跃会话有驻留快照）。
func (bridge *Bridge) SnapshotOf(sessionID string) (application.Snapshot, error) {
	app, ok := bridge.app.(sessionAwareApplication)
	if !ok {
		return application.Snapshot{}, errors.New("session-scoped API is not supported by the application")
	}
	return app.SnapshotOf(sessionID)
}

// SubscribeSession 返回按会话过滤的事件订阅（M1：chat 生命周期事件携带
// session_id 路由键）。
func (bridge *Bridge) SubscribeSession(sessionID string, buffer int) (application.Subscription, error) {
	app, ok := bridge.app.(sessionAwareApplication)
	if !ok {
		return application.Subscription{}, errors.New("session-scoped API is not supported by the application")
	}
	return app.SubscribeSession(sessionID, buffer)
}

func (bridge *Bridge) ResolveInteraction(id, optionID string) error {
	return bridge.app.ResolveInteraction(bridge.requestContext(), id, optionID)
}

func (bridge *Bridge) SelectAccount(name string) error {
	return bridge.app.SelectAccount(bridge.requestContext(), name)
}

func (bridge *Bridge) SwitchEffort(level string) error {
	return bridge.app.SwitchEffort(bridge.requestContext(), level)
}

func (bridge *Bridge) SwitchPlugin(name string) error {
	return bridge.app.SwitchPlugin(bridge.requestContext(), name)
}

func (bridge *Bridge) LoadMoreHistory(limit int) error {
	return bridge.app.LoadMoreHistory(limit)
}

func (bridge *Bridge) Suggestions(input string) []application.Suggestion {
	return bridge.app.Suggestions(input)
}

func (bridge *Bridge) DeleteSession(sessionID string) error {
	if err := bridge.app.DeleteSession(sessionID); err != nil {
		return err
	}
	bridge.settleCatalog()
	return nil
}

// SetSessionMeta 写会话展示元数据（置顶/别名/排序位）。参数保持扁平供 renderer
// 调用；写成功后由 application 唤醒目录刷新，并在返回前等一轮收敛，使前端
// 重拉的快照已按新排序与别名呈现。
func (bridge *Bridge) SetSessionMeta(sessionID string, pinned bool, alias string, sortOrder int) error {
	if err := bridge.app.SetSessionMeta(sessionID, application.SessionMeta{
		Pinned: pinned, Alias: alias, SortOrder: sortOrder,
	}); err != nil {
		return err
	}
	bridge.settleCatalog()
	return nil
}

func (bridge *Bridge) CreateWorkspace(name, rootPath, gitRemote string) error {
	return bridge.app.CreateWorkspace(name, rootPath, gitRemote)
}

func (bridge *Bridge) BindWorkspace(workspaceID string) error {
	return bridge.app.BindWorkspace(workspaceID)
}

func (bridge *Bridge) UnbindWorkspace() {
	bridge.app.UnbindWorkspace()
}

func (bridge *Bridge) SetFullAccess(on bool) {
	bridge.app.SetFullAccess(on)
}

func (bridge *Bridge) SessionStorageConfig() (sessionstore.Config, error) {
	return bridge.app.SessionStorageConfig()
}

func (bridge *Bridge) TestSessionStorage(config sessionstore.Config) error {
	return bridge.app.TestSessionStorage(bridge.requestContext(), config)
}

func (bridge *Bridge) ConfigureSessionStorage(config sessionstore.Config) error {
	return bridge.app.ConfigureSessionStorage(bridge.requestContext(), config)
}

// ScheduleTask 创建并启动一个定时/周期任务（后端调度器校验白名单/周期或 RunAt）。
func (bridge *Bridge) ScheduleTask(spec seelebridge.ScheduledTaskSpec) (*seelebridge.ScheduledTaskStatus, error) {
	return bridge.app.ScheduleTask(bridge.requestContext(), spec)
}

// CancelScheduledTask 取消并移除定时/周期任务。
func (bridge *Bridge) CancelScheduledTask(id string) error {
	return bridge.app.CancelScheduledTask(id)
}

// SearchHistory 检索会话历史聊天记录（压缩栈索引 → 真实记录；
// Wails 前端历史检索面板数据源，返回权威 seelexctx/search.Result）。
func (bridge *Bridge) SearchHistory(query string, limit int) (seelexctxsearch.Result, error) {
	return bridge.app.SearchHistory(bridge.requestContext(), query, limit)
}

// WorkspaceTree 转发工作树目录列表（参数与业务校验在 application 层）。
func (bridge *Bridge) WorkspaceTree(relPath string, depth int) (dto.TreeListing, error) {
	return bridge.app.WorkspaceTree(relPath, depth)
}

// WorkspaceFileCount 转发工作区文件统计。
func (bridge *Bridge) WorkspaceFileCount() (dto.TreeCount, error) {
	return bridge.app.WorkspaceFileCount()
}

// WorkspaceGitLog 转发工作区 git 提交记录树（最近 limit 条提交的 --graph
// 拓扑行；只读元数据，不含 diff/文件内容；非 git 仓库返回 Result.Error）。
func (bridge *Bridge) WorkspaceGitLog(limit int) (dto.GitLogResult, error) {
	return bridge.app.WorkspaceGitLog(limit)
}

// ToolResultContent 按 result_ref 分页读回完整工具输出（快照被截断的
// 工具输出，"加载完整输出"数据源；参数透传，业务校验在 application 层）。
func (bridge *Bridge) ToolResultContent(resultRef string, offset, limit int) (application.ToolResultPage, error) {
	return bridge.app.ToolResultContent(bridge.requestContext(), resultRef, offset, limit)
}

// PerfStats 返回性能追踪钩子的后端数据面（无内容指标：快照载荷体积/
// 会话规模/归档体积，供前端渲染进程对照）。
func (bridge *Bridge) PerfStats() application.PerfStats {
	return bridge.app.PerfStats()
}

// PromptLayers 返回当前会话注入的 prompt 前缀层（轨迹视图数据源）。
func (bridge *Bridge) PromptLayers() []application.PromptLayer {
	if bridge == nil || bridge.app == nil {
		return nil
	}
	return bridge.app.PromptLayers()
}
