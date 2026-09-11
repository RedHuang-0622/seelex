package goal

// advisor.go — DS-A2A 治理落地（协议 docs/2026-09-07-seele-a2a-framework-req/ds-a2a-protocol.md §1-§10 的
// goal 域可运行切片；配套 ds-a2a-detailed-design.md §4）。
//
// 模型（取代旧的"同会话双角色共享上下文"治理，即 techleader.go 旧的每回合从 Controller
// 实时重建 goal 帧 + SetSessionTail 喂 a 尾窗 + AppendDirective 写回 goal 共享指令环）：
//
//	EXEC(a)  = Controller + Supervisor 事件登记（a 事件账本水位 execSeq 单调，turn 仅推进水位不帧化）
//	ADVISOR(b) = AdvisorSession：独立上下文 = 锚点帧(goal.start) + 追加帧（ref_seq 单调, 只尾部追加）
//	            + b 自身回合段 selfRounds（= 用户例子中 b 的 6(b)）
//	b 回合输入 = b 上下文自身渲染（不含 a 实时转录/尾窗）→ 前缀稳定 + 只尾部追加
//	          ⇒ 相邻回合输入公共前缀记 cached_input_tokens → 命中回升可观测（协议 §2 C1-C5）
//	b→a 产物 = corr 信封指令（幂等队列，受信注入 a，不回写 goal 共享状态）
//	B4 铁律 = a 永不等待 b：回合失败（429/超时）→ gate 按缺席矩阵（low 放行 / high escalate）
//
// 并发：AdvisorSession 无自锁，全部访问须在 Supervisor.mu 下（回合串行、快照持锁），保证 -race 安全。

import (
	"fmt"
	"strings"
)

// PeerState 是 b 生命周期状态（协议 §9，headless/前端可投影）。
type PeerState string

const (
	PeerDetached        PeerState = "detached"
	PeerBound           PeerState = "bound"
	PeerEvaluating      PeerState = "evaluating"
	PeerAdvisoryPending PeerState = "advisory_pending"
	PeerReaped          PeerState = "reaped" // unbind 后（reason=done|evicted|killed）
)

// FrameKind 是 a→b 帧类型（协议 §4 默认抽帧集；transcript.inc 默认不进 b = 跳帧控量）。
type FrameKind string

const (
	FrameGoalStart         FrameKind = "goal.start"         // 锚点：goal 域创建时快照（必进）
	FrameGoalUpdated       FrameKind = "goal.update"        // 目标更新（按策略进）
	FrameStepCheckpoint    FrameKind = "tool.checkpoint"    // 里程碑/打点（推荐进）
	FrameContextCompacted  FrameKind = "context.compacted"  // a 上下文压缩（必进，防遗忘）
	FrameApprovalRequested FrameKind = "approval.requested" // 审批预筛（按策略进）
	FrameTerminalProposed  FrameKind = "terminal.proposed"  // 终态提议（必进，触发 gate）
)

var validFrameKinds = map[FrameKind]bool{
	FrameGoalStart: true, FrameGoalUpdated: true, FrameStepCheckpoint: true,
	FrameContextCompacted: true, FrameApprovalRequested: true, FrameTerminalProposed: true,
}

// Frame 是 a→b 同步单元（协议 §3/§4 信封子集；ref_seq = 基于 a 事件水位）。
type Frame struct {
	Kind   FrameKind `json:"kind"`
	RefSeq uint64    `json:"ref_seq"`
	At     int64     `json:"at,omitempty"`
	Source string    `json:"source,omitempty"`
	Detail string    `json:"detail,omitempty"`
}

// Validate 校验帧字段（有界：detail ≤ MaxSignalDetailRunes）。
func (f Frame) Validate() error {
	if !validFrameKinds[f.Kind] {
		return fmt.Errorf("%w: 非法帧 kind %q", ErrInvalidArgument, f.Kind)
	}
	if len([]rune(f.Detail)) > MaxSignalDetailRunes {
		return fmt.Errorf("%w: 帧 detail 超长（> %d runes）", ErrInvalidArgument, MaxSignalDetailRunes)
	}
	return nil
}

// Round 是 b 自身回合段（b 上下文尾部追加的 6(b) 类内容：advisory/verdict 摘要 + 缓存观测）。
type Round struct {
	At           int64         `json:"at"`
	Trigger      string        `json:"trigger,omitempty"`
	RefSeq       uint64        `json:"ref_seq"`        // 本回合评估基于的 a 水位
	Corr         string        `json:"corr,omitempty"` // b→a 幂等信封 id
	Kind         DirectiveKind `json:"kind"`
	Summary      string        `json:"summary"`
	InputTokens  int64         `json:"input_tokens"`
	CachedTokens int64         `json:"cached_input_tokens"` // 命中前缀 token（公共前缀）
}

// CacheStats 汇总 b 的缓存命中观测（协议 §2：验证"命中回升"）。
type CacheStats struct {
	Rounds           int   `json:"rounds"`
	TotalInputTokens int64 `json:"total_input_tokens"`
	TotalCached      int64 `json:"total_cached_input_tokens"`
	LastInputTokens  int64 `json:"last_input_tokens"`
	LastCached       int64 `json:"last_cached_input_tokens"`
	HitRatio         int   `json:"hit_ratio"` // 最近一次回合 cached/input *100
}

// AdvisorSession 是 ADVISOR(b) 的独立上下文（协议 §1/§2）。
type AdvisorSession struct {
	PeerID string    `json:"peer_id"`
	GoalID string    `json:"goal_id,omitempty"`
	State  PeerState `json:"state"`
	Rebind int       `json:"rebind,omitempty"` // 会话内 b 重建次数（goal 域同生命周期为 0/1）

	Anchor  GoalFrame  `json:"anchor"`      // 锚点快照（bind 时一次快照，不随后续 update 变化）
	Frames  []Frame    `json:"frames"`      // 追加帧（ref_seq 严格递增；只尾部追加）
	Rounds  []Round    `json:"rounds"`      // b 自身回合段（只尾部追加）
	Applied uint64     `json:"applied_seq"` // b 已应用 a 事件水位
	Head    uint64     `json:"head_seq"`    // a 当前水位（EXEC 账本）
	Cache   CacheStats `json:"cache"`
	Reason  string     `json:"unbind_reason,omitempty"`

	cachedInputText string // 上一回合输入全文（缓存命中 LCP 观测用；不导出 JSON）
	corrSeq         uint64 // b→a corr 信封自增（协议 §5 幂等）
}

// nextCorr 生成下一条 b→a 产物的关联 id（corr-<n>）。
func (b *AdvisorSession) nextCorr() string {
	b.corrSeq++
	return fmt.Sprintf("corr-%d", b.corrSeq)
}

// appendFrame 追加一帧（幂等：ref_seq ≤ 已应用水位 → dup=true 不追加；单调性检查）。
// 调用方须持 Supervisor.mu。
func (b *AdvisorSession) appendFrame(frame Frame) (bool, error) {
	if err := frame.Validate(); err != nil {
		return false, err
	}
	if frame.RefSeq <= b.Applied {
		return true, nil // dup_frame：幂等丢弃（协议 §7.2/§10）
	}
	if len(b.Frames) > 0 {
		last := b.Frames[len(b.Frames)-1].RefSeq
		if frame.RefSeq < last {
			return false, fmt.Errorf("%w: 帧回退 ref_seq %d < %d（跳帧合法，回退非法）",
				ErrInvalidArgument, frame.RefSeq, last)
		}
		if frame.RefSeq == last {
			return true, nil
		}
	}
	b.Frames = append(b.Frames, frame)
	b.Applied = frame.RefSeq
	return false, nil
}

// appendRound 追加一条 b 自身回合段（只尾部追加；cap MaxDirectives 保护展示记忆）。
func (b *AdvisorSession) appendRound(round Round) {
	b.Rounds = append(b.Rounds, round)
	if len(b.Rounds) > MaxEmbedRounds {
		drop := len(b.Rounds) - MaxEmbedRounds
		b.Rounds = append([]Round(nil), b.Rounds[drop:]...)
	}
}

// RoundMemories 返回最近自身回合摘要（TLMemory 素材：TL 记得自己说过什么，但不写 a 的 goal 状态）。
func (b *AdvisorSession) RoundMemories(limit int) []string {
	out := make([]string, 0, len(b.Rounds))
	start := 0
	if limit > 0 && len(b.Rounds) > limit {
		start = len(b.Rounds) - limit
	}
	for _, round := range b.Rounds[start:] {
		if round.Summary != "" {
			out = append(out, round.Summary)
		}
	}
	return out
}

// renderEmbed 构建一次 b 回合的有界输入：锚点 + 追加帧 + 自身回合记忆（全部来自 b 上下文，
// 不含 a 实时尾窗）。调用方须持 Supervisor.mu。
func (b *AdvisorSession) renderEmbed(trigger string) (TLSessionEmbed, string) {
	embed := TLSessionEmbed{
		PeerID:   b.PeerID,
		Goal:     b.Anchor,
		Frames:   append([]Frame(nil), b.Frames...),
		TLMemory: b.RoundMemories(MaxEmbedRounds),
		Trigger:  trigger,
	}
	return embed, embed.RenderText()
}

// MaxEmbedRounds 是 b 上下文保留的自身回合记忆上限（展示护栏）。
const MaxEmbedRounds = 8

// estimateTokens 是 token 估算（runes/4；仅用于原型缓存观测，真实 provider 用量由装配层上报）。
func estimateTokens(text string) int64 {
	n := int64(len([]rune(text)))
	if n <= 0 {
		return 0
	}
	return n/4 + 1
}

// RenderText 把一次 b 回合输入渲染为文本（P_b 角色说明 + 锚点 + 帧账本 + 自身回合段）。
// 前缀稳定（角色+锚点在前）→ 相邻回合公共前缀命中（协议 C3/C4）。
func (e TLSessionEmbed) RenderText() string {
	var builder strings.Builder
	builder.WriteString("[advisor] 你是评审者(ADVISOR)，上下文仅来自下列帧账本与你的回合记忆。\n")
	builder.WriteString("[goal-anchor]\n")
	builder.WriteString(goalFrameText(e.Goal))
	if len(e.Frames) > 0 {
		builder.WriteString("[frames]\n")
		for _, frame := range e.Frames {
			fmt.Fprintf(&builder, "- [%s] ref=%d", frame.Kind, frame.RefSeq)
			if frame.Source != "" {
				fmt.Fprintf(&builder, " src=%s", frame.Source)
			}
			if frame.Detail != "" {
				fmt.Fprintf(&builder, " : %s", frame.Detail)
			}
			builder.WriteString("\n")
		}
	}
	if len(e.TLMemory) > 0 {
		builder.WriteString("[your-rounds]\n")
		for _, memory := range e.TLMemory {
			fmt.Fprintf(&builder, "- %s\n", memory)
		}
	}
	if e.Trigger != "" {
		fmt.Fprintf(&builder, "[trigger] %s\n", e.Trigger)
	}
	return builder.String()
}

// goalFrameText 渲染锚点目标（复刻 Goal 帧关键信息，有界）。
func goalFrameText(frame GoalFrame) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "id=%s title=%s status=%s\n", frame.ID, frame.Title, frame.Status)
	if statement := strings.TrimSpace(frame.Statement); statement != "" {
		fmt.Fprintf(&builder, "statement: %s\n", statement)
	}
	if len(frame.Acceptance) > 0 {
		builder.WriteString("acceptance:\n")
		for _, item := range frame.Acceptance {
			fmt.Fprintf(&builder, "- %s\n", item)
		}
	}
	for _, item := range frame.Progress {
		fmt.Fprintf(&builder, "progress [%s]: %s\n", item.Kind, item.Content)
	}
	return builder.String()
}

// markEvaluating / markAdvisory / markBound 是状态机辅助（调用方持锁）。
func (b *AdvisorSession) markBound(peerID string, anchor GoalFrame, refSeq uint64, now int64) {
	b.PeerID = peerID
	b.GoalID = anchor.ID
	b.Anchor = anchor
	b.Applied = refSeq
	b.Head = refSeq
	b.State = PeerBound
	b.Reason = ""
}
