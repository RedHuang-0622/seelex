package session

// live_dispatch.go 是**子代理第一视角实时流分发器**（P21「域方法随类型走」：
// 从 `backend/engine/assemble/runtime_live.go` 把状态与判据搬进本域，装配层只留薄转发；
// plan §2.5 的 `runtime_live` → 多智能体侧）。
//
// 它把两条事件源汇进一个统一通道，按 nodeID 广播给订阅者，并维护每节点的**有界回放窗口**：
//
//   - 阶段日志（宿主 `NodeStageEvents`）与工具事件（`ToolEventState.Subscribe`）到达即投递
//     （即时输出，非轮询/缓存）；
//   - 订阅面给"历史回放快照 + 只读实时通道 + 幂等取消函数"，分页面读同一份窗口
//     （add but not modify 的两种读法）；
//   - assistant 正文增量只在该节点已有订阅者（dispatcher 已启动）时保留：无人订阅不积累，
//     避免运行路径被实时面拖慢。
//
// 宿主面走函数值端口（事件源是宿主的私有组件，域包不能反向依赖装配层）；窗口上限的
// **归一化判据**（0 → 默认 512、小于下限 → 50）也随域搬来，构造时注入配置值。

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/model"
)

const (
	// LiveChanCap 是实时流订阅通道/分发通道的容量。
	LiveChanCap = 128
	// LiveHistoryCap 是每节点实时事件历史缓冲上限的**默认值**（超出淘汰最旧；打开详情时
	// 回放该缓冲，保证"从 start 到最新"的滚动上下文）。实际上限可经配置覆盖。
	LiveHistoryCap = 512
	// LiveHistoryMinWindow 是窗口上限的**下限**：配置比它还小 → 收敛到它
	// （窗口至少要装得下一页，否则分页读法永远只看得见一两条）。
	LiveHistoryMinWindow = 50
	// LivePageDefaultLimit 是分页读法的默认页大小（limit <= 0 时用它）。
	LivePageDefaultLimit = 50
)

// NormalizeLiveWindow 归一化实时回放窗口上限：未配置（0）→ 默认 512；小于下限 → 下限。
func NormalizeLiveWindow(configured int) int {
	if configured == 0 {
		return LiveHistoryCap
	}
	if configured < LiveHistoryMinWindow {
		return LiveHistoryMinWindow
	}
	return configured
}

// LiveDispatcherDeps 是分发器的事件源（函数值端口；nil = 该事件源未装配）。
type LiveDispatcherDeps struct {
	// StageEvents 取阶段日志通道（宿主 `NodeStageEvents`）。
	StageEvents func() <-chan model.NodeStageLog
	// ToolEvents 订阅工具事件（宿主 `ToolEventState.Subscribe`），返回取消函数。
	ToolEvents func(fn func(SubagentToolEvent)) func()
}

// LiveDispatcher 是实时流分发器（有状态：一例对应一个进程级实时面）。
type LiveDispatcher struct {
	deps   LiveDispatcherDeps
	window int

	mu         sync.Mutex
	started    bool
	stop       chan struct{}
	ch         chan dto.SubagentLiveEvent
	subs       map[string][]chan dto.SubagentLiveEvent
	history    map[string][]dto.SubagentLiveEvent
	toolCancel func()
}

// NewLiveDispatcher 装配一例（窗口按 `NormalizeLiveWindow` 归一）。
//
// 窗口表（订阅表 / 回放窗口）在构造时就建好：`Broadcast` 在 `Start` 之前也要能用
// （装配层在装配期就 `startLiveDispatcher`，而域级用例会先播事件再订阅——向 nil map
// 赋值会 panic，这个坑在 P21-2 的域级用例里当场踩到）。
func NewLiveDispatcher(configuredWindow int, deps LiveDispatcherDeps) *LiveDispatcher {
	return &LiveDispatcher{
		deps:    deps,
		window:  NormalizeLiveWindow(configuredWindow),
		subs:    make(map[string][]chan dto.SubagentLiveEvent),
		history: make(map[string][]dto.SubagentLiveEvent),
	}
}

// Window 返回归一化后的窗口上限（分页与历史淘汰共用同一个数）。
func (dispatcher *LiveDispatcher) Window() int {
	if dispatcher == nil {
		return LiveHistoryCap
	}
	return dispatcher.window
}

// Start 启动分发器（幂等）：汇入阶段日志与工具事件，按 nodeID 广播。
func (dispatcher *LiveDispatcher) Start() {
	if dispatcher == nil {
		return
	}
	dispatcher.mu.Lock()
	if dispatcher.started {
		dispatcher.mu.Unlock()
		return
	}
	dispatcher.started = true
	dispatcher.stop = make(chan struct{})
	dispatcher.ch = make(chan dto.SubagentLiveEvent, LiveChanCap)
	if dispatcher.subs == nil {
		dispatcher.subs = make(map[string][]chan dto.SubagentLiveEvent)
	}
	if dispatcher.history == nil {
		dispatcher.history = make(map[string][]dto.SubagentLiveEvent)
	}
	liveCh := dispatcher.ch
	liveStop := dispatcher.stop
	dispatcher.mu.Unlock()

	go func() { // 广播循环：统一通道 → 按 nodeID 派发
		for {
			select {
			case event := <-liveCh:
				dispatcher.Broadcast(event)
			case <-liveStop:
				return
			}
		}
	}()
	go func() { // 阶段事件源
		if dispatcher.deps.StageEvents == nil {
			return
		}
		stages := dispatcher.deps.StageEvents()
		if stages == nil {
			return
		}
		for {
			select {
			case stage, ok := <-stages:
				if !ok {
					return
				}
				select {
				case liveCh <- StageLiveEvent(stage):
				case <-liveStop:
					return
				default:
				}
			case <-liveStop:
				return
			}
		}
	}()
	if dispatcher.deps.ToolEvents != nil {
		dispatcher.mu.Lock()
		dispatcher.toolCancel = dispatcher.deps.ToolEvents(func(event SubagentToolEvent) {
			select {
			case liveCh <- ToolLiveEvent(event):
			default:
			}
		})
		dispatcher.mu.Unlock()
	}
}

// Stop 停止分发（幂等；关机路径调用）。
func (dispatcher *LiveDispatcher) Stop() {
	if dispatcher == nil {
		return
	}
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	if !dispatcher.started {
		return
	}
	if dispatcher.toolCancel != nil {
		dispatcher.toolCancel()
		dispatcher.toolCancel = nil
	}
	select {
	case <-dispatcher.stop:
	default:
		close(dispatcher.stop)
	}
	dispatcher.started = false
}

// Subscribe 订阅 node 第一视角实时流：返回**历史回放**（从 subagent start 到订阅时刻的
// 有界事件缓冲）+ 只读实时通道 + 取消函数（取消幂等）。阶段与工具事件到达即投递。
func (dispatcher *LiveDispatcher) Subscribe(nodeID string) ([]dto.SubagentLiveEvent, <-chan dto.SubagentLiveEvent, func(), error) {
	if dispatcher == nil || nodeID == "" {
		return nil, nil, nil, fmt.Errorf("live subscribe: node id required")
	}
	dispatcher.Start()
	ch := make(chan dto.SubagentLiveEvent, LiveChanCap)
	dispatcher.mu.Lock()
	// 注册通道与取历史快照在同一把锁内：此后新事件既进历史、也广播给 ch，
	// 不会出现"快照之后、注册之前"的缺口。
	history := append([]dto.SubagentLiveEvent(nil), dispatcher.history[nodeID]...)
	dispatcher.subs[nodeID] = append(dispatcher.subs[nodeID], ch)
	dispatcher.mu.Unlock()
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			dispatcher.mu.Lock()
			subs := dispatcher.subs[nodeID]
			for index, candidate := range subs {
				if candidate == ch {
					dispatcher.subs[nodeID] = append(subs[:index], subs[index+1:]...)
					break
				}
			}
			// 摘表与 `close` 必须在**同一临界区**里：广播的投递也在锁内（见 Broadcast），
			// 两处同步之后就不存在"通道刚被关、发送已经在路上"的窗口。
			close(ch)
			dispatcher.mu.Unlock()
		})
	}
	return history, ch, cancel, nil
}

// HistoryPage 读回一页实时回放（有界窗口 + 分页），与 Subscribe 共用同一份窗口。
//
// 语义：offset 从窗口内最旧一条算起（0 = 最旧）；offset < 0 → 0；limit <= 0 → 默认 50；
// limit > 窗口上限 → 收敛到窗口；offset >= total → 空页且 HasMore=false（offset 原样回带）；
// Total = 窗口内条数；HasMore = offset+len(events) < Total；Events 顺序 = 由旧到新。
func (dispatcher *LiveDispatcher) HistoryPage(nodeID string, offset, limit int) dto.SubagentLiveHistoryPage {
	page := dto.SubagentLiveHistoryPage{ScopeID: nodeID}
	if dispatcher == nil || nodeID == "" {
		return page
	}
	window := dispatcher.Window()
	if limit <= 0 {
		limit = LivePageDefaultLimit
	}
	if limit > window {
		limit = window
	}
	if offset < 0 {
		offset = 0
	}
	dispatcher.mu.Lock()
	// 与订阅面同一把锁：快照是"这一刻的窗口"，此后新事件继续进窗口（下一趟能翻到）。
	history := append([]dto.SubagentLiveEvent(nil), dispatcher.history[nodeID]...)
	dispatcher.mu.Unlock()
	total := len(history)
	page.Offset, page.Limit, page.Total = offset, limit, total
	if offset < total {
		end := offset + limit
		if end > total {
			end = total
		}
		page.Events = append([]dto.SubagentLiveEvent(nil), history[offset:end]...)
	}
	page.HasMore = page.Offset+len(page.Events) < total
	return page
}

// Broadcast 把一条事件写进该节点窗口并投递给订阅者（窗口超限丢最旧；订阅者满投即丢）。
func (dispatcher *LiveDispatcher) Broadcast(event dto.SubagentLiveEvent) {
	if dispatcher == nil {
		return
	}
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	subs := dispatcher.subs[event.NodeID]
	history := append(dispatcher.history[event.NodeID], event)
	if window := dispatcher.Window(); len(history) > window {
		history = history[len(history)-window:]
	}
	dispatcher.history[event.NodeID] = history
	// 投递**在同一临界区内**：取消订阅（`Subscribe` 的 cancel）会在锁内摘表并 `close(ch)`，
	// 若这里把通道复制到锁外再发，就会出现"复制之后、发送之前通道被关"的窗口——
	// 向已关闭通道发送会 panic（P21-2 搬迁后域级用例当场复现）。发送本身是带 `default`
	// 的非阻塞投递，因此持锁发送不会让慢读者拖住广播。
	for _, ch := range subs {
		select {
		case ch <- event:
		default:
		}
	}
}

// RecordAssistant 投递子代理 assistant 正文增量（G7 正文增量 kind 的数据源接线）：
// 只在该节点已有详情订阅者（dispatcher 已启动）时保留实时面；无人订阅时不积累。
func (dispatcher *LiveDispatcher) RecordAssistant(nodeID, delta string) {
	if dispatcher == nil || nodeID == "" || strings.TrimSpace(delta) == "" {
		return
	}
	dispatcher.mu.Lock()
	started := dispatcher.started
	liveCh := dispatcher.ch
	liveStop := dispatcher.stop
	dispatcher.mu.Unlock()
	if !started {
		return
	}
	select {
	case liveCh <- AssistantLiveEvent(nodeID, delta):
	case <-liveStop:
	default:
	}
}

// AssistantLiveEvent 组装一条 assistant 正文增量事件。
func AssistantLiveEvent(nodeID, delta string) dto.SubagentLiveEvent {
	return dto.SubagentLiveEvent{
		NodeID: nodeID,
		At:     time.Now(),
		Kind:   "assistant",
		Assistant: &dto.SubagentAssistant{
			Text: delta,
		},
	}
}

// StageLiveEvent 把阶段日志投影成实时流事件（`model.NodeStageLog` 与 `dto.NodeStageLog`
// 同一类型，别名单源）。
func StageLiveEvent(log model.NodeStageLog) dto.SubagentLiveEvent {
	return dto.SubagentLiveEvent{
		NodeID: log.NodeID, At: log.At, Kind: "stage",
		Stage: &log,
	}
}

// ToolEventToDTO 把子代理工具调用运行态事件投影为实时流对外 DTO：
// Duration（time.Duration）→ DurationMS（毫秒）。
//
// 沿革：这段原在 `seelebridge/internal/mapper`（plan §2.10 要求删除该包），M4-T2 就地内联到
// 装配层的 `toolLiveEvent`；P21-2 随本域搬到这里，仍只有本文件一个调用方。
func ToolEventToDTO(event SubagentToolEvent) dto.SubagentTool {
	return dto.SubagentTool{
		ID: event.ID, NodeID: event.NodeID, Name: event.Name,
		Arguments: event.Arguments, Result: event.Result, Error: event.Error,
		Status: event.Status, StartedAt: event.StartedAt,
		DurationMS: event.Duration.Milliseconds(),
	}
}

// ToolLiveEvent 组装一条工具事件（投影见 ToolEventToDTO）。
func ToolLiveEvent(event SubagentToolEvent) dto.SubagentLiveEvent {
	tool := ToolEventToDTO(event)
	return dto.SubagentLiveEvent{
		NodeID: event.NodeID, At: time.Now(), Kind: "tool",
		Tool: &tool,
	}
}
