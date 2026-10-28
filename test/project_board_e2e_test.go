package test

import (
	"context"
	"encoding/json"
	"fmt"
	boot "github.com/RedHuang-0622/seelex/boot"
	"github.com/RedHuang-0622/seelex/kernel/limit"

	// project_board_e2e_test.go — M6 端到端：模型**真的**通过工具面把投影读回来。
	//
	// 这条用例走的链子：模型发起 project_board_read → 框架分发 → 权限面放行（ro 簇）→
	// handler（boot.RegisterProjectBoardReadTool）→ 存储面折叠读 → 投影行回到模型可见的
	// 工具结果里。它证明的是"读面端到端可用"，而不是只证明某个 Go 函数返回值正确。
	//
	// 写侧为什么不在这一条里（分段各有更准的用例，硬凑只会得到假绿）：
	//   - 「工具完成段 → 订阅口」：application/core 的 TestProjectBoardHooksOnExistingSyncPoint
	//     （真实完成段 + 记录替身，钉住转发的内容与会话归属）；
	//   - 「订阅口 → 真实存储面落盘」：本包 TestProjectBoardEnabledWritesArtifacts
	//     （真实 store.Router，断言 <项目根>/.seelex/board/ 下真的出现两份 jsonl）。
	//
	// 写侧的活体取数要求会话在**活体工作表格**里真有行；全链路 harness 的 task 注册表分区
	// 口径与生产不同（同一次 todo 落在会话分区，视图分区为空），现场造不出来——把写侧搬进
	// 这条用例只会得到"因为现场不存在所以什么都没发生"的假绿。故写侧分开钉，见上。
	"github.com/RedHuang-0622/seelex/contract/dto"
	storeboardrouter "github.com/RedHuang-0622/seelex/internal/host/boardrouter"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const projectBoardE2EItem = "看板端到端标记 9f3a1c"

// projectBoardE2EScript 是一次 provider 调用的脚本：给了 tool 就宣告工具调用，
// 否则给一句终答（与全链路基座的 mock provider 同形，只是调用可配置）。
type projectBoardE2EScript struct {
	tool string
	args string
}

type projectBoardE2EServer struct {
	*httptest.Server

	mu     sync.Mutex
	bodies [][]byte
	script []projectBoardE2EScript
}

func newProjectBoardE2EServer(t *testing.T) *projectBoardE2EServer {
	t.Helper()
	server := &projectBoardE2EServer{}
	server.Server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		server.serve(t, writer, request)
	}))
	t.Cleanup(server.Close)
	return server
}

// setScript 覆盖后续调用脚本（测试在"回合之间"改脚本）。
func (server *projectBoardE2EServer) setScript(script ...projectBoardE2EScript) {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.script = script
}

func (server *projectBoardE2EServer) recorded() [][]byte {
	server.mu.Lock()
	defer server.mu.Unlock()
	out := make([][]byte, len(server.bodies))
	copy(out, server.bodies)
	return out
}

func (server *projectBoardE2EServer) serve(t *testing.T, writer http.ResponseWriter, request *http.Request) {
	t.Helper()
	defer request.Body.Close()
	body, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	server.mu.Lock()
	server.bodies = append(server.bodies, body)
	call := len(server.bodies)
	var script projectBoardE2EScript
	if call-1 < len(server.script) {
		script = server.script[call-1]
	}
	server.mu.Unlock()

	writer.Header().Set("Content-Type", "text/event-stream")
	flusher, ok := writer.(http.Flusher)
	if !ok {
		http.Error(writer, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	if script.tool != "" {
		writeSSE(t, writer, flusher, map[string]any{
			"choices": []any{map[string]any{
				"index": 0,
				"delta": map[string]any{
					"tool_calls": []any{map[string]any{
						"index": 0,
						"id":    fmt.Sprintf("call_%d", call),
						"type":  "function",
						"function": map[string]any{
							"name":      script.tool,
							"arguments": script.argumentsOrDefault(),
						},
					}},
				},
				"finish_reason": "tool_calls",
			}},
		})
	} else {
		writeSSE(t, writer, flusher, map[string]any{
			"choices": []any{map[string]any{
				"index":         0,
				"delta":         map[string]any{"content": "好。"},
				"finish_reason": "",
			}},
		})
		writeSSE(t, writer, flusher, map[string]any{
			"choices": []any{map[string]any{
				"index": 0, "delta": map[string]any{}, "finish_reason": "stop",
			}},
		})
	}
	fmt.Fprint(writer, "data: [DONE]\n\n")
	flusher.Flush()
}

func (script projectBoardE2EScript) argumentsOrDefault() string {
	if strings.TrimSpace(script.args) != "" {
		return script.args
	}
	return "{}"
}

// projectBoardE2EReadArgs 组 project_board_read 的入参（指定会话与文件）。
func projectBoardE2EReadArgs(sessionID, file string) string {
	encoded, err := json.Marshal(map[string]string{"session": sessionID, "file": file})
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func TestProjectBoardEndToEndThroughToolFace(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	projectRoot := t.TempDir()
	provider := newProjectBoardE2EServer(t)
	AccountsPath := projectBoardAccountsFor(t, provider.URL, projectRoot)

	limits := limit.Limits{}
	limits.ProjectBoard.Enabled = true
	harness := newFullChainHarnessWithLimits(t, AccountsPath, projectRoot, 30*time.Second, true, limits)
	// 落点是**会话所属工作区的项目根**（`<项目根>/.seelex/board/`）：生产里由工作区绑定给出，
	// 所以这里也先把这个会话绑到一个真实工作区上——不绑就没有落点，读面读不到东西。
	viewSession := harness.components.Session.Snapshot.Snapshot().Session.ID
	workspace, err := harness.workspaces.Create("board-e2e", projectRoot, "")
	if err != nil {
		t.Fatalf("建工作区: %v", err)
	}
	harness.workspaces.BindSession(viewSession, workspace.ID)
	// 按生产装配挂读取面（本用例要打的正是这把工具）。
	boot.RegisterProjectBoardReadTool(harness.runtime, storeboardrouter.New(), harness.app)

	// 落一份**真**投影（走存储面，不是内存替身）：读面要读的就是它。
	//
	// 行归属**另一个**会话（session-e2e），落点仍是本项目的同一份平铺文件——"一个项目一份
	// worktable.jsonl、多会话共用、行内 scope.session 区分"正是本轮的口径；读面按会话过滤。
	const sessionID = "session-e2e"
	projectRootForSession := harness.components.Execution.ProjectBoard.ProjectRootFor(viewSession)
	if strings.TrimSpace(projectRootForSession) == "" {
		t.Fatal("会话没有工作区绑定（没有落点）：本用例会退化成同义反复")
	}
	if _, err := storeboardrouter.New().AppendProjectBoardRows(projectRootForSession, sessionID, dto.ProjectBoardWorkFile,
		[]dto.ProjectBoardRow{{
			Schema: dto.ProjectBoardSchema, Kind: dto.ProjectBoardKindWork,
			Key: "work:todo:0", Seq: 1, At: time.Now().Unix(),
			Scope:   dto.ProjectBoardScope{Session: sessionID},
			Payload: map[string]any{"task": projectBoardE2EItem, "kind": "todo", "status": "doing"},
		}}); err != nil {
		t.Fatalf("写投影: %v", err)
	}
	if artifacts := projectBoardArtifacts(t, projectRoot); !containsArtifact(artifacts, dto.ProjectBoardWorkFile) {
		t.Fatalf("投影没有落在项目根的 .seelex/board 之下：%v", artifacts)
	}

	provider.setScript(
		projectBoardE2EScript{tool: "project_board_read", args: projectBoardE2EReadArgs(sessionID, "worktable")},
		projectBoardE2EScript{},
	)
	if err := harness.components.Conversation.Submission.Submit(ctx, "请用 project_board_read 读一下这个会话的看板，然后只回复『好』。"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if err := harness.components.Runtime.Lifecycle.WaitForIdle(ctx); err != nil {
		t.Fatalf("turn did not become idle: %v", err)
	}
	if snapshot := harness.components.Session.Snapshot.Snapshot(); snapshot.Chat.Error != "" {
		t.Fatalf("turn failed: %s", snapshot.Chat.Error)
	}

	bodies := provider.recorded()
	if len(bodies) < 2 {
		t.Fatalf("记录到 %d 个 provider 请求，希望 >= 2（工具轮 + 终答轮）", len(bodies))
	}
	// 第一个请求里没有投影（读之前的控制面）：投影是按需读来的，不是装配注入的。
	if strings.Contains(string(bodies[0]), projectBoardE2EItem) {
		t.Fatal("第一个请求里就带着投影内容：读面还没被调用（I7 会被误解成注入）")
	}
	// 最后一个请求里带着本次工具结果：它必须含投影行 ⇒ 读面端到端通了。
	last := string(bodies[len(bodies)-1])
	if !strings.Contains(last, projectBoardE2EItem) {
		t.Fatalf("读面的工具结果没回到模型可见的请求里（端到端断了）：%s", clipForFailure(last, 600))
	}
	if !strings.Contains(last, `"kind":"work"`) && !strings.Contains(last, `\"kind\":\"work\"`) {
		t.Fatalf("工具结果里看不到 work 行：%s", clipForFailure(last, 600))
	}
}
