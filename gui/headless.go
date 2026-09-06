package gui

// headless.go 承载桌面 GUI 的“headless 冒烟接口”（headlessUI 设计）：
// 真实 GUI 进程内打开一个仅绑定 127.0.0.1 的本地控制面，让外部驱动像
// 前端一样调用 Bridge 同源能力（Snapshot/Submit/ResumeSession/
// BeginNewSession/PerfStats…），并订阅全量会话事件流做时间线/热力分析。
//
// 定位与边界：
//   - 默认关闭：只有显式设置 SEELEX_HEADLESS_PORT 才启动（开发/冒烟用，
//     生产双击启动不受影响）；只监听回环地址，不对外网开放。
//   - 能力面 = 前端 Bridge 的窄子集（gui/bridge.go 的 Application 契约），
//     不新增业务状态机；真实工作仍全部经 application core 执行。
//   - /events 订阅全局事件流（含所有会话），冒烟驱动据此记录事件到达
//     时刻/速率，与 /rpc 的调用耗时共同构成“热力时间消耗”数据面。

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// headlessEnvPort 是启用 headless 冒烟接口的环境变量（回环端口号）。
const headlessEnvPort = "SEELEX_HEADLESS_PORT"

// headlessEventBuffer 是事件流订阅缓冲（全量会话事件可能较密；溢出时 hub
// 会以 resync.required 全局事件兜底，驱动应按快照重拉处理）。
const headlessEventBuffer = 4096

// headlessServer 是 headless 冒烟接口的 HTTP 服务（单进程内绑定）。
type headlessServer struct {
	app Application
	srv *http.Server
}

// startHeadlessIfRequested 在设置 SEELEX_HEADLESS_PORT 时启动回环控制面；
// 未设置时返回空操作（生产双击路径零成本）。
func startHeadlessIfRequested(app Application) (func(), error) {
	port := strings.TrimSpace(os.Getenv(headlessEnvPort))
	if port == "" {
		return func() {}, nil
	}
	if _, err := strconv.Atoi(port); err != nil {
		return nil, fmt.Errorf("headless %s 必须是端口号，得到 %q", headlessEnvPort, port)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		return nil, fmt.Errorf("headless 接口监听失败: %w", err)
	}
	server := &headlessServer{app: app}
	go func() { _ = server.Serve(listener) }()
	stop := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.srv.Shutdown(ctx)
	}
	return stop, nil
}

// Serve 在给定监听器上提供 headless 冒烟接口（/healthz、/rpc、/events）。
func (server *headlessServer) Serve(listener net.Listener) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", server.serveHealth)
	mux.HandleFunc("/rpc", server.serveRPC)
	mux.HandleFunc("/events", server.serveEvents)
	server.srv = &http.Server{Handler: mux}
	return server.srv.Serve(listener)
}

func (server *headlessServer) serveHealth(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	_, _ = writer.Write([]byte(`{"ok":true}`))
}

// rpcRequest 是 /rpc 的入参形状：method + 位置参数数组（与 Bridge 方法签名
// 一一对应，减少驱动层的参数名转换）。
type rpcRequest struct {
	Method string            `json:"method"`
	Args   []json.RawMessage `json:"args"`
}

type rpcResponse struct {
	OK     bool   `json:"ok"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (server *headlessServer) serveRPC(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	var payload rpcRequest
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeRPCError(writer, fmt.Sprintf("decode rpc: %v", err))
		return
	}
	result, err := server.dispatch(payload.Method, payload.Args)
	if err != nil {
		writeRPCError(writer, err.Error())
		return
	}
	_ = json.NewEncoder(writer).Encode(rpcResponse{OK: true, Result: result})
}

func writeRPCError(writer http.ResponseWriter, message string) {
	_ = json.NewEncoder(writer).Encode(rpcResponse{OK: false, Error: message})
}

// dispatch 把冒烟驱动的方法调用映射到 Application（含会话级扩展）契约。
// 新增 GUI 命令时在这里补一行即可；命令本身仍走 application core。
func (server *headlessServer) dispatch(method string, args []json.RawMessage) (any, error) {
	stringArg := func(index int, name string) (string, error) {
		if index >= len(args) {
			return "", fmt.Errorf("%s 缺少参数 %s", method, name)
		}
		var value string
		if err := json.Unmarshal(args[index], &value); err != nil {
			return "", fmt.Errorf("%s 参数 %s 必须是字符串: %v", method, name, err)
		}
		return value, nil
	}
	intArg := func(index int, name string) (int, error) {
		if index >= len(args) {
			return 0, nil
		}
		var value int
		if err := json.Unmarshal(args[index], &value); err != nil {
			return 0, fmt.Errorf("%s 参数 %s 必须是整数: %v", method, name, err)
		}
		return value, nil
	}

	switch method {
	case "Snapshot":
		return server.app.Snapshot(), nil
	case "PerfStats":
		return server.app.PerfStats(), nil
	case "BeginNewSession":
		return nil, server.app.BeginNewSession()
	case "Submit":
		text, err := stringArg(0, "text")
		if err != nil {
			return nil, err
		}
		return nil, server.app.Submit(context.Background(), text)
	case "ResumeSession":
		sessionID, err := stringArg(0, "sessionID")
		if err != nil {
			return nil, err
		}
		return nil, server.app.ResumeSession(sessionID)
	case "ForkSessionLatest":
		parentID, err := stringArg(0, "parentID")
		if err != nil {
			return nil, err
		}
		return server.app.ForkSessionLatest(parentID)
	case "CancelChat":
		requestID, err := stringArg(0, "requestID")
		if err != nil {
			return nil, err
		}
		return server.app.CancelChat(requestID), nil
	case "LoadMoreHistory":
		limit, err := intArg(0, "limit")
		if err != nil {
			return nil, err
		}
		return nil, server.app.LoadMoreHistory(limit)
	case "ResolveInteraction":
		id, err := stringArg(0, "id")
		if err != nil {
			return nil, err
		}
		optionID, err := stringArg(1, "optionID")
		if err != nil {
			return nil, err
		}
		return nil, server.app.ResolveInteraction(context.Background(), id, optionID)
	}

	switch method {
	case "ListSessions":
		sessionAware, ok := server.app.(sessionAwareApplication)
		if !ok {
			return nil, fmt.Errorf("方法 %s 需要会话级宿主能力，当前不可用", method)
		}
		return sessionAware.ListSessions(), nil
	case "SnapshotOf":
		sessionAware, ok := server.app.(sessionAwareApplication)
		if !ok {
			return nil, fmt.Errorf("方法 %s 需要会话级宿主能力，当前不可用", method)
		}
		sessionID, err := stringArg(0, "sessionID")
		if err != nil {
			return nil, err
		}
		return sessionAware.SnapshotOf(sessionID)
	case "ActivateSession":
		sessionAware, ok := server.app.(sessionAwareApplication)
		if !ok {
			return nil, fmt.Errorf("方法 %s 需要会话级宿主能力，当前不可用", method)
		}
		sessionID, err := stringArg(0, "sessionID")
		if err != nil {
			return nil, err
		}
		return nil, sessionAware.ActivateSession(sessionID)
	}
	return nil, fmt.Errorf("未知 headless 方法: %s", method)
}

// serveEvents 以 SSE 方式推送全量会话事件（含 payload 体积与到达时刻由
// 驱动侧打点）。事件行只带公开元数据，不携带会话正文，避免把完整对话内容
// 重复流过冒烟通道。
func (server *headlessServer) serveEvents(writer http.ResponseWriter, request *http.Request) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		http.Error(writer, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")
	writer.WriteHeader(http.StatusOK)
	// 立即冲刷首包，让客户端拿到 SSE 响应头（否则无事件期间连接不建链）。
	_, _ = fmt.Fprint(writer, ": connected\n\n")
	flusher.Flush()

	subscription := server.app.Subscribe(headlessEventBuffer)
	defer subscription.Close()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case <-heartbeat.C:
			_, _ = fmt.Fprint(writer, ": ping\n\n")
			flusher.Flush()
		case item, open := <-subscription.Events:
			if !open {
				return
			}
			line, marshalErr := json.Marshal(map[string]any{
				"delivery_seq":  item.DeliverySeq,
				"kind":          string(item.Kind),
				"session_id":    item.SessionID,
				"request_id":    item.RequestID,
				"revision":      item.Revision,
				"payload_bytes": len(item.Payload),
			})
			if marshalErr != nil {
				continue
			}
			_, _ = fmt.Fprintf(writer, "data: %s\n\n", line)
			flusher.Flush()
		}
	}
}
