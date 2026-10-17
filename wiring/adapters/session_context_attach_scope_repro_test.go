package adapters

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	assemble "github.com/RedHuang-0622/seelex/backend/engine/assemble"
	"github.com/RedHuang-0622/seelex/store"
)

// ── 错误复现：会话 context 的落盘键（AttachSessionContext 的装配缺口）──────────
//
// 现场（2026-10-14，dist/seelex-gui-dev/.seelex/sessions-json）：同一个
// session-ff14a72e0e1dc437 同时躺在 project-03b5b8d29af3f44b（会话自己的绑定项目）
// 与 project-e3b0c44298fc1c14（= sha256("")，默认项目；整份 972 行 message 在
// 16:12:10 一次性写进一个新建目录，而它的首行消息 created_at 是 13:02:34）。目录
// 枚举按项目分格 ⇒ 前端把**一个**会话显示成两个，而没有任何 fork 指令。
//
// 这一条钉住其中的装配侧根因：SessionContextStore 是**会话级**存储（system prompt
// /压缩帧/四栈只属于这个会话），但 AttachSessionContext 把入参 workspaceID 整条丢掉、
// 也不注入 resolver，于是 store 回退到 Router 的活跃写作用域 = "用户现在在看哪个
// 项目"。用户在视图里切项目（或后台会话收尾时视图已在别处），后台会话的上下文就写
// 进了视图项目——第二个项目目录从此长出来。
//
// 判别力：装配时故意把**视图项目**当 workspaceID 传下去（现场那条 resume 路径就是
// 这么把 location 传下来的），并让 Router 的活跃写作用域也指向视图项目；写入必须
// 仍然只落在会话自己的项目里。
func TestAttachSessionContextBindsSessionProjectNotViewScope(t *testing.T) {
	root := t.TempDir()
	router, err := store.NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })

	const ownerProject = "ws-owner" // 会话真正归属的项目（workspace 绑定）
	const viewProject = "ws-view"   // 视图当前项目（= Router 活跃写作用域）
	const sessionID = "sess-context-attach-scope"

	if err := router.SaveCommitWorkspace(ownerProject, sessionID, store.Commit{
		Events: []store.Event{{Seq: 1, Role: "user", Content: "owner-side"}},
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	storageRoot := filepath.Join(root, "sessions-json")
	before := sessionScopeProjects(t, storageRoot)

	accounts := filepath.Join(t.TempDir(), "accounts.yaml")
	if err := os.WriteFile(accounts, []byte("roles:\n  agent:\n    - model: test-model\n      base_url: http://localhost\n      api_key: test-key-not-used\n"), 0o644); err != nil {
		t.Fatalf("write accounts: %v", err)
	}
	runtime, err := assemble.NewRuntime(assemble.RuntimeConfig{AccountsPath: accounts})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	t.Cleanup(runtime.Shutdown)
	if _, err := runtime.NewMainSessionWithID(sessionID, nil); err != nil {
		t.Fatalf("NewMainSessionWithID: %v", err)
	}

	port := SessionPort{Manager: NewSessionManager().WithRouter(router), Runtime: runtime}
	// wiring/bootstrap 装配点：会话 → 绑定项目的解析器（workspace.Repo，权威）。
	port.SetWorkspaceResolver(func(sid string) string {
		if sid == sessionID {
			return ownerProject
		}
		return ""
	})

	// 用户在视图里切到了另一个项目：活跃写作用域随之改变，会话归属没变。
	router.SetWorkspace(viewProject)

	// 生产装配：入参给的是**视图项目**（R3 之前的调用口径）。
	if err := port.AttachSessionContext(viewProject, sessionID); err != nil {
		t.Fatalf("AttachSessionContext: %v", err)
	}
	st := runtime.SessionContextStoreFor(sessionID)
	if st == nil {
		t.Fatal("AttachSessionContext 之后 Runtime 里应有本会话的 context store")
	}
	if err := st.SetSystemPrompt("会话级提示：按会话归属落盘"); err != nil {
		t.Fatalf("SetSystemPrompt: %v", err)
	}
	if err := st.PushCompact(store.CompactFrame{
		SegmentID: "seg-1", From: 0, To: 4, Summary: "compressed rounds", CompressedAt: time.Now(),
	}); err != nil {
		t.Fatalf("PushCompact: %v", err)
	}

	after := sessionScopeProjects(t, storageRoot)
	if len(after) != len(before) || !sameStrings(after, before) {
		t.Fatalf("会话上下文落进了第二个项目目录：写入前 %v → 写入后 %v。\n"+
			"同一会话在 %q 与视图项目 %q 各留一份，目录枚举按项目分格 ⇒ 前端把它显示成两个会话（没有 fork 指令）。\n"+
			"装配必须把 store 的落盘键绑到会话自己的项目（绑定 resolver → 入参 workspaceID），而不是 Router 活跃写作用域。",
			before, after, ownerProject, viewProject)
	}
	if len(after) != 1 {
		t.Fatalf("会话 context 只应落在一个项目目录里，实际 %v", after)
	}
}

// sessionScopeProjects 返回存储根下**含会话目录**的项目目录名（project-<hash>）。
// 它不依赖 hash 实现：只回答"这个会话被写进了几个项目"。
func sessionScopeProjects(t *testing.T, storageRoot string) []string {
	t.Helper()
	entries, err := os.ReadDir(storageRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read storage root %q: %v", storageRoot, err)
	}
	var projects []string
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "project-") {
			continue
		}
		sessions, err := os.ReadDir(filepath.Join(storageRoot, entry.Name()))
		if err != nil {
			continue
		}
		for _, candidate := range sessions {
			if candidate.IsDir() && strings.HasPrefix(candidate.Name(), "session-") {
				projects = append(projects, entry.Name())
				break
			}
		}
	}
	sort.Strings(projects)
	return projects
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}
