// session_project_root_test.go — 「会话 A 的工具根被视图会话的项目顶掉」复现。
//
// 生产事实：seelebridge 的工具路径根是进程级 projectScope（见
// Runtime.PerSessionExecution 注释），core 只在会话切换/恢复/绑定时按
// **视图会话**绑根。于是当一个会话在后台跑（SubmitToSession / 冷恢复续跑）而
// 视图已切到另一个项目的会话时，前者的 read_file/write_file/bash 会解析到后者的
// 项目根 —— 文件写进别的项目，即"工作区污染"。
package core

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// BindProjectRootFor / UnbindProjectRootFor 是 fakeRuntime 对生产
// Runtime.BindProjectRootFor 的镜像（按会话记录工具根）。
func (runtime *fakeRuntime) BindProjectRootFor(sessionID, rootPath string) error {
	if sessionID == "" {
		return errors.New("session ID is required")
	}
	runtime.sessionProjectRootsMu.Lock()
	defer runtime.sessionProjectRootsMu.Unlock()
	if runtime.sessionProjectRoots == nil {
		runtime.sessionProjectRoots = make(map[string]string)
	}
	runtime.sessionProjectRoots[sessionID] = rootPath
	return nil
}

func (runtime *fakeRuntime) UnbindProjectRootFor(sessionID string) {
	runtime.sessionProjectRootsMu.Lock()
	defer runtime.sessionProjectRootsMu.Unlock()
	delete(runtime.sessionProjectRoots, sessionID)
}

// toolRootForSession 模拟工具面的路径根解析：会话根优先，未绑定时回退进程默认
// 根（当前视图会话）。这正是 seelebridge tools.Router 对 ctx 会话的解析口径。
func (runtime *fakeRuntime) toolRootForSession(sessionID string) string {
	runtime.sessionProjectRootsMu.Lock()
	root := runtime.sessionProjectRoots[sessionID]
	runtime.sessionProjectRootsMu.Unlock()
	if root != "" {
		return root
	}
	return runtime.ProjectRoot()
}

// multiProjectWorkspace 给每个工作区分配独立 ID（fakeWorkspace.Create 固定返回
// project-1，无法表达"两个项目并行"）。
type multiProjectWorkspace struct {
	*fakeWorkspace
	mu       sync.Mutex
	sequence int
}

func newMultiProjectWorkspace() *multiProjectWorkspace {
	return &multiProjectWorkspace{fakeWorkspace: newFakeWorkspace()}
}

func (repo *multiProjectWorkspace) Create(name, rootPath, gitRemote string) (WorkspaceInfo, error) {
	repo.mu.Lock()
	repo.sequence++
	id := "project-" + string(rune('a'+repo.sequence-1))
	repo.mu.Unlock()
	item := WorkspaceInfo{ID: id, Name: name, RootPath: rootPath, GitRemote: gitRemote}
	repo.fakeWorkspace.mu.Lock()
	repo.fakeWorkspace.items[id] = item
	repo.fakeWorkspace.mu.Unlock()
	return item, nil
}

// TestBackgroundSessionKeepsOwnProjectRoot 复现工作区污染：会话 A 绑定项目 A，
// 视图切到项目 B 的会话后，后台为 A 跑一轮，A 的工具根必须仍是项目 A。
func TestBackgroundSessionKeepsOwnProjectRoot(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	engine := &fakeEngine{chunks: []string{"ok"}}
	runtime := &fakeRuntime{}
	sessions := &scopedSessions{}
	workspaces := newMultiProjectWorkspace()
	service := mustNew(t, Dependencies{
		Engine: engine, Runtime: runtime,
		Plugins: &fakePlugins{current: PluginInfo{Name: "default"}}, Skills: fakeSkills{},
		Sessions: sessions, Workspace: workspaces,
	})
	defer service.Shutdown()

	if _, err := workspaces.Create("A", rootA, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := workspaces.Create("B", rootB, ""); err != nil {
		t.Fatal(err)
	}

	// 会话 A：绑定项目 A 并跑一轮，成为已加载（后台可并行）会话。
	if err := service.BeginNewSession(); err != nil {
		t.Fatal(err)
	}
	if err := service.BindWorkspace("project-a"); err != nil {
		t.Fatal(err)
	}
	if err := service.Submit(context.Background(), "A 的第一轮"); err != nil {
		t.Fatal(err)
	}
	waitSessionIdle(t, service)
	sessionA := service.Snapshot().Session.ID
	if sessionA == "" {
		t.Fatal("会话 A 未物化")
	}

	// 视图切到项目 B 的新会话：进程级工具根随之指向 B。
	if err := service.BeginNewSession(); err != nil {
		t.Fatal(err)
	}
	if err := service.BindWorkspace("project-b"); err != nil {
		t.Fatal(err)
	}
	if err := service.Submit(context.Background(), "B 的第一轮"); err != nil {
		t.Fatal(err)
	}
	waitSessionIdle(t, service)
	sessionB := service.Snapshot().Session.ID
	if runtime.ProjectRoot() != rootB {
		t.Fatalf("前置条件不成立：进程级工具根 = %q, want %q（视图会话 B 的项目）", runtime.ProjectRoot(), rootB)
	}

	// 后台为会话 A 续跑：工具根必须还是 A 自己的项目。
	if err := service.SubmitToSession(context.Background(), sessionA, "A 的续跑"); err != nil {
		t.Fatalf("SubmitToSession(A): %v", err)
	}
	waitSessionIdle(t, service)

	if got := runtime.toolRootForSession(sessionA); got != rootA {
		t.Errorf("会话 A 的工具根 = %q, want %q：后台会话解析到了视图会话的项目根（工作区污染）", got, rootA)
	}
	if got := runtime.toolRootForSession(sessionB); got != rootB {
		t.Errorf("会话 B 的工具根 = %q, want %q", got, rootB)
	}
}

// waitSessionIdle 等待全部会话回合结束（多会话并行时视图 Chat 状态不代表进程空闲）。
func waitSessionIdle(t *testing.T, service *Service) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.WaitForIdle(ctx); err != nil {
		t.Fatalf("等待回合结束: %v", err)
	}
}
