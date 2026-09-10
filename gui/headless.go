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
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
	"github.com/RedHuang-0622/seelex/sessionstore"
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

// RunHeadless 是 headless 控制面的独立入口（调试/冒烟用，不启动桌面窗口）：
// 与 GUI 进程内装配一致地启动回环 RPC + 事件流，但不依赖 WebView2/Wails。
// 未设置 SEELEX_HEADLESS_PORT 时返回可读错误；启动后阻塞到进程收到中断或
// 终止信号（外部驱动结束后 kill 进程即可）。
func RunHeadless(app Application) error {
	port := strings.TrimSpace(os.Getenv(headlessEnvPort))
	if port == "" {
		return fmt.Errorf("headless 调试入口需要 %s=<端口>（例如 SEELEX_HEADLESS_PORT=39123）", headlessEnvPort)
	}
	stop, err := startHeadlessIfRequested(app)
	if err != nil {
		return err
	}
	defer stop()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	<-signals
	return nil
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
	if strings.HasPrefix(method, "goal.") {
		return server.dispatchGoal(method, args)
	}
	if strings.HasPrefix(method, "role.") {
		return server.dispatchRole(method, args)
	}
	if strings.HasPrefix(method, "schedule.") {
		return server.dispatchSchedule(method, args)
	}
	if strings.HasPrefix(method, "team.") {
		return server.dispatchTeam(method, args)
	}
	if strings.HasPrefix(method, "subagent.") {
		return server.dispatchSubagent(method, args)
	}
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
	case "CreateWorkspace":
		name, err := stringArg(0, "name")
		if err != nil {
			return nil, err
		}
		rootPath, err := stringArg(1, "rootPath")
		if err != nil {
			return nil, err
		}
		gitRemote, err := stringArg(2, "gitRemote")
		if err != nil {
			return nil, err
		}
		return nil, server.app.CreateWorkspace(name, rootPath, gitRemote)
	case "BindWorkspace":
		workspaceID, err := stringArg(0, "workspaceID")
		if err != nil {
			return nil, err
		}
		return nil, server.app.BindWorkspace(workspaceID)
	case "UnbindWorkspace":
		server.app.UnbindWorkspace()
		return nil, nil
	case "WaitIdle":
		// 异步冒烟驱动在 Submit 后等待全部已接受 chat 收敛（0/缺参回退
		// 5 分钟默认护栏，避免控制面调用永久悬挂）。
		timeoutSeconds, err := intArg(0, "timeoutSeconds")
		if err != nil {
			return nil, err
		}
		ctx, cancel := waitContext(timeoutSeconds, 5*time.Minute)
		defer cancel()
		if err := server.app.WaitForIdle(ctx); err != nil {
			return nil, fmt.Errorf("%s 等待空闲失败: %w", method, err)
		}
		return nil, nil
	case "WaitCatalogRefresh":
		// 命令（BeginNewSession/ResumeSession/ActivateSession）后等待会话
		// 目录 worker 覆盖本次变更再读 ListSessions，避免读到旧目录。
		timeoutSeconds, err := intArg(0, "timeoutSeconds")
		if err != nil {
			return nil, err
		}
		ctx, cancel := waitContext(timeoutSeconds, 15*time.Second)
		defer cancel()
		if err := server.app.WaitCatalogRefresh(ctx); err != nil {
			return nil, fmt.Errorf("%s 等待目录收敛失败: %w", method, err)
		}
		return nil, nil
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

// roleRPCApplication 是 Application 的 R2/R4 群聊角色扩展面（headless
// 冒烟/巡检透传；真实排序/幂等/floor 仍由 sessionstore 执行）。
type roleRPCApplication interface {
	CreateRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (sessionstore.RoleSessionInfo, error)
	AppendRoleDraft(mainSessionID, roleName, roleSessionID string, rows []sessionstore.RoleDraftRow) error
	ReadRoleDraft(mainSessionID, roleName, roleSessionID string) ([]sessionstore.RoleDraftRow, error)
	SyncRoleDraft(mainSessionID, roleName, roleSessionID string, order []string) (sessionstore.RoleDraftSyncResult, error)
	AppendRoleSessionRows(mainSessionID, roleName, roleSessionID string, rows []sessionstore.Event) error
	ReadRoleSessionRows(mainSessionID, roleName, roleSessionID string) ([]sessionstore.Event, error)
	RoleSnapshot(mainSessionID, roleName, roleSessionID string) (sessionstore.RoleSnapshot, error)
	AssembleRoleWire(mainSessionID, roleName, roleSessionID string, budget, k int) (sessionstore.RoleWireSnapshot, error)
	SetLifecycleOrder(sessionID, policy string, roles []string) error
	SetRoleLifecycle(mainSessionID, roleName, roleSessionID string, joinSeq uint64, ref *sessionstore.CompactRef) error
	ListRoleSessions(mainSessionID string) ([]string, error)
	ScheduleRegister(sessionID string, payload sessionstore.ScheduleEventPayload) error
	ScheduleCancel(sessionID string, payload sessionstore.ScheduleEventPayload) error
	ScheduleFire(sessionID string, payload sessionstore.ScheduleEventPayload) error
}

type roleCreateRequest struct {
	MainSessionID string `json:"main_session_id"`
	RoleName      string `json:"role_name"`
	RoleSessionID string `json:"role_session_id"`
	JoinSeqID     uint64 `json:"join_seq_id,omitempty"`
}

type roleDraftRequest struct {
	MainSessionID string                      `json:"main_session_id"`
	RoleName      string                      `json:"role_name"`
	RoleSessionID string                      `json:"role_session_id"`
	Rows          []sessionstore.RoleDraftRow `json:"rows,omitempty"`
	Order         []string                    `json:"order,omitempty"`
	Budget        int                         `json:"budget,omitempty"`
	K             int                         `json:"k,omitempty"`
}

type roleBackupRequest struct {
	MainSessionID string               `json:"main_session_id"`
	RoleName      string               `json:"role_name"`
	RoleSessionID string               `json:"role_session_id"`
	Rows          []sessionstore.Event `json:"rows,omitempty"`
}

type roleOrderRequest struct {
	SessionID   string   `json:"session_id"`
	OrderPolicy string   `json:"order_policy"`
	OrderRoles  []string `json:"order_roles,omitempty"`
}

type roleLifecycleRequest struct {
	MainSessionID string                   `json:"main_session_id"`
	RoleName      string                   `json:"role_name"`
	RoleSessionID string                   `json:"role_session_id"`
	JoinSeqID     uint64                   `json:"join_seq_id,omitempty"`
	CompactRef    *sessionstore.CompactRef `json:"compact_ref,omitempty"`
}

type scheduleRPCRequest struct {
	SessionID string                            `json:"session_id"`
	Payload   sessionstore.ScheduleEventPayload `json:"payload"`
}

func decodeHeadlessObject(method string, args []json.RawMessage, destination any) error {
	if len(args) == 0 {
		return fmt.Errorf("%s 缺少参数对象", method)
	}
	if err := json.Unmarshal(args[0], destination); err != nil {
		return fmt.Errorf("%s 参数解码失败: %w", method, err)
	}
	return nil
}

func (server *headlessServer) dispatchRole(method string, args []json.RawMessage) (any, error) {
	app, ok := server.app.(roleRPCApplication)
	if !ok {
		return nil, fmt.Errorf("%s: 当前 Application 未装配群聊角色扩展面", method)
	}
	switch method {
	case "role.create":
		var request roleCreateRequest
		if err := decodeHeadlessObject(method, args, &request); err != nil {
			return nil, err
		}
		return app.CreateRoleSession(request.MainSessionID, request.RoleName, request.RoleSessionID, request.JoinSeqID)
	case "role.append_draft":
		var request roleDraftRequest
		if err := decodeHeadlessObject(method, args, &request); err != nil {
			return nil, err
		}
		return nil, app.AppendRoleDraft(request.MainSessionID, request.RoleName, request.RoleSessionID, request.Rows)
	case "role.read_draft":
		var request roleDraftRequest
		if err := decodeHeadlessObject(method, args, &request); err != nil {
			return nil, err
		}
		return app.ReadRoleDraft(request.MainSessionID, request.RoleName, request.RoleSessionID)
	case "role.sync_draft":
		var request roleDraftRequest
		if err := decodeHeadlessObject(method, args, &request); err != nil {
			return nil, err
		}
		return app.SyncRoleDraft(request.MainSessionID, request.RoleName, request.RoleSessionID, request.Order)
	case "role.append_backup":
		var request roleBackupRequest
		if err := decodeHeadlessObject(method, args, &request); err != nil {
			return nil, err
		}
		return nil, app.AppendRoleSessionRows(request.MainSessionID, request.RoleName, request.RoleSessionID, request.Rows)
	case "role.read_backup":
		var request roleBackupRequest
		if err := decodeHeadlessObject(method, args, &request); err != nil {
			return nil, err
		}
		return app.ReadRoleSessionRows(request.MainSessionID, request.RoleName, request.RoleSessionID)
	case "role.snapshot":
		var request roleDraftRequest
		if err := decodeHeadlessObject(method, args, &request); err != nil {
			return nil, err
		}
		return app.RoleSnapshot(request.MainSessionID, request.RoleName, request.RoleSessionID)
	case "role.wire":
		var request roleDraftRequest
		if err := decodeHeadlessObject(method, args, &request); err != nil {
			return nil, err
		}
		return app.AssembleRoleWire(request.MainSessionID, request.RoleName, request.RoleSessionID, request.Budget, request.K)
	case "role.list":
		var request roleCreateRequest
		if err := decodeHeadlessObject(method, args, &request); err != nil {
			return nil, err
		}
		return app.ListRoleSessions(request.MainSessionID)
	case "role.set_order":
		var request roleOrderRequest
		if err := decodeHeadlessObject(method, args, &request); err != nil {
			return nil, err
		}
		return nil, app.SetLifecycleOrder(request.SessionID, request.OrderPolicy, request.OrderRoles)
	case "role.set_lifecycle":
		var request roleLifecycleRequest
		if err := decodeHeadlessObject(method, args, &request); err != nil {
			return nil, err
		}
		return nil, app.SetRoleLifecycle(request.MainSessionID, request.RoleName, request.RoleSessionID, request.JoinSeqID, request.CompactRef)
	default:
		return nil, fmt.Errorf("未知 role headless 方法: %s", method)
	}
}

func (server *headlessServer) dispatchSchedule(method string, args []json.RawMessage) (any, error) {
	app, ok := server.app.(roleRPCApplication)
	if !ok {
		return nil, fmt.Errorf("%s: 当前 Application 未装配定时插话扩展面", method)
	}
	var request scheduleRPCRequest
	if err := decodeHeadlessObject(method, args, &request); err != nil {
		return nil, err
	}
	switch method {
	case "schedule.register":
		return nil, app.ScheduleRegister(request.SessionID, request.Payload)
	case "schedule.cancel":
		return nil, app.ScheduleCancel(request.SessionID, request.Payload)
	case "schedule.fire":
		return nil, app.ScheduleFire(request.SessionID, request.Payload)
	default:
		return nil, fmt.Errorf("未知 schedule headless 方法: %s", method)
	}
}

// goalRPCApplication 是 Application 的 goal 扩展面（P1 headless 透传：
// goal.<method> → application goal 协调器，按当前视图会话路由）。
type goalRPCApplication interface {
	GoalBeginFor(context.Context, string, goaldomain.BeginRequest) (*goaldomain.GoalRecord, error)
	GoalUpdateFor(context.Context, string, goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error)
	GoalProposeFinishFor(context.Context, string, goaldomain.FinishRequest) (goaldomain.FinishProposalResult, error)
	GoalStatusFor(string) (goaldomain.StatusView, error)
	GoalNextFor(context.Context, string) (bool, error)
	GoalBreakFor(context.Context, string, string) error
	GoalGovernanceViewFor(string) *dto.GoalGovernanceView
}

// dispatchGoal 把 goal.<method> 透传到 application（当前视图会话）。
func (server *headlessServer) dispatchGoal(method string, args []json.RawMessage) (any, error) {
	app, ok := server.app.(goalRPCApplication)
	if !ok {
		return nil, fmt.Errorf("%s: 当前 Application 未装配 goal 扩展面", method)
	}
	sessionID := server.app.Snapshot().Session.ID
	ctx := context.Background()
	decodeArg := func(destination any) error {
		if len(args) == 0 {
			return nil
		}
		return json.Unmarshal(args[0], destination)
	}
	switch method {
	case "goal.begin":
		var request goaldomain.BeginRequest
		if err := decodeArg(&request); err != nil {
			return nil, fmt.Errorf("%s 参数解码失败: %w", method, err)
		}
		return app.GoalBeginFor(ctx, sessionID, request)
	case "goal.update":
		var request goaldomain.UpdateRequest
		if err := decodeArg(&request); err != nil {
			return nil, fmt.Errorf("%s 参数解码失败: %w", method, err)
		}
		return app.GoalUpdateFor(ctx, sessionID, request)
	case "goal.propose_finish":
		var request goaldomain.FinishRequest
		if err := decodeArg(&request); err != nil {
			return nil, fmt.Errorf("%s 参数解码失败: %w", method, err)
		}
		return app.GoalProposeFinishFor(ctx, sessionID, request)
	case "goal.status":
		return app.GoalStatusFor(sessionID)
	case "goal.gov_next":
		return app.GoalNextFor(ctx, sessionID)
	case "goal.gov_snapshot":
		return app.GoalGovernanceViewFor(sessionID), nil
	case "goal.gov_break":
		var request struct {
			Reason string `json:"reason"`
		}
		if err := decodeArg(&request); err != nil {
			return nil, fmt.Errorf("%s 参数解码失败: %w", method, err)
		}
		return nil, app.GoalBreakFor(ctx, sessionID, request.Reason)
	default:
		return nil, fmt.Errorf("未知 goal headless 方法: %s", method)
	}
}

// waitContext 为异步等待类 RPC 构造带护栏的 context：显式 timeoutSeconds
// 为正时按秒生效，否则回退 fallback（保证缺参/0 也不会无限悬挂）。
func waitContext(timeoutSeconds int, fallback time.Duration) (context.Context, context.CancelFunc) {
	if timeoutSeconds > 0 {
		return context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	}
	return context.WithTimeout(context.Background(), fallback)
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
