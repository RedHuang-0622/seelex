package core

// goal_directive_visible_immediately_test.go — 「ADVISOR 的裁决要在产出它的那一
// 回合就可见」的回归（2026-09-16 P1-3 team work 探针实测缺口的修复）。
//
// 修复前：治理回合（ADVISOR）在 runChat 收尾（goalAdvanceAfterChat）里跑完，
// 它产出的 b→a 指令只能留在待注入队列里 —— 要等**下一次**用户提交才被排空注入，
// 再在下一次回合尾才回放进可见会话。于是"提交完就盯着面板"的用户与真实 API
// 探针都永远看不到裁决（探针等满 15 分钟仍无行）。
//
// 修复后：指令在产出的那一回合末尾就回放。指令本身**不被消费** —— 下一次
// ChatStream 前的受信注入（injectGoalDirectivesForStart）时机与语义不变，且同一
// corr 只回放一次（下一次回合的常规回放不再重复写行）。

import (
	"context"
	"strings"
	"testing"

	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
)

// advisorDirectiveRows 取可见会话里的 ADVISOR 裁决行（kind + 归属双条件：
// 探针与前端都按这两个字段取行，不靠正文措辞）。
func advisorDirectiveRows(messages []Message) []Message {
	var rows []Message
	for _, message := range messages {
		if message.Kind == goaldomain.DirectiveRowKind && message.RoleName == RoleNameTL {
			rows = append(rows, message)
		}
	}
	return rows
}

func TestAdvisorVerdictVisibleInProducingTurn(t *testing.T) {
	service := newTestService(t, &fakeEngine{chunks: []string{"收", "到"}}, withTestSessions(&fakeSessions{}))
	if err := service.Submit(context.Background(), "开始"); err != nil {
		t.Fatalf("Submit(#1): %v", err)
	}
	snapshot := waitForSnapshot(t, service, func(snapshot Snapshot) bool { return !snapshot.Chat.Running })
	sessionID := snapshot.Session.ID
	if sessionID == "" {
		t.Fatal("会话未物化")
	}
	if _, err := service.GoalBeginFor(context.Background(), sessionID, goaldomain.BeginRequest{Title: "裁决可见性"}); err != nil {
		t.Fatalf("GoalBeginFor: %v", err)
	}

	// 席位轮转退场后，回合尾不再自动产出 ADVISOR 裁决；指令的来源改为显式入口
	// （终态 gate / 审批预筛）。这里把一条 b→a 指令投进待注入队列，再跑**同一段
	// 回合尾回放**（chat.go 在 ChatStream 收尾调用 publishPendingGoalDirectivesFor），
	// 验证"产出它的那一回合就可见"与 corr 去重两条口径不变。
	service.components.goal.bundleFor(sessionID).sup.Mailbox().PublishDirective(
		goaldomain.TLDirective{Corr: "corr-1", Kind: goaldomain.DirectiveCorrect, Content: "先补负路径单测再收口"},
	)
	service.publishPendingGoalDirectivesFor(sessionID)

	rows := advisorDirectiveRows(visibleConversationFor(service, sessionID))
	if len(rows) != 1 {
		t.Fatalf("裁决应在产出它的那一回合就可见（且只有一行）：得到 %d 行 = %+v", len(rows), rows)
	}
	if !strings.Contains(rows[0].Content, "先补负路径单测再收口") {
		t.Fatalf("裁决行正文不是 ADVISOR 的指令原文：%q", rows[0].Content)
	}
	if !strings.Contains(rows[0].Content, "[TL 指令 corr-") {
		t.Fatalf("裁决行缺少 corr 信封（回放行的稳定标识）：%q", rows[0].Content)
	}

	// 下一次回合的常规链路：排空注入（受信区）→ 回合尾回放。已回放的 corr
	// 必须去重，否则同一裁决会在聊天区出现两行。
	service.injectGoalDirectivesForStart(sessionID)
	service.injectGoalDirectivesFor(sessionID)
	if rows := advisorDirectiveRows(visibleConversationFor(service, sessionID)); len(rows) != 1 {
		t.Fatalf("下一次回合的常规回放重复写行了（corr 去重失效）：得到 %d 行 = %+v", len(rows), rows)
	}
}
