package main

// teamwork_headless_smoke_test.go — **headless 冒烟**：把团队作业面（leader 派活 → teammate
// 干活 → 尾插回执 → 做完自动返回 → 会话查看 → 验收收口）在**真实装配**上跑一遍，逐阶段留探针。
//
// 为什么必须是这一层：假装载只能证明"我调用过的那些函数是对的"。这条链的每一跳都在不同层
// （工具面 → 编排域 → jobs.Manager → 应用层触发 → 会话投影），任何一跳错位都只会在**真实
// 装配**上显形——而本轮的三个现场（切会话看板消失 / 做完不自动返回 / 看 teammate 会话看到的
// 是历史）全都是这种错位。
//
// 与前端同源的那条路径也在探针里：看板与"这件事的会话"读的都是**后端只读投影**
// （runtime.teamwork_board / TeammateSessionLive）——GUI 与 TUI 都只消费它们，因此这一遍
// 冒烟覆盖的就是有前端的那条路径（不是另造一条 headless 专用的）。
//
// 驱动方式：脚本化 provider（**不真调模型**）。leader 的每个回合按脚本发一个工具调用，
// worker 那一轮按标记（<round_input> + "以 exec 的身份"）识别并回一句正文——
// 标记取自**解码后的正文**（wire 上 `<` 是 `\u003c`，见 requestBodyText）。
// 「做完自动返回」起的那个回合（正文是 application 侧那条回执）**不消费脚本**：
// 它是框架自己起的，验收由用例自己选时机驱动。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core"
	"github.com/RedHuang-0622/seelex/seelebridge"
	seeteamwork "github.com/RedHuang-0622/seelex/seelebridge/teamwork"
	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/sessionstore"
	workspacerepo "github.com/RedHuang-0622/seelex/workspace"
)

// smokeStep 是一次探针：名字 + 结论（宁可留痕，也不静默通过）。
type smokeStep struct {
	Name  string
	Value string
}

type smokeReport struct {
	mu    sync.Mutex
	steps []smokeStep
}

func (r *smokeReport) add(name, format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value := fmt.Sprintf(format, args...)
	r.steps = append(r.steps, smokeStep{Name: name, Value: value})
	fmt.Printf("[teammate-smoke] %-28s %s\n", name, value)
}

func (r *smokeReport) text() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	lines := make([]string, 0, len(r.steps))
	for _, step := range r.steps {
		lines = append(lines, step.Name+": "+step.Value)
	}
	return strings.Join(lines, "\n")
}

// teamworkSmokeProvider 是脚本化 provider：**按请求内容分辨"谁的回合"**。
//
// 为什么不按严格顺序：一条链里 leader 回合与 worker 回合交错（worker 是后台作业，它的回合
// 在另一个 goroutine 上跑），按序号排脚本会在并发下互相错位；按内容标记分辨才是稳定的。
type teamworkSmokeProvider struct {
	mu          sync.Mutex
	leaderCalls []scriptedResponse
	leaderTurns int
	workerTurns int
	// triggeredTurns 数"做完自动返回"起的回合（正文由 application 侧那条回执认出来）。
	triggeredTurns int
	seenLeader     []string
	// workerCalls 是**逐次 worker 回合**的脚本（同一次回合里的多轮补全各占一项：
	// 工具调用那一轮与"拿到工具结果之后"那一轮）。留空时退回既有的纯文本应答，
	// 既有冒烟的读数因此一个字不变。它存在的理由：要验"teammate 在自己的现场上真
	// 干活"就必须让 worker 回合**真的调工具**（写文件 + git commit），而不是只回正文。
	workerCalls []scriptedResponse
	// workers 是逐次 worker 回合的采样（含**本轮 system prompt 原文**）：装配的落点
	// （技能目录段有没有进员工的 system）只有在 wire 上才看得见。
	workers []workerSample
}

// workerSample 是一次 worker 回合的采样。SystemPrompt 是请求里第一条 system 消息的
// 正文——员工回合的装配面（装配进来的技能目录段）就落在这一条上。
type workerSample struct {
	Role         string
	SystemPrompt string
	LastUser     string
}

func (p *teamworkSmokeProvider) serve(t *testing.T, writer http.ResponseWriter, request *http.Request) {
	t.Helper()
	defer request.Body.Close()
	var payload struct {
		Stream   bool            `json:"stream"`
		Messages json.RawMessage `json:"messages"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	// **按解码后的正文辨认**，不要在原始 JSON 字符串上子串匹配：请求体里 `<` `>`
	// 会被写成 `\u003c` `\u003e`，拿 "<round_input>" 去匹配永远匹配不上——worker 回合
	// 因此会被误判成 leader 回合，把脚本里给 leader 的回应（验收、派下游）喂给 worker，
	// 冒烟的每一跳就都错位了（现场：P4 拿到的是第二个作业的 handle）。
	//
	// 判定只看**最后一条 user 消息**（本轮输入），不看整段会话：自动返回的回执作为一条
	// user 行留在会话里，按"整段里有没有"判会把**此后每一个回合**都认成自动返回回合，
	// 那些回合的脚本回应（验收、派下游）就永远发不出去。
	body, lastUser := requestBodyText(payload.Messages)
	// worker 回合的判据建在**语义**上：工作正文被包进 `<round_input>`，尾部是
	// 「以 <role> 的身份完成这一轮」那条 task 行（见 seelebridge 的 workerRoundInput）。
	// 不写死某个角色名：写死会在换角色时把 worker 回合误判成 leader 回合，
	// 脚本游标于是被 worker 吃掉，整条冒烟的每一跳都错位。
	workerTurn := strings.Contains(lastUser, "<round_input>") &&
		strings.Contains(lastUser, " 的身份完成这一轮")
	// 自动返回回合：正文是 application 侧那条回执（见 async_completion.go 的
	// teamworkCompletionPrompt）。它**不消费脚本游标**——验收交给冒烟自己驱动（P7）。
	// 否则验收会与 P5/P6 的观测抢跑：accept 一到，这件事的会话就被释放，P6 连
	// "读这件事自己的会话"的输入都没有了（现场就是这么红的）。
	triggeredTurn := strings.Contains(lastUser, "teammate 作业已完成")
	t.Logf("[provider] stream=%v worker=%v triggered=%v leaderIdx=%d body=%q",
		payload.Stream, workerTurn, triggeredTurn, p.leaderIndex(), truncateBodyForLog(body))

	p.mu.Lock()
	response := scriptedResponse{text: "收到"}
	switch {
	case workerTurn:
		p.workerTurns++
		p.workers = append(p.workers, workerSample{
			Role:         roleFromWorkerInput(lastUser),
			SystemPrompt: requestSystemPrompt(payload.Messages),
			LastUser:     lastUser,
		})
		if p.workerTurns <= len(p.workerCalls) {
			response = p.workerCalls[p.workerTurns-1]
		} else {
			response = scriptedResponse{text: "worker[exec]：这一轮做完了，结论见回执。"}
		}
	case triggeredTurn:
		p.triggeredTurns++
		response = scriptedResponse{text: "leader：收到回执，等我自己安排验收"}
	default:
		index := p.leaderTurns
		p.leaderTurns++
		p.seenLeader = append(p.seenLeader, body)
		if index < len(p.leaderCalls) {
			response = p.leaderCalls[index]
		} else {
			response = scriptedResponse{text: "leader：收到"}
		}
	}
	p.mu.Unlock()

	// 两条补全路都要服务：**leader 走主会话**（SSE，delta 形状），**worker 走框架会话
	// 的补全路**（`newRoleEngine` 用 framework session + agent completer，请求
	// `stream=false`，响应形状是 `choices[].message`）。只认 SSE 的话，worker 回合会被
	// 400 挡在门外——冒烟就永远跑不到"尾插回执 / 做完自动返回"那几步。
	if !payload.Stream {
		message := map[string]any{"role": "assistant"}
		if response.toolName != "" {
			callID := response.toolID
			if callID == "" {
				callID = fmt.Sprintf("call-%d", time.Now().UnixNano())
			}
			message["tool_calls"] = []any{map[string]any{
				"index": 0, "id": callID, "type": "function",
				"function": map[string]any{"name": response.toolName, "arguments": response.toolArgs},
			}}
		} else {
			message["content"] = response.text
		}
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(map[string]any{
			"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": "stop"}},
		}); err != nil {
			t.Errorf("encode non-stream completion: %v", err)
		}
		return
	}

	writer.Header().Set("Content-Type", "text/event-stream")
	flusher, ok := writer.(http.Flusher)
	if !ok {
		http.Error(writer, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	if response.toolName != "" {
		callID := response.toolID
		if callID == "" {
			callID = fmt.Sprintf("call-%d", time.Now().UnixNano())
		}
		writeSSE(t, writer, flusher, map[string]any{
			"choices": []any{map[string]any{
				"index": 0,
				"delta": map[string]any{
					"tool_calls": []any{map[string]any{
						"index": 0, "id": callID, "type": "function",
						"function": map[string]any{"name": response.toolName, "arguments": response.toolArgs},
					}},
				},
				"finish_reason": nil,
			}},
		})
		fmt.Fprint(writer, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}
	writeSSE(t, writer, flusher, map[string]any{
		"choices": []any{map[string]any{
			"index": 0, "delta": map[string]any{"content": response.text}, "finish_reason": "stop",
		}},
	})
	fmt.Fprint(writer, "data: [DONE]\n\n")
	flusher.Flush()
}

// requestBodyText 把请求体的 messages 解成一段可判定的正文（角色 + 文本）+ **最后一条
// user 消息**（本轮输入）。
//
// 为什么必须解码而不是直接用 json.RawMessage 的字符串：`<` `>` `&` 在 wire 上被转义成
// `\u003c` 这类形式，未解码的正文里没有 "<round_input>" 这个字面量。**判定要建在语义上，
// 不要建在 wire 的转义形式上**——转义形式是传输细节，随编码器变。
//
// 为什么要单独给"本轮输入"：整段会话里可能**留着**上一次的输入（比如自动返回的回执就是一条
// user 行），按整段判会把后续回合也认成那一类。
func requestBodyText(raw json.RawMessage) (body string, lastUser string) {
	var messages []struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	}
	if err := json.Unmarshal(raw, &messages); err != nil {
		return string(raw), ""
	}
	var joined strings.Builder
	for _, message := range messages {
		text := messageContentText(message.Content)
		joined.WriteString(message.Role)
		joined.WriteString(":")
		joined.WriteString(text)
		joined.WriteString("\n")
		if message.Role == "user" && strings.TrimSpace(text) != "" {
			lastUser = text
		}
	}
	return joined.String(), lastUser
}

// messageContentText 取一条消息的文本（content 可能是字符串，也可能是分片数组）。
func messageContentText(content any) string {
	switch value := content.(type) {
	case string:
		return value
	case []any:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			if object, ok := item.(map[string]any); ok {
				if text, ok := object["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

// requestSystemPrompt 取请求里**第一条 system 消息**的正文：员工回合的装配面（装配
// 进来的技能目录段与那一行口径纠正）就落在这一条上，因此它是"装配有没有落到真回合"
// 唯一可核的那一面。
func requestSystemPrompt(raw json.RawMessage) string {
	var messages []struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	}
	if err := json.Unmarshal(raw, &messages); err != nil {
		return ""
	}
	for _, message := range messages {
		if message.Role == "system" {
			return messageContentText(message.Content)
		}
	}
	return ""
}

// roleFromWorkerInput 从 worker 的工作正文里取角色名（尾部 task 行
// 「以 <role> 的身份完成这一轮」；组装它的地方是 seelebridge 的 workerRoundInput）。
// 取不到就返回空串——那说明这一轮的输入形状变了，样本按"不知道是谁"记下来。
func roleFromWorkerInput(lastUser string) string {
	const prefix = "以 "
	const suffix = " 的身份完成这一轮"
	start := strings.LastIndex(lastUser, prefix)
	if start < 0 {
		return ""
	}
	rest := lastUser[start+len(prefix):]
	end := strings.Index(rest, suffix)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}

// workerSamples 是逐次 worker 回合的采样（装配落点的取证面）。
func (p *teamworkSmokeProvider) workerSamples() []workerSample {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]workerSample(nil), p.workers...)
}

func (p *teamworkSmokeProvider) counts() (leader, worker int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.leaderTurns, p.workerTurns
}

// triggeredCount 是"做完自动返回"起的回合数（脱锁读，供汇总用）。
func (p *teamworkSmokeProvider) triggeredCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.triggeredTurns
}

func (p *teamworkSmokeProvider) leaderIndex() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.leaderTurns
}

// truncateBodyForLog 只截请求体前一段进日志（诊断用；正文可能很长）。
func truncateBodyForLog(body string) string {
	runes := []rune(body)
	if len(runes) > 240 {
		return string(runes[:240]) + "…"
	}
	return body
}

// teamworkSmoke 是一次冒烟的现场：真实装配 + 报告。
type teamworkSmoke struct {
	harness  fullChainHarness
	app      *application.Service
	provider *teamworkSmokeProvider
	report   *smokeReport
	// planStore / keyFor 是**取证面**：探针要能把"落盘的计划事实"与"看板投影"分开读，
	// 否则"投影陈旧"会被误判成"尾插没落"（反过来也一样）。
	planStore seeteamwork.PlanStore
	keyFor    func(sessionID string) (sessionstore.Key, bool)
}

// newTeamworkSmoke 装配真实链路：真实 store / 工作区 / 引擎端口 / application，
// 并按组合根的方式注入 teamwork 编排面（main.go 的 SetTeamworkBackend 那一段）。
func newTeamworkSmoke(t *testing.T, provider *teamworkSmokeProvider) *teamworkSmoke {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provider.serve(t, w, r)
	}))
	t.Cleanup(server.Close)

	tempDir := t.TempDir()
	accountsPath := filepath.Join(tempDir, "accounts.yaml")
	accounts := "roles:\n  agent:\n    - model: test-model\n      base_url: " + server.URL +
		"\n      api_key: test-key\n"
	if err := os.WriteFile(accountsPath, []byte(accounts), 0o600); err != nil {
		t.Fatal(err)
	}
	// 后台作业终态触发回合（"做完自动返回"）必须开着：这是本轮要验的那条链。
	limits := seelexctx.Limits{}
	limits.AsyncExec.Enabled = true
	limits.AsyncExec.TriggerConversation = true
	// 但开关的**读侧**读的是进程内那份生效 limits（`core.Limits()`，由组合根
	// `core.ApplyLimits` 注入，见 main.go 的装配段），不是 RuntimeConfig 里那一份。
	// 少了这一步，消费者会按默认（关）跳过整条扫描——"做完自动返回"在冒烟里永远是关的，
	// 测试就永远验不到它（假绿：什么都没发生也叫通过）。
	previousLimits := core.Limits()
	core.ApplyLimits(limits)
	t.Cleanup(func() { core.ApplyLimits(previousLimits) })

	// teamwork 编排面：与 main.go 同一段装配（PlanStore + Boards + JobOutputs + KeyFor），
	// 而且**同一个位置**——在 application.New 之前。位置是契约的一部分：应用的
	// 生命周期消费者在 application.New 里就把信号口读走，晚注入 ⇒ 它读到 nil 通道 ⇒
	// 「做完自动返回」静默消失（见 fullChainBackendInstaller 的注释）。
	var planStoreForProbe seeteamwork.PlanStore
	var keyForForProbe func(string) (sessionstore.Key, bool)
	harness := newFullChainHarnessWithLimits(t, accountsPath, tempDir, 60*time.Second, true, limits,
		func(store *sessionstore.Router, workspaces *workspacerepo.Repo, runtime *seelebridge.Runtime) error {
			repo, ok := store.TeamworkFor()
			if !ok {
				t.Fatal("真实 JSON 后端应提供 teamwork 读/写面")
			}
			boards, _ := store.BoardsFor()
			keyFor := func(sessionID string) (sessionstore.Key, bool) {
				workspace, exists := workspaces.SessionWorkspace(sessionID)
				if !exists || strings.TrimSpace(workspace.ID) == "" {
					return sessionstore.Key{}, false
				}
				return sessionstore.Key{ProjectID: workspace.ID, SessionID: sessionID}, true
			}
			planStore := seeteamwork.NewPlanStore(repo)
			planStoreForProbe, keyForForProbe = planStore, keyFor
			return runtime.SetTeamworkBackend(seelebridge.TeamworkBackend{
				Store:        planStore,
				Boards:       boards,
				JobOutputs:   seelebridge.NewTeamworkJobOutputs(repo, keyFor),
				KeyFor:       keyFor,
				MaxTeammates: 3,
			})
		})
	smoke := &teamworkSmoke{
		harness: harness, app: harness.app, provider: provider, report: &smokeReport{},
		planStore: planStoreForProbe, keyFor: keyForForProbe,
	}
	return smoke
}

// persistedItem 读**落盘**的计划里这一项（取证用：与看板投影分开读）。
func (s *teamworkSmoke) persistedItem(ctx context.Context, itemID string) (sessionstore.TeamworkWorkItem, bool) {
	if s.planStore == nil || s.keyFor == nil {
		return sessionstore.TeamworkWorkItem{}, false
	}
	key, ok := s.keyFor(s.app.Snapshot().Session.ID)
	if !ok {
		return sessionstore.TeamworkWorkItem{}, false
	}
	plan, err := s.planStore.ReadPlan(ctx, key)
	if err != nil {
		return sessionstore.TeamworkWorkItem{}, false
	}
	for _, milestone := range plan.Milestones {
		for _, item := range milestone.Items {
			if item.ID == itemID {
				return item, true
			}
		}
	}
	return sessionstore.TeamworkWorkItem{}, false
}

// startProjectSession 建一个绑定到项目工作区的会话（KeyFor 才有解）。
func (s *teamworkSmoke) startProjectSession(t *testing.T, ctx context.Context) string {
	t.Helper()
	root := t.TempDir()
	if err := s.app.CreateWorkspace("smoke-project", root, ""); err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	if err := s.app.Submit(ctx, "开一个团队，把两件事排进契约里程碑"); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if err := s.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("WaitForIdle: %v", err)
	}
	sessionID := s.app.Snapshot().Session.ID
	if sessionID == "" {
		t.Fatal("会话没有物化")
	}
	s.report.add("P0 会话与项目", "session=%s root=%s", sessionID, root)
	return sessionID
}

// waitBoard 轮询看板直到条件满足（真实链路里投影是异步刷新的事件驱动）。
func (s *teamworkSmoke) waitBoard(t *testing.T, sessionID string, ready func(*dto.TeamworkBoardView) bool) *dto.TeamworkBoardView {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if board := s.harness.runtime.TeamworkBoardSnapshot(sessionID); board != nil && ready(board) {
			return board
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("看板条件在窗口内没满足；最后一次投影=%+v\n可见会话逐行：\n%s",
		s.harness.runtime.TeamworkBoardSnapshot(sessionID), s.conversationDump())
	return nil
}

// waitJobTerminal 轮询 teammate 作业表直到该句柄终态（或超时）。
func (s *teamworkSmoke) waitJobTerminal(t *testing.T, handle string) dto.TeamworkJobCompletionRecord {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		for _, record := range s.harness.runtime.TeamworkJobCompletions() {
			if record.Handle == handle && record.State != dto.AsyncStateRunning {
				return record
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("teammate 作业 %s 没有在窗口内终态：%+v", handle, s.harness.runtime.TeamworkJobCompletions())
	return dto.TeamworkJobCompletionRecord{}
}

// waitUserRow 轮询可见会话直到出现含 needle 的用户行（自动返回的证据）。
func (s *teamworkSmoke) waitUserRow(t *testing.T, needle string) string {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		for _, message := range s.app.Snapshot().Conversation {
			if message.Role == "user" && strings.Contains(message.Content, needle) {
				return message.Content
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("可见会话里没等到含 %q 的用户行", needle)
	return ""
}

// waitToolResult 轮询可见会话直到**某个工具**的结果含 needle（回合是异步跑的，断言要给窗口：
// 直接读"最后一次结果"会在两条链竞速时读到上一条）。
func (s *teamworkSmoke) waitToolResult(t *testing.T, tool, needle string) string {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		for _, message := range s.app.Snapshot().Conversation {
			if message.Role != "tool" || message.Tool == nil || message.Tool.Name != tool {
				continue
			}
			if strings.Contains(message.Tool.Result, needle) {
				return message.Tool.Result
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("可见会话里没等到 %s 的结果含 %q", tool, needle)
	return ""
}

// toolStatuses 按顺序取某个工具每次调用的状态（success / error）。
func (s *teamworkSmoke) toolStatuses(name string) []string {
	statuses := []string{}
	for _, message := range s.app.Snapshot().Conversation {
		// 一次工具调用在可见会话里有两行（tool 行 + tool_result 行）：只数 tool 行，
		// 否则每次调用都被数两遍（状态位也就错位了）。
		if message.Role != "tool" || message.Tool == nil || message.Tool.Name != name {
			continue
		}
		statuses = append(statuses, message.Tool.Status.String())
	}
	return statuses
}

// firstLineOf 取文本首行（探针输出要短）。
func firstLineOf(text string) string {
	text = strings.TrimSpace(text)
	if index := strings.Index(text, "\n"); index >= 0 {
		return text[:index]
	}
	return text
}

// conversationDump 把可见会话逐行摊开（探针失败时的取证：哪一行是谁、工具名与正文）。
func (s *teamworkSmoke) conversationDump() string {
	lines := []string{}
	for index, message := range s.app.Snapshot().Conversation {
		tool := ""
		if message.Tool != nil {
			tool = fmt.Sprintf("tool=%s status=%s result=%q", message.Tool.Name, message.Tool.Status, message.Tool.Result)
		}
		lines = append(lines, fmt.Sprintf("#%d role=%s %s content=%q", index, message.Role, tool, message.Content))
	}
	return strings.Join(lines, "\n")
}

// toolResultsOf 从可见会话里按顺序取某个工具的每次结果文本（探针用；同名工具可能被调用
// 多次，取"最后一次"会把负路径的证据覆盖掉——本轮冒烟里 team_work 正是这种情形）。
func (s *teamworkSmoke) toolResultsOf(name string) []string {
	results := []string{}
	for _, message := range s.app.Snapshot().Conversation {
		if message.Role != "tool" || message.Tool == nil || message.Tool.Name != name {
			continue
		}
		// 成功走 Tool.Result，失败走可见正文（工具错误的原文在那里）——两路都要收，
		// 否则"负路径被拒"这件事在探针里看不见。
		text := strings.TrimSpace(message.Tool.Result)
		if text == "" {
			text = strings.TrimSpace(message.Content)
		}
		if text != "" {
			results = append(results, text)
		}
	}
	return results
}

// toolResultOf 取某个工具最后一次的结果文本。
func (s *teamworkSmoke) toolResultOf(name string) string {
	results := s.toolResultsOf(name)
	if len(results) == 0 {
		return ""
	}
	return results[len(results)-1]
}

// toolResultMatching 取某个工具结果里含 needle 的那一次（负路径取证用）。
func (s *teamworkSmoke) toolResultMatching(name, needle string) string {
	for _, result := range s.toolResultsOf(name) {
		if strings.Contains(result, needle) {
			return result
		}
	}
	return ""
}

// initSmokeGitRepo 造一个临时 git 仓库（冒烟要跑**真** worktree：一 Work Item 一套现场
// 是本轮的硬要求，降级到共享主工作区就验不到"隔离 + 回收"那一跳）。
func initSmokeGitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "smoke@seelex.local"},
		{"config", "user.name", "seelex smoke"},
	} {
		if err := runGit(root, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runGit(root, "add", "."); err != nil {
		t.Fatal(err)
	}
	if err := runGit(root, "commit", "-m", "base"); err != nil {
		t.Fatal(err)
	}
	return root
}

func runGit(root string, args ...string) error {
	command := exec.Command("git", args...)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %v: %v (%s)", args, err, strings.TrimSpace(string(output)))
	}
	return nil
}

// TestTeamworkHeadlessSmoke 是这一轮的**逐阶段冒烟**（阶段错位只会在真实装配上显形）。
//
// 阶段与探针（每一步都留痕，失败时能指出是哪一跳断的）：
//
//	P0 会话与项目工作区       —— KeyFor 有解（看板/作业的会话作用域）
//	P1 编排面装配             —— team_* 工具在面 + teammates 作业信号口在
//	P2 计划（里程碑屏障）      —— plan 落盘、看板投影里里程碑 2 条、**没有 stages 这一格**
//	P3 排活 + 屏障负路径       —— m-docs 被拒（依赖的里程碑没完成）→ m-contract 排活成功
//	P4 派发 + 尾插            —— 受理回执拿 handle → 作业终态 → 工作项 review + 尾插回执
//	P5 做完自动返回            —— 空闲会话被起一个回合（正文指向 team_context / team_accept）
//	P6 当前 teammate 会话      —— 这件事自己的会话读得到（不是员工的长期历史会话）
//	P6b 实时读数的 wire 形状    —— 应用层那一面（GUI 经 Wails 拿的 JSON）带前端要读的键
//	P7 验收收口               —— accept 通过（幂等释放）+ 下游依赖闸门打开（wi-check 可派）
func TestTeamworkHeadlessSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("真实装配冒烟，short 模式跳过")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	provider := &teamworkSmokeProvider{leaderCalls: []scriptedResponse{
		// P2：计划 = 两名在编 + 两道里程碑屏障（m-contract → m-docs）。
		{toolName: "team_plan", toolArgs: `{"team_id":"smoke-team","members":[{"role":"exec","role_session_id":"smoke-exec"},{"role":"verify","role_session_id":"smoke-verify","tools_policy":"readonly"}],"milestones":[{"id":"m-contract","name":"契约"},{"id":"m-docs","name":"文档","depends_on":["m-contract"]}]}`},
		// P3-a：给开着的里程碑排活（DAG：wi-check 依赖 wi-impl）。
		{toolName: "team_work", toolArgs: `{"milestone":"m-contract","items":[{"id":"wi-impl","role":"exec","name":"实现","goal":"把契约落成代码"},{"id":"wi-check","role":"verify","name":"复核","depends_on":["wi-impl"]}]}`},
		// P3-b：给屏障后面的里程碑排活必须被拒（判据 = 依赖的里程碑**排过活**且没 done）。
		{toolName: "team_work", toolArgs: `{"milestone":"m-docs","items":[{"id":"wi-doc","role":"verify","name":"文档"}]}`},
		// P4：派发这件事（受理回执即返回）。
		{toolName: "team_dispatch", toolArgs: `{"item":"wi-impl","goal":"把契约落成代码"}`},
		{text: "leader：已派发，等回执"},
		// P5：做完自动返回由 application 侧起一个回合；脚本对那个回合只回正文
		// （provider 的 triggeredTurn 分支**不消费**这里的条目，测试才能自己选时机验收）。
		// P7：验收收口 → 下游 wi-check（依赖 wi-impl）可派。
		{toolName: "team_accept", toolArgs: `{"id":"wi-impl"}`},
		// P7：验收后下游工作项（依赖 wi-impl）必须可派。
		{toolName: "team_dispatch", toolArgs: `{"item":"wi-check","goal":"复核实现"}`},
		{text: "leader：已收口"},
	}}
	smoke := newTeamworkSmoke(t, provider)

	// ── P1：编排面装配 ────────────────────────────────────────────────
	visible := map[string]bool{}
	for _, tool := range smoke.harness.runtime.VisibleTools(ctx) {
		visible[tool.Name] = true
	}
	for _, name := range []string{"team_plan", "team_work", "team_dispatch", "team_accept", "team_fail", "team_items", "team_context", "jobs_manage"} {
		if !visible[name] {
			t.Fatalf("P1 断言失败：注入 teamwork 后工具面缺少 %s（可见工具：%v）", name, visible)
		}
	}
	if smoke.harness.runtime.TeamworkJobEvents() == nil {
		t.Fatal("P1 断言失败：teammate 作业信号口缺失（做完自动返回这条链永远不醒）")
	}
	smoke.report.add("P1 编排面装配", "team_* 9 件在面 · teammate 作业信号口在")

	// ── P0：会话 + 项目工作区（必须是真 git 仓库：worktree 跳才验得到）────────
	root := initSmokeGitRepo(t)
	if err := smoke.app.CreateWorkspace("smoke-project", root, ""); err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	if err := smoke.app.Submit(ctx, "开一个团队，把两件事排进契约里程碑"); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if err := smoke.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("WaitForIdle: %v", err)
	}
	sessionID := smoke.app.Snapshot().Session.ID
	smoke.report.add("P0 会话与项目", "session=%s git-root=%s", sessionID, root)

	// ── P2：计划 + 看板投影 ──────────────────────────────────────────
	board := smoke.waitBoard(t, sessionID, func(board *dto.TeamworkBoardView) bool {
		return len(board.Milestones) == 2 && len(board.WorkItems) >= 2
	})
	if board.TeamID != "smoke-team" {
		t.Fatalf("P2 断言失败：看板 team_id=%q", board.TeamID)
	}
	if len(board.Milestones) != 2 || board.Milestones[1].DependsOn[0] != "m-contract" {
		t.Fatalf("P2 断言失败：里程碑屏障没搬对：%+v", board.Milestones)
	}
	encoded, err := json.Marshal(board)
	if err != nil {
		t.Fatalf("P2 编码看板：%v", err)
	}
	if strings.Contains(string(encoded), `"stages"`) {
		t.Fatal("P2 断言失败：投影里仍有 stages（阶段口径没有整条退场）")
	}
	smoke.report.add("P2 计划与看板", "milestones=2 屏障=%v work_items=2 无 stages 键",
		board.Milestones[1].DependsOn)

	// ── P3：负路径（屏障）+ 正路径（排活）─────────────────────────────
	// 负路径：给屏障后面的 m-docs 排活那一次必须以**错误**收场（第二次 team_work 调用）。
	statuses := smoke.toolStatuses("team_work")
	if len(statuses) < 2 {
		t.Fatalf("P3 断言失败：team_work 只被调用 %d 次（脚本是两次）\n会话逐行：\n%s", len(statuses), smoke.conversationDump())
	}
	if statuses[0] != "success" || statuses[1] != "error" {
		t.Fatalf("P3 断言失败：期望 [开着的里程碑=success, 屏障后面的=error]，得到 %v\n会话逐行：\n%s",
			statuses, smoke.conversationDump())
	}
	// 正路径：给开着的 m-contract 排活那一次必须成功（同一次调用的回执）。
	accepted := smoke.toolResultMatching("team_work", "m-contract")
	if !strings.Contains(accepted, `"ok":true`) {
		t.Fatalf("P3 断言失败：开着的里程碑排活没成功：%q", accepted)
	}
	// 探针顺带记录一个**呈现面**事实：拒绝原因被通用文案替换了（模型与用户都看不到具体
	// 原因），这条不进断言，只留痕——它是下一步的输入（根因可见性）。
	rejectionShown := smoke.toolResultMatching("team_work", "依赖的里程碑")
	smoke.report.add("P3 屏障负路径", "调用状态=%v · 拒绝原文可见=%v（呈现面=%q）",
		statuses, rejectionShown != "", firstLineOf(rejectionShown))
	// 被拒的那件事**没有**落地：看板里只该有 m-contract 的两条（拒绝是有效拒收，
	// 不是"记了一笔但没排"）。
	board = smoke.waitBoard(t, sessionID, func(board *dto.TeamworkBoardView) bool {
		for _, item := range board.WorkItems {
			if item.ID == "wi-doc" {
				return false
			}
		}
		return len(board.WorkItems) == 2
	})
	for _, item := range board.WorkItems {
		if item.Milestone != "m-contract" {
			t.Fatalf("P3 断言失败：被拒的排活留下了痕迹：%+v", item)
		}
	}
	smoke.report.add("P3 排活", "work_items=2（全在 m-contract）· exec 队列=%v", board.Members[0].Queue)

	// ── P4：派发 + worker 作业终态 + 尾插回执 ────────────────────────
	dispatchReceipt := smoke.toolResultOf("team_dispatch")
	if !strings.Contains(dispatchReceipt, `"handle"`) || !strings.Contains(dispatchReceipt, "不等待") {
		t.Fatalf("P4 断言失败：派发受理回执不含 handle / 不等待口径：%q", dispatchReceipt)
	}
	var receipt struct {
		Handle string `json:"handle"`
	}
	if err := json.Unmarshal([]byte(dispatchReceipt), &receipt); err != nil {
		// 工具结果可能带前缀文本：退一步抓 handle 字面量。
		index := strings.Index(dispatchReceipt, `"handle":"`)
		if index < 0 {
			t.Fatalf("P4 断言失败：拿不到 handle：%q", dispatchReceipt)
		}
		rest := dispatchReceipt[index+len(`"handle":"`):]
		receipt.Handle = rest[:strings.Index(rest, `"`)]
	}
	if receipt.Handle == "" {
		t.Fatalf("P4 断言失败：handle 为空：%q", dispatchReceipt)
	}
	job := smoke.waitJobTerminal(t, receipt.Handle)
	smoke.report.add("P4 teammate 作业", "handle=%s state=%s exit=%d 归属=%s/%s summary=%q",
		job.Handle, job.State, job.ExitCode, job.Role, job.WorkItem, job.Summary)
	// 落盘事实与投影分开读：尾插的**状态收敛**写在计划里，回执写在 teammate 消息队列，
	// 看板只是两者的投影。三条读数不一致时，这份探针能指出是哪一条没落。
	if settled, ok := smoke.persistedItem(ctx, "wi-impl"); ok {
		smoke.report.add("P4 落盘事实", "wi-impl 状态=%s 句柄=%s note=%q",
			settled.StatusOrPending(), settled.Handle, firstLineOf(settled.Note))
	}

	board = smoke.waitBoard(t, sessionID, func(board *dto.TeamworkBoardView) bool {
		for _, member := range board.Members {
			if member.Role == "exec" && len(member.Messages) > 0 {
				return true
			}
		}
		return false
	})
	exec := board.Members[0]
	if exec.Status != "running" && exec.Status != "free" {
		t.Fatalf("P4 断言失败：teammate 状态只允许 running/free，得到 %q", exec.Status)
	}
	smoke.report.add("P4 尾插回执", "teammate=%s 状态=%s 回执=%q 工作项状态=%s",
		exec.Role, exec.Status, exec.Messages[len(exec.Messages)-1].Text, board.WorkItems[0].Status)

	// ── P5：做完自动返回（空闲会话被起一个回合）───────────────────────
	body := smoke.waitUserRow(t, "teammate 作业已完成")
	if !strings.Contains(body, "team_context") || !strings.Contains(body, "team_accept / team_fail") {
		t.Fatalf("P5 断言失败：自动返回的正文没给出收口姿势：%q", body)
	}
	if strings.Contains(body, "job_manage(op=fetch") {
		t.Fatalf("P5 断言失败：正文指错了取回工具（teammate 作业在 jobs_manage 一侧）：%q", body)
	}
	smoke.report.add("P5 自动返回", "空闲会话被起回合；正文含 team_context/team_accept，未指向 job_manage")
	// 这一回合跑完再往下看：P6 读的是"这件事的会话**此刻**在说什么"，回合还在飞的时候
	// 读数会随执行面变化（而且要等它落定，才谈得上"验收把它释放掉"）。
	if err := smoke.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("P5 WaitForIdle: %v", err)
	}

	// ── P6：当前 teammate 会话（这件事自己的会话，不是员工的历史会话）──
	itemSession := board.WorkItems[0].SessionID
	if itemSession == "" || itemSession == exec.RoleSessionID {
		t.Fatalf("P6 断言失败：工作项会话号缺失或退回了角色会话：item=%q role=%q\n可见会话逐行：\n%s",
			itemSession, exec.RoleSessionID, smoke.conversationDump())
	}
	live := smoke.harness.runtime.TeammateSessionLive(itemSession)
	if !live.Running || live.Role != "exec" {
		t.Fatalf("P6 断言失败：这件事的会话读不到执行面：%+v", live)
	}
	found := false
	for _, message := range live.Messages {
		if strings.Contains(message.Text, "契约落成代码") {
			found = true
		}
	}
	if !found {
		t.Fatalf("P6 断言失败：实时读数里没有这一轮的工作正文：%+v", live.Messages)
	}
	smoke.report.add("P6 当前 teammate 会话", "session=%s role=%s 行数=%d（含本轮工作正文）",
		live.SessionID, live.Role, len(live.Messages))

	// ── P6b：**和前端同形的那一跳**（Wails 拿的是 JSON，不是 Go 结构体）──────
	// GUI 的「查看 teammate 的会话」经 `Service.TeammateSessionLiveFor`（窄端口）拿读数，
	// 再按 snake_case 键读它（`renderTeammateLiveSession`）。少了这一跳，Go 侧全绿而前端
	// 读到的是一份"全是 undefined"的对象——2026-10-04 现场：这条读面在 GUI 上永远走
	// "执行面不在本进程"的分支（字段缺 json tag ⇒ wire 上是 Go 字段名）。
	// 探针取的是**应用层**那一面（不是 runtime 的直接调用），所以窄端口漏转发也会在这里红。
	liveWire, err := json.Marshal(smoke.app.TeammateSessionLiveFor(itemSession))
	if err != nil {
		t.Fatalf("P6b 编码实时读数：%v", err)
	}
	for _, key := range []string{`"session_id"`, `"role"`, `"live"`, `"running"`, `"messages"`} {
		if !strings.Contains(string(liveWire), key) {
			t.Fatalf("P6b 断言失败：实时读数的 JSON 里没有前端要读的键 %s：%s", key, liveWire)
		}
	}
	if !strings.Contains(string(liveWire), `"running":true`) {
		t.Fatalf("P6b 断言失败：这件事的执行面还在，running 必须是 true：%s", liveWire)
	}
	smoke.report.add("P6b 实时读数的 wire 形状", "应用层读数（%d 字节）带前端要读的 snake_case 键", len(liveWire))

	// ── P7：验收收口 + 下游闸门 ──────────────────────────────────────
	// 验收由冒烟**自己驱动**：自动返回那一回合只回正文（见 provider 的 triggeredTurn 分支），
	// 于是 P6 读到的是还没被释放的"这件事自己的会话"。这里再推一个回合做验收。
	if err := smoke.app.Submit(ctx, "验收 wi-impl，然后派下游的复核"); err != nil {
		t.Fatalf("P7 Submit: %v", err)
	}
	if err := smoke.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("P7 WaitForIdle: %v", err)
	}
	acceptReceipt := smoke.waitToolResult(t, "team_accept", `"ok":true`)
	if !strings.Contains(acceptReceipt, `"ok":true`) {
		t.Fatalf("P7 断言失败：验收没有成功：%q", acceptReceipt)
	}
	board = smoke.waitBoard(t, sessionID, func(board *dto.TeamworkBoardView) bool {
		for _, item := range board.WorkItems {
			if item.ID == "wi-impl" {
				return item.Status == "done"
			}
		}
		return false
	})
	secondDispatch := smoke.waitToolResult(t, "team_dispatch", `"item":"wi-check"`)
	if !strings.Contains(secondDispatch, `"item":"wi-check"`) {
		t.Fatalf("P7 断言失败：下游工作项没有拿到受理回执（闸门没开）：%q", secondDispatch)
	}
	smoke.report.add("P7 验收与闸门", "accept ok · wi-impl=done · 下游 wi-check 受理回执=%q",
		strings.TrimSpace(secondDispatch))

	leaderTurns, workerTurns := provider.counts()
	smoke.report.add("汇总", "leader 回合=%d（含自动返回 %d）· worker 回合=%d",
		leaderTurns, provider.triggeredCount(), workerTurns)
	t.Log("\n===== teammate headless smoke =====\n" + smoke.report.text())
}
