package core

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// input_team_suggestions_repro_test.go — `@`（手动召唤团队）的建议面复现：
// 用户打 @ 时下面列不出任何已注册团队（团队库里有 goal-a2a，面板却是空的）。
//
// 两件事一起钉：候选来自**全局团队库**；且候选的来源不能变成"每次按键读一次盘"
// （Suggestions 跑在 TUI 的 View() 与 GUI 每次输入事件上）。因此这里除了断言候选
// 内容，还数了一次"建议面读了几次库"。

// libraryTeam 造一条团队库条目（team_id + 展示名 + 一个员工）。
func libraryTeam(teamID, name string) dto.TeamLibraryEntry {
	return dto.TeamLibraryEntry{
		TeamID: teamID, TeamKind: teamID, Name: name,
		Roles: []dto.RoleSpec{{RoleName: "auditor"}},
	}
}

// countingLibrarySessions 在既有内存母本上加一个读计数：建议面"逐键不读盘"的
// 判据只能看读次数（否则一个缓慢但正确的实现也能过）。
type countingLibrarySessions struct {
	*librarySessions
	reads int
}

func (s *countingLibrarySessions) ReadTeamLibrary(sessionID string) (dto.TeamLibrary, error) {
	s.reads++
	return s.librarySessions.ReadTeamLibrary(sessionID)
}

// suggestionTexts 取候选的 Text 列表（断言顺序无关的包含关系）。
func suggestionTexts(suggestions []Suggestion) []string {
	texts := make([]string, 0, len(suggestions))
	for _, suggestion := range suggestions {
		texts = append(texts, suggestion.Text)
	}
	return texts
}

func TestSuggestionsListTeamLibraryAndFollowLibraryWrites(t *testing.T) {
	sessions := &countingLibrarySessions{librarySessions: newLibrarySessions()}
	sessions.setLibrary(dto.TeamLibrary{
		Configured: true,
		Teams:      []dto.TeamLibraryEntry{libraryTeam("goal-a2a", "改良 goal-a2a")},
	})
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))

	// `@` 要列出库里那支团队（kind = team，Text = team_id = 用户接下来要打的字）。
	addressed := service.Suggestions(SigilTeam)
	if len(addressed) != 1 || addressed[0].Text != "goal-a2a" || addressed[0].Kind != SuggestionKindTeam {
		t.Fatalf("`@` 应列出团队库条目：%#v", addressed)
	}
	if !strings.Contains(addressed[0].Description, "改良 goal-a2a") {
		t.Fatalf("候选描述应带展示名：%#v", addressed[0])
	}
	if sessions.reads != 1 {
		t.Fatalf("首次取候选应只读一次库：reads=%d", sessions.reads)
	}

	// `@` 加前缀要能前缀过滤。
	if hit := service.Suggestions(SigilTeam + "go"); len(hit) != 1 || hit[0].Text != "goal-a2a" {
		t.Fatalf("`@go` 应命中 goal-a2a：%#v", hit)
	}
	// 展示名（用户起的名字）同样是个可命中的键：`@改良` 也要弹出这一行，而插进
	// 输入框的 Text 仍是 team_id（可召唤、且不会因为名字含空格把输入推进参数区）。
	if byName := service.Suggestions(SigilTeam + "改良"); len(byName) != 1 || byName[0].Text != "goal-a2a" {
		t.Fatalf("`@改良` 应按展示名命中 goal-a2a：%#v", byName)
	}
	if miss := service.Suggestions(SigilTeam + "zz"); len(miss) != 0 {
		t.Fatalf("前缀不命中时不该有候选：%#v", miss)
	}
	// 逐键提示：上面几次调用都不该再去读盘（候选已在进程内缓存）。
	if sessions.reads != 1 {
		t.Fatalf("逐键提示不该重复读库：reads=%d", sessions.reads)
	}

	// 进入参数区（含空格）仍然不弹面板——即使前缀能命中库里的名字。
	if panel := service.Suggestions(SigilTeam + "goal-a2a 看看这个 bug"); panel != nil {
		t.Fatalf("参数区不该给建议：%#v", panel)
	}

	// 写库后建议面立刻反映（缓存失效，而不是等重启）：写路径自己读写库的次数不计，
	// 建议面必须**重取一次**新库，随后又回到缓存路径（不再读盘）。
	if _, err := service.AgentTeamSaveTeam("main-1", libraryTeam("audit-team", "审计小队")); err != nil {
		t.Fatalf("AgentTeamSaveTeam: %v", err)
	}
	refill := sessions.reads
	after := suggestionTexts(service.Suggestions(SigilTeam))
	if len(after) != 2 || !strings.Contains(strings.Join(after, ","), "audit-team") {
		t.Fatalf("写库后建议面应含新团队：%v", after)
	}
	if sessions.reads != refill+1 {
		t.Fatalf("写库后建议面应重取一次库：reads=%d, want %d", sessions.reads, refill+1)
	}
	if again := service.Suggestions(SigilTeam); sessions.reads != refill+1 || len(again) != 2 {
		t.Fatalf("重取后的快照应命中缓存：reads=%d %#v", sessions.reads, again)
	}

	// 删库后同理（缓存不能停在旧库上）。
	if _, err := service.AgentTeamDeleteTeam("main-1", "goal-a2a"); err != nil {
		t.Fatalf("AgentTeamDeleteTeam: %v", err)
	}
	refill = sessions.reads
	after = suggestionTexts(service.Suggestions(SigilTeam))
	if len(after) != 1 || after[0] != "audit-team" {
		t.Fatalf("删库后建议面应只剩 audit-team：%v", after)
	}
	if sessions.reads != refill+1 {
		t.Fatalf("删库后建议面应重取一次库：reads=%d, want %d", sessions.reads, refill+1)
	}
}

// perSessionLibrarySessions 让"哪个会话问的"决定读到哪份库：真实端口按锚定会话
// 解析数据根（团队库是全局母表，旧的**项目级**布局回退按锚定会话定位项目），
// 所以两份库本来就可能不同。
type perSessionLibrarySessions struct {
	*librarySessions
	bySession map[string]dto.TeamLibrary
}

func (s *perSessionLibrarySessions) ReadTeamLibrary(sessionID string) (dto.TeamLibrary, error) {
	if library, ok := s.bySession[sessionID]; ok {
		return library, nil
	}
	return s.librarySessions.ReadTeamLibrary(sessionID)
}

// TestSuggestionsScopeTeamLibraryPerSession 钉住快照的键是**会话**：切到另一侧的
// 会话时，`@` 读到的必须是那一侧解析出来的库，而不是上一个会话缓存下来的那份。
func TestSuggestionsScopeTeamLibraryPerSession(t *testing.T) {
	sessions := &perSessionLibrarySessions{
		librarySessions: newLibrarySessions(),
		bySession: map[string]dto.TeamLibrary{
			"sess-a": {Configured: true, Teams: []dto.TeamLibraryEntry{libraryTeam("team-a", "A 队")}},
			"sess-b": {Configured: true, Teams: []dto.TeamLibraryEntry{libraryTeam("team-b", "B 队")}},
		},
	}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))
	switchView := func(sessionID string) {
		service.ViewMu.Lock()
		service.Core.Snapshot.Session.ID = sessionID
		service.ViewMu.Unlock()
	}

	switchView("sess-a")
	if got := suggestionTexts(service.Suggestions(SigilTeam)); len(got) != 1 || got[0] != "team-a" {
		t.Fatalf("sess-a 的 `@` 应列出 A 侧的库：%v", got)
	}
	switchView("sess-b")
	if got := suggestionTexts(service.Suggestions(SigilTeam)); len(got) != 1 || got[0] != "team-b" {
		t.Fatalf("切到 sess-b 后 `@` 应列出 B 侧的库（不是上一个会话缓存的那份）：%v", got)
	}
}

// TestSuggestionsTeamEmptyLibraryStaysQuiet 钉住空库与读不到的边界：`@` 整列为空
// （不弹面板）且不报错——"库里一支团队都没有"是合法状态，不是失败。
func TestSuggestionsTeamEmptyLibraryStaysQuiet(t *testing.T) {
	if got := newTestService(t, &fakeEngine{}, withTestSessions(newLibrarySessions())).
		Suggestions(SigilTeam); len(got) != 0 {
		t.Fatalf("空库时 `@` 不该有候选：%#v", got)
	}
	// 宿主没装配团队库读面（端口不实现 agentTeamLibraryPort）同样安静。
	if got := newTestService(t, &fakeEngine{}, withTestSessions(&teamRecordingSessions{})).
		Suggestions(SigilTeam); len(got) != 0 {
		t.Fatalf("读不到库时 `@` 不该有候选：%#v", got)
	}
}
