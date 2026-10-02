package search

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/seelexctx/memory"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// orphanHeavyStream 造一条**事件下标与单元下标不同步**的事件流：中间夹一个孤儿
// tool 事件（CompleteEventUnits 会跳过它，不成单元）与一个未知角色事件（同样跳过）。
//
//	事件:  u#1(seq1) u#1(seq2) ORPHAN(seq3) u#2(seq4) UNKNOWN(seq5) u#3(seq6)
//	单元:  [0]={seq1,seq2}     [1]={seq4}              [2]={seq6}
//
// 于是"事件下标 3 → 单元下标"既不是 3 也不是 3−1：孤儿与未知角色各吃掉一个下标，
// 任何减法都会错位。这正是装配层压缩帧只能按 Seq 反查、不能推算的原因。
func orphanHeavyStream() []sessionstore.Event {
	return []sessionstore.Event{
		{Seq: 1, Role: "user", Content: "查一下压缩帧索引面为什么是空的"},
		{Seq: 2, Role: "assistant", Content: "因为装配层压缩不推 CompactStack"},
		{Seq: 3, Role: "tool", Name: "orphan_result", Content: "孤儿工具结果，不构成单元"},
		{Seq: 4, Role: "user", Content: "那 search_history 走的是兜底扫描"},
		{Seq: 5, Role: "mystery", Content: "未知角色，归档证据但不成单元"},
		{Seq: 6, Role: "user", Content: "接通之后 indexed_frames 就不为 0 了"},
	}
}

// TestEventSeqUnitRangeMapsByLookupNotArithmetic：EventSeq → 单元下标必须按 Seq
// 查找，不能按事件下标推算。
//
// 有牙：把 buildHit 里的 EventSeq 分支删掉（回退 clamp From/To），
// TestBuildHitPrefersEventSeqOverUnitIndex 立刻红在读回错误的轮次上。
func TestEventSeqUnitRangeMapsByLookupNotArithmetic(t *testing.T) {
	units := sessionstore.CompleteEventUnits(orphanHeavyStream())
	if len(units) != 3 {
		t.Fatalf("孤儿/未知角色应被跳过，单元数 = %d，want 3", len(units))
	}
	// seq 1..2 → 单元 0；seq 4 → 单元 1；seq 6 → 单元 2。
	for _, testCase := range []struct {
		name             string
		eventFrom        uint64
		eventTo          uint64
		wantFrom, wantTo int
	}{
		{"首单元", 1, 2, 0, 0},
		{"跨到第二单元", 1, 4, 0, 1},
		{"只含中间单元", 4, 4, 1, 1},
		{"跨过孤儿仍连续", 2, 6, 0, 2},
		{"末单元", 6, 6, 2, 2},
	} {
		got, ok := eventSeqUnitRange(units, testCase.eventFrom, testCase.eventTo)
		if !ok {
			t.Fatalf("%s：反查失败（event %d..%d）", testCase.name, testCase.eventFrom, testCase.eventTo)
		}
		if got.from != testCase.wantFrom || got.to != testCase.wantTo {
			t.Fatalf("%s：单元区间 = [%d,%d]，want [%d,%d]",
				testCase.name, got.from, got.to, testCase.wantFrom, testCase.wantTo)
		}
	}
}

// TestEventSeqUnitRangeRefusesToGuess：定不了界就如实说定不了，不 clamp、不猜。
// 空命中比"看起来合法但指向别的轮次"的错读安全得多。
func TestEventSeqUnitRangeRefusesToGuess(t *testing.T) {
	units := sessionstore.CompleteEventUnits(orphanHeavyStream())
	for _, testCase := range []struct {
		name      string
		eventFrom uint64
		eventTo   uint64
	}{
		{"区间整个在事件流之后", 900, 999},
		{"eventTo 为零（未声明）", 1, 0},
	} {
		if got, ok := eventSeqUnitRange(units, testCase.eventFrom, testCase.eventTo); ok {
			t.Fatalf("%s：应当拒绝定位，却返回 [%d,%d]", testCase.name, got.from, got.to)
		}
	}
	if _, ok := eventSeqUnitRange(nil, 1, 2); ok {
		t.Fatal("空事件流不得定位成功")
	}
	// 全零 Seq 的单元（合成事件）无法定界。
	synthetic := [][]sessionstore.Event{{{Seq: 0, Role: "user", Content: "合成事件"}}}
	if _, ok := eventSeqUnitRange(synthetic, 1, 2); ok {
		t.Fatal("只有合成事件的单元不得定位成功")
	}
}

// TestBuildHitPrefersEventSeqOverUnitIndex：帧同时带 From/To 与 EventSeq 时，
// 以 EventSeq 为准。装配层压缩帧的 From/To 是 0（它算不准单元下标），若仍按
// From/To clamp，就会读到事件流最开头的单元——那是**别的轮次**，而且不报错。
func TestBuildHitPrefersEventSeqOverUnitIndex(t *testing.T) {
	units := sessionstore.CompleteEventUnits(orphanHeavyStream())
	candidate := memory.Candidate{
		SegmentID: "compact-sess-1",
		Summary:   "接通压缩帧索引面",
		From:      0, To: 0, // 装配层压缩帧：单元下标未声明
		EventFrom: 6, EventTo: 6,
	}
	hit := buildHit("索引面", candidate, units, 4_000)
	if hit.From != 2 || hit.To != 2 || hit.Units != 1 {
		t.Fatalf("应按 EventSeq 定位到末单元，got from=%d to=%d units=%d", hit.From, hit.To, hit.Units)
	}
	if len(hit.Records) != 1 || !strings.Contains(hit.Records[0].Content, "indexed_frames") {
		t.Fatalf("读回的不是 seq6 那一轮：%+v", hit.Records)
	}
}

// TestBuildHitReportsUnmappableRangeInsteadOfClamping：定不了界时在 Summary 里
// 说清原因并返回空记录，绝不 clamp 出一个假区间。
func TestBuildHitReportsUnmappableRangeInsteadOfClamping(t *testing.T) {
	units := sessionstore.CompleteEventUnits(orphanHeavyStream())
	hit := buildHit("索引面", memory.Candidate{
		SegmentID: "compact-stale", Summary: "旧帧", EventFrom: 900, EventTo: 999,
	}, units, 4_000)
	if len(hit.Records) != 0 || hit.Units != 0 {
		t.Fatalf("定不了界时不得读回任何记录：%+v", hit)
	}
	if !strings.Contains(hit.Summary, "区间无法定位") || !strings.Contains(hit.Summary, "900..999") {
		t.Fatalf("定不了界必须说明原因与区间：%q", hit.Summary)
	}
}

// TestBuildHitKeepsUnitIndexFallbackForLegacyFrames：未声明 EventSeq 的帧
// （控制器帧 / 真空区帧 / 旧记录）仍走 From/To，行为逐位不变。
func TestBuildHitKeepsUnitIndexFallbackForLegacyFrames(t *testing.T) {
	units := sessionstore.CompleteEventUnits(orphanHeavyStream())
	hit := buildHit("兜底", memory.Candidate{
		SegmentID: "compact-gap-1", Summary: "真空区帧", From: 0, To: 1,
	}, units, 4_000)
	if hit.From != 0 || hit.To != 1 || hit.Units != 2 {
		t.Fatalf("旧帧应沿用 From/To，got from=%d to=%d units=%d", hit.From, hit.To, hit.Units)
	}
	if strings.Contains(hit.Summary, "区间无法定位") {
		t.Fatalf("走回退分支不得报定位失败：%q", hit.Summary)
	}
}
