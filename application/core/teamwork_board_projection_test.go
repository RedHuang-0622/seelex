package core

// teamwork_board_projection_test.go — 团队看板在**应用层会话快照**里的接线（契约
// docs/arch/team-board-gui-tui-contract.md §3）。
//
// 为什么这一层要单独钉：桥侧（seelebridge）的组装已经有自己的用例，模型层的深拷贝
// 也有；但"装配探测 → view_state 收集 → SessionRuntime 槽位 → JSON 键"这三跳只要有一跳
// 断了，GUI/TUI 拿到的就永远是 undefined（面板不出现，且没有任何报错）。

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// teamBoardRuntime 给测试桩补上**窄可选**能力面：Runtime 实现它，投影里就有团队看板；
// 不实现它的桩（默认 fakeRuntime）走"未装配"分支——两条路径都要有用例。
type teamBoardRuntime struct {
	*fakeRuntime
	board *dto.TeamworkBoardView
}

func (r teamBoardRuntime) TeamworkBoardSnapshot(string) *dto.TeamworkBoardView { return r.board }

func teamBoardFixture() *dto.TeamworkBoardView {
	return &dto.TeamworkBoardView{
		TeamID:     "team-board-gui-tui",
		Version:    2,
		MaxMembers: 6,
		Members:    []dto.TeamworkMemberView{{Role: "arch", RoleSessionID: "s-arch", Status: "running"}},
		Milestones: []dto.TeamworkMilestoneView{
			{ID: "m-1", Name: "设计", DependsOn: []string{"m-0"}},
		},
		WorkItems: []dto.TeamworkWorkItemView{
			{ID: "wi-impl", Milestone: "m-1", Role: "impl_ui", DependsOn: []string{"wi-design"}},
		},
		Jobs:   []dto.TeamworkJobView{{Handle: "a7", State: "running", Node: "wi-impl"}},
		Events: []dto.TeamworkEventView{{At: 1790870000, Kind: "plan"}},
	}
}

func TestCollectRuntimeProjectionCarriesTeamworkBoard(t *testing.T) {
	board := teamBoardFixture()
	svc := newTestService(t, &fakeEngine{}, withTestRuntime(teamBoardRuntime{fakeRuntime: &fakeRuntime{}, board: board}))

	projection := svc.collectRuntimeProjection(t.Context())

	got := projection.Runtime.TeamworkBoard
	if got == nil {
		t.Fatal("Runtime 实现了窄接口，投影里就必须有团队看板")
	}
	if got.TeamID != "team-board-gui-tui" || len(got.Milestones) != 1 || len(got.WorkItems) != 1 || got.MaxMembers != 6 {
		t.Fatalf("投影搬运不一致：%+v", got)
	}

	// JSON 键名是前后端唯一的对接口径（gui/frontend/dist/snapshot-shape.js 登记的
	// SESSION_RUNTIME_KEYS 就是它）。
	encoded, err := json.Marshal(projection.Runtime)
	if err != nil {
		t.Fatalf("encode runtime: %v", err)
	}
	if !strings.Contains(string(encoded), `"teamwork_board"`) {
		t.Fatalf("会话运行原件必须以 teamwork_board 为键下发：%s", encoded)
	}
	if !strings.Contains(string(encoded), `"max_members":6`) {
		t.Fatalf("在编上限必须下发（看板要能显示 在编 n/max）：%s", encoded)
	}

	// 冻结契约：快照是并发读者的共享值，克隆必须独立（内嵌切片也要）。
	cloned := cloneRuntimeState(projection.Runtime)
	cloned.TeamworkBoard.WorkItems[0].DependsOn[0] = "mutated"
	if projection.Runtime.TeamworkBoard.WorkItems[0].DependsOn[0] != "wi-design" {
		t.Fatal("clone 必须深拷贝团队看板（否则前端会读到一半的写）")
	}
}

func TestCollectRuntimeProjectionOmitsTeamworkBoardWithoutPort(t *testing.T) {
	// 未装配 teamwork 的宿主（默认 fakeRuntime 没实现窄接口）：投影留空，
	// 且 **JSON 里不出现这个键**——前端据此整块退场，而不是渲染一个空壳。
	svc := newTestService(t, &fakeEngine{})

	projection := svc.collectRuntimeProjection(t.Context())
	if projection.Runtime.TeamworkBoard != nil {
		t.Fatalf("未装配时不得下发团队看板：%+v", projection.Runtime.TeamworkBoard)
	}
	encoded, err := json.Marshal(projection.Runtime)
	if err != nil {
		t.Fatalf("encode runtime: %v", err)
	}
	if strings.Contains(string(encoded), "teamwork_board") {
		t.Fatalf("未装配时连键都不该出现（omitempty）：%s", encoded)
	}
}

func TestTeamworkBoardViewForWithoutPort(t *testing.T) {
	// Service 侧转发面的降温路径：Runtime 不是 TeamworkBoardProjection → nil。
	svc := newTestService(t, &fakeEngine{})
	if view := svc.TeamworkBoardViewFor("session-a"); view != nil {
		t.Fatalf("未装配时转发面必须返回 nil，得到 %+v", view)
	}
}
