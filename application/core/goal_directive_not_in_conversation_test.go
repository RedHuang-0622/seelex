package core

// goal_directive_not_in_conversation_test.go — 「ADVISOR 是被调用的 agent，不是
// 对话席位」的回归（2026-10-01 口径修正）。
//
// 口径：b→a 裁决**不进可见对话**，只走两条通道——
//  1. 受信注入：下一次 ChatStream 前把 〔[TL 指令 corr-N] <正文>〕 以 user 角色
//     写进引擎受信区（模型看得到，EXEC 据此行动）；
//  2. tl 角色会话：goal_team_recorder 每回合落 role_context/原文两行（前端
//     "评审过程"面板）。
//
// 修正前的症状（2026-09-16 P1-3 引入的"可见回放"，2026-10-01 撤除）：回合尾把同一
// 批指令回放成聊天区的 ADVISOR 行（assistant + role_name=tl + kind=tl_directive），
// ADVISOR 于是看起来像在对话里发言——用户口径：它是被调用的 agent（goal 终态 gate
// 的评审者），不是参与对话的席位。

import (
	"context"
	"strings"
	"testing"

	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
)

func TestAdvisorDirectiveNeverEntersVisibleConversation(t *testing.T) {
	service := newTestService(t, &fakeEngine{chunks: []string{"收", "到"}}, withTestSessions(&fakeSessions{}))
	if err := service.Submit(context.Background(), "开始"); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	snapshot := waitForSnapshot(t, service, func(snapshot Snapshot) bool { return !snapshot.Chat.Running })
	sessionID := snapshot.Session.ID
	if sessionID == "" {
		t.Fatal("会话未物化")
	}
	if _, err := service.GoalBeginFor(context.Background(), sessionID, goaldomain.BeginRequest{Title: "裁决不进对话"}); err != nil {
		t.Fatalf("GoalBeginFor: %v", err)
	}
	// 治理回合产出（或终态 gate 直接给出）一条 b→a 裁决，落在待注入队列里。
	service.components.goal.bundleFor(sessionID).sup.Mailbox().PublishDirective(
		goaldomain.TLDirective{Corr: "corr-1", Kind: goaldomain.DirectiveCorrect, Content: "先补负路径单测再收口"},
	)

	// 受信注入点（下一次 ChatStream 起手）：排空待注入队列并写进引擎受信区。
	service.injectGoalDirectivesForStart(sessionID)

	// 判据一：对话里没有 ADVISOR 行——既没有 tl 归属行，也没有 `[TL 指令 corr-` 正文。
	for _, message := range visibleConversationFor(service, sessionID) {
		if message.RoleName == RoleNameTL {
			t.Fatalf("ADVISOR 不该在对话里发言（role_name=tl 行）：%+v", message)
		}
		if strings.Contains(message.Content, "[TL 指令 corr-") {
			t.Fatalf("裁决回放行仍在可见会话里：%+v", message)
		}
	}

	// 判据二：受信注入照旧——"不进对话"不等于"不到 EXEC"：裁决以 〔…〕 形式写进
	// 引擎历史，EXEC 的下一次模型调用看得到它。
	history := service.engineHistoryFor(sessionID)
	injected := false
	for _, message := range history {
		if strings.Contains(message.Content, "TL 指令 corr-1") &&
			strings.Contains(message.Content, "先补负路径单测再收口") {
			injected = true
		}
	}
	if !injected {
		t.Fatalf("裁决没有注入引擎受信区（EXEC 收不到裁决）：%+v", history)
	}
}
