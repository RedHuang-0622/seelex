package core

import (
	"context"
	"errors"
	"github.com/RedHuang-0622/seelex/backend/engine/session"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"time"
)

// archiveCommandSessions 是 C2 ArchiveSession 的测试桩：会话粒度目录
// （scopedSessions）+ model.SessionRecord 读写面。目录行状态以 record 为准
// （模拟生产 SessionPort：SessionsOf 从 record 补状态），供"归档→目录过滤"
// 断言。
type archiveCommandSessions struct {
	*scopedSessions
	mu      sync.Mutex
	records map[string]model.SessionRecord // "projectID|sessionID"
}

func newArchiveCommandSessions() *archiveCommandSessions {
	return &archiveCommandSessions{
		scopedSessions: &scopedSessions{
			catalog:   map[string][]model.SessionInfo{},
			histories: map[string]map[string][]contract.EngineMessage{},
		},
		records: map[string]model.SessionRecord{},
	}
}

func (sessions *archiveCommandSessions) add(projectID, sessionID string) {
	now := time.Now()
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	sessions.scopedSessions.mu.Lock()
	sessions.catalog[projectID] = append(sessions.catalog[projectID], model.SessionInfo{
		ID: sessionID, UpdatedAt: now,
	})
	if sessions.histories[projectID] == nil {
		sessions.histories[projectID] = map[string][]contract.EngineMessage{}
	}
	sessions.histories[projectID][sessionID] = []contract.EngineMessage{{Role: "user", Content: sessionID}}
	sessions.scopedSessions.mu.Unlock()
	sessions.records[projectID+"|"+sessionID] = model.SessionRecord{
		Version:   session_runtime.SessionRecordVersion,
		ID:        sessionID,
		Status:    SessionStatusIdle,
		UpdatedAt: now,
		Conversation: model.ConversationRecord{Messages: []model.Message{{
			ID: "m1", Role: "user", Content: sessionID + " content", CreatedAt: now,
		}}},
	}
}

func (sessions *archiveCommandSessions) recordFor(projectID, sessionID string) (model.SessionRecord, bool) {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	record, ok := sessions.records[projectID+"|"+sessionID]
	return record, ok
}

func (sessions *archiveCommandSessions) SaveSessionRecordWorkspace(projectID, sessionID string, record model.SessionRecord) error {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	sessions.records[projectID+"|"+sessionID] = record
	return nil
}

func (sessions *archiveCommandSessions) SaveSessionRecord(sessionID string, record model.SessionRecord) error {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	sessions.records["|"+sessionID] = record
	return nil
}

func (sessions *archiveCommandSessions) LoadSessionRecordWorkspace(projectID, sessionID string) (model.SessionRecord, error) {
	record, ok := sessions.recordFor(projectID, sessionID)
	if !ok {
		return model.SessionRecord{}, fs.ErrNotExist
	}
	return record, nil
}

func (sessions *archiveCommandSessions) LoadSessionRecord(sessionID string) (model.SessionRecord, error) {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	for key, record := range sessions.records {
		if strings.HasSuffix(key, "|"+sessionID) {
			return record, nil
		}
	}
	return model.SessionRecord{}, fs.ErrNotExist
}

// SessionsOf 以 record 状态覆盖目录行（与生产 adapter 一致）。
func (sessions *archiveCommandSessions) SessionsOf(projectID string) []model.SessionInfo {
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
	service := mustNew(t, contract.Dependencies{
		Engine:    &fakeEngine{},
		Runtime:   &fakeRuntime{},
		Plugins:   &fakePlugins{current: model.PluginInfo{Name: "default"}},
		Skills:    fakeSkills{},
		Sessions:  sessions,
		Workspace: newFakeWorkspace(),
	})
	t.Cleanup(frontendOfTestService(service).Shutdown)
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := frontendOfTestService(service).WaitCatalogRefresh(ctx); err != nil {
		t.Fatalf("catalog settle after project add: %v", err)
	}
	if got := len(service.Snapshot().Sessions); got != 2 {
		t.Fatalf("catalog after settle = %d sessions, want 2", got)
	}

	if err := frontendOfTestService(service).ArchiveSession("session-a"); err != nil {
		t.Fatalf("ArchiveSession: %v", err)
	}
	if err := frontendOfTestService(service).WaitCatalogRefresh(ctx); err != nil {
		t.Fatalf("catalog settle after archive: %v", err)
	}
	ids := sessionIDsOf(service.Snapshot().Sessions)
	if len(ids) != 1 || !containsSessionID(ids, "session-b") || containsSessionID(ids, "session-a") {
		t.Fatalf("archived session-a still listed: %v", ids)
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := frontendOfTestService(service).WaitCatalogRefresh(ctx); err != nil {
		t.Fatalf("catalog settle: %v", err)
	}

	service.ViewMu.Lock()
	unit := service.sessionUnitLocked("session-a")
	unit.SetChatState(model.ChatState{Running: true}, nil)
	service.ViewMu.Unlock()
	if err := frontendOfTestService(service).ArchiveSession("session-a"); !errors.Is(err, ErrChatRunning) {
		t.Fatalf("archive while running = %v, want ErrChatRunning", err)
	}
	service.ViewMu.Lock()
	unit.SetChatState(model.ChatState{QueuedCount: 1}, nil)
	service.ViewMu.Unlock()
	if err := frontendOfTestService(service).ArchiveSession("session-a"); !errors.Is(err, ErrChatRunning) {
		t.Fatalf("archive while queued = %v, want ErrChatRunning", err)
	}
	service.ViewMu.Lock()
	unit.SetChatState(model.ChatState{}, nil)
	unit.AddApproval("approval-1")
	service.ViewMu.Unlock()
	if err := frontendOfTestService(service).ArchiveSession("session-a"); !errors.Is(err, ErrChatRunning) {
		t.Fatalf("archive while awaiting approval = %v, want ErrChatRunning", err)
	}
	service.ViewMu.Lock()
	unit.RemoveApproval("approval-1")
	service.ViewMu.Unlock()
	if err := frontendOfTestService(service).ArchiveSession("session-a"); err != nil {
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
	if err := frontendOfTestService(service).ArchiveSession("ghost"); !errors.Is(err, ErrSessionNotFoundArchive) {
		t.Fatalf("archive missing session = %v, want ErrSessionNotFoundArchive", err)
	}
}
