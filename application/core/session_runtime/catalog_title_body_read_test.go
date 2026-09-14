package session_runtime

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/core/internal/state"
	"github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/seelexctx"
)

// catalogProbeSessions 是目录刷新探针桩：分别计数三条不同的读路径——
//
//	bodyReads   = LoadHistoryRange（会话历史窗口；物理实现 = 全量打开 message 分片）
//	headReads   = SessionTitle（header-only；只打开 metadata/message.json）
//	recordReads = LoadSessionRecord（record 通道/派生 record）
//	inputProbes = FirstUserInputs（标题回填；有界读 = 只打开首个 message 分片）
//
// 探针用途：坐实"目录刷新 → 标题解析"的调用链（节点 144 baseline），并在修复后
// 作为回归护栏——目录刷新期间**正文读（全量历史）必须恒为 0**；只有"枚举行与会话
// 头都没有标题"的老会话才允许一次有界回填（inputProbes，每会话一次）。
type catalogProbeSessions struct {
	*forkTestSessions

	rows    []model.SessionInfo
	record  model.SessionRecord
	history []contract.EngineMessage
	title   string
	// inputs 是标题回填看到的最早用户输入（有界读面返回的候选）；hasLayout
	// 对应"会话已有已发布行"。
	inputs    []string
	hasLayout bool

	bodyReads   int
	headReads   int
	recordReads int
	inputProbes int
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

// FirstUserInputs 实现 session_runtime.FirstUserInputPort（标题回填的有界读面）。
func (s *catalogProbeSessions) FirstUserInputs(projectID, sessionID string, limit int) ([]string, bool, error) {
	s.inputProbes++
	return append([]string(nil), s.inputs...), s.hasLayout, nil
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

// catalogNoInputPortSessions 与探针桩同形，但**不实现** FirstUserInputPort
// （宿主未落地有界读面）：目录面必须维持零正文读，标题留空交给前端回退——
// 绝不在应用层"猜"标题。
type catalogNoInputPortSessions struct {
	*forkTestSessions
	rows        []model.SessionInfo
	headReads   int
	savedTitles []string
}

func (s *catalogNoInputPortSessions) SessionsOf(string) []model.SessionInfo { return s.rows }

func (s *catalogNoInputPortSessions) SessionTitle(projectID, sessionID string) (string, bool, error) {
	s.headReads++
	return "", false, nil
}

func (s *catalogNoInputPortSessions) SaveSessionTitle(sessionID, title string) error {
	s.savedTitles = append(s.savedTitles, sessionID+"="+title)
	return nil
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
// 目录里 3 个会话都没有标题（老库：标题写穿上线前落盘）——刷新一轮目录后
//
//	红线（修复前）：标题兜底 sessionNameFromTail 会为每个会话打开消息体，
//	  bodyReads = 2 × 会话数（长会话还是全量读）；
//	绿线（修复后）：标题只来自"枚举行 + 会话头 + 一次性有界回填"，正文读必须为 0，
//	  且回填只走有界读面（inputProbes = 每会话一次）、结果写穿会话头。
func TestCatalogRefreshResolvesTitlesWithoutBodyReads(t *testing.T) {
	sessions := &catalogProbeSessions{
		forkTestSessions: &forkTestSessions{},
		rows: []model.SessionInfo{
			{ID: "s-1"}, {ID: "s-2"}, {ID: "s-3"},
		},
		// record 存在但无标题（S20 后的真实形态：标题不再随 record 落盘）。
		record:    model.SessionRecord{Version: SessionRecordVersion, ID: "s-1"},
		history:   []contract.EngineMessage{{Role: "user", ContentSet: true, Content: "第一轮问题"}},
		title:     "",
		inputs:    []string{"第一轮问题"},
		hasLayout: true,
	}
	coordinator := newCatalogProbeCoordinator(t, sessions)

	coordinator.refreshCatalogProjects(nil)

	t.Logf("probe: bodyReads=%d headReads=%d recordReads=%d inputProbes=%d",
		sessions.bodyReads, sessions.headReads, sessions.recordReads, sessions.inputProbes)
	if sessions.bodyReads != 0 {
		t.Fatalf("目录刷新期间读到了会话正文（LoadHistoryRange %d 次，record %d 次，head %d 次）："+
			"标题解析不得打开全量历史", sessions.bodyReads, sessions.recordReads, sessions.headReads)
	}
	if sessions.inputProbes != len(sessions.rows) {
		t.Fatalf("有界回填探针 = %d 次, want %d（每会话一次）", sessions.inputProbes, len(sessions.rows))
	}
}

// TestCatalogRefreshBackfillsTitleFromStoredInput：老会话（枚举行与会话头都没有
// 标题）的标题由**一次性有界回填**重建：存储里最早的用户输入 → 标题（displayUserInput
// + SessionTitle 归一化）→ 写穿会话头。回填后同进程不再探（记忆），且目录行与
// 标题表都拿到"用户第一问"。
func TestCatalogRefreshBackfillsTitleFromStoredInput(t *testing.T) {
	sessions := &catalogProbeSessions{
		forkTestSessions: &forkTestSessions{},
		rows: []model.SessionInfo{
			{ID: "s-1"}, {ID: "s-2"},
		},
		title:     "",
		inputs:    []string{"  用户输入的第一问  "},
		hasLayout: true,
	}
	coordinator := newCatalogProbeCoordinator(t, sessions)

	coordinator.refreshCatalogProjects(nil)

	rows, _ := coordinator.CatalogCache()
	for _, row := range rows {
		if row.Name != "用户输入的第一问" {
			t.Fatalf("目录行 %s 标题 = %q, want 用户第一问", row.ID, row.Name)
		}
	}
	if got := coordinator.SessionTitleFor("s-1").Value; got != "用户输入的第一问" {
		t.Fatalf("标题表 s-1 = %q, want 用户第一问（枚举行回填）", got)
	}
	// 写穿会话头：下一次目录刷新（乃至重启）零正文读。
	if len(sessions.savedTitles) != 2 {
		t.Fatalf("写穿标题 = %v, want 两会话各一次", sessions.savedTitles)
	}
	for _, saved := range sessions.savedTitles {
		if !strings.HasSuffix(saved, "=用户输入的第一问") {
			t.Fatalf("写穿标题内容 = %q, want 用户第一问", saved)
		}
	}
	if sessions.bodyReads != 0 {
		t.Fatalf("回填走了全量历史读：bodyReads=%d", sessions.bodyReads)
	}

	// 第二轮刷新：探针结论已记忆，不再重复探（每会话一次）。
	probes := sessions.inputProbes
	coordinator.refreshCatalogProjects(nil)
	if sessions.inputProbes != probes {
		t.Fatalf("第二轮又探了 %d 次（共 %d），want 只探一次", sessions.inputProbes-probes, sessions.inputProbes)
	}
}

// TestCatalogRefreshTitleBackfillRetriesWhenNoRowsYet：新会话尚未落盘（没有已发布
// 行）时存储答不上来，回填不得把"暂时没有答案"记成负结论——下一轮刷新必须重试，
// 否则该会话标题要等重启才补。
func TestCatalogRefreshTitleBackfillRetriesWhenNoRowsYet(t *testing.T) {
	sessions := &catalogProbeSessions{
		forkTestSessions: &forkTestSessions{},
		rows:             []model.SessionInfo{{ID: "s-new"}},
		title:            "",
		hasLayout:        false,
	}
	coordinator := newCatalogProbeCoordinator(t, sessions)

	coordinator.refreshCatalogProjects(nil)
	first := sessions.inputProbes
	if first == 0 {
		t.Fatal("未落盘会话没有被探过")
	}
	// 会话落盘（有用户输入）后下一轮刷新补上标题。
	sessions.hasLayout = true
	sessions.inputs = []string{"刚发出的第一问"}
	coordinator.refreshCatalogProjects(nil)
	rows, _ := coordinator.CatalogCache()
	if len(rows) != 1 || rows[0].Name != "刚发出的第一问" {
		t.Fatalf("第二轮目录行 = %+v, want 标题补上", rows)
	}
}

// TestCatalogRefreshWithoutInputPortKeepsTitlesEmpty：宿主未落地有界读面
// （FirstUserInputPort 缺失）时目录面维持零正文读，标题留空交给前端回退——
// 应用层不猜标题，也不回退成"读正文猜标题"。
func TestCatalogRefreshWithoutInputPortKeepsTitlesEmpty(t *testing.T) {
	sessions := &catalogNoInputPortSessions{
		forkTestSessions: &forkTestSessions{},
		rows:             []model.SessionInfo{{ID: "s-1"}, {ID: "s-2"}},
	}
	coordinator := newCatalogProbeCoordinator(t, sessions)

	coordinator.refreshCatalogProjects(nil)

	rows, _ := coordinator.CatalogCache()
	for _, row := range rows {
		if row.Name != "" {
			t.Fatalf("无端口时目录行 %s 标题 = %q, want 空（不猜标题）", row.ID, row.Name)
		}
	}
	if sessions.headReads != len(sessions.rows) {
		t.Fatalf("会话头读次数 = %d, want %d", sessions.headReads, len(sessions.rows))
	}
	if len(sessions.savedTitles) != 0 {
		t.Fatalf("无端口时发生了标题写穿 %v, want 无", sessions.savedTitles)
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
