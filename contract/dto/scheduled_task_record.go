package dto

import "time"

// ScheduledTaskRecord 是定时任务全局 JSONL 里的一行：一条任务定义的**一次变更**。
// Deleted=true 时是墓碑行（取消），只认 ID。
//
// 物理位置：2026-10-16 按 plan §2.12.1 从 `seelebridge/scheduler/persistence.go` 归位到这里。
// plan 原文把记录指向 `store/schedule/`，但端口（`contract.ScheduledTaskPersistence`）的签名
// 用到它、而契约层不能反向依赖 store——两者不能同时成立，所以形状随端口进契约层
// （见 docs/2026-10-15-architecture-reshape/m4-record.md D-M4-17）。
type ScheduledTaskRecord struct {
	ID        string            `json:"id"`
	Spec      ScheduledTaskSpec `json:"spec"`
	Enabled   bool              `json:"enabled"`
	Deleted   bool              `json:"deleted,omitempty"`
	UpdatedAt time.Time         `json:"updated_at"`
}
