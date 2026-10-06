package seelebridge

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/mapper"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	subagentsession "github.com/RedHuang-0622/seelex/seelebridge/session"
)

// subagentLiveChanCap 是实时流订阅通道/分发通道的容量。
const subagentLiveChanCap = 128

// subagentLiveHistoryCap 是每节点实时事件历史缓冲上限的**默认值**（超出淘汰最旧；
// 打开详情时回放该缓冲，保证"从 start 到最新"的滚动上下文）。实际上限可经
// RuntimeConfig.SubagentLiveWindow 配置，读法与写入点都走 subagentLiveWindowSize()。
const subagentLiveHistoryCap = 512

// subagentLiveHistoryMinWindow 是窗口上限的**下限**：配置比它还小 → 收敛到它
// （窗口至少要装得下一页，否则分页读法永远只看得见一两条）。
const subagentLiveHistoryMinWindow = 50

// subagentLivePageDefaultLimit 是分页读法的默认页大小（limit <= 0 时用它）。
const subagentLivePageDefaultLimit = 50

// subagentLiveWindowSize 归一化实时回放窗口上限：未配置（0）→ 默认 512；
// 小于下限 → 下限。零值 Runtime 也走这一份归一（不把窗口剪成 0）。
func (r *Runtime) subagentLiveWindowSize() int {
	if r == nil {
		return subagentLiveHistoryCap
	}
	configured := r.subagentLiveWindow
	if configured == 0 {
		return subagentLiveHistoryCap
	}
	if configured < subagentLiveHistoryMinWindow {
		return subagentLiveHistoryMinWindow
	}
	return configured
}

// startLiveDispatcher 启动 node 第一视角实时流分发器（幂等）：阶段日志
// （SubagentSessions.StageEvents）+ 工具事件（ToolEventState.Subscribe）
// 汇入统一通道，按 nodeID 广播到订阅者。即时输出面：事件发生即投递。
func (r *Runtime) startLiveDispatcher() {
	r.liveMu.Lock()
	if r.liveStarted {
		r.liveMu.Unlock()
		return
	}
	r.liveStarted = true
	r.liveStop = make(chan struct{})
	r.liveCh = make(chan dto.SubagentLiveEvent, subagentLiveChanCap)
	r.liveSubs = make(map[string][]chan dto.SubagentLiveEvent)
	r.liveHistory = make(map[string][]dto.SubagentLiveEvent)
	liveCh := r.liveCh
	liveStop := r.liveStop
	r.liveMu.Unlock()

	go func() { // 广播循环：统一通道 → 按 nodeID 派发
		for {
			select {
			case event := <-liveCh:
				r.broadcastLive(event)
			case <-liveStop:
				return
			}
		}
	}()
	go func() { // 阶段事件源
		stages := r.NodeStageEvents()
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
				case liveCh <- stageLiveEvent(stage):
				case <-liveStop:
					return
				default:
				}
			case <-liveStop:
				return
			}
		}
	}()
	if r.toolEvents != nil {
		r.liveMu.Lock()
		r.liveToolCancel = r.toolEvents.Subscribe(func(event subagentsession.SubagentToolEvent) {
			select {
			case liveCh <- toolLiveEvent(event):
			default:
			}
		})
		r.liveMu.Unlock()
	}
}

// SubscribeSubagentLive 订阅 node 第一视角实时流：返回**历史回放**
// （从 subagent start 到订阅时刻的有界事件缓冲）+ 只读实时通道 + 取消函数
// （取消幂等）。阶段与工具事件到达即投递（即时输出，非轮询/缓存）。
func (r *Runtime) SubscribeSubagentLive(nodeID string) ([]dto.SubagentLiveEvent, <-chan dto.SubagentLiveEvent, func(), error) {
	if r == nil || nodeID == "" {
		return nil, nil, nil, fmt.Errorf("live subscribe: node id required")
	}
	r.startLiveDispatcher()
	ch := make(chan dto.SubagentLiveEvent, subagentLiveChanCap)
	r.liveMu.Lock()
	// 注册通道与取历史快照在同一把锁内：此后新事件既进历史、也广播给 ch，
	// 不会出现"快照之后、注册之前"的缺口。
	history := append([]dto.SubagentLiveEvent(nil), r.liveHistory[nodeID]...)
	r.liveSubs[nodeID] = append(r.liveSubs[nodeID], ch)
	r.liveMu.Unlock()
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			r.liveMu.Lock()
			subs := r.liveSubs[nodeID]
			for index, candidate := range subs {
				if candidate == ch {
					r.liveSubs[nodeID] = append(subs[:index], subs[index+1:]...)
					break
				}
			}
			r.liveMu.Unlock()
			close(ch)
		})
	}
	return history, ch, cancel, nil
}

// SubagentLiveHistoryPage 读回 node 第一视角实时回放的**一页**（有界窗口 + 分页）。
//
// 与 SubscribeSubagentLive 共用**同一份**窗口（r.liveHistory，超上限丢最旧），
// 订阅面的签名与返回值不变——这是 add but not modify 的另一种读法。
//
// 语义：
//   - offset 从**窗口内最旧一条**算起（0 = 最旧）；offset < 0 → 0；
//   - limit <= 0 → 默认 50；limit > 窗口上限 → 收敛到窗口；
//   - offset >= total → 空页且 HasMore=false（offset 原样回带，调用方看得出自己越界了）；
//   - Total = 窗口内条数；HasMore = offset+len(events) < Total；
//   - Events 顺序 = 窗口内由旧到新。
func (r *Runtime) SubagentLiveHistoryPage(nodeID string, offset, limit int) dto.SubagentLiveHistoryPage {
	page := dto.SubagentLiveHistoryPage{ScopeID: nodeID}
	if r == nil || nodeID == "" {
		return page
	}
	window := r.subagentLiveWindowSize()
	if limit <= 0 {
		limit = subagentLivePageDefaultLimit
	}
	if limit > window {
		limit = window
	}
	if offset < 0 {
		offset = 0
	}
	r.liveMu.Lock()
	// 与订阅面同一把锁：快照是"这一刻的窗口"，此后新事件继续进窗口（下一趟能翻到）。
	history := append([]dto.SubagentLiveEvent(nil), r.liveHistory[nodeID]...)
	r.liveMu.Unlock()
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

func (r *Runtime) broadcastLive(event dto.SubagentLiveEvent) {
	r.liveMu.Lock()
	subs := append([]chan dto.SubagentLiveEvent(nil), r.liveSubs[event.NodeID]...)
	history := append(r.liveHistory[event.NodeID], event)
	if window := r.subagentLiveWindowSize(); len(history) > window {
		history = history[len(history)-window:]
	}
	r.liveHistory[event.NodeID] = history
	r.liveMu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- event:
		default:
		}
	}
}

// recordNodeAssistant 投递子代理 assistant 正文增量（G7 正文增量 kind 的
// 数据源接线：AgentNode.Run 在节点 Session 的 ChatStream onChunk 边界调用，
// 证据见 node/agent_node.go——ChatStream 与 Chat 等价执行，仅额外给出流式
// 正文分片）。只在该节点已有详情订阅者（dispatcher 已启动）时保留实时面；
// 无人订阅时不积累，避免运行路径被实时面拖慢。
func (r *Runtime) recordNodeAssistant(nodeID, delta string) {
	if r == nil || nodeID == "" || strings.TrimSpace(delta) == "" {
		return
	}
	r.liveMu.Lock()
	started := r.liveStarted
	liveCh := r.liveCh
	liveStop := r.liveStop
	r.liveMu.Unlock()
	if !started {
		return
	}
	select {
	case liveCh <- assistantLiveEvent(nodeID, delta):
	case <-liveStop:
	default:
	}
}

func assistantLiveEvent(nodeID, delta string) dto.SubagentLiveEvent {
	return dto.SubagentLiveEvent{
		NodeID: nodeID,
		At:     time.Now(),
		Kind:   "assistant",
		Assistant: &dto.SubagentAssistant{
			Text: delta,
		},
	}
}

// stopLiveDispatcher 停止实时流分发（幂等；Shutdown 调用）。
func (r *Runtime) stopLiveDispatcher() {
	r.liveMu.Lock()
	defer r.liveMu.Unlock()
	if !r.liveStarted {
		return
	}
	if r.liveToolCancel != nil {
		r.liveToolCancel()
		r.liveToolCancel = nil
	}
	select {
	case <-r.liveStop:
	default:
		close(r.liveStop)
	}
	r.liveStarted = false
}

func stageLiveEvent(log model.NodeStageLog) dto.SubagentLiveEvent {
	return dto.SubagentLiveEvent{
		NodeID: log.NodeID, At: log.At, Kind: "stage",
		Stage: &log, // model.NodeStageLog 与 dto.NodeStageLog 同一类型（alias 单源）
	}
}

func toolLiveEvent(event subagentsession.SubagentToolEvent) dto.SubagentLiveEvent {
	tool := mapper.ToolEventToDTO(event)
	return dto.SubagentLiveEvent{
		NodeID: event.NodeID, At: time.Now(), Kind: "tool",
		Tool: &tool,
	}
}
