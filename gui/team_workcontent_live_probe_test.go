package gui

// TestRealAPITeamWorkContentLiveProbe 是「team work 是否接线到工作内容」的真实
// API 全链路探针。它回答一个具体问题：AgentTeam 装配之后，teammate（goal-a2a 的
// ADVISOR/tl）到底看到的是不是 EXEC 的真实工作内容。
//
// 链路（每一步都经 headless 控制面，与 GUI 同装配）：
//   1) 真实 API Submit #1 物化主会话（EXEC 会话 = main）；
//   2) goal.begin → 隐式装配 goal-a2a（tl 成员 + order_policy=goal_loop）；
//   3) Submit #2 让 EXEC 真的干一轮活（回答里带唯一 marker）；
//   4) 读三处事实：
//        - team.view           ：成员表 + 工作顺序 + floor_role（前端 Agent Team 面板数据源）
//        - goal.gov_snapshot   ：治理轮次 / peer 状态 / 最近裁决
//        - role.snapshot(tl)   ：ADVISOR 每回合的 role_context（它实际收到的输入原文）
//                                与 tl_directive（它给出的裁决原文）
//   5) Submit #3 → 上一回合的 TL 指令在本回合起点被注入 EXEC 受信区，并回放成
//      可见 assistant 行（role_name=tl）；随后再读 Snapshot 核对可见会话的角色归属。
//
// 断言口径（链路口径，不替产品下结论）：
//   - 硬断言：装配、真实轮次、TL 回合确实发生过（否则这条冒烟证明不了任何事）；
//   - 接线判据：TL 输入里必须有 EXEC 的工作正文（marker）与 work.progress 帧，
//     成员表 floor_role 必须被填。置 SMOKE_TEAM_WORK_LIVE_EXPECT_WIRED=1 时把这三
//     项升级为硬断言（回归哨兵：2026-09-14 的缺口已修，缺口若复活即红灯）。
//
// 运行（真实 API，默认跳过）：
//
//	go build -o tmp/bin/seelex-headless.exe .
//	$env:SMOKE_TEAM_WORK_LIVE='1'
//	go test ./gui -run TestRealAPITeamWorkContentLiveProbe -v -count=1 -timeout 20m
//
// 可选 env：
//
//	SMOKE_TEAM_WORK_LIVE_TARGET        目标二进制（默认 tmp/bin/seelex-headless.exe）
//	SMOKE_TEAM_WORK_LIVE_KEEP_STORE    置 1 保留临时数据根
//	SMOKE_TEAM_WORK_LIVE_EXPECT_WIRED  置 1 时把"工作正文已进 TL 输入 + floor 已填"变成硬断言
//	SMOKE_TEAM_WORK_LIVE_MARKER        工作正文 marker（默认自动生成）

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/model"
)

func TestRealAPITeamWorkContentLiveProbe(t *testing.T) {
	if os.Getenv("SMOKE_TEAM_WORK_LIVE") == "" {
		t.Skip("set SMOKE_TEAM_WORK_LIVE=1 to run the team-work content live probe")
	}
	repoRoot := forkLiveRepoRoot(t)
	target := forkLiveEnvPath("SMOKE_TEAM_WORK_LIVE_TARGET",
		filepath.Join(repoRoot, "tmp", "bin", "seelex-headless.exe"))
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("smoke target missing: %s（先 go build -o tmp/bin/seelex-headless.exe .）", target)
	}
	storeDir, err := os.MkdirTemp("", "seelex-team-work-live-")
	if err != nil {
		t.Fatalf("mk temp store: %v", err)
	}
	defer func() {
		if os.Getenv("SMOKE_TEAM_WORK_LIVE_KEEP_STORE") == "" {
			_ = os.RemoveAll(storeDir)
		} else {
			t.Logf("[store] 保留现场: %s", storeDir)
		}
	}()

	port := forkLiveFreePort(t)
	proc := forkLiveSpawn(t, target, repoRoot, storeDir, port, "")
	defer proc.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Minute)
	defer cancel()
	proc.waitHealthy(ctx, t, time.Now().Add(90*time.Second))

	marker := strings.TrimSpace(os.Getenv("SMOKE_TEAM_WORK_LIVE_MARKER"))
	if marker == "" {
		marker = fmt.Sprintf("WORK-CONTENT-MARKER-%d", time.Now().Unix()%100000)
	}
	report := map[string]any{"marker": marker, "target": filepath.Base(target)}

	// 1) 物化主会话（EXEC = main 会话）。
	if _, err := proc.rpc(ctx, "Submit", "这是 team work 接线冒烟。请只回复 OK，不要调用任何工具。"); err != nil {
		t.Fatalf("Submit(#1): %v", err)
	}
	if _, err := proc.rpc(ctx, "WaitIdle", 300); err != nil {
		t.Fatalf("WaitIdle(#1): %v", err)
	}
	mainSessionID := roleLiveSessionID(t, ctx, proc)
	if mainSessionID == "" {
		t.Fatal("main session did not materialize")
	}
	report["main_session_id"] = mainSessionID

	// 2) goal 上线 → 隐式装配 goal-a2a 团队（不调 team.materialize）。
	if _, err := proc.rpc(ctx, "goal.begin", map[string]any{
		"title":      "team work 接线冒烟",
		"statement":  "验证 teammate 是否收到 EXEC 的真实工作内容",
		"acceptance": []string{"tl 成员出现在成员表", "能读到 tl 回合的输入原文"},
	}); err != nil {
		t.Fatalf("goal.begin: %v", err)
	}
	view := teamWorkView(t, ctx, proc, mainSessionID)
	tlSessionID := teamWorkRoleSessionID(view, "tl")
	if tlSessionID == "" {
		t.Fatalf("goal 创建后未见 tl 成员（自动装配未生效）: %+v", view.Members)
	}
	report["after_goal_view"] = view
	if view.OrderPolicy != dto.OrderPolicyGoalLoop {
		t.Fatalf("order_policy = %q, want %q", view.OrderPolicy, dto.OrderPolicyGoalLoop)
	}

	// 3) EXEC 真的干一轮活：工作正文里必须出现 marker。
	workPrompt := fmt.Sprintf("请把下面这一行原样写进你的回答里（不要调用任何工具，不要改动它）：%s", marker)
	if _, err := proc.rpc(ctx, "Submit", workPrompt); err != nil {
		t.Fatalf("Submit(#2 工作轮): %v", err)
	}
	if _, err := proc.rpc(ctx, "WaitIdle", 600); err != nil {
		t.Fatalf("WaitIdle(#2): %v", err)
	}
	snapAfterWork, err := proc.snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot(#2 后): %v", err)
	}
	report["work_turn"] = teamWorkConversationFacts(snapAfterWork.Conversation, marker)
	if snapAfterWork.Chat.Error != "" {
		t.Fatalf("工作轮 chat error: %s", snapAfterWork.Chat.Error)
	}

	// 4) 三处事实：TL 回合是否发生、它看到了什么、成员表 floor 是否被填。
	govRaw, err := proc.rpc(ctx, "goal.gov_snapshot", map[string]any{})
	if err != nil {
		t.Fatalf("goal.gov_snapshot: %v", err)
	}
	var gov dto.GoalGovernanceView
	if err := json.Unmarshal(govRaw, &gov); err != nil {
		t.Fatalf("decode gov_snapshot: %v", err)
	}
	report["governance"] = gov
	report["active"] = gov.Active
	t.Logf("[gov] active=%v peer=%q last_directive=%q",
		gov.Active, gov.PeerState, truncateForLog(gov.LastDirective, 200))

	tlSnapshot, err := roleLiveSnapshot(ctx, proc, mainSessionID, "tl", tlSessionID)
	if err != nil {
		t.Fatalf("role.snapshot(tl): %v", err)
	}
	tlInputs, tlOutputs, mainHosts := teamWorkTLRoundRows(tlSnapshot)
	if len(tlInputs) == 0 {
		t.Fatalf("tl 角色会话没有 role_context 行：ADVISOR 回合没有记录输入原文（装配=%+v）", view.Members)
	}
	t.Logf("[tl] 回合数=%d（输入原文 %d 字）", len(tlInputs), len([]rune(tlInputs[len(tlInputs)-1])))
	t.Logf("[tl] 输入原文全文：\n%s", tlInputs[len(tlInputs)-1])
	report["tl_rounds"] = len(tlInputs)
	report["tl_input_last"] = tlInputs[len(tlInputs)-1]
	report["tl_output_last"] = ""
	if len(tlOutputs) > 0 {
		report["tl_output_last"] = tlOutputs[len(tlOutputs)-1]
	}
	report["tl_main_host_rows"] = len(mainHosts)

	// 判据 A：ADVISOR 收到的输入里有没有 EXEC 的工作正文（以及承载它的帧）？
	inputHasMarker := strings.Contains(tlInputs[len(tlInputs)-1], marker)
	inputHasAnchor := strings.Contains(tlInputs[len(tlInputs)-1], "goal-anchor")
	inputHasWorkFrame := strings.Contains(tlInputs[len(tlInputs)-1], "work.progress")
	report["tl_input_has_work_content"] = inputHasMarker
	report["tl_input_has_goal_anchor"] = inputHasAnchor
	report["tl_input_has_work_frame"] = inputHasWorkFrame
	if !inputHasAnchor {
		t.Fatalf("tl 输入缺少 goal 锚点（协议不变量被破坏）:\n%s", tlInputs[len(tlInputs)-1])
	}

	// 判据 B：成员表 floor_role 是否被后端填过（前端「floor —」的判据）。
	viewAfter := teamWorkView(t, ctx, proc, mainSessionID)
	report["after_work_view"] = viewAfter
	report["view_floor_role"] = viewAfter.FloorRole
	report["role_snapshot_floor"] = tlSnapshot.Floor
	t.Logf("[team.view] floor_role=%q（role.snapshot 的 message head floor=%+v）",
		viewAfter.FloorRole, tlSnapshot.Floor)

	// 5) 再跑一轮：上一回合的 TL 指令在回合起点注入 EXEC 受信区并回放成可见行。
	if _, err := proc.rpc(ctx, "Submit", "继续：请用一行确认你收到了本回合消息。"); err != nil {
		t.Fatalf("Submit(#3 注入轮): %v", err)
	}
	if _, err := proc.rpc(ctx, "WaitIdle", 600); err != nil {
		t.Fatalf("WaitIdle(#3): %v", err)
	}
	snapFinal, err := proc.snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot(#3 后): %v", err)
	}
	conv := teamWorkConversationFacts(snapFinal.Conversation, marker)
	report["final_conversation"] = conv
	// 判据 C：可见会话里 tl 行是否带 role_name（前端 roleIdentity 的数据源）。
	tlRows, rolesSeen := 0, map[string]int{}
	for _, message := range snapFinal.Conversation {
		rolesSeen[message.RoleName]++
		if message.RoleName == model.RoleNameTL {
			tlRows++
		}
	}
	report["conversation_role_names"] = rolesSeen
	report["conversation_tl_rows"] = tlRows
	report["final_tl_rows_main_doc"] = len(tlSnapshot.MainRows)
	t.Logf("[conversation] role_name 分布=%v；tl 可见行=%d", rolesSeen, tlRows)

	// 6) 结论行（不代替产品裁决，只把判据摆在一起）。
	verdict := map[string]any{
		"team_assembled":            tlSessionID != "",
		"tl_round_ran":              len(tlInputs) > 0,
		"tl_input_has_work_content": inputHasMarker,
		"tl_input_has_work_frame":   inputHasWorkFrame,
		"view_floor_role_filled":    viewAfter.FloorRole != "",
		"conversation_has_tl_rows":  tlRows > 0,
	}
	report["verdict"] = verdict
	t.Logf("[verdict] %+v", verdict)
	t.Logf("[verdict] ADVISOR 输入 = goal 锚点 + 帧账本（含 work.progress 工作进展）+ 自身回合记忆；"+
		"工作正文进 TL 输入 = %v（marker=%s）", inputHasMarker, marker)
	t.Logf("[verdict] team.view.floor_role 由后端填充 = %v（前端就此渲染 floor 高亮）",
		viewAfter.FloorRole != "")

	// 回归哨兵：缺口（EXEC 工作内容不进 ADVISOR 输入 / floor 无出口）已修；置
	// SMOKE_TEAM_WORK_LIVE_EXPECT_WIRED=1 时把这两条钉成硬断言。
	if os.Getenv("SMOKE_TEAM_WORK_LIVE_EXPECT_WIRED") == "1" {
		if !inputHasWorkFrame {
			t.Fatalf("ADVISOR 输入缺少 work.progress 帧（EXEC 工作内容未接线）:\n%s", tlInputs[len(tlInputs)-1])
		}
		if !inputHasMarker {
			t.Fatalf("ADVISOR 输入不含 EXEC 工作正文 marker %q:\n%s", marker, tlInputs[len(tlInputs)-1])
		}
		if viewAfter.FloorRole == "" {
			t.Fatalf("team.view.floor_role 未被填充（floor 没有出口；role.snapshot floor=%+v）", tlSnapshot.Floor)
		}
	}

	teamWorkWriteReport(t, repoRoot, report)
}

// ── 事实提取 ────────────────────────────────────────────────

func teamWorkView(t *testing.T, ctx context.Context, proc *forkLiveProc, mainSessionID string) dto.TeamView {
	t.Helper()
	raw, err := proc.rpc(ctx, "team.view", map[string]any{"main_session_id": mainSessionID})
	if err != nil {
		t.Fatalf("team.view: %v", err)
	}
	var view dto.TeamView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("decode team.view: %v", err)
	}
	return view
}

func teamWorkRoleSessionID(view dto.TeamView, roleName string) string {
	for _, member := range view.Members {
		if member.RoleName == roleName {
			return member.RoleSessionID
		}
	}
	return ""
}

// teamWorkTLRoundRows 从 tl 角色快照里取出三类行：ADVISOR 每回合的输入原文
// （role_context）、它的裁决原文（tl_directive），以及 EXEC 交还发言权的标记
// （round_host）。行可能落在 main 文档（sync 之后）或 draft（未 sync）。
func teamWorkTLRoundRows(snapshot dto.RoleSnapshot) (inputs, outputs, hosts []string) {
	collect := func(rows []dto.RoleRow) {
		for _, row := range rows {
			switch row.Kind {
			case "role_context":
				inputs = append(inputs, row.Content)
			case "tl_directive":
				outputs = append(outputs, row.Content)
			case "round_host":
				hosts = append(hosts, row.Content)
			}
		}
	}
	collect(snapshot.MainRows)
	for _, draft := range snapshot.DraftRows {
		collect([]dto.RoleRow{draft.Event})
	}
	return inputs, outputs, hosts
}

// teamWorkConversationFacts 汇总可见会话的事实：工作正文 marker 落在哪种角色行上、
// 角色归属字段是否齐全。
func teamWorkConversationFacts(messages []model.Message, marker string) map[string]any {
	roles := map[string]int{}
	markerRoles := []string{}
	missingRoleName := 0
	for _, message := range messages {
		roles[message.RoleName]++
		if message.RoleName == "" && message.Role != "tool" {
			missingRoleName++
		}
		if strings.Contains(message.Content, marker) {
			markerRoles = append(markerRoles, message.RoleName+"|"+message.Role)
		}
	}
	return map[string]any{
		"count":             len(messages),
		"role_names":        roles,
		"marker_rows":       markerRoles,
		"missing_role_name": missingRoleName,
	}
}

func truncateForLog(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max]) + "…"
}

func teamWorkWriteReport(t *testing.T, repoRoot string, report map[string]any) {
	t.Helper()
	dir := filepath.Join(repoRoot, "tmp", "headless-smoke", "reports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Logf("[report] 建目录失败: %v", err)
		return
	}
	path := filepath.Join(dir, fmt.Sprintf("team-work-%s.json", time.Now().Format("20060102-150405")))
	payload, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Logf("[report] 编码失败: %v", err)
		return
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Logf("[report] 写入失败: %v", err)
		return
	}
	t.Logf("[report] %s", path)
}
