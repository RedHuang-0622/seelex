package core

import (
	"testing"

	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
)

// goal_stack_view_test.go — 钉住「工作台按活动栈分块查看」的**后端投影**面。
//
// 已知缺口（改动前）：治理视图（runtime.goal_governance）只有栈顶一帧，会话里嵌套
// 压栈（新 goal 压栈、栈下目标转 paused）之后，工作台看不到栈上还有什么。
// 这里钉的是投影语义：逐帧、栈底→栈顶、栈顶标记为 active、每帧带自己的内容。

func TestGoalStackFramesProjectsEveryFrame(t *testing.T) {
	stack := []*goaldomain.GoalRecord{
		{
			ID: "g-0", Title: "外层目标", Statement: "先做外层",
			Status: goaldomain.StatusPaused, Acceptance: []string{"a", "b"}, UpdatedAt: 10,
		},
		{
			ID: "g-1", Title: "内层目标", Statement: "再压栈",
			Status: goaldomain.StatusActive,
			Progress: []goaldomain.Progress{
				{At: 1, Kind: goaldomain.ProgressMilestone, Content: "p1"},
				{At: 2, Kind: goaldomain.ProgressFinding, Content: "p2"},
				{At: 3, Kind: goaldomain.ProgressDecision, Content: "p3"},
				{At: 4, Kind: goaldomain.ProgressRisk, Content: "p4"},
			},
		},
	}
	frames := goalStackFrames(stack)
	if len(frames) != 2 {
		t.Fatalf("活动栈应逐帧投影（2 帧），得 %d", len(frames))
	}
	if frames[0].ID != "g-0" || frames[0].Status != string(goaldomain.StatusPaused) || frames[0].Active {
		t.Fatalf("栈下帧应是 paused 且非 active: %+v", frames[0])
	}
	if len(frames[0].Acceptance) != 2 || frames[0].Statement != "先做外层" {
		t.Fatalf("栈下帧应带自己的内容: %+v", frames[0])
	}
	if frames[1].ID != "g-1" || !frames[1].Active || frames[1].Status != string(goaldomain.StatusActive) {
		t.Fatalf("栈顶帧应是 active: %+v", frames[1])
	}
	if len(frames[1].Progress) != 3 || frames[1].Progress[0].Content != "p2" {
		t.Fatalf("每帧进度只取最近 3 条: %+v", frames[1].Progress)
	}
	if len(frames[1].ProgressAll) != 4 || frames[1].ProgressAll[0].Content != "p1" {
		t.Fatalf("ProgressAll 应是这一帧保留的全部打点（详情面用）: %+v", frames[1].ProgressAll)
	}
}

// TestGoalStackFramesCarriesDetailFields 钉住"点开看详情"需要的字段不缺：
// 非目标范围、创建时间、完整打点流水——看板卡片只用得到一部分，属性表要全。
func TestGoalStackFramesCarriesDetailFields(t *testing.T) {
	stack := []*goaldomain.GoalRecord{{
		ID: "g-2", Title: "详情面", Statement: "把该显示的字段都带上",
		Status:     goaldomain.StatusActive,
		Acceptance: []string{"验收 1"},
		OutOfScope: []string{"不做 A"},
		CreatedAt:  100,
		UpdatedAt:  200,
		Progress: []goaldomain.Progress{
			{At: 1, Kind: goaldomain.ProgressMilestone, Content: "只有一条"},
		},
	}}
	frames := goalStackFrames(stack)
	if len(frames) != 1 {
		t.Fatalf("应得到 1 帧，得 %d", len(frames))
	}
	frame := frames[0]
	if len(frame.OutOfScope) != 1 || frame.OutOfScope[0] != "不做 A" {
		t.Fatalf("非目标范围应进投影: %+v", frame)
	}
	if frame.CreatedAt != 100 || frame.UpdatedAt != 200 {
		t.Fatalf("创建/更新时间应进投影: %+v", frame)
	}
	if len(frame.ProgressAll) != 1 || frame.ProgressAll[0].Content != "只有一条" {
		t.Fatalf("完整打点流水应进投影: %+v", frame)
	}
}

func TestGoalStackFramesEmptyAndNilRecords(t *testing.T) {
	if frames := goalStackFrames(nil); frames != nil {
		t.Fatalf("空栈应返回 nil（工作台不渲染空壳）: %+v", frames)
	}
	frames := goalStackFrames([]*goaldomain.GoalRecord{nil, {ID: "g-0", Status: goaldomain.StatusActive}})
	if len(frames) != 1 || frames[0].ID != "g-0" {
		t.Fatalf("nil 帧应被跳过: %+v", frames)
	}
	if !frames[0].Active {
		t.Fatalf("唯一一帧应标记为 active（栈顶）: %+v", frames[0])
	}
}
