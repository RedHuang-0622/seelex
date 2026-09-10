package gui

// TestRealAPIAgentTeamLiveProbe 是 AgentTeam 角色工厂的真实 API 冒烟：启动与 GUI
// 同装配的 headless 进程，先经真实 API 物化主会话，再经 headless 暴露的
// `team.*` 接口装配 goal preset、查看成员表、做角色配置 CRUD 与工作顺序设置，
// 并核对设计稿不变量（定时 agent 不入 order_roles、subagent 不出现、重复装配
// 幂等），最后抓 goroutine/mutex/block pprof 现场判断死锁与竞争。
//
// 运行（真实 API，默认跳过）：
//   go build -tags pprof -o tmp/headless-smoke/seelex-pprof-team.exe .
//   $env:SMOKE_TEAM_LIVE='1'; $env:SMOKE_TEAM_LIVE_PPROF='1'
//   go test ./gui -run TestRealAPIAgentTeamLiveProbe -v -count=1 -timeout 20m

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

func TestRealAPIAgentTeamLiveProbe(t *testing.T) {
	if os.Getenv("SMOKE_TEAM_LIVE") == "" {
		t.Skip("set SMOKE_TEAM_LIVE=1 to run the real-API agent team live probe")
	}
	repoRoot := forkLiveRepoRoot(t)
	usePprof := os.Getenv("SMOKE_TEAM_LIVE_PPROF") == "1"
	defaultTarget := filepath.Join(repoRoot, "tmp", "bin", "seelex-headless.exe")
	if usePprof {
		defaultTarget = filepath.Join(repoRoot, "tmp", "headless-smoke", "seelex-pprof-team.exe")
	}
	target := forkLiveEnvPath("SMOKE_TEAM_LIVE_TARGET", defaultTarget)
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("smoke target missing: %s（先构建含 team.* 接口的 headless 二进制）", target)
	}
	storeDir, err := os.MkdirTemp("", "seelex-team-live-")
	if err != nil {
		t.Fatalf("mk temp store: %v", err)
	}
	defer func() {
		if os.Getenv("SMOKE_TEAM_LIVE_KEEP_STORE") == "" {
			_ = os.RemoveAll(storeDir)
		} else {
			t.Logf("[store] 保留现场: %s", storeDir)
		}
	}()

	port := forkLiveFreePort(t)
	pprofAddr := ""
	if usePprof {
		pprofAddr = "127.0.0.1:" + forkLiveFreePort(t)
	}
	proc := forkLiveSpawn(t, target, repoRoot, storeDir, port, pprofAddr)
	defer proc.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	proc.waitHealthy(ctx, t, time.Now().Add(90*time.Second))

	// 1) 真实 API 物化主会话（角色会话必须挂在已发布的主会话上）。
	prompt := strings.TrimSpace(os.Getenv("SMOKE_TEAM_LIVE_PROMPT"))
	if prompt == "" {
		prompt = "这是 AgentTeam 工厂冒烟。请只回复 OK，不要调用任何工具。"
	}
	if _, err := proc.rpc(ctx, "Submit", prompt); err != nil {
		t.Fatalf("real API Submit: %v", err)
	}
	if _, err := proc.rpc(ctx, "WaitIdle", 300); err != nil {
		t.Fatalf("WaitIdle: %v", err)
	}
	mainSessionID := roleLiveSessionID(t, ctx, proc)
	if mainSessionID == "" {
		t.Fatal("main session did not materialize")
	}
	notice := []string{}

	// 2) preset 清单：goal 是内置实例之一，不是唯一形态。
	presetsRaw, err := proc.rpc(ctx, "team.presets", map[string]any{})
	if err != nil {
		t.Fatalf("team.presets: %v", err)
	}
	var presets []dto.TeamSpec
	if err := json.Unmarshal(presetsRaw, &presets); err != nil {
		t.Fatalf("decode team.presets: %v", err)
	}
	kinds := map[string]bool{}
	for _, preset := range presets {
		kinds[preset.TeamKind] = true
	}
	for _, want := range []string{string(dto.TeamKindGoalA2A), string(dto.TeamKindReview)} {
		if !kinds[want] {
			t.Fatalf("preset %q missing from %v", want, kinds)
		}
	}

	// 3) 用 goal preset 装配（TL 上线 + 顺序策略落 lifecycle head）。
	first := teamLiveMaterialize(t, ctx, proc, mainSessionID, string(dto.TeamKindGoalA2A), 0)
	if !teamLiveHasRole(first.Sessions, "tl") {
		t.Fatalf("goal preset must create the tl role session: %+v", first.Sessions)
	}
	if strings.Join(first.View.OrderRoles, ",") != "user,main,tl" {
		t.Fatalf("goal order_roles = %v", first.View.OrderRoles)
	}
	if first.View.OrderPolicy != dto.OrderPolicyGoalLoop {
		t.Fatalf("goal order policy = %q", first.View.OrderPolicy)
	}
	teamLiveAssertNoSubagent(t, first.View)
	if len(first.View.DesignNotice) != 0 {
		notice = append(notice, first.View.DesignNotice...)
	}

	// 3.5) 真实轮次必须真的落在主文档上：只有 user 行 = provider 调用失败，
	// 这条链路（headless/存储/角色寄存器）就退化成了"没跑模型也算过"。
	realTurn := teamLiveAssertRealTurnLanded(t, ctx, proc, mainSessionID)

	// 3.6) 顺序装配后必须真的能走一遍 role draft → sequencer sync，并且 floor 随
	// message head 发布（设计 §6.1.11）。只装配顺序不产生 floor 是预期状态，但只要
	// 发生过一次 sync，floor 就必须出现；否则就是链路偏差。
	teamLiveSyncRoleDraft(t, ctx, proc, mainSessionID, "tl", "goal-a2a-tl")
	afterSync, err := roleLiveSnapshot(ctx, proc, mainSessionID, "tl", "goal-a2a-tl")
	if err != nil {
		t.Fatalf("role.snapshot(after sync): %v", err)
	}
	if afterSync.Floor == nil || afterSync.Floor.RoleName != "tl" || afterSync.Floor.RoleSessionID == "" {
		t.Fatalf("sync 后 floor 必须随 message head 发布: %+v", afterSync.Floor)
	}
	if len(afterSync.DesignWarnings) != 0 {
		notice = append(notice, afterSync.DesignWarnings...)
	}

	// 4) 重复装配必须幂等：不产生第二个 tl 角色会话（AT6）。
	second := teamLiveMaterialize(t, ctx, proc, mainSessionID, string(dto.TeamKindGoalA2A), 0)
	for _, session := range second.Sessions {
		if session.Created {
			t.Fatalf("repeat materialize must not create sessions: %+v", second.Sessions)
		}
	}

	// 5) 第二个团队实例：同一工厂 + 同一 RPC 面，只换 TeamSpec（AT8）。
	review := teamLiveMaterialize(t, ctx, proc, mainSessionID, string(dto.TeamKindReview), 0)
	if !teamLiveHasRole(review.Sessions, "reviewer") {
		t.Fatalf("review preset must create the reviewer role session: %+v", review.Sessions)
	}
	if review.Spec.OrderPolicy != dto.OrderPolicyUserMainDecided {
		t.Fatalf("review order policy = %q", review.Spec.OrderPolicy)
	}

	// 6) 角色管理：CRUD + 顺序设置（前端拖拽只提交 order_roles）。
	if _, err := proc.rpc(ctx, "team.put_role", map[string]any{
		"main_session_id": mainSessionID,
		"role": map[string]any{
			"role_name": "auditor", "role_kind": "agent",
			"order_priority": 5, "tools_policy": "readonly",
		},
	}); err != nil {
		t.Fatalf("team.put_role(auditor): %v", err)
	}
	orderRaw, err := proc.rpc(ctx, "team.set_order", map[string]any{
		"main_session_id": mainSessionID,
		"order_policy":    "user_main_decided",
		"order_roles":     []string{"user", "main", "reviewer", "auditor"},
	})
	if err != nil {
		t.Fatalf("team.set_order: %v", err)
	}
	var orderView dto.TeamView
	if err := json.Unmarshal(orderRaw, &orderView); err != nil {
		t.Fatalf("decode team.set_order: %v", err)
	}
	if strings.Join(orderView.OrderRoles, ",") != "user,main,reviewer,auditor" {
		t.Fatalf("order roles = %v", orderView.OrderRoles)
	}
	if len(orderView.DesignNotice) != 0 {
		notice = append(notice, orderView.DesignNotice...)
	}

	// 7) 设计稿不变量：定时 agent 不得进入 order_roles（必须显式报错）。
	if _, err := proc.rpc(ctx, "team.put_role", map[string]any{
		"main_session_id": mainSessionID,
		"role": map[string]any{
			"role_name": "digest", "role_kind": "timer", "join_policy": "scheduled",
		},
	}); err != nil {
		t.Fatalf("team.put_role(digest): %v", err)
	}
	if _, err := proc.rpc(ctx, "team.set_order", map[string]any{
		"main_session_id": mainSessionID,
		"order_policy":    "user_main_decided",
		"order_roles":     []string{"user", "main", "digest"},
	}); err == nil {
		t.Fatal("定时 agent 进入 order_roles 必须被拒绝（设计稿 §7.1）")
	}
	viewRaw, err := proc.rpc(ctx, "team.view", map[string]any{"main_session_id": mainSessionID})
	if err != nil {
		t.Fatalf("team.view: %v", err)
	}
	var view dto.TeamView
	if err := json.Unmarshal(viewRaw, &view); err != nil {
		t.Fatalf("decode team.view: %v", err)
	}
	teamLiveAssertNoSubagent(t, view)
	if len(view.Scheduled) != 1 || view.Scheduled[0].RoleName != "digest" || view.Scheduled[0].InOrder {
		t.Fatalf("定时分区 = %+v, want digest 且不在工作顺序", view.Scheduled)
	}

	// 8) 删除角色：从注册表与工作顺序里一起摘除。
	if _, err := proc.rpc(ctx, "team.delete_role", map[string]any{
		"main_session_id": mainSessionID, "role_name": "auditor",
	}); err != nil {
		t.Fatalf("team.delete_role: %v", err)
	}
	viewRaw, err = proc.rpc(ctx, "team.view", map[string]any{"main_session_id": mainSessionID})
	if err != nil {
		t.Fatalf("team.view(after delete): %v", err)
	}
	if err := json.Unmarshal(viewRaw, &view); err != nil {
		t.Fatalf("decode team.view(after delete): %v", err)
	}
	for _, name := range view.OrderRoles {
		if name == "auditor" {
			t.Fatalf("deleted role must leave order_roles: %v", view.OrderRoles)
		}
	}

	// 9) race/pprof 现场 + 进程仍可响应：确认无数据竞争、无死锁/卡死，并落报告。
	raceClean := true
	if stderr := proc.stderr.String(); strings.Contains(stderr, "DATA RACE") {
		raceClean = false
		t.Fatalf("race 检测到数据竞争:\n%s", stderr)
	}
	report := map[string]any{
		"main_session_id": mainSessionID,
		"preset_kinds":    keysOf(kinds),
		"goal_order":      first.View.OrderRoles,
		"review_order":    review.Spec.OrderRoles,
		"final_order":     view.OrderRoles,
		"scheduled":       view.Scheduled,
		"design_notice":   notice,
		"real_turn":       realTurn,
		"race_clean":      raceClean,
		"observed_at":     time.Now().Format(time.RFC3339),
	}
	if usePprof {
		forkLiveDumpGoroutines(t, pprofAddr, repoRoot, "team-live")
		roleLiveDumpMutex(t, pprofAddr, repoRoot)
		report["pprof_addr"] = pprofAddr
	}
	response, err := proc.client.Get(proc.base + "/healthz")
	if err != nil {
		t.Fatalf("healthz after team chain: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("healthz status after team chain = %d", response.StatusCode)
	}
	report["healthz"] = response.StatusCode
	teamLiveWriteReport(t, repoRoot, report)
	if len(notice) != 0 {
		t.Fatalf("链路出现不符合设计稿的偏差: %v", notice)
	}
}

// teamLiveAssertRealTurnLanded 用 target 角色的 role.snapshot 读主文档行，确认真实
// provider 轮次（assistant）已经落到 main message；顺带核对角色归属字段。
func teamLiveAssertRealTurnLanded(t *testing.T, ctx context.Context, proc *forkLiveProc, mainSessionID string) map[string]any {
	t.Helper()
	snapshot, err := roleLiveSnapshot(ctx, proc, mainSessionID, "tl", "goal-a2a-tl")
	if err != nil {
		t.Fatalf("role.snapshot(tl): %v", err)
	}
	assistant, user := 0, 0
	for _, row := range snapshot.MainRows {
		switch row.Role {
		case "assistant":
			assistant++
			if row.RoleName == "" {
				t.Fatalf("assistant 行缺角色归属（role_name 为空）: %+v", row)
			}
		case "user":
			user++
			if row.RoleName != "user" {
				t.Fatalf("用户输入行的 role_name 必须是 user: %+v", row)
			}
		}
	}
	if user == 0 || assistant == 0 {
		t.Fatalf("真实 API 轮次未落到主文档（user=%d assistant=%d）：provider 调用可能已失败，"+
			"本次冒烟不能算通过", user, assistant)
	}
	if snapshot.UnassignedRoleRows != 0 {
		t.Fatalf("主文档存在缺角色归属的行: %d", snapshot.UnassignedRoleRows)
	}
	// 本步只核对真实轮次；floor 由随后的一次 sync 负责产生（见 3.6），
	// 因此这里允许 floor 为空，但其它设计偏差仍必须为空。
	for _, warning := range snapshot.DesignWarnings {
		if strings.Contains(warning, "floor") {
			continue
		}
		t.Fatalf("存储层报告设计偏差: %v", snapshot.DesignWarnings)
	}
	return map[string]any{
		"user_rows":      user,
		"assistant_rows": assistant,
	}
}

// teamLiveSyncRoleDraft 走一次完整的角色 actor 路径：写自己的 draft → sequencer
// 同步进 main message（成功后 draft 即删）。
func teamLiveSyncRoleDraft(t *testing.T, ctx context.Context, proc *forkLiveProc, mainSessionID, roleName, roleSessionID string) {
	t.Helper()
	if _, err := proc.rpc(ctx, "role.append_draft", map[string]any{
		"main_session_id": mainSessionID, "role_name": roleName, "role_session_id": roleSessionID,
		"rows": []map[string]any{{
			"round_id": 1, "role_name": roleName, "role_session_id": roleSessionID, "unit_seq": 1,
			"event": map[string]any{"role": "assistant", "kind": "llm", "content": "TL-AGENTTEAM-SMOKE"},
		}},
	}); err != nil {
		t.Fatalf("role.append_draft: %v", err)
	}
	if _, err := proc.rpc(ctx, "role.sync_draft", map[string]any{
		"main_session_id": mainSessionID, "role_name": roleName, "role_session_id": roleSessionID,
		"order": []string{"user", "main", roleName},
	}); err != nil {
		t.Fatalf("role.sync_draft: %v", err)
	}
	rows, err := roleLiveDraftRows(ctx, proc, mainSessionID, roleName, roleSessionID)
	if err != nil {
		t.Fatalf("role.read_draft(after sync): %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("sync 成功后 draft 必须立即删除: %+v", rows)
	}
}

func teamLiveMaterialize(t *testing.T, ctx context.Context, proc *forkLiveProc, mainSessionID, teamKind string, joinSeq uint64) dto.TeamMaterializeResult {
	t.Helper()
	raw, err := proc.rpc(ctx, "team.materialize", map[string]any{
		"main_session_id": mainSessionID, "team_kind": teamKind, "join_seq_id": joinSeq,
	})
	if err != nil {
		t.Fatalf("team.materialize(%s): %v", teamKind, err)
	}
	var result dto.TeamMaterializeResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode team.materialize(%s): %v", teamKind, err)
	}
	return result
}

func teamLiveHasRole(sessions []dto.TeamRoleSession, roleName string) bool {
	for _, session := range sessions {
		if session.RoleName == roleName {
			return true
		}
	}
	return false
}

// teamLiveAssertNoSubagent：subagent 是 tool calling 能力，不得出现在成员表里。
func teamLiveAssertNoSubagent(t *testing.T, view dto.TeamView) {
	t.Helper()
	for _, member := range view.Members {
		if strings.HasPrefix(member.RoleName, "subagent") || member.RoleName == "sub" {
			t.Fatalf("subagent 不得进入 AgentTeam 成员表: %+v", view.Members)
		}
	}
}

func teamLiveWriteReport(t *testing.T, repoRoot string, report map[string]any) {
	t.Helper()
	dir := filepath.Join(repoRoot, "tmp", "headless-smoke", "reports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Logf("[report] 建目录失败: %v", err)
		return
	}
	path := filepath.Join(dir, fmt.Sprintf("team-live-%s.json", time.Now().Format("20060102-150405")))
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Logf("[report] 编码失败: %v", err)
		return
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Logf("[report] 写入失败: %v", err)
		return
	}
	t.Logf("[report] %s", path)
}

func keysOf(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}
