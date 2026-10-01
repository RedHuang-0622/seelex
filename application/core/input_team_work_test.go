package core

import (
	"context"
	"slices"
	"strings"
	"testing"

	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
)

// input_team_work_test.go — "召唤即干活 + 干完就走人"的验收。
//
// 背景（2026-09-17 用户实测）：`@goal-a2a 你看看怎么做个演示` 只装配了团队，
// tl 的角色会话 total_rows=0——因为 goal-a2a 的座位（tl/ADVISOR）由 goal 治理
// 驱动，而召唤路径从不落 goal。口径改成：
//   - 带附言 = 有活要干 → 装备 + 落 goal + 附言作为一条输入下发（主会话这一轮即
//     EXEC，回合尾 Governor 让 teammate 上场）；
//   - 目标收口（栈里没有 active goal）→ 团队离场（删注册表 + 复位顺序）；
//   - 不带附言 = 只装配、待命（没有要干的活就不该凭空落 goal）。

func TestSubmitTeamWithTrailingTextBeginsGoalAndTeamLeaves(t *testing.T) {
	sessions, service := summonFixture(t, 3)

	const message = "把登录逻辑重构一下"
	if err := service.Submit(context.Background(), "@goal-a2a "+message); err != nil {
		t.Fatalf("Submit(@goal-a2a 附言): %v", err)
	}
	// 召唤口径不变：照旧装配、附言照旧作为一条输入下发。
	if roles := sessions.ensuredRoles(); !slices.Contains(roles, "tl") {
		t.Fatalf("召唤装配缺失：%v", roles)
	}
	waitForSnapshot(t, service, func(snapshot Snapshot) bool {
		return slices.Contains(conversationTexts(snapshot.Conversation), "@goal-a2a "+message)
	})

	// 召唤即干活：附言落成一个 active goal（没有它，回合尾的治理循环看不到目标，
	// teammate 不会上场）。
	status, err := service.GoalStatusFor("sess-summon")
	if err != nil {
		t.Fatalf("GoalStatusFor: %v", err)
	}
	if status.Active == nil {
		t.Fatal("带附言的召唤必须落一个 active goal，否则 teammate 不会上场")
	}
	if !strings.Contains(status.Active.Statement, message) {
		t.Fatalf("附言应成为 goal 陈述：%+v", status.Active)
	}
	if text := noticesText(service); !strings.Contains(text, "已落目标") {
		t.Fatalf("回执应说明已落目标：%q", text)
	}

	// 干完就走人：目标收口后团队离场（注册表清空 + 顺序复位）。
	if _, err := service.GoalProposeFinishFor(context.Background(), "sess-summon", goaldomain.FinishRequest{Reason: "任务已完成"}); err != nil {
		t.Fatalf("GoalProposeFinishFor: %v", err)
	}
	if afterwards, err := service.GoalStatusFor("sess-summon"); err != nil || afterwards.Active != nil {
		t.Fatalf("收口后不该还有 active goal：%+v err=%v", afterwards.Active, err)
	}
	if sessions.dismissCount() == 0 {
		t.Fatal("目标收口后团队应离场（干完就走人）")
	}
	if registry := sessions.registrySnapshot(); registry.Configured || len(registry.Roles) != 0 {
		t.Fatalf("离场后注册表应为空：%+v", registry)
	}
	if _, order := sessions.lifecycleSnapshot(); len(order) != 0 {
		t.Fatalf("离场后顺序应复位：%v", order)
	}
}

func TestSubmitTeamWithoutTrailingTextKeepsTeam(t *testing.T) {
	sessions := &teamRecordingSessions{}
	service := summonService(t, sessions)

	if err := service.Submit(context.Background(), "@goal-a2a"); err != nil {
		t.Fatalf("Submit(@goal-a2a): %v", err)
	}
	if status, err := service.GoalStatusFor("sess-summon"); err == nil && status.Active != nil {
		t.Fatalf("不带附言的召唤不该落 goal：%+v", status.Active)
	}
	if sessions.dismissCount() != 0 {
		t.Fatal("没有目标收口，团队不该离场")
	}
}
