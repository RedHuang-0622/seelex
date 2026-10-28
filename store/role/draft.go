package role

// draft.go — 角色 draft WAL（未同步草稿的只追加流水）。
//
// M9 从根 `store/role_session.go` 迁入。落盘形态与语义逐字保持（用户磁盘上已有
// 这些文件）：
//
//   - **只追加**：一次 append 只写尾部字节 + 一行 `'\n'`，不重写整份文件；
//   - **文件存在即待同步**：没有单独的"是否已同步"标记位，draft 文件在 = 有未同步
//     行，文件不在 = 没有；
//   - **发布成功即删除**：sync 把行 append 进 main message 后立刻删掉 draft，无保留
//     窗口——draft 不是审计记录，审计记录是 main message；
//   - **每角色一把文件锁**：不同角色的 append 互不阻塞；
//   - **崩溃残尾视为未提交**：追加前先截断末尾无换行的半行（`storeroot.TruncateCrashTail`），
//     所以"崩在写一半"不会把后续 JSON 拼坏。
//
// 生态位：本文件只做"草稿怎么落盘 + 怎么排序 + 怎么算幂等身份"，**不写 main
// message**。写 main message 的唯一入口是注入的 `SessionPort`（根 store 实现），
// 因此本包不 import 根 `store`，也不需要知道 head 长什么样。

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

	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
	storeroot "github.com/RedHuang-0622/seelex/store/root"
	storesession "github.com/RedHuang-0622/seelex/store/session"
)

// DraftRow 是一行未同步角色草稿。Event 是最终要 append 进 main message 的事件行。
type DraftRow struct {
	RoundID       uint64                   `json:"round_id"`
	RoleName      string                   `json:"role_name"`
	RoleSessionID string                   `json:"role_session_id"`
	UnitSeq       uint64                   `json:"unit_seq"`
	MessageID     string                   `json:"message_id,omitempty"`
	Event         protocol.TranscriptEvent `json:"event"`
}

// SyncResult 是一次 sequencer sync 的结果。AlreadySynced 表示该批 draft 之前已由
// 同一 commit_id 发布，本次只清理残留 draft，未重复 append。
type SyncResult struct {
	CommitID      string `json:"commit_id"`
	SyncedRows    int    `json:"synced_rows"`
	LastSeq       uint64 `json:"last_seq"`
	LastMessageID string `json:"last_message_id,omitempty"`
	AlreadySynced bool   `json:"already_synced"`
}

// SessionPort 是 sequencer 写 main message 所需的窄端口（由 store/role 定义，
// 根 store 实现）。SyncDraft 是唯一写 main message 的入口。
//
// 端口只有两个方法，是因为 sync 只需要两件事：幂等判定要读 head，发布要一次
// 原子 append + head/floor 同发。读全部行、建 shard、修索引都属于实现细节，
// 不应泄漏到本包（否则领域包会反向依赖存储机制）。
type SessionPort interface {
	// MessageHead 读 main message 当前 head（幂等判定用）。
	MessageHead() (storesession.MessageHead, error)
	// CommitSync 原子 append 事件行并发布 head + floor。
	CommitSync(commitID string, rows []protocol.TranscriptEvent, floor *storesession.Floor) (storesession.MessageHead, error)
}

// draftLocks 是角色 draft 的按路径写锁（path → *sync.Mutex）。每个角色独立锁，
// 互不阻塞；同一角色并发 append 在此排队，保证"一次 Write 一批行"的整份性。
var draftLocks sync.Map

func draftLock(path string) *sync.Mutex {
	value, _ := draftLocks.LoadOrStore(path, &sync.Mutex{})
	return value.(*sync.Mutex)
}

// DraftPath 返回 `<roleRoot>/draft/<roleName>.jsonl`。
func DraftPath(roleRoot, roleName string) string {
	return filepath.Join(roleRoot, draftDirName, roleName+".jsonl")
}

// AppendDraft 追加未同步 role draft。每个角色独立锁，互不阻塞。
//
// roleSessionID 用于补齐空行的归属（迁移前取自角色会话的 sessionID）；行上已有的
// RoleName / RoleSessionID 不会被覆盖——草稿里记的是"谁写的"，不是"写到哪"。
func AppendDraft(roleRoot, roleName, roleSessionID string, rows []DraftRow) error {
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
			rows[index].RoleSessionID = roleSessionID
		}
	}
	path := DraftPath(roleRoot, roleName)
	lock := draftLock(path)
	lock.Lock()
	defer lock.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := storeroot.TruncateCrashTail(file); err != nil {
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

// ReadDraft 读该角色的未同步 draft。文件不存在 = 没有待同步行（不是错误）；
// 末尾无换行的半行视为崩溃残尾丢弃，坏行跳过（不让一行坏 JSON 埋掉整批草稿）。
func ReadDraft(roleRoot, roleName string) ([]DraftRow, error) {
	path := DraftPath(roleRoot, roleName)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return []DraftRow{}, nil
	}
	if err != nil {
		return nil, err
	}
	segments := bytes.Split(data, []byte{'\n'})
	rows := make([]DraftRow, 0, len(segments))
	for index, segment := range segments {
		if index == len(segments)-1 && len(bytes.TrimSpace(segment)) > 0 {
			continue // 崩溃残尾
		}
		segment = bytes.TrimSpace(segment)
		if len(segment) == 0 {
			continue
		}
		var row DraftRow
		if err := json.Unmarshal(segment, &row); err != nil {
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// DeleteDraft 删除该角色的 draft 文件（= 已发布，没有待同步行）。
func DeleteDraft(roleRoot, roleName string) error {
	return os.Remove(DraftPath(roleRoot, roleName))
}

// SortDraftRows 按 round_id → role 顺序 → unit_seq 稳定排序。这是 sequencer 的
// 排序键（§8.3）；同一 unit 内调用方应已按 CompleteEventUnits 排好
// tool_call→tool→final。入参切片不被修改（先复制）。
func SortDraftRows(rows []DraftRow, order []string) []DraftRow {
	out := append([]DraftRow(nil), rows...)
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

// DraftCommitID 推导这批 draft 的确定性提交身份。
//
// 身份对**行序不敏感**（身份字符串先排序再哈希）：同一批行无论以什么顺序读回，
// 都得到同一个 commit_id，幂等判定才不会被"这次读回来的顺序不同"骗过去。
func DraftCommitID(roleName, roleSessionID string, rows []DraftRow) string {
	identity := make([]string, 0, len(rows))
	for _, row := range rows {
		identity = append(identity, fmt.Sprintf("%d|%s|%s|%d|%s",
			row.RoundID, row.RoleName, row.RoleSessionID, row.UnitSeq, row.MessageID))
		identity = append(identity, string(row.Event.Kind)+"|"+row.Event.Role+"|"+
			row.Event.MessageID+"|"+row.Event.ToolCallID+"|"+row.Event.Name+"|"+
			row.Event.Content+"|"+row.Event.ResultRef)
	}
	slices.Sort(identity)
	return "role-draft-" + roleName + "-" + storeroot.Hash(roleSessionID+"|"+strings.Join(identity, "\n"))
}

// RowsToEvents 把 draft 行转成待发布的事件行：补齐角色归属、round/unit 与
// message_id 兜底，旧数据（Kind 为空）按 `session.EventKindOf` 回退。
func RowsToEvents(rows []DraftRow) []protocol.TranscriptEvent {
	events := make([]protocol.TranscriptEvent, 0, len(rows))
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
			event.Kind = storesession.EventKindOf(event)
		}
		events = append(events, event)
	}
	return events
}

// SyncDraft 是 sequencer 的存储侧同步入口：读该角色 draft → 排序 → 幂等判定 →
// 经端口 append 进 main message 并原子发布 head + floor → 成功后删除 draft。
//
// 失败语义（半同步窗口）：
//   - 端口 CommitSync 失败：**不删 draft**。行没发布出去，draft 是唯一证据；
//   - CommitSync 成功但删 draft 失败：返回显式错误，但**不重试发布**。重跑本函数
//     时 head.LastCommitID == commitID，走 AlreadySynced 分支只清理 draft——
//     这就是幂等恢复路径。
func SyncDraft(roleRoot, roleName, roleSessionID string, order []string, port SessionPort) (SyncResult, error) {
	rows, err := ReadDraft(roleRoot, roleName)
	if err != nil {
		return SyncResult{}, err
	}
	if len(rows) == 0 {
		return SyncResult{}, nil
	}
	rows = SortDraftRows(rows, order)
	commitID := DraftCommitID(roleName, roleSessionID, rows)
	head, err := port.MessageHead()
	if err != nil {
		return SyncResult{}, err
	}
	if head.LastCommitID == commitID {
		// 半同步窗口：head 已发布、draft 未删；幂等删除，不重复 append。
		// 删除失败在这里不升级为错误：行已经在 main message 里，draft 留着只会让
		// 下一次 sync 再走一遍本分支（自愈），而报错会让调用方以为没发布。
		_ = DeleteDraft(roleRoot, roleName)
		return SyncResult{
			CommitID: commitID, SyncedRows: len(rows), LastSeq: head.LastSeq,
			LastMessageID: head.LastMessageID, AlreadySynced: true,
		}, nil
	}
	events := RowsToEvents(rows)
	floor := storesession.Floor{
		RoleName: roleName, RoleSessionID: roleSessionID,
		RoundID: rows[len(rows)-1].RoundID, UpdatedAt: time.Now().UTC(),
	}
	syncedHead, err := port.CommitSync(commitID, events, &floor)
	if err != nil {
		return SyncResult{}, err
	}
	if err := DeleteDraft(roleRoot, roleName); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return SyncResult{}, fmt.Errorf("session storage: sync published but draft cleanup failed: %w", err)
	}
	return SyncResult{
		CommitID: commitID, SyncedRows: len(events), LastSeq: syncedHead.LastSeq,
		LastMessageID: syncedHead.LastMessageID,
	}, nil
}
