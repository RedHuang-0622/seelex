package core

import (
	"testing"
)

// TestBindWorkspaceSetsProjectScope 回归：seelebridge 的 tool/worktree 项目
// 根仍是进程级 projectScope（SetSessionWorkspace 只解决存储键），因此
// BindWorkspace 必须设置全局项目根与 Router 写作用域——不管宿主是否声明
// PerSessionExecution。
func TestBindWorkspaceSetsProjectScope(t *testing.T) {
	runtime := &fakeRuntime{}
	sessions := &scopedSessions{}
	workspaces := newFakeWorkspace()
	service := mustNew(t, Dependencies{
		Engine: &fakeEngine{}, Runtime: runtime, Plugins: &fakePlugins{current: PluginInfo{Name: "default"}},
		Skills: fakeSkills{}, Sessions: sessions, Workspace: workspaces,
	})
	defer service.Shutdown()

	if err := service.BeginNewSession(); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, err := workspaces.Create("project", root, ""); err != nil {
		t.Fatal(err)
	}
	if err := service.BindWorkspace("project-1"); err != nil {
		t.Fatal(err)
	}
	// 工具/工作树仍读进程级 projectScope：绑定后必须有根，否则工作区会话
	// 的所有路径类工具都失效。
	if runtime.projectRoot != root {
		t.Fatalf("project root after BindWorkspace = %q, want %q（工作区绑定未生效；per-session root 能力未实现前不得跳过全局根）", runtime.projectRoot, root)
	}
	if sessions.Workspace() != "project-1" {
		t.Fatalf("router write scope after BindWorkspace = %q, want project-1", sessions.Workspace())
	}
}
