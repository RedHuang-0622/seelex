package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

// ── 测试脚手架 ───────────────────────────────────────────────────────────

type asyncSessionKey struct{}

func asyncTestCtx(root, session string) context.Context {
	ctx := model.WithNodeScope(context.Background(), model.NodeScope{
		NodeID: "x", Role: model.RoleSubAgent, BranchID: "x", WorkspaceID: root,
	})
	return context.WithValue(ctx, asyncSessionKey{}, session)
}

func asyncSessionFromCtx(ctx context.Context) string {
	if session, ok := ctx.Value(asyncSessionKey{}).(string); ok {
		return session
	}
	return "sess-default"
}

func asyncTestRouter(t *testing.T, enabled bool) *Router {
	t.Helper()
	router := NewRouter(Deps{
		ToolCallTimeout:        time.Minute,
		DisableDockerAutoStart: true,
		SessionKey:             asyncSessionFromCtx,
		AsyncExecEnabled:       enabled,
		// 批次解析口固定回答一个 ID：后台行要归到派发它的那次请求（投影用例断言这一点）。
		AsyncBatchID: func(string) string { return "req-test" },
	})
	// 用例自己收现场：否则一轮 go test 就在临时目录里留下几个 seelex-async-*。
	t.Cleanup(router.CloseAsync)
	return router
}

// newAsyncRegistryForTest 与生产构造函数同形，只多做一件事：用例结束时收走它的
// 临时输出目录。登记表唯一的真实资源就是这个目录，不删就是每跑一轮攒一批。
func newAsyncRegistryForTest(t *testing.T) *asyncRegistry {
	t.Helper()
	reg := newAsyncRegistry()
	t.Cleanup(reg.close)
	return reg
}

// dispatchForTest 走 bash_bg 工具面派发一条后台命令（JobTool.Add 的进程作业面）。
// description 是必填项（工作表格行标题），测试统一给一句固定标签。
func dispatchForTest(t *testing.T, router *Router, ctx context.Context, command string) asyncPayload {
	t.Helper()
	args, err := json.Marshal(map[string]interface{}{
		"command": command, "description": "test dispatch",
	})
	if err != nil {
		t.Fatal(err)
	}
	output, err := router.scopedBashBg(ctx, string(args))
	if err != nil {
		t.Fatalf("dispatch %q: %v", command, err)
	}
	return decodeAsyncPayload(t, output)
}

func pollForTest(t *testing.T, router *Router, ctx context.Context, handle string, waitMS int) asyncPayload {
	t.Helper()
	args, err := json.Marshal(map[string]interface{}{"op": "fetch", "handle": handle, "wait_ms": waitMS})
	if err != nil {
		t.Fatal(err)
	}
	output, err := router.scopedJobManage(ctx, string(args))
	if err != nil {
		t.Fatalf("poll %s: %v", handle, err)
	}
	return decodeAsyncPayload(t, output)
}

// manageForTest 走 job_manage 的任意 op（只读那一档的用例用它做旁路观测）。
func manageForTest(t *testing.T, router *Router, ctx context.Context, op, handle string) (asyncPayload, error) {
	t.Helper()
	args, err := json.Marshal(map[string]interface{}{"op": op, "handle": handle, "wait_ms": -1})
	if err != nil {
		t.Fatal(err)
	}
	output, err := router.scopedJobManage(ctx, string(args))
	if err != nil {
		return asyncPayload{}, err
	}
	return decodeAsyncPayload(t, output), nil
}

func decodeAsyncPayload(t *testing.T, output string) asyncPayload {
	t.Helper()
	var payload asyncPayload
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("异步载荷不是合法 JSON: %v (%q)", err, output)
	}
	return payload
}

// waitAsyncTerminalForTest 派发了真实后台命令的用例在结束前要等执行体收尾：终态是
// awaitAsync 关掉文件之后才合成的，没等到就退出，句柄还开着，收尾删不掉目录，
// 而进程一死就再没人来删（这正是临时目录里那 44 个的来源）。
func waitAsyncTerminalForTest(t *testing.T, router *Router, handle string) {
	t.Helper()
	run, ok := router.async.snapshot(handle)
	if !ok {
		t.Fatalf("句柄 %s 不在登记表里", handle)
	}
	select {
	case <-run.done:
	case <-time.After(time.Minute):
		t.Fatalf("句柄 %s 未在预算内收尾", handle)
	}
}

// ── 端到端：派发回执 + 增量取回 ──────────────────────────────────────────

// TestAsyncDispatchAckCarriesNoCommandOutput 验证受理回执是受理而不是结果：
// 回执里出现命令输出，就说明"工具调用配平"被写成了"命令完成"，那是本切片
// 存在的全部理由（offload 的是等待，不是上下文配对）。
func TestAsyncDispatchAckCarriesNoCommandOutput(t *testing.T) {
	router := asyncTestRouter(t, true)
	ctx := asyncTestCtx(t.TempDir(), "sess-a")
	ack := dispatchForTest(t, router, ctx, "echo ack-payload-marker")

	if ack.Status != "accepted" || ack.State != asyncStateRunning {
		t.Fatalf("回执 = %+v，want status=accepted state=running", ack)
	}
	if ack.Handle == "" || ack.LogPath == "" {
		t.Fatalf("回执必须带句柄与输出落点: %+v", ack)
	}
	if ack.Output != "" {
		t.Fatalf("受理回执不得携带命令输出: %q", ack.Output)
	}
	waitAsyncTerminalForTest(t, router, ack.Handle)
}

// TestAsyncPollDeliversOnlyNewBytes 是切片的主验收：反复轮询同一句柄，把每次
// 增量拼起来必须**恰好等于**命令输出——一次不多（不重播整份日志）一次不少
// （最后一截不丢）。
func TestAsyncPollDeliversOnlyNewBytes(t *testing.T) {
	router := asyncTestRouter(t, true)
	ctx := asyncTestCtx(t.TempDir(), "sess-a")
	ack := dispatchForTest(t, router, ctx, "echo incremental-marker")

	var delivered strings.Builder
	deadline := time.Now().Add(60 * time.Second)
	payload := ack
	for payload.State == asyncStateRunning {
		if time.Now().After(deadline) {
			t.Fatalf("后台命令未在 60s 内结束，最后一次取回 = %+v", payload)
		}
		payload = pollForTest(t, router, ctx, ack.Handle, 200)
		delivered.WriteString(payload.Output)
	}

	if payload.ExitCode != 0 {
		t.Fatalf("exit_code = %d，want 0（取回载荷 = %+v）", payload.ExitCode, payload)
	}
	if payload.Status != "finished" {
		t.Fatalf("终态 status = %q，want finished", payload.Status)
	}
	if got := strings.Count(delivered.String(), "incremental-marker"); got != 1 {
		t.Fatalf("增量交付必须恰好一次，实际 %d 次（累计正文 %q）", got, delivered.String())
	}

	// 终态 + 已交付 ⇒ 销项（设计文档 §A.3 的行生命周期）：行从投影里消失，
	// history 里上面那次取回成为唯一事实。再取就是"已销项"，不是重播。
	if _, ok := router.async.snapshot(ack.Handle); ok {
		t.Fatal("终态取回之后句柄仍在登记表里：行不会从投影里消失")
	}
	if _, err := manageForTest(t, router, ctx, "fetch", ack.Handle); err == nil {
		t.Fatal("已销项句柄再取回必须报错（不能重播整份日志）")
	}
}

// TestAsyncPollRejectsForeignSession 验证句柄只在本会话内有效：跨会话取回会把
// 别的会话的命令输出带进本会话上下文。
func TestAsyncPollRejectsForeignSession(t *testing.T) {
	router := asyncTestRouter(t, true)
	root := t.TempDir()
	owner := asyncTestCtx(root, "sess-owner")
	intruder := asyncTestCtx(root, "sess-intruder")
	ack := dispatchForTest(t, router, owner, "echo owner-only-marker")

	args, err := json.Marshal(map[string]interface{}{"op": "fetch", "handle": ack.Handle, "wait_ms": -1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := router.scopedJobManage(intruder, string(args)); err == nil {
		t.Fatal("跨会话取回必须被拒绝")
	}
	waitAsyncTerminalForTest(t, router, ack.Handle)
	if _, err := router.scopedJobManage(owner, string(args)); err != nil {
		t.Fatalf("本会话取回不得报错: %v", err)
	}
}

// TestAsyncPollUnknownHandleFailsLoudly 验证未知句柄是错误而不是空成功：
// 谎报成功会让模型以为命令没有输出。
func TestAsyncPollUnknownHandleFailsLoudly(t *testing.T) {
	router := asyncTestRouter(t, true)
	ctx := asyncTestCtx(t.TempDir(), "sess-a")
	if _, err := router.scopedJobManage(ctx, `{"op":"fetch","handle":"a404"}`); err == nil {
		t.Fatal("未知句柄必须报错")
	}
	if _, err := router.scopedJobManage(ctx, `{"op":"fetch"}`); err == nil {
		t.Fatal("缺 handle 必须报错")
	}
}

// ── 登记表：去重、上限、驱逐、增量游标 ──────────────────────────────────

func TestAsyncRegistryDedupsOnlyWhileRunning(t *testing.T) {
	registry := newAsyncRegistryForTest(t)
	first, started, err := registry.begin("sess-a", "echo same", "", "")
	if err != nil || !started {
		t.Fatalf("首次派发: started=%v err=%v", started, err)
	}
	second, startedAgain, err := registry.begin("sess-a", "echo same", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if startedAgain || second.handle != first.handle {
		t.Fatalf("同会话同命令在途期间必须复用一个执行体: started=%v handle=%s", startedAgain, second.handle)
	}

	// 跑完后允许重发：一次失败（或一次成功）不得永久封死这条命令。
	registry.finish(first.handle, 0)
	third, startedThird, err := registry.begin("sess-a", "echo same", "", "")
	if err != nil || !startedThird {
		t.Fatalf("终态后重发: started=%v err=%v", startedThird, err)
	}
	if third.handle == first.handle {
		t.Fatal("重发必须拿到新句柄（句柄是每次派发的身份）")
	}

	// 去重键含会话：另一个会话的同名命令不受影响。
	other, otherStarted, err := registry.begin("sess-b", "echo same", "", "")
	if err != nil || !otherStarted {
		t.Fatalf("跨会话不得共享去重键: started=%v err=%v", otherStarted, err)
	}
	if other.key == first.key {
		t.Fatal("去重键必须含会话标识")
	}
}

// TestAsyncRegistryBeginHandsOutACopy 钉住派发侧只持副本：执行体收尾在锁内改写表内
// 那条记录的 state/exit，派发侧若拿到指针就是在锁外读这两个字段（-race 可见）。
// 语义上也必须是副本——受理回执是已定稿的一行账，命令恰好在同一瞬间结束都不回头改它。
func TestAsyncRegistryBeginHandsOutACopy(t *testing.T) {
	registry := newAsyncRegistryForTest(t)
	run, started, err := registry.begin("sess-a", "echo copy", "", "")
	if err != nil || !started {
		t.Fatalf("派发: started=%v err=%v", started, err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(run.logPath)) })

	registry.finish(run.handle, 0)
	if run.state != asyncStateRunning {
		t.Fatalf("派发侧拿到了表内指针: state=%s", run.state)
	}
	accepted, err := renderAccepted(run, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(accepted), `"state":"running"`) {
		t.Fatalf("回执必须渲染派发时刻的状态: %s", string(accepted))
	}
	// 取回侧读的是表内最新态，不受派发副本影响。
	fresh, ok := registry.snapshot(run.handle)
	if !ok || fresh.state != asyncStateDone {
		t.Fatalf("终态未登记: ok=%v state=%s", ok, fresh.state)
	}
}

func TestAsyncRegistryCapsRunningDispatch(t *testing.T) {
	registry := newAsyncRegistryForTest(t)
	for i := 0; i < asyncMaxRunning; i++ {
		if _, started, err := registry.begin("sess-a", fmt.Sprintf("echo %d", i), "", ""); err != nil || !started {
			t.Fatalf("第 %d 次派发: started=%v err=%v", i, started, err)
		}
	}
	if _, _, err := registry.begin("sess-a", "echo overflow", "", ""); err == nil {
		t.Fatal("在途上限必须拒绝新派发（不许把机器跑满）")
	}
}

// TestAsyncRegistryEvictionDropsRecordAndLog 验证驱逐连带删除输出文件：
// 只封记录会让临时目录在一个长会话里无界增长。
func TestAsyncRegistryEvictionDropsRecordAndLog(t *testing.T) {
	registry := newAsyncRegistryForTest(t)
	oldest, _, err := registry.begin("sess-a", "echo oldest", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldest.logPath, []byte("oldest\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry.finish(oldest.handle, 0)

	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(oldest.logPath)) })

	for i := 0; i < asyncMaxRecords+2; i++ {
		run, started, err := registry.begin("sess-a", fmt.Sprintf("echo churn-%d", i), "", "")
		if err != nil || !started {
			t.Fatalf("churn %d: started=%v err=%v", i, started, err)
		}
		registry.finish(run.handle, 0)
	}
	if _, ok := registry.snapshot(oldest.handle); ok {
		t.Fatal("最老的已完成记录必须被驱逐")
	}
	if _, err := os.Stat(oldest.logPath); !os.IsNotExist(err) {
		t.Fatalf("被驱逐记录的输出文件必须删除: err=%v", err)
	}
}

// TestAsyncRegistryCloseRemovesOutputDir 钉住收尾把后台输出目录带走：目录是进程级
// 资源，进程死了就没人再删——不删就是每个进程在临时目录里攒一份垃圾（实测 44 份）。
func TestAsyncRegistryCloseRemovesOutputDir(t *testing.T) {
	registry := newAsyncRegistryForTest(t)
	run, _, err := registry.begin("sess-a", "echo close", "", "")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(run.logPath)
	if err := os.WriteFile(run.logPath, []byte("out\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	registry.finish(run.handle, 0) // 没有在跑的执行体了 ⇒ 当场可删
	registry.close()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("收尾必须删除输出目录: err=%v", err)
	}
	registry.close() // 幂等：没有目录可删时不得炸
}

// TestAsyncRegistryCloseSweepsLateFinish 钉住"当场删不掉 ⇒ 收尾补删"这条分支：
// close 总是先试，握着日志句柄时（Windows）删不动，目录只能留到最后一条 finish——
// 那一刻句柄已关，才是确定的可删点。非 Windows 上 close 当场就删掉了，这条仍然通过，
// 只是走不到补删分支。
func TestAsyncRegistryCloseSweepsLateFinish(t *testing.T) {
	registry := newAsyncRegistryForTest(t)
	run, _, err := registry.begin("sess-a", "echo late", "", "")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(run.logPath)
	file, err := os.Create(run.logPath)
	if err != nil {
		t.Fatal(err)
	}

	registry.close()
	if _, _, err := registry.begin("sess-a", "echo after-close", "", ""); err == nil {
		_ = file.Close()
		t.Fatal("关停后不得再登记——那会新建一个没人回收的目录")
	}

	_ = file.Close() // 执行体收尾前先关文件（awaitAsync 就是这个顺序）
	registry.finish(run.handle, 0)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("最后一条收尾必须把目录补删掉: err=%v", err)
	}
}

func TestAsyncRegistryTailDeliversOnlyNewBytes(t *testing.T) {
	registry := newAsyncRegistryForTest(t)
	run, _, err := registry.begin("sess-a", "echo tail", "", "")
	if err != nil {
		t.Fatal(err)
	}
	registry.finish(run.handle, 0)
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(run.logPath)) })
	if err := os.WriteFile(run.logPath, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}

	steps := []struct {
		budget int
		want   string
	}{
		{4, "0123"},
		{4, "4567"},
		{4, "89"},
		{4, ""},
	}
	for _, step := range steps {
		next, delta, truncated, ok := registry.advanceTail(run.handle, step.budget)
		if !ok {
			t.Fatalf("advanceTail 报告句柄丢失")
		}
		if delta != step.want {
			t.Fatalf("增量 = %q, want %q", delta, step.want)
		}
		if truncated {
			t.Fatal("未触上限时不得声明截断")
		}
		registry.markCursor(run.handle, next, truncated)
	}
}

// TestAsyncCappedWriterTruncatesWithoutShortWrite 验证超上限只丢字节：
// 向子进程报短写会让命令自己异常退出，那是把基础设施问题伪装成命令问题。
func TestAsyncCappedWriterTruncatesWithoutShortWrite(t *testing.T) {
	registry := newAsyncRegistryForTest(t)
	run, _, err := registry.begin("sess-a", "echo cap", "", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(run.logPath)) })
	file, err := os.Create(run.logPath)
	if err != nil {
		t.Fatal(err)
	}
	writer := &cappedLogWriter{registry: registry, handle: run.handle, file: file, remain: 8}

	if written, err := writer.Write([]byte("aaaa")); written != 4 || err != nil {
		t.Fatalf("首次写入 = (%d, %v)，want (4, nil)", written, err)
	}
	if written, err := writer.Write([]byte("bbbbbbbb")); written != 8 || err != nil {
		t.Fatalf("超上限写入必须报已消费全部入参: (%d, %v)", written, err)
	}
	if written, err := writer.Write([]byte("cccc")); written != 4 || err != nil {
		t.Fatalf("触顶后写入仍须报已消费: (%d, %v)", written, err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(run.logPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "aaaabbbb" {
		t.Fatalf("落盘内容 = %q, want %q", string(data), "aaaabbbb")
	}
	snapshot, ok := registry.snapshot(run.handle)
	if !ok || !snapshot.capHit {
		t.Fatal("触顶必须登记进执行表，否则取回不会声明截断")
	}
	if _, _, truncated, _ := registry.advanceTail(run.handle, 1024); !truncated {
		t.Fatal("触顶后取回必须声明 truncated=true")
	}
}

func TestClampAsyncWaitMS(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{-1, 0},
		{0, asyncDefaultWaitMS},
		{1500, 1500},
		{asyncMaxWaitMS + 1, asyncMaxWaitMS},
	}
	for _, testCase := range cases {
		if got := clampAsyncWaitMS(testCase.in); got != testCase.want {
			t.Fatalf("clampAsyncWaitMS(%d) = %d, want %d", testCase.in, got, testCase.want)
		}
	}
}

// ── 开关：schema、注册与 handler 三处必须同步 ────────────────────────────

type capturedTool struct {
	description string
	schema      map[string]interface{}
	handler     func(ctx context.Context, argsJSON string) (string, error)
}

func captureRegisteredTools(t *testing.T, enabled bool) map[string]capturedTool {
	t.Helper()
	captured := map[string]capturedTool{}
	router := NewRouter(Deps{
		RegisterTool: func(name, description string, schema map[string]interface{}, handler func(context.Context, string) (string, error)) {
			captured[name] = capturedTool{description: description, schema: schema, handler: handler}
		},
		AsyncExecEnabled: enabled,
	})
	router.Register()
	return captured
}

func bashPropertyNames(t *testing.T, tool capturedTool) map[string]bool {
	t.Helper()
	properties, ok := tool.schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("bash schema 缺少 properties: %+v", tool.schema)
	}
	names := map[string]bool{}
	for name := range properties {
		names[name] = true
	}
	return names
}

// TestJobCapabilityGatesSchemaAndRegistration 验证能力开关是"关就是关"：
// 关时作业面（bash_bg / read_batch / job_manage）一个都不注册，bash schema 里也没有
// background（那个开关已经搬去 bash_bg），旧入参 background=true 直接报错
// （不得静默按同步执行）。
func TestJobCapabilityGatesSchemaAndRegistration(t *testing.T) {
	off := captureRegisteredTools(t, false)
	for _, name := range []string{"bash_bg", "read_batch", "job_manage"} {
		if _, present := off[name]; present {
			t.Fatalf("切片关闭时不该注册 %s", name)
		}
	}
	if bashPropertyNames(t, off["bash"])["background"] {
		t.Fatal("bash schema 不得再下发 background（那一类由 bash_bg 承担）")
	}
	if _, present := off["bash_read"]; !present {
		t.Fatal("bash_read 与作业切片无关：它必须常驻（只读命令的免打断面）")
	}
	ctx := asyncTestCtx(t.TempDir(), "sess-a")
	if _, err := NewRouter(Deps{ToolCallTimeout: time.Minute}).scopedBash(ctx, `{"command":"echo x","background":true}`); err == nil {
		t.Fatal("background=true 必须直接报错（改用 bash_bg），不得静默按同步执行")
	}
	if _, err := NewRouter(Deps{}).scopedJobManage(ctx, `{"op":"fetch","handle":"a1"}`); err == nil {
		t.Fatal("切片关闭时 job_manage 必须直接报错")
	}

	on := captureRegisteredTools(t, true)
	for _, name := range []string{"bash_bg", "read_batch", "job_manage"} {
		if _, present := on[name]; !present {
			t.Fatalf("切片打开时必须注册 %s", name)
		}
	}
	if bashPropertyNames(t, on["bash"])["background"] {
		t.Fatal("bash schema 永远不下发 background")
	}
	if !strings.Contains(on["bash_bg"].description, "acceptance receipt") {
		t.Fatalf("bash_bg 描述必须点明回执不含命令输出: %q", on["bash_bg"].description)
	}
	if !strings.Contains(on["bash_read"].description, "READ-ONLY") {
		t.Fatalf("bash_read 描述必须点明只读边界: %q", on["bash_read"].description)
	}
	if !strings.Contains(on["job_manage"].description, "consumer-style") {
		t.Fatalf("job_manage 描述必须点明取回是消费式: %q", on["job_manage"].description)
	}
}

// TestBashFamilyRoutingTable 是 K-3 的判据（TC-K3-1）：三个 bash 名字按预期分组。
//
//   - bash / bash_bg → rw：写类与后台受管；sub/员工才能杀掉自己派的作业。
//   - bash_read      → ro：只读命令免打断（这正是分裂的目的）。
//   - job_manage     → rw：它含 kill/done，取回/终止必须与派发同组。
//   - read_batch     → ro：只读扇出，不碰共享状态。
func TestBashFamilyRoutingTable(t *testing.T) {
	cases := []struct {
		tool  string
		group string
	}{
		{"bash", GroupRW},
		{"bash_bg", GroupRW},
		{"bash_read", GroupRO},
		{"job_manage", GroupRW},
		{"read_batch", GroupRO},
	}
	groups := DefaultPermissionGroupList()
	for _, testCase := range cases {
		group, ok := RoutePermissionGroup(groups, testCase.tool)
		if !ok {
			t.Errorf("%s 未归入任何权限组：未分封的工具按框架默认走审批", testCase.tool)
			continue
		}
		if group.Name != testCase.group {
			t.Errorf("%s 路由组 = %q, want %q", testCase.tool, group.Name, testCase.group)
		}
	}
	// 旧名字必须彻底下线（L-2/L-4 的判据）：还留在表里就意味着还有一条注册路径活着。
	for _, retired := range []string{"async_output", "async_kill"} {
		if _, ok := RoutePermissionGroup(groups, retired); ok {
			t.Errorf("%s 已下线，不得再出现在权限表里", retired)
		}
	}
}
