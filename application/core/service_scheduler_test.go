package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/seelebridge"
)

// TestStartScheduledSessionCreatesNewSessionInWorkspace 钉住定时触发的会话落点：
//
//  1. **默认新建会话**（不是投给当前会话、也不切用户的视图指针）；
//  2. 指定工作区时，新会话真的装配到那个项目——会话绑定、framework 显式键、
//     工具面按会话解析的项目根三处都要对上（少一处就是"选了工作区但任务
//     跑在别的项目里"，即工作区污染）；
//  3. 提示词落在**新会话**的可见投影里，当前视图会话一条都不多。
//
// 会话记录本身仍走会话自己的存储/读写纪律（这里只验"落在哪个会话、绑哪个
// 项目"这一层，落盘口径由 sessionstore 的用例钉住）。
func TestStartScheduledSessionCreatesNewSessionInWorkspace(t *testing.T) {
	root := t.TempDir()
	engine := &fakeEngine{chunks: []string{"收到"}}
	runtime := &fakeRuntime{}
	workspaces := newFakeWorkspace()
	workspaces.items["project-1"] = WorkspaceInfo{ID: "project-1", Name: "巡检项目", RootPath: root}
	sessions := &scopedSessions{}
	service := mustNew(t, Dependencies{
		Engine: engine, Runtime: runtime, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions, Workspace: workspaces,
	})
	defer service.Shutdown()

	before := service.Snapshot().Session.ID
	sessionID, err := service.StartScheduledSession(context.Background(), "每天巡检一次", "project-1")
	if err != nil {
		t.Fatalf("StartScheduledSession: %v", err)
	}
	if sessionID == "" || sessionID == before {
		t.Fatalf("定时触发必须落到新会话：new=%q current=%q", sessionID, before)
	}
	if !strings.HasPrefix(sessionID, "sched") {
		t.Fatalf("新建会话 ID 前缀 = %q，want sched 前缀（一眼能看出是定时任务开的会话）", sessionID)
	}
	if workspaces.bindings[sessionID] != "project-1" {
		t.Fatalf("新会话没有绑定工作区：bindings=%v", workspaces.bindings)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatalf("定时会话没有回到 idle：%v", err)
	}
	// 工具根按会话解析：回合起点（runChat）会把新会话自己的项目根绑上，
	// 因此这里读到的必须是工作区根，而不是进程默认根。
	if got := runtime.toolRootForSession(sessionID); got != root {
		t.Fatalf("新会话的工具根 = %q，want 工作区根 %q", got, root)
	}
	if got := service.Snapshot().Session.ID; got != before {
		t.Fatalf("定时任务把用户的视图会话切走了：before=%q after=%q", before, got)
	}
	if view := conversationContents(service.Snapshot().Conversation); strings.Contains(view, "每天巡检一次") {
		t.Fatalf("提示词落到了当前视图会话，而不是新会话：%q", view)
	}
	snapshot, err := service.SnapshotOf(sessionID)
	if err != nil {
		t.Fatalf("SnapshotOf(%q): %v", sessionID, err)
	}
	if got := conversationContents(snapshot.Conversation); !strings.Contains(got, "每天巡检一次") {
		t.Fatalf("新会话里看不到提示词：%q", got)
	}
}

// TestStartScheduledSessionWithoutWorkspaceKeepsSessionUnbound 钉住"工作区是
// 可选"这一半：不给工作区就是**不绑项目**的新会话——不能继承视图会话的项目
// 绑定（否则在项目 A 里建的定时任务会偷偷跑在项目 A 上）。
func TestStartScheduledSessionWithoutWorkspaceKeepsSessionUnbound(t *testing.T) {
	root := t.TempDir()
	engine := &fakeEngine{chunks: []string{"收到"}}
	runtime := &fakeRuntime{}
	workspaces := newFakeWorkspace()
	workspaces.items["project-1"] = WorkspaceInfo{ID: "project-1", Name: "巡检项目", RootPath: root}
	service := mustNew(t, Dependencies{
		Engine: engine, Runtime: runtime, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: &scopedSessions{}, Workspace: workspaces,
	})
	defer service.Shutdown()

	// 当前会话先绑到项目（视图会话的项目绑定不该传染给定时会话）。
	workspaces.BindSession(service.Snapshot().Session.ID, "project-1")

	sessionID, err := service.StartScheduledSession(context.Background(), "无工作区巡检", "")
	if err != nil {
		t.Fatalf("StartScheduledSession: %v", err)
	}
	if _, bound := workspaces.bindings[sessionID]; bound {
		t.Fatalf("未指定工作区的新会话不该绑项目：%v", workspaces.bindings)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 没有按会话绑根 = 这个会话的工具面走进程默认根（会话切换的正常语义），
	// 关键是它**没有**自己的项目根——没人把别的项目塞给它。
	if got := runtime.sessionProjectRoot(sessionID); got != "" {
		t.Fatalf("无工作区的新会话被绑上了项目根 %q", got)
	}
}

// sessionProjectRoot 读 fakeRuntime 的按会话工具根（同包测试直读，加锁）。
func (runtime *fakeRuntime) sessionProjectRoot(sessionID string) string {
	runtime.sessionProjectRootsMu.Lock()
	defer runtime.sessionProjectRootsMu.Unlock()
	return runtime.sessionProjectRoots[sessionID]
}

// conversationContents 把可见会话拼成一段文本（判据只看"出现过没有"）。
func conversationContents(messages []Message) string {
	parts := make([]string, 0, len(messages))
	for _, message := range messages {
		parts = append(parts, message.Content)
	}
	return strings.Join(parts, "\n")
}

// TestUpdateScheduledTaskForwardsAndValidatesWorkspace 钉住编辑这条链的应用层口径：
// 定义合法性交给调度器（与创建同一份判据），**工作区存在性**在应用层判（与创建
// 同一份判据）——改到不存在的工作区要当场拒绝，而不是等到触发那天才失败。
func TestUpdateScheduledTaskForwardsAndValidatesWorkspace(t *testing.T) {
	runtime := &fakeRuntime{}
	workspaces := newFakeWorkspace()
	workspaces.items["project-1"] = WorkspaceInfo{ID: "project-1", Name: "巡检项目", RootPath: t.TempDir()}
	service := mustNew(t, Dependencies{
		Engine: &fakeEngine{}, Runtime: runtime, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: &fakeSessions{}, Workspace: workspaces,
	})
	defer service.Shutdown()

	updated, err := service.UpdateScheduledTask(context.Background(), "sched_test", ScheduledTaskSpec{
		Name: "改后的名字", Kind: seelebridge.ScheduledTaskPrompt, Prompt: "改后的提示词",
		WorkspaceID: "project-1", Interval: time.Hour, Enabled: true,
	})
	if err != nil {
		t.Fatalf("UpdateScheduledTask: %v", err)
	}
	if updated == nil || updated.ID != "sched_test" || updated.Name != "改后的名字" {
		t.Fatalf("编辑结果 = %+v", updated)
	}
	if len(runtime.updatedTasks) != 1 || runtime.updatedTasks[0] != "sched_test" {
		t.Fatalf("编辑没有按 ID 转发：%v", runtime.updatedTasks)
	}

	// 不存在的工作区：当场拒绝，且不惊动调度器。
	if _, err := service.UpdateScheduledTask(context.Background(), "sched_test", ScheduledTaskSpec{
		Name: "n", Kind: seelebridge.ScheduledTaskPrompt, Prompt: "P", WorkspaceID: "ws_missing", Interval: time.Hour,
	}); err == nil {
		t.Fatal("编辑到不存在的工作区必须报错")
	}
	if len(runtime.updatedTasks) != 1 {
		t.Fatalf("被拒的编辑不该转发到调度器：%v", runtime.updatedTasks)
	}

	// 空 ID：编辑没有目标，直接拒绝。
	if _, err := service.UpdateScheduledTask(context.Background(), "   ", ScheduledTaskSpec{
		Name: "n", Kind: seelebridge.ScheduledTaskPrompt, Prompt: "P", Interval: time.Hour,
	}); err == nil {
		t.Fatal("空任务 ID 必须报错")
	}
}
