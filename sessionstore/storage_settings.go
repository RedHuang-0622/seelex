// 魔法数字与配置覆盖链（my_design §11）。
//
// 设计稿的键是 `session_storage.<域>.<叶子>`；落到配置体系里保留同一条覆盖链，
// 但键名用扁平叶子名（与 seele.yaml `limits:` 段的既有风格一致）：
//
//	默认值  ←  limits.session_storage.*（seele.yaml）  ←  session-storage.json
//
// 三层只在 `Router` 打开后端时合并一次（`resolveStorageSettings`），此后运行期
// 不再改：各通道从 storeEngine.settings 读数，禁止再各自硬编码。
package sessionstore

import (
	"errors"
	"fmt"
)

// Settings 是装配根（main.go）注入 §11 覆盖层的公开类型：字段零值 = 未配置，
// 由 resolveStorageSettings 补默认值。布尔项用指针，因为设计默认值里有 false。
type Settings = storageSettings

// NewSettings 返回一个只含指定覆盖项的 Settings（全部零值 = 用默认）。
func NewSettings() Settings { return Settings{} }

// storageSettings 是 v8 存储运行参数（默认值 = my_design §11 初稿）。
type storageSettings struct {
	MessageShardRows      int    `json:"message_shard_rows,omitempty"`
	RetryCacheMaxItems    int    `json:"retry_cache_max_items,omitempty"`
	RetryCacheMaxChars    int    `json:"retry_cache_max_chars,omitempty"`
	WireRecentErrors      int    `json:"retry_cache_wire_recent_errors,omitempty"`
	CompactFrameThreshold int    `json:"retention_compact_frame_threshold,omitempty"`
	RawBytesAlert         uint64 `json:"retention_raw_bytes_alert,omitempty"`
	RetentionMode         string `json:"retention_mode,omitempty"`
	QueuePersistPending   *bool  `json:"queue_persist_pending,omitempty"`
	StaleAfterSeconds     int    `json:"lock_stale_after_seconds,omitempty"`
	AutoRecover           *bool  `json:"lock_auto_recover,omitempty"`
	BlobSoftLimitChars    int    `json:"big_tool_result_soft_limit_chars,omitempty"`
	BlobHardLimitBytes    int    `json:"big_tool_result_hard_limit_bytes,omitempty"`
	BlobSessionQuotaBytes int    `json:"big_tool_result_session_quota_bytes,omitempty"`
	// WireBudgetTokens / WireSoftRatio / WireTargetRatio 是 §5.2 的 wire 装配
	// 预算：软阈值触发压缩、目标阈值决定裁剪到哪。
	WireBudgetTokens int     `json:"wire_budget_tokens,omitempty"`
	WireSoftRatio    float64 `json:"wire_soft_ratio,omitempty"`
	WireTargetRatio  float64 `json:"wire_target_ratio,omitempty"`
}

// 布尔项用指针：设计默认值里有 false（auto_recover），零值无法区分「未设置」。
func boolValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

// defaultStorageSettings 返回设计默认值（§11 表格 + §5.2 预算）。
func defaultStorageSettings() storageSettings {
	persistPending := true
	autoRecover := false
	return storageSettings{
		MessageShardRows:      100,
		RetryCacheMaxItems:    64,
		RetryCacheMaxChars:    8 << 20,
		WireRecentErrors:      3,
		CompactFrameThreshold: 30,
		RawBytesAlert:         256 << 20,
		RetentionMode:         retentionModeManual,
		QueuePersistPending:   &persistPending,
		StaleAfterSeconds:     300,
		AutoRecover:           &autoRecover,
		BlobSoftLimitChars:    60000,
		BlobHardLimitBytes:    16 << 20,
		BlobSessionQuotaBytes: 64 << 20,
		WireBudgetTokens:      200000,
		WireSoftRatio:         0.75,
		WireTargetRatio:       0.60,
	}
}

// resolveStorageSettings 按覆盖链合并：base（默认）← limits ← persisted。
// 零值/nil 视为「未覆盖」。
func resolveStorageSettings(layers ...storageSettings) storageSettings {
	out := defaultStorageSettings()
	for _, layer := range layers {
		out = mergeStorageSettings(out, layer)
	}
	return out
}

func mergeStorageSettings(base, override storageSettings) storageSettings {
	if override.MessageShardRows != 0 {
		base.MessageShardRows = override.MessageShardRows
	}
	if override.RetryCacheMaxItems != 0 {
		base.RetryCacheMaxItems = override.RetryCacheMaxItems
	}
	if override.RetryCacheMaxChars != 0 {
		base.RetryCacheMaxChars = override.RetryCacheMaxChars
	}
	if override.WireRecentErrors != 0 {
		base.WireRecentErrors = override.WireRecentErrors
	}
	if override.CompactFrameThreshold != 0 {
		base.CompactFrameThreshold = override.CompactFrameThreshold
	}
	if override.RawBytesAlert != 0 {
		base.RawBytesAlert = override.RawBytesAlert
	}
	if override.RetentionMode != "" {
		base.RetentionMode = override.RetentionMode
	}
	if override.QueuePersistPending != nil {
		base.QueuePersistPending = override.QueuePersistPending
	}
	if override.StaleAfterSeconds != 0 {
		base.StaleAfterSeconds = override.StaleAfterSeconds
	}
	if override.AutoRecover != nil {
		base.AutoRecover = override.AutoRecover
	}
	if override.BlobSoftLimitChars != 0 {
		base.BlobSoftLimitChars = override.BlobSoftLimitChars
	}
	if override.BlobHardLimitBytes != 0 {
		base.BlobHardLimitBytes = override.BlobHardLimitBytes
	}
	if override.BlobSessionQuotaBytes != 0 {
		base.BlobSessionQuotaBytes = override.BlobSessionQuotaBytes
	}
	if override.WireBudgetTokens != 0 {
		base.WireBudgetTokens = override.WireBudgetTokens
	}
	if override.WireSoftRatio != 0 {
		base.WireSoftRatio = override.WireSoftRatio
	}
	if override.WireTargetRatio != 0 {
		base.WireTargetRatio = override.WireTargetRatio
	}
	return base
}

// validateStorageSettings 校验配置覆盖（非法值显式拒绝）。
func validateStorageSettings(config storageSettings) error {
	if config.MessageShardRows < 1 {
		return errors.New("session storage: message.shard_rows must be > 0")
	}
	if config.WireRecentErrors < 1 {
		return fmt.Errorf("session storage: wire_recent_errors must be >= 1 (got %d)", config.WireRecentErrors)
	}
	if config.CompactFrameThreshold < 1 {
		return errors.New("session storage: compact_frame_threshold must be > 0")
	}
	if config.RetryCacheMaxItems < 1 || config.RetryCacheMaxChars < 1 {
		return errors.New("session storage: retry_cache limits must be > 0")
	}
	if config.BlobSoftLimitChars < 1 || config.BlobHardLimitBytes < config.BlobSoftLimitChars {
		return errors.New("session storage: big_tool_result limits invalid")
	}
	if config.BlobSessionQuotaBytes < config.BlobHardLimitBytes {
		return errors.New("session storage: session quota must cover hard limit")
	}
	if config.RetentionMode != retentionModeManual {
		return errors.New("session storage: retention mode must be manual (current implementation)")
	}
	if config.StaleAfterSeconds < 1 {
		return errors.New("session storage: lock.stale_after_seconds must be > 0")
	}
	if config.WireBudgetTokens < 1 {
		return errors.New("session storage: wire.budget_tokens must be > 0")
	}
	if !(config.WireSoftRatio > 0 && config.WireSoftRatio <= 1) ||
		!(config.WireTargetRatio > 0 && config.WireTargetRatio <= config.WireSoftRatio) {
		return errors.New("session storage: wire ratios must be 0 < target <= soft <= 1")
	}
	return nil
}

// shardRows 返回 message/event 分片行数（已解析，恒 > 0）。
func (settings storageSettings) shardRows() int { return settings.MessageShardRows }

// wireBudget 返回 (budget, softTokens, targetTokens)（§5.2）。
func (settings storageSettings) wireBudget() (int, int, int) {
	budget := settings.WireBudgetTokens
	if budget <= 0 {
		budget = defaultStorageSettings().WireBudgetTokens
	}
	softRatio := settings.WireSoftRatio
	if softRatio <= 0 {
		softRatio = defaultStorageSettings().WireSoftRatio
	}
	targetRatio := settings.WireTargetRatio
	if targetRatio <= 0 {
		targetRatio = defaultStorageSettings().WireTargetRatio
	}
	return budget, int(float64(budget) * softRatio), int(float64(budget) * targetRatio)
}
