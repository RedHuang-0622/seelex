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
	app     Application
	info    AppInfo
	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	sub     application.Subscription
	wg      sync.WaitGroup
	running bool
	emitFn  EventEmitter
	streams map[string]func()
}

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
	bridge.sub = bridge.subscribeView()
	bridge.emitFn = emit
	bridge.streams = make(map[string]func())
	bridge.running = true
	loopContext := bridge.ctx
	subscription := bridge.sub
	bridge.wg.Add(1)
	bridge.mu.Unlock()

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
			}
		}
	}()
}

// subscribeView 订阅当前视图会话的事件流。会话归属由 application 在投递端
// 判定（sessionID 为空 = 跟随视图指针，草稿物化与切换都由它覆盖），Bridge
// 不再保存"当前会话"副本，渲染层也收不到别会话的事件。
// 宿主不支持会话级订阅时退回全局订阅：此时应用本身也没有多会话状态可污染。
func (bridge *Bridge) subscribeView() application.Subscription {
	if app, ok := bridge.app.(sessionAwareApplication); ok {
		if subscription, err := app.SubscribeSession("", 256); err == nil {
			return subscription
		}
	}
	return bridge.app.Subscribe(256)
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
	if err := bridge.app.BeginNewSession(); err != nil {
		return err
	}
	bridge.settleCatalog()
	return nil
}

func (bridge *Bridge) ResumeSession(sessionID string) error {
	return bridge.app.ResumeSession(sessionID)
}

// ForkSessionLatest 从会话最新完整轮次分支出新会话并切换（Wails 前端会话
// 树「分支」按钮数据源；返回子会话 ID）。
func (bridge *Bridge) ForkSessionLatest(sessionID string) (string, error) {
	childID, err := bridge.app.ForkSessionLatest(sessionID)
	if err != nil {
		return "", err
	}
	bridge.settleCatalog()
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
	return app.ActivateSession(sessionID)
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
