package core

import (
	"testing"
)

// TestRunningSessionNotRerootedByAttach 回归（复现报告中“切到其它会话后工具
// 不按会话原进程恢复”的机理）：全局 projectScope 是进程级执行面（工具/
// 工作树仍读全局根），只要还有任意会话在运行中（尤其异步冷恢复把视图先切到
// 目标、原会话 A 仍在后台跑——此时“目标==当前视图”并不意味着没有在途会话），
// bindProjectRootIfSafe 就必须拒绝重绑全局根——否则在途会话接下来的路径工具
// 会解析到新绑定的项目根（跨会话串写）。进程完全空闲后才允许重绑/收尾对齐。
func TestRunningSessionNotRerootedByAttach(t *testing.T) {
	runtime := &fakeRuntime{}
	sessions := &scopedSessions{}
	service := mustNew(t, Dependencies{
		Engine:   &fakeEngine{sessionID: "session-a"},
		Runtime:  runtime,
		Plugins:  &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills:   fakeSkills{},
		Sessions: sessions,
	})
	defer service.Shutdown()

	// 视图会话 = A（异步冷恢复把视图切到目标后、原会话仍在后台跑的形态：
	// 绑定方拿到的“当前会话”正是这个视图会话，不能因此放行重绑）。
	service.ViewMu.Lock()
	service.Core.Snapshot.Session = SessionState{ID: "session-a"}
	unit := service.sessionUnitLocked("session-a")
	unit.SetChatState(ChatState{Running: true, RequestID: "r1"}, nil)
	service.ViewMu.Unlock()

	rootB := t.TempDir()
	// 关键：A 仍在运行中（视图=A 亦不例外）→ 必须拒绝重绑全局根。
	if got := service.bindProjectRootIfSafe("session-a", rootB); got {
		t.Fatal("bindProjectRootIfSafe rebound global root while a session is running")
	}
	if runtime.projectRoot != "" {
		t.Fatalf("global project root = %q after refused bind, want unchanged", runtime.projectRoot)
	}

	// 进程回到完全空闲后：允许重绑（工具按当前视图会话工作区恢复的前提）。
	service.ViewMu.Lock()
	unit.SetChatState(ChatState{}, nil)
	service.ViewMu.Unlock()
	if got := service.bindProjectRootIfSafe("session-a", rootB); !got {
		t.Fatal("bindProjectRootIfSafe refused bind while idle")
	}
	if runtime.projectRoot != rootB {
		t.Fatalf("project root after idle bind = %q, want %q", runtime.projectRoot, rootB)
	}
}
