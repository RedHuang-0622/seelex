package core

import (
	"strings"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// 草稿尾恢复的应用侧装配（A4）接线证明：
//   - 探测 → 决策 → 恢复：recoverable 必须真的调用恢复入口，且**在可见正文读之前**
//     （否则恢复出来的行进不了本次加载的可见会话）；
//   - gap 只报告、不发布、不清理（红线 3）；
//   - clean / 能力未装配 → 静默，不产生任何写；
//   - 结论对用户可见（system 行进入可见会话，不是只写日志）。

// pendingTailSessions 在 fakeSessions 之上实现草稿尾端口，按真实顺序记录调用。
type pendingTailSessions struct {
	fakeSessions
	mu        sync.Mutex
	calls     []string
	probe     dto.PendingMessageTailReport
	probeOK   bool
	recovered dto.PendingMessageTailReport
	recoverOK bool
}

func (s *pendingTailSessions) record(call string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, call)
}

func (s *pendingTailSessions) callOrder() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

func (s *pendingTailSessions) PendingMessageTailWorkspace(_, _ string) (dto.PendingMessageTailReport, bool, error) {
	s.record("probe")
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.probe, s.probeOK, nil
}

func (s *pendingTailSessions) RecoverPendingMessageTailWorkspace(_, _ string) (dto.PendingMessageTailReport, bool, error) {
	s.record("recover")
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recovered, s.recoverOK, nil
}

func (s *pendingTailSessions) DiscardPendingMessageTailWorkspace(_, _ string) (dto.PendingMessageTailReport, bool, error) {
	s.record("discard")
	s.mu.Lock()
	defer s.mu.Unlock()
	return dto.PendingMessageTailReport{Status: dto.PendingTailDiscarded}, true, nil
}

// pendingTailVisibleTail 返回可见会话里最后一条 system 行的正文（没有则 ""）。
func pendingTailVisibleTail(t *testing.T, service *Service) string {
	t.Helper()
	conversation := service.Snapshot().Conversation
	for index := len(conversation) - 1; index >= 0; index-- {
		if conversation[index].Role == "system" {
			return conversation[index].Content
		}
	}
	return ""
}

// TestPendingTailRecoverableIsRecoveredAndVisible：基座一致的草稿尾在冷加载时被
// 真的恢复，且用户能在会话里看到"恢复了 N 行"。
func TestPendingTailRecoverableIsRecoveredAndVisible(t *testing.T) {
	sessions := &pendingTailSessions{
		probe:     dto.PendingMessageTailReport{Status: dto.PendingTailRecoverable, HeadSeq: 7, TailFrom: 8, TailTo: 9, RowCount: 2},
		probeOK:   true,
		recovered: dto.PendingMessageTailReport{Status: dto.PendingTailRecovered, HeadSeq: 9, TailFrom: 8, TailTo: 9, RowCount: 2},
		recoverOK: true,
	}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))

	if err := service.ResumeSession("sess-tail"); err != nil {
		t.Fatal(err)
	}
	order := sessions.callOrder()
	if len(order) < 2 || order[0] != "probe" || order[1] != "recover" {
		t.Fatalf("端口调用序列 = %v, want 以 [probe recover] 开头", order)
	}
	tail := pendingTailVisibleTail(t, service)
	if !strings.Contains(tail, "Recovered 2 uncommitted") {
		t.Fatalf("可见会话里没有草稿尾恢复提示：%q（调用序列 %v）", tail, order)
	}
	if !strings.Contains(tail, "7 → 9") {
		t.Fatalf("提示未带发布点位移（用户无法判断恢复了哪一段）：%q", tail)
	}
}

// TestPendingTailGapIsReportedOnly：基座断裂只报告——不得调用恢复，也不得丢弃，
// 用户看到"内容仍在磁盘、需人工确认"。
func TestPendingTailGapIsReportedOnly(t *testing.T) {
	sessions := &pendingTailSessions{
		probe:   dto.PendingMessageTailReport{Status: dto.PendingTailGap, HeadSeq: 7, TailFrom: 11, RowCount: 3, Reason: "base mismatch"},
		probeOK: true,
	}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))

	if err := service.ResumeSession("sess-gap"); err != nil {
		t.Fatal(err)
	}
	for _, call := range sessions.callOrder() {
		if call != "probe" {
			t.Fatalf("gap 状态下不得发布/清理草稿尾，实际调用序列 = %v", sessions.callOrder())
		}
	}
	tail := pendingTailVisibleTail(t, service)
	if !strings.Contains(tail, "cannot be recovered safely") {
		t.Fatalf("gap 未向用户报告：%q", tail)
	}
}

// TestPendingTailCleanIsSilent：没有草稿尾时不做任何写、不打扰用户。
func TestPendingTailCleanIsSilent(t *testing.T) {
	sessions := &pendingTailSessions{
		probe:   dto.PendingMessageTailReport{Status: dto.PendingTailClean, HeadSeq: 7},
		probeOK: true,
	}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))

	if err := service.ResumeSession("sess-clean"); err != nil {
		t.Fatal(err)
	}
	if order := sessions.callOrder(); len(order) != 1 || order[0] != "probe" {
		t.Fatalf("clean 只该探测一次，实际调用序列 = %v", order)
	}
	if tail := pendingTailVisibleTail(t, service); strings.Contains(tail, "uncommitted") {
		t.Fatalf("clean 不该产生任何草稿尾提示：%q", tail)
	}
}

// TestPendingTailRunningSessionNotTouched：回合正在跑的会话，加载路径不得抢发布点
// （草稿尾属于在飞写者；否则两个写者会互相 reap 草稿）。
func TestPendingTailRunningSessionNotTouched(t *testing.T) {
	sessions := &pendingTailSessions{
		probe:   dto.PendingMessageTailReport{Status: dto.PendingTailRecoverable, HeadSeq: 7, RowCount: 2},
		probeOK: true,
	}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))

	service.ViewMu.Lock()
	service.sessionUnitLocked("sess-run").UpdateChat(func(chat *ChatState) { chat.Running = true }, nil)
	service.ViewMu.Unlock()
	if notice := service.recoverPendingMessageTailAtLoad("sess-run"); notice != "" {
		t.Fatalf("运行中会话不该被恢复入口打扰：%q", notice)
	}
	if order := sessions.callOrder(); len(order) != 0 {
		t.Fatalf("运行中会话连探测都不该发：%v", order)
	}
}

// TestPendingTailCapabilityAbsentIsNoop：存储未装配该能力时静默退回（纯增强语义）。
func TestPendingTailCapabilityAbsentIsNoop(t *testing.T) {
	service := newTestService(t, &fakeEngine{}, withTestSessions(&fakeSessions{}))
	if err := service.ResumeSession("sess-no-cap"); err != nil {
		t.Fatal(err)
	}
	if tail := pendingTailVisibleTail(t, service); strings.Contains(tail, "uncommitted") {
		t.Fatalf("能力未装配时不该出现草稿尾提示：%q", tail)
	}
}
