package gui

// TestHeadlessSubagentRouteSmoke 是 subagent 链路的离线拦截包冒烟：
//   - /events（headless SSE）拦截全局事件流，验证后端发布时携带正确的
//     session_id 路由键与 kind；
//   - hub.SubscribeSession 模拟前端按视图会话订阅（Bridge 同款过滤），
//     验证 main-session 的 subagent 工具/节点事件到达正确订阅，其它会话
//     的事件不串入本订阅。
// 前端 reducer 的“及时更新”由 gui/frontend/dist 的 event-chain /
// live-content-freshness 契约测试覆盖（node --test）。
import (
	"bufio"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application"
)

func TestHeadlessSubagentRouteSmoke(t *testing.T) {
	fake := &sessionAwareFakeApplication{fakeApplication: newFakeApplication()}
	base := newHeadlessTestServer(t, fake)

	// 模拟前端按当前视图会话订阅（Bridge 在 ActivateSession/ResumeSession
	// 后重建订阅，过滤在投递端完成）。
	viewSubscription := fake.hub.SubscribeSession("main-session", 64)
	defer viewSubscription.Close()

	// /events 全局拦截：SSE 元数据面（kind/session_id/delivery_seq）。
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(base + "/events")
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	sse := make(chan map[string]any, 8)
	go func() {
		for {
			line, readErr := reader.ReadString('\n')
			if readErr != nil {
				return
			}
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var item map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &item); err != nil {
				continue
			}
			sse <- item
		}
	}()

	toolPayload := application.SubagentToolEvent{ID: "subtool-1", NodeID: "node-fk1", Name: "bash", Arguments: "{}", Status: "running"}
	mainStarted := fake.hub.PublishSession(application.EventSubagentToolStarted, 2, "request-fork", "main-session", toolPayload)
	toolPayload.Status = "success"
	toolPayload.Result = "ok"
	mainCompleted := fake.hub.PublishSession(application.EventSubagentToolCompleted, 3, "request-fork", "main-session", toolPayload)
	otherChanged := fake.hub.PublishSession(application.EventSubagentChanged, 4, "request-background", "background-session", map[string]any{
		"node_id": "node-bg", "plan_status": "running", "progress": 0.1,
		"node": map[string]any{"id": "node-bg", "status": "running", "children": []any{}},
	})

	// 后端路由键：会话级事件必须携带发布方指定的 session_id。
	if mainStarted.SessionID != "main-session" || mainCompleted.SessionID != "main-session" {
		t.Fatalf("main-session events routed with sid %q/%q", mainStarted.SessionID, mainCompleted.SessionID)
	}
	if otherChanged.SessionID != "background-session" {
		t.Fatalf("background event routed with sid %q, want background-session", otherChanged.SessionID)
	}

	// 按会话订阅只收 main-session 的两条 subagent 工具事件；background-session
	// 的 subagent.changed 不进入该缓冲。
	gotMain := 0
	deadline := time.After(3 * time.Second)
	for gotMain < 2 {
		select {
		case event := <-viewSubscription.Events:
			switch event.Kind {
			case application.EventSubagentToolStarted, application.EventSubagentToolCompleted:
				if event.SessionID != "main-session" {
					t.Fatalf("view subscription got foreign session event: sid=%q kind=%s", event.SessionID, event.Kind)
				}
				gotMain++
			case application.EventSubagentChanged:
				t.Fatalf("view subscription leaked background-session event: %s", event.Kind)
			}
		case <-deadline:
			t.Fatalf("view subscription received %d/2 main-session subagent events", gotMain)
		}
	}
	select {
	case leaked := <-viewSubscription.Events:
		t.Fatalf("view subscription received unexpected extra event: kind=%s sid=%q", leaked.Kind, leaked.SessionID)
	default:
	}

	// /events 全局拦截应看到全部三条（含不同会话路由键），证明“发到了正确的路由”。
	seen := map[string]bool{}
	interceptDeadline := time.After(3 * time.Second)
	for len(seen) < 3 {
		select {
		case item := <-sse:
			key := item["session_id"].(string) + ":" + item["kind"].(string)
			seen[key] = true
		case <-interceptDeadline:
			t.Fatalf("/events interception saw %d/3 routed events: %v", len(seen), seen)
		}
	}
	for _, key := range []string{
		"main-session:subagent.tool.started",
		"main-session:subagent.tool.completed",
		"background-session:subagent.changed",
	} {
		if !seen[key] {
			t.Fatalf("/events interception missing routed event %q (saw %v)", key, seen)
		}
	}
}
