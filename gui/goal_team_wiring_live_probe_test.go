package gui

// TestRealAPIGoalTeamWiringLiveProbe 是 goal → AgentTeam 自动接线的真实 API
// 冒烟：物化主会话后**只调 `goal.begin`**（不调用任何 `team.materialize`），
// 然后要求 `team.view` 已经能看到 tl 成员与 goal_loop 顺序——即"goal 上线即
// 装配 goal-a2a 团队"（TL 的 JoinPolicy=on_goal_create）在真实链路上成立。
//
// 这条探针专门覆盖 2026-09-11 新增的隐式接线：既有 `TestRealAPIAgentTeamLiveProbe`
// 走的是显式装配，覆盖不到它。
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

	// 2) 前置断言：goal 之前不该已有 tl，否则这条探针证明不了"自动装配"。
	// 未装配时 team.view 会因缺 lifecycle.json 报错——这正是"还没有团队"。
	before, beforeErr := goalTeamLiveView(t, ctx, proc, mainSessionID)
	if beforeErr != nil {
		t.Logf("[goal→team] goal 前 team.view 尚不可用（未装配，符合预期）: %v", beforeErr)
	} else if goalTeamLiveHasRole(before, "tl") {
		t.Fatalf("goal 之前 tl 已存在，本探针无法证明自动装配: %+v", before.Members)
	}

	// 3) 只发 goal.begin —— 全程没有任何 team.materialize 调用。
	if _, err := proc.rpc(ctx, "goal.begin", map[string]any{"title": "goal→团队接线冒烟"}); err != nil {
		t.Fatalf("goal.begin: %v", err)
	}

	// 4) goal 上线后，团队必须已自动装配：tl 成员 + goal_loop 顺序，且不含 subagent。
	after, err := goalTeamLiveView(t, ctx, proc, mainSessionID)
	if err != nil {
		t.Fatalf("team.view(after goal): %v", err)
	}
	if !goalTeamLiveHasRole(after, "tl") {
		t.Fatalf("goal 创建后未见 tl 成员（自动装配未生效）: %+v", after.Members)
	}
	if after.OrderPolicy != dto.OrderPolicyGoalLoop {
		t.Fatalf("order_policy = %q, want %q", after.OrderPolicy, dto.OrderPolicyGoalLoop)
	}
	teamLiveAssertNoSubagent(t, after)
	t.Logf("[goal→team] 自动装配成功：tl 已上线，order_policy=%s order_roles=%v members=%d",
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
