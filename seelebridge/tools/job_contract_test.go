package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// 作业契约本身的判据（打点 K-1 / K-2 / K-6 与 L-1 / L-3 / L-4）。
//
// 这里只断言**可观测**的东西：状态字面量、迁移次数、游标位置、载荷字段、行数上限。
// 不断言内部函数名或字段顺序——契约的形状已经由编译期断言（var _ JobTool）钉住。

// TestJobToolContractImplementations 钉住"契约至少被两类作业实现"：
// 进程作业（bash_bg）与进程内扇出作业（read_batch）。编译期断言就是判据本身，
// 这条用例保证那两个断言真的在源码里（删掉实现会被编译期拦下）。
func TestJobToolContractImplementations(t *testing.T) {
	var (
		_ JobTool = (*bashBgTool)(nil)
		_ JobTool = (*inlineReadTool)(nil)
	)
	// 契约的四个管理动作对三类作业是**同一份实现**（jobManager）：
	// 这样 observe/fetch/kill/done 的语义不会因为工具不同而漂移。
	manager := &jobManager{router: asyncTestRouter(t, true)}
	var _ = manager
}

// K-2：终态摘要有界。摘要在打点块里按轮数重播，无界即按轮数线性烧 token。
func TestJobSummaryIsBounded(t *testing.T) {
	registry := newAsyncRegistryForTest(t)
	run, _, err := registry.begin("sess-sum", "cat huge.log", "读一个超大文件", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(run.logPath)) })
	// 800 行，末行是 200 个 4 字节字符（emoji）：末行采样按 120 字符截断，字节能到
	// 480，加上前缀就越过 512 的硬上限——这条用例钉的是"按**字节**封顶"这条口径。
	var builder strings.Builder
	for range 799 {
		builder.WriteString("ok\n")
	}
	builder.WriteString(strings.Repeat("🙂", 200))
	builder.WriteByte('\n')
	writeLogForTest(t, run.logPath, builder.String())

	registry.finish(run.handle, 0)
	snapshot, ok := registry.snapshot(run.handle)
	if !ok {
		t.Fatal("终态记录不见了")
	}
	if len(snapshot.summary) > asyncSummaryMaxBytes {
		t.Fatalf("摘要 %d 字节，超过硬上限 %d（%q）", len(snapshot.summary), asyncSummaryMaxBytes, snapshot.summary)
	}
	if !strings.Contains(snapshot.summary, "全文经 job_manage(op=fetch) 取回") {
		t.Fatalf("超限摘要必须标注全文走取回: %q", snapshot.summary)
	}
	if snapshot.lines != 800 {
		t.Fatalf("行数 = %d, want 800（摘要里的行数必须是真事实）", snapshot.lines)
	}
	if !utf8.ValidString(snapshot.summary) {
		t.Fatal("摘要按字节截断时必须落在 UTF-8 字符边界上，否则投影里是乱码")
	}
}

// K-6 / K-5-2：终态只迁移一次。
//
// 两个入口（运行体系被动收尾 / 模型侧主动 done）落到同一状态机，而重复的收尾是
// **无副作用空操作**：第二次迁移若真的发生，`close(run.done)` 会二次关闭而 panic。
func TestJobTerminalTransitionHappensOnce(t *testing.T) {
	registry := newAsyncRegistryForTest(t)
	run, _, err := registry.begin("sess-once", "echo once", "只迁一次", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(run.logPath)) })
	writeLogForTest(t, run.logPath, "ok\n")

	registry.finish(run.handle, 0)
	first, _ := registry.snapshot(run.handle)
	if first.state != asyncStateDone || !first.notified {
		t.Fatalf("首次收尾 = state=%q notified=%v, want done/true", first.state, first.notified)
	}
	if first.summary == "" {
		t.Fatal("终态必须带摘要：回填进打点块的就是它")
	}

	// 第二次（执行体又收尾 / 重复 done）必须是无副作用空操作：状态不变、不 panic。
	registry.finish(run.handle, 2)
	second, _ := registry.snapshot(run.handle)
	if second.state != asyncStateDone || second.exit != 0 {
		t.Fatalf("重复收尾改动了终态: state=%q exit=%d", second.state, second.exit)
	}
	if second.summary != first.summary {
		t.Fatalf("重复收尾重算了摘要（= 第二次回填）: %q → %q", first.summary, second.summary)
	}
}

// L-4：kill 不丢已产出内容。非进程作业没有进程树，取消口是 ctx；但**已经写进正文
// 的字节必须仍能取回**——kill 是"停"，不是"删"。
func TestJobKillPreservesProducedContent(t *testing.T) {
	router := asyncTestRouter(t, true)
	ctx := asyncTestCtx(t.TempDir(), "sess-inline-kill")
	// 直接登记一个进程内作业：它不跑真实读，正文由 appendNote 写（子代理/扇出作业
	// 的产出路径就是这个）。
	run, started, err := router.async.beginJob(JobSpec{
		Kind: JobKindInline, SessionID: "sess-inline-kill", Command: "read a.go", Title: "读 a.go",
	})
	if err != nil || !started {
		t.Fatalf("beginJob: started=%v err=%v", started, err)
	}
	if err := router.async.appendNote(run.handle, "已产出的前半段內容\n"); err != nil {
		t.Fatal(err)
	}
	router.async.setCancel(run.handle, func() {})

	killForTestHandle := JobHandle{Handle: run.handle}
	if _, err := (&jobManager{router: router}).Kill(ctx, killForTestHandle); err != nil {
		t.Fatalf("kill: %v", err)
	}
	router.async.finish(run.handle, asyncKilledExit)

	payload, err := (&jobManager{router: router}).Fetch(ctx, killForTestHandle)
	if err != nil {
		t.Fatalf("kill 后取回: %v", err)
	}
	var decoded asyncPayload
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.State != asyncStateKilled {
		t.Fatalf("终态 = %q, want killed", decoded.State)
	}
	if !strings.Contains(decoded.Output, "已产出的前半段內容") {
		t.Fatalf("kill 前的产出丢了: %+v", decoded)
	}
}

// L-3：批量读**派发即返回**，一批 N 个句柄，且这些句柄与进程作业同一张表
// （在途计数、会话销毁、取回都复用同一条路径）。
func TestReadBatchDispatchesJobsAndReturnsImmediately(t *testing.T) {
	router := asyncTestRouter(t, true)
	root := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("content of "+name+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx := asyncTestCtx(root, "sess-batch")

	args, err := json.Marshal(map[string]interface{}{"paths": []string{"a.txt", "b.txt", "c.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	startedAt := time.Now()
	output, err := router.scopedReadBatch(ctx, string(args))
	if err != nil {
		t.Fatalf("read_batch: %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 5*time.Second {
		t.Fatalf("派发用了 %v：批量读必须派发即返回", elapsed)
	}
	var receipt struct {
		Status string `json:"status"`
		Count  int    `json:"count"`
		Jobs   []struct {
			Handle string `json:"handle"`
			Path   string `json:"path"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal([]byte(output), &receipt); err != nil {
		t.Fatalf("受理回执不是合法 JSON: %v (%q)", err, output)
	}
	if receipt.Status != "accepted" || receipt.Count != 3 || len(receipt.Jobs) != 3 {
		t.Fatalf("回执 = %+v, want accepted/3/3 句柄", receipt)
	}
	seen := map[string]bool{}
	for _, job := range receipt.Jobs {
		if job.Handle == "" || seen[job.Handle] {
			t.Fatalf("句柄缺失或重复: %+v", receipt.Jobs)
		}
		seen[job.Handle] = true
	}

	// 同一张表：在途计数看得到它们，取回拿得到内容，会话销毁能清掉。
	pending := 0
	// 派发即返回 ⇒ 登记也是异步落表的（-race 下把顺序放大）：这里**有界等**它到 3。
	// 原先这里是立刻断言，run 37105792167 的 race-and-coverage 上就拿到 0 而红。
	for until := time.Now().Add(5 * time.Second); ; {
		pending = router.AsyncPendingFor("sess-batch")
		if pending == 3 || time.Now().After(until) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pending != 3 {
		t.Fatalf("AsyncPendingFor = %d, want 3（inline 作业必须与进程作业同一张表）", pending)
	}
	for _, job := range receipt.Jobs {
		// 派发即返回 ⇒ 取回可能撞上"还没读完"：这里等它收尾，再断言内容。
		waitAsyncTerminalForTest(t, router, job.Handle)
		payload := pollForTest(t, router, ctx, job.Handle, -1)
		if !strings.Contains(payload.Output, "content of "+job.Path) {
			t.Fatalf("%s 取回内容不对: %+v", job.Handle, payload)
		}
	}
	if got := router.CloseSessionAsync("sess-batch"); got != 0 {
		// 取回已把终态作业销项，因此这里没有可杀的在途作业——0 就是正确答案。
		t.Fatalf("销项后 CloseSessionAsync = %d, want 0", got)
	}
}

// K-6：管理动作的入口口径（表驱动）。每条都是一次可观测的判定：
// 未知 op、缺 handle、跨会话、在途 done —— 全部必须显式失败。
func TestJobManageEntryRejectsInvalidCalls(t *testing.T) {
	router := asyncTestRouter(t, true)
	root := t.TempDir()
	owner := asyncTestCtx(root, "sess-owner")
	intruder := asyncTestCtx(root, "sess-intruder")
	receipt := dispatchForTest(t, router, owner, "sleep 25")
	t.Cleanup(func() { router.CloseSessionAsync("sess-owner") })

	cases := []struct {
		name string
		ctx  context.Context
		args string
	}{
		{"未知 op", owner, `{"op":"teleport","handle":"` + receipt.Handle + `"}`},
		{"缺 op", owner, `{"handle":"` + receipt.Handle + `"}`},
		{"fetch 缺 handle", owner, `{"op":"fetch"}`},
		{"kill 缺 handle", owner, `{"op":"kill"}`},
		{"done 缺 handle", owner, `{"op":"done"}`},
		{"跨会话 observe", intruder, `{"op":"observe","handle":"` + receipt.Handle + `"}`},
		{"跨会话 fetch", intruder, `{"op":"fetch","handle":"` + receipt.Handle + `"}`},
		{"跨会话 kill", intruder, `{"op":"kill","handle":"` + receipt.Handle + `"}`},
		{"跨会话 done", intruder, `{"op":"done","handle":"` + receipt.Handle + `"}`},
		{"在途 done（终态只由执行体判定）", owner, `{"op":"done","handle":"` + receipt.Handle + `"}`},
		{"未知句柄 observe", owner, `{"op":"observe","handle":"a404"}`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := router.scopedJobManage(testCase.ctx, testCase.args); err == nil {
				t.Fatalf("%s 必须显式失败（不得静默降级成某个看起来安全的动作）", testCase.name)
			}
		})
	}
}

// L-5：子代理作业纳入契约。四件事必须都能从**模型侧**做到：
//
//	① observe 只读看它在干什么（带 kind=subagent，不推进游标）；
//	② kill 提前终止（取消口 + 已产出内容保留）；
//	③ 执行体自己收尾（CompleteJob = 被动 done）；
//	④ done 销项（终态行从投影里划掉，重复 done 无副作用）。
//
// 不需要 OS 进程化：这一整套只依赖句柄契约（设计文档 §B.3）。
func TestSubagentJobContract(t *testing.T) {
	router := asyncTestRouter(t, true)
	ctx := asyncTestCtx(t.TempDir(), "sess-sub")

	cancelled := false
	handle, err := router.AddSubagentJob(JobSpec{
		SessionID: "sess-sub", Command: "audit module A", Title: "子代理: s1",
	}, func() { cancelled = true })
	if err != nil || handle == "" {
		t.Fatalf("AddSubagentJob: handle=%q err=%v", handle, err)
	}
	if got := router.SubagentJobHandles("sess-sub"); len(got) != 1 || got[0] != handle {
		t.Fatalf("子代理作业句柄清单 = %v, want [%s]", got, handle)
	}

	// ① observe：只读读数（不推进游标、不消费输出）。
	manager := &jobManager{router: router}
	observed, err := manager.Status(ctx, JobHandle{Handle: handle})
	if err != nil {
		t.Fatalf("observe 子代理: %v", err)
	}
	var observedPayload asyncPayload
	if err := json.Unmarshal(observed, &observedPayload); err != nil {
		t.Fatal(err)
	}
	if observedPayload.Status != "observed" || !strings.Contains(observedPayload.Output, "subagent") {
		t.Fatalf("子代理观察读数不对: %+v", observedPayload)
	}

	// ② 运行体系把已产出的发现写进正文（产出载体与其他作业一致）。
	if err := router.NoteJob(handle, "发现：module A 的入口在 main.go\n"); err != nil {
		t.Fatalf("NoteJob: %v", err)
	}

	// ③ kill：取消口被调用，且**已产出内容保留**。
	killed, err := manager.Kill(ctx, JobHandle{Handle: handle})
	if err != nil {
		t.Fatalf("kill 子代理: %v", err)
	}
	if !cancelled {
		t.Fatal("kill 没有触发子代理作业的取消口")
	}
	var killedPayload asyncPayload
	if err := json.Unmarshal(killed, &killedPayload); err != nil {
		t.Fatal(err)
	}
	if killedPayload.Status != "killed" {
		t.Fatalf("kill 回执 = %+v, want killed", killedPayload)
	}
	// 执行体收尾（被动 done）：终态迁移一次，重复调用无副作用。
	if !router.CompleteJob(handle, "killed") {
		t.Fatal("CompleteJob 没能合成终态")
	}
	if router.CompleteJob(handle, "killed") {
		t.Fatal("重复 CompleteJob 又迁移了一次终态（终态只迁移一次）")
	}
	fetched, err := manager.Fetch(ctx, JobHandle{Handle: handle})
	if err != nil {
		t.Fatalf("取回被 kill 的子代理: %v", err)
	}
	var fetchedPayload asyncPayload
	if err := json.Unmarshal(fetched, &fetchedPayload); err != nil {
		t.Fatal(err)
	}
	if fetchedPayload.State != asyncStateKilled {
		t.Fatalf("终态 = %q, want killed", fetchedPayload.State)
	}
	if !strings.Contains(fetchedPayload.Output, "module A 的入口") {
		t.Fatalf("kill 前的发现丢了: %+v", fetchedPayload)
	}

	// ④ 重复 done 无副作用（销项之后句柄不在册，但必须说清而不是报未知句柄）。
	if _, err := manager.Done(ctx, JobHandle{Handle: handle}); err != nil {
		t.Fatalf("销项: %v", err)
	}
	repeat, err := manager.Done(ctx, JobHandle{Handle: handle})
	if err != nil {
		t.Fatalf("重复 done 不该报错（幂等）：%v", err)
	}
	var repeatPayload asyncPayload
	if err := json.Unmarshal(repeat, &repeatPayload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(repeatPayload.Output, "已销项") {
		t.Fatalf("重复 done 的说明不对: %+v", repeatPayload)
	}
	if got := router.SubagentJobHandles("sess-sub"); len(got) != 0 {
		t.Fatalf("销项后仍报在册子代理: %v", got)
	}
}

// L-1：bash_bg 的派发回执与旧 `bash(background=true)` **逐字段等价**。
//
// 等价不是"看着像"：回执只由 renderJobAccepted 渲染一条路径产出，这条用例钉住它
// 带着 handle / log_path / state=running / exit_code=-1（模型侧的观感必须连续）。
func TestJobAcceptedReceiptMatchesLegacyShape(t *testing.T) {
	router := asyncTestRouter(t, true)
	ctx := asyncTestCtx(t.TempDir(), "sess-receipt")
	receipt := dispatchForTest(t, router, ctx, "sleep 25")
	t.Cleanup(func() { router.CloseSessionAsync("sess-receipt") })

	if receipt.Status != "accepted" || receipt.State != asyncStateRunning {
		t.Fatalf("回执 = %+v, want accepted/running", receipt)
	}
	if receipt.Handle == "" || receipt.LogPath == "" || receipt.ExitCode != -1 {
		t.Fatalf("回执缺字段（handle/log_path/exit_code=-1）: %+v", receipt)
	}
	if receipt.Output != "" {
		t.Fatalf("受理回执不得携带作业输出: %q", receipt.Output)
	}
	// 旧路径（bash(background=true)）与契约路径必须落到同一条登记记录上。
	snapshot, ok := router.async.snapshot(receipt.Handle)
	if !ok {
		t.Fatal("回执里的句柄不在登记表里")
	}
	if snapshot.kind != string(JobKindProcess) || snapshot.logPath != receipt.LogPath {
		t.Fatalf("登记记录与回执不一致: %+v", snapshot)
	}
}
