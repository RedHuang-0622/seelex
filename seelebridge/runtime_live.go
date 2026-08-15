package seelebridge

import (
	"fmt"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/mapper"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	subagentsession "github.com/RedHuang-0622/seelex/seelebridge/session"
)

// subagentLiveChanCap 是实时流订阅通道/分发通道的容量。
const subagentLiveChanCap = 128

// subagentLiveHistoryCap 是每节点实时事件历史缓冲上限（超出淘汰最旧；
// 打开详情时回放该缓冲，保证"从 start 到最新"的滚动上下文）。
const subagentLiveHistoryCap = 512

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

func (r *Runtime) broadcastLive(event dto.SubagentLiveEvent) {
	r.liveMu.Lock()
	subs := append([]chan dto.SubagentLiveEvent(nil), r.liveSubs[event.NodeID]...)
	history := append(r.liveHistory[event.NodeID], event)
	if len(history) > subagentLiveHistoryCap {
		history = history[len(history)-subagentLiveHistoryCap:]
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
