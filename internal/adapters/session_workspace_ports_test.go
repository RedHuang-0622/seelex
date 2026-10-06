package adapters

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/session"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// TestSessionPortTitleRoundTrip：标题读写面在适配层落地——写穿到真实存储
// （Router → message head 的目录枚举面），读回只读会话头。装配缺方法时应用层
// 的能力断言会静默失败（标题不落盘，目录刷新又退回读正文猜标题），因此这里
// 用真存储验证这条链。
func TestSessionPortTitleRoundTrip(t *testing.T) {
	root := t.TempDir()
	router, err := sessionstore.NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })

	const projectID = "project-adapter"
	const sessionID = "sess-adapter"
	if err := router.SaveCommitWorkspace(projectID, sessionID, sessionstore.Commit{
		Events: []sessionstore.Event{{Seq: 1, Role: "user", Content: "first"}},
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	port := SessionPort{Manager: session.NewManager().WithRouter(router)}
	port.SetWorkspaceResolver(func(string) string { return projectID })
	if err := port.SaveSessionTitle(sessionID, "适配器标题"); err != nil {
		t.Fatalf("SaveSessionTitle: %v", err)
	}
	// 存储层（同一路径）可见：写穿生效。
	if title, ok, err := port.granular().SessionTitle(projectID, sessionID); err != nil || !ok || title != "适配器标题" {
		t.Fatalf("granular title = %q ok=%v err=%v, want 适配器标题", title, ok, err)
	}
	// 端口面（header-only）读回。
	title, ok, err := port.SessionTitle(projectID, sessionID)
	if err != nil || !ok || title != "适配器标题" {
		t.Fatalf("SessionTitle = %q ok=%v err=%v, want 适配器标题", title, ok, err)
	}
}

// TestSessionPortFirstUserInputs：标题回填的有界读面在适配层落地——项目归属
// 按会话绑定解析（R3：不以 Router 活跃写作用域为准），读完只回用户输入正文
// （跳过以 user 身份写盘的注入行）。
func TestSessionPortFirstUserInputs(t *testing.T) {
	root := t.TempDir()
	router, err := sessionstore.NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })

	const projectID = "project-adapter-inputs"
	const sessionID = "sess-adapter-inputs"
	if err := router.SaveCommitWorkspace(projectID, sessionID, sessionstore.Commit{
		Events: []sessionstore.Event{
			{Seq: 1, Role: "user", Kind: sessionstore.EventKindInternal, Content: "<!-- seelex:active-skill:v1 -->"},
			{Seq: 2, Role: "user", Kind: sessionstore.EventKindUserInput, Content: "适配器层的第一问"},
		},
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	port := SessionPort{Manager: session.NewManager().WithRouter(router)}
	port.SetWorkspaceResolver(func(string) string { return projectID })

	inputs, hasLayout, err := port.FirstUserInputs(projectID, sessionID, sessionstore.FirstUserInputProbeRows)
	if err != nil || !hasLayout {
		t.Fatalf("FirstUserInputs hasLayout=%v err=%v", hasLayout, err)
	}
	if len(inputs) != 1 || inputs[0] != "适配器层的第一问" {
		t.Fatalf("FirstUserInputs = %q, want [适配器层的第一问]（注入行不算用户输入）", inputs)
	}
	if _, hasLayout, err := port.FirstUserInputs(projectID, "sess-missing", 0); err != nil || hasLayout {
		t.Fatalf("未落盘会话 hasLayout=%v err=%v, want false", hasLayout, err)
	}
}

// TestAdaptGranularInfosCarriesTimelineFields（左侧栏日期占位回归）：目录
// 枚举摘要从 sessionstore 适配到 model.SessionInfo 时，updated_at 与
// token_count 必须透传。适配层把它们置零会让快照里的 updated_at 恒为
// 0001-01-01T00:00:00Z，前端侧栏每条会话都渲染成同一个占位日期。
func TestAdaptGranularInfosCarriesTimelineFields(t *testing.T) {
	now := time.Date(2026, 9, 2, 10, 30, 0, 0, time.UTC)
	rows := []sessionstore.SessionInfo{
		{ID: "s1", Title: "会话一", Status: sessionstore.StatusIdle, UpdatedAt: now, TokenCount: 42},
	}
	out := adaptGranularInfos(rows)
	if len(out) != 1 {
		t.Fatalf("adapt len = %d, want 1", len(out))
	}
	row := out[0]
	if row.ID != "s1" || row.Name != "会话一" || row.Status.String() != string(sessionstore.StatusIdle) {
		t.Fatalf("identity fields lost: %+v", row)
	}
	if row.UpdatedAt.IsZero() || !row.UpdatedAt.Equal(now) {
		t.Fatalf("updated_at = %v, want %v（不得置零成占位日期）", row.UpdatedAt, now)
	}
	if row.TokenCount != 42 {
		t.Fatalf("token_count = %d, want 42", row.TokenCount)
	}
}
