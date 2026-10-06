package seelebridge

// worktree_chain_live_test.go — **真机验收**：子代理那条链的「现场生命周期」端到端。
//
// 为什么需要它（既有用例覆盖不到的那一段）：仓库里已有的真 git 用例分得很细——
//   - `worktree_test.go` 手动驱动 beginNodeWorktree/Finish（**不经过真正的计划执行**）；
//   - `worktree/*_test.go` 用脚本化 git 打桩（**不碰真 git**）；
//   - `fork_live_smoke_test.go` 走真 API，但**只断言取回的产出文本**，不看现场。
// 于是"派发 → 建现场 → 现场上真干活 → 收尾 rebase/merge 回主工作区 → 按成功路径析构"
// 这一整条，**没有任何一条用例在一次运行里同时验过**。本文件补的就是它：
// 真 API（模型自己决定怎么干活）+ 真 git（真建现场、真合并）+ 逐帧取证。
//
// 运行（真实 API，默认跳过；会消耗额度，约 1-3 分钟一轮）：
//
//	$env:SEELEX_LIVE_SMOKE='1'
//	go test ./seelebridge -run TestLiveSubagentWorktreeChain -v -count=1 -timeout 20m
//
// 可选 env：SEELEX_ACCOUNTS_PATH（默认 ../config/accounts.yaml）、SEELEX_LIVE_KEEP_SCENE=1。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/workplan/sugar/approve"
	"github.com/RedHuang-0622/seelex/seelebridge/worktree"
)

// liveApproveGate 是收尾合并审批门的记录器：它被问到，就证明确实在工作区里**提交过**
// 东西（Finish 只在 `commits > 0` 时才问审批），问题正文里带着 `git diff --stat` 的
// 原文——这就是"现场上真干了活"的第一手证据。
type liveApproveGate struct {
	mu       sync.Mutex
	asked    []string
	contents []string
	choice   string
}

func (g *liveApproveGate) Ask(_ context.Context, question approve.Question) (any, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.asked = append(g.asked, question.ID)
	g.contents = append(g.contents, question.Content)
	choice := g.choice
	if choice == "" {
		choice = "approve"
	}
	return choice, nil
}

func (g *liveApproveGate) snapshot() (ids []string, contents []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.asked...), append([]string(nil), g.contents...)
}

// liveSceneFrames 是运行期对现场的逐帧取证（plan_run 阻塞期间由探针轮询）。
type liveSceneFrames struct {
	mu             sync.Mutex
	registered     bool
	path           string
	fileInScene    bool
	fileInMain     bool
	pathSeenInMain bool
	samples        int
}

func (f *liveSceneFrames) record(nodeID string, repo string, fileName string) {
	// 只读轮询：读注册表 + 读两个路径上有没有那个文件（"工作落在现场、没落在主工作区"）。
	_, registered := liveSceneRegistry.Info(nodeID)
	path := ""
	if info, ok := liveSceneRegistry.Info(nodeID); ok {
		path = info.Path
	}
	inScene := false
	inMain := false
	if path != "" {
		if _, err := os.Stat(filepath.Join(path, fileName)); err == nil {
			inScene = true
		}
	}
	if _, err := os.Stat(filepath.Join(repo, fileName)); err == nil {
		inMain = true
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.samples++
	if registered {
		f.registered = true
	}
	if path != "" {
		f.path = path
	}
	if inScene {
		f.fileInScene = true
	}
	if inMain {
		f.fileInMain = true
		if registered {
			// 「现场在册 + 主工作区已有」= 合并已经发生（或模型直接写在了 main）。
			f.pathSeenInMain = true
		}
	}
}

func (f *liveSceneFrames) snapshot() (registered, fileInScene, fileInMain bool, path string, samples int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.registered, f.fileInScene, f.fileInMain, f.path, f.samples
}

// liveSceneRegistry 是探针读注册表用的句柄（见 TestLiveSubagentWorktreeChain 的赋值）。
// 放在包级是为了让 record 保持无状态：探针本身不改任何运行时行为，只读。
var liveSceneRegistry interface {
	Info(nodeID string) (NodeWorktreeInfo, bool)
}

func liveSubagentAccountsPath() string {
	if path := strings.TrimSpace(os.Getenv("SEELEX_ACCOUNTS_PATH")); path != "" {
		return path
	}
	return filepath.Join("..", "config", "accounts.yaml")
}

// TestLiveSubagentWorktreeChain 端到端验一条链：派发 → 现场 → 现场上真干活 →
// 收尾 rebase/merge 回主工作区 → 成功路径析构（目录 + 分支 + 登记三者都清）。
//
// 硬断言（有一条不成立就是产品问题，不是"模型没照做"）：
//  1. 现场在运行期**确实被建出来**且落在主工作区之外（隔离）；
//  2. 模型产出的文件**先出现在现场里**，此时主工作区**还没有**它；
//  3. 收尾**问过合并审批**（= 现场里提交过东西），且 diffstat 里带着该文件；
//  4. 合并后主工作区**拿到了**那个文件与那条提交（rebase/merge 回得来）；
//  5. 成功后现场**被析构**：目录没了、分支没了、登记没了（成功路径的销项策略）。
func TestLiveSubagentWorktreeChain(t *testing.T) {
	if os.Getenv("SEELEX_LIVE_SMOKE") == "" {
		t.Skip("set SEELEX_LIVE_SMOKE=1 to run the real-API worktree chain acceptance")
	}
	accountsPath := liveSubagentAccountsPath()
	if _, err := os.Stat(accountsPath); err != nil {
		t.Fatalf("accounts 不在：%s（用 SEELEX_ACCOUNTS_PATH 指定）", accountsPath)
	}

	repo := setupGitRepo(t)
	const nodeID = "impl"
	marker := fmt.Sprintf("SEELEX-LIVE-%d", time.Now().Unix()%1000000)
	fileName := "live-acceptance.txt"

	runtime, err := NewRuntime(RuntimeConfig{
		AccountsPath:      accountsPath,
		ToolCallTimeout:   5 * time.Minute,
		ApprovalTimeout:   10 * time.Minute,
		HeartbeatInterval: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	if err := runtime.BindProjectRoot(repo); err != nil {
		t.Fatalf("BindProjectRoot: %v", err)
	}
	// plan_load / plan_run 是 goal 技能面里的工具：不打开这一格它们不在可见面上
	// （首次跑出的红就是 `tool "plan_load": tool is not visible for this request`）。
	runtime.SetRuntimeVisibilityProjection(RuntimeVisibilityProjection{GoalSkillActive: true})
	gate := &liveApproveGate{}
	runtime.SetPlanApprovalGate(gate)
	liveSceneRegistry = runtime.worktreeMgr

	goal := fmt.Sprintf(
		"在仓库根目录创建文件 %s，内容就是一行：%s\n"+
			"然后按收尾协议执行 git add -A && git commit -m \"live acceptance %s\"。\n"+
			"只做这一件事，不要碰别的文件，不要跑测试；完成后用一句话汇报。",
		fileName, marker, marker)
	planJSON := fmt.Sprintf(`{"entry":%q,"nodes":{%q:{"input":%q,"kind":"agent"}},"edges":{}}`,
		nodeID, nodeID, goal)
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", planJSON); err != nil {
		t.Fatalf("plan_load: %v", err)
	}

	frames := &liveSceneFrames{}
	stop := make(chan struct{})
	probeDone := make(chan struct{})
	go func() {
		defer close(probeDone)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				frames.record(nodeID, repo, fileName)
			}
		}
	}()

	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	out, runErr := runtime.Agent().DirectDispatch(ctx, "plan_run", `{}`)
	close(stop)
	<-probeDone
	elapsed := time.Since(started)

	t.Logf("=== plan_run 耗时 %s ===", elapsed)
	t.Logf("plan_run 输出：%s", out)

	// ── ① 运行期现场确实建出来了 ──────────────────────────────────────
	registered, fileInScene, _, scenePath, samples := frames.snapshot()
	if !registered {
		t.Fatalf("现场从未在注册表出现（派发/建现场这一跳没发生）；plan_run=%v err=%v 输出=%s", samples, runErr, out)
	}
	t.Logf("现场在册：%v  路径=%s  采样=%d", registered, scenePath, samples)
	if scenePath != "" {
		if rel, relErr := filepath.Rel(repo, scenePath); relErr == nil && !strings.HasPrefix(rel, "..") {
			t.Fatalf("现场必须落在主工作区之外（隔离），实际在 %s 之内：%s", repo, scenePath)
		}
	}

	// ── ② 工作确实落在现场里 ──────────────────────────────────────────
	if !fileInScene {
		t.Fatalf("现场里从未见到 %s——子代理的产出没有落在自己的现场上（现场=%s，输出=%s）", fileName, scenePath, out)
	}
	t.Logf("产出落在现场：%v（现场=%s）", fileInScene, scenePath)

	// ── ③ 收尾问过合并审批（= 现场里提交过），diffstat 里有那个文件 ────
	ids, contents := gate.snapshot()
	if len(ids) == 0 {
		t.Fatalf("收尾从未问过合并审批：说明现场里 commits == 0（收尾协议未执行或根本没干活）；输出=%s", out)
	}
	t.Logf("合并审批：%v\n%s", ids, strings.Join(contents, "\n---\n"))
	joined := strings.Join(contents, "\n")
	if !strings.Contains(joined, fileName) {
		t.Errorf("合并审批的 diffstat 里应看到 %s（现场上真干活的证据），实际：%s", fileName, joined)
	}

	// ── ④ 合并回主工作区 ─────────────────────────────────────────────
	content, readErr := os.ReadFile(filepath.Join(repo, fileName))
	if readErr != nil {
		t.Fatalf("主工作区必须拿到产出 %s（rebase/merge 没合回来）：%v；plan_run=%v 输出=%s",
			fileName, readErr, runErr, out)
	}
	if !strings.Contains(string(content), marker) {
		t.Fatalf("主工作区 %s 的内容不对：%q（期望含 %s）", fileName, string(content), marker)
	}
	logOut, logErr := worktree.GitRunner(repo, "log", "--oneline", "-5")
	if logErr != nil {
		t.Fatalf("git log: %v", logErr)
	}
	t.Logf("主工作区 git log：\n%s", logOut)
	combined := logOut + firstLine(out)
	if !strings.Contains(combined, marker) {
		t.Errorf("主工作区分支上应能看到这条提交/这次工作（%s），实际 log=%q 输出=%q", marker, logOut, firstLine(out))
	}

	// ── ⑤ 成功路径析构：目录 + 分支 + 登记三者都清 ────────────────────
	if scenePath != "" {
		if _, err := os.Stat(scenePath); err == nil {
			t.Errorf("成功合并后现场目录必须被析构，实际还在：%s", scenePath)
		}
	}
	if info, ok := runtime.worktreeMgr.Info(nodeID); ok {
		t.Errorf("成功合并后不得残留登记：%+v", info)
	}
	if left := runtime.worktreeMgr.RegisteredCount(); left != 0 {
		t.Errorf("成功收尾后注册表应为空，实际 %d 条", left)
	}
	if branchOut, err := worktree.GitRunner(repo, "branch", "--list", "seelex/"+nodeID); err == nil {
		if strings.TrimSpace(branchOut) != "" {
			t.Errorf("成功合并后现场分支必须删掉，实际还剩：%s", branchOut)
		}
	}
}

func firstLine(text string) string {
	if index := strings.Index(text, "\n"); index >= 0 {
		return text[:index]
	}
	return text
}
