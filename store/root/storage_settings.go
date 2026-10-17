// 魔法数字与配置覆盖链（my_design §11）。
//
// 设计稿的键是 `session_storage.<域>.<叶子>`；落到配置体系里保留同一条覆盖链，
// 但键名用扁平叶子名（与 seele.yaml `limits:` 段的既有风格一致）：
//
//	默认值  ←  limits.session_storage.*（seele.yaml）  ←  session-storage.json
//
// 三层只在 `Router` 打开后端时合并一次（`ResolveSettings`），此后运行期
// 不再改：各通道从 storeEngine.settings 读数，禁止再各自硬编码。
package root

import (
	"errors"
	"fmt"
)

// NewSettings 返回一个只含指定覆盖项的 Settings（全部零值 = 用默认）。
func NewSettings() Settings { return Settings{} }

// Settings 是 v8 存储运行参数（默认值 = my_design §11 初稿）。装配根（wiring/bootstrap）
// 注入覆盖层：字段零值 = 未配置，由 ResolveSettings 补默认值；布尔项用指针，
// 因为设计默认值里有 false，零值无法区分「未设置」。
type Settings struct {
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
	// AllowMultiProcess 让数据根独占锁在冲突时**不拒绝**本进程（§9）：nil/false =
	// 单实例（默认，冲突即报错），true = 放行（第二个进程继续启动）。用指针是为了
	// 让「未配置」与「显式 false」区分得开（与 AutoRecover 同一纪律：默认值里就有
	// false，零值无法区分）。代价（失去跨进程写者串行化）见 config/seelex.yaml。
	AllowMultiProcess *bool `json:"allow_multi_process,omitempty"`
	// ── 文本大结果 blob 限额（设计 §2.6）──
	// 语义：软限**截断**正文、硬限**不落盘**直接报错（ErrBigToolResultTooLarge）、
	// 会话配额超限报错（ErrBigToolResultQuota）；软限**不调 LLM 做总结**。
	//
	// 现状（2026-10-15）：唯一写入路径 writeBlob 目前**没有生产调用方**（只有包内
	// 单测），因此这三项只在 WithDefaults/Apply 校验与单测里生效。生产里"超大工具
	// 结果"走的是 limits.max_tool_result_chars（**字符数**）→ result_ref + 省略警告
	// （application/core/context_runtime 的 OversizedToolResultWarning），两族数别混。
	BlobSoftLimitChars    int `json:"big_tool_result_soft_limit_chars,omitempty"`
	BlobHardLimitBytes    int `json:"big_tool_result_hard_limit_bytes,omitempty"`
	BlobSessionQuotaBytes int `json:"big_tool_result_session_quota_bytes,omitempty"`
	// 媒体分区（§10）限额：只按字节与像素卡硬限，没有字符软限、永不截断。
	// 配额与 big_tool_result 独立计账，互不挤占（见 media.go 顶部说明）。
	MediaMaxItemBytes       int `json:"media_max_item_bytes,omitempty"`
	MediaSessionQuotaBytes  int `json:"media_session_quota_bytes,omitempty"`
	MediaMaxItemsPerSession int `json:"media_max_items_per_session,omitempty"`
	MediaMaxLongSide        int `json:"media_max_long_side,omitempty"`
	// WireBudgetTokens / WireSoftRatio / WireTargetRatio 是 §5.2 的 wire 装配
	// 预算：软阈值触发压缩、目标阈值决定裁剪到哪。
	WireBudgetTokens int     `json:"wire_budget_tokens,omitempty"`
	WireSoftRatio    float64 `json:"wire_soft_ratio,omitempty"`
	WireTargetRatio  float64 `json:"wire_target_ratio,omitempty"`
}

// RetentionModeManual 是当前唯一实现模式（用户确认后行删除；dry_run=true 为默认安全姿态，
// 见 my_design §11）。
const RetentionModeManual = "manual"

// 布尔项用指针：设计默认值里有 false（auto_recover），零值无法区分「未设置」。
func boolValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

// DefaultSettings 返回设计默认值（§11 表格 + §5.2 预算）。
func DefaultSettings() Settings {
	persistPending := true
	autoRecover := false
	return Settings{
		MessageShardRows:      100,
		RetryCacheMaxItems:    64,
		RetryCacheMaxChars:    8 << 20,
		WireRecentErrors:      3,
		CompactFrameThreshold: 30,
		RawBytesAlert:         256 << 20,
		RetentionMode:         RetentionModeManual,
		QueuePersistPending:   &persistPending,
		StaleAfterSeconds:     300,
		AutoRecover:           &autoRecover,
		BlobSoftLimitChars:    60000,
		BlobHardLimitBytes:    16 << 20,
		BlobSessionQuotaBytes: 64 << 20,
		// 媒体默认值（量级建议，按 4K 截屏的典型体积标定）：
		// 单件 8 MB 能收原图又挡住异常大件；长边 4096 覆盖 4K 宽屏；
		// 会话 256 MB ≈ 200 张 1600px 截图（每张 0.5~1.5 MB）。
		MediaMaxItemBytes:       8 << 20,
		MediaSessionQuotaBytes:  256 << 20,
		MediaMaxItemsPerSession: 500,
		MediaMaxLongSide:        4096,
		WireBudgetTokens:        200000,
		WireSoftRatio:           0.75,
		WireTargetRatio:         0.60,
	}
}

// ResolveSettings 按覆盖链合并：base（默认）← limits ← persisted。
// 零值/nil 视为「未覆盖」。
func ResolveSettings(layers ...Settings) Settings {
	out := DefaultSettings()
	for _, layer := range layers {
		out = MergeSettings(out, layer)
	}
	return out
}

func MergeSettings(base, override Settings) Settings {
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
	if override.AllowMultiProcess != nil {
		base.AllowMultiProcess = override.AllowMultiProcess
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
	if override.MediaMaxItemBytes != 0 {
		base.MediaMaxItemBytes = override.MediaMaxItemBytes
	}
	if override.MediaSessionQuotaBytes != 0 {
		base.MediaSessionQuotaBytes = override.MediaSessionQuotaBytes
	}
	if override.MediaMaxItemsPerSession != 0 {
		base.MediaMaxItemsPerSession = override.MediaMaxItemsPerSession
	}
	if override.MediaMaxLongSide != 0 {
		base.MediaMaxLongSide = override.MediaMaxLongSide
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

// ValidateSettings 校验配置覆盖（非法值显式拒绝）。
func ValidateSettings(config Settings) error {
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
	if config.MediaMaxItemBytes < 1 || config.MediaMaxItemsPerSession < 1 || config.MediaMaxLongSide < 1 {
		return errors.New("session storage: media limits must be > 0")
	}
	if config.MediaSessionQuotaBytes < config.MediaMaxItemBytes {
		return errors.New("session storage: media session quota must cover media max item bytes")
	}
	if config.RetentionMode != RetentionModeManual {
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

// ShardRows 返回 message/event 分片行数（已解析，恒 > 0）。
func (settings Settings) ShardRows() int { return settings.MessageShardRows }

// WireBudget 返回 (budget, softTokens, targetTokens)（§5.2）。
func (settings Settings) WireBudget() (int, int, int) {
	budget := settings.WireBudgetTokens
	if budget <= 0 {
		budget = DefaultSettings().WireBudgetTokens
	}
	softRatio := settings.WireSoftRatio
	if softRatio <= 0 {
		softRatio = DefaultSettings().WireSoftRatio
	}
	targetRatio := settings.WireTargetRatio
	if targetRatio <= 0 {
		targetRatio = DefaultSettings().WireTargetRatio
	}
	return budget, int(float64(budget) * softRatio), int(float64(budget) * targetRatio)
}
