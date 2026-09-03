package core

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

type coldReadSessions struct {
	archiveCommandSessions
	mu     sync.Mutex
	events map[string][]sessionstore.Event
}

func newColdReadSessions() *coldReadSessions {
	return &coldReadSessions{
		archiveCommandSessions: *newArchiveCommandSessions(),
		events:                 map[string][]sessionstore.Event{},
	}
}

func (sessions *coldReadSessions) LoadEventRangeWorkspace(projectID, sessionID string, fromSeq, toSeq uint64) ([]sessionstore.Event, error) {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	all := sessions.events[projectID+"|"+sessionID]
	selected := make([]sessionstore.Event, 0, len(all))
	for _, event := range all {
		if fromSeq == 0 && toSeq == 0 {
			selected = append(selected, event)
			continue
		}
		if event.Seq >= fromSeq && event.Seq <= toSeq {
			selected = append(selected, event)
		}
	}
	return selected, nil
}

func (sessions *coldReadSessions) setEvents(projectID, sessionID string, events []sessionstore.Event) {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	sessions.events[projectID+"|"+sessionID] = events
}

func newColdReadService(t *testing.T, sessions *coldReadSessions) *Service {
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

// TestSnapshotOfColdSessionAssemblesReadOnlyBaseline C1：未驻留（无 unit）
// 会话从 record 拼只读基线——Resident=false、对话/标题/任务来自 record，
// 不再 clone 视图 Runtime（M4 撤销）。
func TestSnapshotOfColdSessionAssemblesReadOnlyBaseline(t *testing.T) {
	sessions := newColdReadSessions()
	sessions.add("", "cold-1")
	now := time.Now()
	if err := sessions.SaveSessionRecordWorkspace("", "cold-1", SessionRecord{
		Version: session_runtime.SessionRecordVersion,
		ID:      "cold-1",
		Title:   SessionTitle{Value: "Cold Session"},
		Status:  SessionStatusIdle,
		Conversation: ConversationRecord{Messages: []Message{{
			ID: "m1", Role: "user", Content: "cold content", CreatedAt: now,
		}}},
		Execution: SessionExecutionRecord{
			Task: &TaskState{Status: TaskCompleted, Summary: "done"},
		},
		PlanStack: []SessionPlanFrame{{
			ID: "plan-1",
			Plan: &PlanState{
				Name: "cold plan", EntryNodeID: "n1", Status: PlanCompleted,
			},
		}},
		ActivePlanID: "plan-1",
		Tasks: []dto.TaskRecord{{
			Key: "plan:n1", Task: "cold task", Status: "done",
		}},
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed cold record: %v", err)
	}
	service := newColdReadService(t, sessions)

	snapshot, err := service.SnapshotOf("cold-1")
	if err != nil {
		t.Fatalf("SnapshotOf(cold): %v", err)
	}
	if snapshot.Resident {
		t.Fatal("cold snapshot must be Resident=false")
	}
	if snapshot.Session.Name != "Cold Session" || snapshot.Session.ID != "cold-1" {
		t.Fatalf("cold session identity = %+v", snapshot.Session)
	}
	if len(snapshot.Conversation) != 1 || snapshot.Conversation[0].Content != "cold content" {
		t.Fatalf("cold conversation = %+v", snapshot.Conversation)
	}
	if snapshot.Task == nil || snapshot.Task.Summary != "done" {
		t.Fatalf("cold task = %+v", snapshot.Task)
	}
	if snapshot.Runtime.Plan == nil || snapshot.Runtime.Plan.Name != "cold plan" {
		t.Fatalf("cold plan = %+v", snapshot.Runtime.Plan)
	}
	if !snapshot.Capabilities.SessionResume || !snapshot.Capabilities.SessionSnapshot {
		t.Fatalf("cold capabilities = %+v", snapshot.Capabilities)
	}
	if snapshot.Revision != 0 {
		t.Fatalf("cold baseline revision = %d, want 0 (no live increments)", snapshot.Revision)
	}
	// 热路径不变量：不建 bundle、不创建 unit。
	service.ViewMu.RLock()
	unit := service.sessions.Unit("cold-1")
	service.ViewMu.RUnlock()
	if unit != nil {
		t.Fatalf("cold read created a session unit: %+v", unit)
	}
}

// TestSnapshotOfColdUnknownSessionStillUnavailable 钉住未知会话的失败语义
// 不变（record 缺失 → ErrSessionSnapshotUnavailable）。
func TestSnapshotOfColdUnknownSessionStillUnavailable(t *testing.T) {
	sessions := newColdReadSessions()
	service := newColdReadService(t, sessions)
	if _, err := service.SnapshotOf("ghost"); !errors.Is(err, ErrSessionSnapshotUnavailable) {
		t.Fatalf("SnapshotOf(ghost) = %v, want ErrSessionSnapshotUnavailable", err)
	}
}

// TestGetSessionTranscriptRange 钉住冷读区间语义：含端点、倒置显式报错、
// (0,0)=全量；未驻留会话直接读事件库。
func TestGetSessionTranscriptRange(t *testing.T) {
	sessions := newColdReadSessions()
	sessions.add("", "cold-1")
	sessions.setEvents("", "cold-1", []sessionstore.Event{
		{Seq: 1, Role: "user", Content: "one"},
		{Seq: 2, Role: "assistant", Content: "two"},
		{Seq: 3, Role: "tool", Content: "three"},
		{Seq: 4, Role: "assistant", Content: "four"},
		{Seq: 5, Role: "user", Content: "five"},
	})
	service := newColdReadService(t, sessions)

	events, err := service.GetSessionTranscript("cold-1", 2, 4)
	if err != nil {
		t.Fatalf("GetSessionTranscript(2..4): %v", err)
	}
	if len(events) != 3 || events[0].Seq != 2 || events[2].Seq != 4 {
		t.Fatalf("transcript 2..4 = %+v", events)
	}
	if events[0].Content != "two" {
		t.Fatalf("transcript content mismatch: %+v", events[0])
	}
	all, err := service.GetSessionTranscript("cold-1", 0, 0)
	if err != nil || len(all) != 5 {
		t.Fatalf("transcript full = %d err=%v, want 5", len(all), err)
	}
	if _, err := service.GetSessionTranscript("cold-1", 5, 2); err == nil {
		t.Fatal("inverted transcript range must error")
	}
	if _, err := service.GetSessionTranscript("  ", 1, 2); err == nil {
		t.Fatal("empty session ID must error")
	}
}

// TestListSessionsReturnsAuthoritativeDirectory C1：目录枚举不要求视图快照
// 整份克隆，行数据与会话树同源。
func TestListSessionsReturnsAuthoritativeDirectory(t *testing.T) {
	sessions := newColdReadSessions()
	sessions.add("", "sess-1")
	service := newColdReadService(t, sessions)
	waitForSnapshot(t, service, func(snapshot Snapshot) bool { return len(snapshot.Sessions) == 1 })

	rows := service.ListSessions()
	if len(rows) != 1 || rows[0].ID != "sess-1" {
		t.Fatalf("ListSessions = %+v", rows)
	}
}
