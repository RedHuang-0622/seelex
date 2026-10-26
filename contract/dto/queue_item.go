package dto

import "time"

// QueueItem is the durable lifecycle queue projection. It intentionally carries
// no internal payload or storage path.
type QueueItem struct {
	Seq       uint64    `json:"seq"`
	ItemID    string    `json:"item_id"`
	RequestID string    `json:"request_id,omitempty"`
	TurnID    string    `json:"turn_id,omitempty"`
	Content   string    `json:"content"`
	State     string    `json:"state"`
	SendCount int       `json:"send_count,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// QueueRecoveryReport is the item-level durable queue recovery result.
type QueueRecoveryReport struct {
	SessionID string      `json:"session_id"`
	Resent    []QueueItem `json:"resent,omitempty"`
	Dropped   []QueueItem `json:"dropped,omitempty"`
	Pending   []QueueItem `json:"pending,omitempty"`
}
