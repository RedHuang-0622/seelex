package goal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// startHeadlessServer 起测试用 goal Headless 控制面。
func startHeadlessServer(t *testing.T, controller *Controller) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(NewServer(controller).Handler())
	t.Cleanup(server.Close)
	return server
}

func TestHeadlessHealthAndBeginStatusFinish(t *testing.T) {
	controller := newTestController(t, DefaultStackDepth)
	server := startHeadlessServer(t, controller)
	client := NewClient(server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Health(ctx); err != nil {
		t.Fatalf("healthz: %v", err)
	}

	// goal_begin → result 为 GoalRecord。
	var record GoalRecord
	if err := client.Call(ctx, "goal_begin", BeginRequest{
		Title: "发布 v1", Statement: "headless 驱动", Acceptance: []string{"go test 全绿"},
	}, &record); err != nil {
		t.Fatalf("goal_begin: %v", err)
	}
	if record.ID == "" || record.Status != StatusActive {
		t.Fatalf("begin 结果异常: %+v", record)
	}

	// 幂等第二次 begin 返回同 ID。
	var again GoalRecord
	if err := client.Call(ctx, "goal_begin", BeginRequest{Title: "发布 v1"}, &again); err != nil {
		t.Fatalf("幂等 begin: %v", err)
	}
	if again.ID != record.ID {
		t.Fatalf("幂等 begin 应返回同 ID: %s != %s", again.ID, record.ID)
	}

	// goal_status → StatusView（栈顶全量）。
	var status StatusView
	if err := client.Call(ctx, "goal_status", nil, &status); err != nil {
		t.Fatalf("goal_status: %v", err)
	}
	if status.Active == nil || status.Active.ID != record.ID || status.Active.Title != "发布 v1" {
		t.Fatalf("status 异常: %+v", status)
	}

	// goal_update → progress 生效。
	var updated GoalRecord
	if err := client.Call(ctx, "goal_update", UpdateRequest{
		ProgressKind: ProgressMilestone, ProgressContent: "headless 装配完成",
	}, &updated); err != nil {
		t.Fatalf("goal_update: %v", err)
	}
	if len(updated.Progress) != 1 {
		t.Fatalf("update 未生效: %+v", updated)
	}

	// goal_projection → 前端投影契约（active + goals 栈底→栈顶）。
	var projection Projection
	if err := client.Call(ctx, "goal_projection", nil, &projection); err != nil {
		t.Fatalf("goal_projection: %v", err)
	}
	if projection.Active == nil || projection.Active.Title != "发布 v1" || len(projection.Goals) != 1 {
		t.Fatalf("projection 异常: %+v", projection)
	}

	// goal_finish → 弹栈；投影回退为空。
	var finished GoalRecord
	if err := client.Call(ctx, "goal_finish", FinishRequest{Result: "v1 发布"}, &finished); err != nil {
		t.Fatalf("goal_finish: %v", err)
	}
	if finished.Status != StatusCompleted {
		t.Fatalf("finish 状态异常: %+v", finished)
	}
	// finish 后投影应为空；复位再解码（omitempty 不会清掉复用结构体的旧指针）。
	var projectionAfter Projection
	if err := client.Call(ctx, "goal_projection", nil, &projectionAfter); err != nil {
		t.Fatalf("goal_projection(空): %v", err)
	}
	if projectionAfter.Active != nil || len(projectionAfter.Goals) != 0 {
		t.Fatalf("finish 后投影应为空: %+v", projectionAfter)
	}
}

func TestHeadlessErrorTransparentAndUnknownMethod(t *testing.T) {
	controller := newTestController(t, 1)
	server := startHeadlessServer(t, controller)
	client := NewClient(server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := controller.Begin(testCtx, BeginRequest{Title: "A"}); err != nil {
		t.Fatalf("begin: %v", err)
	}

	// 栈满错误应透传（含 goal 单例语义文案）。
	var record GoalRecord
	err := client.Call(ctx, "goal_begin", BeginRequest{Title: "B"}, &record)
	if err == nil || !strings.Contains(err.Error(), "栈已满") {
		t.Fatalf("栈满错误应透传, 得 %v", err)
	}

	// 空栈 finish 错误透传：先经 headless 收口 A。
	if err := client.Call(ctx, "goal_finish", FinishRequest{}, &record); err != nil {
		t.Fatalf("headless finish A: %v", err)
	}
	err = client.Call(ctx, "goal_finish", FinishRequest{}, &record)
	if err == nil || !strings.Contains(err.Error(), "无 active") {
		t.Fatalf("空栈错误应透传, 得 %v", err)
	}

	// 未知 method。
	err = client.Call(ctx, "goal_watch", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "未知 method") {
		t.Fatalf("未知 method 应报错, 得 %v", err)
	}
}

// TestHeadlessEventsStream 验证 /events 流：begin/finish 事件行按序到达且
// 携带全量投影（前端无需轮询即可追平状态）。
func TestHeadlessEventsStream(t *testing.T) {
	controller := newTestController(t, DefaultStackDepth)
	server := startHeadlessServer(t, controller)
	client := NewClient(server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	received := make(chan Event, 8)
	go func() {
		_ = client.ReadEvents(ctx, func(event Event) error {
			received <- event
			return nil
		})
	}()
	// 确保 /events 订阅先建立（服务器写回头部即已注册订阅）。
	time.Sleep(50 * time.Millisecond)

	if err := client.Call(ctx, "goal_begin", BeginRequest{Title: "流式目标"}, nil); err != nil {
		t.Fatalf("goal_begin: %v", err)
	}
	select {
	case event := <-received:
		if event.Kind != EventBegin || event.GoalID == "" {
			t.Fatalf("事件异常: %+v", event)
		}
		if event.Projection.Active == nil || event.Projection.Active.Title != "流式目标" {
			t.Fatalf("事件投影异常: %+v", event.Projection)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("/events 未收到 begin 事件")
	}

	if err := client.Call(ctx, "goal_finish", FinishRequest{}, nil); err != nil {
		t.Fatalf("goal_finish: %v", err)
	}
	select {
	case event := <-received:
		if event.Kind != EventFinish || event.Projection.Active != nil {
			t.Fatalf("finish 事件应携带空投影: %+v", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("/events 未收到 finish 事件")
	}
}

// TestHeadlessJSONWireShape 钉住 wire shape：method + args（gui/headless.go
// 同款），result 字段 JSON 可反序列化为契约视图。
func TestHeadlessJSONWireShape(t *testing.T) {
	controller := newTestController(t, DefaultStackDepth)
	server := startHeadlessServer(t, controller)

	body := strings.NewReader(`{"method":"goal_projection","args":[]}`)
	response, err := http.Post(server.URL+"/rpc", "application/json", body)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer response.Body.Close()
	var envelope struct {
		OK     bool       `json:"ok"`
		Result Projection `json:"result"`
		Error  string     `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !envelope.OK || envelope.Error != "" || envelope.Result.Active != nil {
		t.Fatalf("wire 形状异常: %+v", envelope)
	}
}
