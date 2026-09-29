package core

import (
	"context"
	"errors"
	"strings"
	"testing"

	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
)

// goal_permission_test.go — goal 变更/取消的权限口径（2026-09-29）。
//
// 口径：**goal 的修改与取消只有 ADVISOR(TL) 裁决侧有资格**。
//
//	动作                             | agent 工具面                                  | TL 裁决侧 | 人类/运维面
//	追加进度（progress_*）           | 允许                                          | —         | 允许
//	改定义（标题/正文/完成条件/范围） | 拒绝（本用例①）                                | 允许      | 允许（本用例③）
//	取消（finish/abort）             | 无此工具：只有 goal_propose_finish（提议）→ TL 裁决 → 才收口（本用例④） | 允许 | 允许
//
// 为什么这么切：agent 直连改定义 = 在被审查的目标上单方面换掉验收标准，ADVISOR 的评审
// 依据当场失效（"回合期间 goal 被改"的 B 语义正是这件事的兜底，这里是源头收口）。
// 而人类/运维面（显式会话 API、headless 的 goal_update/goal_finish/goal_abort）必须保留：
// 拦掉它等于把"用户无法修正/取消自己的目标"当成安全。

// TestAgentGoalUpdateCannotRewriteDefinition 钉住 agent 工具面的收口与人类面的保留。
func TestAgentGoalUpdateCannotRewriteDefinition(t *testing.T) {
	service := summonService(t, &teamRecordingSessions{mainHeadSeq: 3})
	const sessionID = "sess-summon"
	if _, err := service.GoalBeginFor(context.Background(), sessionID, goaldomain.BeginRequest{
		Title: "目标", Acceptance: []string{"原验收"},
	}); err != nil {
		t.Fatalf("GoalBeginFor: %v", err)
	}
	ctx := withSessionID(context.Background(), sessionID)

	// ① agent 面改定义：四类字段逐个都必须被拒，且给的是可判定的权限错误。
	for _, args := range []string{
		`{"title":"换个目标"}`,
		`{"statement":"换个说法"}`,
		`{"acceptance":["换个验收"]}`,
		`{"out_of_scope":["换个范围"]}`,
	} {
		if _, err := service.GoalUpdateHandler(ctx, args); !errors.Is(err, errGoalDefinitionTLOnly) {
			t.Fatalf("agent 面改定义必须被拒（errGoalDefinitionTLOnly）: args=%s err=%v", args, err)
		}
	}
	// 被拒的请求不得留下任何痕迹（不是"先改后报错"）。
	status, err := service.GoalStatusFor(sessionID)
	if err != nil {
		t.Fatalf("GoalStatusFor: %v", err)
	}
	if status.Active == nil || status.Active.Title != "目标" ||
		len(status.Active.Acceptance) != 1 || status.Active.Acceptance[0] != "原验收" {
		t.Fatalf("被拒的改定义不得落到 goal 上: %+v", status.Active)
	}

	// ② 只追加进度：允许（这是 agent 汇报进展的正常路径，不是改目标）。
	out, err := service.GoalUpdateHandler(ctx, `{"progress_kind":"milestone","progress_content":"跑了单测"}`)
	if err != nil {
		t.Fatalf("agent 面追加进度应被允许: %v", err)
	}
	if !strings.Contains(out, "跑了单测") {
		t.Fatalf("进度应真的落进 goal: %s", out)
	}

	// ③ 显式会话 API（人类/运维面）仍能改定义。
	title := "人类改的标题"
	if _, err := service.GoalUpdateFor(context.Background(), sessionID, goaldomain.UpdateRequest{Title: &title}); err != nil {
		t.Fatalf("人类/运维面改定义应保持可用: %v", err)
	}
	status, err = service.GoalStatusFor(sessionID)
	if err != nil {
		t.Fatalf("GoalStatusFor: %v", err)
	}
	if status.Active == nil || status.Active.Title != "人类改的标题" {
		t.Fatalf("显式会话 API 应能改定义: %+v", status.Active)
	}

	// ④ 取消路径：agent 面**没有**直接取消 goal 的工具（工具族 = goal_begin /
	// goal_update / goal_status / goal_propose_finish，见 register_goal_tools.go），
	// 收口只能由 TL 裁决（verdict_done）或 B4 缺席回退给出。
	// 注：未启用 ADVISOR 的会话里"提议即直连收口"（OutcomeNoTL）是既有 B4 安全默认
	// ——没有 b 可问时不能让目标永远无法收口；那不是 agent 多出来的权力，本轮的权限
	// 收紧也不以它为对象（它由 TechLeaderConfig.Enabled 决定，不由调用者决定）。
	result, err := service.GoalProposeFinishFor(context.Background(), sessionID, goaldomain.FinishRequest{
		Reason: "提议收口",
	})
	if err != nil {
		t.Fatalf("提议收口本身应可调用: %v", err)
	}
	switch result.Outcome {
	case goaldomain.OutcomeNoTL, goaldomain.OutcomeCompleted:
		// B4 缺席回退 / TL 裁决收口：两条合法的取消路径。
	default:
		t.Fatalf("提议收口的结果应是 TL 裁决或 B4 缺席回退, 得 %q", result.Outcome)
	}
}
