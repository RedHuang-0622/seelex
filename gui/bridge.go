package gui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/gui/terminal"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
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
	// SetReasoningEffort / SetLoopLimit 是 composer 底部两条**独立**拉条的落点
	// （思考强度 / 循环次数各自独立；档位切换 = 同时设置两条拉条的预设）。
	SetReasoningEffort(context.Context, string) error
	SetLoopLimit(context.Context, int) error
	// SetSessionReleaseWindow / SessionReleaseWindow 是「会话驻留窗口」的可调面
	// （设置弹窗 → 会话驻留）：写路径返回**真正生效**的值（前端采纳它，而不是假定
	// 请求值生效），读路径供弹窗打开时读一次。
	SetSessionReleaseWindow(context.Context, int) (int, error)
	SessionReleaseWindow() int
	// SetAccountWindow 是「每账号上下文窗口」的可调面（设置 → 账号的挡位/自定义值）：
	// 先落盘（宿主自己的覆盖层）再改运行时，返回**真正生效**的窗口；window <= 0 =
	// 清掉该账号的覆盖（回落 config/seelex.yaml 的 limits.context_window）。
	SetAccountWindow(context.Context, string, int) (int, error)
	// AddAccount 是角色卡下「添加账户」的写面：写账号档 + 热注册，
	// 返回生效的账号名。校验/落盘失败时什么都没变；已落盘但热注册失败时错误里会写明
	// "重启后生效"。
	AddAccount(context.Context, application.AccountDraft) (application.AccountAddResult, error)
	AccountDetails(string) (application.AccountDetails, error)
	UpdateAccount(application.AccountUpdate) (application.AccountDetails, error)
	DeleteAccount(string) (application.AccountDetails, error)
	// AccountBalance 是「查一个账号此刻的余额」的读面（角色卡账户行）。
	// 余额是快照：点一次查一次，不进快照缓存。
	AccountBalance(context.Context, string) (application.AccountBalance, error)
	SwitchPlugin(context.Context, string) error
	LoadMoreHistory(int) error
	// LoadLatestHistory 把可见会话拉回最新一页（历史浏览后的「回到最新」；
	// 分页只移动窗口，数据不动）。
	LoadLatestHistory() error
	Suggestions(string) []application.Suggestion
	DeleteSession(string) error
	// ArchiveSession 把指定会话置为 archived（record 级状态；运行中/待批
	// 拒绝）。归档会话从常规目录隐藏，数据五片保留可按 ID 重开。
	ArchiveSession(string) error
	// SetSessionMeta 写会话展示元数据（置顶/别名/排序位），随会话目录下发。
	SetSessionMeta(string, application.SessionMeta) error
	// WaitCatalogRefresh 等待会话目录 worker 完成一轮覆盖本次变更的刷新，使随后
	// 的 Snapshot() 直接携带权威目录（前端因此不必回填上一次列表伪造状态）。
	WaitCatalogRefresh(context.Context) error
	CreateWorkspace(name, rootPath, gitRemote string) error
	CreateWorkspaces(requests []dto.WorkspaceCreateRequest) (dto.WorkspaceCreateBatchResult, error)
	BindWorkspace(workspaceID string) error
	UnbindWorkspace()
	SetFullAccess(bool) bool
	// SetPermissionTier 切换视图会话的权限档位（manual/edit/auto/full）并返回
	// 真正生效的档位；未识别的档位 id 报错。会话级归属（G4）。
	SetPermissionTier(string) (string, error)
	SubagentSessionDetail(nodeID string) (*application.SubagentDetail, error)
	ClearSubagentTree() error
	// UpdateWorkItemStatus 更新工作表格任务状态（v1：仅 todo 的
	// pending/doing/done；plan/subagent 由执行器管理）。
	UpdateWorkItemStatus(id, status string) error
	ScheduleTask(context.Context, dto.ScheduledTaskSpec) (*dto.ScheduledTaskStatus, error)
	// UpdateScheduledTask 用一份新定义覆盖既有任务（编辑；ID 是操作键）。
	UpdateScheduledTask(context.Context, string, dto.ScheduledTaskSpec) (*dto.ScheduledTaskStatus, error)
	CancelScheduledTask(string) error
	// SearchHistory 检索会话历史聊天记录（压缩栈索引 → 真实记录；
	// GUI 历史检索面板数据源）。
	SearchHistory(context.Context, string, int) (application.SearchResult, error)
	// WorkspaceTree 列出当前工作区某目录的子条目（工作树数据源；只含
	// 元数据，不含文件内容）。
	WorkspaceTree(relPath string, depth int) (dto.TreeListing, error)
	// WorkspaceFileCount 统计当前工作区文件/目录数（工作树文件数 badge）。
	WorkspaceFileCount() (dto.TreeCount, error)
	// WorkspaceGitRepos 发现当前工作区内的 git 仓库（多仓库面板的仓库标签
	// 数据源；只读扫描，有界——深度/目录/条目预算触顶时 Truncated 如实上报）。
	// 未绑定工作区或后端不支持时返回带 Error 文案的空结果，不是 Go error。
	WorkspaceGitRepos() (dto.GitReposResult, error)
	// WorkspaceGitLog 返回**指定仓库**最近 limit 条提交（repo 相对工作区根，
	// "." = 工作区根；含父提交拓扑；只读元数据，不含 diff/文件内容；非 git
	// 仓库返回 Result.Error）。repo 越界/绝对路径是调用方传错参数 → Go error。
	WorkspaceGitLog(repo string, limit int) (dto.GitLogResult, error)
	// WorkspaceChanges 返回**指定仓库**未提交改动（repo 口径同 WorkspaceGitLog；
	// 只读元数据——路径与状态字符，不含 diff/文件内容）。
	WorkspaceChanges(repo string, limit int) (dto.WorkspaceChangesResult, error)
	// WorkspaceGitCommitDetail 返回**指定仓库**某个提交改了哪些文件（提交记录
	// 「点开某一条」数据源；只读元数据——状态/路径/重命名原路径/±行数，不含
	// diff 或文件内容；非 git 仓库或提交不存在返回 Result.Error）。hash 由前端
	// 从提交列表原样带回，形状校验在后端。
	WorkspaceGitCommitDetail(repo, hash string, limit int) (dto.GitCommitDetail, error)
	// WorkspaceFileContent 读取当前工作区某文件的前 limit 字节（文件预览
	// 数据源；root 只来自后端当前 workspace，containment/敏感过滤在
	// workspace 层保证；只读受控字节，不进快照）。
	WorkspaceFileContent(relPath string, limit int64) (dto.FileContent, error)
	// WorkspaceGitCommitFileContent 读取某文件在**指定仓库**某个提交时的内容
	// （提交记录「点开某个文件」数据源）。与 WorkspaceFileContent 同一条可见性
	// 边界，只是字节来自 git 对象库；这条通道只有读，不回写工作区。
	WorkspaceGitCommitFileContent(repo, hash, relPath string, limit int64) (dto.FileContent, error)
	// WorkspaceWriteFile 覆盖当前工作区某文件的全部内容（文件详情面板的
	// 编辑保存；可见性边界与原子发布在 workspace 层保证）。用户即动作主体，
	// 不走主代理权限档位与审批；未装配写端口的宿主显式报错。
	WorkspaceWriteFile(relPath, content string) (dto.FileWriteResult, error)
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
	SnapshotOf(string) (application.SessionSnapshot, error)
	// ListSessions 返回权威会话目录（headless/控制台枚举；与快照目录同源，
	// 不要求会话驻留）。
	ListSessions() []application.SessionInfo
	// GetSessionTranscript 按 Seq 区间读取会话事件日志（冷读可用，不要求
	// 目标会话驻留；(0,0) = 全量）。
	GetSessionTranscript(string, uint64, uint64) ([]application.TranscriptEvent, error)
	SubscribeSession(string, int) (application.Subscription, error)
}

// replayAwareApplication 是 sessionAwareApplication 的再一层可选扩展：宿主能
// 给出带重放窗口的订阅（缓冲写满不丢事件，可按 delivery_seq 增量补取），
// Bridge 才会启用回执重推（C4）。未实现时退回无窗口订阅，溢出仍由 hub 的
// resync.required 兜底。
type replayAwareApplication interface {
	SubscribeSessionWithReplay(sessionID string, buffer, replayWindow int) (application.Subscription, error)
}

// messageEditForkApplication 是「编辑用户消息并自动 fork」的原子能力面。
// 它刻意是可选窄接口：未装配的宿主明确报错，不能由 Bridge 拆成
// ForkSession + Submit 两条请求。
type messageEditForkApplication interface {
	EditUserMessageAndFork(context.Context, string, model.EditUserMessageForkRequest) (model.EditUserMessageForkResult, error)
}

// queueAwareApplication 是 Application 的可选排队输入编辑面：调换排队顺序 +
// 撤回到输入框。只作用于「排队中」的输入（回合运行期间的队列），不触碰正在
// 运行的回合；未装配时 Bridge 方法返回可展示错误，不静默退化。
type queueAwareApplication interface {
	// ReorderQueuedInput 把目标会话排队输入中 from 位置的条目移动到 to 位置
	//（空 sessionID = 当前视图会话）。
	ReorderQueuedInput(sessionID string, from, to int) error
	// RecallQueuedInput 撤回目标会话的一条排队输入并返回其展示原文（引擎侧
	// 载荷随条目丢弃）。
	RecallQueuedInput(sessionID string, index int) (string, error)
}

// roleSettingsApplication 是 Application 的**可选**角色设置写面（设置 → 角色设置）。
//
// 为什么窄可选而不是并进 Application：面里只有真机应用层有这两条（会话级角色权限
// 登记），还没装配的宿主（GUI 的假应用、headless 假面）不该被逼着写空实现——缺了就
// 在桥面显式报"未装配"，不静默退化成"空权限"（与 queueAwareApplication 同一条口径）。
type roleSettingsApplication interface {
	// RolePermissionVector 读某角色在某会话的生效权限向量（登记优先，缺失按五级链路）。
	RolePermissionVector(sessionID, role string) ([]string, error)
	// SetRolePermissionVector 写入某角色的权限向量（空集 = 显式不给任何能力）。
	SetRolePermissionVector(sessionID, role string, vector []string) error
	// RoleDefaultVector / SetRoleDefaultVector 是**角色级默认**（跨会话）：权限面板
	// 给每个角色定的那一份默认向量，落在角色默认层，会话登记仍压在它之上。
	RoleDefaultVector(sessionID, role string) ([]string, bool, error)
	SetRoleDefaultVector(sessionID, role string, vector []string) error
	// 角色默认账号（项目作用域）。空 account = 未设置，由账号池按角色选择。
	RoleAccount(sessionID, role string) (string, bool, error)
	SetRoleAccount(sessionID, role, account string) error
	RoleInheritMainAgent(sessionID, role string) (bool, error)
	SetRoleInheritMainAgent(sessionID, role string, inherit bool) error
	RoleSSHUserSpaces(sessionID, role string) ([]string, error)
	RoleDefaultSSHUserSpaces(sessionID, role string) ([]string, error)
	SetRoleSSHUserSpaces(sessionID, role string, paths []string) error
	SetRoleSessionSSHUserSpaces(sessionID, role string, paths []string) error
}

// roleSessionApplication 是 Application 的**角色会话只读观察面**（团队看板在用：
// 成员行「查看会话」与「员工会话」入口）。
//
// 语义边界（2026-10-12 J-1a）：Agent Team 的**装配面**（成员表 / 角色 CRUD / 工作
// 顺序 / 团队库 / 员工库 / 全局母本 / 员工提示词）已整条退场，因此本接口只留两条
// **读者**：
//
//   - RoleSnapshot：某个角色会话的只读观察面（`dto.RoleSnapshot`，角色 draft 的
//     投影；B1 保留面，全仓唯一实现在 application/core/role_session.go）；
//   - TeammateSessionLiveFor / TeammateSessionLivePageFor：当前 teammate 会话的实时
//     投影（团队看板的「这件事的会话此刻在说什么」）。
//
// 为什么它们留下：这两条读的是**角色会话**这件事实，与"会话里装配了哪支团队"无关；
// 团队看板（teamwork）是保留能力面。接口名不再叫 agentTeam*：名字里的 Agent Team
// 面已经不在了，继续用它只会让下一个人以为成员表还能从这里读到。
type roleSessionApplication interface {
	RoleSnapshot(mainSessionID, roleName, roleSessionID string) (dto.RoleSnapshot, error)
	// TeammateSessionLiveFor 是**当前 teammate 会话**的实时读面（这件事自己的会话——
	// 进程内执行面，正文不在会话库里）。
	TeammateSessionLiveFor(sessionID string) dto.TeammateSessionLiveView
	// TeammateSessionLivePageFor 是同一份读面的**分页读法**（有界窗口 + 分页）。
	TeammateSessionLivePageFor(sessionID string, offset, limit int) dto.TeammateSessionLiveView
	// WorkItemExecutionFor is the single WorkItem detail/progress read.
	WorkItemExecutionFor(ctx context.Context, mainSessionID, itemID string, page dto.ConversationViewRequest) (dto.WorkItemExecutionView, error)
	// TeammateInspectionFor is the safe teammate detail projection.
	TeammateInspectionFor(ctx context.Context, mainSessionID, role string) (dto.TeammateInspectionView, error)
	// ConversationView is the unified main/subagent/teammate history capability.
	ConversationView(ctx context.Context, ref dto.ConversationSessionRef, request dto.ConversationViewRequest) (dto.SessionRecordPage, error)
	ConversationChat(ctx context.Context, ref dto.ConversationSessionRef, request dto.ConversationChatRequest) (dto.ChatReceipt, error)
	RecoverConversation(ctx context.Context, ref dto.ConversationSessionRef) (dto.SessionRecovery, error)
}

// 编译期断言：生产 Application（application.Service = *core.Service）必须满足本
// 接口。此前这里的方法名漂移成 AgentTeamMaterializePreset（Service 侧叫
// MaterializeAgentTeamPreset），只有测试假实现满足它——于是 bridge 单测全绿，
// 而真机 GUI 一调 Agent Team 就断言失败、报"当前 Application 未装配 A2A 角色
// 管理面"（2026-09-11 修复）。用假实现顶替接口断言是这类漂移的温床，故在此
// 用真实类型钉死。
// 2026-10-12（J-1a）：Agent Team 装配面退场后，本断言的对象只剩"角色会话只读观察
// 面"——它仍然钉死真机（读面漂移同样会退化成"未装配"），只是钉的东西变窄了。
var (
	// 基础面 + 三个可选扩展面：生产 Application 必须全部满足，否则真机会在
	// 类型断言处退化成"未装配"（agentTeamApplication 就是这么漂移过一次）。
	_ Application                = (*application.Service)(nil)
	_ sessionAwareApplication    = (*application.Service)(nil)
	_ replayAwareApplication     = (*application.Service)(nil)
	_ roleSessionApplication     = (*application.Service)(nil)
	_ queueAwareApplication      = (*application.Service)(nil)
	_ roleSettingsApplication    = (*application.Service)(nil)
	_ messageEditForkApplication = (*application.Service)(nil)
)

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
	// StartupWarning 是启动期配置问题的可见文本。bootstrap 同时把它写入会话
	// 通知区；GUI 不再用原生模态框阻塞窗口打开。
	StartupWarning string
	// AppearancePath 是 GUI 外观设置（当前皮肤 + 由它派生的窗口材质）的落盘路径，
	// 由宿主按配置责任链给出（见 wiring/config 的 GUIAppearancePath）。建窗期读它决定窗口
	// 是否走透明/亚克力；换肤期由 Bridge.SetWindowAppearance 写它。
	// 空串 = 不落盘：窗口保持不透明，换肤照常。
	AppearancePath string
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
	app  Application
	info AppInfo
	mu   sync.Mutex
	// switchMu 串行化“视图会话切换类”命令（BeginNewSession/ResumeSession/
	// ActivateSession/ForkSessionLatest/EditUserMessageAndFork）：忙时会话产生高频事件时，并发点击
	// 多个会话会导致多次 resubscribe 交错；串行后一次只处理一次切换。
	switchMu sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	sub      application.Subscription
	// subscribedSessionID 是当前订阅的事件会话键（mu 保护）：草稿早分配
	// SID 后，视图会话在 Submit 物化（或 legacy 引擎回退 StartSession 另发
	// ID）时可能变化；relay 发现事件 sid 与订阅键不一致即重订阅。
	subscribedSessionID string
	wg                  sync.WaitGroup
	running             bool
	emitFn              EventEmitter
	// 事件投递回执（C4，均由 mu 保护）：Go→WebView 这条腿没有任何投递反馈
	// （EventEmitter 无返回值），事件"发过了"不等于"渲染层应用了"。ackedSeq 是
	// 渲染层回执的应用水位；落后于订阅水位时 Bridge 从重放窗口增量重推，
	// resendTimer/resendTries 是那次重推的节流与止损。
	ackedSeq    uint64
	resendTimer *time.Timer
	resendTries int
	// relayGen 是 relay 代际（mu 保护）：每次（重）订阅自增。relay 在投递每条
	// 事件前校验自己仍是当前代际，被更新的 resubscribe 取代后立即退出——
	// 旧订阅的余量事件不得在新订阅的权威基线之后再投递到渲染层（跨会话
	// 迟到事件会污染新订阅从 1 重计的 delivery_seq 水位；渲染层已幂等容忍，
	// 此处把污染源收窄到最小）。
	relayGen uint64
	// terminals 是下栏终端的会话管理器（termMu 保护，惰性创建）。它刻意**不**
	// 属于会话状态：终端是用户本地 shell，与 Snapshot/revision/订阅换代无关。
	terminals *terminal.Manager
	termMu    sync.Mutex
	// appearancePath 是 GUI 外观设置的落盘路径（构造时固定，之后只读）：
	// SetWindowAppearance 写它，run_wails 启动期读它。空串 = 不落盘。
	appearancePath string
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

func NewBridge(app Application, options Options) (*Bridge, error) {
	if app == nil {
		return nil, errors.New("gui: application is required")
	}
	title := strings.TrimSpace(options.Title)
	if title == "" {
		title = "Seelex"
	}
	return &Bridge{app: app, info: AppInfo{Title: title, Version: options.Version, Project: discoverProject(options.ProjectRoot)}, appearancePath: strings.TrimSpace(options.AppearancePath)}, nil
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
	bridge.running = true
	loopContext := bridge.ctx
	subscription := bridge.subscribeViewLocked()
	bridge.sub = subscription
	bridge.mu.Unlock()
	bridge.startRelay(loopContext, emit, subscription, bridge.nextRelayGeneration())
}

// nextRelayGeneration 分配一个新的 relay 代际序号（调用方不需要持有 mu）。
func (bridge *Bridge) nextRelayGeneration() uint64 {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	bridge.relayGen++
	return bridge.relayGen
}

// relayIsCurrent 报告指定代际是否仍是当前 relay（未被更新的 resubscribe
// 取代）。投递事件前校验，代际过期即退出，旧订阅余量事件不再外投。
func (bridge *Bridge) relayIsCurrent(gen uint64) bool {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	return bridge.running && bridge.relayGen == gen
}

// startRelay 启动一个订阅的转发 goroutine（relay 生命周期随 bridge.ctx
// 取消或 resubscribe 换代；gen 是本次订阅的代际，被取代后 relay 不得再投递）。
func (bridge *Bridge) startRelay(loopContext context.Context, emit EventEmitter, subscription application.Subscription, gen uint64) {
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
				// 已被更新的 resubscribe 取代：本 relay 停在这里，已取出的
				// 旧订阅事件不再投递（避免其 delivery_seq 污染新订阅水位）。
				if !bridge.relayIsCurrent(gen) {
					return
				}
				// 应用在后台异步改回了权威视图会话（运行中冷恢复失败回退）：
				// 本订阅仍钉在失败目标会话上，其会话级事件流之后将永远沉默，
				// 渲染层停在 restoring 空壳（输入区禁用）。该进程级事件不是给
				// 渲染层的内容，而是订阅对齐信号——重建后新订阅的 seelex:ready
				// 会把权威基线重投给渲染层。
				if event.Kind == application.EventViewSessionChanged {
					bridge.syncSubscriptionToView()
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
	// 换代：旧 relay 在下次取事件时发现代际过期即退出，不投递余量事件。
	bridge.startRelay(loopContext, emit, subscription, bridge.nextRelayGeneration())
}

// syncSubscriptionToView 把事件订阅键对齐到应用的权威视图会话。应用可能在
// Bridge 不知情时**异步**改回视图会话（运行中冷恢复失败：hot attach 回切换
// 前会话 / 退化为新建草稿）：此时订阅键仍钉在失败目标会话上，其会话级订阅
// 永远沉默，渲染层停在 restoring 空壳（输入区禁用、消息发不出去）。调用方
// 为 relay（收到 view.session.changed 进程级事件）与视图命令成功路径；订阅
// 已对齐时不做任何事（幂等，无锁空转成本以外无副作用）。
func (bridge *Bridge) syncSubscriptionToView() {
	bridge.mu.Lock()
	if !bridge.running {
		bridge.mu.Unlock()
		return
	}
	subscribed := bridge.subscribedSessionID
	bridge.mu.Unlock()
	if subscribed == bridge.app.Snapshot().Session.ID {
		return
	}
	bridge.resubscribe()
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
	bridge.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	subscription.Close()
	bridge.wg.Wait()
	// 终端随宿主退出一起结束：先杀子进程再关伪控制台，否则退出路径会留下
	// 悬挂的 shell（Windows 上还会拖住 conhost）。
	bridge.closeTerminals()
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

// SubagentSessionDetail 返回子代理节点详情（会话记录 + 上下文快照 +
// 功能打点；条目详情弹窗唯一的数据面）。
func (bridge *Bridge) SubagentSessionDetail(nodeID string) (*application.SubagentDetail, error) {
	return bridge.app.SubagentSessionDetail(nodeID)
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
	// 提交前先把订阅对齐到权威视图会话：异步冷恢复失败回退后，Bridge 的
	// 订阅键可能仍钉在失败目标上（会话级事件流已沉默），若用户此刻成功发
	// 出消息，运行结果将永远不回显。
	bridge.syncSubscriptionToView()
	return bridge.app.Submit(bridge.requestContext(), text)
}

func (bridge *Bridge) BeginNewSession() error {
	bridge.switchMu.Lock()
	defer bridge.switchMu.Unlock()
	if err := bridge.app.BeginNewSession(); err != nil {
		return err
	}
	bridge.settleCatalog()
	// 对齐而非"前后会话不同才重订阅"：应用可能已在此前异步回退过视图
	// （冷恢复失败），订阅键与权威视图会话一致才是硬不变量。
	bridge.syncSubscriptionToView()
	return nil
}

func (bridge *Bridge) ResumeSession(sessionID string) error {
	bridge.switchMu.Lock()
	defer bridge.switchMu.Unlock()
	if err := bridge.app.ResumeSession(sessionID); err != nil {
		return err
	}
	bridge.syncSubscriptionToView()
	return nil
}

// ForkSessionLatest 从会话最新完整轮次分支出新会话并切换（Wails 前端会话
// 树「分支」按钮数据源；返回子会话 ID）。
func (bridge *Bridge) ForkSessionLatest(sessionID string) (string, error) {
	bridge.switchMu.Lock()
	defer bridge.switchMu.Unlock()
	childID, err := bridge.app.ForkSessionLatest(sessionID)
	if err != nil {
		return "", err
	}
	bridge.settleCatalog()
	bridge.syncSubscriptionToView()
	return childID, nil
}

// EditUserMessageAndFork 是最小 GUI 写面：一次 RPC 内完成编辑源消息的分叉与
// 修改后输入提交。请求里的 operation_id 由前端稳定生成，后端负责双击幂等。
func (bridge *Bridge) EditUserMessageAndFork(parentSessionID string, request model.EditUserMessageForkRequest) (model.EditUserMessageForkResult, error) {
	app, ok := bridge.app.(messageEditForkApplication)
	if !ok {
		return model.EditUserMessageForkResult{}, errors.New("当前 Application 未装配消息编辑分叉面")
	}
	bridge.switchMu.Lock()
	defer bridge.switchMu.Unlock()
	result, err := app.EditUserMessageAndFork(bridge.requestContext(), parentSessionID, request)
	if err != nil {
		return model.EditUserMessageForkResult{}, err
	}
	bridge.settleCatalog()
	bridge.syncSubscriptionToView()
	return result, nil
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

// ReorderQueuedInput 调换目标会话排队输入的顺序（队列条目的 ↑/↓ 操作）。
// 只作用于排队中的输入；越界/非运行态/会话不存在由 application 层给出明确
// 错误，Bridge 只做透传（空 sessionID = 当前视图会话）。
func (bridge *Bridge) ReorderQueuedInput(sessionID string, from, to int) error {
	app, ok := bridge.app.(queueAwareApplication)
	if !ok {
		return errors.New("queued input editing is not supported by the application")
	}
	return app.ReorderQueuedInput(sessionID, from, to)
}

// RecallQueuedInput 撤回目标会话的一条排队输入，返回其展示原文（渲染层把
// 原文写回 composer 重新编辑）。只作用于排队中的输入；越界/非运行态/会话
// 不存在由 application 层给出明确错误，Bridge 只做透传。
func (bridge *Bridge) RecallQueuedInput(sessionID string, index int) (string, error) {
	app, ok := bridge.app.(queueAwareApplication)
	if !ok {
		return "", errors.New("queued input editing is not supported by the application")
	}
	return app.RecallQueuedInput(sessionID, index)
}

// ActivateSession 切换指定会话为当前会话（M1：切换即恢复，运行中拒绝）。
func (bridge *Bridge) ActivateSession(sessionID string) error {
	bridge.switchMu.Lock()
	defer bridge.switchMu.Unlock()
	app, ok := bridge.app.(sessionAwareApplication)
	if !ok {
		return errors.New("session-scoped API is not supported by the application")
	}
	if err := app.ActivateSession(sessionID); err != nil {
		return err
	}
	bridge.syncSubscriptionToView()
	return nil
}

// SnapshotOf 返回指定会话的权威会话快照（G3：SessionSnapshot，进程级目录
// 与能力清单不在其中；经 capabilities.session_snapshot 声明）。
func (bridge *Bridge) SnapshotOf(sessionID string) (application.SessionSnapshot, error) {
	app, ok := bridge.app.(sessionAwareApplication)
	if !ok {
		return application.SessionSnapshot{}, errors.New("session-scoped API is not supported by the application")
	}
	return app.SnapshotOf(sessionID)
}

// ListSessions 返回权威会话目录（C1 冷读宿主面；与会话树同一数据源）。
func (bridge *Bridge) ListSessions() []application.SessionInfo {
	app, ok := bridge.app.(sessionAwareApplication)
	if !ok {
		return nil
	}
	return app.ListSessions()
}

// sessionInputIndexApplication 是 Application 的「全量用户输入索引」可选扩展面
// （对话区右侧索引数据源）。未装配时 Bridge 方法返回可展示错误，不静默退化
// 成「空索引」（否则右侧轨道看起来能点、其实什么都没索引到）。
type sessionInputIndexApplication interface {
	// SessionInputIndex 返回目标会话的**全量**用户输入索引（seq/round 序号、
	// 有界摘要、是否在当前已加载窗口）；未驻留会话同样可用。
	SessionInputIndex(sessionID string) (model.SessionInputIndex, error)
}

// 编译期断言：生产 Application（application.Service = *core.Service）必须满足
// 本可选面；用假实现顶替会让真机在类型断言处退化成"未装配"。
var _ sessionInputIndexApplication = (*application.Service)(nil)

// SessionInputIndex 返回指定会话的全量用户输入索引（对话区右侧索引数据源：
// 只索引用户输入，覆盖整会话含尚未加载到前端的早期轮次；只带摘要不带正文）。
// Bridge 只做透传，索引语义由 application 层给出。
func (bridge *Bridge) SessionInputIndex(sessionID string) (model.SessionInputIndex, error) {
	app, ok := bridge.app.(sessionInputIndexApplication)
	if !ok {
		return model.SessionInputIndex{}, errors.New("session input index is not supported by the application")
	}
	return app.SessionInputIndex(sessionID)
}

// GetSessionTranscript 按 Seq 区间读取会话事件日志（C1；未驻留会话同样
// 可用，(0,0) = 全量）。
func (bridge *Bridge) GetSessionTranscript(sessionID string, fromSeq, toSeq uint64) ([]application.TranscriptEvent, error) {
	app, ok := bridge.app.(sessionAwareApplication)
	if !ok {
		return nil, errors.New("session-scoped API is not supported by the application")
	}
	return app.GetSessionTranscript(sessionID, fromSeq, toSeq)
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

// SetReasoningEffort 下发"想多深"（composer 底部「推理强度」拉条）。
//
// 与 SwitchEffort 的分工：SwitchEffort 一次改三项（prompt 层 / plan 策略 / ReAct
// 预算，界面上已没有它的滑杆，只经 `/effort` 命令），这两个入口各改一项。两者共用
// 同一条运行守卫（视图会话 running 时拒绝）。
func (bridge *Bridge) SetReasoningEffort(effort string) error {
	return bridge.app.SetReasoningEffort(bridge.requestContext(), effort)
}

// SetLoopLimit 下发"跑多少轮"（composer 底部「循环次数」拉条）。
func (bridge *Bridge) SetLoopLimit(limit int) error {
	return bridge.app.SetLoopLimit(bridge.requestContext(), limit)
}

// SetSessionReleaseWindow 改「受保护集之外最多保留几个已加载会话」（设置弹窗 →
// 会话驻留），返回**真正生效**的窗口。
//
// 窗口是**现读**的进程级 limits（释放面每次决策读一次），所以这一步之后即刻生效：
// 不重启、不重建任何 LRU 结构。落盘由后端自己的 ReleaseWindowStore 负责（宿主没
// 装配时只本次运行有效）；写盘失败时后端整步不改内存，返回值因此只是错误，前端把
// 控件回滚到最后一份权威值是诚实的。
func (bridge *Bridge) SetSessionReleaseWindow(window int) (int, error) {
	return bridge.app.SetSessionReleaseWindow(bridge.requestContext(), window)
}

// SessionReleaseWindow 读当前生效的会话驻留窗口（弹窗打开时读一次；与
// SetSessionReleaseWindow 同一份权威，前端不另存一份"我以为的值"）。
func (bridge *Bridge) SessionReleaseWindow() int {
	return bridge.app.SessionReleaseWindow()
}

// SetAccountWindow 改一个账号的上下文窗口（设置 → 账号）。window <= 0 = 清掉该账号
// 的覆盖（回落到 config/seelex.yaml 的 limits.context_window）。
//
// 返回值是**真正生效**的窗口：前端采纳它，不假定请求值生效（与 SetSessionReleaseWindow
// 同一条口径——后端可能归一化/拒绝，界面显示的必须是生效的那个）。
func (bridge *Bridge) SetAccountWindow(name string, window int) (int, error) {
	return bridge.app.SetAccountWindow(bridge.requestContext(), name, window)
}

// AccountWindowPresets 返回界面挡位（token，升序）——**后端下发**，前端不自己写死
// 一份（与权限档位、effort 档位同一条口径：档位表只有一处，改它不用改两处）。
func (bridge *Bridge) AccountWindowPresets() []int {
	return application.AccountWindowPresets()
}

// AccountRoles 返回角色卡下账户新增/编辑的角色词表。
//
// 与 AccountWindowPresets 同一姿势：**后端下发**，前端不写死一份。
func (bridge *Bridge) AccountRoles() []string {
	return application.AccountRoles()
}

// AddAccount 在角色卡下新增账号：写账号档 + 热注册进运行时。
//
// 返回**生效的账号名**（`agent-2` 这种由账号档位置推导的名字）——界面必须采纳它，
// 因为下一步"选中它/改窗口/查余额"都按名字打；自己猜下一个序号会在并发添加或
// 账号档被人手改过时错位。
//
// 错误语义：校验或落盘失败 ⇒ 什么都没变；已落盘但热注册失败 ⇒ 错误里会写明
// "重启后生效"，此时账号档确实改了（界面不要报成"添加失败"让用户重复加）。
func (bridge *Bridge) AddAccount(draft application.AccountDraft) (application.AccountAddResult, error) {
	return bridge.app.AddAccount(bridge.requestContext(), draft)
}

// AccountDetails 读取编辑账户窗口需要的非秘密字段（密钥永不回传）。
func (bridge *Bridge) AccountDetails(name string) (application.AccountDetails, error) {
	return bridge.app.AccountDetails(strings.TrimSpace(name))
}

// UpdateAccount 原位更新账户；api_key 留空保留原密钥。
func (bridge *Bridge) UpdateAccount(update application.AccountUpdate) (application.AccountDetails, error) {
	return bridge.app.UpdateAccount(update)
}

// DeleteAccount 删除账户并返回被删条目的非秘密回执。
func (bridge *Bridge) DeleteAccount(name string) (application.AccountDetails, error) {
	return bridge.app.DeleteAccount(strings.TrimSpace(name))
}

// AccountBalance 查一个账号此刻的余额（角色卡账户行）。
//
// 余额是**快照**：不进快照、不缓存，点一次查一次（供应商侧一次账单请求，读的是
// 当下真实值）。供应商没有余额接口时返回一句可展示的原话（ErrBalanceUnsupported
// 的措辞），而不是猜一个 URL。
func (bridge *Bridge) AccountBalance(name string) (application.AccountBalance, error) {
	return bridge.app.AccountBalance(bridge.requestContext(), name)
}

func (bridge *Bridge) SwitchPlugin(name string) error {
	return bridge.app.SwitchPlugin(bridge.requestContext(), name)
}

func (bridge *Bridge) LoadMoreHistory(limit int) error {
	return bridge.app.LoadMoreHistory(limit)
}

func (bridge *Bridge) LoadLatestHistory() error {
	return bridge.app.LoadLatestHistory()
}

// SetWindowAppearance 落盘"当前皮肤 + 由它派生的窗口材质"。前端在换肤回流时调用
// （材质由 window-treatment.js 的责任链确认），run_wails 在**下次建窗时**读回它决定
// 窗口要不要走透明/亚克力——Wails 没有运行时开关，所以换毛玻璃皮肤需要重启才生效。
//
// 它是纯本地副作用（写一个小 JSON），不碰 Application，因此不设运行守卫：换肤在任何
// 时刻都该能记住。落盘失败按原样返回给调用方（前端只记不弹），不阻断换肤本身。
func (bridge *Bridge) SetWindowAppearance(skin, treatment string) error {
	return writeWindowAppearance(bridge.appearancePath, windowAppearance{
		Skin:            strings.TrimSpace(skin),
		WindowTreatment: normalizeWindowTreatment(treatment),
	})
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

// ArchiveSession 归档指定会话（C2）：转发 application 门控，成功后等目录
// 收敛再返回，使 renderer 重拉的快照已过滤归档行。
func (bridge *Bridge) ArchiveSession(sessionID string) error {
	if err := bridge.app.ArchiveSession(sessionID); err != nil {
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

// CreateWorkspaces 批量登记项目（不绑定当前会话）；前端在确认 temp list 后一次提交。
func (bridge *Bridge) CreateWorkspaces(requests []dto.WorkspaceCreateRequest) (dto.WorkspaceCreateBatchResult, error) {
	return bridge.app.CreateWorkspaces(requests)
}

func (bridge *Bridge) BindWorkspace(workspaceID string) error {
	return bridge.app.BindWorkspace(workspaceID)
}

func (bridge *Bridge) UnbindWorkspace() {
	bridge.app.UnbindWorkspace()
}

// SetFullAccess 切换全权模式并返回**真正生效的值**：前端据此渲染开关，
// 不再取反本地旧状态（旧状态滞后时取反会把"开启"点成"关闭"）。
func (bridge *Bridge) SetFullAccess(on bool) bool {
	return bridge.app.SetFullAccess(on)
}

// SetPermissionTier 切换视图会话的权限档位（manual/edit/auto/full）并返回**真正
// 生效的档位**：前端据此渲染当前档，不再猜方向。未识别的档位 id 报错（前端展示
// 后端错误，不静默改档）。会话级归属（G4）：只影响当前视图会话。
func (bridge *Bridge) SetPermissionTier(tier string) (string, error) {
	return bridge.app.SetPermissionTier(tier)
}

// ── 设置 → 角色设置（权限向量 / 默认 plugin）─────────────────────────────
//
// 前端 app.js 里的三处 invoke(...) 逐字对应下面三个方法：
//
//	invoke("PermissionVector")            读当前生效的一组权限标签
//	invoke("SetPermissionVector", tags)   提交整组标签（一次写入 = 一次完整状态）
//	invoke("SetDefaultPlugin", name)      设默认 plugin
//
// 作用对象是**当前视图会话**的**会话自持角色**：设置面板编辑的是"这个会话自己"的
// 角色设置，所以会话 ID 与会话角色都由桥面解析，前端不传（它也不该知道）。

// sessionSelfRole 是"会话自持角色"的规范名：权限向量按 (sessionID, role) 落键。
// 主代理在账号角色词表里是 agent（界面显示 mainagent）；root 只是它的执行主体类。
//
// 同一个名字也用在 application/core/service_assembler.go 的会话加载处（恢复该角色的
// 权限），两处必须一致——那里只能写字面量（core 不能反向 import gui），改名要一起改。
const sessionSelfRole = string(dto.RoleAgent)

// roleSettings 取角色设置写面（未装配显式报错，不静默退化）。
func (bridge *Bridge) roleSettings() (roleSettingsApplication, error) {
	if bridge == nil || bridge.app == nil {
		return nil, errors.New("bridge is not assembled")
	}
	app, ok := bridge.app.(roleSettingsApplication)
	if !ok {
		return nil, errors.New("当前 Application 未装配角色设置面（RolePermissionVector / SetRolePermissionVector）")
	}
	return app, nil
}

// ── 会话 SSH（项目环境表 + 会话状态）─────────────────────────────────────────
//
// 两块分开，口径不混（2026-10-25 用户定调）：
//   · 环境表挂在**项目**上（dev / test / prod …）——连接信息的唯一事实源；
//     项目操作直接收项目 ID，与会话是否已经建立持久 binding 无关；
//   · 会话只持有首次固定、之后不可切换的环境名。
//
// 桥面照抄 roleSettings 的做法：窄接口 + 类型断言，不往 Application 那条约面上
// 加方法（那条面是所有 fake 都要满足的最小集）。

// sessionSSHApplication 是会话 SSH 要用到的应用层方法（窄接口，由本包声明）。
type sessionSSHApplication interface {
	WorkspaceEnvironments(projectID string) ([]model.WorkspaceEnvironmentInfo, string, error)
	SetWorkspaceEnvironment(projectID, name string, change model.WorkspaceEnvironmentChange) (model.WorkspaceEnvironmentInfo, error)
	DeleteWorkspaceEnvironment(projectID, name string) error
	SetDefaultWorkspaceEnvironment(projectID, name string) error
	SessionSSHState(sessionID string) (model.SessionSSHInfo, error)
	SetSessionSSHEnv(sessionID, projectID, env string) (model.SessionSSHInfo, error)
	SetSessionSSHAllowed(sessionID string, envs []string) (model.SessionSSHInfo, error)
}

func (bridge *Bridge) sessionSSH() (sessionSSHApplication, error) {
	if bridge == nil || bridge.app == nil {
		return nil, errors.New("bridge is not assembled")
	}
	app, ok := bridge.app.(sessionSSHApplication)
	if !ok {
		return nil, errors.New("当前 Application 未装配会话 SSH 面（WorkspaceEnvironments / SessionSSHState）")
	}
	return app, nil
}

// WorkspaceEnvironments 读指定项目的环境表与默认环境名。
func (bridge *Bridge) WorkspaceEnvironments(projectID string) ([]model.WorkspaceEnvironmentInfo, string, error) {
	app, err := bridge.sessionSSH()
	if err != nil {
		return nil, "", err
	}
	return app.WorkspaceEnvironments(strings.TrimSpace(projectID))
}

// SetWorkspaceEnvironment 写一个项目环境（口令三态；Delete=true 则删除）。
func (bridge *Bridge) SetWorkspaceEnvironment(projectID, name string, change model.WorkspaceEnvironmentChange) (model.WorkspaceEnvironmentInfo, error) {
	app, err := bridge.sessionSSH()
	if err != nil {
		return model.WorkspaceEnvironmentInfo{}, err
	}
	return app.SetWorkspaceEnvironment(strings.TrimSpace(projectID), name, change)
}

// DeleteWorkspaceEnvironment 删除一个项目环境（连同它的口令）。
func (bridge *Bridge) DeleteWorkspaceEnvironment(projectID, name string) error {
	app, err := bridge.sessionSSH()
	if err != nil {
		return err
	}
	return app.DeleteWorkspaceEnvironment(strings.TrimSpace(projectID), name)
}

// SetDefaultWorkspaceEnvironment 指定项目默认环境（空串 = 取消默认）。
func (bridge *Bridge) SetDefaultWorkspaceEnvironment(projectID, name string) error {
	app, err := bridge.sessionSSH()
	if err != nil {
		return err
	}
	return app.SetDefaultWorkspaceEnvironment(strings.TrimSpace(projectID), name)
}

// SessionSSHState 读当前视图会话的固定工作环境。
func (bridge *Bridge) SessionSSHState() (model.SessionSSHInfo, error) {
	app, err := bridge.sessionSSH()
	if err != nil {
		return model.SessionSSHInfo{}, err
	}
	return app.SessionSSHState(bridge.app.Snapshot().Session.ID)
}

// SetSessionSSHEnv 首次固定当前视图会话的工作环境。绑定后不可修改或清空；
// 换环境必须新建会话。
func (bridge *Bridge) SetSessionSSHEnv(projectID, env string) (model.SessionSSHInfo, error) {
	app, err := bridge.sessionSSH()
	if err != nil {
		return model.SessionSSHInfo{}, err
	}
	return app.SetSessionSSHEnv(bridge.app.Snapshot().Session.ID, strings.TrimSpace(projectID), env)
}

// SetSessionSSHAllowed 是旧会话放权状态的兼容写面；新流程以固定环境为准。
func (bridge *Bridge) SetSessionSSHAllowed(envs []string) (model.SessionSSHInfo, error) {
	app, err := bridge.sessionSSH()
	if err != nil {
		return model.SessionSSHInfo{}, err
	}
	return app.SetSessionSSHAllowed(bridge.app.Snapshot().Session.ID, envs)
}

// PermissionVector 读当前视图会话自持角色的**生效**权限向量：登记优先，缺失按五级
// 链路解析（user_allow > agent_dispatch > session_default > global_default）。
//
// 返回的是解析结果（允许的标签，规范组序），不含 `!` 否定项——否定项是层内表达，
// 不是"当前能力"。错误如实返回（未装配登记能力 = 显式错误，前端因此标"未生效"而
// 不是把空向量画成"什么都没开"）。
func (bridge *Bridge) PermissionVector() ([]string, error) {
	app, err := bridge.roleSettings()
	if err != nil {
		return nil, err
	}
	sessionID := bridge.app.Snapshot().Session.ID
	return app.RolePermissionVector(sessionID, sessionSelfRole)
}

// PermissionRoles 列出权限面板的「按角色」行：角色名 + 名下账号 + 该角色的默认向量。
//
// 角色名取自**账号名前缀**（agent-1 → agent），判据与 internal/model.AccountRoleFromName
// 同一条：界面上的角色就是 accounts.yaml 里 roles.* 的那几个词，不另造一套名字，
// 否则"面板上的角色"与"配置里的角色"会成为两份对不上的事实。
//
// 一次调用读回全部角色（面板打开时只要一次往返），每行的向量都带 explicit：
// explicit=false 表示这个角色没定过默认，此时 Vector 是链路解析结果（只读预览）。
func (bridge *Bridge) PermissionRoles() ([]dto.PermissionRoleInfo, error) {
	app, err := bridge.roleSettings()
	if err != nil {
		return nil, err
	}
	snapshot := bridge.app.Snapshot()
	order, groups := permissionRoleGroups(snapshot.Runtime.Accounts)
	out := make([]dto.PermissionRoleInfo, 0, len(order))
	for _, role := range order {
		info, err := permissionRoleInfoFor(app, snapshot.Session.ID, role, groups[role])
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, nil
}

// RoleDefaultVector 读一个角色的默认向量（explicit=false 时 Vector 是链路解析结果）。
func (bridge *Bridge) RoleDefaultVector(role string) (dto.PermissionRoleInfo, error) {
	app, err := bridge.roleSettings()
	if err != nil {
		return dto.PermissionRoleInfo{}, err
	}
	role = strings.TrimSpace(role)
	if role == "" {
		return dto.PermissionRoleInfo{}, errors.New("permission roles: role must not be empty")
	}
	snapshot := bridge.app.Snapshot()
	_, groups := permissionRoleGroups(snapshot.Runtime.Accounts)
	return permissionRoleInfoFor(app, snapshot.Session.ID, role, groups[role])
}

// SetRoleDefaultVector 写一个角色的默认向量，并返回**落盘后读回**的那一行。
//
// 与 SetPermissionVector 同一条口径：前端采纳返回值，不假定请求值生效——写进去的
// 是"默认"，而生效值还要和三态规整（`!tag`、重复项）对一遍。
func (bridge *Bridge) SetRoleDefaultVector(role string, tags []string) (dto.PermissionRoleInfo, error) {
	app, err := bridge.roleSettings()
	if err != nil {
		return dto.PermissionRoleInfo{}, err
	}
	role = strings.TrimSpace(role)
	if role == "" {
		return dto.PermissionRoleInfo{}, errors.New("permission roles: role must not be empty")
	}
	sessionID := bridge.app.Snapshot().Session.ID
	if err := app.SetRoleDefaultVector(sessionID, role, tags); err != nil {
		return dto.PermissionRoleInfo{}, err
	}
	snapshot := bridge.app.Snapshot()
	_, groups := permissionRoleGroups(snapshot.Runtime.Accounts)
	return permissionRoleInfoFor(app, sessionID, role, groups[role])
}

// SetRoleAccount 设置某个角色优先使用的账号，并返回落盘后读回的整行。
// 账号必须来自同名角色组；跨角色账号会让下一次回合使用错误凭据。
func (bridge *Bridge) SetRoleAccount(role, account string) (dto.PermissionRoleInfo, error) {
	app, err := bridge.roleSettings()
	if err != nil {
		return dto.PermissionRoleInfo{}, err
	}
	role, account = strings.TrimSpace(role), strings.TrimSpace(account)
	if role == "" || account == "" {
		return dto.PermissionRoleInfo{}, errors.New("permission roles: role and account are required")
	}
	snapshot := bridge.app.Snapshot()
	_, groups := permissionRoleGroups(snapshot.Runtime.Accounts)
	valid := false
	for _, candidate := range groups[role] {
		if candidate.Name == account {
			valid = true
			break
		}
	}
	if !valid {
		return dto.PermissionRoleInfo{}, fmt.Errorf("permission roles: account %q does not belong to role %q", account, role)
	}
	if err := app.SetRoleAccount(snapshot.Session.ID, role, account); err != nil {
		return dto.PermissionRoleInfo{}, err
	}
	return permissionRoleInfoFor(app, snapshot.Session.ID, role, groups[role])
}

// SetRoleInheritance 切换 subagent / teammate 是否继承 mainagent 的默认权限。
func (bridge *Bridge) SetRoleInheritance(role string, inherit bool) (dto.PermissionRoleInfo, error) {
	app, err := bridge.roleSettings()
	if err != nil {
		return dto.PermissionRoleInfo{}, err
	}
	role = strings.TrimSpace(role)
	snapshot := bridge.app.Snapshot()
	if err := app.SetRoleInheritMainAgent(snapshot.Session.ID, role, inherit); err != nil {
		return dto.PermissionRoleInfo{}, err
	}
	_, groups := permissionRoleGroups(snapshot.Runtime.Accounts)
	return permissionRoleInfoFor(app, snapshot.Session.ID, role, groups[role])
}

// SetRoleSSHUserSpaces 写角色在远端项目根之外的路径允许范围。
func (bridge *Bridge) SetRoleSSHUserSpaces(role string, paths []string) (dto.PermissionRoleInfo, error) {
	app, err := bridge.roleSettings()
	if err != nil {
		return dto.PermissionRoleInfo{}, err
	}
	role = strings.TrimSpace(role)
	snapshot := bridge.app.Snapshot()
	if err := app.SetRoleSSHUserSpaces(snapshot.Session.ID, role, paths); err != nil {
		return dto.PermissionRoleInfo{}, err
	}
	_, groups := permissionRoleGroups(snapshot.Runtime.Accounts)
	return permissionRoleInfoFor(app, snapshot.Session.ID, role, groups[role])
}

// SetRoleSessionSSHUserSpaces 写当前会话中单个 agent role 的 SSH 路径覆盖。
func (bridge *Bridge) SetRoleSessionSSHUserSpaces(role string, paths []string) (dto.PermissionRoleInfo, error) {
	app, err := bridge.roleSettings()
	if err != nil {
		return dto.PermissionRoleInfo{}, err
	}
	role = strings.TrimSpace(role)
	snapshot := bridge.app.Snapshot()
	if err := app.SetRoleSessionSSHUserSpaces(snapshot.Session.ID, role, paths); err != nil {
		return dto.PermissionRoleInfo{}, err
	}
	_, groups := permissionRoleGroups(snapshot.Runtime.Accounts)
	return permissionRoleInfoFor(app, snapshot.Session.ID, role, groups[role])
}

// SetRoleSessionPermission 写本会话里某个 agent role 的显式权限覆盖。
// 它只影响当前会话，不改变角色的跨会话默认。
func (bridge *Bridge) SetRoleSessionPermission(role string, tags []string) (dto.PermissionRoleInfo, error) {
	app, err := bridge.roleSettings()
	if err != nil {
		return dto.PermissionRoleInfo{}, err
	}
	role = strings.TrimSpace(role)
	snapshot := bridge.app.Snapshot()
	if err := app.SetRolePermissionVector(snapshot.Session.ID, role, tags); err != nil {
		return dto.PermissionRoleInfo{}, err
	}
	_, groups := permissionRoleGroups(snapshot.Runtime.Accounts)
	return permissionRoleInfoFor(app, snapshot.Session.ID, role, groups[role])
}

// permissionRoleGroups 把账号列表按角色归并：返回角色顺序（按账号出现顺序，稳定）
// 与角色 → 账号列表。
func permissionRoleGroups(accounts []model.AccountInfo) ([]string, map[string][]model.AccountInfo) {
	order := make([]string, 0, len(accounts)+len(dto.AccountRoleVocabulary()))
	groups := map[string][]model.AccountInfo{}
	// 四个标准 agent role 是权限板的固定骨架：即使某个角色今天没有账号，用户仍要能
	// 先配置它的默认权限，再从该角色卡添加账号。账号只是角色的子项，不能反过来决定
	// 角色是否存在。
	for _, role := range dto.AccountRoleVocabulary() {
		order = append(order, role)
		groups[role] = []model.AccountInfo{}
	}
	for _, account := range accounts {
		role := strings.TrimSpace(account.Role)
		if role == "" {
			// 兼容旧宿主快照：新快照总是携带显式 Role。
			role = string(dto.AccountRoleFromName(account.Name))
		}
		if _, ok := groups[role]; !ok {
			order = append(order, role)
			groups[role] = []model.AccountInfo{}
		}
		groups[role] = append(groups[role], account)
	}
	return order, groups
}

// permissionRoleInfoFor 组装权限面板的一行：账号名 + 展示用供应商/模型 + 生效默认向量。
// 供应商/模型取第一个未禁用账号（只做展示，任何判定都不看它）。
func permissionRoleInfoFor(app roleSettingsApplication, sessionID, role string, accounts []model.AccountInfo) (dto.PermissionRoleInfo, error) {
	info := dto.PermissionRoleInfo{Role: role}
	for _, account := range accounts {
		info.Accounts = append(info.Accounts, account.Name)
		if !account.Disabled && info.Model == "" {
			info.Provider, info.Model = account.Provider, account.Model
		}
	}
	defaultVector, explicit, err := app.RoleDefaultVector(sessionID, role)
	if err != nil {
		return dto.PermissionRoleInfo{}, err
	}
	effectiveVector, err := app.RolePermissionVector(sessionID, role)
	if err != nil {
		return dto.PermissionRoleInfo{}, err
	}
	info.Vector = effectiveVector
	if explicit {
		info.DefaultVector = defaultVector
	} else {
		info.DefaultVector = []string{}
	}
	info.Explicit = explicit
	info.InheritMainAgent, err = app.RoleInheritMainAgent(sessionID, role)
	if err != nil {
		return dto.PermissionRoleInfo{}, err
	}
	info.SSHUserSpaces, err = app.RoleSSHUserSpaces(sessionID, role)
	if err != nil {
		return dto.PermissionRoleInfo{}, err
	}
	info.DefaultSSHUserSpaces, err = app.RoleDefaultSSHUserSpaces(sessionID, role)
	if err != nil {
		return dto.PermissionRoleInfo{}, err
	}
	info.SessionSSHUserSpaces = append([]string{}, info.SSHUserSpaces...)
	account, _, err := app.RoleAccount(sessionID, role)
	if err != nil {
		return dto.PermissionRoleInfo{}, err
	}
	for _, candidate := range info.Accounts {
		if candidate == account {
			info.Account = account
			break
		}
	}
	return info, nil
}

// SetPermissionVector 提交当前视图会话自持角色的**整组**权限标签（不是增删单格：
// 一次写入 = 一次完整状态，后端不用猜这次是加还是减），并返回落盘后的生效向量。
//
// 返回值是**真正生效**的那个（规整后的），前端采纳它而不是假定请求值生效——与
// SetPermissionTier / SetSessionReleaseWindow 同一条口径。
func (bridge *Bridge) SetPermissionVector(tags []string) ([]string, error) {
	app, err := bridge.roleSettings()
	if err != nil {
		return nil, err
	}
	sessionID := bridge.app.Snapshot().Session.ID
	if err := app.SetRolePermissionVector(sessionID, sessionSelfRole, tags); err != nil {
		return nil, err
	}
	return app.RolePermissionVector(sessionID, sessionSelfRole)
}

// SetDefaultPlugin 设"这个会话默认装载的 plugin"。
//
// 诚实边界（不假装写成）：默认 plugin 的**持久化落点今天还不存在**——角色设置存储
// 只承载权限向量；进程启动恒激活 "default"（wiring/bootstrap.ActivateDefaultPlugin）。
// 因此这里显式报出缺落点，而不是把它实现成"现在就切一次 plugin"：SwitchPlugin 会
// 重建提示栈并清空当前对话（且运行中会话直接拒绝），那是另一件事，共用入口等于让
// 用户在设置弹窗里意外丢历史。落点补齐后本方法改为写配置即可，语义不变。
func (bridge *Bridge) SetDefaultPlugin(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("默认 plugin 名不能为空")
	}
	return errors.New("默认 plugin 还没有持久化落点：角色设置存储今天只承载权限向量；" +
		"要立刻切换请用 SwitchPlugin（会重建提示栈并清空当前对话）")
}

func (bridge *Bridge) ScheduleTask(spec dto.ScheduledTaskSpec) (*dto.ScheduledTaskStatus, error) {
	return bridge.app.ScheduleTask(bridge.requestContext(), spec)
}

// UpdateScheduledTask 编辑既有定时/周期任务（同一份 spec 形状；任务 ID 是操作键，
// 名称只做展示）。
func (bridge *Bridge) UpdateScheduledTask(id string, spec dto.ScheduledTaskSpec) (*dto.ScheduledTaskStatus, error) {
	return bridge.app.UpdateScheduledTask(bridge.requestContext(), id, spec)
}

// CancelScheduledTask 取消并移除定时/周期任务。
func (bridge *Bridge) CancelScheduledTask(id string) error {
	return bridge.app.CancelScheduledTask(id)
}

// ── 角色会话读面（团队看板用）────────────────────────────────────────
//
// 2026-10-12（J-1a）：本段原本是 Agent Team 子页的数据源（成员表 / 角色 CRUD /
// 工作顺序 / 团队库 / 员工库 / 母本 / 员工提示词），整条退场；只留下面两条**读者**
// （角色会话观察面 + teammate 实时会话），它们服务的是团队看板（teamwork，保留面）。
//
// sessionID 为空 = 当前视图会话（前端不必自己记会话号；会话切换后重拉即可）。
// 这些方法只做参数归一与窄转发，语义在 application 与存储侧。

func (bridge *Bridge) roleSessionApp() (roleSessionApplication, error) {
	app, ok := bridge.app.(roleSessionApplication)
	if !ok {
		return nil, errors.New("当前 Application 未装配角色会话读面")
	}
	return app, nil
}

// roleSessionTarget 解析目标会话：显式 ID 优先，空值取当前视图会话。
func (bridge *Bridge) roleSessionTarget(sessionID string) string {
	if trimmed := strings.TrimSpace(sessionID); trimmed != "" {
		return trimmed
	}
	return bridge.app.Snapshot().Session.ID
}

// AgentTeamRoleSnapshot 读取某个角色会话的只读观察面（成员行「查看会话」/ 团队看板
// 「员工会话」入口）：EXEC（main）与 ADVISOR（tl）等角色各自有独立会话，前端据此把
// 两个 agent 的真实行分开显示，而不是把角色正文混进主会话。只读，不写任何状态。
//
// 方法名**保留不改**（2026-10-12 J-1a 的显式口径）：前端 `gui/frontend/dist/app.js`
// 按这个名字调 RPC，改名等于给并行条目制造一次无谓联动。它是**通用角色会话只读
// 观察面**，与已经退场的 Agent Team 装配面无关。
//
// 2026-10-12 起 `role_session_id` 必填，调用方自己给：此前"只带 role_name"的回退是
// 从成员表（`AgentTeamView`）反解角色会话号——那张成员表已随装配面退场，回退没有
// 事实来源，所以这里**不再有回退**（缺号即报错，不再猜）。
func (bridge *Bridge) AgentTeamRoleSnapshot(sessionID, roleName, roleSessionID string) (dto.RoleSnapshot, error) {
	app, err := bridge.roleSessionApp()
	if err != nil {
		return dto.RoleSnapshot{}, err
	}
	roleName = strings.TrimSpace(roleName)
	if roleName == "" {
		return dto.RoleSnapshot{}, errors.New("角色名不能为空")
	}
	session := bridge.roleSessionTarget(sessionID)
	if session == "" {
		return dto.RoleSnapshot{}, errors.New("当前没有可查看的角色会话")
	}
	roleSessionID = strings.TrimSpace(roleSessionID)
	if roleSessionID == "" {
		return dto.RoleSnapshot{}, fmt.Errorf("角色 %s 还没有独立会话（role_session_id 必填）", roleName)
	}
	return app.RoleSnapshot(session, roleName, roleSessionID)
}

// TeammateSessionLive 读取**当前 teammate 会话**的实时只读投影（"这件事的会话此刻在
// 说什么"）——团队看板里点开一个工作项/一位 teammate 时用它，而不是读会话库。
//
// 为什么要单开一个读面（2026-10-04 用户口径：查看 teammate 的会话"全是历史会话"）：
// 员工的角色会话是落盘的长期历史（跨工作项、跨轮次）；而一 Work Item 有**自己的会话**
// （一 Work Item 一套 Session + worktree），那一轮活是**进程内执行面**（刻意不接
// DurableHistory）——从会话库读它只会读到主会话的历史。
//
// 只读，不写任何状态；会话不在本进程里 → Running=false 的视图（调用方据此如实说明，
// 而不是假装"看到的是空的当前会话"）。
func (bridge *Bridge) TeammateSessionLive(roleSessionID string) (dto.TeammateSessionLiveView, error) {
	return bridge.teammateSessionLive(roleSessionID, 0, 0, false)
}

// TeammateSessionLivePage 是同一份读数的**分页读法**（有界窗口 + 分页）：前端"翻页"用
// 它（offset 从该会话第一条算起；limit<=0 → 后端默认页大小）。与 TeammateSessionLive
// 共用同一份裁剪/分页判据，桥只做参数归一与转发，不缓存、不推导第二份事实。
func (bridge *Bridge) TeammateSessionLivePage(roleSessionID string, offset, limit int) (dto.TeammateSessionLiveView, error) {
	return bridge.teammateSessionLive(roleSessionID, offset, limit, true)
}

// teammateSessionLive 是上面两个读法的共同实现（同一份参数归一：只读、不写任何状态）。
func (bridge *Bridge) teammateSessionLive(roleSessionID string, offset, limit int, paginated bool) (dto.TeammateSessionLiveView, error) {
	app, err := bridge.roleSessionApp()
	if err != nil {
		return dto.TeammateSessionLiveView{}, err
	}
	roleSessionID = strings.TrimSpace(roleSessionID)
	if roleSessionID == "" {
		return dto.TeammateSessionLiveView{}, errors.New("会话号不能为空（工作项自己的会话号）")
	}
	if paginated {
		return app.TeammateSessionLivePageFor(roleSessionID, offset, limit), nil
	}
	return app.TeammateSessionLiveFor(roleSessionID), nil
}

// WorkItemExecution is the single server read for WorkItem details and the
// teammate WorkItem progress/session page.
func (bridge *Bridge) WorkItemExecution(mainSessionID, itemID string, offset, limit int) (dto.WorkItemExecutionView, error) {
	app, err := bridge.roleSessionApp()
	if err != nil {
		return dto.WorkItemExecutionView{}, err
	}
	return app.WorkItemExecutionFor(bridge.requestContext(), strings.TrimSpace(mainSessionID), strings.TrimSpace(itemID), dto.ConversationViewRequest{
		Offset: offset, Limit: limit,
	})
}

// TeammateInspection returns prompt, plugin, limit, and permission details.
func (bridge *Bridge) TeammateInspection(mainSessionID, role string) (dto.TeammateInspectionView, error) {
	app, err := bridge.roleSessionApp()
	if err != nil {
		return dto.TeammateInspectionView{}, err
	}
	return app.TeammateInspectionFor(bridge.requestContext(), strings.TrimSpace(mainSessionID), strings.TrimSpace(role))
}

func (bridge *Bridge) ConversationView(ref dto.ConversationSessionRef, offset, limit int) (dto.SessionRecordPage, error) {
	app, err := bridge.roleSessionApp()
	if err != nil {
		return dto.SessionRecordPage{}, err
	}
	return app.ConversationView(bridge.requestContext(), ref, dto.ConversationViewRequest{Offset: offset, Limit: limit})
}

func (bridge *Bridge) ConversationChat(ref dto.ConversationSessionRef, message string, waitMS int) (dto.ChatReceipt, error) {
	app, err := bridge.roleSessionApp()
	if err != nil {
		return dto.ChatReceipt{}, err
	}
	return app.ConversationChat(bridge.requestContext(), ref, dto.ConversationChatRequest{Message: message, WaitMS: waitMS})
}

func (bridge *Bridge) RecoverConversation(ref dto.ConversationSessionRef) (dto.SessionRecovery, error) {
	app, err := bridge.roleSessionApp()
	if err != nil {
		return dto.SessionRecovery{}, err
	}
	return app.RecoverConversation(bridge.requestContext(), ref)
}

// ── Agent Team 装配面已退场（2026-10-12 J-1a）
//
// 这一段原本是团队库 / 员工库 / 全局母本 / 员工提示词的窄转发（GUI「团队库」「员工库」
// 两张表与「普及搭配到全局」的数据源），随 Agent Team 用户面整条删除。
// 会话角色基座（RoleSessionID / 角色 draft / floor / RoleSnapshot）与团队看板
// （teamwork）**不在**这一段，它们仍在。
// SearchHistory 检索会话历史聊天记录（压缩栈索引 → 真实记录；
// Wails 前端历史检索面板数据源，返回门面再导出的 `application.SearchResult`）。
func (bridge *Bridge) SearchHistory(query string, limit int) (application.SearchResult, error) {
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

// WorkspaceGitRepos 转发「工作区内 git 仓库发现」（仓库标签栏数据源；扫描边界、
// 剪枝与预算在 workspace 域内保证，应用层只做端口断言与文案回落）。
func (bridge *Bridge) WorkspaceGitRepos() (dto.GitReposResult, error) {
	return bridge.app.WorkspaceGitRepos()
}

// WorkspaceGitLog 转发指定仓库的 git 提交记录（repo 相对工作区根，"." = 工作
// 区根；最近 limit 条提交 + 父提交拓扑；只读元数据，不含 diff/文件内容；
// 非 git 仓库返回 Result.Error）。
func (bridge *Bridge) WorkspaceGitLog(repo string, limit int) (dto.GitLogResult, error) {
	return bridge.app.WorkspaceGitLog(repo, limit)
}

// WorkspaceChanges 转发指定仓库的未提交改动（repo 口径同 WorkspaceGitLog；暂存/
// 未暂存/未跟踪/冲突；只读元数据——路径与状态字符，不含 diff/文件内容；
// 非 git 仓库返回 Result.Error 供面板显示错误态）。
func (bridge *Bridge) WorkspaceChanges(repo string, limit int) (dto.WorkspaceChangesResult, error) {
	return bridge.app.WorkspaceChanges(repo, limit)
}

// WorkspaceGitCommitDetail 转发指定仓库某个提交的改动文件清单（提交记录「点开
// 某一条」数据源；只读元数据——状态/路径/重命名原路径/±行数，不含 diff 或文件
// 内容；非 git 仓库返回 Result.Error 供面板显示错误态）。
func (bridge *Bridge) WorkspaceGitCommitDetail(repo, hash string, limit int) (dto.GitCommitDetail, error) {
	return bridge.app.WorkspaceGitCommitDetail(repo, hash, limit)
}

// WorkspaceGitCommitFileContent 转发指定仓库"某文件在某个提交时的内容"读取
// （提交记录「点开某个文件」数据源；字节来自 git 对象库，可见性边界与工作树
// 预览同一条；只读受控字节，不进快照，也不写回工作区）。
func (bridge *Bridge) WorkspaceGitCommitFileContent(repo, hash, relPath string, limit int64) (dto.FileContent, error) {
	return bridge.app.WorkspaceGitCommitFileContent(repo, hash, relPath, limit)
}

// WorkspaceFileContent 转发工作树文件预览读取（containment/敏感过滤/上限
// 在 application + workspace 层保证；只读受控字节，不进快照）。
func (bridge *Bridge) WorkspaceFileContent(relPath string, limit int64) (dto.FileContent, error) {
	return bridge.app.WorkspaceFileContent(relPath, limit)
}

// WorkspaceWriteFile 转发工作树文件写入（「文件详情」面板的编辑保存）：用户
// 自己按下的保存，是用户动作而不是主代理动作——不经过权限档位与执行选择页面；
// 可见性边界、二进制拒绝与原子发布在 application + workspace 层保证。
func (bridge *Bridge) WorkspaceWriteFile(relPath, content string) (dto.FileWriteResult, error) {
	return bridge.app.WorkspaceWriteFile(relPath, content)
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

// toolFailureBoardProvider 是 Application 的**窄可选**能力面：工具失败分布看板
// （会话级读数：跟当前视图会话走，落该会话自己的 tool-record.jsonl）。
//
// 为什么是"可选窄接口 + 类型断言"而不是 Application 的成员：口径与
// contract.TeamworkBoardProjection / context_runtime.CompactionIndexPort 一致——
// **有就有、没有就是没装配**。未装配时返回空看板（前端整块 hidden），而不是逼每个
// fake / harness 为了一个只读面板长出空方法。
type toolRecordBoardProvider interface {
	ToolRecordBoard() application.ToolRecordBoard
	// ToolRecordEnabled / SetToolRecordEnabled：计数开关的读与写（设置里那一个开关）。
	ToolRecordEnabled() bool
	SetToolRecordEnabled(enabled bool) (bool, error)
}

// ToolRecordBoard 返回工具记录看板（会话级读数：当前视图会话自己的累计）。
//
// 走"按需拉取"而不是快照：快照是**会话作用域**的可重放状态，把进程级计数塞进去会
// 让"重放一份会话快照"听起来像"重放这台机器的计数"（与 PerfStats 同一条理由）。
func (bridge *Bridge) ToolRecordBoard() application.ToolRecordBoard {
	if provider, ok := bridge.app.(toolRecordBoardProvider); ok {
		return provider.ToolRecordBoard()
	}
	return application.ToolRecordBoard{}
}

// SetToolRecordEnabled 开关工具记录计数（设置里那一个开关；返回生效值）。
//
// 未装配记录面的宿主显式报错（而不是静默返回 false）：用户按了开关却什么都没发生，
// 比"这个宿主不支持"更坏。
func (bridge *Bridge) SetToolRecordEnabled(enabled bool) (bool, error) {
	provider, ok := bridge.app.(toolRecordBoardProvider)
	if !ok {
		return false, fmt.Errorf("工具记录未装配（宿主不支持计数开关）")
	}
	return provider.SetToolRecordEnabled(enabled)
}

// PromptLayers 返回当前会话注入的 prompt 前缀层（轨迹视图数据源）。
func (bridge *Bridge) PromptLayers() []application.PromptLayer {
	if bridge == nil || bridge.app == nil {
		return nil
	}
	return bridge.app.PromptLayers()
}
