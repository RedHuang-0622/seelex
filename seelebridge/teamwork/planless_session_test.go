package teamwork

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/store"
)

// 还没声明团队的会话：**读面给空结果，写面给领域文案**（2026-10-15 现场回归）。
//
// 现场：模型在一个没有团队计划的会话里调 team_items / team_context，收到的是
// `team_items: open .seelex\sessions-json\project-…\metadata\teamwork.json: The system
// cannot find the file specified.`——一条内部路径噪声；模型自己判定"这是错误，不是空结果"。
//
// 这条用例钉住修复后的形状：读面（Items）空集不报错，写面（排活/派发/验收/判失败/收口）
// 给的是同一句"本会话还没有团队计划——先用 team_plan 声明在编成员与里程碑"。
func TestPlanlessSessionReadsEmptyAndWritesSpeakDomain(t *testing.T) {
	fixture := newFixture(t, 6)
	ctx := context.Background()

	// 读面：空计划 = 空结果，不是错误。
	items, err := fixture.coordinator.Items(ctx)
	if err != nil {
		t.Fatalf("没有计划时 team_items 必须给空结果：%v", err)
	}
	if len(items) != 0 {
		t.Fatalf("没有计划时应为空集，得到 %d 条", len(items))
	}
	if report, err := fixture.coordinator.Recover(ctx); err != nil || report.Items != 0 {
		t.Fatalf("没有计划时恢复读数必须是零值且不报错：report=%+v err=%v", report, err)
	}

	// 写面：同一个判据（requireTeam）在所有入口给出同一句领域文案。
	writes := []struct {
		name string
		run  func() error
	}{
		{"team_work（排活）", func() error {
			return fixture.coordinator.PlanMilestone(ctx, "m-1", []WorkItemSpec{{ID: "wi-1", Role: "design", Name: "x"}})
		}},
		{"team_dispatch(item)", func() error {
			_, err := fixture.coordinator.DispatchItem(ctx, "wi-1")
			return err
		}},
		{"team_item（调整）", func() error {
			return fixture.coordinator.AdjustItem(ctx, "wi-1", WorkItemSpec{Name: "x"})
		}},
		{"team_accept（验收）", func() error {
			return fixture.coordinator.AcceptItem(ctx, "wi-1", "note")
		}},
		{"team_fail（判失败）", func() error {
			return fixture.coordinator.FailItem(ctx, "wi-1", "note")
		}},
		{"team_dispatch(role)", func() error {
			_, err := fixture.coordinator.Dispatch(ctx, "design", "goal")
			return err
		}},
		{"team_milestone（声明里程碑）", func() error {
			return fixture.coordinator.Milestone(ctx, "m-1", "content")
		}},
		{"team_close（收口）", func() error {
			_, err := fixture.coordinator.Close(ctx)
			return err
		}},
	}
	for _, write := range writes {
		err := write.run()
		if !errors.Is(err, errNoPlan) {
			t.Fatalf("%s 必须报「还没有团队计划」这一句（errNoPlan）：%v", write.name, err)
		}
		if !strings.Contains(err.Error(), "team_plan") {
			t.Fatalf("%s 的文案必须指出下一步（team_plan）：%v", write.name, err)
		}
	}
}

// 判据只有一处：IsEmpty 是"还没组队"的唯一判据，空计划之外的计划一律不算空
// （哪怕它没有里程碑、没有成员）——否则收口/排活的入口会被误挡。
func TestRequireTeamOnlyRefusesUndeclaredTeam(t *testing.T) {
	fixture := newFixture(t, 6)
	ctx := context.Background()
	// 声明团队但一份活都不排：这**不是**"还没组队"，入口不得报 errNoPlan。
	if err := fixture.coordinator.SetPlan(ctx, store.TeamworkPlan{
		TeamID:     "probe-team",
		Version:    1,
		Members:    []store.TeamworkMember{{Role: "design"}},
		Milestones: []store.TeamworkMilestone{{ID: "m-1", Name: "设计"}},
	}); err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	if items, err := fixture.coordinator.Items(ctx); err != nil {
		t.Fatalf("已声明团队但还没排活：读面必须给空结果而不是错误：%v", err)
	} else if len(items) != 0 {
		t.Fatalf("还没排活时工作项应为空集，得到 %d 条", len(items))
	}
	if err := fixture.coordinator.Milestone(ctx, "m-1", "content"); err != nil {
		t.Fatalf("已声明团队时不得报「还没有团队计划」：%v", err)
	}
}
