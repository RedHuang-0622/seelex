package adapters

import (
	"testing"

	seeleTypes "github.com/RedHuang-0622/Seele/types"
)

// engine_port_session_loops_test.go —— 定时任务那条"按会话钉循环上限"的链路。
//
// 这一格为什么必须住在引擎端口上（而不是 application 去改进程默认值）：进程级那一格
// （EngineBase.SetMaxLoops，composer"循环次数"拉条的读数）是**所有会话共用的默认值**，
// 定时任务声明的次数只属于它触发的那一个会话。端口按会话持有引擎，"这个会话跑多少轮"
// 因此落在这里；application 侧只经 contract.SessionLoopLimitPort 说一句话。

// loopRecordingEngine 记录被写进来的循环上限（fakeReactorEngine 的 SetMaxLoops 是空
// 实现，按会话那一格需要看得见落点）。
type loopRecordingEngine struct {
	fakeReactorEngine
	loops int
}

func (engine *loopRecordingEngine) SetMaxLoops(loops int) { engine.loops = loops }

// loopPortHarness 造一个按会话建引擎的端口，并记下每个会话拿到的引擎。
func loopPortHarness() (*EnginePort, map[string]*loopRecordingEngine) {
	created := make(map[string]*loopRecordingEngine)
	port := NewEnginePort(nil, func(sessionID string) ReactorEngine {
		engine := &loopRecordingEngine{}
		engine.sessionID = sessionID
		created[sessionID] = engine
		return engine
	}, nil)
	return port, created
}

// resumeMessages 造一次历史替换的载荷（按会话重建引擎走的那条路）。
func resumeMessages() []seeleTypes.Message {
	content := "resume"
	return []seeleTypes.Message{{Role: "user", Content: &content}}
}

// TestEnginePortPinsLoopsPerSession 钉住按会话的那一格真的落到那个会话的引擎上：
// 钉住的会话拿钉值，别的会话拿进程默认值（拉条读数），两者互不串味。
func TestEnginePortPinsLoopsPerSession(t *testing.T) {
	port, created := loopPortHarness()
	// 进程级拉条已经被拖过一次（进程默认值 600）。
	port.SetMaxLoops(600)

	// 定时任务给 sched-a 钉 48：它自己的引擎拿 48。
	port.SetSessionMaxLoopsFor("sched-a", 48)
	if err := port.ActivateSession("sched-a"); err != nil {
		t.Fatalf("ActivateSession(sched-a): %v", err)
	}
	if got := created["sched-a"].loops; got != 48 {
		t.Fatalf("钉住的会话引擎循环上限 = %d, want 48", got)
	}

	// 没钉的会话照旧拿进程默认值。
	if err := port.ActivateSession("plain-b"); err != nil {
		t.Fatalf("ActivateSession(plain-b): %v", err)
	}
	if got := created["plain-b"].loops; got != 600 {
		t.Fatalf("普通会话引擎循环上限 = %d, want 600（进程默认值）", got)
	}

	// 已钉会话的引擎**重建**（历史替换/冷装载走同一台新引擎）之后仍然按钉值跑。
	if err := port.ReplaceRawHistory("sched-a", resumeMessages()); err != nil {
		t.Fatalf("ReplaceRawHistory: %v", err)
	}
	if got := created["sched-a"].loops; got != 48 {
		t.Fatalf("重建后的引擎循环上限 = %d, want 48（钉值随会话走）", got)
	}
}

// TestEnginePortSessionLoopPinLifecycle 钉住这一格的两条退场路径：
//   - 用户在会话里主动改循环次数 = 对该会话的显式选择，撤掉钉值（否则那次选择会在
//     引擎重建时被钉值盖掉——读数与行为相反）；
//   - 会话引擎卸载（LRU 驱逐/卸载）时连钉值一起清掉：端口对已卸载的会话不留状态。
func TestEnginePortSessionLoopPinLifecycle(t *testing.T) {
	port, created := loopPortHarness()
	port.SetMaxLoops(600)
	port.SetSessionMaxLoopsFor("sched-a", 48)
	if err := port.ActivateSession("sched-a"); err != nil {
		t.Fatalf("ActivateSession(sched-a): %v", err)
	}

	// 用户在这个会话里把循环次数改成 300：活跃引擎立即跟着改，钉值被撤掉。
	port.SetMaxLoops(300)
	if got := created["sched-a"].loops; got != 300 {
		t.Fatalf("显式改循环次数没有落到活跃引擎：%d, want 300", got)
	}
	if err := port.ReplaceRawHistory("sched-a", resumeMessages()); err != nil {
		t.Fatalf("ReplaceRawHistory: %v", err)
	}
	if got := created["sched-a"].loops; got != 300 {
		t.Fatalf("重建后循环上限 = %d, want 300（用户的显式选择，不是被撤掉的钉值 48）", got)
	}

	// 卸载会话：钉值随引擎一起离场（重开的会话按进程默认值跑）。
	port.SetSessionMaxLoopsFor("sched-c", 48)
	if err := port.ActivateSession("sched-c"); err != nil {
		t.Fatalf("ActivateSession(sched-c): %v", err)
	}
	if err := port.UnloadSession("sched-c"); err != nil {
		t.Fatalf("UnloadSession: %v", err)
	}
	if err := port.ActivateSession("sched-c"); err != nil {
		t.Fatalf("ActivateSession(sched-c) again: %v", err)
	}
	if got := created["sched-c"].loops; got != 300 {
		t.Fatalf("卸载重开后的循环上限 = %d, want 300（进程默认值，钉值已随卸载清掉）", got)
	}

	// 撤掉钉值（loops <= 0）= 回到进程默认值，之后重建不再按钉值。
	port.SetSessionMaxLoopsFor("sched-d", 48)
	if err := port.ActivateSession("sched-d"); err != nil {
		t.Fatalf("ActivateSession(sched-d): %v", err)
	}
	port.SetSessionMaxLoopsFor("sched-d", 0)
	if err := port.ReplaceRawHistory("sched-d", resumeMessages()); err != nil {
		t.Fatalf("ReplaceRawHistory: %v", err)
	}
	if got := created["sched-d"].loops; got != 300 {
		t.Fatalf("撤掉钉值后重建的循环上限 = %d, want 300（进程默认值）", got)
	}
}
