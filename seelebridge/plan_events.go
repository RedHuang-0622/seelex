package seelebridge

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	frameworkevent "github.com/RedHuang-0622/Seele/event"
	workplanTypes "github.com/RedHuang-0622/Seele/workplan/core/types"
)

// mainAgentID 是主会话在 agent.EventLocator 中的稳定标识（事件定位用）。
const mainAgentID = "seelex-main"

// PlanNodeEvent 是 seelex 侧节点/计划投影事件（不含 Seele 运行时类型）。
// NodeID 为空表示计划级状态（PlanStatus 投影），否则为节点级状态
// （NodeStatus 投影）。Status 取值与 PlanNodeStatus 对齐：
// queued | running | completed | failed | aborted | skipped | canceled | panicked。
type PlanNodeEvent struct {
	PlanID  string
	RunID   string
	NodeID  string
	Kind    string // 展示用 kind（approve 由前端映射为 manual）
	Status  string
	Output  string
	Elapsed string
	At      time.Time
}

// planEventSink 实现 event.Sink：追加到内存事件库（append-only），
// 并把事件投影为 PlanNodeEvent 转发给订阅者（HandlePlanNodeComplete
// 从框架回调改为投影订阅）。持久化（sessionstore 事件库）经 persister
// 钩子接入：slice 4 保持内存事件库 + 投影订阅，会话级事件落库随
// 会话恢复流程（slice 6+）接线。
type planEventSink struct {
	mu         sync.Mutex
	store      []frameworkevent.Event
	persister  func(context.Context, frameworkevent.Event) error
	subscriber func(PlanNodeEvent)
}

func newPlanEventSink() *planEventSink {
	return &planEventSink{store: make([]frameworkevent.Event, 0)}
}

// Append 实现 event.Sink：入库（可选持久化钩子同步写）后投影给订阅者。
// 框架事件本身不携带 kind/elapsed，节点级投影的完整字段由
// AppendNodeResult（runner NodeHook 路径）补充。
func (s *planEventSink) Append(ctx context.Context, ev frameworkevent.Event) error {
	if s == nil {
		return nil
	}
	if err := s.storeEvent(ctx, ev); err != nil {
		return err
	}
	if projected := planEventProjection(ev); projected != nil {
		s.publish(*projected)
	}
	return nil
}

// storeEvent 把事件追加到事件库并执行可选持久化钩子（不投影）。
func (s *planEventSink) storeEvent(ctx context.Context, ev frameworkevent.Event) error {
	s.mu.Lock()
	s.store = append(s.store, ev)
	persister := s.persister
	s.mu.Unlock()
	if persister != nil {
		return persister(ctx, ev)
	}
	return nil
}

// AppendNodeResult 记录 runner NodeHook 的节点完成结果：合成框架形态事件
// 入库（保持事件库完整），并向订阅者投影一次含 kind/elapsed 的节点级事件
// （避免与入库事件的自动投影重复）。
func (s *planEventSink) AppendNodeResult(ctx context.Context, planID, runID string, nr *workplanTypes.NodeResult) {
	if s == nil || nr == nil {
		return
	}
	status := frameworkevent.StatusCompleted
	if nr.Err != nil {
		status = frameworkevent.StatusFailed
	}
	content, _ := json.Marshal(nr.Output)
	_ = s.storeEvent(ctx, frameworkevent.Event{
		Source:     "workplan.runner",
		Type:       frameworkevent.TypeLifecycle,
		Status:     status,
		Scope:      frameworkevent.Scope{PlanID: planID, RunID: runID, NodeID: nr.NodeID},
		Content:    content,
		OccurredAt: nr.EndedAt,
	})
	s.publish(PlanNodeEvent{
		PlanID:  planID,
		RunID:   runID,
		NodeID:  nr.NodeID,
		Kind:    nr.Kind,
		Status:  nr.Status,
		Output:  nr.Output,
		Elapsed: nr.Elapsed().String(),
		At:      nr.EndedAt,
	})
}

// AppendPhase records a Seelex-owned subagent phase while preserving the same
// plan/run/session correlation contract as framework runner events.
func (s *planEventSink) AppendPhase(ctx context.Context, binding PlanBranchBinding, runID, nodeID, status string) {
	if s == nil || nodeID == "" || status == "" {
		return
	}
	at := time.Now()
	ev := frameworkevent.Event{
		ID:         fmt.Sprintf("subagent-phase-%s-%d", nodeID, at.UnixNano()),
		Sequence:   uint64(at.UnixNano()),
		OccurredAt: at,
		Source:     "seelex.subagent",
		Type:       frameworkevent.TypeLifecycle,
		Status:     frameworkevent.Status(status),
		Scope: frameworkevent.Scope{
			PlanID: binding.PlanID, RunID: runID, NodeID: nodeID, BranchID: nodeID,
		},
	}
	if binding.SessionID != "" {
		ev.Locations = []frameworkevent.Location{{
			Kind: "agent.runtime",
			IDs: map[string]string{
				"agent_id": mainAgentID, "session_id": binding.SessionID,
			},
		}}
	}
	_ = s.Append(ctx, ev)
}

// Subscribe 注册投影订阅者（唯一；后注册者覆盖先注册者）。
func (s *planEventSink) Subscribe(fn func(PlanNodeEvent)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.subscriber = fn
	s.mu.Unlock()
}

// SetPersister 安装事件持久化钩子（sessionstore 事件库接线点）。
func (s *planEventSink) SetPersister(fn func(context.Context, frameworkevent.Event) error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.persister = fn
	s.mu.Unlock()
}

// Events 返回事件库的只读拷贝（审计/测试）。
func (s *planEventSink) Events() []frameworkevent.Event {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]frameworkevent.Event(nil), s.store...)
}

// publish 把投影事件转发给订阅者（并发安全）。
func (s *planEventSink) publish(projected PlanNodeEvent) {
	s.mu.Lock()
	subscriber := s.subscriber
	s.mu.Unlock()
	if subscriber != nil {
		subscriber(projected)
	}
}

// planEventProjection 把框架生命周期事件投影为 PlanNodeEvent：
// 计划级（无 NodeID）→ PlanStatus 投影；节点级（有 NodeID，Resume 路径）
// → NodeStatus 投影（kind/elapsed 未知，由 AppendNodeResult 路径补齐）。
// heartbeat 投影为 running（子代理详情页的"最后活跃"时间线刷新源）。
func planEventProjection(ev frameworkevent.Event) *PlanNodeEvent {
	if ev.Type == frameworkevent.TypeHeartbeat {
		if ev.Scope.NodeID == "" {
			return nil
		}
		return &PlanNodeEvent{
			PlanID: ev.Scope.PlanID,
			RunID:  ev.Scope.RunID,
			NodeID: ev.Scope.NodeID,
			Status: string(frameworkevent.StatusRunning),
			At:     ev.OccurredAt,
		}
	}
	if ev.Type != frameworkevent.TypeLifecycle {
		return nil
	}
	projected := &PlanNodeEvent{
		PlanID: ev.Scope.PlanID,
		RunID:  ev.Scope.RunID,
		NodeID: ev.Scope.NodeID,
		Status: string(ev.Status),
		At:     ev.OccurredAt,
	}
	if len(ev.Content) > 0 {
		var output string
		if err := json.Unmarshal(ev.Content, &output); err == nil {
			projected.Output = output
		} else {
			projected.Output = string(ev.Content)
		}
	}
	return projected
}
