package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	frameworkSession "github.com/RedHuang-0622/Seele/session"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/application/core"
	applicationwindow "github.com/RedHuang-0622/seelex/application/window"
	assemble "github.com/RedHuang-0622/seelex/backend/engine/assemble"
	"github.com/RedHuang-0622/seelex/backend/limit"
	"github.com/RedHuang-0622/seelex/backend/limit/overlay"
	toolreg "github.com/RedHuang-0622/seelex/backend/tool/registry"
	"github.com/RedHuang-0622/seelex/backend/tool/search"
	"github.com/RedHuang-0622/seelex/internal/bootseed"
	"github.com/RedHuang-0622/seelex/store"
	storeroot "github.com/RedHuang-0622/seelex/store/root"
	"github.com/RedHuang-0622/seelex/store/workspace"
	"github.com/RedHuang-0622/seelex/wiring/config"
)

// runtimeLimits 是 initRuntime 加载的 limits（后续初始化消费）。
var runtimeLimits limit.Limits

// initRuntime 装配 Runtime：配置责任链 → limits（含数据根覆盖层）→ 账号池。
// storePath 是数据根（显式输入：覆盖层落点由它决定）。
func initRuntime(storePath string) (*assemble.Runtime, error) {
	// 运行参数在 config/seelex.yaml（配置参数文件；权限在 config/seele.yaml）：
	// 责任链 = CWD 的 config/ → CWD 根目录 → 二进制目录的 config/（见
	// config.RuntimeConfigChain）。命中就按它读（存在即读）；全都没有 → 用内嵌默认档在
	// 二进制旁边初始化，再读它（internal/bootseed）。滑动窗口段缺失 → 零值走默认；
	// limits 缺失字段 → 默认值。
	runtimeConfigPath := config.EnsureConfigFile(bootseed.RuntimeConfigName, bootseed.RuntimeConfigPack())
	windowConfig, err := applicationwindow.LoadWindowConfig(runtimeConfigPath)
	if err != nil {
		return nil, fmt.Errorf("加载 window 配置失败: %w", err)
	}
	limits, err := limit.LoadLimits(runtimeConfigPath)
	if err != nil {
		return nil, fmt.Errorf("加载 limits 配置失败: %w", err)
	}
	limits = limits.WithDefaults()
	// 界面上的覆盖层优先于配置：用户在设置弹窗里改过的运行时参数落盘在数据根，
	// 启动时叠在 config/seelex.yaml 的 limits 段之上（内置默认 ← 配置 ← 覆盖层），
	// 见 backend/limit/overlay（M2-C2 归位前在仓库根的 runtime_limits_overlay.go）。
	limitsOverlay := overlay.Read(config.RuntimeLimitsPath(storePath))
	limits = overlay.Apply(limits, limitsOverlay)
	runtimeLimits = limits // InitStore/InitEngine 等后续初始化消费
	toolCallTimeout, _, planDecision, heartbeat, replanWindow, searchTimeout := limits.Durations()
	core.ApplyLimits(limits)
	// window 段同时注入应用侧压缩保留窗口决策（压缩前缀 = min(retain_tokens,
	// ratio × 全量上下文)，硬压缩阈值 force_compact_tokens）。
	applicationwindow.Apply(windowConfig)
	search.ApplyLimits(int(searchTimeout / time.Second))
	runtime, err := assemble.NewRuntime(assemble.RuntimeConfig{
		AccountsPath: config.AccountsPath(), StorePath: storePath,
		// 定时任务定义落**全局** JSONL：与 workspace_index.json 同级（store 的
		// 基目录），不按项目分区、不按会话分片。会话记录仍走 store。
		ScheduledTasksPath:        filepath.Join(filepath.Dir(storePath), "scheduled-tasks.jsonl"),
		ToolCallTimeout:           toolCallTimeout,
		PlanDecisionTimeout:       planDecision,
		HeartbeatInterval:         heartbeat,
		ReplanWindow:              replanWindow,
		MaxConcurrentReplans:      limits.MaxConcurrentReplans,
		MaxReplansPerWindow:       limits.MaxReplansPerWindow,
		MaxReplanProviderRequests: limits.MaxReplanProviderReqs,
		WindowConfig:              windowConfig,
		Limits:                    limits,
		// 界面上按账号设过的窗口（数据根覆盖层的 account_windows 表）：优先级在
		// Runtime.ContextWindowFor 一处判定（账号级 > limits.context_window > 账号档）。
		AccountWindows: limitsOverlay.AccountWindows,
	})
	if err != nil {
		return nil, fmt.Errorf("初始化 Seele Runtime 失败: %w", err)
	}
	// 保留窗口配置校验（《压缩四区模型》边界判定）：保护区下限（比例 × 预算）
	// 高于保留上限 window.retain_tokens 是非法组合，必须在这里拒绝启动 ——
	// 静默取小会把它吞成"配置看起来生效"，直到保护区被压到失忆才以"模型忘了"
	// 的形式显现。预算口径与压缩判据同源（task_context.ContextBudgetFor）。
	if err := core.ValidateRetainWindow(windowConfig, limits, runtime.ContextWindow()); err != nil {
		runtime.Shutdown()
		return nil, fmt.Errorf("加载 window 配置失败: %w", err)
	}
	return runtime, nil
}

func guardMultiProcess(storePath string) error {
	if runtimeLimits.Runtime.AllowMultiProcess {
		return nil
	}
	dataRoot := filepath.Dir(storePath)
	staleAfter := time.Duration(runtimeLimits.SessionStorage.LockStaleAfterSeconds) * time.Second
	if owner, held := storeroot.DataRootLockedByWith(dataRoot, staleAfter); held {
		return fmt.Errorf(
			"数据根 %s 正被另一个 Seelex 实例独占（pid=%d program=%s host=%s）：默认单实例。"+
				"要允许并行启动，在 config/seelex.yaml 设 limits.runtime.allow_multi_process: true"+
				"（代价：多个进程共用同一数据根，失去跨进程写者串行化，一致性由使用者负责）",
			dataRoot, owner.PID, owner.Program, owner.Hostname)
	}
	return nil
}

func InitStore(storePath string) (*store.Router, error) {
	// NestedSessionStore 的 baseDir 与 workspace_index.json 同级
	baseDir := filepath.Dir(storePath)
	store.ApplyLimits(runtimeLimits.SummaryChars)
	router, err := store.NewRouter(baseDir, sessionStorageLimits())
	if err != nil {
		return nil, fmt.Errorf("初始化嵌套存储失败: %w", err)
	}
	return router, nil
}

// sessionStorageLimits 把 seelex.yaml limits.session_storage 映射成存储覆盖层
// （my_design §11 覆盖链：默认值 ← limits，这是唯一可调层）。
// 分片行数沿用既有顶层键 limits.message_shard_size。
func sessionStorageLimits() storeroot.Settings {
	limits := runtimeLimits.SessionStorage
	// 指针：把 limits.runtime.allow_multi_process 原样传进 §9 数据根锁（存储侧默认
	// 也是 false，这里显式带过去，让配置一眼看得出生效）。
	allowMultiProcess := runtimeLimits.Runtime.AllowMultiProcess
	return storeroot.Settings{
		MessageShardRows:        runtimeLimits.MessageShardSize,
		RetryCacheMaxItems:      limits.RetryCacheMaxItems,
		RetryCacheMaxChars:      limits.RetryCacheMaxChars,
		WireRecentErrors:        limits.RetryCacheWireRecentErrors,
		CompactFrameThreshold:   limits.RetentionCompactFrameThreshold,
		RawBytesAlert:           uint64(limits.RetentionRawBytesAlert),
		QueuePersistPending:     limits.QueuePersistPending,
		StaleAfterSeconds:       limits.LockStaleAfterSeconds,
		AutoRecover:             limits.LockAutoRecover,
		AllowMultiProcess:       &allowMultiProcess,
		BlobSoftLimitChars:      limits.BlobSoftLimitChars,
		BlobHardLimitBytes:      limits.BlobHardLimitBytes,
		BlobSessionQuotaBytes:   limits.BlobSessionQuotaBytes,
		MediaMaxItemBytes:       limits.MediaMaxItemBytes,
		MediaSessionQuotaBytes:  limits.MediaSessionQuotaBytes,
		MediaMaxItemsPerSession: limits.MediaMaxItemsPerSession,
		MediaMaxLongSide:        limits.MediaMaxLongSide,
		WireBudgetTokens:        limits.WireBudgetTokens,
		WireSoftRatio:           limits.WireSoftRatio,
		WireTargetRatio:         limits.WireTargetRatio,
	}
}

func InitWorkspaceRepo(storePath string) (*workspace.Repo, error) {
	baseDir := filepath.Dir(storePath)
	repo, err := workspace.NewRepoWithStore(baseDir)
	if err != nil {
		return nil, fmt.Errorf("初始化工作区存储失败: %w", err)
	}
	return repo, nil
}

// InitEngine 按新装配模型创建主会话（session.NewSession）。
// EnginePort 的 ReactorEngine 接口由 *session.Session 直接满足。
func InitEngine(runtime *assemble.Runtime, hooks *application.ToolHookBridge, sessionID string) (*frameworkSession.Session, error) {
	sess, err := runtime.NewMainSessionWithID(sessionID, hooks.Hooks())
	if err != nil {
		return nil, fmt.Errorf("初始化主会话失败: %w", err)
	}
	return sess, nil
}

// toolCommandOutcomeObserver 把命令面的退出码折成一次工具记录（成功/失败），再把事件
// 原样转发给下游观察者（诊断日志）。
//
// 为什么挂在既有的诊断口上：工具层已经在 `bash.process.exited` 里上报了每一次同步命令的
// 退出码（同步主路径与 docker 重试同源于 executeScopedBash），命令面因此不必新增第二个
// 上报口；"退出码非零 = 失败"这一格判据落在 core 的 `RecordToolCommandOutcome`，
// 本函数只做搬运与转发。
//
// 边界：这条 stage 目前**只由同步 `bash` 链发出**（bash_bg 走 async_run 的另一条链，
// 不经 executeScopedBash）。将来若有第二个工具复用那条同步链，必须先把工具名带进事件
// （BashDiagnosticEvent.Tool），否则命令结果会被记到 bash 名下。
func toolCommandOutcomeObserver(next toolreg.BashDiagnosticObserver, app *application.Service) toolreg.BashDiagnosticObserver {
	return func(event toolreg.BashDiagnosticEvent) {
		if event.Stage == "bash.process.exited" {
			// 会话号由工具层带在事件上（BashDiagnosticEvent.Session，只填 ID 不带正文）：
			// 后台会话/teammate 跑的命令因此记在**它自己的**会话读数里，不会被归到视图会话。
			app.RecordToolCommandOutcome(event.Session, "bash", event.ExitCode)
		}
		if next != nil {
			next(event)
		}
	}
}

// sessionKeyFor 把会话 ID 解析成存储作用域键（项目 + 会话）：**按会话自己的项目绑定**解析，
// 未绑定会话返回 false（不落到别人的项目目录里）。装配期的三处（teamwork backend、
// 工具记录）共用这一份解析，不各写一遍。
func sessionKeyFor(workspaces *workspace.Repo, sessionID string) (store.Key, bool) {
	if workspaces == nil {
		return store.Key{}, false
	}
	bound, ok := workspaces.SessionWorkspace(sessionID)
	if !ok || strings.TrimSpace(bound.ID) == "" {
		return store.Key{}, false
	}
	return store.Key{ProjectID: bound.ID, SessionID: sessionID}, true
}

// sessionToolRecordStore 是 contract.ToolRecordSnapshotStore 在本宿主上的实现：
// 把读数写进**会话自己的**存储（`<sessionRoot>/tool-record.jsonl`，一行一批整份快照）。
//
// 未装配 JSON 后端（或键解析不出来）⇒ 空操作不报错：读数是派生读数，落不下去不该
// 让会话提交失败（错误由调用方按 LastError 的同一口径处理，这里选择"静默跳过"是因为
// 键解析失败通常意味着会话还没绑项目——那时也还没有可写的位置）。
type sessionToolRecordStore struct {
	router     *store.Router
	resolveKey func(sessionID string) (store.Key, bool)
}

func (st sessionToolRecordStore) SaveToolRecordSnapshot(sessionID string, snapshot application.ToolRecordSnapshot) error {
	repository, ok := st.repository()
	if !ok {
		return nil
	}
	key, ok := st.key(sessionID)
	if !ok {
		return nil
	}
	return repository.AppendToolRecordSnapshot(context.Background(), key, snapshot)
}

func (st sessionToolRecordStore) LoadToolRecordSnapshot(sessionID string) (application.ToolRecordSnapshot, bool, error) {
	repository, ok := st.repository()
	if !ok {
		return application.ToolRecordSnapshot{}, false, nil
	}
	key, ok := st.key(sessionID)
	if !ok {
		return application.ToolRecordSnapshot{}, false, nil
	}
	raw, found, err := repository.ReadToolRecordSnapshot(context.Background(), key)
	if err != nil || !found {
		return application.ToolRecordSnapshot{}, false, err
	}
	var snapshot application.ToolRecordSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return application.ToolRecordSnapshot{}, false, nil // 坏行 = 当没记过（不阻塞读面）
	}
	return snapshot, true, nil
}

func (st sessionToolRecordStore) repository() (store.ToolRecordRepository, bool) {
	if st.router == nil {
		return nil, false
	}
	return st.router.ToolRecordFor()
}

func (st sessionToolRecordStore) key(sessionID string) (store.Key, bool) {
	if st.resolveKey == nil {
		return store.Key{}, false
	}
	return st.resolveKey(sessionID)
}
