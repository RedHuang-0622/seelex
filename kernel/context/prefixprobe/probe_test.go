package prefixprobe

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLCPBytes(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want int
	}{
		{name: "完全相同", a: "abc", b: "abc", want: 3},
		{name: "前缀扩展", a: "abc", b: "abcdef", want: 3},
		{name: "首字节即分叉", a: "abc", b: "xyz", want: 0},
		{name: "中段分叉", a: "abcdef", b: "abcXef", want: 3},
		{name: "空串", a: "", b: "abc", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LCPBytes(tt.a, tt.b); got != tt.want {
				t.Fatalf("LCPBytes(%q,%q)=%d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// TestLCPBytes_MidRuneIsReachable 记录一条与直觉相反的事实：**合法 UTF-8 下
// 字节 LCP 也可能停在 rune 中间**。同一 Unicode 区的码点共享前导字节，例如
// U+1F642 🙂 = F0 9F 99 82 与 U+1F643 🙃 = F0 9F 99 83 共享前 3 字节，LCP 落在
// 第 4 字节（rune 内部）。这正是 BackoffToRuneBoundary 存在的理由——不是防御性
// 代码，而是必要环节；否则字符数会因残字节被数成 RuneError 而虚高。
func TestLCPBytes_MidRuneIsReachable(t *testing.T) {
	a, b := "emoji🙂tail", "emoji🙃tail"
	common := LCPBytes(a, b)
	if common != 8 {
		t.Fatalf("LCP=%d, want 8（5 字节前缀 + 3 字节共享的表情前导）", common)
	}
	if utf8.RuneStart(a[common]) {
		t.Fatalf("该样本应当落在 rune 中间，实际落在边界上")
	}
	// 不回退直接数：5 个 ASCII + 3 个残字节各被数成 RuneError ⇒ 8，虚高。
	if got := CountChars(a[:common]); got != 8 {
		t.Fatalf("未回退时字符数=%d, want 8（残字节各计 RuneError）", got)
	}
	backed := BackoffToRuneBoundary(a, common)
	if backed != 5 {
		t.Fatalf("回退后=%d, want 5（\"emoji\" 的合法边界）", backed)
	}
	if got := CountChars(a[:backed]); got != 5 {
		t.Fatalf("回退后字符数=%d, want 5", got)
	}
	// 中文同区码点同理。
	c, d := "中", "丮"
	if common := LCPBytes(c, d); common != 2 {
		t.Fatalf("中文同区码点 LCP=%d, want 2", common)
	}
	if got := BackoffToRuneBoundary(c, 2); got != 0 {
		t.Fatalf("中文回退后=%d, want 0", got)
	}
}

func TestBackoffToRuneBoundary(t *testing.T) {
	// "中" 为 3 字节；把下标刻意停在它中间。
	s := "中a"
	tests := []struct {
		name string
		n    int
		want int
	}{
		{name: "零", n: 0, want: 0},
		{name: "越界按流尾", n: 99, want: len(s)},
		{name: "落在 rune 起点不动", n: 3, want: 3},
		{name: "落在 rune 中间回退到起点", n: 1, want: 0},
		{name: "落在 rune 中间回退（第二字节）", n: 2, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BackoffToRuneBoundary(s, tt.n); got != tt.want {
				t.Fatalf("BackoffToRuneBoundary(%q,%d)=%d, want %d", s, tt.n, got, tt.want)
			}
		})
	}
	// 非法字节序列：截断的多字节字符必须被回退掉，避免 RuneError 虚增字符数。
	broken := "中"[:2]
	if got := BackoffToRuneBoundary(broken, len(broken)); got != 0 {
		t.Fatalf("截断字符未回退：got=%d, want 0", got)
	}
}

func TestCountChars(t *testing.T) {
	if got := CountChars("abc"); got != 3 {
		t.Fatalf("ascii=%d, want 3", got)
	}
	if got := CountChars("中文"); got != 2 {
		t.Fatalf("cjk=%d, want 2（按字符不是字节）", got)
	}
	if got := CountChars("中a"); got != 2 {
		t.Fatalf("mixed=%d, want 2", got)
	}
}

func TestProbe_FirstSampleIsNeutral(t *testing.T) {
	probe := New()
	res := probe.Observe(1, Sample{Wire: "system+draft", Key: Key{SystemDigest: "s1"}})
	if !res.First {
		t.Fatalf("首个样本应标记 First")
	}
	if !res.Intact() || res.Unexpected {
		t.Fatalf("首个样本不得判定失效: %+v", res)
	}
	if res.Ratio != 1 {
		t.Fatalf("首个样本 Ratio=%v, want 1（无参照）", res.Ratio)
	}
}

// TestProbe_AppendOnlyKeepsPrefix 是健康轮次的基线：只追加时前缀完整复用，
// 但 ratio 必然小于 1（尾部有新增）——这正是"ratio<1 不等于失效"的守门用例。
func TestProbe_AppendOnlyKeepsPrefix(t *testing.T) {
	probe := New()
	key := Key{SystemDigest: "s1", ToolsDigest: "t1"}
	probe.Observe(1, Sample{Wire: "A", Key: key})
	res := probe.Observe(1, Sample{Wire: "A-B", Key: key})
	if !res.Intact() {
		t.Fatalf("只追加应保持前缀: %s", res.Summary())
	}
	if res.Ratio >= 1 {
		t.Fatalf("有新增时 ratio 必须 < 1，got %v", res.Ratio)
	}
	if res.HitChars != 1 || res.TotalChars != 3 || res.NewChars != 2 {
		t.Fatalf("计量错误: hit=%d total=%d new=%d", res.HitChars, res.TotalChars, res.NewChars)
	}
	if res.Unexpected || res.Expected {
		t.Fatalf("只追加不得记失效: %+v", res)
	}
}

// TestProbe_UnexpectedMissWhenKeyUnchanged 是本探针的核心判据：键没变却断开
// ⇒ 缺陷（Unexpected），并归因到段。
func TestProbe_UnexpectedMissWhenKeyUnchanged(t *testing.T) {
	probe := New()
	key := Key{SystemDigest: "s1", ToolsDigest: "t1"}
	prev := Sample{
		Wire: "SYSTEM" + "TRUTH-1",
		Segments: []Segment{
			{Name: "system", Offset: 0, Len: 6},
			{Name: "truth", Offset: 6, Len: 7},
		},
		Key: key,
	}
	probe.Observe(1, prev)
	// 同一位置被改写：TRUTH-1 变成 TRUTH-X（键没变）。
	cur := Sample{
		Wire: "SYSTEM" + "TRUTH-X",
		Segments: []Segment{
			{Name: "system", Offset: 0, Len: 6},
			{Name: "truth", Offset: 6, Len: 7},
		},
		Key: key,
	}
	res := probe.Observe(2, cur)
	if res.Intact() {
		t.Fatalf("应当判定前缀断裂")
	}
	if !res.Unexpected {
		t.Fatalf("键未变却断开必须记为 Unexpected: %+v", res)
	}
	if res.Expected {
		t.Fatalf("不得同时记为 Expected: %+v", res)
	}
	if res.Divergence == nil {
		t.Fatalf("必须给出分叉点")
	}
	if res.Divergence.ByteOffset != 12 {
		t.Fatalf("分叉字节偏移=%d, want 12（\"SYSTEM\"+\"TRUTH-\"）", res.Divergence.ByteOffset)
	}
	if res.Divergence.Segment != "truth" {
		t.Fatalf("分叉段=%q, want truth", res.Divergence.Segment)
	}
	if got := probe.Cumulative().UnexpectedMiss; got != 1 {
		t.Fatalf("累计 UnexpectedMiss=%d, want 1", got)
	}
	// 未命中字符归因到 truth 段（"X" 1 个字符）。
	if got := probe.Last().MissCharsBySeg["truth"]; got != 1 {
		t.Fatalf("truth 段未命中字符=%d, want 1", got)
	}
}

// TestProbe_ExpectedMissWhenKeyChanged 守住另一半：键变了导致的失效是预期的，
// 但同样要能归因到哪一格键变了。
func TestProbe_ExpectedMissWhenKeyChanged(t *testing.T) {
	probe := New()
	probe.Observe(1, Sample{Wire: "SYSTEM-MEMORY-A", Key: Key{SystemDigest: "s1", MemoryEpoch: 1}})
	res := probe.Observe(2, Sample{Wire: "SYSTEM-MEMORY-B", Key: Key{SystemDigest: "s1", MemoryEpoch: 2}})
	if res.Intact() {
		t.Fatalf("memory_epoch 变了必须判定失效")
	}
	if !res.Expected {
		t.Fatalf("键变了应记为 Expected: %+v", res)
	}
	if res.Unexpected {
		t.Fatalf("键变了不得记为 Unexpected: %+v", res)
	}
	if len(res.Changed) != 1 || res.Changed[0] != "memory_epoch" {
		t.Fatalf("Changed=%v, want [memory_epoch]", res.Changed)
	}
	if got := probe.Cumulative().ExpectedMiss; got != 1 {
		t.Fatalf("累计 ExpectedMiss=%d, want 1", got)
	}
	if got := probe.Cumulative().UnexpectedMiss; got != 0 {
		t.Fatalf("累计 UnexpectedMiss=%d, want 0", got)
	}
}

func TestDiffKeys(t *testing.T) {
	base := Key{SystemDigest: "s", ProjectVersion: "p", MemoryEpoch: 1, CompactSeq: 2, ToolsEpoch: 3, ToolsDigest: "td", RetainedFrom: 4}
	if got := DiffKeys(base, base); len(got) != 0 {
		t.Fatalf("相同键应无变化: %v", got)
	}
	changed := base
	changed.ToolsEpoch = 9
	if got := DiffKeys(base, changed); len(got) != 1 || got[0] != "tools" {
		t.Fatalf("DiffKeys=%v, want [tools]", got)
	}
	changed = base
	changed.RetainedFrom = 40
	changed.CompactSeq = 20
	if got := DiffKeys(base, changed); len(got) != 2 || got[0] != "compact_seq" || got[1] != "retained_from" {
		t.Fatalf("DiffKeys=%v, want [compact_seq retained_from]", got)
	}
}

// TestProbe_ThreeRatiosAccumulate 守住三套口径同时可用：last（最近一次）、
// turn（逐轮）、cum（会话累计）各自独立且可复现。
func TestProbe_ThreeRatiosAccumulate(t *testing.T) {
	probe := New()
	key := Key{SystemDigest: "s1"}
	probe.Observe(1, Sample{Wire: "AAAA", Key: key})       // 4 字符，首次
	probe.Observe(1, Sample{Wire: "AAAABBBB", Key: key})   // hit 4 / total 8
	probe.Observe(2, Sample{Wire: "AAAABBBBCC", Key: key}) // hit 8 / total 10
	last := probe.Last()
	if last.TotalChars != 10 || last.HitChars != 8 {
		t.Fatalf("last=%+v, want hit=8 total=10", last)
	}
	turnOne := probe.Turn(1)
	if turnOne.Requests != 2 || turnOne.HitChars != 8 || turnOne.TotalChars != 12 {
		t.Fatalf("turn1=%+v, want requests=2 hit=8 total=12", turnOne)
	}
	cum := probe.Cumulative()
	if cum.Requests != 3 || cum.HitChars != 16 || cum.TotalChars != 22 {
		t.Fatalf("cum=%+v, want requests=3 hit=16 total=22", cum)
	}
	if got, want := cum.Ratio(), 16.0/22.0; got < want-1e-9 || got > want+1e-9 {
		t.Fatalf("cum.Ratio()=%v, want %v", got, want)
	}
	// cum 的未命中字符 = 新增字符总数（4 + 2 = 6）。
	var miss int64
	for _, chars := range cum.MissCharsBySeg {
		miss += chars
	}
	if miss != 6 {
		t.Fatalf("累计未命中字符=%d, want 6", miss)
	}
}

func TestSegmentAtAndMessages(t *testing.T) {
	segments := []Segment{
		{Name: "system", Offset: 0, Len: 3},
		{Name: "truth", Offset: 3, Len: 4},
	}
	tests := []struct {
		offset int
		want   string
	}{
		{offset: 0, want: "system"},
		{offset: 2, want: "system"},
		{offset: 3, want: "truth"},
		{offset: 6, want: "truth"},
		{offset: 7, want: "unsegmented"}, // 流尾
	}
	for _, tt := range tests {
		if got := SegmentAt(segments, tt.offset); got != tt.want {
			t.Fatalf("SegmentAt(%d)=%q, want %q", tt.offset, got, tt.want)
		}
	}
	if got := SegmentAt(nil, 1); got != "unsegmented" {
		t.Fatalf("无段表时=%q, want unsegmented", got)
	}

	messages := []MessageRef{
		{Index: 0, Role: "system", Start: 0},
		{Index: 1, Role: "user", Start: 3},
	}
	if got := MessageAt(messages, 4); got != 1 {
		t.Fatalf("MessageAt(4)=%d, want 1", got)
	}
	if got := MessageRoleAt(messages, 4); got != "user" {
		t.Fatalf("MessageRoleAt(4)=%q, want user", got)
	}
	if got := MessageAt(nil, 4); got != -1 {
		t.Fatalf("无消息边界时 MessageAt=%d, want -1", got)
	}
}

// TestProbe_ResetDropsBaseline 守住切会话时不会拿旧前缀当参照。
func TestProbe_ResetDropsBaseline(t *testing.T) {
	probe := New()
	probe.Observe(1, Sample{Wire: "old"})
	probe.Reset()
	res := probe.Observe(1, Sample{Wire: "new"})
	if !res.First {
		t.Fatalf("Reset 后首个样本应重新标记 First")
	}
	if !res.Intact() {
		t.Fatalf("Reset 后不得与旧样本比较: %s", res.Summary())
	}
	if got := probe.Cumulative().Requests; got != 1 {
		t.Fatalf("Reset 后累计请求数=%d, want 1", got)
	}
}

// TestResult_Summary 只做一次可读性快照，防止打点格式悄悄漂移。
func TestResult_Summary(t *testing.T) {
	probe := New()
	key := Key{SystemDigest: "s1"}
	probe.Observe(1, Sample{Wire: "ABCD", Key: key})
	res := probe.Observe(1, Sample{Wire: "ABXD", Key: key})
	got := res.Summary()
	for _, want := range []string{"BREAK", "UNEXPECTED", "hit=2/4"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Summary=%q 缺少 %q", got, want)
		}
	}
}
