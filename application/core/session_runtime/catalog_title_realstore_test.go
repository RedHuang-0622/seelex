package session_runtime

import (
	"path/filepath"
	"testing"

	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// realStoreTitleSessions 把标题三面接到真存储（Router/SessionGranularStore）：
// SessionsOf/FirstUserInputs/SessionTitle/SaveSessionTitle 全部直达存储，其余
// contract.SessionPort 面由 forkTestSessions 兜底。用它验证"目录刷新 → 标题解析"
// 这条链在真实存储上端到端成立（不只是桩）。
type realStoreTitleSessions struct {
	*forkTestSessions
	granular *sessionstore.SessionGranularStore
	project  string
}

func (s *realStoreTitleSessions) SessionsOf(projectID string) []model.SessionInfo {
	infos, err := s.granular.SessionsOf(projectID)
	if err != nil {
		return nil
	}
	rows := make([]model.SessionInfo, 0, len(infos))
	for _, info := range infos {
		rows = append(rows, model.SessionInfo{ID: info.ID, Name: info.Title, Status: model.SessionStatus(info.Status)})
	}
	return rows
}

func (s *realStoreTitleSessions) SessionTitle(projectID, sessionID string) (string, bool, error) {
	return s.granular.SessionTitle(projectID, sessionID)
}

func (s *realStoreTitleSessions) SaveSessionTitle(sessionID, title string) error {
	return s.granular.SaveSessionTitle(s.granular.ResolveProjectForSession(sessionID), sessionID, title)
}

func (s *realStoreTitleSessions) FirstUserInputs(projectID, sessionID string, limit int) ([]string, bool, error) {
	return s.granular.FirstUserInputs(projectID, sessionID, limit)
}

// TestCatalogRefreshBackfillsLegacyTitleOnRealStore（真存储端到端）：标题写穿
// 上线前的会话（枚举行与会话头都没有标题 → 侧栏只能显示会话 ID）在一轮目录
// 刷新后恢复"用户第一问"标题，并写穿进会话头——第二轮刷新直接读会话头。
//
// 这正是用户看到的现象（左侧列表全变成 ID 前缀）：标题解析只能从存储里的
// **首条用户输入**重建，且不能为此每轮读整片正文。
func TestCatalogRefreshBackfillsLegacyTitleOnRealStore(t *testing.T) {
	root := t.TempDir()
	router, err := sessionstore.NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })

	const projectID = "project-titles"
	const sessionID = "sess-legacy"
	const question = "老会话的用户第一问"
	// 老会话形态：首行是"以 user 身份写盘"的内部注入行，第二行才是真用户输入；
	// 会话头里没有标题（S20 之前落盘的会话都是这样）。
	if err := router.SaveCommitWorkspace(projectID, sessionID, sessionstore.Commit{
		Events: []sessionstore.Event{
			{Seq: 1, Role: "user", Kind: sessionstore.EventKindInternal, Content: "<!-- seelex:active-skill:v1 -->\n## Trusted Active Skill: goal"},
			{Seq: 2, Role: "user", Kind: sessionstore.EventKindUserInput, Content: question},
			{Seq: 3, Role: "assistant", Kind: sessionstore.EventKindLLM, Content: "回答"},
		},
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	granular := sessionstore.NewSessionGranularStore(router)
	// 生产装配（main.go）：会话绑定由 workspace.Repo 解析；标题写穿按绑定落盘。
	granular.SetWorkspaceResolver(func(string) string { return projectID })

	// 前置：复现"侧栏显示会话 ID"的老库形态（枚举行无标题、会话头无标题）。
	infos, err := granular.SessionsOf(projectID)
	if err != nil || len(infos) != 1 {
		t.Fatalf("seed enumeration = %+v err=%v", infos, err)
	}
	if infos[0].Title != "" {
		t.Fatalf("fixture 已带标题（%q），复现不了老库形态", infos[0].Title)
	}
	if title, ok, err := granular.SessionTitle(projectID, sessionID); err != nil || !ok || title != "" {
		t.Fatalf("fixture 会话头标题 = %q ok=%v err=%v, want 空", title, ok, err)
	}

	port := &realStoreTitleSessions{
		forkTestSessions: &forkTestSessions{},
		granular:         granular,
		project:          projectID,
	}
	coordinator := newCatalogProbeCoordinator(t, port)

	rows, _ := coordinator.sessionCatalogProject(port, projectID)
	if len(rows) != 1 || rows[0].Name != question {
		t.Fatalf("目录行 = %+v, want 标题 %q（用户第一问）", rows, question)
	}
	// 内存标题表由 refreshCatalogProjects 的"枚举行回填"一步刷新（这里显式走
	// 同一步，等价于目录 worker 的一轮刷新）。
	coordinator.backfillCatalogTitles(map[string][]model.SessionInfo{projectID: rows})
	if got := coordinator.SessionTitleFor(sessionID).Value; got != question {
		t.Fatalf("标题表 = %q, want %q", got, question)
	}
	// 写穿：会话头（目录枚举面）带标题；此后 header-only 读回即可。
	title, ok, err := granular.SessionTitle(projectID, sessionID)
	if err != nil || !ok || title != question {
		t.Fatalf("写穿后会话头标题 = %q ok=%v err=%v, want %q", title, ok, err, question)
	}
	// 第二轮：标题来自存储枚举面（无需再回填），目录行仍是同一标题。
	rows, _ = coordinator.sessionCatalogProject(port, projectID)
	if len(rows) != 1 || rows[0].Name != question {
		t.Fatalf("第二轮目录行 = %+v, want 标题 %q", rows, question)
	}
}
