package dto

// PendingMessageTailReport 是「message 通道草稿尾部（seq_draft）」探测/恢复/丢弃
// 的结果快照，跨层传递的纯 DTO（应用层不依赖存储类型）。
//
// 语义（口径见 sessionstore/pending_tail.go 与 recovery-order.md）：
//   - HeadSeq 是**发布点**：seq <= HeadSeq 的行已提交、对读者可见；
//   - TailFrom/TailTo 是草稿尾部区间（seq > HeadSeq 的未提交行），无草稿时为 0；
//   - RowCount 是草稿行数（ExtraShardCount 是整个文件都未登记的分片数，
//     典型来源：分片滚动后、head 发布前崩溃）；
//   - Status 取 clean / recoverable / recovered / gap / discarded 之一。
type PendingMessageTailReport struct {
	SessionID       string `json:"session_id"`
	Status          string `json:"status"`
	HeadSeq         uint64 `json:"head_seq"`
	TailFrom        uint64 `json:"tail_from,omitempty"`
	TailTo          uint64 `json:"tail_to,omitempty"`
	RowCount        int    `json:"row_count,omitempty"`
	ExtraShardCount int    `json:"extra_shard_count,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

// 草稿尾部状态常量（与存储层 sessionstore.PendingTail* 一一对应，避免应用层
// 依赖存储包常量）。
const (
	PendingTailClean       = "clean"
	PendingTailRecoverable = "recoverable"
	PendingTailRecovered   = "recovered"
	PendingTailGap         = "gap"
	PendingTailDiscarded   = "discarded"
)
