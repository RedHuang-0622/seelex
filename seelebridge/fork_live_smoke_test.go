package seelebridge

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/seelexctx"
)

// TestForkSubagentsLiveSmoke 真实 API 冒烟（非默认运行）：
//   - 需要真实账号配置：SEELEX_ACCOUNTS_PATH（默认 ../config/accounts.yaml，
//     只传路径给 Runtime，不读取内容）；
//   - 运行：$env:SEELEX_LIVE_SMOKE=1; go test ./seelebridge -run TestForkSubagentsLiveSmoke -v
//
// 场景：双子代理 fork——一个取时间、一个读 README 总结，summary 节点合并。
//
// 判据（2026-10-07 改造为**产物哈希对照**，口径见 fork_hash_judgment_test.go 头注）：
// 每个句柄取回的正文必须与**该子代理自己记下来的产物**（语义结果
// NodeFirstPersonView(id).Result.Output）规范化（仅去首尾空白）后逐字节相同，且两个
// 子代理的产物摘要必须互不相同——后者是"按编号取回"这件事的判别力前提。
// 旧的"正文里必须出现子代理 id 字面量"判据已删除：它测的是模型措辞（`live_time`
// 只是因为模型提到分支名 `seelex/live_time` 才侥幸过关），不是 fork 链路。
func TestForkSubagentsLiveSmoke(t *testing.T) {
	if os.Getenv("SEELEX_LIVE_SMOKE") == "" {
		t.Skip("set SEELEX_LIVE_SMOKE=1 to run the real-API smoke")
	}
	accountsPath := os.Getenv("SEELEX_ACCOUNTS_PATH")
	if accountsPath == "" {
		accountsPath = filepath.Join("..", "config", "accounts.yaml")
	}
	root, err := filepath.Abs(filepath.Join(".."))
	if err != nil {
		t.Fatal(err)
	}

	runtime, err := NewRuntime(RuntimeConfig{
		AccountsPath:      accountsPath,
		ToolCallTimeout:   5 * time.Minute,
		ApprovalTimeout:   10 * time.Minute,
		HeartbeatInterval: 5 * time.Second,
		Limits: seelexctx.Limits{
			AsyncExec:      seelexctx.AsyncExecLimits{Enabled: true},
			ForkTimeoutSec: 15 * 60,
		},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	if err := runtime.BindProjectRoot(root); err != nil {
		t.Fatalf("BindProjectRoot: %v", err)
	}
	runtime.SetRuntimeVisibilityProjection(RuntimeVisibilityProjection{GoalSkillActive: true})

	// 作业化派发：调用立刻返回句柄，结果经 job_manage(op=fetch) 取回；真实 API 下
	// 等待窗口按 15 分钟给足。
	started := time.Now()
	receipt, err := forkDispatch(t, runtime,
		`{"subagents":[
			{"id":"live_time","goal":"获取当前系统时间并格式化为 yyyy-MM-dd HH:mm:ss"},
			{"id":"live_file","goal":"读取仓库根目录 README.md 的前 20 行，用两句话总结 Seelex 是什么"}
		]}`)
	if err != nil {
		t.Fatalf("fork_subagents live failed (%s): %v", time.Since(started), err)
	}
	handles := make([]string, 0, len(receipt.Jobs))
	for _, job := range receipt.Jobs {
		handles = append(handles, job.Handle)
	}
	forkWaitTerminalFor(t, runtime, handles, 15*time.Minute)

	// 终态在取回前读（终态作业一经取回就销项）：不是 done 就没有可比对的产物面，
	// 与其拿一份空的/失败的正文去比哈希，不如在这里说清是哪一条没跑成。
	states := make(map[string]string, len(handles))
	for _, record := range runtime.AsyncRunsSnapshot() {
		states[record.Handle] = record.State.String()
	}
	for _, job := range receipt.Jobs {
		if state := states[job.Handle]; state != "done" {
			t.Fatalf("子代理作业 %s(id=%s) 终态 = %q, want done（取回前读数 %+v）",
				job.Handle, job.ID, state, states)
		}
	}

	// 基准面 = 每个子代理自己记下来的产物（语义结果，与取回正文来自不同路径）。
	bases := make(map[string]string, len(receipt.Jobs))
	for _, job := range receipt.Jobs {
		view := runtime.NodeFirstPersonView(job.ID)
		if view == nil {
			t.Fatalf("子代理 %s 的第一视角为空（基准面取不到）", job.ID)
		}
		if view.Result == nil {
			t.Fatalf("子代理 %s 的语义结果为空（基准面取不到，哈希对照无法成立）", job.ID)
		}
		if view.Result.NodeID != job.ID {
			t.Fatalf("语义结果 NodeID = %q, want %q", view.Result.NodeID, job.ID)
		}
		base := normalizeForkProduct(view.Result.Output)
		if base == "" {
			t.Fatalf("子代理 %s 的语义结果 Output 为空（基准面为空，判据没有意义）", job.ID)
		}
		bases[job.ID] = base
	}

	// 取回并逐 id 对照摘要：同一份产物摘要只能属于一条作业——两条作业摘要相同
	// 就说明"按编号取回"这件事没有判别力（判据本身无意义），当场说清而不是放过。
	basisOwner := make(map[string]string, len(receipt.Jobs))
	for _, job := range receipt.Jobs {
		got := forkFetch(t, runtime, job.Handle)
		if normalizeForkProduct(got) == "" {
			t.Fatalf("句柄 %s(id=%s) 取回的正文为空", job.Handle, job.ID)
		}
		digest := forkProductDigest(got)
		if digest != forkProductDigest(bases[job.ID]) {
			t.Fatalf("句柄 %s 的取回正文与该子代理 %s 自己的产物不一致%s",
				job.Handle, job.ID, forkProductDiff(bases[job.ID], got))
		}
		if other, duplicated := basisOwner[digest]; duplicated {
			t.Fatalf("子代理 %s 与 %s 的产物摘要相同（%s）：按编号取回没有判别力，判据无意义",
				other, job.ID, digest)
		}
		basisOwner[digest] = job.ID
		t.Logf("句柄 %s(id=%s) 取回正文摘要 %s（%d 字节）", job.Handle, job.ID, digest[:12], len(got))
	}

	t.Logf("耗时: %s；完整会话/打点见工作区子代理树", time.Since(started))
}
