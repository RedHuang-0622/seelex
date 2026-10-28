package integration

import (
	"context"
	assembly "github.com/RedHuang-0622/seelex/application/assembly"
	applicationexecution "github.com/RedHuang-0622/seelex/application/execution"
	"github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"
	applicationreview "github.com/RedHuang-0622/seelex/application/review"
	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	assemble "github.com/RedHuang-0622/seelex/backend/engine/assemble"
	inputrouter "github.com/RedHuang-0622/seelex/backend/engine/input"
	limitapproval "github.com/RedHuang-0622/seelex/backend/limit/approval"
	"github.com/RedHuang-0622/seelex/backend/multiagent/teamwork"
	reviewtool "github.com/RedHuang-0622/seelex/backend/tool/review"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/internal/host"
	"github.com/RedHuang-0622/seelex/kernel/event"
	worktable "github.com/RedHuang-0622/seelex/kernel/worktable"
	middlehistory "github.com/RedHuang-0622/seelex/middle/switch/history"
	"log"
	"os"
	"sync"
	"testing"
	"time"
)

// testServiceOption 定制 newTestService 的依赖注入。
type testServiceOption func(*contract.Dependencies)

type recordingLocker struct {
	mu     sync.Mutex
	calls  []string
	locked int
}

func (locker *recordingLocker) Lock() {
	locker.mu.Lock()
	locker.calls = append(locker.calls, "lock")
	locker.locked++
}

func (locker *recordingLocker) Unlock() {
	locker.calls = append(locker.calls, "unlock")
	locker.locked--
	locker.mu.Unlock()
}

func withSessionID(ctx context.Context, sessionID string) context.Context {
	return task_context.WithSessionID(ctx, sessionID)
}

func sessionIDFromContext(ctx context.Context) string {
	return task_context.SessionIDFromContext(ctx)
}

func ApplyLimits(limits applicationpolicy.Limits) {
	applicationpolicy.Apply(limits)
}

func Limits() applicationpolicy.Limits {
	return applicationpolicy.Get()
}

func workTableBounds() worktable.Limits {
	values := applicationpolicy.Get()
	return worktable.Limits{
		WorkTableRows:  values.WorkTableRows,
		EvidenceChars:  values.EvidenceChars,
		PlanNodeEvents: values.PlanNodeEvents,
	}
}

func withTestSessions(sessions contract.SessionPort) testServiceOption {
	return func(deps *contract.Dependencies) { deps.Sessions = sessions }
}

func withTestRuntime(runtime contract.RuntimePort) testServiceOption {
	return func(deps *contract.Dependencies) { deps.Runtime = runtime }
}

func testResidencyPort() applicationsession.ResidencyPort {
	return host.NewResidencyPort()
}

// withTestReleaseWindowStore 注入「会话驻留窗口」的落盘口（不注入 = 界面上改的值
// 只在本次运行有效）。
func withTestReleaseWindowStore(store contract.ReleaseWindowStore) testServiceOption {
	return func(deps *contract.Dependencies) { deps.ReleaseWindowStore = store }
}

// withTestAccountWindowStore 注入「每账号上下文窗口」的落盘口（不注入 = 界面上改的值
// 只在本次运行有效）。
func withTestAccountWindowStore(store contract.AccountWindowStore) testServiceOption {
	return func(deps *contract.Dependencies) { deps.AccountWindowStore = store }
}

// withTestPlugins 注入自定义插件端口（默认桩只给一个不含来源的 default）。来源面
// （source_kind/source_url/source_root）要落到会话快照，就得从**真实**的
// host.PluginPort 走一遍，所以这个口子必须存在。
func withTestPlugins(plugins contract.PluginPort) testServiceOption {
	return func(deps *contract.Dependencies) { deps.Plugins = plugins }
}

// newTestService 构造一个带默认 fakes 的 Service（测试夹具），并在测试
// 结束时自动 Shutdown（catalog worker 随 teardown 停止）。
func newTestService(t testing.TB, engine contract.ChatEngine, options ...testServiceOption) *assembly.Service {
	t.Helper()
	deps := contract.Dependencies{
		Engine:   engine,
		Runtime:  &fakeRuntime{},
		Plugins:  &fakePlugins{current: model.PluginInfo{Name: "default"}},
		Skills:   fakeSkills{},
		Sessions: &fakeSessions{},
	}
	for _, option := range options {
		option(&deps)
	}
	service := mustNew(t, deps)
	t.Cleanup(func() {
		assembly.ComponentsFor(service).Runtime.Lifecycle.Shutdown()
	})
	return service
}

func debugLog(format string, args ...any) {
	if os.Getenv("SEELEX_TEST_DEBUG") == "1" {
		log.Printf("[integration-debug] "+format, args...)
	}
}

// testExternalPorts 是集成夹具自己的端口表：组合根的表（boot.ServicePorts）
// 还会附带 Goal 协调器，测试要保持迁移前的行为，所以直接向真实 owner 构造。
func testExternalPorts() assembly.ExternalPorts {
	return assembly.ExternalPorts{
		HistoryWindow:      middlehistory.Assembler{},
		Residency:          host.NewResidencyPort(),
		ConversationStream: host.NewConversationStreamPort(),
		AccountWindow:      host.NewAccountWindowPolicy(),
		SkillActivation:    host.NewSkillActivationPort(),
		SessionRecords:     host.NewSessionRecordsPort(),
		ErrorPresentation:  host.NewErrorPresentationPort(),
		ApprovalFactory: func(events event.Hub) contract.ApprovalBroker {
			return limitapproval.NewApprovalBroker(events)
		},
		CommandRegistry: func() contract.CommandRegistryPort {
			return inputrouter.NewCommandRegistry()
		},
		InputRouter: func(handlers contract.InputRouteHandlers) contract.InputDispatcher {
			return inputrouter.NewRouter(inputrouter.RouteHandlers{
				Command:      handlers.Command,
				Plugin:       handlers.Plugin,
				Skill:        handlers.Skill,
				Conversation: handlers.Conversation,
			})
		},
		RoleSessions: host.NewRoleSessionsFactory(),
		TeamworkBoardReader: func(runtime contract.RuntimePort) applicationexecution.BoardReader {
			return teamwork.NewBoardReader(runtime)
		},
		HostSessionForToolCall: assemble.HostSessionForToolCall,
		ReviewProcessor: func(owner applicationreview.Host) applicationreview.RequestProcessor {
			return reviewtool.NewRequests(owner)
		},
	}
}

func mustNew(t testing.TB, deps contract.Dependencies) *assembly.Service {
	t.Helper()
	service, err := assembly.New(deps, testExternalPorts())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return service
}

func componentsOf(service any) assembly.Components {
	switch value := service.(type) {
	case *assembly.Service:
		return assembly.ComponentsFor(value)
	case *legacyTestService:
		return assembly.ComponentsFor(value.Service)
	default:
		return assembly.Components{}
	}
}

// waitForSnapshot 轮询 Snapshot 直到 ready 条件满足，返回满足条件的快照。
func waitForSnapshot(t *testing.T, service any, ready func(model.Snapshot) bool) model.Snapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := componentsOf(service).Session.Snapshot.Snapshot()
		if ready(snapshot) {
			return snapshot
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("snapshot condition not met within deadline")
	return model.Snapshot{}
}

// sessionBackedEngine 模拟生产 session-backed 引擎（OnIterationComplete 在
// Session 锁内执行，不可重入历史操作）。
type sessionBackedEngine struct {
	*fakeEngine
}

func (*sessionBackedEngine) SessionBacked() bool { return true }

// gracefulShutdownEngine 是优雅关闭用例的引擎桩：第一次 ChatStream 阻塞到
// releaseFirst，第二次阻塞到 releaseSecond，用于验证排队输入在 graceful
// shutdown 下仍被消费。
type gracefulShutdownEngine struct {
	*fakeEngine
	mu            sync.Mutex
	calls         int
	firstStarted  chan struct{}
	secondStarted chan struct{}
	releaseFirst  chan struct{}
	releaseSecond chan struct{}
	firstOnce     sync.Once
	secondOnce    sync.Once
}

func newGracefulShutdownEngine() *gracefulShutdownEngine {
	return &gracefulShutdownEngine{
		fakeEngine:    &fakeEngine{},
		firstStarted:  make(chan struct{}),
		secondStarted: make(chan struct{}),
		releaseFirst:  make(chan struct{}),
		releaseSecond: make(chan struct{}),
	}
}

func (engine *gracefulShutdownEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error) {
	engine.mu.Lock()
	engine.calls++
	call := engine.calls
	engine.mu.Unlock()
	if call == 1 {
		engine.firstOnce.Do(func() { close(engine.firstStarted) })
		select {
		case <-engine.releaseFirst:
			return "first", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	engine.secondOnce.Do(func() { close(engine.secondStarted) })
	select {
	case <-engine.releaseSecond:
		return "second", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// ChatStreamFor 显式转发到自身 ChatStream（覆盖内嵌 fakeEngine 的提升方法，
// 保证会话路由面下优雅关闭/消费语义仍生效）。
func (engine *gracefulShutdownEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error) {
	return engine.ChatStream(ctx, input, onChunk)
}

// waitForChatCompletion 轮询直到当前 chat 结束。
func waitForChatCompletion(t *testing.T, service any) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !componentsOf(service).Session.Snapshot.Snapshot().Chat.Running {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("chat did not complete")
}
