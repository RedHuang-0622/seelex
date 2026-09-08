package gui

// TestRealAPIForkLiveProbe 是“顶层链路”的真实 API fork 探针（包内测试，
// 2026-09-08 自 tmp/headless-smoke 迁入）：启动真实 headless GUI 进程
// （SEELEX_HEADLESS_PORT 控制面，与桌面 GUI 同一 Application 装配），经
// /rpc Submit 提交一条消息，主代理 ReAct 调用 fork_subagents 并行派生 N 个
// agent 节点（1+1 / 自定义题均可，SMOKE_FORK_LIVE_GOAL 控制目标文案），
// 逐秒轮询快照验证：
//   - 行状态 queued → running → completed，Assignee 由 main 切换为
//     subagent:<node会话ID>；
//   - 树节点全部 done、会话收敛（Chat.Running=false）、fork 工具 success；
//   - 高并发（默认 SMOKE_FORK_LIVE_PPROF 可开 pprof）无死锁/卡死。
//
// 运行（真实 API，非默认执行；目标二进制默认 tmp/bin/seelex-headless.exe）：
//   $env:SMOKE_FORK_LIVE='1'
//   go test ./gui -run TestRealAPIForkLiveProbe -v -count=1 -timeout 20m
//
// 可选 env：
//   SMOKE_FORK_LIVE_N               子代理个数（默认 3）
//   SMOKE_FORK_LIVE_GOAL            每个子代理目标文案模板；{i} 替换为序号，
//                                    自动追加 (agent fk<i>) 保持 goal 唯一
//   SMOKE_FORK_LIVE_PREAMBLE        追加在提交消息前的顶层指令（例如激活
//                                    #goal 技能、指定调研对象）
//   SMOKE_FORK_LIVE_BIND            置 1 时先 CreateWorkspace(repoRoot)+Bind，
//                                    模拟 GUI 项目绑定后 fork（文件工具可用）
//   SMOKE_FORK_LIVE_IDLE_BUDGET_SEC 等待收敛预算（默认 240）
//   SMOKE_FORK_LIVE_PPROF           置 1 使用 seelex-pprof.exe 并支持 goroutine dump
//   SMOKE_FORK_LIVE_TARGET          目标二进制（默认 tmp/bin/seelex-headless.exe）
//   SMOKE_KEEP_STORE                置 1 保留临时 store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/model"
)

type forkLiveRowState struct {
	ID           string   `json:"id"`
	Status       string   `json:"status"`
	Assignee     string   `json:"assignee,omitempty"`
	Participants []string `json:"participants,omitempty"`
	Trace        int      `json:"trace_count"`
}

type forkLiveTreeNode struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	SessionID string `json:"session_id,omitempty"`
}

type forkLiveSample struct {
	At      string             `json:"at"`
	Running bool               `json:"running"`
	Rows    []forkLiveRowState `json:"rows"`
	Tree    []forkLiveTreeNode `json:"tree"`
}

type forkLiveReport struct {
	Passed         bool             `json:"passed"`
	Scenario       string           `json:"scenario"`
	WallMS         int64            `json:"wall_ms"`
	PeakRunning    int              `json:"peak_running"`
	ChatErr        string           `json:"chat_error,omitempty"`
	ForkTool       *model.ToolCall  `json:"fork_tool,omitempty"`
	RowEndCounts   map[string]int   `json:"row_end_counts,omitempty"`
	TreeEndCounts  map[string]int   `json:"tree_end_counts,omitempty"`
	StderrWarnings []string         `json:"stderr_warnings,omitempty"`
	Samples        []forkLiveSample `json:"samples"`
	Final          model.Snapshot   `json:"final_snapshot,omitempty"`
	Stall          bool             `json:"stall,omitempty"`
	Notes          []string         `json:"notes,omitempty"`
}

// forkLiveRPCResponse 是 headless /rpc 的本地解码形状（Result 保留原文）。
type forkLiveRPCResponse struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

type forkLiveProc struct {
	base   string
	client *http.Client
	cmd    *exec.Cmd
	exit   chan error
	stderr bytes.Buffer
}

func TestRealAPIForkLiveProbe(t *testing.T) {
	if os.Getenv("SMOKE_FORK_LIVE") == "" {
		t.Skip("set SMOKE_FORK_LIVE=1 to run the real-API fork live probe")
	}
	repoRoot := forkLiveRepoRoot(t)
	n := forkLiveEnvInt("SMOKE_FORK_LIVE_N", 3)
	idleBudget := time.Duration(forkLiveEnvInt("SMOKE_FORK_LIVE_IDLE_BUDGET_SEC", 240)) * time.Second
	goalTemplate := os.Getenv("SMOKE_FORK_LIVE_GOAL")
	preamble := os.Getenv("SMOKE_FORK_LIVE_PREAMBLE")
	usePprof := os.Getenv("SMOKE_FORK_LIVE_PPROF") == "1"
	defaultTarget := filepath.Join(repoRoot, "tmp", "bin", "seelex-headless.exe")
	if usePprof {
		defaultTarget = filepath.Join(repoRoot, "tmp", "headless-smoke", "seelex-pprof.exe")
	}
	target := forkLiveEnvPath("SMOKE_FORK_LIVE_TARGET", defaultTarget)
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("smoke target missing: %s", target)
	}
	storeDir, err := os.MkdirTemp("", "seelex-fork-live-")
	if err != nil {
		t.Fatalf("mk temp store: %v", err)
	}
	defer func() {
		if os.Getenv("SMOKE_KEEP_STORE") == "" {
			_ = os.RemoveAll(storeDir)
		} else {
			t.Logf("[store] 保留现场: %s", storeDir)
		}
	}()

	port := forkLiveFreePort(t)
	var pprofAddr string
	if usePprof {
		pprofAddr = "127.0.0.1:" + forkLiveFreePort(t)
	}
	proc := forkLiveSpawn(t, target, repoRoot, storeDir, port, pprofAddr)
	defer proc.stop()
	ctx, cancel := context.WithTimeout(context.Background(), idleBudget+90*time.Second)
	defer cancel()
	proc.waitHealthy(ctx, t, time.Now().Add(90*time.Second))
	if os.Getenv("SMOKE_FORK_LIVE_BIND") == "1" {
		if _, err := proc.rpc(ctx, "CreateWorkspace", "fork-live-root", repoRoot, ""); err != nil {
			t.Fatalf("CreateWorkspace: %v", err)
		}
		snap, err := proc.snapshot(ctx)
		if err != nil {
			t.Fatalf("snapshot after workspace: %v", err)
		}
		workspaceID := ""
		for _, workspace := range snap.Workspaces {
			if filepath.Clean(workspace.RootPath) == filepath.Clean(repoRoot) {
				workspaceID = workspace.ID
				break
			}
		}
		if workspaceID == "" {
			t.Fatal("CreateWorkspace 后未找到仓库工作区")
		}
		if _, err := proc.rpc(ctx, "BindWorkspace", workspaceID); err != nil {
			t.Fatalf("BindWorkspace: %v", err)
		}
		t.Log("[bind] 已绑定仓库工作区")
	}

	var prompt strings.Builder
	if preamble != "" {
		prompt.WriteString(preamble)
		prompt.WriteString("\n")
	}
	prompt.WriteString("Call the fork_subagents tool EXACTLY once to run ")
	prompt.WriteString(strconv.Itoa(n))
	prompt.WriteString(" subagents in parallel, wait for all of them, then reply with a one-line summary. ")
	prompt.WriteString("Do NOT use plan_load/plan_run or any other tool. Subagents:")
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("fk%d", i+1)
		goal := fmt.Sprintf("Reply with the single number %d and stop; do not call tools.", 2*(i+1))
		if goalTemplate != "" {
			goal = strings.ReplaceAll(goalTemplate, "{i}", strconv.Itoa(i+1))
			goal += fmt.Sprintf(" (agent %s)", id)
		}
		fmt.Fprintf(&prompt, " (id '%s', goal '%s')", id, goal)
	}
	prompt.WriteString(" After fork_subagents returns, reply with one short summary line only.")

	started := time.Now()
	if _, err := proc.rpc(ctx, "Submit", prompt.String()); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	t.Logf("[fork-live] n=%d target=%s started", n, filepath.Base(target))

	report := forkLiveReport{Scenario: "fork-subagents-live", Notes: []string{"probe", filepath.Base(target)}}
	lastSummary := ""
	var peakRunning int
	for {
		select {
		case err := <-proc.exit:
			t.Fatalf("进程提前退出: %v\nstderr:\n%s", err, proc.stderr.String())
		default:
		}
		snap, err := proc.snapshot(ctx)
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		rows := forkLiveRows(snap.Runtime.WorkTable)
		tree := forkLiveTree(snap.Runtime.SubAgentTree)
		running := 0
		for _, row := range rows {
			if strings.HasPrefix(row.Status, "running") || row.Status == "doing" {
				running++
			}
		}
		if running > peakRunning {
			peakRunning = running
		}
		report.PeakRunning = peakRunning
		if time.Since(started) >= idleBudget {
			report.Stall = true
			report.Final = snap
			report.WallMS = time.Since(started).Milliseconds()
			report.ChatErr = snap.Chat.Error
			report.StderrWarnings = forkLiveStderrWarnings(proc.stderr.String())
			forkLiveDumpReport(t, repoRoot, report)
			if pprofAddr != "" {
				forkLiveDumpGoroutines(t, pprofAddr, repoRoot, "stall")
			}
			t.Fatalf("卡死复现：%s 内子代理未收敛 running=%v chat_err=%q\n最后状态 rows=%+v tree=%+v",
				idleBudget, snap.Chat.Running, snap.Chat.Error, rows, tree)
		}
		summary := forkLiveCompact(rows, tree)
		if summary != lastSummary {
			t.Logf("[state] running=%v %s", snap.Chat.Running, summary)
			lastSummary = summary
		}
		if time.Since(started).Milliseconds()%5000 < 1100 {
			report.Samples = append(report.Samples, forkLiveSample{
				At:      time.Now().Format(time.RFC3339Nano),
				Running: snap.Chat.Running,
				Rows:    rows,
				Tree:    tree,
			})
		}
		if snap.Chat.Error != "" {
			report.ChatErr = snap.Chat.Error
			report.Final = snap
			report.WallMS = time.Since(started).Milliseconds()
			forkLiveDumpReport(t, repoRoot, report)
			t.Fatalf("chat error: %s", snap.Chat.Error)
		}
		if !snap.Chat.Running {
			time.Sleep(2 * time.Second)
			snap, _ = proc.snapshot(ctx)
			rows = forkLiveRows(snap.Runtime.WorkTable)
			tree = forkLiveTree(snap.Runtime.SubAgentTree)
			report.Passed = true
			report.Final = snap
			report.WallMS = time.Since(started).Milliseconds()
			report.RowEndCounts = forkLiveStatusCounts(rows)
			report.TreeEndCounts = forkLiveTreeCounts(tree)
			report.ForkTool = forkLiveForkTool(snap.Conversation)
			report.StderrWarnings = forkLiveStderrWarnings(proc.stderr.String())
			forkLiveDumpReport(t, repoRoot, report)
			forkLiveAssertOutcome(t, rows, tree, n)
			t.Logf("=== fork-live 冒烟通过：%s 内全部子代理收敛 ===", time.Since(started).Round(time.Millisecond))
			return
		}
		time.Sleep(time.Second)
	}
}

// ── 进程 / RPC 驱动 ─────────────────────────────────────────

func forkLiveRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root not found")
		}
		dir = parent
	}
}

func forkLiveFreePort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	_ = listener.Close()
	return port
}

func forkLiveEnvPath(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func forkLiveEnvInt(name string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func forkLiveSpawn(t *testing.T, target, repoRoot, storeDir, port, pprofAddr string) *forkLiveProc {
	t.Helper()
	cmd := exec.Command(target,
		"-frontend", "headless",
		"-permission", "full_access",
		"-store", filepath.Join(storeDir, "sessions"),
	)
	cmd.Dir = repoRoot
	env := append(os.Environ(), "SEELEX_HEADLESS_PORT="+port)
	if pprofAddr != "" {
		env = append(env, "SEELEX_PPROF_ADDR="+pprofAddr)
	}
	cmd.Env = env
	proc := &forkLiveProc{
		base:   "http://127.0.0.1:" + port,
		client: &http.Client{Timeout: 90 * time.Second},
		cmd:    cmd,
		exit:   make(chan error, 1),
	}
	cmd.Stderr = &proc.stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start target %s: %v", target, err)
	}
	go func() { proc.exit <- cmd.Wait() }()
	return proc
}

func (p *forkLiveProc) stop() {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return
	}
	select {
	case <-p.exit:
		return
	default:
	}
	_ = p.cmd.Process.Kill()
	select {
	case <-p.exit:
	case <-time.After(8 * time.Second):
	}
}

func (p *forkLiveProc) waitHealthy(ctx context.Context, t *testing.T, deadline time.Time) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		select {
		case err := <-p.exit:
			t.Fatalf("进程提前退出: %v\nstderr:\n%s", err, p.stderr.String())
		default:
		}
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, p.base+"/healthz", nil)
		response, err := client.Do(request)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("启动超时，stderr:\n%s", p.stderr.String())
}

func (p *forkLiveProc) rpc(ctx context.Context, method string, args ...any) (json.RawMessage, error) {
	raw := make([]json.RawMessage, 0, len(args))
	for _, arg := range args {
		encoded, _ := json.Marshal(arg)
		raw = append(raw, encoded)
	}
	body, _ := json.Marshal(map[string]any{"method": method, "args": raw})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base+"/rpc", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := p.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	var out forkLiveRPCResponse
	if err := json.NewDecoder(response.Body).Decode(&out); err != nil {
		return nil, err
	}
	if !out.OK {
		return nil, fmt.Errorf("%s: %s", method, out.Error)
	}
	return out.Result, nil
}

func (p *forkLiveProc) snapshot(ctx context.Context) (model.Snapshot, error) {
	raw, err := p.rpc(ctx, "Snapshot")
	if err != nil {
		return model.Snapshot{}, err
	}
	var snap model.Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return model.Snapshot{}, err
	}
	return snap, nil
}

// ── 状态推导与断言 ─────────────────────────────────────────

func forkLiveRows(items []model.WorkItem) []forkLiveRowState {
	var out []forkLiveRowState
	for _, item := range items {
		if item.Kind != "subagent" {
			continue
		}
		out = append(out, forkLiveRowState{
			ID:           item.ID,
			Status:       item.Status,
			Assignee:     item.Assignee,
			Participants: append([]string(nil), item.Participants...),
			Trace:        len(item.Trace),
		})
	}
	return out
}

func forkLiveTree(nodes []dto.SubAgentTreeNode) []forkLiveTreeNode {
	var out []forkLiveTreeNode
	var walk func([]dto.SubAgentTreeNode)
	walk = func(list []dto.SubAgentTreeNode) {
		for _, node := range list {
			out = append(out, forkLiveTreeNode{ID: node.ID, Status: string(node.Status), SessionID: node.SessionID})
			walk(node.Children)
		}
	}
	walk(nodes)
	return out
}

func forkLiveCompact(rows []forkLiveRowState, tree []forkLiveTreeNode) string {
	var parts []string
	for _, row := range rows {
		parts = append(parts, fmt.Sprintf("%s=%s/%s/t%d", row.ID, row.Status, forkLiveShortAssignee(row.Assignee), row.Trace))
	}
	for _, node := range tree {
		parts = append(parts, fmt.Sprintf("T:%s=%s", node.ID, node.Status))
	}
	return strings.Join(parts, " ")
}

func forkLiveShortAssignee(assignee string) string {
	if assignee == "" {
		return "-"
	}
	index := strings.LastIndex(assignee, ":")
	if index < 0 || index+9 > len(assignee) {
		return assignee
	}
	return assignee[:index] + ":" + assignee[index+1:index+9] + "…"
}

func forkLiveStatusCounts(rows []forkLiveRowState) map[string]int {
	out := make(map[string]int)
	for _, row := range rows {
		out[row.Status]++
	}
	return out
}

func forkLiveTreeCounts(nodes []forkLiveTreeNode) map[string]int {
	out := make(map[string]int)
	for _, node := range nodes {
		out[node.Status]++
	}
	return out
}

func forkLiveForkTool(messages []model.Message) *model.ToolCall {
	for _, message := range messages {
		if message.Tool != nil && message.Tool.Name == "fork_subagents" {
			call := *message.Tool
			return &call
		}
	}
	return nil
}

func forkLiveStderrWarnings(stderr string) []string {
	var out []string
	for _, line := range strings.Split(stderr, "\n") {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "429") ||
			strings.Contains(lower, "rate limit") ||
			strings.Contains(lower, "panic") ||
			strings.Contains(lower, "deadlock") ||
			strings.Contains(lower, "timeout") ||
			strings.Contains(lower, "error") {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				if len(trimmed) > 400 {
					trimmed = trimmed[:400] + "…"
				}
				out = append(out, trimmed)
			}
		}
	}
	return out
}

func forkLiveAssertOutcome(t *testing.T, rows []forkLiveRowState, tree []forkLiveTreeNode, n int) {
	t.Helper()
	if len(rows) < n {
		t.Fatalf("worktable subagent 行数 = %d, want >= %d（rows=%+v）", len(rows), n, rows)
	}
	for _, row := range rows {
		if !strings.HasPrefix(row.Status, "completed") && !strings.HasPrefix(row.Status, "failed") && row.Status != "done" {
			t.Fatalf("子代理行未收敛: %s status=%s（rows=%+v）", row.ID, row.Status, rows)
		}
		if !strings.HasPrefix(row.Assignee, "subagent:") {
			t.Fatalf("子代理行认领未生效: %s assignee=%q（期望 subagent:<session>）", row.ID, row.Assignee)
		}
	}
	for _, node := range tree {
		if node.Status != "done" && node.Status != "failed" {
			t.Fatalf("子代理树节点未收敛: %s status=%s（tree=%+v）", node.ID, node.Status, tree)
		}
	}
}

func forkLiveDumpReport(t *testing.T, repoRoot string, report forkLiveReport) {
	t.Helper()
	path := filepath.Join(repoRoot, "tmp", "headless-smoke", "reports",
		fmt.Sprintf("fork-live-gui-%d.json", os.Getpid()))
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	payload, _ := json.MarshalIndent(report, "", "  ")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Logf("[report] 落盘失败: %v", err)
		return
	}
	t.Logf("[report] 落盘: %s", path)
}

func forkLiveDumpGoroutines(t *testing.T, pprofAddr, repoRoot, suffix string) {
	t.Helper()
	client := &http.Client{Timeout: 20 * time.Second}
	response, err := client.Get("http://" + pprofAddr + "/debug/pprof/goroutine?debug=2")
	if err != nil {
		t.Logf("[pprof] 抓取失败: %v", err)
		return
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	path := filepath.Join(repoRoot, "tmp", "headless-smoke", "reports",
		fmt.Sprintf("fork-live-goroutine-%s-%d.txt", suffix, os.Getpid()))
	_ = os.WriteFile(path, body, 0o644)
	t.Logf("[pprof] goroutine dump: %s (%d bytes)", path, len(body))
}
