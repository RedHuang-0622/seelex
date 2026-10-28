package assemble

// runtime_teamwork_scope_test.go 钉住团队面"会话号 → 后端 + 作用域键"的唯一判据：
//   - 未装配 / 回合外调用 / 认不得的会话 / 半截键各有明确出口；
//   - 半截键不再能走到存储层（这是收口前两处只看 ok 的漂移）；
//   - 宿主的 ok 形态与看板域的取数判定对同一份输入给出**同一个**结论（跨实现一致性）。

import (
	"context"
	"errors"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/kernel/board"
	storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"
)

func teamworkScopeFixture() *TeamworkBackend {
	return &TeamworkBackend{
		KeyFor: func(sessionID string) (dto.SessionKey, bool) {
			switch sessionID {
			case "s-good":
				return dto.SessionKey{ProjectID: "p-1", SessionID: sessionID}, true
			case "s-half-project":
				return dto.SessionKey{SessionID: sessionID}, true
			case "s-half-session":
				return dto.SessionKey{ProjectID: "p-1"}, true
			case "s-blank":
				return dto.SessionKey{ProjectID: "  ", SessionID: "\t"}, true
			default:
				return dto.SessionKey{}, false
			}
		},
	}
}

func TestResolveTeamworkScopeJudgment(t *testing.T) {
	var nilRuntime *Runtime
	unassembled := &Runtime{}
	assembled := &Runtime{teamworkBackend: teamworkScopeFixture()}

	cases := []struct {
		name    string
		runtime *Runtime
		session string
		wantErr error // nil = 只要求有错（认不出 / 半截键没有哨兵身份）
		wantOK  bool
	}{
		{name: "runtime 为空", runtime: nilRuntime, session: "s-good", wantErr: errTeamworkNotAssembled},
		{name: "没有会话归属", runtime: assembled, session: "   ", wantErr: errTeamworkNoSession},
		{name: "团队面未装配", runtime: unassembled, session: "s-good", wantErr: errTeamworkNotAssembled},
		{name: "认不得的会话", runtime: assembled, session: "s-unknown"},
		{name: "缺项目分量", runtime: assembled, session: "s-half-project"},
		{name: "缺会话分量", runtime: assembled, session: "s-half-session"},
		{name: "分量只有空白", runtime: assembled, session: "s-blank"},
		{name: "齐全", runtime: assembled, session: "  s-good\t", wantOK: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			backend, _, key, err := c.runtime.resolveTeamworkScope(c.session)
			if c.wantOK {
				if err != nil {
					t.Fatalf("resolveTeamworkScope(%q) 应成功：%v", c.session, err)
				}
				if backend == nil || key.ProjectID != "p-1" || key.SessionID != "s-good" {
					t.Fatalf("解析结果 = %+v / %v", key, backend)
				}
				return
			}
			if err == nil {
				t.Fatalf("resolveTeamworkScope(%q) 必须失败，得 key=%+v", c.session, key)
			}
			if backend != nil || key.Complete() {
				t.Fatalf("失败时必须回零值：backend=%v key=%+v", backend, key)
			}
			if c.wantErr != nil && !errors.Is(err, c.wantErr) {
				t.Fatalf("错误 = %v, want %v", err, c.wantErr)
			}
		})
	}
}

// TestTeamworkKeyJudgmentMatchesBoardDomain 是跨实现一致性用例：同一份 KeyFor 输入同时走
// 宿主的团队作用域解析与看板域的 `TeamworkPlanFor`，两边对"这个会话能不能定位"必须同解。
func TestTeamworkKeyJudgmentMatchesBoardDomain(t *testing.T) {
	r := &Runtime{teamworkBackend: teamworkScopeFixture()}
	readPlan := func(context.Context, dto.SessionKey) (storeteamwork.Plan, error) {
		// 计划非空：这里只比较"键能不能定位"，不让空计划把看板域的结果带偏。
		return storeteamwork.Plan{TeamID: "team-1"}, nil
	}
	sessions := []string{
		"s-good",
		"  s-good  ",
		"s-unknown",
		"s-half-project",
		"s-half-session",
		"s-blank",
		"   ",
	}
	for _, sessionID := range sessions {
		_, hostOK := r.teamworkKeyFor(sessionID)
		_, boardOK := board.TeamworkPlanFor(sessionID, board.TeamworkPlanDeps{
			KeyFor:   r.teamworkKeyFor,
			ReadPlan: readPlan,
		})
		if hostOK != boardOK {
			t.Fatalf("会话 %q：宿主判定 = %v，看板域判定 = %v（判据漂移）", sessionID, hostOK, boardOK)
		}
	}
}
