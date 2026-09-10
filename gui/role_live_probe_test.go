package gui

// TestRealAPIRoleSessionLiveProbe 是 R2/R4 的真实 headless 冒烟：启动与 GUI
// 同装配的 headless 进程，先经真实 API 物化一个主会话，再经 headless 暴露的
// role.* / schedule.* 接口驱动 TL 会话、role draft、sequencer sync、floor 与
// 角色 wire，并抓取 goroutine/mutex pprof 现场。
//
// 运行（真实 API，默认跳过；目标二进制需包含本阶段的 role.* 接口）：
//   go build -tags pprof -o tmp/headless-smoke/seelex-pprof.exe .
//   $env:SMOKE_ROLE_LIVE='1'
//   $env:SMOKE_ROLE_LIVE_PPROF='1'
//   go test ./gui -run TestRealAPIRoleSessionLiveProbe -v -count=1 -timeout 20m
//
// 可选 env：
//   SMOKE_ROLE_LIVE_TARGET     目标二进制（默认 tmp/bin/seelex-headless.exe）
//   SMOKE_ROLE_LIVE_PROMPT     真实 API 物化主会话的提示词
//   SMOKE_ROLE_LIVE_KEEP_STORE 置 1 保留临时数据根
//   SMOKE_PPROF_ADDR           pprof 监听地址（不设则自动分配）

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

func TestRealAPIRoleSessionLiveProbe(t *testing.T) {
	if os.Getenv("SMOKE_ROLE_LIVE") == "" {
		t.Skip("set SMOKE_ROLE_LIVE=1 to run the real-API role session live probe")
	}
	repoRoot := forkLiveRepoRoot(t)
	usePprof := os.Getenv("SMOKE_ROLE_LIVE_PPROF") == "1"
	defaultTarget := filepath.Join(repoRoot, "tmp", "bin", "seelex-headless.exe")
	if usePprof {
		defaultTarget = filepath.Join(repoRoot, "tmp", "headless-smoke", "seelex-pprof.exe")
	}
	target := forkLiveEnvPath("SMOKE_ROLE_LIVE_TARGET", defaultTarget)
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("smoke target missing: %s（先构建含 role.* 接口的 headless 二进制）", target)
	}
	storeDir, err := os.MkdirTemp("", "seelex-role-live-")
	if err != nil {
		t.Fatalf("mk temp store: %v", err)
	}
	defer func() {
		if os.Getenv("SMOKE_ROLE_LIVE_KEEP_STORE") == "" {
			_ = os.RemoveAll(storeDir)
		} else {
			t.Logf("[store] 保留现场: %s", storeDir)
		}
	}()

	port := forkLiveFreePort(t)
	pprofAddr := ""
	if usePprof {
		if configured := strings.TrimSpace(os.Getenv("SMOKE_PPROF_ADDR")); configured != "" {
			pprofAddr = configured
		} else {
			pprofAddr = "127.0.0.1:" + forkLiveFreePort(t)
		}
	}
	proc := forkLiveSpawn(t, target, repoRoot, storeDir, port, pprofAddr)
	defer proc.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	proc.waitHealthy(ctx, t, time.Now().Add(90*time.Second))

	// 1) 真实 API 物化主会话（role session 必须有已发布 main session 根）。
	mainSessionID := roleLiveSessionID(t, ctx, proc)
	prompt := strings.TrimSpace(os.Getenv("SMOKE_ROLE_LIVE_PROMPT"))
	if prompt == "" {
		prompt = "这是 R2/R4 群聊链路冒烟。请只回复 OK，不要调用任何工具。"
	}
	if _, err := proc.rpc(ctx, "Submit", prompt); err != nil {
		t.Fatalf("real API Submit: %v", err)
	}
	if _, err := proc.rpc(ctx, "WaitIdle", 300); err != nil {
		t.Fatalf("WaitIdle: %v", err)
	}
	mainSessionID = roleLiveSessionID(t, ctx, proc)
	if mainSessionID == "" {
		t.Fatal("main session did not materialize")
	}

	// 2) 建 TL 会话并固定 join 点/顺序策略。
	roleSessionID := "tl-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if _, err := proc.rpc(ctx, "role.create", map[string]any{
		"main_session_id": mainSessionID, "role_name": "tl",
		"role_session_id": roleSessionID, "join_seq_id": 0,
	}); err != nil {
		t.Fatalf("role.create: %v", err)
	}
	if _, err := proc.rpc(ctx, "role.set_order", map[string]any{
		"session_id": mainSessionID, "order_policy": "goal_loop",
		"order_roles": []string{"user", "main", "tl"},
	}); err != nil {
		t.Fatalf("role.set_order: %v", err)
	}
	before, err := roleLiveSnapshot(ctx, proc, mainSessionID, "tl", roleSessionID)
	if err != nil {
		t.Fatalf("role.snapshot(before): %v", err)
	}
	if _, err := proc.rpc(ctx, "role.set_lifecycle", map[string]any{
		"main_session_id": mainSessionID, "role_name": "tl",
		"role_session_id": roleSessionID, "join_seq_id": before.MainHeadSeq,
	}); err != nil {
		t.Fatalf("role.set_lifecycle: %v", err)
	}

	// 3) draft 乱序写入 → sequencer 按 unit_seq 排序 sync → 同步即删 + floor。
	rows := []sessionstore.RoleDraftRow{
		{
			RoundID: 1, RoleName: "tl", RoleSessionID: roleSessionID, UnitSeq: 2,
			MessageID: "tl-msg-2", Event: sessionstore.Event{
				Role: "assistant", Kind: sessionstore.EventKindLLM, Content: "TL-DRAFT-2",
			},
		},
		{
			RoundID: 1, RoleName: "tl", RoleSessionID: roleSessionID, UnitSeq: 1,
			MessageID: "tl-msg-1", Event: sessionstore.Event{
				Role: "assistant", Kind: sessionstore.EventKindLLM, Content: "TL-DRAFT-1",
			},
		},
	}
	if _, err := proc.rpc(ctx, "role.append_draft", map[string]any{
		"main_session_id": mainSessionID, "role_name": "tl",
		"role_session_id": roleSessionID, "rows": rows,
	}); err != nil {
		t.Fatalf("role.append_draft: %v", err)
	}
	if result, err := roleLiveDraftRows(ctx, proc, mainSessionID, "tl", roleSessionID); err != nil {
		t.Fatalf("role.read_draft: %v", err)
	} else if len(result) != 2 {
		t.Fatalf("draft rows before sync = %d, want 2", len(result))
	}
	if _, err := proc.rpc(ctx, "role.sync_draft", map[string]any{
		"main_session_id": mainSessionID, "role_name": "tl",
		"role_session_id": roleSessionID, "order": []string{"user", "main", "tl"},
	}); err != nil {
		t.Fatalf("role.sync_draft: %v", err)
	}
	if result, err := roleLiveDraftRows(ctx, proc, mainSessionID, "tl", roleSessionID); err != nil {
		t.Fatalf("role.read_draft(after sync): %v", err)
	} else if len(result) != 0 {
		t.Fatalf("draft rows after sync = %d, want 0（同步即删）", len(result))
	}
	after, err := roleLiveSnapshot(ctx, proc, mainSessionID, "tl", roleSessionID)
	if err != nil {
		t.Fatalf("role.snapshot(after): %v", err)
	}
	roleLiveAssertFloorAndRoleRows(t, after)
	if len(after.DesignWarnings) != 0 {
		// 应用层真实 chat 生产者尚未给 user/main 行盖 RoleName；这是本轮要
		// 观察并登记的设计缺口，不把冒烟本身判失败。
		t.Logf("[design] role snapshot warnings: %v (unassigned_rows=%d)",
			after.DesignWarnings, after.UnassignedRoleRows)
		for _, row := range after.MainRows {
			if row.RoleName == "" {
				t.Logf("[design] unassigned message row: seq=%d role=%s kind=%s message_id=%s task_id=%s",
					row.Seq, row.Role, row.Kind, row.MessageID, row.TaskID)
			}
		}
	}

	// 4) 角色 wire 只含自身 pending draft，主文档不被未同步内容污染。
	wire, err := roleLiveWire(ctx, proc, mainSessionID, "tl", roleSessionID)
	if err != nil {
		t.Fatalf("role.wire: %v", err)
	}
	if wire.PendingRows != 0 || len(wire.DesignWarnings) != 0 {
		t.Fatalf("role wire after sync = pending=%d warnings=%v", wire.PendingRows, wire.DesignWarnings)
	}
	pending := rows[:1]
	pending[0].UnitSeq = 3
	pending[0].MessageID = "tl-msg-3"
	pending[0].Event.Content = "TL-PENDING"
	if _, err := proc.rpc(ctx, "role.append_draft", map[string]any{
		"main_session_id": mainSessionID, "role_name": "tl",
		"role_session_id": roleSessionID, "rows": pending,
	}); err != nil {
		t.Fatalf("role.append_draft(pending): %v", err)
	}
	beforePending, err := roleLiveSnapshot(ctx, proc, mainSessionID, "tl", roleSessionID)
	if err != nil {
		t.Fatal(err)
	}
	wirePending, err := roleLiveWire(ctx, proc, mainSessionID, "tl", roleSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if wirePending.PendingRows != 1 || wirePending.Messages[len(wirePending.Messages)-1].Content != "TL-PENDING" {
		t.Fatalf("pending role wire = %+v", wirePending)
	}
	afterPending, err := roleLiveSnapshot(ctx, proc, mainSessionID, "tl", roleSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterPending.MainRows) != len(beforePending.MainRows) {
		t.Fatalf("pending draft entered main document: before=%d after=%d",
			len(beforePending.MainRows), len(afterPending.MainRows))
	}

	// 5) 角色备份与定时插话 EVENT 走同一 headless 控制面。
	if _, err := proc.rpc(ctx, "role.append_backup", map[string]any{
		"main_session_id": mainSessionID, "role_name": "tl", "role_session_id": roleSessionID,
		"rows": []sessionstore.Event{{Role: "assistant", Kind: sessionstore.EventKindLLM, Content: "TL-BACKUP"}},
	}); err != nil {
		t.Fatalf("role.append_backup: %v", err)
	}
	backupRaw, err := proc.rpc(ctx, "role.read_backup", map[string]any{
		"main_session_id": mainSessionID, "role_name": "tl", "role_session_id": roleSessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	var backup []sessionstore.Event
	if err := json.Unmarshal(backupRaw, &backup); err != nil || len(backup) != 1 || backup[0].Content != "TL-BACKUP" {
		t.Fatalf("role backup = %s err=%v", backupRaw, err)
	}
	for _, method := range []string{"schedule.register", "schedule.cancel", "schedule.fire"} {
		if _, err := proc.rpc(ctx, method, map[string]any{
			"session_id": mainSessionID,
			"payload": map[string]any{
				"schedule_id": "sched-role-live", "role_name": "tl", "role_session_id": roleSessionID,
				"interval": "10m",
			},
		}); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
	}

	// 6) pprof 现场 + 进程仍可响应，确认无死锁/卡死。
	if usePprof {
		forkLiveDumpGoroutines(t, pprofAddr, repoRoot, "role-live")
		roleLiveDumpMutex(t, pprofAddr, repoRoot)
	}
	response, err := proc.client.Get(proc.base + "/healthz")
	if err != nil {
		t.Fatalf("healthz after role chain: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("healthz status after role chain = %d", response.StatusCode)
	}
}

func roleLiveSessionID(t *testing.T, ctx context.Context, proc *forkLiveProc) string {
	t.Helper()
	raw, err := proc.rpc(ctx, "Snapshot")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	var snapshot model.Snapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatalf("decode Snapshot: %v", err)
	}
	return snapshot.Session.ID
}

func roleLiveSnapshot(ctx context.Context, proc *forkLiveProc, mainSessionID, roleName, roleSessionID string) (sessionstore.RoleSnapshot, error) {
	raw, err := proc.rpc(ctx, "role.snapshot", map[string]any{
		"main_session_id": mainSessionID, "role_name": roleName, "role_session_id": roleSessionID,
	})
	if err != nil {
		return sessionstore.RoleSnapshot{}, err
	}
	var snapshot sessionstore.RoleSnapshot
	return snapshot, json.Unmarshal(raw, &snapshot)
}

func roleLiveDraftRows(ctx context.Context, proc *forkLiveProc, mainSessionID, roleName, roleSessionID string) ([]sessionstore.RoleDraftRow, error) {
	raw, err := proc.rpc(ctx, "role.read_draft", map[string]any{
		"main_session_id": mainSessionID, "role_name": roleName, "role_session_id": roleSessionID,
	})
	if err != nil {
		return nil, err
	}
	var rows []sessionstore.RoleDraftRow
	return rows, json.Unmarshal(raw, &rows)
}

func roleLiveWire(ctx context.Context, proc *forkLiveProc, mainSessionID, roleName, roleSessionID string) (sessionstore.RoleWireSnapshot, error) {
	raw, err := proc.rpc(ctx, "role.wire", map[string]any{
		"main_session_id": mainSessionID, "role_name": roleName,
		"role_session_id": roleSessionID, "budget": 200000, "k": 3,
	})
	if err != nil {
		return sessionstore.RoleWireSnapshot{}, err
	}
	var wire sessionstore.RoleWireSnapshot
	return wire, json.Unmarshal(raw, &wire)
}

func roleLiveAssertFloorAndRoleRows(t *testing.T, snapshot sessionstore.RoleSnapshot) {
	t.Helper()
	if snapshot.Floor == nil || snapshot.Floor.RoleName != "tl" || snapshot.Floor.RoleSessionID == "" {
		t.Fatalf("floor = %+v, want tl role session", snapshot.Floor)
	}
	if len(snapshot.MainRows) < 2 {
		t.Fatalf("main rows = %d, want synced TL rows", len(snapshot.MainRows))
	}
	tail := snapshot.MainRows[len(snapshot.MainRows)-2:]
	if tail[0].Content != "TL-DRAFT-1" || tail[1].Content != "TL-DRAFT-2" {
		t.Fatalf("sync order = %q/%q, want TL-DRAFT-1/TL-DRAFT-2", tail[0].Content, tail[1].Content)
	}
	for _, row := range tail {
		if row.RoleName != "tl" || row.RoleSessionID == "" || row.RoundID != 1 {
			t.Fatalf("synced row lost role ownership: %+v", row)
		}
	}
}

func roleLiveDumpMutex(t *testing.T, pprofAddr, repoRoot string) {
	t.Helper()
	roleLiveDumpProfile(t, pprofAddr, repoRoot, "mutex")
	roleLiveDumpProfile(t, pprofAddr, repoRoot, "block")
}

func roleLiveDumpProfile(t *testing.T, pprofAddr, repoRoot, profile string) {
	t.Helper()
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Get("http://" + pprofAddr + "/debug/pprof/" + profile + "?debug=1")
	if err != nil {
		t.Logf("[pprof] %s 抓取失败: %v", profile, err)
		return
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Logf("[pprof] %s 读取失败: %v", profile, err)
		return
	}
	dir := filepath.Join(repoRoot, "tmp", "headless-smoke", "reports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Logf("[pprof] %s 目录创建失败: %v", profile, err)
		return
	}
	path := filepath.Join(dir, fmt.Sprintf("role-%s-%d.txt", profile, time.Now().UnixNano()))
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Logf("[pprof] %s 落盘失败: %v", profile, err)
		return
	}
	t.Logf("[pprof] %s profile: %s (%d bytes)", profile, path, len(body))
}
