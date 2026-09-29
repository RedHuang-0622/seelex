package adapters

import (
	"strings"
	"sync"
	"testing"
	"time"

	frameworkSession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"
)

// 本文件钉住 S3b 的第五条引信：**宿主注入实现（PrepareHistory）不得在 port.mu 内被调**。
//
// 修前的形状：replaceRawHistoryFor / installSessionEngineLocked / ResumeRawSession 三条
// 路径都在持 port.mu 时直接调 port.prepareHistory。生产装配里那是
// Runtime.PrepareMainSessionHistory → sessionBindings.mu → binding.mu →
// DurableHistory.PrepareNextLoad，即「持全进程端口锁调宿主实现」。后果有两层：
//
//   - 慢活计进全局锁：每次历史替换（折叠/恢复）都在 port.mu 内走一遍宿主侧链路，期间所有
//     会话的查表、开回合、读历史全排在这一把锁后面；
//   - 自锁窗口：宿主实现只要回读端口（本文件的用例就是拿 HasSession 模拟这一步），持锁同步
//     重入非重入锁即永久挂死——与本仓 2026-09-23 / 2026-09-29 那两起事故同形。
//
// 修法：锁内只改注册表并**登记**一次交接（armHandoffLocked），解锁之后再交给宿主
// （runHandoff）。锁内决定与锁外调用之间必然有窗口，所以交接带序号闸：本次决定已被更晚
// 的一次取代时，旧历史不再交给宿主（否则把 durable 的 next-load 拉回过期状态）。

// recordingPrepare 记录宿主收到的每一次历史交接（顺序敏感）。
type recordingPrepare struct {
	mu      sync.Mutex
	entries []prepareEntry
}

type prepareEntry struct {
	sessionID string
	history   string
}

func (recorder *recordingPrepare) record(sessionID string, history []types.Message) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.entries = append(recorder.entries, prepareEntry{sessionID: sessionID, history: historyText(history)})
}

func (recorder *recordingPrepare) snapshot() []prepareEntry {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return append([]prepareEntry(nil), recorder.entries...)
}

// newQuietPort 起一个会话后端口 + 安静引擎工厂（历史替换/恢复都走立即安装路径）。
func newQuietPort(t *testing.T) *EnginePort {
	t.Helper()
	main, err := frameworkSession.NewSession(frameworkSession.SessionComponents{Agent: quietAgent{}})
	if err != nil {
		t.Fatalf("NewSession(main): %v", err)
	}
	return NewEnginePort(main, func(string) ReactorEngine {
		fresh, createErr := frameworkSession.NewSession(frameworkSession.SessionComponents{Agent: quietAgent{}})
		if createErr != nil {
			t.Fatalf("NewSession(factory): %v", createErr)
		}
		return fresh
	}, nil)
}

func frameHistory(content string) []types.Message {
	return []types.Message{messageWithContent(content)}
}

// TestPrepareHistoryIsCalledOutsidePortLock 钉住判据本体：宿主实现在交接期间回读端口
// （HasSession 走 port.mu.RLock）必须立刻返回。修前这条调用发生在 port.mu.Lock() 之内，
// 同步重入同一把非重入锁——本用例会在预算内挂死（而不是"慢一点"）。
func TestPrepareHistoryIsCalledOutsidePortLock(t *testing.T) {
	harness := newFuseHarness(t, false)
	handedOff := make(chan struct{})
	harness.port.ApplyDeps(EnginePortDeps{PrepareHistory: func(sessionID string, history []types.Message) {
		// 宿主回读端口：生产实现走的是 bundlesMu/binding.mu，这里是同一类"调用期间还要
		// 向上取端口状态"的一步。锁内调它就是自锁，锁外调它才正常返回。
		if !harness.port.HasSession(sessionID) {
			t.Errorf("交接时端口查不到会话 %s（历史=%q）", sessionID, historyText(history))
		}
		select {
		case <-handedOff:
		default:
			close(handedOff)
		}
	}})

	harness.wait(t, "对空闲会话做替换（宿主回读端口）", func() error {
		return harness.port.ReplaceRawHistoryFor(fuseOtherSession, frameHistory("S3B-HANDOFF"))
	})

	select {
	case <-handedOff:
	default:
		t.Fatal("宿主没收到这次交接（换了引擎却没 arm durable 的 next-load 槽）")
	}
	harness.assertNoFactoryFailure(t)
}

// TestSupersededHandoffIsDropped 钉住序号闸：两次决定并发时，先决定的那个可能后交出去。
// 过期的那一份必须被丢弃——旧代码在锁内交接，天然按锁序串行；挪到锁外之后这条顺序不是
// 白来的，得有判据。
func TestSupersededHandoffIsDropped(t *testing.T) {
	recorder := &recordingPrepare{}
	port := NewEnginePort(nil, nil, nil)
	port.ApplyDeps(EnginePortDeps{PrepareHistory: recorder.record})

	port.mu.Lock()
	first := port.armHandoffLocked(fuseOtherSession, frameHistory("FIRST"))
	second := port.armHandoffLocked(fuseOtherSession, frameHistory("SECOND"))
	port.mu.Unlock()
	if !first.armed || !second.armed {
		t.Fatalf("交接没被登记：first=%+v second=%+v", first, second)
	}

	// 晚决定的那次先兑现（真实现里就是解锁与宿主调用之间那个窗口），先决定的随后才交。
	port.runHandoff(second)
	port.runHandoff(first)

	got := recorder.snapshot()
	if len(got) != 1 || got[0].history != "SECOND" {
		t.Fatalf("过期交接没被丢弃（durable 会被拉回旧历史）：%+v", got)
	}
}

// TestHandoffReachesHostOnEveryInstallPath 反向护栏：三条安装路径各自**恰好**兑现一次交接。
// 把宿主调用搬出 port.mu 时最容易犯的错是搬丢了（引擎换了、durable 的 next-load 槽没 arm）。
func TestHandoffReachesHostOnEveryInstallPath(t *testing.T) {
	const sessionID = "sess-handoff"
	cases := []struct {
		name string
		run  func(t *testing.T, port *EnginePort)
	}{
		{"replace-inactive", func(t *testing.T, port *EnginePort) {
			if err := port.ReplaceRawHistoryFor(sessionID, frameHistory("HANDOFF-FRAME")); err != nil {
				t.Fatalf("ReplaceRawHistoryFor: %v", err)
			}
		}},
		{"replace-active", func(t *testing.T, port *EnginePort) {
			if err := port.ReplaceRawHistory(sessionID, frameHistory("HANDOFF-FRAME")); err != nil {
				t.Fatalf("ReplaceRawHistory: %v", err)
			}
		}},
		{"resume", func(t *testing.T, port *EnginePort) {
			if err := port.ResumeRawSession(sessionID, frameHistory("HANDOFF-FRAME")); err != nil {
				t.Fatalf("ResumeRawSession: %v", err)
			}
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			port := newQuietPort(t)
			recorder := &recordingPrepare{}
			port.ApplyDeps(EnginePortDeps{PrepareHistory: recorder.record})

			testCase.run(t, port)

			got := recorder.snapshot()
			if len(got) != 1 {
				t.Fatalf("宿主收到的交接次数 = %d，期望 1：%+v", len(got), got)
			}
			if got[0].sessionID != sessionID || !strings.Contains(got[0].history, "HANDOFF-FRAME") {
				t.Fatalf("交接内容不对：%+v", got[0])
			}
		})
	}
}

// TestPendingInstallHandsOffOutsidePortLock 钉住回合出口那条路径：会话忙时登记的待安装
// 在计数归零时安装，交接同样要走到锁外（旧实现在 port.mu 内交接，宿主回读即自锁）。
func TestPendingInstallHandsOffOutsidePortLock(t *testing.T) {
	harness := newFuseHarness(t, false)
	handedOff := make(chan struct{}, 8)
	harness.port.ApplyDeps(EnginePortDeps{PrepareHistory: func(sessionID string, _ []types.Message) {
		if !harness.port.HasSession(sessionID) {
			t.Errorf("交接时端口查不到会话 %s", sessionID)
		}
		handedOff <- struct{}{}
	}})

	// 旧替身引擎（不实现 historyQueuer）才走 pendingHistory 登记路径。这里手工给「那一台
	// 要跑回合的会话」登记一次待安装，让它的回合出口（计数归零 + 有登记）兑现这次安装。
	harness.port.mu.Lock()
	harness.port.armPendingLocked(harness.hangKey, frameHistory("PENDING-FRAME"))
	harness.port.mu.Unlock()

	harness.startHangingTurn(t)
	harness.releaseHang(t)

	select {
	case <-handedOff:
	case <-time.After(fuseQuickBudget):
		t.Fatal("回合出口的安装没把交接交出去（或交在了 port.mu 之内）")
	}
	harness.assertNoFactoryFailure(t)
}

func messageWithContent(content string) types.Message {
	text := content
	return types.Message{Role: "user", Content: &text}
}
