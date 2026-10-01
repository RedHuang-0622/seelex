package core

import (
	"testing"
	"time"
)

// TestBeginNewSessionClearsPreviousSessionContextFacts 钉住「新建会话」的**会话事实**
// 清空纪律：进入草稿即离开了上一个会话，快照里属于它的会话事实必须一并清掉，不能让
// 新会话显示（进而在首次提交时落盘成）上一个会话的任务面与已读文件。
//
// 现场（2026-10-01 用户报告）：「上下文压缩总是污染前端，然后在新开会话的时候带到
// 新建会话」——右栏「上下文压缩」在**全新会话**里仍列着上一个会话的压缩记录。
// 机制：压缩记录挂在 `Snapshot.Task.ContextCompactions` 上，而 `BeginNewSession`
// 清了 Plan/Conversation/Chat/Interaction，**唯独没清 `Snapshot.Task`**（同一批
// 会话事实在 `unloadSession` 里是清了的，两条路径口径不一致）。
//
// 这条污染的代价不止显示：新会话首次提交时 `PersistCurrentSession` 按会话落盘
// （`session_runtime/archive.go` 的 `Execution.Task` / `Execution.ReadFiles` 都取自
// 这份全局镜像），于是上一个会话的压缩记录与已读文件会被**写成新会话自己的历史**。
func TestBeginNewSessionClearsPreviousSessionContextFacts(t *testing.T) {
	applyWindowConfig(t, WindowConfig{RetainTokens: 9_000, Ratio: 0.7})
	service, _, sessionID := compactTestService(t, "task-draft-scope")
	appendWindowRounds(t, service, "task-draft-scope", 16, 32_000)
	compactNow(t, service, sessionID)

	service.ViewMu.Lock()
	service.Core.Snapshot.ReadFiles = []ReadFileRef{{Path: "application/core/session_draft.go", ReadAt: time.Now()}}
	service.ViewMu.Unlock()

	if records := compactionRecords(service, sessionID); len(records) == 0 {
		t.Fatal("前置条件不成立：会话 A 没有压缩记录")
	}
	sessionA := service.Snapshot()
	if sessionA.Task == nil || len(sessionA.Task.ContextCompactions) == 0 {
		t.Fatalf("前置条件不成立：会话 A 的快照任务面没有压缩记录：%+v", sessionA.Task)
	}
	if len(sessionA.ReadFiles) == 0 {
		t.Fatal("前置条件不成立：会话 A 没有已读文件")
	}

	if err := service.BeginNewSession(); err != nil {
		t.Fatalf("BeginNewSession: %v", err)
	}
	draft := service.Snapshot()
	if draft.Session.ID == sessionA.Session.ID || draft.Session.ID == "" {
		t.Fatalf("新建会话 ID = %q，期望一个不同于 A(%q) 的草稿会话", draft.Session.ID, sessionA.Session.ID)
	}
	if !draft.Session.Draft {
		t.Fatalf("新建会话应是草稿态：%+v", draft.Session)
	}
	if draft.Task != nil {
		t.Fatalf("新建会话带上了上一个会话的任务面（压缩记录挂在这里）：%+v", draft.Task)
	}
	if len(draft.ReadFiles) != 0 {
		t.Fatalf("新建会话带上了上一个会话的已读文件：%+v", draft.ReadFiles)
	}
}
