package seelebridge

// fork_hash_judgment_test.go —— fork 取回的**产物哈希对照**判据（真机冒烟 + 确定性对照共用）。
//
// 判据的取向：一条子代理作业的取回正文必须与**该子代理自己记下来的产物**逐字节等价
// （只规范化首尾空白），而不是"正文里出现某个 id 字面量"。字面量判据没有判别力：子代理
// 的最终答复里只要碰巧提到 `seelex/live_time` 这样的分支名就侥幸过关，而真正没提到自己
// id 的产物（`live_file`）会红——那测的是模型怎么措辞，不是 fork 链路。
// 哈希对照则有判别力：把 handle 交叉配对必然对不上。
//
// 基准面选 `NodeFirstPersonView(id).Result.Output`——该子代理自己的语义结果（AgentNode
// 收尾时从子代理会话快照提取登记，经 session.SubagentSessions.Result 只读取回，见
// seelebridge/node/agent_node.go:mergeBack）。它和取回正文来自**两条不同的路径**
// （语义结果面 vs 作业正文面：作业正文由 fork 编排按 spec.ID 取子代理树摘要写入、
// 经 job_manage(op=fetch) 消费式取回），所以"哈希相等"不是同义反复。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/types"
	seenode "github.com/RedHuang-0622/seelex/seelebridge/node"
)

// normalizeForkProduct 是产物规范化：**只**去首尾空白（作业正文末尾带一个换行，
// 语义结果的 Output 保留原样）。刻意不做大小写折叠 / 行尾折叠 / 去空行——那些宽松
// 处理会掩盖"取回的不是这一条"这类偏差，让判据从"同一产物"退化成"长得差不多"。
func normalizeForkProduct(product string) string {
	return strings.TrimSpace(product)
}

// forkProductDigest 是规范化产物的 sha256 摘要（十六进制）。
func forkProductDigest(product string) string {
	sum := sha256.Sum256([]byte(normalizeForkProduct(product)))
	return hex.EncodeToString(sum[:])
}

// forkProductEdges 取规范化产物的首尾片段（失败信息里定位差异用；各留 80 字节，
// 产物可能有上千字，整段打进失败信息只会淹掉读数）。
func forkProductEdges(product string) (string, string) {
	const edge = 80
	if len(product) <= 2*edge {
		return product, product
	}
	return product[:edge], product[len(product)-edge:]
}

// forkProductDiff 渲染两侧产物的可比读数：长度 + 首尾片段，并在"取回正文以基准开头"
// 时点明最可能的成因（基准面被产品侧上限截断），免得读到差异的人先怀疑 fork 链路。
func forkProductDiff(want, got string) string {
	normalizedWant := normalizeForkProduct(want)
	normalizedGot := normalizeForkProduct(got)
	wantHead, wantTail := forkProductEdges(normalizedWant)
	gotHead, gotTail := forkProductEdges(normalizedGot)
	note := ""
	switch {
	case normalizedWant == "" || normalizedGot == "":
		note = "；有一侧为空（对照不成立）"
	case len(normalizedWant) == 2000 && strings.HasPrefix(normalizedGot, normalizedWant):
		note = "；取回正文以基准开头——基准面疑似被产品侧上限截断（node 语义结果 Output 上限 nodeOutputMax）"
	}
	return fmt.Sprintf("：基准 len=%d head=%q tail=%q，取回 len=%d head=%q tail=%q%s",
		len(normalizedWant), wantHead, wantTail, len(normalizedGot), gotHead, gotTail, note)
}

// forkScriptedProduct 把"某个子代理的身份"折成一条脚本化产物：同一身份恒等、不同身份
// 必然不同（sha256 摘要前 16 位十六进制），且**不含 id 字面量**——后者让"前提钉子"仍然
// 成立：旧的"正文里必须出现 id 字面量"判据在这条完全绿的链路上也必然红。
func forkScriptedProduct(taskID string) string {
	sum := sha256.Sum256([]byte(taskID))
	return "脚本化产物-" + hex.EncodeToString(sum[:])[:16]
}

// scriptedProductCompleter 按**子代理身份（NodeScope.TaskID）**作答，并记录每次调用
// 服务了谁。
//
// 为什么产物不按"账号"或"目标文本"给：这两条路都被实测证伪过（2026-10-07，
// _logs/wi_fork_hash_probe20.txt / _logs/wi_fork_hash_diag_full.txt / wi_fork_hash_negctl.txt）：
//
//  1. **按账号**给固定回复（"child-one 答 time、child-two 答 file"）不成立——fork 子代理的
//     账号解析不是契约：子代理作用域没有 branchID 时走"当前余量最大的账号"
//     （seelebridge/account/manager.go:186-196 leastBusyForRole），有 branchID 时才走
//     FNV-1a 稳定哈希（seelebridge/account/account.go:59 ResolveForBranch / :104 StableIndex）。
//     实测 -count=15：id 取 one_agent/two_agent 时**两个子代理恒被同一账号服务**
//     （child-two 15/15），id 取 time_agent/file_agent 时恒分居两个账号 15/15。
//     靠 id 撞账号来"保证两条产物不同"，判据就被账号解析的偶然性牵着走。
//
//  2. **按目标文本**作答也不成立——子代理提示词里可能出现**兄弟子代理的目标**：全量套件里
//     本用例两次红在"两个子代理产物相同"，而同一进程里单跑恒绿（同一份代码、同一组 id）。
//     把每次调用的"命中了哪个目标"打进日志后可见提示词内容是**随运行时机变化**的
//     （ZZPROBE 读数：hasTimeGoal/hasReadmeGoal 在一次全量跑里 true/false，在另一次里
//     红用例侧的读数指向兄弟目标），因此任何按提示词文本作答的脚本化 completer 都可能
//     张冠李戴。判据本身不该建在这上面。
//
// 按身份作答则与"哪个账号服务谁""提示词里写了什么"都无关：无论谁服务谁，产物都跟着
// 子代理身份走。
type scriptedProductCompleter struct {
	mu     sync.Mutex
	served []string
}

func (c *scriptedProductCompleter) Complete(ctx context.Context, _ []types.Message, _ []types.Tool) (types.Message, error) {
	scope, _ := seenode.NodeScopeFromContext(ctx)
	c.mu.Lock()
	c.served = append(c.served, scope.TaskID)
	c.mu.Unlock()
	if scope.TaskID == "" {
		return types.Message{}, fmt.Errorf("脚本化 completer 拿不到子代理身份（NodeScope.TaskID 为空），产物无从确定")
	}
	reply := forkScriptedProduct(scope.TaskID)
	return types.Message{Role: "assistant", Content: &reply}, nil
}

func (c *scriptedProductCompleter) seenServed() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.served...)
}

// TestForkProductHashJudgmentDeterministic 是 fork 取回的**确定性对照**用例：
// 用脚本化 completer 造两个产物不同的子代理（不发起任何网络调用，参看
// fork_smoke_test.go 的 TestForkSubagentsSmokeTimeAndFileSummary），把
// "句柄 → 该子代理自己的产物"这条对照在确定性路径上钉住：
//
//  1. 基准 = 该子代理自己记下来的**语义结果**（NodeFirstPersonView(id).Result.Output）——
//     确定性路径拿得到这个面（AgentNode 收尾经 mergeBack 登记，与真机同一段代码），
//     所以不需要退化到别的产物面；
//  2. 基准必须**就是**该 id 的脚本化产物（钉在与取回路径无关的事实源上：产物只由
//     子代理身份决定，既不看账号也不看提示词文本），且两个子代理的产物必然不同；
//  3. 取回 = job_manage(op=fetch, handle)，规范化摘要必须与基准相同；
//  4. **交叉配对必须对不上**：拿 A 的句柄去对照 B 的基准必须不等。否则"按编号取回"
//     这件事没有判别力（两条作业正文一样、或者都退回了整批正文，都会在这里红）。
func TestForkProductHashJudgmentDeterministic(t *testing.T) {
	runtime := newAsyncTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	if _, err := runtime.NewMainSessionWithID("sess_hash", nil); err != nil {
		t.Fatal(err)
	}

	// 子代理 id 只出现在派发参数里；两个子代理的产物由各自的身份决定。
	subagentIDs := []string{"time_agent", "file_agent"}
	// 两个账号各挂一个**同种**completer：谁服务谁都能给出"跟着身份走"的产物。
	childOne := &scriptedProductCompleter{}
	childTwo := &scriptedProductCompleter{}
	injectScriptedCompleters(t, runtime, map[string]agent.Completer{
		"child-one": childOne,
		"child-two": childTwo,
	})

	receipt, err := forkDispatch(t, runtime,
		`{"subagents":[{"id":"time_agent","goal":"输出当前时间"},{"id":"file_agent","goal":"查看 README 并总结文件内容"}]}`)
	if err != nil {
		t.Fatalf("fork_subagents 派发失败: %v", err)
	}
	handles := make([]string, 0, len(receipt.Jobs))
	for _, job := range receipt.Jobs {
		handles = append(handles, job.Handle)
	}
	forkWaitTerminal(t, runtime, handles)
	t.Logf("账号服务读数：child-one %v；child-two %v（账号解析不是契约，见 scriptedProductCompleter 注释）",
		childOne.seenServed(), childTwo.seenServed())

	// 终态在取回前读（终态作业一经取回就销项）。
	states := make(map[string]string, len(handles))
	for _, record := range runtime.AsyncRunsSnapshot() {
		states[record.Handle] = record.State.String()
	}

	// 基准面：每个子代理自己的语义结果——它必须逐字就是**该 id 的脚本化产物**（把基准面
	// 钉在与取回路径无关的事实源上），且两个子代理的基准是两条不同的产物。
	bases := make(map[string]string, len(receipt.Jobs))
	baseOwner := make(map[string]string, len(receipt.Jobs))
	for _, job := range receipt.Jobs {
		if state := states[job.Handle]; state != "done" {
			t.Fatalf("子代理作业 %s(id=%s) 终态 = %q, want done（取回前读数 %+v）", job.Handle, job.ID, state, states)
		}
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
		want := forkScriptedProduct("subagent:" + job.ID)
		base := normalizeForkProduct(view.Result.Output)
		if base == "" {
			t.Fatalf("子代理 %s 的语义结果 Output 为空（基准面为空，判据没有意义）", job.ID)
		}
		if base != want {
			t.Fatalf("子代理 %s 的语义结果不是该身份自己的产物%s", job.ID, forkProductDiff(want, base))
		}
		// 前提钉子：脚本化产物**不含自己的 id 字面量**。旧的"正文里必须出现 id 字面量"
		// 判据在这样一条完全绿链路上也必然红——它测的是模型怎么措辞，不是 fork 链路。
		if strings.Contains(base, job.ID) {
			t.Fatalf("脚本化产物 %q 竟然含自己的 id：本用例的前提（产物不含 id 字面量）不成立", base)
		}
		if other, duplicated := baseOwner[base]; duplicated {
			t.Fatalf("子代理 %s 与 %s 的产物面相同（%q）：两条作业没有可区分的产物", other, job.ID, base)
		}
		baseOwner[base] = job.ID
		bases[job.ID] = base
		t.Logf("子代理 %s 的基准产物 = %q（%d 字节）", job.ID, base, len(base))
	}
	if len(baseOwner) != len(subagentIDs) {
		t.Fatalf("基准产物只覆盖了 %d 个子代理（want %d）", len(baseOwner), len(subagentIDs))
	}

	// 取回：逐 id 对照摘要；同时记录每个 id 的取回产物供交叉配对用。
	fetched := make(map[string]string, len(receipt.Jobs))
	for _, job := range receipt.Jobs {
		got := normalizeForkProduct(forkFetch(t, runtime, job.Handle))
		if got == "" {
			t.Fatalf("句柄 %s(id=%s) 取回的正文为空", job.Handle, job.ID)
		}
		gotDigest := forkProductDigest(got)
		if gotDigest != forkProductDigest(bases[job.ID]) {
			t.Fatalf("句柄 %s 的取回正文与该子代理 %s 自己的产物不一致%s", job.Handle, job.ID, forkProductDiff(bases[job.ID], got))
		}
		fetched[job.ID] = got
		t.Logf("句柄 %s(id=%s) 取回 %d 字节，摘要 %s", job.Handle, job.ID, len(got), gotDigest[:12])
	}

	// 判别力：两个子代理的产物摘要必须互不相同（否则按编号取回没有判别力）。
	basisByDigest := make(map[string]string, len(bases))
	for id, base := range bases {
		digest := forkProductDigest(base)
		if other, duplicated := basisByDigest[digest]; duplicated {
			t.Fatalf("子代理 %s 与 %s 的产物摘要相同（%s）：判据没有判别力", other, id, digest)
		}
		basisByDigest[digest] = id
	}
	// 交叉配对必须对不上：A 的取回不能等于 B 的基准（也不能等于 B 的取回）。
	for id, got := range fetched {
		for otherID := range bases {
			if otherID == id {
				continue
			}
			if forkProductDigest(got) == forkProductDigest(bases[otherID]) {
				t.Fatalf("交叉配对对上了：子代理 %s 的取回正文等于子代理 %s 的产物%s",
					id, otherID, forkProductDiff(bases[otherID], got))
			}
		}
	}
}
