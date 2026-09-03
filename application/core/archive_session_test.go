package core

import (
	"errors"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
)

// archiveCommandSessions 是 C2 ArchiveSession 的测试桩：会话粒度目录
// （scopedSessions）+ model.SessionRecord 读写面。目录行状态以 record 为准
// （模拟生产 SessionPort：SessionsOf 从 record 补状态），供"归档→目录过滤"
// 断言。
type archiveCommandSessions struct {
	*scopedSessions
	mu      sync.Mutex
	records map[string]SessionRecord // "projectID|sessionID"
}

func newArchiveCommandSessions() *archiveCommandSessions {
	return &archiveCommandSessions{
		scopedSessions: &scopedSessions{
			catalog:   map[string][]SessionInfo{},
			histories: map[string]map[string][]EngineMessage{},
		},
		records: map[string]SessionRecord{},
	}
}

func (sessions *archiveCommandSessions) add(projectID, sessionID string) {
	now := time.Now()
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	sessions.scopedSessions.mu.Lock()
	sessions.catalog[projectID] = append(sessions.catalog[projectID], SessionInfo{
		ID: sessionID, UpdatedAt: now,
	})
	if sessions.histories[projectID] == nil {
		sessions.histories[projectID] = map[string][]EngineMessage{}
	}
	sessions.histories[projectID][sessionID] = []EngineMessage{{Role: "user", Content: sessionID}}
	sessions.scopedSessions.mu.Unlock()
	sessions.records[projectID+"|"+sessionID] = SessionRecord{
		Version:   session_runtime.SessionRecordVersion,
		ID:        sessionID,
		Status:    SessionStatusIdle,
		UpdatedAt: now,
		Conversation: ConversationRecord{Messages: []Message{{
			ID: "m1", Role: "user", Content: sessionID + " content", CreatedAt: now,
		}}},
	}
}

func (sessions *archiveCommandSessions) recordFor(projectID, sessionID string) (SessionRecord, bool) {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	record, ok := sessions.records[projectID+"|"+sessionID]
	return record, ok
}

func (sessions *archiveCommandSessions) SaveSessionRecordWorkspace(projectID, sessionID string, record SessionRecord) error {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	sessions.records[projectID+"|"+sessionID] = record
	return nil
}

func (sessions *archiveCommandSessions) SaveSessionRecord(sessionID string, record SessionRecord) error {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	sessions.records["|"+sessionID] = record
	return nil
}

func (sessions *archiveCommandSessions) LoadSessionRecordWorkspace(projectID, sessionID string) (SessionRecord, error) {
	record, ok := sessions.recordFor(projectID, sessionID)
	if !ok {
		return SessionRecord{}, fs.ErrNotExist
	}
	return record, nil
}

func (sessions *archiveCommandSessions) LoadSessionRecord(sessionID string) (SessionRecord, error) {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	for key, record := range sessions.records {
		if strings.HasSuffix(key, "|"+sessionID) {
			return record, nil
		}
	}
	return SessionRecord{}, fs.ErrNotExist
}

// SessionsOf 以 record 状态覆盖目录行（与生产 adapter 一致）。
func (sessions *archiveCommandSessions) SessionsOf(projectID string) []SessionInfo {
	infos := sessions.scopedSessions.SessionsOf(projectID)
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	for index := range infos {
		if record, ok := sessions.records[projectID+"|"+infos[index].ID]; ok {
			infos[index].Status = record.Status
		}
	}
	return infos
}

func archiveTestService(t *testing.T, sessions *archiveCommandSessions) *Service {
	t.Helper()
	service := mustNew(t, Dependencies{
		Engine:    &fakeEngine{},
		Runtime:   &fakeRuntime{},
		Plugins:   &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills:    fakeSkills{},
		Sessions:  sessions,
		Workspace: newFakeWorkspace(),
	})
	t.Cleanup(service.Shutdown)
	return service
}

// TestArchiveSessionHidesSessionFromItsProjectGrid C2 契约：归档后 record
// 状态为 archived，且目标项目的一轮范围刷新把该行过滤掉——其它项目行与
// 会话记录（含可见消息）原样保留（不是全局数组上的标记位）。
func TestArchiveSessionHidesSessionFromItsProjectGrid(t *testing.T) {
	sessions := newArchiveCommandSessions()
	sessions.add("", "session-a")
	service := archiveTestService(t, sessions)
	if _, err := service.Deps.Workspace.Create("proj", "C:\\proj", ""); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	sessions.add("project-1", "session-b")
	waitForSnapshot(t, service, func(snapshot Snapshot) bool { return len(snapshot.Sessions) == 2 })

	if err := service.ArchiveSession("session-a"); err != nil {
		t.Fatalf("ArchiveSession: %v", err)
	}
	snapshot := waitForSnapshot(t, service, func(snapshot Snapshot) bool {
		ids := sessionIDsOf(snapshot.Sessions)
		return len(ids) == 1 && containsSessionID(ids, "session-b")
	})
	if containsSessionID(sessionIDsOf(snapshot.Sessions), "session-a") {
		t.Fatalf("archived session-a still listed: %v", sessionIDsOf(snapshot.Sessions))
	}
	record, ok := sessions.recordFor("", "session-a")
	if !ok || record.Status != SessionStatusArchived {
		t.Fatalf("archived record status = %q (ok=%v), want archived", record.Status, ok)
	}
	if len(record.Conversation.Messages) != 1 || record.Conversation.Messages[0].Content != "session-a content" {
		t.Fatalf("archived record lost conversation: %+v", record.Conversation.Messages)
	}
	other, ok := sessions.recordFor("project-1", "session-b")
	if !ok || other.Status != SessionStatusIdle {
		t.Fatalf("other project record disturbed by archive: status=%q ok=%v", other.Status, ok)
	}
}

// TestArchiveSessionRejectsBusySessions C2 门控：running/queued/awaiting
// 会话拒绝归档（复用 sessionBusy 语义），空闲后允许。
func TestArchiveSessionRejectsBusySessions(t *testing.T) {
	sessions := newArchiveCommandSessions()
	sessions.add("", "session-a")
	service := archiveTestService(t, sessions)
	waitForSnapshot(t, service, func(snapshot Snapshot) bool { return len(snapshot.Sessions) == 1 })

	service.ViewMu.Lock()
	unit := service.sessionUnitLocked("session-a")
	unit.SetChatState(ChatState{Running: true}, nil)
	service.ViewMu.Unlock()
	if err := service.ArchiveSession("session-a"); !errors.Is(err, ErrChatRunning) {
		t.Fatalf("archive while running = %v, want ErrChatRunning", err)
	}
	service.ViewMu.Lock()
	unit.SetChatState(ChatState{QueuedCount: 1}, nil)
	service.ViewMu.Unlock()
	if err := service.ArchiveSession("session-a"); !errors.Is(err, ErrChatRunning) {
		t.Fatalf("archive while queued = %v, want ErrChatRunning", err)
	}
	service.ViewMu.Lock()
	unit.SetChatState(ChatState{}, nil)
	unit.AddApproval("approval-1")
	service.ViewMu.Unlock()
	if err := service.ArchiveSession("session-a"); !errors.Is(err, ErrChatRunning) {
		t.Fatalf("archive while awaiting approval = %v, want ErrChatRunning", err)
	}
	service.ViewMu.Lock()
	unit.RemoveApproval("approval-1")
	service.ViewMu.Unlock()
	if err := service.ArchiveSession("session-a"); err != nil {
		t.Fatalf("archive after idle: %v", err)
	}
	record, ok := sessions.recordFor("", "session-a")
	if !ok || record.Status != SessionStatusArchived {
		t.Fatalf("record after idle archive status=%q ok=%v, want archived", record.Status, ok)
	}
}

// TestArchiveSessionMissingRecord 钉住失败路径：会话不存在/record 缺失时
// 显式报错，不静默成功。
func TestArchiveSessionMissingRecord(t *testing.T) {
	sessions := newArchiveCommandSessions()
	service := archiveTestService(t, sessions)
	if err := service.ArchiveSession("ghost"); !errors.Is(err, ErrSessionNotFoundArchive) {
		t.Fatalf("archive missing session = %v, want ErrSessionNotFoundArchive", err)
	}
}
