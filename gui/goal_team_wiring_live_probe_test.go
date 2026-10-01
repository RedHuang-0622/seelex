package gui

// TestRealAPIGoalTeamWiringLiveProbe 是 goal × AgentTeam 的真实 API 冒烟：物化主会话
// 后**显式装配**一份 TeamSpec（含一名不属于任何模板的 worker），再调 `goal.begin`，
// 然后要求 `team.view` **一字未变**——即"goal 上线不碰会话团队"。
//
// 为什么反转成这条（2026-10-01）：修前 goal 上线会按内置形态 goal-a2a 自动装配团队，
// 而装配 = 注册表 + lifecycle 顺序的**整份替换**，于是"开始一个 goal"会把用户手工加的
// 员工一起冲成模板那三个人。自动装配随形态目录一起删除后，这条探针守的是"别再把它接
// 回来"（单元级回归见 application/core 的 TestGoalBeginLeavesSessionTeamAlone）。
//
// 运行（真实 API，默认跳过；目标二进制需含 goal.* 与 team.* 接口）：
//
//	go build -tags pprof -o tmp/headless-smoke/seelex-pprof-team.exe .
//	$env:SMOKE_GOAL_TEAM_LIVE='1'; $env:SMOKE_GOAL_TEAM_LIVE_PPROF='1'
//	go test ./gui -run TestRealAPIGoalTeamWiringLiveProbe -v -count=1 -timeout 20m
//
// 可选 env：
//
//	SMOKE_GOAL_TEAM_LIVE_TARGET     目标二进制（默认 tmp/headless-smoke/seelex-pprof-team.exe）
//	SMOKE_GOAL_TEAM_LIVE_KEEP_STORE 置 1 保留临时数据根

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

func TestRealAPIGoalTeamWiringLiveProbe(t *testing.T) {
	if os.Getenv("SMOKE_GOAL_TEAM_LIVE") == "" {
		t.Skip("set SMOKE_GOAL_TEAM_LIVE=1 to run the real-API goal→team wiring probe")
	}
	repoRoot := forkLiveRepoRoot(t)
	usePprof := os.Getenv("SMOKE_GOAL_TEAM_LIVE_PPROF") == "1"
	defaultTarget := filepath.Join(repoRoot, "tmp", "bin", "seelex-headless.exe")
	if usePprof {
		defaultTarget = filepath.Join(repoRoot, "tmp", "headless-smoke", "seelex-pprof-team.exe")
	}
	target := forkLiveEnvPath("SMOKE_GOAL_TEAM_LIVE_TARGET", defaultTarget)
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("smoke target missing: %s（先构建含 goal.*/team.* 接口的 headless 二进制）", target)
	}
	storeDir, err := os.MkdirTemp("", "seelex-goal-team-live-")
	if err != nil {
		t.Fatalf("mk temp store: %v", err)
	}
	defer func() {
		if os.Getenv("SMOKE_GOAL_TEAM_LIVE_KEEP_STORE") == "" {
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
	if _, err := proc.rpc(ctx, "Submit", "这是 goal→团队接线冒烟。请只回复 OK，不要调用任何工具。"); err != nil {
		t.Fatalf("real API Submit: %v", err)
	}
	if _, err := proc.rpc(ctx, "WaitIdle", 300); err != nil {
		t.Fatalf("WaitIdle: %v", err)
	}
	mainSessionID := roleLiveSessionID(t, ctx, proc)
	if mainSessionID == "" {
		t.Fatal("main session did not materialize")
	}

	// 2) 显式装配一支团队：tl + worker（worker 是用户自己的人，不属于任何模板）。
	spec := dto.TeamSpec{
		TeamID: "goal-a2a", TeamKind: "goal-a2a", OrderPolicy: dto.OrderPolicyGoalLoop,
		Roles: []dto.RoleSpec{
			{RoleName: "user", RoleKind: dto.RoleKindUser},
			{RoleName: "main", RoleKind: dto.RoleKindMain},
			{RoleName: "tl", RoleKind: dto.RoleKindTechlead, OrderPriority: 1},
			{RoleName: "worker", RoleKind: dto.RoleKindAgent, OrderPriority: 2},
		},
	}
	if _, err := proc.rpc(ctx, "team.materialize", map[string]any{
		"main_session_id": mainSessionID, "spec": spec,
	}); err != nil {
		t.Fatalf("team.materialize: %v", err)
	}
	before, err := goalTeamLiveView(t, ctx, proc, mainSessionID)
	if err != nil {
		t.Fatalf("team.view(before goal): %v", err)
	}
	if !goalTeamLiveHasRole(before, "worker") {
		t.Fatalf("装配后应有 worker 成员: %+v", before.Members)
	}

	// 3) 只发 goal.begin —— 全程不再有任何 team.materialize 调用。
	if _, err := proc.rpc(ctx, "goal.begin", map[string]any{"title": "goal×团队接线冒烟"}); err != nil {
		t.Fatalf("goal.begin: %v", err)
	}

	// 4) goal 上线后团队必须**一字未变**：成员集、顺序与策略都不许被 goal 改写。
	after, err := goalTeamLiveView(t, ctx, proc, mainSessionID)
	if err != nil {
		t.Fatalf("team.view(after goal): %v", err)
	}
	if len(after.Members) != len(before.Members) {
		t.Fatalf("goal 上线改写了成员表：before=%d after=%d（%+v）", len(before.Members), len(after.Members), after.Members)
	}
	if strings.Join(after.OrderRoles, ",") != strings.Join(before.OrderRoles, ",") {
		t.Fatalf("goal 上线改写了工作顺序：before=%v after=%v", before.OrderRoles, after.OrderRoles)
	}
	if after.OrderPolicy != before.OrderPolicy {
		t.Fatalf("goal 上线改写了顺序策略：before=%q after=%q", before.OrderPolicy, after.OrderPolicy)
	}
	if !goalTeamLiveHasRole(after, "worker") {
		t.Fatalf("goal 上线把用户自己加的 worker 冲掉了: %+v", after.Members)
	}
	teamLiveAssertNoSubagent(t, after)
	t.Logf("[goal×team] goal 上线未触碰会话团队：order_policy=%s order_roles=%v members=%d",
		after.OrderPolicy, after.OrderRoles, len(after.Members))
}

func goalTeamLiveView(t *testing.T, ctx context.Context, proc *forkLiveProc, mainSessionID string) (dto.TeamView, error) {
	t.Helper()
	raw, err := proc.rpc(ctx, "team.view", map[string]any{"main_session_id": mainSessionID})
	if err != nil {
		return dto.TeamView{}, err
	}
	var view dto.TeamView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("decode team.view: %v", err)
	}
	return view, nil
}

func goalTeamLiveHasRole(view dto.TeamView, roleName string) bool {
	for _, member := range view.Members {
		if member.RoleName == roleName {
			return true
		}
	}
	return false
}
