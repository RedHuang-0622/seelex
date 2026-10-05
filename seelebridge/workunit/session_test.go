package workunit

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// TestSessionLedgerIsSatisfiedByExistingStore：契约要复用**既有**存储，不是再写一份。
// 接口与 `*sessionstore.NodeSessionStore` 的同名同签名由 session.go 里的编译期断言钉住
// （`var _ SessionLedger = (*sessionstore.NodeSessionStore)(nil)`），这里再钉一条语义：
// 这个端口只做"按主会话读写记录"，不带任何一层特有的概念。
func TestSessionLedgerIsSatisfiedByExistingStore(t *testing.T) {
	var ledger SessionLedger = (*sessionstore.NodeSessionStore)(nil)
	if ledger == nil {
		t.Fatal("接口必须能被既有存储直接满足（无适配器）——ledger 为 nil 说明断言没落上")
	}
}

// TestResumeNoticeStatesFactsAndNextStep：恢复说明三层一份：事实在前、动作在后、有界、不编造。
func TestResumeNoticeStatesFactsAndNextStep(t *testing.T) {
	record := sessionstore.NodeSessionRecord{
		NodeID:     "exec-wi-1",
		SessionID:  "role-sess-1",
		Goal:       "把 scene 生命周期接进契约",
		Status:     "running",
		Summary:    "已改完 worktree 认领",
		StagesJSON: []byte(`[{"stage":"running","preview":"改 worktree_manager"}]`),
		Worktree:   sessionstore.NodeWorktreeRecord{Path: "/tmp/wt/exec-wi-1", Branch: "seelex/exec-wi-1"},
	}
	notice := RecoveryNote(KindSubagent, record)
	for _, want := range []string{"exec-wi-1", "role-sess-1", "把 scene 生命周期接进契约", "running", "worktree 认领", "seelex/exec-wi-1", "下一步"} {
		if !strings.Contains(notice, want) {
			t.Fatalf("恢复说明缺事实 %q：\n%s", want, notice)
		}
	}
	// 前缀族与既有的 subagent 说明同源（`seelebridge` 侧的前缀常量 = 本族 + " subagent"）：
	// 两层在审计里因此能被同一条判据认出来。
	if !strings.HasPrefix(notice, RecoveryNotePrefix+" "+string(KindSubagent)) {
		t.Fatalf("恢复说明要用稳定前缀族：%q", notice)
	}
	if runes := len([]rune(notice)); runes > resumeNoticeLimit {
		t.Fatalf("恢复说明 %d rune 超过上限 %d", runes, resumeNoticeLimit)
	}

	// 空记录也要给得出一份说明（不 panic、不撒谎）：没有的事实写成"记录里没有"。
	empty := RecoveryNote(KindTeammate, sessionstore.NodeSessionRecord{})
	if !strings.HasPrefix(empty, RecoveryNotePrefix+" "+string(KindTeammate)) {
		t.Fatalf("teammate 的恢复说明应落在同一前缀族里（前缀尾换成 teammate）：%q", empty)
	}
	if !strings.Contains(empty, "记录里没有目标正文") {
		t.Fatalf("空记录也要给得出一份说明（不 panic、不撒谎）：缺的事实要写成「记录里没有」，而不是编造：%q", empty)
	}
}

// TestResumeEmptyReadsAsNoop：没有存储/没有残留时的读数必须干净（不是错误、也不是"有事"）。
func TestResumeEmptyReadsAsNoop(t *testing.T) {
	if !(Resume{}).Empty() {
		t.Fatal("零值 Resume 必须是 Empty")
	}
	if (Resume{Sessions: 1}).Empty() {
		t.Fatal("回灌到一个会话就不算 Empty")
	}
	if (Resume{Interrupted: []string{"sub-1"}}).Empty() {
		t.Fatal("有中断项就不算 Empty（要有人处置）")
	}
}
