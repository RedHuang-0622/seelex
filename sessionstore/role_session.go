// 群聊角色会话与 role draft 基建（my_design §8.3 / 提示词 R2+R4）。
//
// 本文件只提供存储侧接口与落盘形态，不接应用层 EVENT 生产者/actor；应用层
// 后续按这些入口装配 TL/agent-team 会话、写 draft 并触发 sequencer sync。
//
// 关键语义：
//   - main 会话是群聊正文唯一权威；非 main 角色会话是备份/冷恢复来源；
//   - role draft = append-only 未同步 WAL，文件存在即待同步；sync 成功后
//     立即删除，无保留窗口；
//   - sequencer 唯一写 message，floor 随 message head 原子发布。
package sessionstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	RoleUser = "user"
	RoleMain = "main"
	RoleTL   = "tl"
)

// RoleSessionInfo 是角色会话的基础信息（接口返回，不含引擎内部对象）。
type RoleSessionInfo struct {
	MainSessionID string `json:"main_session_id"`
	RoleName      string `json:"role_name"`
	RoleSessionID string `json:"role_session_id"`
	Root          string `json:"root"`
}

// RoleDraftRow 是一行未同步角色草稿。Event 是最终要 append 进 message 的
// 事件行；round_id/role_name/role_session_id/unit_seq/message_id 是 sequencer
// 排序与幂等凭据的一部分。
type RoleDraftRow struct {
	RoundID       uint64 `json:"round_id"`
	RoleName      string `json:"role_name"`
	RoleSessionID string `json:"role_session_id"`
	UnitSeq       uint64 `json:"unit_seq"`
	MessageID     string `json:"message_id,omitempty"`
	Event         Event  `json:"event"`
}

// RoleDraftSyncResult 是一次 sequencer sync 的结果。AlreadySynced 表示该批
// draft 之前已由同一 commit_id 发布，本次只清理残留 draft，未重复 append。
type RoleDraftSyncResult struct {
	CommitID      string `json:"commit_id"`
	SyncedRows    int    `json:"synced_rows"`
	LastSeq       uint64 `json:"last_seq"`
	LastMessageID string `json:"last_message_id,omitempty"`
	AlreadySynced bool   `json:"already_synced"`
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

var roleDraftLocks sync.Map // path -> *sync.Mutex

func roleDraftLock(path string) *sync.Mutex {
	value, _ := roleDraftLocks.LoadOrStore(path, &sync.Mutex{})
	return value.(*sync.Mutex)
}

// roleSessionRoot 返回角色会话子树的物理根。main 直接使用主会话根；tl 使用
// goal_<hash>/；其它未来 agent-team 角色使用 role_<hash>/。
func (store *storeEngine) roleSessionRoot(mainKey Key, roleName, roleSessionID string) string {
	mainRoot := store.sessionRoot(mainKey)
	if roleName == RoleMain {
		return mainRoot
	}
	prefix := "role_"
	if roleName == RoleTL {
		prefix = "goal_"
	}
	return filepath.Join(mainRoot, prefix+hash(roleSessionID))
}

// roleStore 返回指定角色会话的引擎与 key。main 复用当前引擎；非 main 以角色
// 子树为根创建轻量子引擎（与 subagent 子树机制同构）。
func (store *storeEngine) roleStore(mainKey Key, roleName, roleSessionID string) (*storeEngine, Key) {
	if roleName == RoleMain {
		return store, mainKey
	}
	root := store.roleSessionRoot(mainKey, roleName, roleSessionID)
	return newStoreEngine(root, store.settings), Key{SessionID: roleSessionID}
}

// createRoleSession 创建非 main 角色会话（message/event/metadata 同构空现场），
// 并写 join_seq_id。main 角色不创建独立子树。
func (store *storeEngine) createRoleSession(mainKey Key, roleName, roleSessionID string, joinSeq uint64) (*storeEngine, Key, error) {
	if roleName == "" || roleSessionID == "" {
		return nil, Key{}, errors.New("session storage: role_name and role_session_id are required")
	}
	if roleName == RoleMain {
		return nil, Key{}, errors.New("session storage: main role session is the main session itself")
	}
	if _, err := store.ensureLayoutGuide(mainKey); err != nil {
		return nil, Key{}, err
	}
	root := store.roleSessionRoot(mainKey, roleName, roleSessionID)
	childStore := newStoreEngine(root, store.settings)
	childKey := Key{SessionID: roleSessionID}
	if childStore.sessionExists(childKey) {
		return nil, Key{}, errors.New("session storage: role session already exists")
	}
	commitID := roleSessionCommitID(roleName, roleSessionID)
	if _, err := childStore.messageCommit(childKey, commitID, nil); err != nil {
		_ = os.RemoveAll(root)
		return nil, Key{}, err
	}
	payload, _ := json.Marshal(map[string]any{
		"main_session_id": mainKey.SessionID,
		"role_name":       roleName,
		"role_session_id": roleSessionID,
		"join_seq_id":     joinSeq,
	})
	if _, err := childStore.structuralEventCommit(childKey, commitID, []structuralEvent{{
		Kind: structuralEventRoleSession, Payload: payload,
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

func roleSessionCommitID(roleName, roleSessionID string) string {
	return "role-session-" + roleName + "-" + hash(roleSessionID)
}

func roleDraftPath(roleStore *storeEngine, key Key, roleName string) string {
	return filepath.Join(roleStore.sessionRoot(key), "draft", roleName+".jsonl")
}

// appendRoleDraft 追加未同步 role draft。每个角色独立锁，互不阻塞。
func appendRoleDraft(roleStore *storeEngine, key Key, roleName string, rows []RoleDraftRow) error {
	if roleName == "" {
		return errors.New("session storage: role draft requires role_name")
	}
	if len(rows) == 0 {
		return nil
	}
	for index := range rows {
		if rows[index].RoleName == "" {
			rows[index].RoleName = roleName
		}
		if rows[index].RoleSessionID == "" {
			rows[index].RoleSessionID = key.SessionID
		}
	}
	path := roleDraftPath(roleStore, key, roleName)
	lock := roleDraftLock(path)
	lock.Lock()
	defer lock.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := truncateCrashTail(file); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Seek(0, io.SeekEnd); err != nil {
		file.Close()
		return err
	}
	buffer := make([]byte, 0, len(rows)*256)
	for _, row := range rows {
		data, err := json.Marshal(row)
		if err != nil {
			file.Close()
			return err
		}
		buffer = append(buffer, data...)
		buffer = append(buffer, '\n')
	}
	if _, err := file.Write(buffer); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func readRoleDraft(roleStore *storeEngine, key Key, roleName string) ([]RoleDraftRow, error) {
	path := roleDraftPath(roleStore, key, roleName)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return []RoleDraftRow{}, nil
	}
	if err != nil {
		return nil, err
	}
	segments := bytes.Split(data, []byte{'\n'})
	rows := make([]RoleDraftRow, 0, len(segments))
	for index, segment := range segments {
		if index == len(segments)-1 && len(bytes.TrimSpace(segment)) > 0 {
			continue // 崩溃残尾
		}
		segment = bytes.TrimSpace(segment)
		if len(segment) == 0 {
			continue
		}
		var row RoleDraftRow
		if err := json.Unmarshal(segment, &row); err != nil {
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func deleteRoleDraft(roleStore *storeEngine, key Key, roleName string) error {
	return os.Remove(roleDraftPath(roleStore, key, roleName))
}

// sortRoleDraftRows 按 round_id → role 顺序 → unit_seq 稳定排序。这是
// sequencer 的排序键（§8.3）；同一 unit 内调用方应已按 CompleteEventUnits
// 排好 tool_call→tool→final。
func sortRoleDraftRows(rows []RoleDraftRow, order []string) []RoleDraftRow {
	out := append([]RoleDraftRow(nil), rows...)
	rank := make(map[string]int, len(order))
	for index, role := range order {
		rank[role] = index
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].RoundID != out[j].RoundID {
			return out[i].RoundID < out[j].RoundID
		}
		leftRank, leftOK := rank[out[i].RoleName]
		rightRank, rightOK := rank[out[j].RoleName]
		if leftOK != rightOK {
			return leftOK
		}
		if leftOK && rightOK && leftRank != rightRank {
			return leftRank < rightRank
		}
		if out[i].RoleName != out[j].RoleName {
			return out[i].RoleName < out[j].RoleName
		}
		return out[i].UnitSeq < out[j].UnitSeq
	})
	return out
}

func roleDraftCommitID(roleName, roleSessionID string, rows []RoleDraftRow) string {
	identity := make([]string, 0, len(rows))
	for _, row := range rows {
		identity = append(identity, fmt.Sprintf("%d|%s|%s|%d|%s",
			row.RoundID, row.RoleName, row.RoleSessionID, row.UnitSeq, row.MessageID))
		identity = append(identity, string(row.Event.Kind)+"|"+row.Event.Role+"|"+
			row.Event.MessageID+"|"+row.Event.ToolCallID+"|"+row.Event.Name+"|"+
			row.Event.Content+"|"+row.Event.ResultRef)
	}
	slices.Sort(identity)
	return "role-draft-" + roleName + "-" + hash(roleSessionID+"|"+strings.Join(identity, "\n"))
}

func roleDraftRowsToEvents(rows []RoleDraftRow) []Event {
	events := make([]Event, 0, len(rows))
	for _, row := range rows {
		event := row.Event
		event.RoleName = row.RoleName
		event.RoleSessionID = row.RoleSessionID
		event.RoundID = row.RoundID
		event.UnitSeq = row.UnitSeq
		if event.MessageID == "" {
			event.MessageID = row.MessageID
		}
		if event.Kind == "" {
			event.Kind = EventKindOf(event)
		}
		events = append(events, event)
	}
	return events
}

// syncRoleDraft 是 sequencer 的存储侧同步入口：读该角色 draft → 排序 → 幂等
// 判定 → append 进 main message 并原子发布 head + floor → 成功后删除 draft。
func (store *storeEngine) syncRoleDraft(mainKey Key, roleName, roleSessionID string, order []string) (RoleDraftSyncResult, error) {
	roleStore, roleKey := store.roleStore(mainKey, roleName, roleSessionID)
	rows, err := readRoleDraft(roleStore, roleKey, roleName)
	if err != nil {
		return RoleDraftSyncResult{}, err
	}
	if len(rows) == 0 {
		return RoleDraftSyncResult{}, nil
	}
	rows = sortRoleDraftRows(rows, order)
	commitID := roleDraftCommitID(roleName, roleSessionID, rows)
	head, err := store.readMessageHead(mainKey)
	if err != nil {
		return RoleDraftSyncResult{}, err
	}
	if head.LastCommitID == commitID {
		// 半同步窗口：head 已发布、draft 未删；幂等删除，不重复 append。
		_ = deleteRoleDraft(roleStore, roleKey, roleName)
		return RoleDraftSyncResult{
			CommitID: commitID, SyncedRows: len(rows), LastSeq: head.LastSeq,
			LastMessageID: head.LastMessageID, AlreadySynced: true,
		}, nil
	}
	events := roleDraftRowsToEvents(rows)
	floor := Floor{
		RoleName: roleName, RoleSessionID: roleSessionID,
		RoundID: rows[len(rows)-1].RoundID, UpdatedAt: time.Now().UTC(),
	}
	syncedHead, err := store.messageCommitSync(mainKey, commitID, events, &floor)
	if err != nil {
		return RoleDraftSyncResult{}, err
	}
	if err := deleteRoleDraft(roleStore, roleKey, roleName); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return RoleDraftSyncResult{}, fmt.Errorf("session storage: sync published but draft cleanup failed: %w", err)
	}
	return RoleDraftSyncResult{
		CommitID: commitID, SyncedRows: len(events), LastSeq: syncedHead.LastSeq,
		LastMessageID: syncedHead.LastMessageID,
	}, nil
}

// readRoleSessionRows 读取角色会话备份行（冷恢复/审计用）。main 角色读主会话。
func (store *storeEngine) readRoleSessionRows(mainKey Key, roleName, roleSessionID string) ([]Event, error) {
	roleStore, roleKey := store.roleStore(mainKey, roleName, roleSessionID)
	return roleStore.readAllRows(roleKey)
}

// readRoleSnapshot 汇总角色会话观察面（只读）。
func (store *storeEngine) readRoleSnapshot(mainKey Key, roleName, roleSessionID string) (RoleSnapshot, error) {
	if roleName == "" || roleSessionID == "" {
		return RoleSnapshot{}, errors.New("session storage: role_name and role_session_id are required")
	}
	roleStore, roleKey := store.roleStore(mainKey, roleName, roleSessionID)
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
	draftRows, err := readRoleDraft(roleStore, roleKey, roleName)
	if err != nil {
		return RoleSnapshot{}, err
	}
	snapshot := RoleSnapshot{
		MainSessionID:    mainKey.SessionID,
		RoleName:         roleName,
		RoleSessionID:    roleSessionID,
		Root:             store.roleSessionRoot(mainKey, roleName, roleSessionID),
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
	for _, row := range mainRows {
		if row.RoleName == "" {
			snapshot.UnassignedRoleRows++
		}
	}
	snapshot.DesignWarnings = roleSnapshotWarnings(snapshot)
	return snapshot, nil
}

// assembleRoleWire 构造角色可见 wire：main compact_ref + seq > 切点的 main 行
// + 该角色自身 pending draft（未 sync 不进主文档）。user draft 不在此路径。
func (store *storeEngine) assembleRoleWire(mainKey Key, roleName, roleSessionID string, budget, k int) (RoleWireSnapshot, error) {
	if roleName == RoleUser {
		return RoleWireSnapshot{}, errors.New("session storage: user draft is input-box material, not role wire")
	}
	roleStore, roleKey := store.roleStore(mainKey, roleName, roleSessionID)
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
	draftRows, err := readRoleDraft(roleStore, roleKey, roleName)
	if err != nil {
		return RoleWireSnapshot{}, err
	}
	draftRows = sortRoleDraftRows(draftRows, nil)
	pending := roleDraftRowsToEvents(draftRows)
	for index := range pending {
		// 只为本次装配提供稳定序号；真正全局 seq 只能由 sequencer 在 sync 时
		// 分配，pending 行不得写回 message。
		pending[index].Seq = head.LastSeq + uint64(index) + 1
	}
	combined := append(append([]Event(nil), rows...), pending...)
	result := assembleWireRows(frame, combined, nil, wireParams{Budget: budget, K: k})
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
func (store *storeEngine) appendRoleSessionRows(mainKey Key, roleName, roleSessionID string, rows []Event) error {
	if len(rows) == 0 {
		return nil
	}
	roleStore, roleKey := store.roleStore(mainKey, roleName, roleSessionID)
	commitID := roleBackupCommitID(roleName, roleSessionID, rows)
	_, err := roleStore.messageCommit(roleKey, commitID, rows)
	return err
}

func roleBackupCommitID(roleName, roleSessionID string, rows []Event) string {
	return "role-backup-" + roleName + "-" + hash(roleSessionID+"|"+messageRowsCommitID(rows))
}
