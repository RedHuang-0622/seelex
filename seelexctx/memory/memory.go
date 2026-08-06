// Package memory 提供超长上下文的历史记忆选取：按当前请求从压缩栈帧
// （CompactStack）中选出相关过往记忆，渲染为有界的「相关记忆」PromptBlock。
//
// 背景（2026-08-06）：会话压缩只渲染 CompactStack 栈顶帧，栈顶摘要递归
// 内嵌全部旧摘要——既不选择也不设界。超长会话里模型要么被迫接收全部
// 拼接摘要（无界），要么在将来截断后丢失久远但相关的记忆。本包把选取
// 与渲染做成显式一步：词法相关性打分（确定性、零外部依赖）→ top-K →
// token 有界块。
//
// 选取是确定性的词法匹配（ASCII 词 + CJK bigram），不引入向量基建；
// Select 输入输出都是纯数据结构，未来可被 embedding 检索器替换
// （docs/plan/memory-architecture.md 的 graph role 可用后）。
package memory

import (
	"strings"
	"unicode"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// Candidate 是一条可被选取的历史记忆：压缩栈中的一帧。
// Summary 是压缩摘要正文，Evidence 是指向不可变结果的证据引用。
type Candidate struct {
	SegmentID string
	Summary   string
	Evidence  []sessionstore.EvidenceRef
	From      int // 累计 ChatQueue 单元索引（CompactFrame.From）
	To        int // 累计 ChatQueue 单元索引（CompactFrame.To）
}

// Options 选取参数；零值字段回退默认（见 DefaultOptions）。
type Options struct {
	// Limit 最多返回的记忆条数（0 → 3）。
	Limit int
	// MinScore 最低命中分数（0 → 0：任何命中即入选；>0 过滤低相关）。
	MinScore float64
	// RecencyWeight 越新的记忆加分权重（0 → 0.15；加分 = weight*(i+1)/n）。
	RecencyWeight float64
	// MaxTokens 渲染块的 token 预算（0 → 1024；RenderMemoryBlock 消费）。
	MaxTokens int
}

// DefaultOptions 返回默认选取参数：limit=3、min_score=0、
// recency_weight=0.15、max_tokens=1024。
func DefaultOptions() Options {
	return Options{Limit: 3, MinScore: 0, RecencyWeight: 0.15, MaxTokens: 1024}
}

// Select 按查询词法相关性选取候选记忆，返回按分数降序的 top-K（仅命中项）。
// 查询为空或无可选词项 → 返回 nil（不渲染块）。
func Select(query string, candidates []Candidate, opts Options) []Candidate {
	if strings.TrimSpace(query) == "" || len(candidates) == 0 {
		return nil
	}
	opts.applyDefaults()
	terms := tokenize(query)
	if len(terms) == 0 {
		return nil
	}
	scored := make([]scoredCandidate, 0, len(candidates))
	for index, candidate := range candidates {
		score := candidate.score(terms)
		if score <= 0 || score < opts.MinScore {
			continue // MinScore 作用于命中分数（recency 加分前），过滤弱相关
		}
		// recency 加分：越新的压缩段（索引越大）轻微优先，同分时新者排前。
		bonus := opts.RecencyWeight * float64(index+1) / float64(len(candidates))
		scored = append(scored, scoredCandidate{candidate: candidate, score: score + bonus})
	}
	// 分数降序；同分时越新的候选（To 大）越靠前。
	sortScored(scored)
	selected := make([]Candidate, 0, min(opts.Limit, len(scored)))
	for _, entry := range scored {
		selected = append(selected, entry.candidate)
		if len(selected) >= opts.Limit {
			break
		}
	}
	if len(selected) == 0 {
		return nil
	}
	return selected
}

func (o *Options) applyDefaults() {
	def := DefaultOptions()
	if o.Limit <= 0 {
		o.Limit = def.Limit
	}
	if o.MinScore <= 0 {
		o.MinScore = def.MinScore
	}
	if o.RecencyWeight <= 0 {
		o.RecencyWeight = def.RecencyWeight
	}
	if o.MaxTokens <= 0 {
		o.MaxTokens = def.MaxTokens
	}
}

type scoredCandidate struct {
	candidate Candidate
	score     float64
}

func sortScored(entries []scoredCandidate) {
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0; j-- {
			left, right := entries[j-1], entries[j]
			if left.score > right.score || (left.score == right.score && left.candidate.To >= right.candidate.To) {
				break
			}
			entries[j-1], entries[j] = right, left
		}
	}
}

// score 统计查询词项在摘要/证据/段标识中的命中加权和。
// 权重：摘要正文 1.0、证据引用 0.5、段标识 0.2。
// 证据与段标识命中必须先于零值守卫统计（摘要无命中时证据仍可命中）。
func (c Candidate) score(terms []string) float64 {
	if len(terms) == 0 {
		return 0
	}
	text := strings.ToLower(c.Summary)
	var total float64
	for _, term := range terms {
		total += float64(strings.Count(text, term))
	}
	for _, evidence := range c.Evidence {
		evidenceText := strings.ToLower(evidence.Ref + " " + evidence.Summary)
		for _, term := range terms {
			if strings.Contains(evidenceText, term) {
				total += 0.5
			}
		}
	}
	if total > 0 && strings.Contains(strings.ToLower(c.SegmentID), terms[0]) {
		total += 0.2
	}
	return total
}

// tokenize 把查询/文本切分为词项：ASCII 词（≥2 字母数字）+ CJK bigram。
// CJK 连续串切 2-gram（单字对超长会话噪音过大，不构成词项）。
func tokenize(text string) []string {
	lower := strings.ToLower(text)
	terms := make([]string, 0, 8)
	runes := []rune(lower)
	index := 0
	for index < len(runes) {
		r := runes[index]
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			start := index
			for index < len(runes) && (unicode.IsLetter(runes[index]) || unicode.IsDigit(runes[index])) {
				index++
			}
			word := string(runes[start:index])
			if isCJK(word) {
				appendBigrams(&terms, word)
			} else if len(word) >= 2 {
				terms = append(terms, word)
			}
		default:
			index++
		}
	}
	return dedupe(terms)
}

// isCJK 判断字串是否全为 CJK 统一表意文字（含扩展区）。
func isCJK(value string) bool {
	for _, r := range value {
		if !unicode.Is(unicode.Han, r) && !unicode.Is(unicode.Hiragana, r) && !unicode.Is(unicode.Katakana, r) {
			return false
		}
	}
	return len(value) > 0
}

// appendBigrams 把 CJK 串切成相邻 2-gram 词项。
func appendBigrams(terms *[]string, word string) {
	runes := []rune(word)
	for i := 0; i+1 < len(runes); i++ {
		*terms = append(*terms, string(runes[i:i+2]))
	}
}

// dedupe 去重并保持首次出现顺序（同一词项不重复计分）。
func dedupe(terms []string) []string {
	seen := make(map[string]struct{}, len(terms))
	result := make([]string, 0, len(terms))
	for _, term := range terms {
		if _, ok := seen[term]; ok {
			continue
		}
		seen[term] = struct{}{}
		result = append(result, term)
	}
	return result
}
