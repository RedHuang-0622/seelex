package core

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMaterializeDraftSessionBindsContextStoreForNewSession 钉住「新会话也必须绑定
// 它自己的会话上下文存储」这条装配不变量。
//
// 现场（2026-10-04）：压缩帧的 `readback.note` 写着
// `推帧失败：compaction index: 会话上下文存储未绑定（压缩栈不可用）`。根因不在压缩，
// 而在**新建会话**这条路径——生产上唯一的挂接点是 resume 的
// `SessionPort.AttachSessionContext`（application/core/session_history.go），
// 而 `materializeDraftSession` 把草稿物化成真实会话时**显式解绑**（"新会话无既有
// context：保持解绑"），此后没有任何一处再挂上。于是这个会话的**整个第一生命周期**
// 都没有 store：
//   - seelebridge 的 `PushCompactionFrame` / `ReadbackCompactionSummary` 直接报
//     "会话上下文存储未绑定（压缩栈不可用）"：装配层压缩推不了帧、拿不到 segment_id，
//     帧正文只能写一句"没有模型生成的读后感"；
//   - `stackBlocks`（plan/task/skill/compact 栈块）与 `relatedMemoryBlocks` 恒为空。
//
// 会话一旦被 resume（切走再切回、或重启后打开）就一切正常——所以症状只出现在"新开
// 的会话"里。现场那两帧所属的两个会话键（`seelex-1791042860343116700-1`、
// `draft_1790523014165652000_1`）都是"新建会话"的早分配 SID。
func TestMaterializeDraftSessionBindsContextStoreForNewSession(t *testing.T) {
	engine := &fakeEngine{}
	sessions := &contextAwareSessions{}
	service := newTestService(t, engine, withTestSessions(sessions))

	if err := service.BeginNewSession(); err != nil {
		t.Fatalf("BeginNewSession: %v", err)
	}
	draftID := service.Snapshot().Session.ID
	if draftID == "" {
		t.Fatal("新建会话没有早分配 SID")
	}
	// 空闲会话离开时仍按既有口径解绑（防四栈串台，见 TestBeginNewSessionDetachesSessionContext）；
	// 这一步的计数要在物化前后都用来证明"物化没有额外解绑"。
	sessions.mu.Lock()
	detachedAtDraft := sessions.detached
	sessions.mu.Unlock()
	if detachedAtDraft != 1 {
		t.Fatalf("离开空闲会话应解绑一次（既有防串台口径）：detached=%d，want 1", detachedAtDraft)
	}

	if err := service.materializeDraftForSubmit(draftID, "第一个问题"); err != nil {
		t.Fatalf("materializeDraftForSubmit: %v", err)
	}
	// 物化后活跃会话就是那个新会话（早分配 SID 复用）。
	if got := service.Snapshot().Session.ID; got != draftID {
		t.Fatalf("物化后会话 ID = %q，want %q", got, draftID)
	}

	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	if len(sessions.attached) == 0 || !strings.HasSuffix(sessions.attached[len(sessions.attached)-1], ":"+draftID) {
		t.Fatalf("新建会话物化后必须绑定它自己的 context store；attach 调用 = %v（want 末次为 :%s）",
			sessions.attached, draftID)
	}
	if sessions.detached != detachedAtDraft {
		t.Fatalf("物化不得再解绑（新会话靠挂接，不靠解绑）：detached %d → %d", detachedAtDraft, sessions.detached)
	}
}

// TestBeginNewSessionKeepsRunningSessionContextStore 钉住"离开会话"这条路上的
// 判据：被离开的会话**仍在跑**时不得解绑它的 context store。
//
// 原因：`DetachSessionContext()` 解绑的是**当前活跃 bundle**（
// seelebridge.Runtime.AttachSessionContextStore(nil) → activeBundle），而跑着的会话
// 的回合会继续装配 provider 上下文——那一步遇到装配层压缩就会发现自己"未绑定"，
// 推帧直接报 `会话上下文存储未绑定（压缩栈不可用）`：与"新建会话从未绑定"同一个
// 症状、同一个根因面（store 与"会话当前在跑"的事实脱节）。空闲会话照旧解绑（见
// TestBeginNewSessionDetachesSessionContext，那条口径不变）。
func TestBeginNewSessionKeepsRunningSessionContextStore(t *testing.T) {
	engine := &fakeEngine{sessionID: "session-a", history: []EngineMessage{{Role: "user", Content: "long turn"}}}
	sessions := &contextAwareSessions{archiveSessions: archiveSessions{history: engine.History()}}
	service := newTestService(t, engine, withTestSessions(sessions))

	service.ViewMu.Lock()
	service.Core.Snapshot.Session = SessionState{ID: "session-a", Name: "长任务"}
	service.Core.Snapshot.Conversation = []Message{{ID: "user-1", Role: "user", Content: "跑着的一轮", CreatedAt: time.Now()}}
	service.sessionUnitLocked("session-a").SetChatState(ChatState{Running: true, RequestID: "req-a"}, nil)
	service.ViewMu.Unlock()

	if err := service.BeginNewSession(); err != nil {
		t.Fatalf("BeginNewSession: %v", err)
	}
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	if sessions.detached != 0 {
		t.Fatalf("被离开的会话仍在跑，不得解绑它的 context store（detached=%d）", sessions.detached)
	}
}

// contextAwareScopedSessions 给项目切换靶场的 scopedSessions 补上 context 挂接口
// （contextAware 那一对），用来观察"另起的独立会话有没有被挂上 store"。
type contextAwareScopedSessions struct {
	*scopedSessions
	mu       sync.Mutex
	attached []string
}

func (sessions *contextAwareScopedSessions) AttachSessionContext(workspaceID, sessionID string) error {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	sessions.attached = append(sessions.attached, workspaceID+":"+sessionID)
	return nil
}

func (sessions *contextAwareScopedSessions) DetachSessionContext() {}

// TestWorkspaceSwitchFreshSessionBindsContextStore 钉住第三条"让某个会话成为当前
// 会话"的路径：**切项目时另起的独立会话**（workspace_usecase.go 的
// startFreshSession 分支）与 /new 的草稿物化、resume 共用同一条挂接。三处漏一处，
// 那个会话的整段活跃期就处于"未绑定"——装配层压缩推不了帧（现场那句
// "会话上下文存储未绑定（压缩栈不可用）"），栈块与记忆块也一律为空。
func TestWorkspaceSwitchFreshSessionBindsContextStore(t *testing.T) {
	engine := &fakeEngine{chunks: []string{"ok"}}
	runtime := &fakeRuntime{}
	workspaces := newMultiProjectWorkspace()
	sessions := &contextAwareScopedSessions{scopedSessions: &scopedSessions{}}
	service := mustNew(t, Dependencies{
		Engine: engine,
		Runtime: runtimeWithContextLimits{
			fakeRuntime: runtime, window: 200_000, output: 8_192,
		},
		Plugins: &fakePlugins{current: PluginInfo{Name: "default"}}, Skills: fakeSkills{},
		Sessions: sessions, Workspace: workspaces,
	})
	t.Cleanup(service.Shutdown)
	if _, err := workspaces.Create("A", t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := workspaces.Create("B", t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}

	if err := service.BeginNewSession(); err != nil {
		t.Fatalf("BeginNewSession: %v", err)
	}
	if err := service.BindWorkspace("project-a"); err != nil {
		t.Fatalf("BindWorkspace(A): %v", err)
	}
	if err := service.Submit(context.Background(), "A 的第一轮"); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitSessionIdle(t, service)
	sessionA := service.Snapshot().Session.ID

	// 切到项目 B：当前会话有历史 → 起一条独立会话。
	if err := service.BindWorkspace("project-b"); err != nil {
		t.Fatalf("BindWorkspace(B): %v", err)
	}
	waitSessionIdle(t, service)
	sessionB := service.Snapshot().Session.ID
	if sessionB == "" || sessionB == sessionA {
		t.Fatalf("项目切换应新建独立会话（否则这条用例没有判别力）：A=%q B=%q", sessionA, sessionB)
	}

	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	if len(sessions.attached) == 0 || !strings.HasSuffix(sessions.attached[len(sessions.attached)-1], ":"+sessionB) {
		t.Fatalf("切项目另起的会话必须绑定它自己的 context store；attach 调用 = %v（want 末次为 :%s）",
			sessions.attached, sessionB)
	}
}
