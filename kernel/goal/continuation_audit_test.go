package goal

import "testing"

// 续跑/暂停/恢复三个 kind 都是**非终态**：它们只往 append-only 账本加痕迹，
// 不许污染收口账本与看板 history（isTerminalAuditKind 只认 finish/abort）。
func TestContinuationAuditKindsAreNotTerminal(t *testing.T) {
	for _, kind := range []EventKind{EventContinue, EventPause, EventResume} {
		if isTerminalAuditKind(string(kind)) {
			t.Fatalf("%s 被当成终态：会污染收口账本/看板 history", kind)
		}
		if kind == EventFinish || kind == EventAbort {
			t.Fatalf("%s 与收口 kind 重名", kind)
		}
	}
	// 词表互不重名（防止复制粘贴写错）。
	seen := map[EventKind]struct{}{}
	for _, kind := range []EventKind{EventBegin, EventUpdate, EventFinish, EventAbort, EventRestore, EventContinue, EventPause, EventResume} {
		if _, ok := seen[kind]; ok {
			t.Fatalf("重复的 kind: %s", kind)
		}
		seen[kind] = struct{}{}
	}
}
