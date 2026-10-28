// Package session owns transcript shard naming and pending-tail classification.
// Head publication and message orchestration remain in the root adapter until
// later M5 slices move them.
package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	frameworkStorage "github.com/RedHuang-0622/Seele/seelectx/storage"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
	storelayout "github.com/RedHuang-0622/seelex/store/internal/layout"
	storeroot "github.com/RedHuang-0622/seelex/store/root"
)

const (
	PendingTailClean       = "clean"
	PendingTailRecoverable = "recoverable"
	PendingTailRecovered   = "recovered"
	PendingTailGap         = "gap"
	PendingTailDiscarded   = "discarded"
)

// PendingTailReport is the stable draft-tail report.
type PendingTailReport struct {
	SessionID   string                     `json:"session_id"`
	Status      string                     `json:"status"`
	HeadSeq     uint64                     `json:"head_seq"`
	TailFrom    uint64                     `json:"tail_from,omitempty"`
	TailTo      uint64                     `json:"tail_to,omitempty"`
	Rows        []protocol.TranscriptEvent `json:"rows,omitempty"`
	ExtraShards []string                   `json:"extra_shards,omitempty"`
	Reason      string                     `json:"reason,omitempty"`
}

// RowCount returns the number of unpublished rows.
func (report PendingTailReport) RowCount() int { return len(report.Rows) }

// MessageHead is metadata/message.json payload and its stable JSON wire.
type MessageHead struct {
	SessionID          string                       `json:"session_id"`
	LastSeq            uint64                       `json:"last_seq"`
	LastMessageID      string                       `json:"last_message_id,omitempty"`
	Shards             []ShardInfo                  `json:"shards,omitempty"`
	TotalRows          uint64                       `json:"total_rows"`
	WatermarkSeq       uint64                       `json:"watermark_seq,omitempty"`
	WatermarkMessageID string                       `json:"watermark_message_id,omitempty"`
	LastCommitID       string                       `json:"last_commit_id,omitempty"`
	Meta               frameworkStorage.SessionMeta `json:"meta,omitempty"`
	Floor              *Floor                       `json:"floor,omitempty"`
}

// ShardInfo is one message shard index entry.
type ShardInfo struct {
	Path    string `json:"path"`
	FromSeq uint64 `json:"from_seq"`
	ToSeq   uint64 `json:"to_seq"`
	Count   int    `json:"count"`
	SHA256  string `json:"sha256,omitempty"`
	Bytes   int64  `json:"bytes,omitempty"`
}

// Floor is the current speaking-role record carried by message head.
type Floor struct {
	RoleName      string    `json:"role_name"`
	RoleSessionID string    `json:"role_session_id,omitempty"`
	RoundID       uint64    `json:"round_id,omitempty"`
	Seq           uint64    `json:"seq,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// EmptyMessageHead returns the canonical zero head for a session.
func EmptyMessageHead(sessionID string) MessageHead {
	return MessageHead{SessionID: sessionID}
}

// AssignRowSeqs validates runtime-assigned event identities before persistence.
// Seq <= base is treated as an idempotent replay and skipped. New rows must
// start at base+1 and remain strictly contiguous; Store never invents a seq.
func AssignRowSeqs(rows []protocol.TranscriptEvent, base uint64, commitID string) ([]protocol.TranscriptEvent, error) {
	if len(rows) == 0 {
		return []protocol.TranscriptEvent{}, nil
	}
	next := base
	delta := make([]protocol.TranscriptEvent, 0, len(rows))
	for _, row := range rows {
		if row.Seq == 0 {
			return nil, errors.New("session storage: message rows must carry an explicit seq")
		} else {
			if row.Seq <= base {
				continue
			}
			if row.Seq != next+1 {
				return nil, fmt.Errorf("session storage: message rows must be contiguous after head %d (got %d)", next, row.Seq)
			}
			next = row.Seq
		}
		row.CommitID = commitID
		delta = append(delta, row)
	}
	return delta, nil
}

// StructuralEventKind is the stable EVENT channel kind vocabulary.
type StructuralEventKind string

const (
	StructuralEventCompacted          StructuralEventKind = "compacted"
	StructuralEventFork               StructuralEventKind = "fork"
	StructuralEventSubagent           StructuralEventKind = "subagent"
	StructuralEventRoleSession        StructuralEventKind = "role_session"
	StructuralEventInterrupted        StructuralEventKind = "interrupted"
	StructuralEventRolledBack         StructuralEventKind = "rolled_back"
	StructuralEventRequestBegin       StructuralEventKind = "request_begin"
	StructuralEventRequestEnd         StructuralEventKind = "request_end"
	StructuralEventTurnBegin          StructuralEventKind = "turn_begin"
	StructuralEventTurnEnd            StructuralEventKind = "turn_end"
	StructuralEventTokenUsage         StructuralEventKind = "token_usage"
	StructuralEventArchived           StructuralEventKind = "session_archived"
	StructuralEventScheduleRegistered StructuralEventKind = "schedule.registered"
	StructuralEventScheduleCancelled  StructuralEventKind = "schedule.cancelled"
	StructuralEventScheduleFired      StructuralEventKind = "schedule.fired"
)

// StructuralEvent is one append-only EVENT row.
type StructuralEvent struct {
	EventID         uint64              `json:"event_id"`
	Kind            StructuralEventKind `json:"kind"`
	AnchorMessageID string              `json:"anchor_message_id,omitempty"`
	AnchorSeq       uint64              `json:"anchor_seq,omitempty"`
	FrameID         string              `json:"frame_id,omitempty"`
	CommitID        string              `json:"commit_id"`
	Payload         json.RawMessage     `json:"payload,omitempty"`
	CreatedAt       time.Time           `json:"created_at"`
}

// EventShardInfo is one event-head shard entry.
type EventShardInfo struct {
	Path   string `json:"path"`
	FromID uint64 `json:"from_id"`
	ToID   uint64 `json:"to_id"`
	Count  int    `json:"count"`
	Bytes  int64  `json:"bytes"`
}

// EventHead is metadata/event.json payload.
type EventHead struct {
	SessionID    string           `json:"session_id"`
	LastID       uint64           `json:"last_event_id"`
	LastCommitID string           `json:"last_commit_id,omitempty"`
	Total        uint64           `json:"total"`
	Shards       []EventShardInfo `json:"shards,omitempty"`
}

// StructuralEventsCommitID derives the deterministic EVENT commit identity.
func StructuralEventsCommitID(events []StructuralEvent) string {
	identity := make([]string, 0, len(events))
	for _, event := range events {
		identity = append(identity, string(event.Kind)+"|"+event.AnchorMessageID+
			"|"+event.FrameID+"|"+string(event.Payload))
	}
	return "ev-" + storeroot.Hash(strings.Join(identity, "\n"))
}

// MessageRowsCommitID derives the deterministic transcript commit identity.
func MessageRowsCommitID(rows []protocol.TranscriptEvent) string {
	identity := make([]string, 0, len(rows))
	for _, row := range rows {
		identity = append(identity, row.Kind+"|"+row.Role+"|"+row.MessageID+"|"+
			row.ToolCallID+"|"+row.Name+"|"+row.Content+"|"+row.ResultRef)
	}
	return "msg-" + storeroot.Hash(strings.Join(identity, "\n"))
}

// ApplyPendingTailHead builds the head that publishes a validated draft tail.
func ApplyPendingTailHead(
	head MessageHead,
	rows []protocol.TranscriptEvent,
	shards []ShardInfo,
	total uint64,
	commitID string,
	now time.Time,
) MessageHead {
	tokens := 0
	lastMessageID := ""
	for _, row := range rows {
		tokens += row.TokenCount
		if row.MessageID != "" {
			lastMessageID = row.MessageID
		}
		if row.CommitID != "" {
			commitID = row.CommitID
		}
	}
	if commitID == "" {
		commitID = MessageRowsCommitID(rows)
	}
	head.Shards = shards
	if len(rows) > 0 {
		head.LastSeq = rows[len(rows)-1].Seq
	}
	head.LastMessageID = lastMessageID
	head.TotalRows = total
	head.LastCommitID = commitID
	head.Meta.TokenCount += tokens
	head.Meta.ShardCount = len(shards)
	head.Meta.UpdatedAt = now
	if head.Meta.SessionID == "" {
		head.Meta.SessionID = head.SessionID
	}
	if head.Meta.CreatedAt.IsZero() {
		head.Meta.CreatedAt = now
	}
	return head
}

// ApplyCommittedMessageHead updates the published head after message rows were
// appended; shard indexes are supplied by the physical writer.
func ApplyCommittedMessageHead(
	head MessageHead,
	delta []protocol.TranscriptEvent,
	commitID string,
	now time.Time,
) MessageHead {
	head.LastCommitID = commitID
	tokens := 0
	for _, row := range delta {
		tokens += row.TokenCount
	}
	head.Meta.TokenCount += tokens
	head.Meta.ShardCount = len(head.Shards)
	head.Meta.UpdatedAt = now
	if head.Meta.SessionID == "" {
		head.Meta.SessionID = head.SessionID
		head.Meta.CreatedAt = now
	}
	return head
}

// QueueState is the durable draft/queue item state.
type QueueState string

const (
	QueueQueued  QueueState = "queued"
	QueueSending QueueState = "sending"
	QueueSent    QueueState = "sent"
	QueueFailed  QueueState = "failed"
)

// DraftItem is the single durable input draft.
type DraftItem struct {
	SessionID string    `json:"session_id"`
	Content   string    `json:"content"`
	State     string    `json:"state,omitempty"`
	SavedAt   time.Time `json:"saved_at"`
}

// QueueRow is the private durable lifecycle row. The public projection is
// contract/dto.QueueItem; root/session code converts at the boundary.
type QueueRow struct {
	Seq        uint64         `json:"seq"`
	ItemID     string         `json:"item_id"`
	RequestID  string         `json:"request_id,omitempty"`
	Content    string         `json:"content"`
	State      QueueState     `json:"state"`
	CreatedAt  time.Time      `json:"created_at"`
	SendCount  int            `json:"send_count,omitempty"`
	Payload    map[string]any `json:"payload,omitempty"`
	ConsumedBy string         `json:"consumed_by,omitempty"`
}

// LifecycleState is the file-authoritative draft/queue projection.
type LifecycleState struct {
	Draft *DraftItem
	Queue []QueueRow
}

// LifecycleHead is metadata/lifecycle.json payload.
type LifecycleHead struct {
	SessionID     string      `json:"session_id"`
	DraftRevision uint64      `json:"draft_revision"`
	QueueCount    int         `json:"queue_count"`
	QueueHeadSeq  uint64      `json:"queue_head_seq"`
	HeadState     QueueState  `json:"head_state,omitempty"`
	SendingItemID string      `json:"sending_item_id,omitempty"`
	CommitID      string      `json:"commit_id,omitempty"`
	ArchivedAt    time.Time   `json:"archived_at,omitempty"`
	UpdatedAt     time.Time   `json:"updated_at"`
	OrderPolicy   string      `json:"order_policy,omitempty"`
	OrderRoles    []string    `json:"order_roles,omitempty"`
	JoinSeqID     uint64      `json:"join_seq_id,omitempty"`
	CompactRef    *CompactRef `json:"compact_ref,omitempty"`
}

// CompactRef points a role session at its compact frame.
type CompactRef struct {
	FrameID    string `json:"frame_id"`
	AppliedSeq uint64 `json:"applied_seq"`
}

// QueueHeadState returns the current queue head state.
func QueueHeadState(items []QueueRow) QueueState {
	if len(items) == 0 {
		return ""
	}
	return items[0].State
}

// RetentionHead is metadata/retention.json payload.
type RetentionHead struct {
	SessionID          string    `json:"session_id"`
	Mode               string    `json:"mode"`
	DryRun             bool      `json:"dry_run"`
	WatermarkSeq       uint64    `json:"watermark_seq,omitempty"`
	WatermarkMessageID string    `json:"watermark_message_id,omitempty"`
	DeletedRows        uint64    `json:"deleted_rows,omitempty"`
	CompactThreshold   int       `json:"compact_frame_threshold,omitempty"`
	RawBytesAlert      uint64    `json:"raw_bytes_alert,omitempty"`
	UpdatedAt          time.Time `json:"updated_at"`
}

var (
	ErrRetentionRequiresConfirm = errors.New("session storage: LRU deletion requires user confirmation (mode=manual)")
	ErrForkBeforeWatermark      = errors.New("session storage: fork start must be >= LRU watermark")
)

const (
	CompactBoundaryComplete = "complete"
	CompactBoundaryOpen     = "open"
)

// CompactFrameRecord is one compact summary frame.
type CompactFrameRecord struct {
	FrameID        string    `json:"frame_id"`
	PrevID         string    `json:"prev_id,omitempty"`
	MessageFrom    string    `json:"message_from"`
	MessageTo      string    `json:"message_to"`
	MessageFromSeq uint64    `json:"message_from_seq"`
	MessageToSeq   uint64    `json:"message_to_seq"`
	Summary        string    `json:"summary"`
	BoundaryStatus string    `json:"boundary_status"`
	CommitID       string    `json:"commit_id"`
	CompressedAt   time.Time `json:"compressed_at"`
}

// CompactHead is metadata/compact.json payload.
type CompactHead struct {
	SessionID     string              `json:"session_id"`
	LastFrameID   string              `json:"last_frame_id,omitempty"`
	LastMessageTo string              `json:"last_message_to,omitempty"`
	LastSeq       uint64              `json:"last_message_to_seq"`
	FrameCount    int                 `json:"frame_count"`
	LatestFrame   *CompactFrameRecord `json:"latest_frame,omitempty"`
}

// ValidateCompactFrame rejects frames beyond the published message head.
func ValidateCompactFrame(frame CompactFrameRecord, publishedSeq uint64) error {
	if frame.MessageToSeq > publishedSeq {
		return fmt.Errorf("session storage: compact frame message_to=%d beyond message head %d", frame.MessageToSeq, publishedSeq)
	}
	return nil
}

// ValidateForkStart enforces distinct child, watermark and non-overwrite rules.
func ValidateForkStart(
	parentProject, parentSession, childProject, childSession string,
	watermark, fromSeq uint64,
	childExists bool,
) error {
	if fromSeq < watermark {
		return ErrForkBeforeWatermark
	}
	if childSession == parentSession && childProject == parentProject {
		return errors.New("session storage: fork child must differ from parent")
	}
	if childExists {
		return errors.New("session storage: fork child already exists")
	}
	return nil
}

// ValidateRetentionPrefix rejects deleting a non-contiguous published prefix.
func ValidateRetentionPrefix(remaining []protocol.TranscriptEvent, upToSeq, lastSeq uint64) error {
	if len(remaining) > 0 && remaining[0].Seq > upToSeq+1 && remaining[0].Seq <= lastSeq {
		return errors.New("session storage: LRU delete range is not a continuous prefix")
	}
	return nil
}

// NextQueueSeq returns the next stable queue sequence.
func NextQueueSeq(items []QueueRow) uint64 {
	var top uint64
	for _, item := range items {
		if item.Seq > top {
			top = item.Seq
		}
	}
	return top + 1
}

// NewQueueRow creates one queued row with deterministic identity.
func NewQueueRow(state LifecycleState, requestID, content string, now time.Time) QueueRow {
	seq := NextQueueSeq(state.Queue)
	itemID := "q:" + requestID
	if requestID == "" {
		itemID = "q:" + storeroot.Hash(content+"|"+fmt.Sprintf("%d", seq))
	}
	return QueueRow{
		Seq: seq, ItemID: itemID, RequestID: requestID,
		Content: content, State: QueueQueued, CreatedAt: now,
	}
}

// Enqueue appends one item unless the same requestID is already queued.
func Enqueue(state LifecycleState, requestID, content string, now time.Time) (LifecycleState, bool) {
	for _, item := range state.Queue {
		if requestID != "" && item.RequestID == requestID {
			return state, false
		}
	}
	state.Queue = append(state.Queue, NewQueueRow(state, requestID, content, now))
	return state, true
}

// SaveDraft installs a normal unsent draft.
func SaveDraft(state LifecycleState, sessionID, content string, now time.Time) LifecycleState {
	state.Draft = &DraftItem{SessionID: sessionID, Content: content, State: "未发送", SavedAt: now}
	return state
}

// MoveDraftToQueue atomically clears draft and appends the queued copy.
func MoveDraftToQueue(state LifecycleState, sessionID, content string, now time.Time) LifecycleState {
	state.Draft = nil
	state.Queue = append(state.Queue, NewQueueRow(state, "", content, now))
	return state
}

// DirectSendState clears draft and appends only when an older queue item exists.
func DirectSendState(state LifecycleState, content string, now time.Time) (LifecycleState, bool) {
	state.Draft = nil
	if len(state.Queue) == 0 {
		return state, false
	}
	state.Queue = append(state.Queue, NewQueueRow(state, "", content, now))
	return state, true
}

// SendFront transitions the FIFO head to sending.
func SendFront(state LifecycleState) (LifecycleState, QueueRow, error) {
	if len(state.Queue) == 0 {
		return state, QueueRow{}, errors.New("session storage: queue empty")
	}
	item := state.Queue[0]
	if item.State == QueueSending || item.State == QueueSent {
		return state, QueueRow{}, errors.New("session storage: queue head already sending")
	}
	item.State = QueueSending
	item.SendCount++
	state.Queue[0] = item
	return state, item, nil
}

// ConfirmFront removes the head after a published send.
func ConfirmFront(state LifecycleState, requestID string) (LifecycleState, error) {
	if len(state.Queue) == 0 {
		return state, errors.New("session storage: queue empty")
	}
	front := state.Queue[0]
	if front.State != QueueSending && front.State != QueueSent {
		return state, errors.New("session storage: illegal queued -> sent without send record")
	}
	if requestID != "" && front.RequestID != "" && front.RequestID != requestID {
		return state, errors.New("session storage: queue head request mismatch")
	}
	state.Queue = state.Queue[1:]
	return state, nil
}

// FailFront removes the head and returns its content to draft.
func FailFront(state LifecycleState, sessionID string, now time.Time) (LifecycleState, error) {
	if len(state.Queue) == 0 {
		return state, errors.New("session storage: queue empty")
	}
	front := state.Queue[0]
	state.Queue = state.Queue[1:]
	state.Draft = &DraftItem{
		SessionID: sessionID, Content: front.Content,
		State: "发送失败退回", SavedAt: now,
	}
	return state, nil
}

// FailDirect returns a directly-sent content back to draft.
func FailDirect(state LifecycleState, sessionID, content string, now time.Time) LifecycleState {
	state.Draft = &DraftItem{
		SessionID: sessionID, Content: content,
		State: "发送失败退回", SavedAt: now,
	}
	return state
}

// PublishMessageHead publishes the typed message head through layout.
func PublishMessageHead(layout *storelayout.Layout, scope storelayout.Scope, commitID string, head MessageHead, updatedAt time.Time) (string, error) {
	return layout.Publish(scope, "message", commitID, head, updatedAt)
}

// PublishEventHead publishes the typed event head through layout.
func PublishEventHead(layout *storelayout.Layout, scope storelayout.Scope, commitID string, head EventHead, updatedAt time.Time) (string, error) {
	return layout.Publish(scope, "event", commitID, head, updatedAt)
}

// ClassifyPendingTail preserves the single draft-tail recoverability rule.
func ClassifyPendingTail(headSeq uint64, rows []protocol.TranscriptEvent, duplicate bool) (string, string) {
	if len(rows) == 0 {
		return PendingTailClean, ""
	}
	if duplicate {
		return PendingTailGap, "草稿尾部存在重复 seq（多个分片承载同一 seq）：不发布"
	}
	want := headSeq + 1
	for _, row := range rows {
		if row.Seq != want {
			return PendingTailGap, fmt.Sprintf(
				"草稿尾部与发布点不连续：期望 seq %d，实际 %d（发布点已不被本段独占，只能在一致前缀处截断）",
				want, row.Seq)
		}
		want++
	}
	return PendingTailRecoverable, ""
}

// MessageShardName returns the stable sorted-shard filename.
func MessageShardName(fromSeq, toSeq uint64) string {
	return fmt.Sprintf("message_%d_%d.jsonl", fromSeq, toSeq)
}

// ParseMessageShardName validates and parses a message shard name.
func ParseMessageShardName(name string) (uint64, uint64, bool) {
	var fromSeq, toSeq uint64
	if _, err := fmt.Sscanf(name, "message_%d_%d.jsonl", &fromSeq, &toSeq); err != nil {
		return 0, 0, false
	}
	return fromSeq, toSeq, fromSeq > 0 && toSeq >= fromSeq
}

// EventShardName returns the stable event-shard filename.
func EventShardName(fromID, toID uint64) string {
	return fmt.Sprintf("event_%d_%d.jsonl", fromID, toID)
}

// ParseEventShardName validates and parses an event shard name.
func ParseEventShardName(name string) (uint64, uint64, bool) {
	var fromID, toID uint64
	if _, err := fmt.Sscanf(name, "event_%d_%d.jsonl", &fromID, &toID); err != nil {
		return 0, 0, false
	}
	return fromID, toID, fromID > 0 && toID >= fromID
}
