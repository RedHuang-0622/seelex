// 群聊角色会话的根包兼容面（M9）。
//
// 唯一实现在 store/role：draft WAL（append-only、文件存在即待同步、sync 成功
// 后立即删除、崩溃残尾截断）、角色会话子树路径与提交身份、permission 登记。
// 本文件只保留四类东西：
//   - 同身份 alias（类型与角色常量）；
//   - 组合根 lifecycle：建角色子树的装配步骤（建目录 → message 提交 →
//     role_session 结构化事件 → join_seq_id）；
//   - 观察面（RoleSnapshot / RoleWireSnapshot）：需要根包既有的 message、event、
//     compact 读通道，领域包不持这些机制；
//   - 向 store/role 的单层转发与 main message 端口适配（sequencer 唯一写入口）。
package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"

	storerole "github.com/RedHuang-0622/seelex/store/role"
	storesession "github.com/RedHuang-0622/seelex/store/session"
)

const (
	// RoleUser / RoleMain / RoleTL 是角色名常量（身份由 store/role 定义）。
	RoleUser = storerole.RoleUser
	RoleMain = storerole.RoleMain
	RoleTL   = storerole.RoleTL
)

// RoleDraftRow 是一行未同步角色草稿（= store/role.DraftRow，落盘 tag 不变）。
type RoleDraftRow = storerole.DraftRow

// RoleDraftSyncResult 是一次 sequencer sync 的结果（= store/role.SyncResult）。
type RoleDraftSyncResult = storerole.SyncResult

// RoleSessionInfo 是角色会话的基础信息（接口返回，不含引擎内部对象）。
type RoleSessionInfo struct {
	MainSessionID string `json:"main_session_id"`
	RoleName      string `json:"role_name"`
	RoleSessionID string `json:"role_session_id"`
	Root          string `json:"root"`
}

// RoleSnapshot 是 headless/巡检用的角色会话只读观察面：把 main 正文、
// 角色备份、未同步 draft、floor 与 join/compact 切点放在同一份快照里，供
// 真实 API 冒烟核对设计稿不变量（不写任何状态）。
type RoleSnapshot struct {
	MainSessionID string `json:"main_session_id"`
	RoleName      string `json:"role_name"`
	RoleSessionID string `json:"role_session_id"`
	Root          string `json:"root,omitempty"`

	JoinSeqID   uint64      `json:"join_seq_id,omitempty"`
	CompactRef  *CompactRef `json:"compact_ref,omitempty"`
	OrderPolicy string      `json:"order_policy,omitempty"`
	OrderRoles  []string    `json:"order_roles,omitempty"`

	MainHeadCommitID string `json:"main_head_commit_id,omitempty"`
	MainHeadSeq      uint64 `json:"main_head_seq,omitempty"`
	Floor            *Floor `json:"floor,omitempty"`

	MainRows  []Event        `json:"main_rows,omitempty"`
	RoleRows  []Event        `json:"role_rows,omitempty"`
	DraftRows []RoleDraftRow `json:"draft_rows,omitempty"`
	// PrefixCutSeq 是该角色**自己那份团队记录**的起点 seq：判据与 assembleRoleWire
	// 一致（join_seq_id，有 compact_ref 时取更大的 applied_seq；main 角色复用主会话
	// 本身，恒为 0、不受闸门）。seq <= PrefixCutSeq 的 main 行不属于它的前缀匹配
	// 区间——那是它入伙之前、或已被压缩掉的部分，只能以占位呈现。
	PrefixCutSeq uint64 `json:"prefix_cut_seq,omitempty"`
	// VisibleMainRows / OutsidePrefixMainRows 是按 PrefixCutSeq 切分的 main 行计数
	// （观察读数；正文仍以 MainRows 为准）。
	VisibleMainRows       int `json:"visible_main_rows,omitempty"`
	OutsidePrefixMainRows int `json:"outside_prefix_main_rows,omitempty"`
	// UnassignedRoleRows 是 main message 中缺 role_name 的行数。它直接暴露
	// “应用层群聊生产者尚未给所有行盖角色归属”的设计缺口。
	UnassignedRoleRows int `json:"unassigned_role_rows,omitempty"`

	// DesignWarnings 是存储层可判定的设计稿偏差（例如 message 行缺角色归属、
	// floor 越界、顺序表缺当前 floor 角色）。它只报事实，不自动修补。
	DesignWarnings []string `json:"design_warnings,omitempty"`
}

// RoleWireMessage 是角色可见 wire 的公开形态（与 provider 消息字段对齐）。
type RoleWireMessage struct {
	Role             string          `json:"role"`
	Content          string          `json:"content,omitempty"`
	ReasoningContent string          `json:"reasoning_content,omitempty"`
	ToolCalls        []EventToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string          `json:"tool_call_id,omitempty"`
	Name             string          `json:"name,omitempty"`
	ResultRef        string          `json:"result_ref,omitempty"`
	Seq              uint64          `json:"seq,omitempty"`
	Internal         bool            `json:"internal,omitempty"`
	Repair           bool            `json:"repair,omitempty"`
	Attempt          bool            `json:"attempt,omitempty"`
}

// RoleWireSnapshot 是 §5.7/R3 的角色物化 wire 观察面：main compact 引用 +
// seq > max(join_seq_id, compact_ref.applied_seq) 的已发布行 + 自身 pending
// draft。它只读，不写主文档。
type RoleWireSnapshot struct {
	MainSessionID string `json:"main_session_id"`
	RoleName      string `json:"role_name"`
	RoleSessionID string `json:"role_session_id"`
	AppliedSeq    uint64 `json:"applied_seq"`

	Messages     []RoleWireMessage `json:"messages"`
	NeedCompact  bool              `json:"need_compact"`
	PrefixDigest string            `json:"prefix_digest"`
	TailStartSeq uint64            `json:"tail_start_seq"`
	FrameApplied bool              `json:"frame_applied,omitempty"`
	Open         bool              `json:"open,omitempty"`
	PendingRows  int               `json:"pending_rows,omitempty"`

	DesignWarnings []string `json:"design_warnings,omitempty"`
}

// roleSessionEngine 返回角色会话的引擎与 key：main 复用当前引擎；非 main 以
// 角色子树为根装配**只含机制**的子引擎。子树不再递归装配 artifact/context/
// teamwork/role —— 那是同型子引擎的复制（M10 归零）。
func roleSessionEngine(store *storeEngine, mainKey Key, roleName, roleSessionID string) (*storeEngine, Key) {
	if roleName == RoleMain {
		return store, mainKey
	}
	root := storerole.SessionRoot(store.sessionRoot(mainKey), roleName, roleSessionID)
	return newSubsessionEngine(root, store.settings), Key{SessionID: roleSessionID}
}

// roleSessionRoot 返回角色会话子树的物理根（路径规则唯一实现在 store/role）。
func roleSessionRoot(store *storeEngine, mainKey Key, roleName, roleSessionID string) string {
	return storerole.SessionRoot(store.sessionRoot(mainKey), roleName, roleSessionID)
}

// mainSessionPort 把根 store 的 message 通道适配成 role.SessionPort：sequencer
// 唯一写 main message 的入口，floor 与 head 在同一提交内发布。
type mainSessionPort struct {
	store *storeEngine
	key   Key
}

func (port mainSessionPort) MessageHead() (storesession.MessageHead, error) {
	return port.store.readMessageHead(port.key)
}

func (port mainSessionPort) CommitSync(commitID string, rows []Event, floor *storesession.Floor) (storesession.MessageHead, error) {
	return port.store.messageCommitSync(port.key, commitID, rows, floor)
}

// syncRoleDraft 是 sequencer 的存储侧同步入口：读该角色 draft → 排序 → 幂等
// 判定 → append 进 main message 并原子发布 head + floor → 成功后删除 draft。
// 判据与写序全部在 store/role；本函数只把 main message 通道接成端口。
func syncRoleDraft(store *storeEngine, mainKey Key, roleName, roleSessionID string, order []string) (RoleDraftSyncResult, error) {
	roleStore, roleKey := roleSessionEngine(store, mainKey, roleName, roleSessionID)
	return storerole.SyncDraft(
		roleStore.sessionRoot(roleKey),
		roleName, roleSessionID, order,
		mainSessionPort{store: store, key: mainKey},
	)
}

// createRoleSession 创建非 main 角色会话（message/event/metadata 同构空现场），
// 并写 join_seq_id。main 角色不创建独立子树。这是组合根 lifecycle：目录与
// 提交顺序由这里定，draft/permission/路径判据都不在这里。
func createRoleSession(store *storeEngine, mainKey Key, roleName, roleSessionID string, joinSeq uint64) (*storeEngine, Key, error) {
	if roleName == "" || roleSessionID == "" {
		return nil, Key{}, errors.New("session storage: role_name and role_session_id are required")
	}
	if roleName == RoleMain {
		return nil, Key{}, errors.New("session storage: main role session is the main session itself")
	}
	if _, err := store.ensureLayoutGuide(mainKey); err != nil {
		return nil, Key{}, err
	}
	root := roleSessionRoot(store, mainKey, roleName, roleSessionID)
	childStore := newSubsessionEngine(root, store.settings)
	childKey := Key{SessionID: roleSessionID}
	if childStore.sessionExists(childKey) {
		return nil, Key{}, errors.New("session storage: role session already exists")
	}
	commitID := storerole.SessionCommitID(roleName, roleSessionID)
	if _, err := childStore.messageCommit(childKey, commitID, nil); err != nil {
		_ = os.RemoveAll(root)
		return nil, Key{}, err
	}
	if _, err := childStore.structuralEventCommit(childKey, commitID, []structuralEvent{{
		Kind:    structuralEventRoleSession,
		Payload: storerole.JoinPayload(mainKey.SessionID, roleName, roleSessionID, joinSeq),
	}}); err != nil {
		_ = os.RemoveAll(root)
		return nil, Key{}, err
	}
	if err := childStore.setRoleLifecycle(childKey, joinSeq, nil); err != nil {
		_ = os.RemoveAll(root)
		return nil, Key{}, err
	}
	return childStore, childKey, nil
}

// readRoleSessionRows 读取角色会话备份行（冷恢复/审计用）。main 角色读主会话。
func readRoleSessionRows(store *storeEngine, mainKey Key, roleName, roleSessionID string) ([]Event, error) {
	roleStore, roleKey := roleSessionEngine(store, mainKey, roleName, roleSessionID)
	return roleStore.readAllRows(roleKey)
}

// readRoleSnapshot 汇总角色会话观察面（只读）。
func readRoleSnapshot(store *storeEngine, mainKey Key, roleName, roleSessionID string) (RoleSnapshot, error) {
	if roleName == "" || roleSessionID == "" {
		return RoleSnapshot{}, errors.New("session storage: role_name and role_session_id are required")
	}
	roleStore, roleKey := roleSessionEngine(store, mainKey, roleName, roleSessionID)
	roleLifecycle, err := roleStore.readLifecycleHead(roleKey)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return RoleSnapshot{}, err
	}
	mainLifecycle, err := store.readLifecycleHead(mainKey)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return RoleSnapshot{}, err
	}
	mainHead, err := store.readMessageHead(mainKey)
	if err != nil {
		return RoleSnapshot{}, err
	}
	mainRows, err := store.readAllRows(mainKey)
	if err != nil {
		return RoleSnapshot{}, err
	}
	roleRows, err := roleStore.readAllRows(roleKey)
	if err != nil {
		return RoleSnapshot{}, err
	}
	draftRows, err := storerole.ReadDraft(roleStore.sessionRoot(roleKey), roleName)
	if err != nil {
		return RoleSnapshot{}, err
	}
	snapshot := RoleSnapshot{
		MainSessionID:    mainKey.SessionID,
		RoleName:         roleName,
		RoleSessionID:    roleSessionID,
		Root:             roleSessionRoot(store, mainKey, roleName, roleSessionID),
		JoinSeqID:        roleLifecycle.JoinSeqID,
		CompactRef:       cloneCompactRef(roleLifecycle.CompactRef),
		OrderPolicy:      mainLifecycle.OrderPolicy,
		OrderRoles:       append([]string(nil), mainLifecycle.OrderRoles...),
		MainHeadCommitID: mainHead.LastCommitID,
		MainHeadSeq:      mainHead.LastSeq,
		Floor:            cloneFloor(mainHead.Floor),
		MainRows:         mainRows,
		RoleRows:         roleRows,
		DraftRows:        draftRows,
	}
	// 该角色记录的起点（= 它的前缀匹配切点）：判据与 assembleRoleWire 一致——
	// join_seq_id，有 compact_ref 时取更大的 applied_seq；main 复用主会话本身，
	// 恒为 0、不受闸门。seq <= 切点的 main 行不属于它的记录。
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
	snapshot.DesignWarnings = roleSnapshotWarnings(snapshot)
	return snapshot, nil
}

// assembleRoleWire 构造角色可见 wire：main compact_ref + seq > 切点的 main 行
// + 该角色自身 pending draft（未 sync 不进主文档）。user draft 不在此路径。
func assembleRoleWire(store *storeEngine, mainKey Key, roleName, roleSessionID string, budget, k int) (RoleWireSnapshot, error) {
	if roleName == RoleUser {
		return RoleWireSnapshot{}, errors.New("session storage: user draft is input-box material, not role wire")
	}
	roleStore, roleKey := roleSessionEngine(store, mainKey, roleName, roleSessionID)
	lifecycle, err := roleStore.readLifecycleHead(roleKey)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return RoleWireSnapshot{}, err
	}
	compactHead, err := store.readCompactHead(mainKey)
	if err != nil {
		return RoleWireSnapshot{}, err
	}
	applied := lifecycle.JoinSeqID
	if lifecycle.CompactRef != nil && lifecycle.CompactRef.AppliedSeq > applied {
		applied = lifecycle.CompactRef.AppliedSeq
	}
	frame := compactHead.LatestFrame
	if roleName != RoleMain {
		switch {
		case lifecycle.CompactRef == nil:
			// 角色加入前发生的 compact 不属于该角色的可见区间；不把旧帧
			// 冒充 join 后共享内容（I18/I22）。
			if frame != nil && frame.MessageToSeq > lifecycle.JoinSeqID {
				return RoleWireSnapshot{}, fmt.Errorf("session storage: role %s missing compact_ref for main frame %s", roleName, frame.FrameID)
			}
			frame = nil
		case frame == nil:
			return RoleWireSnapshot{}, fmt.Errorf("session storage: role %s references missing compact frame %s", roleName, lifecycle.CompactRef.FrameID)
		case frame.FrameID != lifecycle.CompactRef.FrameID:
			return RoleWireSnapshot{}, fmt.Errorf("session storage: role %s compact_ref %s != main frame %s",
				roleName, lifecycle.CompactRef.FrameID, frame.FrameID)
		}
	}
	rows, err := store.readRows(mainKey, applied+1, 0)
	if err != nil {
		return RoleWireSnapshot{}, err
	}
	head, err := store.readMessageHead(mainKey)
	if err != nil {
		return RoleWireSnapshot{}, err
	}
	draftRows, err := storerole.ReadDraft(roleStore.sessionRoot(roleKey), roleName)
	if err != nil {
		return RoleWireSnapshot{}, err
	}
	draftRows = storerole.SortDraftRows(draftRows, nil)
	pending := storerole.RowsToEvents(draftRows)
	for index := range pending {
		// 只为本次装配提供稳定序号；真正全局 seq 只能由 sequencer 在 sync 时
		// 分配，pending 行不得写回 message。
		pending[index].Seq = head.LastSeq + uint64(index) + 1
	}
	combined := append(append([]Event(nil), rows...), pending...)
	result := assembleWireRows(frame, combined, nil, applyWireBudget(store.settings, wireParams{Budget: budget, K: k}))
	snapshot := RoleWireSnapshot{
		MainSessionID: mainKey.SessionID,
		RoleName:      roleName,
		RoleSessionID: roleSessionID,
		AppliedSeq:    applied,
		Messages:      roleWireMessages(result.Messages),
		NeedCompact:   result.NeedCompact,
		PrefixDigest:  result.PrefixDigest,
		TailStartSeq:  result.TailStartSeq,
		FrameApplied:  result.FrameApplied,
		Open:          result.Open,
		PendingRows:   len(pending),
	}
	for _, row := range rows {
		if row.RoleName == "" {
			snapshot.DesignWarnings = append(snapshot.DesignWarnings, "role wire 已发布行缺 role_name")
			break
		}
	}
	for _, row := range draftRows {
		if row.RoleName != roleName || row.RoleSessionID != roleSessionID {
			snapshot.DesignWarnings = append(snapshot.DesignWarnings, "pending draft 的 role 归属与装配角色不一致")
			break
		}
	}
	return snapshot, nil
}

func roleWireMessages(messages []wireMessage) []RoleWireMessage {
	out := make([]RoleWireMessage, 0, len(messages))
	for _, message := range messages {
		out = append(out, RoleWireMessage{
			Role: message.Role, Content: message.Content,
			ReasoningContent: message.ReasoningContent,
			ToolCalls:        message.ToolCalls, ToolCallID: message.ToolCallID,
			Name: message.Name, ResultRef: message.ResultRef, Seq: message.Seq,
			Internal: message.Internal, Repair: message.Repair, Attempt: message.Attempt,
		})
	}
	return out
}

func cloneCompactRef(ref *CompactRef) *CompactRef {
	if ref == nil {
		return nil
	}
	copy := *ref
	return &copy
}

func cloneFloor(floor *Floor) *Floor {
	if floor == nil {
		return nil
	}
	copy := *floor
	return &copy
}

func roleSnapshotWarnings(snapshot RoleSnapshot) []string {
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

// appendRoleSessionRows 向角色会话写备份行。main 角色写主会话 message；非 main
// 写各自子树。调用方保证只写该角色自己的行。
func appendRoleSessionRows(store *storeEngine, mainKey Key, roleName, roleSessionID string, rows []Event) error {
	if len(rows) == 0 {
		return nil
	}
	roleStore, roleKey := roleSessionEngine(store, mainKey, roleName, roleSessionID)
	commitID := storerole.BackupCommitID(roleName, roleSessionID, rows)
	_, err := roleStore.messageCommit(roleKey, commitID, rows)
	return err
}
