package gui

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application"
)

func newHeadlessTestServer(t *testing.T, app Application) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &headlessServer{app: app}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.srv.Shutdown(ctx)
	})
	return "http://" + listener.Addr().String()
}

func headlessRPC(t *testing.T, base, method string, args ...any) rpcResponse {
	t.Helper()
	rawArgs := make([]json.RawMessage, 0, len(args))
	for _, arg := range args {
		encoded, err := json.Marshal(arg)
		if err != nil {
			t.Fatalf("marshal arg %v: %v", arg, err)
		}
		rawArgs = append(rawArgs, encoded)
	}
	body, err := json.Marshal(rpcRequest{Method: method, Args: rawArgs})
	if err != nil {
		t.Fatalf("marshal rpc request: %v", err)
	}
	response, err := http.Post(base+"/rpc", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /rpc %s: %v", method, err)
	}
	defer response.Body.Close()
	var result rpcResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode rpc response %s: %v", method, err)
	}
	return result
}

// TestHeadlessHealthAndRPC 验证 headless 冒烟接口的命令面：方法映射到
// Application 契约，参数错误/未知方法显式报错。
func TestHeadlessHealthAndRPC(t *testing.T) {
	fake := newFakeApplication()
	base := newHeadlessTestServer(t, fake)

	healthResponse, err := http.Get(base + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	_ = healthResponse.Body.Close()
	if healthResponse.StatusCode != http.StatusOK {
		t.Fatalf("healthz status = %d", healthResponse.StatusCode)
	}

	if result := headlessRPC(t, base, "Submit", "hello headless"); !result.OK {
		t.Fatalf("Submit failed: %s", result.Error)
	} else if fake.submitted != "hello headless" {
		t.Fatalf("Submit text = %q", fake.submitted)
	}
	if result := headlessRPC(t, base, "ResumeSession", "session-x"); !result.OK {
		t.Fatalf("ResumeSession failed: %s", result.Error)
	} else if fake.resumedSession != "session-x" {
		t.Fatalf("resumed = %q", fake.resumedSession)
	}
	if result := headlessRPC(t, base, "BeginNewSession"); !result.OK {
		t.Fatalf("BeginNewSession failed: %s", result.Error)
	} else if !fake.beganNewSession {
		t.Fatal("BeginNewSession did not reach application")
	}
	if result := headlessRPC(t, base, "CancelChat", "request-1"); !result.OK {
		t.Fatalf("CancelChat failed: %s", result.Error)
	} else if fake.cancelled != "request-1" {
		t.Fatalf("cancelled = %q", fake.cancelled)
	}
	if result := headlessRPC(t, base, "PerfStats"); !result.OK {
		t.Fatalf("PerfStats failed: %s", result.Error)
	}

	if result := headlessRPC(t, base, "Submit"); result.OK || !strings.Contains(result.Error, "缺少参数") {
		t.Fatalf("Submit without args should fail, got ok=%v error=%q", result.OK, result.Error)
	}
	if result := headlessRPC(t, base, "NoSuchMethod"); result.OK || !strings.Contains(result.Error, "未知 headless 方法") {
		t.Fatalf("unknown method should fail, got ok=%v error=%q", result.OK, result.Error)
	}
}

// TestHeadlessEventsStream 验证 /events 以 SSE 推送会话事件（公开元数据面）。
func TestHeadlessEventsStream(t *testing.T) {
	fake := newFakeApplication()
	base := newHeadlessTestServer(t, fake)

	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(base + "/events")
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)

	fake.hub.Publish(application.EventMessageDelta, 2, "request-1", application.MessageDelta{MessageID: "message-9"})

	for {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			t.Fatalf("read sse line: %v", readErr)
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &payload); err != nil {
			t.Fatalf("decode sse payload: %v", err)
		}
		if payload["kind"] != string(application.EventMessageDelta) {
			t.Fatalf("sse kind = %v", payload["kind"])
		}
		if payload["request_id"] != "request-1" {
			t.Fatalf("sse request_id = %v", payload["request_id"])
		}
		if _, ok := payload["delivery_seq"]; !ok {
			t.Fatal("sse delivery_seq missing")
		}
		return
	}
}
