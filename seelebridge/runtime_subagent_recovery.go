package seelebridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	frameworkevent "github.com/RedHuang-0622/Seele/event"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// 子代理会话记录的生命周期（用户约定，2026-08-24）：
//
//   - 运行期落盘：`<mainSessionID>-<subSessionID>.json` 放在主会话目录
//     `subagents/` 下（进程中断/崩溃可从最近一次记录恢复）；
//   - 结束（done/failed）收敛：最终结论（语义结果/终态）写入主会话事件库
//     （Source=seelex.subagent.result，"结论跟随 mainagent"），随后删除
//     节点自己的记录文件；详情数据面保留在内存快照，进程存活期内可读；
//   - 恢复锚点：重启后从主会话事件库重建子代理树（结论事件）与 worktable
//     认领（Assignee → subagent:<节点会话ID>），崩溃遗留节点从残留记录恢复。

// subagentConclusionEventSource 是子代理最终结论在统一事件库中的稳定 Source。
const subagentConclusionEventSource = "seelex.subagent.result"

// subagentConclusion 是子代理最终结论的事件载荷（跟随 mainagent 持久化）。
type subagentConclusion struct {
	NodeID    string          `json:"node_id"`
	SessionID string          `json:"session_id"`
	Status    string          `json:"status"` // done | failed
	Goal      string          `json:"goal,omitempty"`
	Summary   string          `json:"summary,omitempty"`
	Error     string          `json:"error,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
}

// AttachSubSessionStore 装配子代理会话记录持久化（Router 就绪后注入）：
// 运行期落盘 + 结束时结论回传主会话 + 记录删除。幂等。
func (r *Runtime) AttachSubSessionStore(store *sessionstore.NodeSessionStore) {
	if r == nil || r.subagentSessions == nil || store == nil {
		return
	}
	r.nodeSessionStore = store
	r.subagentSessions.Configure(store, r.MainSessionID, func() string {
		return r.sessionProjectIDFor(r.MainSessionID())
	}, r.persistSubagentConclusion)
}

// sessionProjectIDFor 返回会话绑定的项目 ID（无绑定 = 默认项目 ""）。角色/
// 子代理记录按该稳定键落盘，不读 Router active workspace —— fork 执行期
// 可能切换 active scope，读取它会写到另一个项目。
func (r *Runtime) sessionProjectIDFor(sessionID string) string {
	if r == nil || strings.TrimSpace(sessionID) == "" {
		return ""
	}
	r.sessionWorkspacesMu.RLock()
	defer r.sessionWorkspacesMu.RUnlock()
	return r.sessionWorkspaces[sessionID]
}

// persistSubagentConclusion 把子代理最终结论写入主会话事件库
// （"结论跟随 mainagent"）：事件携带 agent.runtime session_id 定位，
// 与 A/B 类事实同一 sessionstore 事件库；best-effort。
func (r *Runtime) persistSubagentConclusion(mainSessionID string, record sessionstore.NodeSessionRecord) {
	if r == nil || mainSessionID == "" || record.NodeID == "" {
		return
	}
	r.eventPersisterMu.Lock()
	persister := r.eventPersister
	r.eventPersisterMu.Unlock()
	if persister == nil {
		return
	}
	conclusion := subagentConclusion{
		NodeID: record.NodeID, SessionID: record.SessionID, Status: record.Status,
		Goal: record.Goal, Summary: record.Summary, Error: record.Error,
		Result: record.ResultJSON,
	}
	content, _ := json.Marshal(conclusion)
	at := time.Now()
	event := frameworkevent.Event{
		Sequence:   uint64(at.UnixNano()),
		OccurredAt: at,
		Source:     subagentConclusionEventSource,
		Type:       frameworkevent.TypeLifecycle,
		Status:     frameworkevent.StatusCompleted,
		Scope:      frameworkevent.Scope{NodeID: record.NodeID},
		Content:    content,
		Locations: []frameworkevent.Location{{
			Kind: "agent.runtime",
			IDs:  map[string]string{"session_id": mainSessionID},
		}},
	}
	if record.Status == "failed" {
		event.Status = frameworkevent.StatusFailed
	}
	if err := persister(context.Background(), event); err != nil {
		log.Printf("seelebridge: persist subagent conclusion %q: %v", record.NodeID, err)
	}
}

// RestoreSubagentAnchors 从持久化重建目标会话的子代理恢复锚点：
//  1. 崩溃遗留节点：nodeStore 残留记录 → SubagentSessions 详情数据面 +
//     SubagentTree 树节点 + WorktreeManager worktree 现场；
//  2. 已完成节点：主会话事件库的结论事件（seelex.subagent.result）→
//     重建树节点（含 subSessionID），application 工作表格刷新后认领回填
//     subagent:<节点会话ID>，不再停留 main。
//
// 无存储装配时 no-op（保持既有纯内存行为）。
func (r *Runtime) RestoreSubagentAnchors(sessionID string) error {
	if r == nil || sessionID == "" {
		return nil
	}
	router := r.durableHistoryRouter()
	if router == nil {
		return nil
	}
	projectID := r.sessionProjectIDFor(sessionID)
	records := []sessionstore.NodeSessionRecord{}
	if r.nodeSessionStore != nil {
		var err error
		records, err = r.nodeSessionStore.List(projectID, sessionID)
		if err != nil {
			return fmt.Errorf("restore subagent anchors: list node sessions: %w", err)
		}
	}
	if r.subagentSessions != nil {
		r.subagentSessions.Restore(records)
	}
	if r.subagentTree != nil {
		r.subagentTree.Restore(records)
	}
	if r.worktreeMgr != nil {
		r.worktreeMgr.Restore(records)
	}
	conclusions, err := loadSubagentConclusions(context.Background(), router, projectID, sessionID)
	if err != nil {
		return fmt.Errorf("restore subagent anchors: load conclusions: %w", err)
	}
	if r.subagentTree != nil {
		r.subagentTree.Restore(conclusions)
	}
	return nil
}

// loadSubagentConclusions 从主会话事件库读取子代理最终结论事件。
func loadSubagentConclusions(ctx context.Context, router *sessionstore.Router, projectID, sessionID string) ([]sessionstore.NodeSessionRecord, error) {
	entries, err := router.ReadFrameworkEventsWorkspace(ctx, projectID, sessionID)
	if err != nil {
		return nil, err
	}
	records := make([]sessionstore.NodeSessionRecord, 0, len(entries))
	for _, entry := range entries {
		var event frameworkevent.Event
		if err := json.Unmarshal(entry.Payload, &event); err != nil {
			continue
		}
		if event.Source != subagentConclusionEventSource || len(event.Content) == 0 {
			continue
		}
		var conclusion subagentConclusion
		if err := json.Unmarshal(event.Content, &conclusion); err != nil || conclusion.NodeID == "" {
			continue
		}
		records = append(records, sessionstore.NodeSessionRecord{
			SchemaVersion: sessionstore.NodeSessionSchemaVersion,
			NodeID:        conclusion.NodeID,
			SessionID:     conclusion.SessionID,
			Goal:          conclusion.Goal,
			Status:        conclusion.Status,
			Summary:       conclusion.Summary,
			Error:         conclusion.Error,
			ResultJSON:    conclusion.Result,
		})
	}
	return records, nil
}
