package board

// teamwork_plan_test.go 是 `TeamworkPlanFor` 的**域级**用例（P21-11：判定随实现搬进看板域后
// 就地钉住；宿主装配的那条链另在 `backend/engine/assemble` 的端到端用例里跑）。

import (
	"context"
	"errors"
	"testing"

	"github.com/RedHuang-0622/seelex/store"
)

func teamworkPlanFixture() store.TeamworkPlan {
	return store.TeamworkPlan{TeamID: "team-1", Version: 3}
}

// TestTeamworkPlanForRejectsIncompleteDeps：两格读数缺一（未装配 / nil 端口）⇒ false，
// 不 panic——宿主可能还没装 teamwork 后端，这条口被问到时必须安静地回"没有"。
func TestTeamworkPlanForRejectsIncompleteDeps(t *testing.T) {
	cases := map[string]TeamworkPlanDeps{
		"两格都缺": {},
		"只给 KeyFor": {
			KeyFor: func(string) (store.Key, bool) { return store.Key{}, false },
		},
		"只给 ReadPlan": {
			ReadPlan: func(context.Context, store.Key) (store.TeamworkPlan, error) {
				return teamworkPlanFixture(), nil
			},
		},
	}
	for name, deps := range cases {
		if plan, ok := TeamworkPlanFor("s-1", deps); ok {
			t.Fatalf("%s：缺装配时必须回 false，得到 %+v", name, plan)
		}
	}
}

// TestTeamworkPlanForRejectsEmptySession：空会话号（错的输入）⇒ false，且**不去碰**两格读数。
func TestTeamworkPlanForRejectsEmptySession(t *testing.T) {
	asked := 0
	deps := TeamworkPlanDeps{
		KeyFor: func(string) (store.Key, bool) { asked++; return store.Key{}, false },
		ReadPlan: func(context.Context, store.Key) (store.TeamworkPlan, error) {
			asked++
			return teamworkPlanFixture(), nil
		},
	}
	for _, bad := range []string{"", "   ", "\t"} {
		if plan, ok := TeamworkPlanFor(bad, deps); ok {
			t.Fatalf("会话 %q 必须回 false，得到 %+v", bad, plan)
		}
	}
	if asked != 0 {
		t.Fatalf("空会话号不该惊动宿主读数，实际读了 %d 次", asked)
	}
}

// TestTeamworkPlanForRejectsHalfKey：键缺项目或缺会话分量 ⇒ false，**读口一次都不许被调**
// （半截键读出来的东西归不到任何一个会话上）。这条判据与 teamwork 域的 coordinatorFor 同源。
func TestTeamworkPlanForRejectsHalfKey(t *testing.T) {
	for name, key := range map[string]store.Key{
		"缺项目":  {SessionID: "s-1"},
		"缺会话":  {ProjectID: "p-1"},
		"只有空白": {ProjectID: "  ", SessionID: "\t"},
	} {
		reads := 0
		deps := TeamworkPlanDeps{
			KeyFor: func(string) (store.Key, bool) { return key, true },
			ReadPlan: func(context.Context, store.Key) (store.TeamworkPlan, error) {
				reads++
				return teamworkPlanFixture(), nil
			},
		}
		if plan, ok := TeamworkPlanFor("s-1", deps); ok {
			t.Fatalf("%s：半截键必须回 false，得到 %+v", name, plan)
		}
		if reads != 0 {
			t.Fatalf("%s：半截键不该去读计划，实际读了 %d 次", name, reads)
		}
	}
}

// TestTeamworkPlanForTreatsReadFailureAndEmptyPlanAlike：读失败与"空计划"同解
// （调用方对两者只做同一件事：整块退场）。**空计划不能被当成真计划**去渲染一张空看板。
func TestTeamworkPlanForTreatsReadFailureAndEmptyPlanAlike(t *testing.T) {
	key := store.Key{ProjectID: "p-1", SessionID: "s-1"}
	cases := map[string]func(context.Context, store.Key) (store.TeamworkPlan, error){
		"读失败": func(context.Context, store.Key) (store.TeamworkPlan, error) {
			return store.TeamworkPlan{}, errors.New("boom")
		},
		"空计划": func(context.Context, store.Key) (store.TeamworkPlan, error) {
			return store.TeamworkPlan{}, nil
		},
	}
	for name, readPlan := range cases {
		deps := TeamworkPlanDeps{
			KeyFor:   func(string) (store.Key, bool) { return key, true },
			ReadPlan: readPlan,
		}
		if plan, ok := TeamworkPlanFor("s-1", deps); ok {
			t.Fatalf("%s：必须回 false，得到 %+v", name, plan)
		}
	}
}

// TestTeamworkPlanForReturnsPlanVerbatim：有计划 ⇒ 原样返回（本口不做任何收口折算），
// 且读口拿到的就是 KeyFor 解析出的那把键（不重算、不新造第二条读链）。
func TestTeamworkPlanForReturnsPlanVerbatim(t *testing.T) {
	key := store.Key{ProjectID: "p-1", SessionID: "s-1"}
	var gotKey store.Key
	deps := TeamworkPlanDeps{
		KeyFor: func(sessionID string) (store.Key, bool) {
			if sessionID != "s-1" {
				t.Fatalf("KeyFor 收到的会话号 = %q, want s-1（归一后）", sessionID)
			}
			return key, true
		},
		ReadPlan: func(_ context.Context, k store.Key) (store.TeamworkPlan, error) {
			gotKey = k
			return teamworkPlanFixture(), nil
		},
	}
	plan, ok := TeamworkPlanFor("  s-1  ", deps)
	if !ok {
		t.Fatal("有计划时必须报告拿到（否则投影永远空）")
	}
	if gotKey != key {
		t.Fatalf("读口收到的键 = %+v, want %+v", gotKey, key)
	}
	if plan.TeamID != "team-1" || plan.Version != 3 {
		t.Fatalf("计划必须原样搬运：%+v", plan)
	}
}
