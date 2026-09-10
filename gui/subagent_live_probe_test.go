package gui

// TestRealAPISubagentResumeLiveProbe 是 A 流（subagent 中断后恢复续跑）的真实
// API 冒烟：
//
//	阶段 1：真实 API 物化主会话 → 经 subagent.fork（与模型调用
//	        fork_subagents 同一条执行链）派发一个长任务子代理 → 观察到
//	        表格里 active 的残留记录后直接杀掉进程（模拟中断）；
//	阶段 2：用同一数据根冷启动 → subagent.list 看到可续跑残留 →
//	        subagent.recover 走七步模板续跑 → 记录收敛、结论唯一；
//	        同时抓 goroutine/mutex/block pprof，并检查 stderr 无 DATA RACE。
//
// 运行（真实 API，默认跳过；目标二进制需包含 subagent.* 接口）：
//
//	go build -tags pprof -o tmp/headless-smoke/seelex-pprof-subagent.exe .
//	$env:SMOKE_SUBAGENT_LIVE='1'
//	$env:SMOKE_SUBAGENT_LIVE_PPROF='1'
//	go test ./gui -run TestRealAPISubagentResumeLiveProbe -v -count=1 -timeout 25m
//
// 可选 env：
//
//	SMOKE_SUBAGENT_LIVE_TARGET       目标二进制（默认 tmp/bin/seelex-headless.exe）
//	SMOKE_SUBAGENT_LIVE_GOAL         子代理目标（默认一段长文任务）
//	SMOKE_SUBAGENT_LIVE_WAIT_SEC     等待 active 残留记录的上限（默认 300）
//	SMOKE_SUBAGENT_LIVE_HOLD_MS     节点注册后的测试 hold（默认 30000）
//	SMOKE_SUBAGENT_LIVE_KEEP_STORE   置 1 保留临时数据根

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

func TestRealAPISubagentResumeLiveProbe(t *testing.T) {
	if os.Getenv("SMOKE_SUBAGENT_LIVE") == "" {
		t.Skip("set SMOKE_SUBAGENT_LIVE=1 to run the real-API subagent resume live probe")
	}
	repoRoot := forkLiveRepoRoot(t)
	usePprof := os.Getenv("SMOKE_SUBAGENT_LIVE_PPROF") == "1"
	defaultTarget := filepath.Join(repoRoot, "tmp", "bin", "seelex-headless.exe")
	if usePprof {
		defaultTarget = filepath.Join(repoRoot, "tmp", "headless-smoke", "seelex-pprof-subagent.exe")
	}
	target := forkLiveEnvPath("SMOKE_SUBAGENT_LIVE_TARGET", defaultTarget)
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("smoke target missing: %s（先构建含 subagent.* 接口的 headless 二进制）", target)
	}
	storeDir, err := os.MkdirTemp("", "seelex-subagent-live-")
	if err != nil {
		t.Fatalf("mk temp store: %v", err)
	}
	defer func() {
		if os.Getenv("SMOKE_SUBAGENT_LIVE_KEEP_STORE") == "" {
			_ = os.RemoveAll(storeDir)
		} else {
			t.Logf("[store] 保留现场: %s", storeDir)
		}
	}()
	// 请求记录（role + 内容指纹，不含正文）：用于核对续跑轮次的 wire 形状。
	requestLog := filepath.Join(storeDir, "requests.jsonl")
	t.Setenv("SEELEX_REQUEST_LOG", requestLog)
	// 子代理可能在很短时间内完成；测试窗口内保持节点注册态，确保外部驱动
	// 能稳定观测 active 再杀进程。生产默认不设置该变量。
	t.Setenv("SEELEX_SUBAGENT_HOLD_MS", strconv.Itoa(forkLiveEnvInt("SMOKE_SUBAGENT_LIVE_HOLD_MS", 30000)))

	waitBudget := time.Duration(forkLiveEnvInt("SMOKE_SUBAGENT_LIVE_WAIT_SEC", 300)) * time.Second
	forkGoal := strings.TrimSpace(os.Getenv("SMOKE_SUBAGENT_LIVE_GOAL"))
	if forkGoal == "" {
		forkGoal = "分章节写一篇 1200 字以上的中文长文，主题是会话存储引擎的取舍；写完后用一句话总结。不要调用其它工具。"
	}
	forkNodeID := "smoke-sub-1"

	// ── 阶段 1：真实 API 派发子代理，并在执行中终止进程 ──────────────────
	port1 := forkLiveFreePort(t)
	pprof1 := ""
	if usePprof {
		pprof1 = "127.0.0.1:" + forkLiveFreePort(t)
	}
	proc1 := forkLiveSpawn(t, target, repoRoot, storeDir, port1, pprof1)
	defer proc1.stop()
	ctx1, cancel1 := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel1()
	proc1.waitHealthy(ctx1, t, time.Now().Add(90*time.Second))

	if _, err := proc1.rpc(ctx1, "Submit", "这是 subagent 中断恢复冒烟的准备步骤。请只回复 OK，不要调用任何工具。"); err != nil {
		t.Fatalf("real API Submit(warmup): %v", err)
	}
	if _, err := proc1.rpc(ctx1, "WaitIdle", 300); err != nil {
		t.Fatalf("WaitIdle(warmup): %v", err)
	}
	mainSessionID := roleLiveSessionID(t, ctx1, proc1)
	if mainSessionID == "" {
		t.Fatal("main session did not materialize")
	}
	// 确定性派发：异步发起（该调用会一直阻塞到 fork DAG 跑完），随后用表格
	// 轮询捕获运行中的残留记录。
	forkDone := make(chan error, 1)
	go func() {
		_, err := proc1.rpc(ctx1, "subagent.fork", mainSessionID, []map[string]any{
			{"id": forkNodeID, "goal": forkGoal},
		})
		forkDone <- err
	}()
	running, err := subagentLiveWaitActive(ctx1, proc1, mainSessionID, time.Now().Add(waitBudget), forkDone)
	if err != nil {
		t.Fatalf("等待 active 子代理记录: %v（子代理未派发或已跑完；可用 SMOKE_SUBAGENT_LIVE_GOAL 调整）", err)
	}
	t.Logf("[phase1] 观察到 active 子代理: node=%s status=%s goal=%.40s",
		running.NodeID, running.Status, running.Goal)
	interruptedNode := running.NodeID
	proc1.stop() // 硬中断：模拟进程在结果写入前终止
	subagentLiveClearStaleLock(storeDir)

	// ── 阶段 2：同一数据根冷启动 → 列表 → 续跑 ──────────────────────────
	port2 := forkLiveFreePort(t)
	pprof2 := ""
	if usePprof {
		pprof2 = "127.0.0.1:" + forkLiveFreePort(t)
	}
	proc2 := forkLiveSpawn(t, target, repoRoot, storeDir, port2, pprof2)
	defer proc2.stop()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel2()
	proc2.waitHealthy(ctx2, t, time.Now().Add(90*time.Second))
	if _, err := proc2.rpc(ctx2, "ResumeSession", mainSessionID); err != nil {
		t.Logf("[phase2] ResumeSession 告警（不致命）: %v", err)
	}

	views, err := subagentLiveList(ctx2, proc2, mainSessionID)
	if err != nil {
		t.Fatalf("subagent.list: %v", err)
	}
	if len(views) == 0 {
		t.Fatalf("冷启动后必须看到中断残留的子代理记录（table active）")
	}
	resumable := 0
	seen := false
	for _, view := range views {
		if view.NodeID == interruptedNode {
			seen = true
			if view.Resumable {
				resumable++
			}
		}
	}
	if !seen || resumable == 0 {
		t.Fatalf("残留记录未判定为可续跑: %+v (want node=%s)", views, interruptedNode)
	}

	report, err := subagentLiveRecover(ctx2, proc2, mainSessionID)
	if err != nil {
		t.Fatalf("subagent.recover: %v", err)
	}
	if report.RecoveryNoteRole != "system" {
		t.Fatalf("恢复说明 provider role = %q, want system", report.RecoveryNoteRole)
	}
	if len(report.Failed) != 0 {
		t.Fatalf("续跑失败: %+v", report)
	}
	resumed := false
	for _, unit := range report.Units {
		if unit.NodeID != interruptedNode {
			continue
		}
		resumed = unit.Resumed || unit.Skipped
		if unit.Resumed && unit.NoteRole != "system" {
			t.Fatalf("续跑单元必须报告 system 恢复说明: %+v", unit)
		}
	}
	if !resumed {
		t.Fatalf("续跑报告未覆盖中断单元 %s: %+v", interruptedNode, report)
	}
	if len(report.Resumed) == 0 {
		t.Logf("[phase2] 本轮未重启（可能已被复用收敛）: %+v", report)
	}

	converged, err := subagentLiveWaitConverged(ctx2, proc2, mainSessionID, 5*time.Minute)
	if err != nil {
		t.Fatalf("等待收敛: %v", err)
	}
	if !converged {
		remaining, _ := subagentLiveList(ctx2, proc2, mainSessionID)
		t.Fatalf("续跑后残留记录未收敛: %+v", remaining)
	}

	out := map[string]any{
		"main_session_id":   mainSessionID,
		"interrupted_node":  interruptedNode,
		"phase1_view":       running,
		"phase2_report":     report,
		"resumed":           report.Resumed,
		"converged":         converged,
		"note_role":         report.RecoveryNoteRole,
		"request_log":       requestLog,
		"request_log_lines": subagentLiveRequestLogLines(requestLog),
		"observed_at":       time.Now().Format(time.RFC3339),
	}
	if stderr := proc2.stderr.String(); strings.Contains(stderr, "DATA RACE") {
		out["race_clean"] = false
		t.Fatalf("race 检测到数据竞争:\n%s", stderr)
	}
	out["race_clean"] = true
	if usePprof {
		forkLiveDumpGoroutines(t, pprof2, repoRoot, "subagent-live")
		roleLiveDumpMutex(t, pprof2, repoRoot)
		out["pprof_addr"] = pprof2
	}
	response, err := proc2.client.Get(proc2.base + "/healthz")
	if err != nil {
		t.Fatalf("healthz after resume chain: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("healthz status after resume chain = %d", response.StatusCode)
	}
	out["healthz"] = response.StatusCode
	subagentLiveWriteReport(t, repoRoot, out)
}

// subagentLiveWaitActive 轮询 subagent.list，等待出现未终结的残留记录。
func subagentLiveWaitActive(ctx context.Context, proc *forkLiveProc, sessionID string, deadline time.Time, forkDone <-chan error) (dto.SubagentRecoveryView, error) {
	var last []dto.SubagentRecoveryView
	var lastErr error
	for time.Now().Before(deadline) {
		select {
		case err := <-forkDone:
			if err != nil {
				return dto.SubagentRecoveryView{}, fmt.Errorf("subagent.fork failed before active record: %w", err)
			}
			return dto.SubagentRecoveryView{}, fmt.Errorf("subagent.fork returned before any active record was observed (last views = %+v; last list error = %v)", last, lastErr)
		default:
		}
		views, err := subagentLiveList(ctx, proc, sessionID)
		if err == nil {
			last = views
			for _, view := range views {
				if view.Active {
					return view, nil
				}
			}
		} else {
			lastErr = err
		}
		time.Sleep(500 * time.Millisecond)
	}
	if lastErr != nil {
		return dto.SubagentRecoveryView{}, fmt.Errorf("timeout: last views = %+v; list error = %w", last, lastErr)
	}
	return dto.SubagentRecoveryView{}, fmt.Errorf("timeout: last views = %+v", last)
}

// subagentLiveWaitConverged 轮询直到表格里不再有残留单元。
func subagentLiveWaitConverged(ctx context.Context, proc *forkLiveProc, sessionID string, budget time.Duration) (bool, error) {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		views, err := subagentLiveList(ctx, proc, sessionID)
		if err != nil {
			return false, err
		}
		if len(views) == 0 {
			return true, nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false, nil
}

func subagentLiveList(ctx context.Context, proc *forkLiveProc, sessionID string) ([]dto.SubagentRecoveryView, error) {
	raw, err := proc.rpc(ctx, "subagent.list", sessionID)
	if err != nil {
		return nil, err
	}
	var views []dto.SubagentRecoveryView
	if err := json.Unmarshal(raw, &views); err != nil {
		return nil, fmt.Errorf("decode subagent.list: %w", err)
	}
	return views, nil
}

func subagentLiveRecover(ctx context.Context, proc *forkLiveProc, sessionID string) (dto.SubagentResumeReport, error) {
	raw, err := proc.rpc(ctx, "subagent.recover", sessionID)
	if err != nil {
		return dto.SubagentResumeReport{}, err
	}
	var report dto.SubagentResumeReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return dto.SubagentResumeReport{}, fmt.Errorf("decode subagent.recover: %w", err)
	}
	return report, nil
}

// subagentLiveRequestLogLines 返回请求日志行数（不解析正文，避免泄漏会话内容）。
func subagentLiveRequestLogLines(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return strings.Count(strings.TrimSpace(string(data)), "\n") + 1
}

func subagentLiveWriteReport(t *testing.T, repoRoot string, report map[string]any) {
	t.Helper()
	dir := filepath.Join(repoRoot, "tmp", "headless-smoke", "reports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Logf("[report] 建目录失败: %v", err)
		return
	}
	path := filepath.Join(dir, fmt.Sprintf("subagent-live-%s.json", time.Now().Format("20060102-150405")))
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

// subagentLiveClearStaleLock 清理被硬中断进程遗留的数据根锁，等价于运维
// 在确认进程已死后手动移除 stale lock。只作用于本测试创建的临时 store。
func subagentLiveClearStaleLock(storeDir string) {
	for _, name := range []string{
		filepath.Join(storeDir, "sessions-json", "lock.owner"),
		filepath.Join(storeDir, "sessions", ".lock"),
	} {
		if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
			// best-effort：后续启动会显式报出锁问题。
		}
	}
}
