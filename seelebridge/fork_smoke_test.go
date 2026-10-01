package seelebridge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/Seele/workplan/codec"
	workplanTypes "github.com/RedHuang-0622/Seele/workplan/core/types"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge/fork"
	seenode "github.com/RedHuang-0622/seelex/seelebridge/node"
	"github.com/RedHuang-0622/seelex/seelebridge/plan"
)

// scopeRecordingCompleter 记录每次请求的 NodeScope.TaskID（验证 B6 装配件
// 只把 task_id 绑进作用域），并返回固定文本。
type scopeRecordingCompleter struct {
	mu      sync.Mutex
	taskIDs []string
	reply   string
}

func (c *scopeRecordingCompleter) Complete(ctx context.Context, _ []types.Message, _ []types.Tool) (types.Message, error) {
	scope, _ := seenode.NodeScopeFromContext(ctx)
	c.mu.Lock()
	c.taskIDs = append(c.taskIDs, scope.TaskID)
	c.mu.Unlock()
	reply := c.reply
	return types.Message{Role: "assistant", Content: &reply}, nil
}

func (c *scopeRecordingCompleter) seenTaskIDs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.taskIDs...)
}

// TestForkSubagentsSmokeTimeAndFileSummary 双子代理冒烟：一个输出时间、一个
// 查看文件并总结，summary 节点（主侧）合并产出；同时验证 B6 task_id 注入
// 到子代理 NodeScope（只绑 id，无内容）与注册表幂等绑定。
func TestForkSubagentsSmokeTimeAndFileSummary(t *testing.T) {
	runtime := newAsyncTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	if _, err := runtime.NewMainSessionWithID("sess_smoke", nil); err != nil {
		t.Fatal(err)
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	timeCompleter := &scopeRecordingCompleter{reply: "当前时间: " + now}
	fileCompleter := &scopeRecordingCompleter{reply: "README 摘要: Seelex 是面向软件工程任务的 Agent Harness / Multi-Agent Runtime"}
	injectScriptedCompleters(t, runtime, map[string]agent.Completer{
		"child-one": timeCompleter,
		"child-two": fileCompleter,
	})

	batch := forkRun(t, runtime,
		`{"subagents":[{"id":"time_agent","goal":"输出当前时间"},{"id":"file_agent","goal":"查看 README 并总结文件内容"}]}`)
	t.Logf("=== 取回的子代理产出（主侧收到的内容）===\n%s", batch.Output)
	for _, handle := range batch.Handles {
		if batch.States[handle] != "done" {
			t.Fatalf("子代理作业 %s 终态 = %q, want done（读数 %+v）", handle, batch.States[handle], batch.States)
		}
	}
	// 两个子代理的产出都在各自句柄的取回正文里。
	if !strings.Contains(batch.Output, now) || !strings.Contains(batch.Output, "README") {
		t.Fatalf("取回的产出必须含两个子代理的输出，得到: %s", batch.Output)
	}

	// B6：两个子代理都拿到注入的 task_id（作用域绑定，无内容污染）。
	seen := append(timeCompleter.seenTaskIDs(), fileCompleter.seenTaskIDs()...)
	want := map[string]bool{"subagent:time_agent": false, "subagent:file_agent": false}
	for _, taskID := range seen {
		if _, ok := want[taskID]; ok {
			want[taskID] = true
		}
	}
	for taskID, found := range want {
		if !found {
			t.Fatalf("subagent NodeScope.TaskID %q was not injected (seen=%v)", taskID, seen)
		}
	}

	// 注册表幂等：两个 task，终态 completed，Assignee 为被动 role:sessionID
	// 身份并已自动上名单。
	tasks := runtime.TaskSnapshot()
	if len(tasks) != 2 {
		t.Fatalf("tasks = %+v, want 2", tasks)
	}
	byID := make(map[string]dto.TaskRecord, len(tasks))
	for _, task := range tasks {
		byID[task.ID] = task
	}
	timeTask := byID["subagent:time_agent"]
	fileTask := byID["subagent:file_agent"]
	if timeTask.Status != dto.TaskCompleted || timeTask.Assignee != "main:sess_smoke" ||
		!containsParticipant(timeTask.Participants, "main:sess_smoke") {
		t.Fatalf("time task = %+v", timeTask)
	}
	if fileTask.Status != dto.TaskCompleted || fileTask.Assignee != "main:sess_smoke" ||
		!containsParticipant(fileTask.Participants, "main:sess_smoke") {
		t.Fatalf("file task = %+v", fileTask)
	}
}

func containsParticipant(participants []string, want string) bool {
	for _, participant := range participants {
		if participant == want {
			return true
		}
	}
	return false
}

// capturingCompleter 是回调注入探针：记录子代理收到的完整消息与 NodeScope
// （TaskID 是否只进作用域、不进 prompt），用于检查任务投放后的实际输入。
type capturingCompleter struct {
	mu       sync.Mutex
	messages []types.Message
	taskID   string
	reply    string
}

func (c *capturingCompleter) Complete(ctx context.Context, messages []types.Message, _ []types.Tool) (types.Message, error) {
	scope, _ := seenode.NodeScopeFromContext(ctx)
	c.mu.Lock()
	c.messages = append([]types.Message(nil), messages...)
	c.taskID = scope.TaskID
	c.mu.Unlock()
	reply := c.reply
	return types.Message{Role: "assistant", Content: &reply}, nil
}

func (c *capturingCompleter) snapshot() ([]types.Message, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]types.Message(nil), c.messages...), c.taskID
}

// TestSubagentTaskDeliveryCallbackInput 回调注入冒烟：fork 一个子代理，用
// Complete 回调抓取它实际收到的输入，验证 charter 提示词规范渲染（目标/
// 预算/收尾协议）以及 B6 task_id 只进 NodeScope 不进 prompt。
func TestSubagentTaskDeliveryCallbackInput(t *testing.T) {
	runtime := newAsyncTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	probe := &capturingCompleter{reply: "完成：输出时间 ok"}
	injectScriptedCompleters(t, runtime, map[string]agent.Completer{"child-one": probe})

	forkAndWait(t, runtime, `{"subagents":[{"id":"time_agent","goal":"输出当前时间并格式化"}]}`)

	messages, taskID := probe.snapshot()
	if len(messages) == 0 {
		t.Fatal("callback received no messages from the subagent")
	}
	roles := make([]string, 0, len(messages))
	lengths := make([]int, 0, len(messages))
	for _, message := range messages {
		roles = append(roles, message.Role)
		if message.Content != nil {
			lengths = append(lengths, len(*message.Content))
		} else {
			lengths = append(lengths, 0)
		}
	}
	t.Logf("回调收到的消息角色与长度: roles=%v lengths=%v", roles, lengths)
	var full strings.Builder
	for _, message := range messages {
		content := ""
		if message.Content != nil {
			content = *message.Content
		}
		fmt.Fprintf(&full, "[%s] %s\n", message.Role, content)
	}
	t.Logf("=== 子代理实际收到的输入（回调注入）===\n%s", full.String())

	// charter 规范渲染断言。
	joined := full.String()
	for _, want := range []string{
		"# Role", "# Context", "# Task", "# Investigation", "# Constraints", "# Verification",
		"目标：\n- 输出当前时间并格式化",
		"最大迭代轮数",
		"工作强度评估",
		"git add -A && git commit",
		"fork_subagents",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("charter missing %q:\n%s", want, joined)
		}
	}
	// B6：task_id 只绑进 NodeScope，不进 prompt 内容。
	if taskID != "subagent:time_agent" {
		t.Fatalf("NodeScope.TaskID = %q, want subagent:time_agent", taskID)
	}
	if strings.Contains(joined, taskID) {
		t.Errorf("task_id must not be injected into prompt content:\n%s", joined)
	}
}

// failingCompleter 模拟子代理运行期失败（报错路径冒烟）。
type failingCompleter struct {
	err error
}

func (f *failingCompleter) Complete(context.Context, []types.Message, []types.Tool) (types.Message, error) {
	return types.Message{}, f.err
}

// TestForkSubagentsFailurePropagationSmoke 报错路径：子代理失败必须**显式**呈现
// （作业终态 failed，不无声卡死），且 worktable 中该子代理任务标 failed。
func TestForkSubagentsFailurePropagationSmoke(t *testing.T) {
	runtime := newAsyncTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	injectScriptedCompleters(t, runtime, map[string]agent.Completer{
		"child-one": &failingCompleter{err: errors.New("boom: subagent crashed")},
	})

	batch := forkRun(t, runtime, `{"subagents":[{"id":"bad_agent","goal":"一定会失败"}]}`)
	if state := batch.States[batch.Handles[0]]; state != "failed" {
		t.Fatalf("失败子代理的作业终态 = %q, want failed（读数 %+v）", state, batch.States)
	}
	// 整批归零（唯一子代理失败）时作业正文仍带着失败原因：模型据此知道为什么没产出。
	if !strings.Contains(batch.Output, "boom") {
		t.Fatalf("失败子代理的作业正文必须带失败原因:\n%s", batch.Output)
	}
	records := runtime.TaskSnapshot()
	found := false
	for _, record := range records {
		if record.ID == "subagent:bad_agent" {
			found = true
			if record.Status != dto.TaskFailed {
				t.Fatalf("subagent task status = %v, want failed", record.Status)
			}
		}
	}
	if !found {
		t.Fatal("subagent task missing from the worktable registry")
	}
}

// TestForkSummaryKeeps2000ChineseRunes 汇总窗口按“字”计数：2000 汉字结论
// 完整保留（不再被 160 字节/行截断）。
//
// 2026-10-01：fork_subagents 改为作业化派发后，汇总节点不再进模型的返回（模型按句柄
// 取回各子代理**完整产出**），汇总窗口因此按纯函数钉住——它是 summary 节点的性质，
// 不是 fork 工具的返回形状。
func TestForkSummaryKeeps2000ChineseRunes(t *testing.T) {
	// 正常长结论是多行的：20 行 × 100 字 = 2000 字，逐行不超 160 字上限。
	lines := make([]string, 20)
	for index := range lines {
		lines[index] = strings.Repeat("文", 100)
	}
	summary, fullRunes, truncated := fork.ResultSummaryLines(strings.Join(lines, "\n"))
	if truncated {
		t.Fatalf("2000 字结论不应被截断（fullRunes=%d）", fullRunes)
	}
	// fullRunes 数的是整段正文（含 19 个换行）：20×100 汉字 + 19 个 '\n'。
	if want := 20*100 + (20 - 1); fullRunes != want {
		t.Fatalf("完整输出字数 = %d, want %d", fullRunes, want)
	}
	if got := strings.Count(summary, "文"); got != 2000 {
		t.Fatalf("结论主体必须完整保留在汇总里（文count=%d）", got)
	}
}

// TestForkSummaryReportsTruncatedSize 超窗截断时汇总节点附上完整输出字数：
// 模型据此判断是否需要 read_tool_result 读回，而不是凭空重跑。
func TestForkSummaryReportsTruncatedSize(t *testing.T) {
	lines := make([]string, 60)
	for index := range lines {
		lines[index] = fmt.Sprintf("行%d %s", index, strings.Repeat("字", 80))
	}
	node := fork.NewSummaryNode(codec.NodeSpec[plan.SeelexNodeInput]{
		ID: "summary", Kind: "summary",
		Input: plan.SeelexNodeInput{ID: "summary", Input: "summarize"},
	})
	wc := workplanTypes.NewWorkflowContext()
	wc.SetResultRaw("s1", strings.Join(lines, "\n"))

	out, err := node.Run(context.Background(), wc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "完整输出") || !strings.Contains(out, "已截断") {
		t.Fatalf("截断时必须附完整输出字数提示：%s", out)
	}
}
