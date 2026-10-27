// wire 装配读取器（v8.1 §5）：会话事实 → 下一次发给 LLM 的 wire 消息序列。
//
// 定位：
//   - 纯函数、只读、无副作用；只消费 guide/module head/message/compact +
//     运行期尝试缓存 + 请求参数，**不读取 EVENT**（R2-EVENT-1）；
//   - wire = compact 摘要正文 + 最新帧 message_to 之后的事件行 + 同一操作
//     最近 K 条尝试；预算超限停在最近完整协议单元边界并置 need_compact；
//   - 孤儿 tool 行跳过；残缺工具轮保留为 open 单元 + 修复占位，不跳后续
//     user（R2-OPEN-1 / R2-ORPHAN-1 / R2-FILTER-1）；
//   - 输出前缀稳定性：无新消息、同参数、同缓存状态下两次输出逐字节一致
//     （R2-STABLE-1）。
package store

import (
	storecontext "github.com/RedHuang-0622/seelex/store/context"
	storeroot "github.com/RedHuang-0622/seelex/store/root"
)

// wireRole 常量与 provider role 对齐（internal 材料以 user 形态进入 wire）。
const (
	wireRoleSystem    = storecontext.WireRoleSystem
	wireRoleUser      = storecontext.WireRoleUser
	wireRoleAssistant = storecontext.WireRoleAssistant
	wireRoleTool      = storecontext.WireRoleTool
)

type wireMessage = storecontext.WireMessage

// defaultWireBudgetTokens 是 wire 装配的兜底预算（字符口径）：只在调用方既未
// 显式给 Budget、又未按配置解析（applyWireBudget）时生效。
// 生产装配路径（assembleWireWorkspace / assembleRoleWire）恒显式注入解析后的
// 预算——不再各自硬编码。
const defaultWireBudgetTokens = storecontext.DefaultWireBudget

type wireParams = storecontext.WireParams

func normalizeWireParams(params wireParams) wireParams {
	return storecontext.NormalizeWireParams(params)
}

// applyWireBudget 把 §5.2 的 wire 装配预算落到一次装配请求，返回装配实际使用的
// **软阈值**（触发压缩的预算）：
//   - 调用方给了绝对预算（由账号上下文窗口推导，如 budget.TargetAfterCompaction）
//     → 按 WireSoftRatio 取软阈值，保持与调用方同轴；
//   - 未给 → 用配置 (WireBudgetTokens × WireSoftRatio) 的绝对值。
//
// 这是 Settings.WireBudgetTokens/WireSoftRatio 在装配链路上的唯一消费点：此前
// 配置侧已就位（Settings.WireBudget() = 200000/150000/120000，仅测试断言），
// 装配侧却硬编码单一预算、软阈值从未生效——「软阈值触发压缩」因此孤立不可用。
// WireTargetRatio（裁剪到目标）属压缩路径口径，由调用方以
// budget.TargetAfterCompaction 消费，不在装配侧重复。
//
// 接收者改成第一个参数（原 `Settings.applyWireBudget`，随 M3-S2 下沉
// Settings 时留在本包）：判据本身逐字未变，只是 `wireParams` 属本包的装配域，
// 不随 `Settings` 进 `store/root`。
func applyWireBudget(settings storeroot.Settings, base wireParams) wireParams {
	return storecontext.ApplyWireBudget(settings, base)
}

type wireResult = storecontext.WireResult

// assembleWire 构造 wire 消息序列（R2 主入口）。
func (store *storeEngine) assembleWire(key Key, cache *attemptCache, params wireParams) (wireResult, error) {
	params = normalizeWireParams(params)
	compactHead, err := store.readCompactHead(key)
	if err != nil {
		return wireResult{}, err
	}
	tailStart := uint64(1)
	frame := compactHead.LatestFrame
	if frame != nil {
		tailStart = frame.MessageToSeq + 1
	}
	rows, err := store.readRows(key, tailStart, 0)
	if err != nil {
		return wireResult{}, err
	}
	return assembleWireRows(frame, rows, cache, params), nil
}

// assembleWireRows 是 wire 装配的纯函数核心：给定最新帧（可空）与尾行，
// 产出 wire 序列（frame 摘要 + 尾窗 + 最近 K 条尝试 + 预算/修复语义）。
// legacy JSON 会话经该纯函数复用同一装配逻辑（取代旧链路组装）。
type rootWireAttemptProvider struct{ cache *attemptCache }

func (provider rootWireAttemptProvider) RecentForAnchor(anchorSeq uint64, k int) []storecontext.WireAttempt {
	items := provider.cache.recentForAllAnchors(anchorSeq, k)
	out := make([]storecontext.WireAttempt, 0, len(items))
	for _, item := range items {
		out = append(out, storecontext.WireAttempt{Role: item.Role, Content: item.Content})
	}
	return out
}

func contextWireRows(rows []Event) []storecontext.WireRow {
	out := make([]storecontext.WireRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, storecontext.WireRow{
			Seq: row.Seq, Kind: EventKindOf(row), Role: row.Role,
			ReasoningContent: row.ReasoningContent, Content: row.Content,
			ProviderContent: row.ProviderContent, ToolCallID: row.ToolCallID,
			Name: row.Name, ToolCalls: row.ToolCalls, ResultRef: row.ResultRef,
			TokenCount: row.TokenCount, WireMaterial: row.WireMaterial,
		})
	}
	return out
}

func assembleWireRows(frame *compactFrameRecord, rows []Event, cache *attemptCache, params wireParams) wireResult {
	var wireFrame *storecontext.WireFrame
	if frame != nil {
		wireFrame = &storecontext.WireFrame{Summary: frame.Summary, MessageToSeq: frame.MessageToSeq}
	}
	return storecontext.AssembleWireRows(wireFrame, contextWireRows(rows), rootWireAttemptProvider{cache: cache}, params)
}

func wireDigest(messages []wireMessage) string { return storecontext.WireDigest(messages) }

func wirePrefixEqual(left, right []wireMessage) bool {
	return storecontext.WirePrefixEqual(left, right)
}

// recentForAllAnchors 归并 AttemptCache.RecentFor 的辅助（按 op 归组取最近
// K；当前单 op 场景与测试一致）。
func (cache *attemptCache) recentForAllAnchors(anchorSeq uint64, k int) []attempt {
	seen := make(map[string]bool)
	var out []attempt
	items := cache.snapshot()
	for _, item := range items {
		if item.AnchorSeq != anchorSeq || seen[item.OperationKey] {
			continue
		}
		seen[item.OperationKey] = true
		out = append(out, cache.RecentFor(anchorSeq, item.OperationKey, k)...)
	}
	return out
}

func (cache *attemptCache) snapshot() []attempt {
	if cache == nil {
		return nil
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return append([]attempt(nil), cache.items...)
}
