package core

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// input_team_test.go — `@` 手动召唤团队的验收。
//
// 口径（与 input_team.go 的边界一一对应）：
//   - `@<团队库条目>` 真装配：在编角色进注册表、发言顺序进 lifecycle、入伙切点 =
//     装配那一刻的主会话消息尾 seq，按 team_id **和**名字都能召唤；
//   - 空名 `@` 自述团队库里的可用团队且不装配任何角色；未知名给可行动提示（列库条目），
//     旧写法 `@off` 指出新前缀；
//   - 旧前缀肌肉记忆：`#`/`$`/`@` 各自给出正确的迁移提示。
//
// **内置形态目录已删**（2026-10-01）：`@` 不再有一条"零 I/O 的内置形态"解析路径，
// 所以这些用例都必须先有**团队库条目**——那也正是用户今天得到可召唤团队的唯一路径
// （面板「团队库」新建，或「把当前团队存进库」）。

// summonFixture 造"会话里有一支 goal 形态团队、且已存进团队库"的现场，然后清掉会话侧
// 现场（之后装配回来的东西只可能来自库条目）。返回的 sessions 用于断言落盘事实。
func summonFixture(t *testing.T, mainHeadSeq uint64) (*librarySessions, *Service) {
	t.Helper()
	sessions := newLibrarySessions()
	sessions.mainHeadSeq = mainHeadSeq
	sessions.setRegistry(dto.TeamRegistry{
		TeamID: "goal-a2a", TeamKind: "goal-a2a", OrderPolicy: dto.OrderPolicyGoalLoop,
		Roles: []dto.RoleSpec{
			{RoleName: "user", RoleKind: dto.RoleKindUser},
			{RoleName: "main", RoleKind: dto.RoleKindMain},
			{RoleName: "tl", RoleKind: dto.RoleKindTechlead, ToolsPolicy: dto.ToolPolicyReadonly, SystemPrompt: "技术负责人提示词"},
		},
	})
	sessions.setLifecycle(dto.OrderPolicyGoalLoop, []string{"user", "main", "tl"})
	service := summonService(t, sessions)
	if _, err := service.AgentTeamSaveCurrentTeam("sess-summon", "goal-a2a", "goal-a2a"); err != nil {
		t.Fatalf("AgentTeamSaveCurrentTeam: %v", err)
	}
	sessions.setRegistry(dto.TeamRegistry{})
	sessions.setOrder(nil)
	return sessions, service
}

// summonService 造一个带团队存储面的服务，并把视图会话钉成固定 ID
// （召唤落在"当前会话"，断言必须指向确定的那个会话）。
func summonService(t *testing.T, sessions SessionPort) *Service {
	t.Helper()
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))
	service.ViewMu.Lock()
	service.Core.Snapshot.Session.ID = "sess-summon"
	service.ViewMu.Unlock()
	return service
}

// noticesText 读可见会话里的系统通知（notice 的落地通道）。
func noticesText(service *Service) string {
	return strings.Join(conversationTexts(service.Snapshot().Conversation), "\n")
}

// waitTeamChanged 等一条 team.changed（超时即失败）。
func waitTeamChanged(t *testing.T, subscription Subscription, sessionID string) Event {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case received := <-subscription.Events:
			if received.Kind != EventTeamChanged {
				continue
			}
			if received.SessionID != sessionID {
				t.Fatalf("team.changed 的 sid = %q, want %q", received.SessionID, sessionID)
			}
			return received
		case <-deadline:
			t.Fatal("没收到 team.changed：面板的成员表不在快照里，没有这条事件就只能靠调用方自觉重取")
		}
	}
}

// TestSubmitTeamPublishesTeamChanged 钉住"召唤后面板有据可依"：装配成功必须发一条
// 会话级 team.changed，且 **revision=0**（载荷不在快照里，带 revision 会被协议层的
// "快照已表示"陈旧判据丢掉，而面板缓存不随快照翻转——见 publishTeamChanged）。
func TestSubmitTeamPublishesTeamChanged(t *testing.T) {
	sessions, service := summonFixture(t, 5)
	_ = sessions
	subscription, err := service.SubscribeSession("sess-summon", 64)
	if err != nil {
		t.Fatalf("SubscribeSession: %v", err)
	}
	defer subscription.Close()

	if err := service.Submit(context.Background(), "@goal-a2a"); err != nil {
		t.Fatalf("Submit(@goal-a2a): %v", err)
	}
	if received := waitTeamChanged(t, subscription, "sess-summon"); received.Revision != 0 {
		t.Fatalf("team.changed 的 revision = %d, want 0", received.Revision)
	}
}

// TestMaterializeAgentTeamPublishesTeamChanged 钉住收口位置：面板 RPC「一键装配」与
// `@` 召唤走的都是 MaterializeAgentTeam，通告钉在这条共同路径上，而不是钉在某个调用方。
func TestMaterializeAgentTeamPublishesTeamChanged(t *testing.T) {
	sessions := &teamRecordingSessions{}
	service := summonService(t, sessions)
	subscription, err := service.SubscribeSession("sess-summon", 64)
	if err != nil {
		t.Fatalf("SubscribeSession: %v", err)
	}
	defer subscription.Close()

	if _, err := service.MaterializeAgentTeam("sess-summon", goalTeamFixture(), 0); err != nil {
		t.Fatalf("MaterializeAgentTeam: %v", err)
	}
	waitTeamChanged(t, subscription, "sess-summon")
}

// TestReadPathDoesNotPublishTeamChanged 是上一条的边界：读成员表**不**发通告。
// 若读路径也发，客户端会陷入"收到通告→重取成员表→又收到通告"的自激循环。
func TestReadPathDoesNotPublishTeamChanged(t *testing.T) {
	sessions := &teamRecordingSessions{}
	service := summonService(t, sessions)
	subscription, err := service.SubscribeSession("sess-summon", 64)
	if err != nil {
		t.Fatalf("SubscribeSession: %v", err)
	}
	defer subscription.Close()

	if _, err := service.AgentTeamView("sess-summon"); err != nil {
		t.Fatalf("AgentTeamView: %v", err)
	}
	// 读路径本身可能有别的副作用事件（会话目录/标题刷新），这里只钉"不发团队通告"。
	deadline := time.After(150 * time.Millisecond)
	for {
		select {
		case received := <-subscription.Events:
			if received.Kind == EventTeamChanged {
				t.Fatal("读路径不该发 team.changed：会退化成「取成员表→通告→再取」的自激循环")
			}
		case <-deadline:
			return
		}
	}
}

func TestSubmitTeamSummonsLibraryTeam(t *testing.T) {
	sessions, service := summonFixture(t, 5)

	if err := service.Submit(context.Background(), "@goal-a2a"); err != nil {
		t.Fatalf("Submit(@goal-a2a): %v", err)
	}
	roles := sessions.ensuredRoles()
	// 库条目里的 user/main 是内建席位（JoinPolicy=builtin，不建角色会话），
	// 真正"新入职"的是 techlead——它必须出现在装配记录里。
	if !slices.Contains(roles, "tl") {
		t.Fatalf("库条目没装配出 techlead 角色会话：%v", roles)
	}
	if joins := sessions.joinSeqSnapshot(); !slices.Contains(joins, "tl|5") {
		t.Fatalf("入伙切点必须是当前消息尾 seq=5：%v", joins)
	}
	policy, order := sessions.lifecycleSnapshot()
	if policy != dto.OrderPolicyGoalLoop || strings.Join(order, ",") != "user,main,tl" {
		t.Fatalf("发言顺序未写入：policy=%q order=%v", policy, order)
	}
	if text := noticesText(service); !strings.Contains(text, "已召唤团队 goal-a2a") {
		t.Fatalf("召唤回执缺失：%q", text)
	}
}

func TestSubmitTeamWithoutNameDescribesLibrary(t *testing.T) {
	sessions, service := summonFixture(t, 0)

	if err := service.Submit(context.Background(), "@"); err != nil {
		t.Fatalf("Submit(@): %v", err)
	}
	text := noticesText(service)
	if !strings.Contains(text, "goal-a2a") {
		t.Fatalf("`@` 自述应列出团队库里的名字：%q", text)
	}
	if roles := sessions.ensuredRoles(); len(roles) != 0 {
		t.Fatalf("空名不该装配任何角色：%v", roles)
	}

	// 团队库为空时给出可行动的下一步（不再有内置形态可退）。
	empty := newLibrarySessions()
	emptyService := summonService(t, empty)
	if err := emptyService.Submit(context.Background(), "@"); err != nil {
		t.Fatalf("Submit(@) 空库: %v", err)
	}
	if text := noticesText(emptyService); !strings.Contains(text, "团队库还是空的") {
		t.Fatalf("空库自述缺行动指引：%q", text)
	}
}

func TestSubmitTeamSummonsLibraryEntryByName(t *testing.T) {
	sessions := newLibrarySessions()
	sessions.setRegistry(dto.TeamRegistry{
		TeamID: "audit-team", TeamKind: "audit-team", OrderPolicy: dto.OrderPolicyUserMainDecided,
		Roles: []dto.RoleSpec{
			{RoleName: "user", RoleKind: dto.RoleKindUser},
			{RoleName: "main", RoleKind: dto.RoleKindMain},
			{RoleName: "auditor", RoleKind: dto.RoleKindAgent, SystemPrompt: "审计员提示词", ToolsPolicy: dto.ToolPolicyReadonly},
		},
	})
	sessions.setLifecycle(dto.OrderPolicyUserMainDecided, []string{"user", "main", "auditor"})
	service := summonService(t, sessions)

	// 先把当前会话在编员工存成一条库条目（走既有"存进团队库"入口，不另造形态）。
	if _, err := service.AgentTeamSaveCurrentTeam("sess-summon", "审计小队", "audit-team"); err != nil {
		t.Fatalf("AgentTeamSaveCurrentTeam: %v", err)
	}
	// 清掉会话侧现场：之后装配回来的东西只可能来自库条目。
	sessions.setRegistry(dto.TeamRegistry{})
	sessions.setOrder(nil)

	// 用**用户起的名字**召唤（不是 team_id）：库条目的两种键都要能命中。
	if err := service.Submit(context.Background(), "@审计小队"); err != nil {
		t.Fatalf("Submit(@审计小队): %v", err)
	}
	if registry := sessions.registrySnapshot(); registry.TeamID != "audit-team" {
		t.Fatalf("库条目没装配回注册表：%+v", registry)
	}
	_, order := sessions.lifecycleSnapshot()
	if strings.Join(order, ",") != "user,main,auditor" {
		t.Fatalf("库条目的顺序没写回：%v", order)
	}
	if roles := sessions.ensuredRoles(); !slices.Contains(roles, "auditor") {
		t.Fatalf("库条目里的员工没进在编表：%v", roles)
	}
	if text := noticesText(service); !strings.Contains(text, "已召唤团队 审计小队") {
		t.Fatalf("召唤回执应显示库条目名字：%q", text)
	}
}

// TestSubmitTeamTrailingTextIsSentAsInput 钉住 `@<团队> <附言>`：附言不再被当成
// 团队名的一部分（旧口径下整句去查库，回执是"未知团队: goal-a2a 这次启动…"，
// 用户那句话被静默吞掉），而是装配后作为一条输入下发。
func TestSubmitTeamTrailingTextIsSentAsInput(t *testing.T) {
	sessions, service := summonFixture(t, 3)

	const message = "这次启动团队主要是看看整个team的工作是否打通。"
	if err := service.Submit(context.Background(), "@goal-a2a "+message); err != nil {
		t.Fatalf("Submit(@goal-a2a 附言): %v", err)
	}
	// 附言不影响装配判据：库条目照旧装配出 techlead、顺序照旧写 lifecycle。
	if roles := sessions.ensuredRoles(); !slices.Contains(roles, "tl") {
		t.Fatalf("附言不该影响装配：%v", roles)
	}
	if joins := sessions.joinSeqSnapshot(); !slices.Contains(joins, "tl|3") {
		t.Fatalf("入伙切点仍取装配那一刻的消息尾 seq：%v", joins)
	}
	if policy, order := sessions.lifecycleSnapshot(); policy != dto.OrderPolicyGoalLoop || strings.Join(order, ",") != "user,main,tl" {
		t.Fatalf("发言顺序未写入：policy=%q order=%v", policy, order)
	}
	text := noticesText(service)
	if strings.Contains(text, "未知团队") {
		t.Fatalf("附言不该触发未知团队：%q", text)
	}
	if !strings.Contains(text, "已召唤团队 goal-a2a") {
		t.Fatalf("召唤回执缺失：%q", text)
	}
	if !strings.Contains(text, "附言已作为本会话的一条输入下发。") {
		t.Fatalf("附言回执缺失：%q", text)
	}
	// 附言作为一条输入下发，且保留用户原文（与 `$<skill> <args>` 同口径）。
	want := "@goal-a2a " + message
	waitForSnapshot(t, service, func(snapshot Snapshot) bool {
		return slices.Contains(conversationTexts(snapshot.Conversation), want)
	})
}

// TestSubmitTeamResolvesMultiWordLibraryNameWithTrailingText 钉住另一半：
// 团队名可以含空格，且"名字 + 附言"仍要切对——最长可命中前缀赢（整串优先，
// 其次是逐级回退的前缀），而不是把首个 token 当名字。
func TestSubmitTeamResolvesMultiWordLibraryNameWithTrailingText(t *testing.T) {
	sessions := newLibrarySessions()
	sessions.setRegistry(dto.TeamRegistry{
		TeamID: "audit-team", TeamKind: "audit-team", OrderPolicy: dto.OrderPolicyUserMainDecided,
		Roles: []dto.RoleSpec{
			{RoleName: "user", RoleKind: dto.RoleKindUser},
			{RoleName: "main", RoleKind: dto.RoleKindMain},
			{RoleName: "auditor", RoleKind: dto.RoleKindAgent, SystemPrompt: "审计员提示词", ToolsPolicy: dto.ToolPolicyReadonly},
		},
	})
	sessions.setLifecycle(dto.OrderPolicyUserMainDecided, []string{"user", "main", "auditor"})
	service := summonService(t, sessions)

	// 存一条名字**含空格**的团队条目；随后清掉会话侧现场，装配只能来自库条目。
	if _, err := service.AgentTeamSaveCurrentTeam("sess-summon", "审计 小队", "audit-team"); err != nil {
		t.Fatalf("AgentTeamSaveCurrentTeam: %v", err)
	}
	sessions.setRegistry(dto.TeamRegistry{})
	sessions.setOrder(nil)

	const message = "请审核登录逻辑"
	if err := service.Submit(context.Background(), "@审计 小队 "+message); err != nil {
		t.Fatalf("Submit(含空格名字 + 附言): %v", err)
	}
	if registry := sessions.registrySnapshot(); registry.TeamID != "audit-team" {
		t.Fatalf("含空格的名字没命中库条目：%+v", registry)
	}
	if roles := sessions.ensuredRoles(); !slices.Contains(roles, "auditor") {
		t.Fatalf("库条目里的员工没进在编表：%v", roles)
	}
	text := noticesText(service)
	if !strings.Contains(text, "已召唤团队 审计 小队") {
		t.Fatalf("召唤回执应显示含空格的库条目名：%q", text)
	}
	if strings.Contains(text, "未知团队") {
		t.Fatalf("含空格的名字不该被判成未知团队：%q", text)
	}
	want := "@审计 小队 " + message
	waitForSnapshot(t, service, func(snapshot Snapshot) bool {
		return slices.Contains(conversationTexts(snapshot.Conversation), want)
	})
}

// TestSubmitTeamUnknownNameWithTrailingTextReportsNameOnly 钉住失败口径：
// 全部候选都没命中时只报"最可能的名字"（首个 token），不把用户整句话当名字
// 回显，也不下发附言（没装配成功就不该有输入）。
func TestSubmitTeamUnknownNameWithTrailingTextReportsNameOnly(t *testing.T) {
	sessions := newLibrarySessions()
	service := summonService(t, sessions)

	if err := service.Submit(context.Background(), "@nope 顺便说一句"); err != nil {
		t.Fatalf("未知团队只补 notice，不该返回错误：%v", err)
	}
	text := noticesText(service)
	if !strings.Contains(text, "未知团队: nope") {
		t.Fatalf("应只报首个 token 当名字：%q", text)
	}
	if strings.Contains(text, "顺便说一句") {
		t.Fatalf("整句不该被当成名字回显：%q", text)
	}
	if roles := sessions.ensuredRoles(); len(roles) != 0 {
		t.Fatalf("未知名不该装配任何角色：%v", roles)
	}
}

func TestSubmitTeamUnknownNameGivesActionableNotice(t *testing.T) {
	sessions := newLibrarySessions()
	sessions.setLibrary(dto.TeamLibrary{
		Teams:      []dto.TeamLibraryEntry{{TeamID: "audit-team", Name: "审计小队"}},
		Configured: true,
	})
	service := summonService(t, sessions)

	if err := service.Submit(context.Background(), "@nope"); err != nil {
		t.Fatalf("未知团队只补 notice，不该返回错误：%v", err)
	}
	text := noticesText(service)
	for _, want := range []string{"未知团队: nope", "audit-team"} {
		if !strings.Contains(text, want) {
			t.Fatalf("未知团队提示缺 %q：%q", want, text)
		}
	}
	if roles := sessions.ensuredRoles(); len(roles) != 0 {
		t.Fatalf("未知名不该装配任何角色：%v", roles)
	}

	// 旧写法 `@off`（前缀调整前 `@` 就是插件面）直接指出新前缀。
	if err := service.Submit(context.Background(), "@off"); err != nil {
		t.Fatalf("Submit(@off): %v", err)
	}
	if text := noticesText(service); !strings.Contains(text, "停用插件请用 #off") {
		t.Fatalf("@off 应指出新前缀：%q", text)
	}
}

func TestSigilMigrationHintsPointAtTheRightPrefix(t *testing.T) {
	sessions := &teamRecordingSessions{}
	service := summonService(t, sessions)

	// `#review`：`#` 现在是"切换插件"，review 是 Skill → 提示用 `$`。
	if err := service.Submit(context.Background(), "#review"); err == nil {
		t.Fatal("#review 应报插件不存在（插件切换失败要抛给调用方）")
	}
	if text := noticesText(service); !strings.Contains(text, "召回 Skill 用 $review") {
		t.Fatalf("#review 缺迁移提示：%q", text)
	}

	// `$default`：`$` 是"召回 Skill"，default 是 Plugin → 提示用 `#`。
	if err := service.Submit(context.Background(), "$default"); err != nil {
		t.Fatalf("未知 Skill 只补 notice，不该返回错误：%v", err)
	}
	if text := noticesText(service); !strings.Contains(text, "切换插件用 #default") {
		t.Fatalf("$default 缺迁移提示：%q", text)
	}

	// `@review`：`@` 是"召唤团队"，review 是 Skill → 提示用 `$`。
	if err := service.Submit(context.Background(), "@review"); err != nil {
		t.Fatalf("Submit(@review): %v", err)
	}
	if text := noticesText(service); !strings.Contains(text, "召回 Skill 用 $review") {
		t.Fatalf("@review 缺迁移提示：%q", text)
	}
}

// TestMasterCRUDsPublishTeamChanged 钉住「母本 CRUD 也发 team.changed」：员工库 /
// 团队库 / 默认顺序都是面板数据的一部分（员工库表、团队库表、Team 栏），写它们
// 同样让面板缓存过期。旧口径只在会话装配面（MaterializeAgentTeam 等）发声，库的
// 增删改只能靠发起方自己重取——另一个观察者（另一窗口、切回来）会停在旧库。
// 这里逐条钉：每一条母本写都要落一条会话级 team.changed（sid = 发起会话）。
func TestMasterCRUDsPublishTeamChanged(t *testing.T) {
	cases := []struct {
		name string
		call func(service *Service) error
	}{
		{"save-employee", func(service *Service) error {
			_, err := service.AgentTeamSaveEmployee("sess-summon", dto.RoleSpec{RoleName: "auditor", RoleKind: dto.RoleKindAgent})
			return err
		}},
		{"delete-employee", func(service *Service) error {
			_, err := service.AgentTeamDeleteEmployee("sess-summon", "auditor")
			return err
		}},
		{"save-team", func(service *Service) error {
			_, err := service.AgentTeamSaveTeam("sess-summon", dto.TeamLibraryEntry{TeamID: "audit-team", Name: "审计小队"})
			return err
		}},
		{"delete-team", func(service *Service) error {
			_, err := service.AgentTeamDeleteTeam("sess-summon", "audit-team")
			return err
		}},
		{"set-default-order", func(service *Service) error {
			_, err := service.AgentTeamSetDefaultOrder("sess-summon", dto.OrderPolicyUserMainDecided, []string{"user", "main"})
			return err
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service := summonService(t, newLibrarySessions())
			subscription, err := service.SubscribeSession("sess-summon", 64)
			if err != nil {
				t.Fatalf("SubscribeSession: %v", err)
			}
			defer subscription.Close()

			if err := testCase.call(service); err != nil {
				t.Fatalf("%s: %v", testCase.name, err)
			}
			received := waitTeamChanged(t, subscription, "sess-summon")
			if received.Revision != 0 {
				t.Fatalf("team.changed 的 revision = %d, want 0", received.Revision)
			}
		})
	}
}
