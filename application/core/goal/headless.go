package goal

// headless.go 承载 goal 域的 Headless 调教接口（headlessGUI 设计，对齐
// gui/headless.go 的 wire shape）：本地/测试驱动经 /rpc 调 goal 控制器
// 同源能力（goal_begin/update/finish/abort/status/projection + WithTechLeader 装配后的
// goal_tl_*/goal_propose_finish/goal_prescreen），经 /events 订阅全量 goal 事件流
// （快照语义）。真实业务全部在 Controller/Supervisor，本层只做方法分发与编解码，
// 不新增状态机（对齐 gui/headless.go 定位）。
//
// 接线位（后续 P0-wiring）：gui/headless.go dispatch 增一行 goal.<method>
// → 本包 Controller（由 application 按会话持有）；本层因此可原样复用。

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/RedHuang-0622/seelex/application/core/govern"
)

// Server 是 goal Headless 控制面（HTTP）。
type Server struct {
	controller *Controller
	supervisor *Supervisor
	governor   govern.Governor
}

// NewServer 构造 goal Headless 服务。
func NewServer(controller *Controller) *Server {
	return &Server{controller: controller}
}

// WithTechLeader 装配 TL 监督器（启用 goal_tl_* / goal_propose_finish / goal_prescreen RPC）。
func (s *Server) WithTechLeader(supervisor *Supervisor) *Server {
	s.supervisor = supervisor
	return s
}

// WithGovernor 装配回合制治理循环（启用 goal_gov_* RPC：多代理治理测试面）。
func (s *Server) WithGovernor(governor govern.Governor) *Server {
	s.governor = governor
	return s
}

// Handler 返回路由（/healthz /rpc /events）。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.serveHealth)
	mux.HandleFunc("/rpc", s.serveRPC)
	mux.HandleFunc("/events", s.serveEvents)
	return mux
}

// RPCRequest 是 /rpc 入参形状（与 gui/headless.go 同款：method + 位置参数）。
type RPCRequest struct {
	Method string            `json:"method"`
	Args   []json.RawMessage `json:"args"`
}

// RPCResponse 是 /rpc 出参形状。
type RPCResponse struct {
	OK     bool   `json:"ok"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (s *Server) serveHealth(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	_, _ = writer.Write([]byte(`{"ok":true}`))
}

func (s *Server) serveRPC(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	var payload RPCRequest
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		_ = json.NewEncoder(writer).Encode(RPCResponse{OK: false, Error: "decode rpc: " + err.Error()})
		return
	}
	result, err := s.dispatch(context.Background(), payload.Method, payload.Args)
	if err != nil {
		_ = json.NewEncoder(writer).Encode(RPCResponse{OK: false, Error: err.Error()})
		return
	}
	_ = json.NewEncoder(writer).Encode(RPCResponse{OK: true, Result: result})
}

// dispatch 把 headless 驱动的方法调用映射到 goal.Controller 契约。
// 新增 goal 命令时在此补一行即可（命令本身仍走 Controller）。
func (s *Server) dispatch(ctx context.Context, method string, args []json.RawMessage) (any, error) {
	decode := func(destination any) error {
		if len(args) == 0 {
			return nil
		}
		if err := json.Unmarshal(args[0], destination); err != nil {
			return fmt.Errorf("%s 参数解码失败: %v", method, err)
		}
		return nil
	}
	switch method {
	case "goal_begin":
		var request BeginRequest
		if err := decode(&request); err != nil {
			return nil, err
		}
		return s.controller.Begin(ctx, request)
	case "goal_update":
		var request UpdateRequest
		if err := decode(&request); err != nil {
			return nil, err
		}
		return s.controller.Update(ctx, request)
	case "goal_finish":
		var request FinishRequest
		if err := decode(&request); err != nil {
			return nil, err
		}
		return s.controller.Finish(ctx, request)
	case "goal_abort":
		var request FinishRequest
		if err := decode(&request); err != nil {
			return nil, err
		}
		return s.controller.Abort(ctx, request)
	case "goal_status":
		return s.controller.Status(), nil
	case "goal_projection":
		return s.controller.Projection(), nil
	case "goal_tl_snapshot":
		sup, err := s.supervisorFor(method)
		if err != nil {
			return nil, err
		}
		return sup.Snapshot(), nil
	case "goal_tl_notify":
		sup, err := s.supervisorFor(method)
		if err != nil {
			return nil, err
		}
		var signal TLEvalSignal
		if err := decode(&signal); err != nil {
			return nil, err
		}
		return nil, sup.Notify(ctx, signal)
	case "goal_tl_eval":
		sup, err := s.supervisorFor(method)
		if err != nil {
			return nil, err
		}
		var request struct {
			Trigger string `json:"trigger,omitempty"`
		}
		if err := decode(&request); err != nil {
			return nil, err
		}
		return sup.RunEval(ctx, request.Trigger)
	case "goal_tl_directives":
		sup, err := s.supervisorFor(method)
		if err != nil {
			return nil, err
		}
		return sup.Mailbox().DrainDirectives(), nil
	case "goal_propose_finish":
		sup, err := s.supervisorFor(method)
		if err != nil {
			return nil, err
		}
		var request FinishRequest
		if err := decode(&request); err != nil {
			return nil, err
		}
		return sup.ProposeFinish(ctx, request)
	case "goal_prescreen":
		sup, err := s.supervisorFor(method)
		if err != nil {
			return nil, err
		}
		var request ApprovalScreenRequest
		if err := decode(&request); err != nil {
			return nil, err
		}
		return sup.PreScreenApproval(ctx, request)
	case "goal_gov_next":
		if s.governor == nil {
			return nil, fmt.Errorf("%s: 治理循环未装配（WithGovernor）", method)
		}
		more, err := s.governor.Next(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]any{"more": more}, nil
	case "goal_gov_snapshot":
		if s.governor == nil {
			return nil, fmt.Errorf("%s: 治理循环未装配（WithGovernor）", method)
		}
		return govern.SnapshotOf(s.governor), nil
	case "goal_gov_break":
		if s.governor == nil {
			return nil, fmt.Errorf("%s: 治理循环未装配（WithGovernor）", method)
		}
		var request struct {
			Reason string `json:"reason"`
		}
		if err := decode(&request); err != nil {
			return nil, err
		}
		s.governor.Break(request.Reason)
		return nil, nil
	default:
		return nil, fmt.Errorf("未知 method %q（可用: goal_begin/goal_update/goal_finish/goal_abort/goal_status/goal_projection/goal_tl_*/goal_propose_finish/goal_prescreen/goal_gov_next/goal_gov_snapshot/goal_gov_break）", method)
	}
}

// supervisorFor 返回 TL 监督器；未装配（WithTechLeader）时报可读错误。
func (s *Server) supervisorFor(method string) (*Supervisor, error) {
	if s.supervisor == nil {
		return nil, fmt.Errorf("%s: techleader 未装配（goal_tl_* 需 WithTechLeader）", method)
	}
	return s.supervisor, nil
}

// serveEvents 以 JSON 行流输出订阅事件（每行一个 Event，写后 flush）。
// 事件自带全量 Projection；客户端落后/丢行可按快照语义重拉投影追平。
func (s *Server) serveEvents(writer http.ResponseWriter, request *http.Request) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		http.Error(writer, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "application/x-ndjson")
	writer.Header().Set("Cache-Control", "no-cache")
	subscription := s.controller.Subscribe(256)
	defer subscription.Close()
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	flusher.Flush()
	for {
		select {
		case event := <-subscription.Events:
			if err := encoder.Encode(event); err != nil {
				return
			}
			flusher.Flush()
		case <-request.Context().Done():
			return
		}
	}
}

// Client 是 goal Headless 调教接口的外部驱动客户端（冒烟脚本/测试/未来
// 前端同源调用）。Base 形如 "http://127.0.0.1:39123"。
type Client struct {
	Base string
	HTTP *http.Client
}

// NewClient 构造客户端。
func NewClient(base string) *Client {
	return &Client{Base: base, HTTP: &http.Client{Timeout: 10 * time.Second}}
}

// Call 调用一个 /rpc 方法；arg 可为 nil（无参）。成功时若 out 非 nil 则解码 result。
func (c *Client) Call(ctx context.Context, method string, arg any, out any) error {
	args := []json.RawMessage{}
	if arg != nil {
		raw, err := json.Marshal(arg)
		if err != nil {
			return err
		}
		args = append(args, raw)
	}
	body, err := json.Marshal(RPCRequest{Method: method, Args: args})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/rpc", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.HTTP.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var envelope RPCResponse
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return err
	}
	if !envelope.OK {
		return fmt.Errorf("goal rpc %s: %s", method, envelope.Error)
	}
	if out != nil && envelope.Result != nil {
		raw, err := json.Marshal(envelope.Result)
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, out)
	}
	return nil
}

// Health 探测 /healthz。
func (c *Client) Health(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+"/healthz", nil)
	if err != nil {
		return err
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz: http %d", response.StatusCode)
	}
	return nil
}

// ReadEvents 逐行读取 /events 流并调用 handle（阻塞至 ctx 取消或流结束）。
// 用于 headless 驱动做事件热力/断言分析（对齐 gui/headless.go /events 用途）。
func (c *Client) ReadEvents(ctx context.Context, handle func(Event) error) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+"/events", nil)
	if err != nil {
		return err
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var event Event
		if err := json.Unmarshal(line, &event); err != nil {
			return fmt.Errorf("events 解码失败: %v", err)
		}
		if err := handle(event); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}
