// 魔法数字与配置（my_design §11）。
//
// 当前实现先落默认值与校验（T-CFG-01）；后续 M5 再并入 seele.yaml limits/
// config 体系做覆盖。
package sessionstore

import (
	"errors"
	"fmt"
)

// storageSettings 是 v8 存储运行参数（默认值 = my_design §11 初稿）。
type storageSettings struct {
	MessageShardRows      int    `json:"session_storage.message.shard_rows"`
	RetryCacheMaxItems    int    `json:"session_storage.retry_cache.max_items"`
	RetryCacheMaxChars    int    `json:"session_storage.retry_cache.max_chars"`
	WireRecentErrors      int    `json:"session_storage.retry_cache.wire_recent_errors"`
	CompactFrameThreshold int    `json:"session_storage.retention.compact_frame_threshold"`
	RawBytesAlert         uint64 `json:"session_storage.retention.raw_bytes_alert"`
	RetentionMode         string `json:"session_storage.retention.mode"`
	QueuePersistPending   bool   `json:"session_storage.queue.persist_pending"`
	StaleAfterSeconds     int    `json:"session_storage.lock.stale_after_seconds"`
	AutoRecover           bool   `json:"session_storage.lock.auto_recover"`
	BlobSoftLimitChars    int    `json:"session_storage.big_tool_result.soft_limit_chars"`
	BlobHardLimitBytes    int    `json:"session_storage.big_tool_result.hard_limit_bytes"`
	BlobSessionQuotaBytes int    `json:"session_storage.big_tool_result.session_quota_bytes"`
}

// defaultStorageSettings 返回设计默认值。
func defaultStorageSettings() storageSettings {
	return storageSettings{
		MessageShardRows:      100,
		RetryCacheMaxItems:    64,
		RetryCacheMaxChars:    8 << 20,
		WireRecentErrors:      3,
		CompactFrameThreshold: 30,
		RawBytesAlert:         256 << 20,
		RetentionMode:         retentionModeManual,
		QueuePersistPending:   true,
		StaleAfterSeconds:     300,
		AutoRecover:           false,
		BlobSoftLimitChars:    60000,
		BlobHardLimitBytes:    16 << 20,
		BlobSessionQuotaBytes: 64 << 20,
	}
}

// validateStorageSettings 校验配置覆盖（非法值显式拒绝）。
func validateStorageSettings(config storageSettings) error {
	if config.MessageShardRows != 0 && config.MessageShardRows < 1 {
		return errors.New("session storage: message.shard_rows must be > 0")
	}
	if config.WireRecentErrors < 1 {
		return fmt.Errorf("session storage: wire_recent_errors must be >= 1 (got %d)", config.WireRecentErrors)
	}
	if config.CompactFrameThreshold < 1 {
		return errors.New("session storage: compact_frame_threshold must be > 0")
	}
	if config.BlobSoftLimitChars < 1 || config.BlobHardLimitBytes < config.BlobSoftLimitChars {
		return errors.New("session storage: big_tool_result limits invalid")
	}
	if config.BlobSessionQuotaBytes < config.BlobHardLimitBytes {
		return errors.New("session storage: session quota must cover hard limit")
	}
	if config.RetentionMode != "" && config.RetentionMode != retentionModeManual {
		return errors.New("session storage: retention mode must be manual (current implementation)")
	}
	return nil
}
