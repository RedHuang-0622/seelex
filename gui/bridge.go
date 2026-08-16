package gui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

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
	CancelChat(string) bool
	ResolveInteraction(context.Context, string, string) error
	SelectAccount(context.Context, string) error
	SwitchEffort(context.Context, string) error
	SwitchPlugin(context.Context, string) error
	LoadMoreHistory(int) error
	Suggestions(string) []application.Suggestion
	DeleteSession(string) error
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
	bridge.sub = bridge.app.Subscribe(256)
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
	return bridge.app.BeginNewSession()
}

func (bridge *Bridge) ResumeSession(sessionID string) error {
	return bridge.app.ResumeSession(sessionID)
}

func (bridge *Bridge) CancelChat(requestID string) bool {
	if bridge.app.CancelChat(requestID) {
		return true
	}
	// A renderer can hold the previous request ID for one event tick while a
	// queued turn is promoted. Retry against the application's current active
	// request instead of silently turning the stop button into a no-op.
	if strings.TrimSpace(requestID) != "" {
		return bridge.app.CancelChat("")
	}
	return false
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
	return bridge.app.DeleteSession(sessionID)
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
