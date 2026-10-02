package adapters

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	frameworkSession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/application/contract"
)

// 本文件钉住 S3b 的判据：**压缩路径上任何一次 port.mu 持有，都不得跨越一次可能
// 阻塞的 Session.mu 等待**。
//
// 修前的三处引信（各对应一条用例）：
//   - replaceRawHistoryFor 非活跃分支：持 port.mu.Lock() 直接对目标引擎
//     ClearHistory + AppendHistory → 目标恰好在飞时堵在别人的会话锁上；
//   - ReplaceRawHistory 与活跃分支：engineCalls>0 时仍就地改 port.engine 的历史
//     → 同一把锁、同一个堵法；
//   - RawHistoryFor：port.mu.RLock() 内调 engine.History() → 持 RLock 等会话锁，
//     Go 的 RWMutex 有写者排队后连新读者也停。
//
// 同上口径的第四处（S3b 之后才收）：活跃别名读面 History / RawHistory —— 它要等的
// 是同一把 Session.mu，触发者却是用户按得到的 `/history` 与工作区切换。
//
// 三条的后果都不是"这一次调用慢"，而是**全进程所有会话**的开回合排在 port.mu 后面
// （ChatStreamFor 自己要 Lock 才能开回合）。所以每条用例同时断言两件事：压缩自己当场
// 返回，且**别的会话照样开得动回合**。
//
// 判据用真实 frameworkSession.Session：Session.mu 的持锁范围只在它身上存在，替身
// 引擎复现不了（同 engine_port_reentrance_test.go 的口径）。

const (
	// fuseOtherSession 是端口注册表里那台空闲会话的键（工厂造的引擎自带随机号，
	// 注册键由端口决定，所以固定键即可）。挂死那台的键就是它自己的 SessionID()。
	fuseOtherSession = "sess-other"
	// fuseQuickBudget：登记压缩、跑一轮空闲会话都该是毫秒级；2s 已留出真实 Session
	// 装配与 CI 抖动的余量，远低于"一整轮挂死的回合"。
	fuseQuickBudget = 2 * time.Second
)

// hangAgent 在第一次工具派发里等到测试放行。ChatStream 从进函数持会话锁到出函数，
// 因此 entered 关闭之后、release 关闭之前，这台 Session 的会话锁一定被别人持着。
type hangAgent struct {
	llm      types.ChatCompleter
	toolName string
	entered  chan struct{}
	release  chan struct{}
	once     sync.Once
}

func (a *hangAgent) VisibleTools(context.Context) []types.Tool {
	return []types.Tool{{Type: "function", Function: types.ToolFunction{
		Name: a.toolName, Description: "hang", Parameters: map[string]any{"type": "object"},
	}}}
}

func (a *hangAgent) Dispatch(_ context.Context, name, _ string) (string, error) {
	if name != a.toolName {
		return "", nil
	}
	a.once.Do(func() { close(a.entered) })
	<-a.release
	return "released", nil
}

func (a *hangAgent) LLM() types.ChatCompleter { return a.llm }

// quietCompleter 一次调用就出正文，不开工具：用来证明"这个会话还跑得动回合"。
type quietCompleter struct{}

func (quietCompleter) Complete(_ context.Context, _ []types.Message, _ []types.Tool) (types.Message, error) {
	text := "quiet-ok"
	return types.Message{Role: "assistant", Content: &text}, nil
}

func (quietCompleter) CompleteStream(_ context.Context, _ []types.Message, _ []types.Tool, _ func(string)) (string, string, []types.ToolCall, error) {
	return "quiet-ok", "", nil, nil
}

func (q quietCompleter) CompleteStreamEvents(ctx context.Context, messages []types.Message, tools []types.Tool, _ func(types.StreamEvent)) (string, string, []types.ToolCall, error) {
	return q.CompleteStream(ctx, messages, tools, nil)
}

type quietAgent struct{}

func (quietAgent) VisibleTools(context.Context) []types.Tool { return nil }
func (quietAgent) Dispatch(context.Context, string, string) (string, error) {
	return "", nil
}
func (quietAgent) LLM() types.ChatCompleter { return quietCompleter{} }

// fuseHarness 组一个真实双会话端口：一台回合挂死（会话锁整轮不放），一台空闲。
// 工厂只造空闲引擎——安装登记历史时它会被调到，绝不能把挂死那台再造一遍。
type fuseHarness struct {
	port      *EnginePort
	hangKey   string
	entered   chan struct{}
	release   chan struct{}
	released  sync.Once
	prepared  chan string
	failed    chan error
	turnsDone chan error
	folded    []contract.EngineMessage
}

// newFuseHarness 装配端口。hangActive=true 时把挂死那台设为活跃会话（钉活跃分支），
// false 时活跃会话是空闲那台（钉非活跃分支）。
func newFuseHarness(t *testing.T, hangActive bool) *fuseHarness {
	t.Helper()
	entered := make(chan struct{})
	release := make(chan struct{})
	completer := &scriptedCompleter{toolName: "compact_context"}
	hanging, err := frameworkSession.NewSession(frameworkSession.SessionComponents{
		Agent: &hangAgent{
			llm: completer, toolName: "compact_context",
			entered: entered, release: release,
		},
	})
	if err != nil {
		t.Fatalf("NewSession(hang): %v", err)
	}
	harness := &fuseHarness{
		hangKey:   hanging.SessionID(),
		entered:   entered,
		release:   release,
		prepared:  make(chan string, 8),
		failed:    make(chan error, 4),
		turnsDone: make(chan error, 4),
		folded:    compactionFrame("S3B-FOLDED-FRAME"),
	}
	harness.port = NewEnginePort(hanging, func(string) ReactorEngine {
		fresh, createErr := frameworkSession.NewSession(frameworkSession.SessionComponents{Agent: quietAgent{}})
		if createErr != nil {
			harness.failed <- createErr
			return nil
		}
		return fresh
	}, nil)
	harness.port.ApplyDeps(EnginePortDeps{PrepareHistory: func(sessionID string, _ []types.Message) {
		select {
		case harness.prepared <- sessionID:
		default:
		}
	}})
	if !harness.port.SessionBacked() {
		t.Fatalf("期望 session-backed 端口（生产装配），拿到的是替身")
	}
	if err := harness.port.ActivateSession(fuseOtherSession); err != nil {
		t.Fatalf("ActivateSession(other): %v", err)
	}
	if hangActive {
		if err := harness.port.ActivateSession(harness.hangKey); err != nil {
			t.Fatalf("ActivateSession(hang): %v", err)
		}
	}
	t.Cleanup(func() { harness.releaseHangNow() })
	return harness
}

// startHangingTurn 起回合并等到工具派发点：此刻那台 Session 的会话锁一定被持着。
func (h *fuseHarness) startHangingTurn(t *testing.T) {
	t.Helper()
	go func() {
		_, err := h.port.ChatStreamFor(h.hangKey, context.Background(), "go", nil)
		h.turnsDone <- err
	}()
	select {
	case <-h.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("挂死回合没跑到工具派发点（装配有问题，本用例判据不成立）")
	}
}

// runQuietTurn 让空闲会话跑一整轮，返回耗时；调用方用它证明 port.mu 没被冻住。
func (h *fuseHarness) runQuietTurn(t *testing.T) time.Duration {
	t.Helper()
	started := time.Now()
	done := make(chan error, 1)
	go func() {
		_, err := h.port.ChatStreamFor(fuseOtherSession, context.Background(), "hello", nil)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("空闲会话开回合失败：%v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("空闲会话的开回合被拖住（port.mu 被人持着等会话锁 = 全进程冻结）")
	}
	return time.Since(started)
}

// wait 在 fuseQuickBudget 内跑完 fn；超时即判定「持着 port.mu 等别人的会话锁」。
func (h *fuseHarness) wait(t *testing.T, desc string, fn func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%s：%v", desc, err)
		}
	case <-time.After(fuseQuickBudget):
		t.Fatalf("%s 在 %s 内没返回（= 持着 port.mu 等别人的会话锁）", desc, fuseQuickBudget)
	}
}

func (h *fuseHarness) releaseHangNow() {
	h.released.Do(func() { close(h.release) })
}

// releaseHang 放行挂死的回合并等它收尾。
func (h *fuseHarness) releaseHang(t *testing.T) {
	t.Helper()
	h.releaseHangNow()
	select {
	case err := <-h.turnsDone:
		if err != nil {
			t.Fatalf("挂死回合收尾失败：%v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("挂死回合收尾超时")
	}
}

func (h *fuseHarness) assertNoFactoryFailure(t *testing.T) {
	t.Helper()
	select {
	case err := <-h.failed:
		t.Fatalf("引擎工厂造不出会话：%v", err)
	default:
	}
}

func compactionFrame(content string) []contract.EngineMessage {
	return []contract.EngineMessage{{Role: "user", Content: content, ContentSet: true}}
}

func historyText(history []types.Message) string {
	text := ""
	for _, message := range history {
		if message.Content != nil {
			text += *message.Content
		}
	}
	return text
}

// compactedWithInFlightTail 组装一次"保留在飞单元"的压缩产物：压缩帧 + 当前历史里
// assistant 带 tool_calls 的那一截。尾部必须保留——引擎会拒收丢掉它的替换
// （ErrInFlightToolCallDropped），而压缩产物本来就不含这一截。
func compactedWithInFlightTail(port *EnginePort, sessionID, frame string) []contract.EngineMessage {
	folded := compactionFrame(frame)
	for _, message := range port.HistoryFor(sessionID) {
		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			folded = append(folded, message)
		}
	}
	return folded
}

// TestCompactionOnBusySessionLandsInTurnAndDoesNotFreezePort 钉写面非活跃分支：目标会话
// 有回合在飞时，锁外压缩**当场把替换交给引擎排队**（不换引擎、不等回合收尾），其它
// 会话的开回合不受影响；替换在该回合的下一个检查点落地，那条在飞 tool 结果不会成孤儿。
//
// 旧实现在这条路径上只能"登记待安装 + 等回合收尾换一台干净引擎"：Seele 让回合忙时
// 的替换排队到检查点之后，宿主不再需要那套登记表（见 EnginePort.queueSessionHistory）。
func TestCompactionOnBusySessionLandsInTurnAndDoesNotFreezePort(t *testing.T) {
	harness := newFuseHarness(t, false)
	harness.startHangingTurn(t)

	harness.wait(t, "对正在跑回合的会话做锁外压缩", func() error {
		return harness.port.ReplaceHistoryFor(harness.hangKey,
			compactedWithInFlightTail(harness.port, harness.hangKey, "S3B-FOLDED-FRAME"))
	})
	harness.runQuietTurn(t)

	harness.releaseHang(t)
	history := historyText(harness.port.RawHistoryFor(harness.hangKey))
	if !strings.Contains(history, "S3B-FOLDED-FRAME") {
		t.Fatalf("在飞会话的压缩没落地：history=%q", history)
	}
	if !strings.Contains(history, "released") {
		t.Fatalf("压缩把刚 append 的 tool 结果抹掉了（孤儿/丢失）：history=%q", history)
	}
	if harness.port.SessionID() != fuseOtherSession {
		t.Fatalf("后台会话的压缩把活跃会话切走了：%s", harness.port.SessionID())
	}
	harness.assertNoFactoryFailure(t)
}

// TestCompactionOnBusyActiveSessionDoesNotFreezePort 钉写面活跃分支：活跃别名上的压缩与
// 会话内压缩同路（排队到检查点），不换引擎、不影响其它会话。
func TestCompactionOnBusyActiveSessionDoesNotFreezePort(t *testing.T) {
	harness := newFuseHarness(t, true)
	if harness.port.SessionID() != harness.hangKey {
		t.Fatalf("用例前提不成立：活跃会话应为挂死那台，实际 %s", harness.port.SessionID())
	}
	harness.startHangingTurn(t)

	harness.wait(t, "对活跃的在飞会话做压缩", func() error {
		return harness.port.ReplaceHistoryFor(harness.hangKey,
			compactedWithInFlightTail(harness.port, harness.hangKey, "S3B-FOLDED-FRAME"))
	})
	harness.runQuietTurn(t)

	harness.releaseHang(t)
	history := historyText(harness.port.RawHistoryFor(harness.hangKey))
	if !strings.Contains(history, "S3B-FOLDED-FRAME") {
		t.Fatalf("活跃会话的压缩没落地：history=%q", history)
	}
	if !strings.Contains(history, "released") {
		t.Fatalf("压缩把刚 append 的 tool 结果抹掉了（孤儿/丢失）：history=%q", history)
	}
	harness.assertNoFactoryFailure(t)
}

// TestHistoryReadOfBusySessionIsNonBlocking 钉读面：读一台在飞会话的历史**立刻**
// 返回当前工作历史（Seele 的工作状态短临界区），既能读到在飞状态，也不会把别的会话
// 堵在 port.mu 后面。旧模型下这条读会等整轮不放的会话锁，且持着全进程读锁等，
// 一次长流式就能让界面和所有会话一起冻住。
func TestHistoryReadOfBusySessionIsNonBlocking(t *testing.T) {
	harness := newFuseHarness(t, false)
	harness.startHangingTurn(t)

	// 非活跃别名：RawHistoryFor。
	read := make(chan []types.Message, 1)
	go func() { read <- harness.port.RawHistoryFor(harness.hangKey) }()
	select {
	case messages := <-read:
		if len(messages) == 0 {
			t.Fatal("在飞会话的历史读返回了空历史")
		}
	case <-time.After(fuseQuickBudget):
		t.Fatal("在飞会话的历史读没有立刻返回（读面又跨了整轮等待）")
	}

	// 判据：读期间 / 读之后 port.mu 都空着——别会话照样开得动回合。
	harness.runQuietTurn(t)
	if got := historyText(harness.port.RawHistoryFor(fuseOtherSession)); got == "" {
		t.Fatal("空闲会话的历史读不到（读面把 port.mu 的状态弄坏了）")
	}
	harness.releaseHang(t)
	harness.assertNoFactoryFailure(t)
}

// TestActiveHistoryReadOfBusySessionIsNonBlocking 钉活跃别名读面（History /
// RawHistory）：它和 RawHistoryFor 是同一份工作历史，触发者却是**用户在回合跑着时
// 按得到的** `/history` 命令与工作区切换——必须同样立刻返回。
func TestActiveHistoryReadOfBusySessionIsNonBlocking(t *testing.T) {
	harness := newFuseHarness(t, true)
	if harness.port.SessionID() != harness.hangKey {
		t.Fatalf("用例前提不成立：活跃会话应为挂死那台，实际 %s", harness.port.SessionID())
	}
	harness.startHangingTurn(t)

	read := make(chan []contract.EngineMessage, 1)
	go func() { read <- harness.port.History() }()
	select {
	case messages := <-read:
		if len(messages) == 0 {
			t.Fatal("在飞活跃会话的历史读返回了空历史")
		}
	case <-time.After(fuseQuickBudget):
		t.Fatal("在飞活跃会话的历史读没有立刻返回")
	}

	harness.runQuietTurn(t)
	harness.releaseHang(t)
	harness.assertNoFactoryFailure(t)
}

// TestCompactionsAreKeyedBySession 钉住"压缩各归各的会话"：一台在飞、一台空闲时，两次压缩
// 分别落在自己的会话上，谁也不会挤掉谁（旧实现是单槽登记 + 换引擎，legacy ChatStream
// 退出点还会把别人那份装到自己头上）。
func TestCompactionsAreKeyedBySession(t *testing.T) {
	harness := newFuseHarness(t, false)

	harness.wait(t, "对空闲会话压缩", func() error {
		return harness.port.ReplaceHistoryFor(fuseOtherSession, compactionFrame("S3B-OTHER-FRAME"))
	})
	harness.startHangingTurn(t)
	harness.wait(t, "对挂死会话压缩", func() error {
		return harness.port.ReplaceHistoryFor(harness.hangKey,
			compactedWithInFlightTail(harness.port, harness.hangKey, "S3B-FOLDED-FRAME"))
	})
	if got := historyText(harness.port.RawHistoryFor(fuseOtherSession)); got != "S3B-OTHER-FRAME" {
		t.Fatalf("空闲会话已装的压缩被另一会话挤掉了：history=%q", got)
	}

	harness.releaseHang(t)
	if got := historyText(harness.port.RawHistoryFor(harness.hangKey)); !strings.Contains(got, "S3B-FOLDED-FRAME") {
		t.Fatalf("挂死会话的压缩没按自己的键落地：history=%q", got)
	}
	if got := historyText(harness.port.RawHistoryFor(fuseOtherSession)); got != "S3B-OTHER-FRAME" {
		t.Fatalf("另一会话的压缩被串改：history=%q", got)
	}
	harness.assertNoFactoryFailure(t)
}
