package assemble

// runtime_project_board_test.go — 项目看板的窄可选端口（`TeamworkPlanFor`）的口径钉住。
//
// 这个口是 application/core 的 `TeamworkPlanSource` 在桥这一侧的唯一实现：看板投影取
// teamwork 计划时走它（application/core/project_board.go 的 ProjectBoardTeamPlanFor 从
// Deps.Runtime 上做类型断言后调它）。只钉四件事，多钉一件都是第二份判定：
//   - 没装 teamwork backend / 会话号解析不出作用域 / 该会话没有计划 / 读失败 ⇒
//     `(零值, false)`，不 panic、不上抛（派生投影取数口"I1：可删可重建"的必然要求）；
//   - 有计划的会话 ⇒ 原样返回计划（复用既有的 KeyFor + Store.ReadPlan 读链，不新写第二条）；
//   - 半截键（KeyFor 给出缺项目或缺会话的键）⇒ `false`，不当"有键"用；
//   - nil Runtime / 空会话号（错的输入）⇒ `false`。
//
// 基座复用同目录既有脚手架：newTestRuntime / memPlanStore / teamworkTestBackend /
// boardPlanFixture。

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/seelex/store"
)

func TestTeamworkPlanForRequiresBackendAndScope(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()

	// ① 未装 teamwork：没有计划可给 ⇒ false（有就有、没有就是没有）。
	if plan, ok := r.TeamworkPlanFor("s-team"); ok {
		t.Fatalf("未装 teamwork 时不得报告拿到了计划：%+v", plan)
	}

	st := &memPlanStore{}
	if err := r.SetTeamworkBackend(teamworkTestBackend(st, "s-team")); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}

	// ② 装配了，但该会话还没有计划（memPlanStore 对缺失计划返回错误）⇒ false，不上抛。
	if plan, ok := r.TeamworkPlanFor("s-team"); ok {
		t.Fatalf("没有计划时应返回 false，得到 %+v", plan)
	}
	// ③ KeyFor 解不出的会话号（空串 / 全是空白 / 别的会话）⇒ false（错的输入不 panic）。
	for _, bad := range []string{"", "   ", "s-other"} {
		if plan, ok := r.TeamworkPlanFor(bad); ok {
			t.Fatalf("会话 %q 解析不出作用域时应返回 false，得到 %+v", bad, plan)
		}
	}
}

func TestTeamworkPlanForReturnsStoredPlan(t *testing.T) {
	r := newTestRuntime(t)
	defer r.Shutdown()

	st := &memPlanStore{}
	backend := teamworkTestBackend(st, "s-team")
	if err := r.SetTeamworkBackend(backend); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	plan := boardPlanFixture()
	key, _ := backend.KeyFor("s-team")
	if err := st.WritePlan(context.Background(), key, plan, 6); err != nil {
		t.Fatalf("WritePlan: %v", err)
	}

	got, ok := r.TeamworkPlanFor("s-team")
	if !ok {
		t.Fatal("有计划时必须报告拿到（否则投影永远空）")
	}
	// 计划是**原样搬运**（本口不做任何收口折算）：团队号、版本、成员数、里程碑数与状态逐项相同。
	if got.TeamID != plan.TeamID || got.Version != plan.Version ||
		len(got.Members) != len(plan.Members) || len(got.Milestones) != len(plan.Milestones) {
		t.Fatalf("计划应从既有读链原样取出：期望 %+v，得到 %+v", plan, got)
	}
	if got.Milestones[0].ID != plan.Milestones[0].ID || got.Milestones[0].Status != plan.Milestones[0].Status {
		t.Fatalf("里程碑字段不得被本口改写：期望 %+v，得到 %+v", plan.Milestones[0], got.Milestones[0])
	}
	// 同一份计划再问一次：口径稳定（本口没有缓存，读链每次给同一份）。
	again, ok := r.TeamworkPlanFor("s-team")
	if !ok || again.TeamID != plan.TeamID {
		t.Fatalf("重复取数口径必须稳定：ok=%v plan=%+v", ok, again)
	}
}

// TestTeamworkPlanForRejectsHalfKey：KeyFor 给出半截键 ⇒ false，不当"有键"用。
//
// 为什么单列一条：投影落点是按 (项目, 会话) 分层的（契约 §3），半截键读出来的东西
// 归不到任何一个会话上。口径与同包 coordinatorFor 的显式键校验一致——键完整性只有一份判据。
func TestTeamworkPlanForRejectsHalfKey(t *testing.T) {
	st := &memPlanStore{}
	plan := boardPlanFixture()
	// 半截键下**真的有**一份计划：这样"返回 false"只能是键完整性判据的结果，而不是
	// "存储里恰好没有"。
	if err := st.WritePlan(context.Background(), store.Key{SessionID: "s-half"}, plan, 6); err != nil {
		t.Fatalf("WritePlan: %v", err)
	}

	r := newTestRuntime(t)
	defer r.Shutdown()
	if err := r.SetTeamworkBackend(TeamworkBackend{
		Store: st,
		KeyFor: func(id string) (store.Key, bool) {
			// 故意只给会话：项目分量缺失（半截键）。
			return store.Key{SessionID: id}, true
		},
		MaxTeammates: 6,
	}); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}

	if got, ok := r.TeamworkPlanFor("s-half"); ok {
		t.Fatalf("半截键（缺项目）不得当有键用：plan=%+v", got)
	}
}

func TestTeamworkPlanForNilRuntimeIsFalse(t *testing.T) {
	var r *Runtime
	if plan, ok := r.TeamworkPlanFor("s-team"); ok {
		t.Fatalf("nil Runtime 必须返回 false 而不 panic，得到 %+v", plan)
	}
	if plan, ok := r.TeamworkPlanFor(""); ok {
		t.Fatalf("nil Runtime + 空会话号必须返回 false，得到 %+v", plan)
	}
}
