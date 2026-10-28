package session

// conclusion.go 是**子代理最终结论的持久化投影**（用户约定，2026-08-24）。
//
// 落点沿革（P21-14「域方法随类型走」）：从 `backend/engine/assemble/runtime_subagent_recovery.go`
// 把结论事件的**形状与两向投影**搬进子代理域——plan §2.7 把
// `seelebridge/runtime_subagent_{recovery,resume,session}.go` 列为 `backend/multiagent/subagent/`
// 的目标。装配层只剩"谁来落盘 / 从哪读"的接线。
//
// 生命周期（本文件负责其中"结论"这一段）：
//
//   - 运行期落盘：`<mainSessionID>-<subSessionID>.json` 放在主会话目录 `subagents/` 下
//     （进程中断/崩溃可从最近一次记录恢复）；
//   - 结束（done/failed）收敛：最终结论（语义结果/终态）写入主会话事件库
//     （Source=seelex.subagent.result，"结论跟随 mainagent"），随后删除节点自己的记录文件；
//     详情数据面保留在内存快照，进程存活期内可读；
//   - 恢复锚点：重启后从主会话事件库重建子代理树（结论事件）与 worktable
//     认领（Assignee → subagent:<节点会话ID>），崩溃遗留节点从残留记录恢复。

import (
	"encoding/json"
	"time"

	frameworkevent "github.com/RedHuang-0622/Seele/event"

	"github.com/RedHuang-0622/seelex/contract/dto"
	storeworkunit "github.com/RedHuang-0622/seelex/store/workunit"
)

// ConclusionEventSource 是子代理最终结论在统一事件库中的稳定 Source
// （与 workplan.runner / seelex.jobs / seelex.telemetry.summary 同库共存）。
const ConclusionEventSource = "seelex.subagent.result"

// Conclusion 是子代理最终结论的事件载荷（跟随 mainagent 持久化）。
type Conclusion struct {
	NodeID    string          `json:"node_id"`
	SessionID string          `json:"session_id"`
	Status    string          `json:"status"` // done | failed
	Goal      string          `json:"goal,omitempty"`
	Summary   string          `json:"summary,omitempty"`
	Error     string          `json:"error,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
}

// ConclusionEvent 把一条节点会话记录投影为**结论事实事件**：
//
//   - 序号取时间基（`uint64(at.UnixNano())`，与 Seelex 自己的事件同策略——本库按会话排序）；
//   - 终态映射：记录判失败 ⇒ `StatusFailed`，否则 `Completed`；
//   - 定位：`agent.runtime.session_id` 挂**主会话**（结论跟随 mainagent），
//     `Scope.NodeID` 记节点自身（读侧据此重建树节点）。
//
// 调用方负责"没落点就别造事件"的守卫（主会话号或节点号为空时不该调用它）。
func ConclusionEvent(mainSessionID string, record storeworkunit.Record, at time.Time) frameworkevent.Event {
	content, _ := json.Marshal(Conclusion{
		NodeID: record.NodeID, SessionID: record.SessionID, Status: record.Status,
		Goal: record.Goal, Summary: record.Summary, Error: record.Error,
		Result: record.ResultJSON,
	})
	status := frameworkevent.StatusCompleted
	if state, ok := dto.ParseSubAgentNodeStatus(record.Status); ok && state == dto.SubAgentFailed {
		status = frameworkevent.StatusFailed
	}
	return frameworkevent.Event{
		Sequence:   uint64(at.UnixNano()),
		OccurredAt: at,
		Source:     ConclusionEventSource,
		Type:       frameworkevent.TypeLifecycle,
		Status:     status,
		Scope:      frameworkevent.Scope{NodeID: record.NodeID},
		Content:    content,
		Locations: []frameworkevent.Location{{
			Kind: "agent.runtime",
			IDs:  map[string]string{"session_id": mainSessionID},
		}},
	}
}

// ConclusionsFromEvents 从主会话事件库的条目里读出结论记录（**结论事件的反向投影**，
// 与 `ConclusionEvent` 是同一条契约的两向）。
//
// 跳过读不懂的条目（不是事件 / 不是本 Source / 载荷解析不出 / 没带节点号）：事件库是
// A/B 类事实共用的追加日志，恢复链只认自己那一族；跳过而不报错，是"库里有别家事实"的
// 正常情形，不是损坏。
func ConclusionsFromEvents(entries []dto.EventLogEntry) []storeworkunit.Record {
	records := make([]storeworkunit.Record, 0, len(entries))
	for _, entry := range entries {
		var event frameworkevent.Event
		if err := json.Unmarshal(entry.Payload, &event); err != nil {
			continue
		}
		if event.Source != ConclusionEventSource || len(event.Content) == 0 {
			continue
		}
		var conclusion Conclusion
		if err := json.Unmarshal(event.Content, &conclusion); err != nil || conclusion.NodeID == "" {
			continue
		}
		records = append(records, storeworkunit.Record{
			SchemaVersion: storeworkunit.SchemaVersion,
			NodeID:        conclusion.NodeID,
			SessionID:     conclusion.SessionID,
			Goal:          conclusion.Goal,
			Status:        conclusion.Status,
			Summary:       conclusion.Summary,
			Error:         conclusion.Error,
			ResultJSON:    conclusion.Result,
		})
	}
	return records
}

// TakeoverPredicate 返回恢复链上的**接管判据**：只有"记录的主人已经不在本进程里"的记录
// 才按崩溃口径接管（running/queued → interrupted）。本进程仍在跑的节点由内存态继续表达，
// 恢复不得把它覆盖成中断。
//
// `stillLive` 是宿主读数（节点号 → 是否还在本进程的在册执行面）；nil = 没有内存判据 ⇒
// 一律按记录恢复（保持既有语义）。
func TakeoverPredicate(stillLive func(nodeID string) bool) func(storeworkunit.Record) bool {
	return func(record storeworkunit.Record) bool {
		if stillLive == nil {
			return true
		}
		return !stillLive(record.NodeID)
	}
}
