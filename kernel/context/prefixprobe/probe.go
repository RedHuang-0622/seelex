// Package prefixprobe 计量相邻两次 provider 请求之间的字节前缀命中情况。
//
// 设计口径见 docs/arch/context-prefix-stability-l1.md §7。三条要点：
//
//  1. **采样面是真正要发出去的字节**，不是重新序列化的结果——否则测到的是
//     "我以为的请求"，那正是 I1（字节冻结）要防的自欺。
//  2. **判据是前缀关系而不是比率**：append-only 的健康轮次里 LCP 小于请求总长
//     （尾部有新增），所以 ratio < 1 是正常的；失效的判据是
//     `cur.Wire` 不再以 `prev.Wire` 为前缀。
//  3. **与 prefix_key 对撞**：键变了 → 失效是预期的（ExpectedMiss）；键没变却
//     断了 → 缺陷（UnexpectedMiss），并归因到具体段。
package prefixprobe

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Segment 是请求字节流里的一段命名区间（左闭右开：[Offset, Offset+Len)）。
// 段名建议与装配顺序一致：tools / system / project / memory / compact /
// truth / sentinel / plan / env / worktable / draft / input。
type Segment struct {
	Name   string
	Offset int
	Len    int
}

// End 返回段尾的字节偏移。
func (s Segment) End() int { return s.Offset + s.Len }

// Key 是前缀复用键（arch 文档 §4）。零值表示该格未参与判定。
//
// 全部字段可比较，因此 Key 本身可直接用 == / != 判断是否同一前缀身份。
type Key struct {
	SystemDigest   string
	ProjectVersion string
	MemoryEpoch    uint64
	CompactSeq     uint64
	ToolsEpoch     uint64
	ToolsDigest    string
	RetainedFrom   int
}

// MessageRef 标记一条消息在字节流里的起点，用于把分叉归因到 role。
type MessageRef struct {
	Index int
	Role  string
	Start int
}

// Sample 是一次「即将发出的请求」快照。
type Sample struct {
	Wire     string
	Segments []Segment
	Key      Key
	Messages []MessageRef
}

// Counters 是可叠加的计量（arch 文档 §7.3）。
type Counters struct {
	Requests       int
	TotalChars     int64
	HitChars       int64
	MissCharsBySeg map[string]int64
	ExpectedMiss   int
	UnexpectedMiss int
}

// Ratio 返回累计命中比（hit / total）；无样本时为 0。
func (c Counters) Ratio() float64 {
	if c.TotalChars == 0 {
		return 0
	}
	return float64(c.HitChars) / float64(c.TotalChars)
}

// Clone 深拷一份（map 需要独立）。
func (c Counters) Clone() Counters {
	out := c
	if c.MissCharsBySeg != nil {
		out.MissCharsBySeg = make(map[string]int64, len(c.MissCharsBySeg))
		for k, v := range c.MissCharsBySeg {
			out.MissCharsBySeg[k] = v
		}
	}
	return out
}

func (c *Counters) addMiss(segment string, chars int64) {
	if chars <= 0 {
		return
	}
	if c.MissCharsBySeg == nil {
		c.MissCharsBySeg = map[string]int64{}
	}
	c.MissCharsBySeg[segment] += chars
}

// Divergence 是首个分叉点（arch 文档 §7.4）。
type Divergence struct {
	ByteOffset   int
	CharOffset   int
	Segment      string
	MessageIndex int
	Role         string
}

func (d Divergence) String() string {
	return fmt.Sprintf("byte=%d char=%d segment=%s msg#%d role=%s",
		d.ByteOffset, d.CharOffset, d.Segment, d.MessageIndex, d.Role)
}

// Result 是一次观测的结论。
type Result struct {
	Turn       uint64
	HitChars   int
	TotalChars int
	NewChars   int
	Ratio      float64

	// Divergence 为 nil 表示"上一条请求是本条的前缀"（完全复用）；首次观测恒为 nil。
	Divergence *Divergence
	// KeyChanged 表示两次请求的前缀身份不同。
	KeyChanged bool
	// Changed 列出变化了的键格名（便于打点归因）。
	Changed []string
	// Expected / Unexpected 区分"键变了导致的失效"与"键没变却断了的缺陷"。
	Expected   bool
	Unexpected bool
	// First 表示这是该 Probe 的第一次观测（无参照，不判定失效）。
	First bool
}

// Intact 报告本条请求是否完整复用了上一条请求的字节。
func (r Result) Intact() bool { return r.Divergence == nil }

// Probe 维护上一条样本与三套计数器（cum / perTurn / last）。
//
// 零值不可用，须经 New 构造。Probe 非并发安全：一次会话的请求是串行的，
// 跨会话各自持有一个实例。
type Probe struct {
	prev  *Sample
	cum   Counters
	last  Counters
	turns map[uint64]*Counters
}

// New 构造一个空探针。
func New() *Probe {
	return &Probe{turns: map[uint64]*Counters{}}
}

// Observe 记录一次请求快照并返回本次观测结论。
//
// 调用点：draft 每次追加之后、请求发出之前（arch 文档 §7.2）。
func (p *Probe) Observe(turn uint64, cur Sample) Result {
	res := Result{Turn: turn, TotalChars: CountChars(cur.Wire)}
	if res.TotalChars > 0 {
		res.Ratio = 1
	}
	if p.prev == nil {
		res.First = true
		res.HitChars = res.TotalChars
		p.record(res, cur, len(cur.Wire))
		p.prev = cloneSample(cur)
		return res
	}

	prev := p.prev
	hitBytes := BackoffToRuneBoundary(cur.Wire, LCPBytes(prev.Wire, cur.Wire))
	res.HitChars = CountChars(cur.Wire[:hitBytes])
	res.NewChars = res.TotalChars - res.HitChars
	if res.TotalChars > 0 {
		res.Ratio = float64(res.HitChars) / float64(res.TotalChars)
	}

	res.KeyChanged = prev.Key != cur.Key
	if res.KeyChanged {
		res.Changed = DiffKeys(prev.Key, cur.Key)
	}

	// 判据是前缀关系：上一条请求的全部字节必须被本条请求完整复现。
	if hitBytes < len(prev.Wire) {
		res.Divergence = &Divergence{
			ByteOffset:   hitBytes,
			CharOffset:   res.HitChars,
			Segment:      SegmentAt(cur.Segments, hitBytes),
			MessageIndex: MessageAt(cur.Messages, hitBytes),
			Role:         MessageRoleAt(cur.Messages, hitBytes),
		}
		res.Expected = res.KeyChanged
		res.Unexpected = !res.KeyChanged
	}

	p.record(res, cur, hitBytes)
	p.prev = cloneSample(cur)
	return res
}

// Last 返回最近一次观测的计数。
func (p *Probe) Last() Counters { return p.last.Clone() }

// Cumulative 返回会话累计计数。
func (p *Probe) Cumulative() Counters { return p.cum.Clone() }

// Turn 返回某一轮的累计计数（轮不存在时返回零值）。
func (p *Probe) Turn(turn uint64) Counters {
	if c, ok := p.turns[turn]; ok && c != nil {
		return c.Clone()
	}
	return Counters{}
}

// Reset 丢弃参照样本与全部计数（用于切会话或压缩后重置参照）。
func (p *Probe) Reset() {
	p.prev = nil
	p.cum = Counters{}
	p.last = Counters{}
	p.turns = map[uint64]*Counters{}
}

func (p *Probe) record(res Result, cur Sample, hitBytes int) {
	p.last = Counters{
		Requests:       1,
		TotalChars:     int64(res.TotalChars),
		HitChars:       int64(res.HitChars),
		ExpectedMiss:   boolToInt(res.Expected),
		UnexpectedMiss: boolToInt(res.Unexpected),
	}
	// 未被复用的字符按段归因：从分叉点起直到本条请求结尾。
	assignMiss(&p.last, cur, hitBytes)

	p.cum.Requests++
	p.cum.TotalChars += int64(res.TotalChars)
	p.cum.HitChars += int64(res.HitChars)
	p.cum.ExpectedMiss += boolToInt(res.Expected)
	p.cum.UnexpectedMiss += boolToInt(res.Unexpected)
	accumulateMiss(&p.cum, p.last.MissCharsBySeg)

	if p.turns == nil {
		p.turns = map[uint64]*Counters{}
	}
	counter, ok := p.turns[res.Turn]
	if !ok || counter == nil {
		counter = &Counters{}
		p.turns[res.Turn] = counter
	}
	counter.Requests++
	counter.TotalChars += int64(res.TotalChars)
	counter.HitChars += int64(res.HitChars)
	counter.ExpectedMiss += boolToInt(res.Expected)
	counter.UnexpectedMiss += boolToInt(res.Unexpected)
	accumulateMiss(counter, p.last.MissCharsBySeg)
}

// assignMiss 把 [hitBytes, len(cur.Wire)) 这段未命中字节按段名累加进 c。
func assignMiss(c *Counters, cur Sample, hitBytes int) {
	if hitBytes >= len(cur.Wire) {
		return
	}
	if len(cur.Segments) == 0 {
		c.addMiss("unsegmented", int64(CountChars(cur.Wire[hitBytes:])))
		return
	}
	for _, seg := range cur.Segments {
		start := seg.Offset
		if start < hitBytes {
			start = hitBytes
		}
		end := seg.End()
		if end > len(cur.Wire) {
			end = len(cur.Wire)
		}
		if end <= start {
			continue
		}
		c.addMiss(seg.Name, int64(CountChars(cur.Wire[start:end])))
	}
}

func accumulateMiss(dst *Counters, src map[string]int64) {
	for name, chars := range src {
		dst.addMiss(name, chars)
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func cloneSample(s Sample) *Sample {
	out := s
	if s.Segments != nil {
		out.Segments = append([]Segment(nil), s.Segments...)
	}
	if s.Messages != nil {
		out.Messages = append([]MessageRef(nil), s.Messages...)
	}
	return &out
}

// LCPBytes 返回两个字节串的最长公共前缀长度。
func LCPBytes(a, b string) int {
	limit := len(a)
	if len(b) < limit {
		limit = len(b)
	}
	for i := 0; i < limit; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return limit
}

// BackoffToRuneBoundary 把 n 回退到最近的位置 k（k ≤ n），使 s[:k] 是合法 UTF-8。
//
// 这不是防御性代码，而是**必要**的：合法 UTF-8 下字节 LCP 也可能停在 rune
// 中间。同一 Unicode 区的码点共享前导字节，例如 U+1F642 🙂 = F0 9F 99 82 与
// U+1F643 🙃 = F0 9F 99 83 共享前 3 字节，LCP 落在第 4 字节（rune 内部）；
// 中文同区码点（U+4E2D 中 = E4 B8 AD 与 U+4E2E 丮 = E4 B8 AE）同理。
// 若不回退，utf8.RuneCountInString 会把残字节数成 RuneError，字符数虚高。
func BackoffToRuneBoundary(s string, n int) int {
	if n <= 0 {
		return 0
	}
	if n > len(s) {
		n = len(s)
	}
	last := 0
	for i := 0; i < n; {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			break // 非法字节：停在上一个合法边界
		}
		if i+size > n {
			break // 该 rune 跨过 n：停在它之前
		}
		i += size
		last = i
	}
	return last
}

// CountChars 按 Unicode 字符（rune）计数；非法字节按 RuneError 各计一个。
func CountChars(s string) int { return utf8.RuneCountInString(s) }

// SegmentAt 返回包含该字节偏移的段名；越界或未分段时返回 "unsegmented"。
func SegmentAt(segments []Segment, offset int) string {
	if offset < 0 {
		return "unsegmented"
	}
	for _, seg := range segments {
		if offset >= seg.Offset && offset < seg.End() {
			return seg.Name
		}
	}
	if len(segments) == 0 {
		return "unsegmented"
	}
	// 落在流尾（所有段之后）时归到最后一个"未闭合"的概念上：仍标 unsegmented，
	// 让调用方知道分叉发生在已命名区间之外。
	return "unsegmented"
}

// MessageAt 返回包含该字节偏移的消息下标；无消息边界信息时返回 -1。
func MessageAt(messages []MessageRef, offset int) int {
	index := -1
	for _, ref := range messages {
		if ref.Start <= offset {
			index = ref.Index
			continue
		}
		break
	}
	return index
}

// MessageRoleAt 返回包含该字节偏移的消息 role；未知时返回空串。
func MessageRoleAt(messages []MessageRef, offset int) string {
	role := ""
	for _, ref := range messages {
		if ref.Start <= offset {
			role = ref.Role
			continue
		}
		break
	}
	return role
}

// DiffKeys 返回两把键之间变化的格名（按 arch 文档 §4 的六格顺序）。
func DiffKeys(prev, cur Key) []string {
	var changed []string
	if prev.SystemDigest != cur.SystemDigest {
		changed = append(changed, "system_digest")
	}
	if prev.ProjectVersion != cur.ProjectVersion {
		changed = append(changed, "project_version")
	}
	if prev.MemoryEpoch != cur.MemoryEpoch {
		changed = append(changed, "memory_epoch")
	}
	if prev.CompactSeq != cur.CompactSeq {
		changed = append(changed, "compact_seq")
	}
	if prev.ToolsEpoch != cur.ToolsEpoch || prev.ToolsDigest != cur.ToolsDigest {
		changed = append(changed, "tools")
	}
	if prev.RetainedFrom != cur.RetainedFrom {
		changed = append(changed, "retained_from")
	}
	return changed
}

// Summary 生成一行人读摘要，供 t.Logf / 打点使用。
func (r Result) Summary() string {
	if r.First {
		return fmt.Sprintf("turn=%d FIRST chars=%d（无参照，不判定）", r.Turn, r.TotalChars)
	}
	state := "prefix OK"
	if !r.Intact() {
		state = "prefix BREAK " + r.Divergence.String()
		if r.Expected {
			state += " [expected: key=" + strings.Join(r.Changed, ",") + "]"
		} else {
			state += " [UNEXPECTED]"
		}
	}
	return fmt.Sprintf("turn=%d %s hit=%d/%d (%.1f%%) new=%d",
		r.Turn, state, r.HitChars, r.TotalChars, r.Ratio*100, r.NewChars)
}
