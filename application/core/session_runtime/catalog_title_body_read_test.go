package session_runtime

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/core/internal/state"
	"github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/seelexctx"
)

// catalogProbeSessions 是目录刷新探针桩：分别计数两条不同的读路径——
//
//	bodyReads  = LoadHistoryRange（会话历史窗口；物理实现 = 打开 message 分片）
//	headReads  = SessionTitle（header-only；只打开 metadata/message.json）
//	recordReads= LoadSessionRecord（record 通道/派生 record）
//
// 探针用途：坐实"目录刷新 → 标题兜底 → 正文读"调用链（节点 144 baseline），
// 并在修复后作为回归护栏——目录刷新期间正文读必须恒为 0。
type catalogProbeSessions struct {
	*forkTestSessions

	rows    []model.SessionInfo
	record  model.SessionRecord
	history []contract.EngineMessage
	title   string

	bodyReads   int
	headReads   int
	recordReads int
	savedTitles []string
}

func (s *catalogProbeSessions) SessionsOf(string) []model.SessionInfo { return s.rows }

func (s *catalogProbeSessions) LoadHistory(string) ([]contract.EngineMessage, error) {
	return s.history, nil
}

func (s *catalogProbeSessions) LoadHistoryRange(sessionID string, offset, limit int) ([]contract.EngineMessage, int, error) {
	s.bodyReads++
	history := s.history
	if offset > len(history) {
		offset = len(history)
	}
	end := offset + limit
	if limit <= 0 || end > len(history) {
		end = len(history)
	}
	return history[offset:end], len(history), nil
}

func (s *catalogProbeSessions) LoadSessionRecord(string) (model.SessionRecord, error) {
	s.recordReads++
	return s.record, nil
}

// SaveSessionTitle / SessionTitle 实现 session_runtime.SessionTitlePort
// （写穿面 + header-only 只读面）。
func (s *catalogProbeSessions) SaveSessionTitle(sessionID, title string) error {
	s.savedTitles = append(s.savedTitles, sessionID+"="+title)
	return nil
}

func (s *catalogProbeSessions) SessionTitle(projectID, sessionID string) (string, bool, error) {
	s.headReads++
	if s.title == "" {
		return "", false, nil
	}
	return s.title, true, nil
}

// catalogProbeView 是 Snapshot revision bump 桩（目录刷新发布镜像时调用）。
type catalogProbeView struct{ bumps int }

func (v *catalogProbeView) BumpLocked() uint64 {
	v.bumps++
	return uint64(v.bumps)
}

func newCatalogProbeCoordinator(t *testing.T, sessions contract.SessionPort) *Coordinator {
	t.Helper()
	core := state.New(contract.Dependencies{Sessions: sessions, Events: event.NewEventHub()})
	return NewCoordinator(Deps{
		Core:              core,
		View:              &catalogProbeView{},
		Closed:            func() bool { return false },
		IsInternalContent: func(string) bool { return false },
		DisplayUserInput:  func(input string) string { return input },
		Limits:            func() seelexctx.Limits { return seelexctx.Limits{HistoryWindow: 200, SessionNameRunes: 16} },
	})
}

// TestCatalogRefreshResolvesTitlesWithoutBodyReads 是节点 144 的计数探针：
// 目录里 3 个会话都没有标题（旧库/未回填），刷新一轮目录后——
//
//	红线（修复前）：标题兜底 sessionNameFromTail 会为每个会话打开消息体，
//	  bodyReads = 2 × 会话数；
//	绿线（修复后）：标题只来自"枚举行 + 会话头"，正文读必须为 0。
func TestCatalogRefreshResolvesTitlesWithoutBodyReads(t *testing.T) {
	sessions := &catalogProbeSessions{
		forkTestSessions: &forkTestSessions{},
		rows: []model.SessionInfo{
			{ID: "s-1"}, {ID: "s-2"}, {ID: "s-3"},
		},
		// record 存在但无标题（S20 后的真实形态：标题不再随 record 落盘）。
		record:  model.SessionRecord{Version: SessionRecordVersion, ID: "s-1"},
		history: []contract.EngineMessage{{Role: "user", ContentSet: true, Content: "第一轮问题"}},
		title:   "",
	}
	coordinator := newCatalogProbeCoordinator(t, sessions)

	coordinator.refreshCatalogProjects(nil)

	t.Logf("probe: bodyReads=%d headReads=%d recordReads=%d", sessions.bodyReads, sessions.headReads, sessions.recordReads)
	if sessions.bodyReads != 0 {
		t.Fatalf("目录刷新期间读到了会话正文（LoadHistoryRange %d 次，record %d 次，head %d 次）："+
			"标题解析不得打开消息分片", sessions.bodyReads, sessions.recordReads, sessions.headReads)
	}
}

// TestCatalogRefreshBackfillsTitlesFromRows 验证"标题表由枚举行回填"：枚举行
// 自带标题（存储层会话头的目录枚举面）时，一轮目录刷新就把内存标题表补齐，
// 且不产生任何额外读（不读正文、不读 record、不读头）。
func TestCatalogRefreshBackfillsTitlesFromRows(t *testing.T) {
	sessions := &catalogProbeSessions{
		forkTestSessions: &forkTestSessions{},
		rows: []model.SessionInfo{
			{ID: "s-1", Name: "第一轮问题"},
			{ID: "s-2", Name: "另一个第一轮问题"},
		},
	}
	coordinator := newCatalogProbeCoordinator(t, sessions)

	coordinator.refreshCatalogProjects(nil)

	for _, want := range []struct{ id, title string }{{"s-1", "第一轮问题"}, {"s-2", "另一个第一轮问题"}} {
		if got := coordinator.SessionTitleFor(want.id); got.Value != want.title {
			t.Fatalf("SessionTitleFor(%q) = %q, want %q（标题表未由枚举行回填）", want.id, got.Value, want.title)
		}
	}
	if sessions.bodyReads != 0 || sessions.recordReads != 0 || sessions.headReads != 0 {
		t.Fatalf("回填标题产生了额外读：body=%d record=%d head=%d",
			sessions.bodyReads, sessions.recordReads, sessions.headReads)
	}
}

// TestCatalogRefreshHeaderOnlyTitleFallback：枚举行没带标题时只允许 header-only
// 兜底（每个会话一次会话头读），正文读必须为 0。
func TestCatalogRefreshHeaderOnlyTitleFallback(t *testing.T) {
	sessions := &catalogProbeSessions{
		forkTestSessions: &forkTestSessions{},
		rows: []model.SessionInfo{
			{ID: "s-1"}, {ID: "s-2"}, {ID: "s-3"},
		},
		title: "持久化在会话头里的标题",
	}
	coordinator := newCatalogProbeCoordinator(t, sessions)

	coordinator.refreshCatalogProjects(nil)

	if sessions.bodyReads != 0 {
		t.Fatalf("header-only 兜底打开了消息分片：bodyReads=%d", sessions.bodyReads)
	}
	if sessions.headReads != len(sessions.rows) {
		t.Fatalf("header-only 读次数 = %d, want %d（每会话一次会话头读）", sessions.headReads, len(sessions.rows))
	}
	if got := coordinator.SessionTitleFor("s-2").Value; got != "持久化在会话头里的标题" {
		t.Fatalf("SessionTitleFor(s-2) = %q, want header title", got)
	}
}

// TestSetSessionTitleWritesThrough：标题设置即写穿存储（空标题不写：草稿清理
// 路径不得造会话目录）。
func TestSetSessionTitleWritesThrough(t *testing.T) {
	sessions := &catalogProbeSessions{forkTestSessions: &forkTestSessions{}}
	coordinator := newCatalogProbeCoordinator(t, sessions)

	coordinator.SetSessionTitleLocked("s-1", model.SessionTitle{Value: "手动标题", Source: "first_request"})
	coordinator.SetSessionTitleLocked("s-2", model.SessionTitle{})

	if len(sessions.savedTitles) != 1 || sessions.savedTitles[0] != "s-1=手动标题" {
		t.Fatalf("写穿记录 = %v, want [s-1=手动标题]", sessions.savedTitles)
	}
	if got := coordinator.SessionTitleFor("s-2").Value; got != "" {
		t.Fatalf("内存标题表 = %q, want 空（清标题仍生效）", got)
	}
}
