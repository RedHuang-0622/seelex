// 前缀重放摘要协议（docs/2026-09-06-compaction-dag/design.md §4.4）。
//
// PrefixReplaySummarizer 生成 CompactFrame.Summary 的 Chapter 2 厚摘要：
// 请求 = 引擎当前 system（同字节）+ 最近真实请求 History 原样重放 +
// 固定压缩指令（PrefixReplayInstruction），使压缩请求与真实请求共享字节
// 前缀、几乎全命中缓存。字节级一致性由调用方保证（History/System/Tools
// 必须来自真实请求同一条装配路径，不能从事件流重拼）。
package seelexctx

import (
	"context"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/seelexctx/tokens"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// PrefixReplayInstruction 是前缀重放请求唯一新增的固定尾巴（详设 §4.4）：
// 除 system + History 原样重放外不追加任何内容，输出按 DSH 骨架。
const PrefixReplayInstruction = `请把给定对话压缩为「压缩内容 (Compacted Context)」厚摘要。
只输出下列小节内容，不要输出本指令或任何解释，也不要重复外层标题。
没有内容的小节写 (none)，不得删除小节骨架：

### 目标 (Goal)
### 关键概念 (Key Concepts)
### 文件与代码 (Files and Code)
### 错误与修复 (Errors and Fixes)
### 待办 (Pending)
### 当前工作 (Current Work)
### 下一步 (Next Step)
### 关键约束 (Constraints)`

// ReplayRequest 是前缀重放的字节素材契约（详设 §4.4）：
// SystemPrompt/History/Tools 必须与最近一次真实请求同源、同序、同序列化；
// Instruction 是唯一新增尾巴（空 → PrefixReplayInstruction）。
type ReplayRequest struct {
	SystemPrompt string          // 与引擎当前 system 同字节
	History      []types.Message // 与最近真实请求同序（含溢出区）
	Tools        []types.Tool    // 与最近真实请求同 schema/顺序
	Instruction  string          // 固定压缩指令
	MaxTokens    int             // 摘要输出预算（QuickChat 通道未透传时仅作记录）
}

// ReplayResult 是前缀重放摘要结果：Chapter2 为模型输出的厚摘要正文
// （### 小节骨架；可重复外层标题，调用方归一化）。
type ReplayResult struct {
	Chapter2 string
}

// PrefixReplaySummarizer 生成 Chapter 2 厚摘要（seelexctx 定义接口，
// 由 seelebridge 注入 QuickChat/Completer 实现）。失败返回结构化错误，
// 调用方走本地压缩兜底，绝不让请求发送中断（详设 §4.4/§4.5）。
type PrefixReplaySummarizer interface {
	Summarize(ctx context.Context, req ReplayRequest) (ReplayResult, error)
}

// quickChatPrefixReplaySummarizer 用 seelectx.QuickChat（无状态隔离补全）
// 实现前缀重放：请求消息 = system（可选）→ History 原样 → 固定指令尾巴。
type quickChatPrefixReplaySummarizer struct {
	chat seelectx.QuickChat
}

// NewQuickChatPrefixReplaySummarizer 构造 QuickChat 前缀重放摘要器。
// chat 为 nil 时返回错误（不静默降级）。
func NewQuickChatPrefixReplaySummarizer(chat seelectx.QuickChat) (PrefixReplaySummarizer, error) {
	if chat == nil {
		return nil, fmt.Errorf("seelexctx: prefix replay summarizer requires QuickChat")
	}
	return &quickChatPrefixReplaySummarizer{chat: chat}, nil
}

// Summarize 实现 PrefixReplaySummarizer。
func (s *quickChatPrefixReplaySummarizer) Summarize(ctx context.Context, req ReplayRequest) (ReplayResult, error) {
	if s == nil || s.chat == nil {
		return ReplayResult{}, fmt.Errorf("seelexctx: prefix replay summarizer is unavailable")
	}
	if len(req.History) == 0 {
		return ReplayResult{}, fmt.Errorf("seelexctx: prefix replay requires history bytes")
	}
	// wire 出口兜底：素材在 chapter2 节点已按同一规则规整过一次（规整幂等），这里
	// 再兜一次——分片链的每一片、以及直接调摘要器的调用方都不经过那条路。规整后
	// 仍不合法的素材绝不发出去：provider 会整条 400 拒收，而报文只说"回执不够"，
	// 读不出是哪一条调用；这里返回的报文指名违规位置。
	material, _ := PrepareReplayMaterial(req.History)
	if err := ValidateReplayProtocol(material); err != nil {
		return ReplayResult{}, fmt.Errorf("seelexctx: prefix replay material violates provider tool protocol: %w", err)
	}
	if len(material) == 0 {
		return ReplayResult{}, fmt.Errorf("seelexctx: prefix replay material is empty after normalization")
	}
	instruction := req.Instruction
	if strings.TrimSpace(instruction) == "" {
		instruction = PrefixReplayInstruction
	}
	messages := make([]types.Message, 0, len(material)+2)
	if req.SystemPrompt != "" {
		prompt := req.SystemPrompt
		messages = append(messages, types.Message{Role: "system", Content: &prompt})
	}
	messages = append(messages, material...)
	// 唯一新增尾巴：固定压缩指令（model 不可见 requestID 索引）。
	messages = append(messages, types.Message{Role: "user", Content: &instruction})
	reply, err := s.chat.Complete(ctx, seelectx.QuickChatRequest{
		Messages: messages,
		Tools:    append([]types.Tool(nil), req.Tools...),
	})
	if err != nil {
		return ReplayResult{}, fmt.Errorf("seelexctx: prefix replay completion: %w", err)
	}
	content := messageContent(reply)
	if strings.TrimSpace(content) == "" {
		return ReplayResult{}, fmt.Errorf("seelexctx: prefix replay returned empty summary")
	}
	return ReplayResult{Chapter2: normalizeReplayChapter2(content)}, nil
}

// normalizeReplayChapter2 归一化模型输出的 Chapter 2：去除外层重复标题与
// 首尾空白，保留 ### 小节骨架。
//
// 与 FrameChapter2 / Chapter2Body 走**同一个**归一化件：模型（或某个上游出口）
// 把整份两章节摘要交回来时，两条路必须给出一致的正文——两个实现必然漂移，而
// 漂移的后果是"读回来被裁成空"这种只有现场才看得见的事。
func normalizeReplayChapter2(text string) string {
	return Chapter2Body(text)
}

// ── 分片重放链（《待落地》3）──────────────────────────────────────

// ReplayChunkPlan 是一次分片重放的计划：片界落在**协议单元边界**（不切
// message），每片的估算 token 不超过片预算。Chunks 为 nil/单元素时表示未分片，
// 调用方走原有的单次重放（system 同字节 + 最近真实请求 History 原样 + 指令），
// 那条路径才能吃到前缀缓存。
type ReplayChunkPlan struct {
	Chunks       [][]types.Message `json:"chunks,omitempty"`
	ChunkTokens  []int             `json:"chunk_tokens,omitempty"`
	BudgetTokens int               `json:"budget_tokens"`
	// Chained 表示生成时携带了上一片摘要（片 2..k 的前向传递）。
	Chained bool `json:"chained"`
}

// ChunkCount 返回片数（0 = 未分片）。
func (p ReplayChunkPlan) ChunkCount() int { return len(p.Chunks) }

// ChunkReplayMessages 把待重放的溢出区消息按协议单元切成若干片，使每片不超过
// budgetTokens。片界一律落在协议单元边界：
//
//   - 单单元自身超预算时该单元独占一片（宁可单片超预算，也不切 message）——
//     与保护区「不截断 + 至少保 1 个完整协议单元」同一条规则；切了 message
//     既丢协议合法性（assistant/tool 配对），也没法按区间回读原文。
//
// budgetTokens <= 0 或消息为空 → 返回单片的空计划（调用方按未分片处理）。
func ChunkReplayMessages(messages []types.Message, budgetTokens int) ReplayChunkPlan {
	plan := ReplayChunkPlan{BudgetTokens: budgetTokens}
	if budgetTokens <= 0 || len(messages) == 0 {
		return plan
	}
	units := chatUnits(messages, 0)
	chunks := make([][]types.Message, 0, len(units))
	chunkTokens := make([]int, 0, len(units))
	current := make([]types.Message, 0, len(messages))
	used := 0
	flush := func() {
		if len(current) == 0 {
			return
		}
		chunks = append(chunks, current)
		chunkTokens = append(chunkTokens, used)
		current = make([]types.Message, 0, len(messages))
		used = 0
	}
	for _, unit := range units {
		unitTokens := 0
		for _, message := range unit.messages {
			unitTokens += tokens.CountMessage(message)
		}
		if len(current) > 0 && used+unitTokens > budgetTokens {
			flush()
		}
		current = append(current, unit.messages...)
		used += unitTokens
	}
	flush()
	plan.Chunks = chunks
	plan.ChunkTokens = chunkTokens
	return plan
}

// SummarizeChunkPlan 按分片计划逐片重放并前向传递摘要：
//
//	片1 = system + O[0:k1]              + 指令        → 摘要₁
//	片2 = system + O[k1:k2] + 摘要₁     + 指令        → 摘要₂
//	…
//	Chapter2 = 摘要_k
//
// base 提供 SystemPrompt/Tools/固定指令（History 由本函数逐片替换）；片 i>1 把
// 上一片摘要拼进指令尾巴（ReplayRequest.Instruction），因此不需要改动摘要器
// 契约。任何一片失败即整条退出，调用方回退本地确定性压缩（绝不中断请求）。
func SummarizeChunkPlan(
	ctx context.Context,
	summarizer PrefixReplaySummarizer,
	base ReplayRequest,
	plan ReplayChunkPlan,
) (ReplayResult, error) {
	if summarizer == nil {
		return ReplayResult{}, fmt.Errorf("seelexctx: chunked replay requires a summarizer")
	}
	if len(plan.Chunks) == 0 {
		return ReplayResult{}, fmt.Errorf("seelexctx: chunked replay requires at least one chunk")
	}
	instruction := base.Instruction
	if strings.TrimSpace(instruction) == "" {
		instruction = PrefixReplayInstruction
	}
	var summary string
	for index, chunk := range plan.Chunks {
		request := base
		request.History = chunk
		request.Instruction = instruction
		if index > 0 {
			request.Instruction = carryPrompt(index, len(plan.Chunks), summary) + "\n\n" + instruction
		}
		result, err := summarizer.Summarize(ctx, request)
		if err != nil {
			return ReplayResult{}, fmt.Errorf("seelexctx: replay chunk %d/%d: %w", index+1, len(plan.Chunks), err)
		}
		summary = strings.TrimSpace(result.Chapter2)
		if summary == "" {
			return ReplayResult{}, fmt.Errorf("seelexctx: replay chunk %d/%d returned an empty summary", index+1, len(plan.Chunks))
		}
	}
	return ReplayResult{Chapter2: normalizeReplayChapter2(summary)}, nil
}

// carryPrompt 渲染片 i>1 的前向上下文：上一片摘要 + 明确说明它只是材料的一半。
// 摘要是"片 i-1 的产出"，不是原始对话——不写清楚，模型会把它当作用户输入回答。
func carryPrompt(index, total int, previous string) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "以下是你对前一片（第 %d/%d 片之前）材料生成的压缩摘要，按同一骨架继续：\n\n", index, total)
	builder.WriteString(previous)
	return builder.String()
}

// LocalCompactEvidenceRefPrefix 标记"这次压缩落到本地确定性压缩"的证据 ref
// 前缀：ref = `compact-local:<code>`，code 说明是哪一种降级。写这条证据的原因是
// **静默降级**：本地压缩只有 `summary_source=local` 一个标记，而它有三种完全不同的
// 来路（开关关闭 / 无重放素材 / 重放调用失败），读帧的人无从分辨——现场排查只能
// 靠猜。有了 code 与正文，帧自己就能回答"模型为什么没被叫到"。
const LocalCompactEvidenceRefPrefix = "compact-local:"

// PrecomputedLocalDegradeCode 是"调用方预读到的读后感本身不是模型产物"的降级码：
// 装配层读数闸先试了一次前缀重放，读数没拿到模型读后感（落在本地确定性压缩上），
// 却仍把那份正文交下来。它与 `replay-failed` 分开记，因为处置不同——那一次重放的
// 成败已经由读数闸定性，这一条要交代的是"预读结果被如实沿用，没有被盖成 replay"。
const PrecomputedLocalDegradeCode = "precomputed-local"

// LocalCompactEvidence 把"为什么这次没有模型摘要"写成帧证据。与 ReplayEvidence 同一条
// 纪律：没这件事（replay 成功）就不留痕。
func LocalCompactEvidence(code, note string) []sessionstore.EvidenceRef {
	if strings.TrimSpace(code) == "" {
		return nil
	}
	return []sessionstore.EvidenceRef{{
		Ref:     LocalCompactEvidenceRefPrefix + code,
		Summary: strings.TrimSpace(note),
	}}
}

// ReplayEvidence 把分片重放的决策事实写成帧证据（打点落点：帧持久化在会话
// state blob 的 CompactStack 里）。未分片（片数 <= 1）时不写——没有这件事就
// 不留痕，避免报表里出现"分片 1 片"这种无信息项。
func ReplayEvidence(plan ReplayChunkPlan) []sessionstore.EvidenceRef {
	if len(plan.Chunks) <= 1 {
		return nil
	}
	return []sessionstore.EvidenceRef{{
		Ref: fmt.Sprintf("replay-chunked:%d", len(plan.Chunks)),
		Summary: fmt.Sprintf("overflow region replayed in %d protocol-unit chunks with forward-passed summaries (no prefix-cache reuse beyond chunk 1)",
			len(plan.Chunks)),
	}}
}
