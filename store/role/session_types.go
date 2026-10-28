package role

import (
	"fmt"
	"slices"

	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
	storecontext "github.com/RedHuang-0622/seelex/store/context"
	storesession "github.com/RedHuang-0622/seelex/store/session"
)

// BuildSnapshot projects raw main/role facts into the role inspection view.
func BuildSnapshot(
	mainSessionID, roleName, roleSessionID, root string,
	roleLifecycle, mainLifecycle storesession.LifecycleHead,
	mainHead storesession.MessageHead,
	mainRows, roleRows []protocol.TranscriptEvent,
	draftRows []DraftRow,
) Snapshot {
	snapshot := Snapshot{
		MainSessionID: mainSessionID, RoleName: roleName, RoleSessionID: roleSessionID, Root: root,
		JoinSeqID: roleLifecycle.JoinSeqID, CompactRef: cloneCompactRef(roleLifecycle.CompactRef),
		OrderPolicy: mainLifecycle.OrderPolicy, OrderRoles: append([]string(nil), mainLifecycle.OrderRoles...),
		MainHeadCommitID: mainHead.LastCommitID, MainHeadSeq: mainHead.LastSeq, Floor: cloneFloor(mainHead.Floor),
		MainRows: mainRows, RoleRows: roleRows, DraftRows: draftRows,
	}
	if roleName != RoleMain {
		snapshot.PrefixCutSeq = snapshot.JoinSeqID
		if snapshot.CompactRef != nil && snapshot.CompactRef.AppliedSeq > snapshot.PrefixCutSeq {
			snapshot.PrefixCutSeq = snapshot.CompactRef.AppliedSeq
		}
	}
	for _, row := range mainRows {
		if row.RoleName == "" {
			snapshot.UnassignedRoleRows++
		}
		if row.Seq > snapshot.PrefixCutSeq {
			snapshot.VisibleMainRows++
		} else {
			snapshot.OutsidePrefixMainRows++
		}
	}
	snapshot.DesignWarnings = SnapshotWarnings(snapshot)
	return snapshot
}

// ResolveWireWindow returns the applied sequence and the main compact frame that
// are valid for this role. A role may only use a frame it explicitly references.
func ResolveWireWindow(roleName string, lifecycle storesession.LifecycleHead, compact storesession.CompactHead) (uint64, *storesession.CompactFrameRecord, error) {
	applied := lifecycle.JoinSeqID
	if lifecycle.CompactRef != nil && lifecycle.CompactRef.AppliedSeq > applied {
		applied = lifecycle.CompactRef.AppliedSeq
	}
	frame := compact.LatestFrame
	if roleName == RoleMain {
		return applied, frame, nil
	}
	switch {
	case lifecycle.CompactRef == nil:
		if frame != nil && frame.MessageToSeq > lifecycle.JoinSeqID {
			return 0, nil, fmt.Errorf("session storage: role %s missing compact_ref for main frame %s", roleName, frame.FrameID)
		}
		return applied, nil, nil
	case frame == nil:
		return 0, nil, fmt.Errorf("session storage: role %s references missing compact frame %s", roleName, lifecycle.CompactRef.FrameID)
	case frame.FrameID != lifecycle.CompactRef.FrameID:
		return 0, nil, fmt.Errorf("session storage: role %s compact_ref %s != main frame %s", roleName, lifecycle.CompactRef.FrameID, frame.FrameID)
	default:
		return applied, frame, nil
	}
}

// MergeWirePending appends this role's unsynced draft using provisional seqs and
// returns attribution warnings. It never writes the rows back to storage.
func MergeWirePending(roleName, roleSessionID string, rows []protocol.TranscriptEvent, head storesession.MessageHead, draftRows []DraftRow) ([]protocol.TranscriptEvent, int, []string) {
	draftRows = SortDraftRows(draftRows, nil)
	pending := RowsToEvents(draftRows)
	for index := range pending {
		pending[index].Seq = head.LastSeq + uint64(index) + 1
	}
	combined := append(append([]protocol.TranscriptEvent(nil), rows...), pending...)
	var warnings []string
	for _, row := range rows {
		if row.RoleName == "" {
			warnings = append(warnings, "role wire 已发布行缺 role_name")
			break
		}
	}
	for _, row := range draftRows {
		if row.RoleName != roleName || row.RoleSessionID != roleSessionID {
			warnings = append(warnings, "pending draft 的 role 归属与装配角色不一致")
			break
		}
	}
	return combined, len(pending), warnings
}

// BuildWireSnapshot projects the shared context wire result into the role view.
func BuildWireSnapshot(mainSessionID, roleName, roleSessionID string, applied uint64, result storecontext.WireResult, pending int, warnings []string) WireSnapshot {
	return WireSnapshot{
		MainSessionID: mainSessionID, RoleName: roleName, RoleSessionID: roleSessionID,
		AppliedSeq: applied, Messages: WireMessages(result.Messages), NeedCompact: result.NeedCompact,
		PrefixDigest: result.PrefixDigest, TailStartSeq: result.TailStartSeq,
		FrameApplied: result.FrameApplied, Open: result.Open, PendingRows: pending,
		DesignWarnings: append([]string(nil), warnings...),
	}
}

func cloneCompactRef(ref *storesession.CompactRef) *storesession.CompactRef {
	if ref == nil {
		return nil
	}
	copy := *ref
	return &copy
}

func cloneFloor(floor *storesession.Floor) *storesession.Floor {
	if floor == nil {
		return nil
	}
	copy := *floor
	return &copy
}

// WireMessages projects context wire rows into the role-facing shape.
func WireMessages(messages []storecontext.WireMessage) []WireMessage {
	out := make([]WireMessage, 0, len(messages))
	for _, message := range messages {
		out = append(out, WireMessage{
			Role: message.Role, Content: message.Content,
			ReasoningContent: message.ReasoningContent,
			ToolCalls:        message.ToolCalls, ToolCallID: message.ToolCallID,
			Name: message.Name, ResultRef: message.ResultRef, Seq: message.Seq,
			Internal: message.Internal, Repair: message.Repair, Attempt: message.Attempt,
		})
	}
	return out
}

// SnapshotWarnings reports observable design drift without repairing storage.
func SnapshotWarnings(snapshot Snapshot) []string {
	var warnings []string
	if snapshot.Floor == nil {
		warnings = append(warnings, "message head 缺 floor：群聊发言权未随 head 发布")
	} else {
		if snapshot.Floor.Seq > snapshot.MainHeadSeq {
			warnings = append(warnings, "floor.seq 越界：大于 message head.last_seq")
		}
		if len(snapshot.OrderRoles) > 0 && !slices.Contains(snapshot.OrderRoles, snapshot.Floor.RoleName) {
			warnings = append(warnings, "floor.role_name 不在 lifecycle.order_roles")
		}
	}
	seenRole := false
	unassigned := 0
	for _, row := range snapshot.MainRows {
		if row.RoleName == "" {
			unassigned++
		}
		if row.Seq <= snapshot.JoinSeqID {
			continue
		}
		if row.RoleName == "" {
			warnings = append(warnings, "join_seq_id 之后的 message 行缺 role_name")
		} else if row.RoleSessionID == "" {
			warnings = append(warnings, "message 行有 role_name 但缺 role_session_id")
		}
		if row.RoleName == snapshot.RoleName {
			seenRole = true
			if row.RoleSessionID != "" && row.RoleSessionID != snapshot.RoleSessionID {
				warnings = append(warnings, "message 行的 role_session_id 与查询角色不一致")
			}
		}
	}
	if unassigned > 0 {
		warnings = append(warnings, "存在 message 行缺 role_name/role_session_id：R4 应用层生产者尚未覆盖全部行")
	}
	if snapshot.RoleName != RoleMain && !seenRole && len(snapshot.RoleRows) > 0 {
		warnings = append(warnings, "角色备份存在但 main message 尚无该角色已 sync 行")
	}
	return warnings
}

// Snapshot is the read-only inspection view of one role session.
type Snapshot struct {
	MainSessionID string `json:"main_session_id"`
	RoleName      string `json:"role_name"`
	RoleSessionID string `json:"role_session_id"`
	Root          string `json:"root,omitempty"`

	JoinSeqID   uint64                   `json:"join_seq_id,omitempty"`
	CompactRef  *storesession.CompactRef `json:"compact_ref,omitempty"`
	OrderPolicy string                   `json:"order_policy,omitempty"`
	OrderRoles  []string                 `json:"order_roles,omitempty"`

	MainHeadCommitID string                     `json:"main_head_commit_id,omitempty"`
	MainHeadSeq      uint64                     `json:"main_head_seq,omitempty"`
	Floor            *storesession.Floor        `json:"floor,omitempty"`
	MainRows         []protocol.TranscriptEvent `json:"main_rows,omitempty"`
	RoleRows         []protocol.TranscriptEvent `json:"role_rows,omitempty"`
	DraftRows        []DraftRow                 `json:"draft_rows,omitempty"`

	PrefixCutSeq          uint64   `json:"prefix_cut_seq,omitempty"`
	VisibleMainRows       int      `json:"visible_main_rows,omitempty"`
	OutsidePrefixMainRows int      `json:"outside_prefix_main_rows,omitempty"`
	UnassignedRoleRows    int      `json:"unassigned_role_rows,omitempty"`
	DesignWarnings        []string `json:"design_warnings,omitempty"`
}

// WireMessage is the role-visible provider-shaped wire row.
type WireMessage struct {
	Role             string                        `json:"role"`
	Content          string                        `json:"content,omitempty"`
	ReasoningContent string                        `json:"reasoning_content,omitempty"`
	ToolCalls        []protocol.TranscriptToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string                        `json:"tool_call_id,omitempty"`
	Name             string                        `json:"name,omitempty"`
	ResultRef        string                        `json:"result_ref,omitempty"`
	Seq              uint64                        `json:"seq,omitempty"`
	Internal         bool                          `json:"internal,omitempty"`
	Repair           bool                          `json:"repair,omitempty"`
	Attempt          bool                          `json:"attempt,omitempty"`
}

// WireSnapshot is the read-only materialized role wire view.
type WireSnapshot struct {
	MainSessionID  string        `json:"main_session_id"`
	RoleName       string        `json:"role_name"`
	RoleSessionID  string        `json:"role_session_id"`
	AppliedSeq     uint64        `json:"applied_seq"`
	Messages       []WireMessage `json:"messages"`
	NeedCompact    bool          `json:"need_compact"`
	PrefixDigest   string        `json:"prefix_digest"`
	TailStartSeq   uint64        `json:"tail_start_seq"`
	FrameApplied   bool          `json:"frame_applied,omitempty"`
	Open           bool          `json:"open,omitempty"`
	PendingRows    int           `json:"pending_rows,omitempty"`
	DesignWarnings []string      `json:"design_warnings,omitempty"`
}
