package context_runtime

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// frameMetadataFrom 抽出帧正文里那个 fenced json 块并解析。抽不出/解析不了直接
// 失败——"正文里有花括号"不等于"正文里有一个可解析的元数据块"，而后者才是
// 规范形状的全部意义（能被对拍用例逐字段比对）。
func frameMetadataFrom(t *testing.T, body string) compactionFrameMetadata {
	t.Helper()
	const open = "```json\n"
	start := strings.Index(body, open)
	if start < 0 {
		t.Fatalf("帧正文没有 ```json 元数据块：\n%s", body)
	}
	rest := body[start+len(open):]
	end := strings.Index(rest, "\n```")
	if end < 0 {
		t.Fatalf("帧正文的 json 块没有闭合：\n%s", body)
	}
	var meta compactionFrameMetadata
	if err := json.Unmarshal([]byte(rest[:end]), &meta); err != nil {
		t.Fatalf("元数据块不是合法 JSON（%v）：\n%s", err, rest[:end])
	}
	if strings.Count(body, open) != 1 {
		t.Fatalf("帧正文应当有且仅有一个元数据块，实际 %d 个", strings.Count(body, open))
	}
	return meta
}

// TestCompactionFrameBodyIsJSONMetadataPlusReadingNotes 钉住规范形状：
// 一帧 = JSON 元数据 + Markdown 读后感，元数据**不再**以散文行表达。
//
// 散文行（v1 的 `reason: x · origin: y`、`tokens: compared N → assembled M`）
// 既不能被 json.Unmarshal，也就不能被任何对拍用例逐字段比对；改一个措辞就改掉了
// 一条事实的表达，而读侧无从察觉。把 compactionFrameBody 改回 v1 形状，本用例红。
func TestCompactionFrameBodyIsJSONMetadataPlusReadingNotes(t *testing.T) {
	body := compactionFrameBody(compactionFrameInput{
		Version:         7,
		Reason:          "context_budget",
		Origin:          "explicit_after_turn",
		At:              time.Date(2026, 9, 28, 16, 40, 2, 0, time.UTC),
		ComparedTokens:  129_409,
		AssembledTokens: 20_480,
		SoftThreshold:   118_962,
		HardThreshold:   142_754,
	})
	if !strings.HasPrefix(body, compactionFrameMarker) {
		t.Fatalf("帧正文必须以 v2 标记开头：\n%s", body)
	}
	if strings.Contains(body, compactionFrameMarkerV1) {
		t.Fatalf("v2 正文不得同时带 v1 标记：\n%s", body)
	}
	meta := frameMetadataFrom(t, body)
	if meta.Schema != compactionFrameSchema {
		t.Fatalf("schema = %q，want %q", meta.Schema, compactionFrameSchema)
	}
	// v1 的散文元数据行一律不得再现。
	for _, stale := range []string{
		"reason: context_budget ·",
		"tokens: compared ",
		"injected: no ——",
		"injected: yes ——",
		"folded: 本次没有可记的区间边界",
		"## Context zones",
	} {
		if strings.Contains(body, stale) {
			t.Fatalf("帧正文仍在用 v1 散文元数据行 %q：\n%s", stale, body)
		}
	}
	// 读后感那一半必须在（此处无栈帧摘要 → 本地兜底材料）。
	if !strings.Contains(body, "## 折叠材料 (Folded Material)") {
		t.Fatalf("帧正文缺少 Markdown 一半：\n%s", body)
	}
}

// TestCompactionFrameBodyMetadataCarriesFoldFacts：元数据块里的每个字段都对得上
// 这次折叠的事实，不重算、不推算。injected 尤其不能撒谎——普通折叠走保留窗口
// 路径，并没有把读后感注入 provider 历史，写成 true 等于告诉读者"模型看得到"。
func TestCompactionFrameBodyMetadataCarriesFoldFacts(t *testing.T) {
	layout := ContextLayout{
		Zones: []ContextZone{
			{Kind: ZoneStable, Tokens: 4_200, Messages: 1, Source: "system"},
			{Kind: ZoneFolded, Tokens: 90_000, Messages: 132, Source: "transcript"},
		},
		Retain:          RetainDecision{AllContextTokens: 200_000, BudgetTokens: 174_488, Retained: 91_000},
		ComparedTokens:  129_409,
		EstimatedTokens: 20_480,
		SoftThreshold:   118_962,
		HardThreshold:   142_754,
		Compacting:      true,
	}
	meta := frameMetadataFrom(t, compactionFrameBody(compactionFrameInput{
		Version:       7,
		Reason:        "context_budget",
		Origin:        "auto",
		At:            time.Date(2026, 9, 28, 16, 40, 2, 0, time.UTC),
		SegmentID:     "compact-sess-1",
		SummarySource: "replay",
		Injected:      false,
		Range: compactionFoldedRange{
			MessageFrom: "message-1", MessageTo: "message-103",
			EventFrom: 1, EventTo: 6, Units: 5,
			Label: "消息 message-1..message-103 / 事件 1..6",
		},
		Layout:              layout,
		ReadbackToolResults: []string{"result:call_abc"},
	}))
	if meta.Version != 7 || meta.Reason != "context_budget" || meta.Origin != "auto" {
		t.Fatalf("版本/原因/来源不对：%+v", meta)
	}
	if meta.SegmentID != "compact-sess-1" || meta.SummarySource != "replay" {
		t.Fatalf("段标识/摘要来源不对：%+v", meta)
	}
	if meta.Injected {
		t.Fatalf("保留窗口路径不得报 injected=true：%+v", meta)
	}
	if meta.Folded == nil {
		t.Fatal("有区间边界时 folded 不得为空")
	}
	if meta.Folded.EventFrom != 1 || meta.Folded.EventTo != 6 || meta.Folded.Units != 5 ||
		meta.Folded.MessageTo != "message-103" {
		t.Fatalf("被折区间不是记录值：%+v", meta.Folded)
	}
	// 四区与判据量只嵌 layout 一份，不另立 tokens 区块重复同样的数字。
	if len(meta.Layout.Zones) != 2 || meta.Layout.ComparedTokens != 129_409 ||
		meta.Layout.Retain.Retained != 91_000 {
		t.Fatalf("layout 未原样带入元数据：%+v", meta.Layout)
	}
	if meta.Readback.CompressedTurn != "compact-sess-1" {
		t.Fatalf("细筛入口的 segment_id 不对：%+v", meta.Readback)
	}
	if len(meta.Readback.ToolResults) != 1 || meta.Readback.ToolResults[0] != "result:call_abc" {
		t.Fatalf("细筛入口的工具结果句柄不对：%+v", meta.Readback)
	}
	if !meta.At.Equal(time.Date(2026, 9, 28, 16, 40, 2, 0, time.UTC)) {
		t.Fatalf("折叠时刻不对：%s", meta.At)
	}
}

// TestCompactionFrameBodyOmitsEmptyFoldedRange：没有可记的区间边界时 folded 整个
// 字段缺席（omitempty），而不是留一个全零对象——空区间不是"区间为 0..0"，
// 是"没有边界可记"，两者必须能区分。
func TestCompactionFrameBodyOmitsEmptyFoldedRange(t *testing.T) {
	meta := frameMetadataFrom(t, compactionFrameBody(compactionFrameInput{
		Version: 2, Reason: "context_budget_autonomous", Origin: "auto", Injected: true,
	}))
	if meta.Folded != nil {
		t.Fatalf("空区间不得渲染 folded 对象：%+v", meta.Folded)
	}
	if !meta.Injected {
		t.Fatalf("自主压缩帧正文确实注入了 provider 历史，必须报 injected=true：%+v", meta)
	}
}

// TestCompactionFrameBodyEmbedsSummaryVerbatim：有栈帧摘要时**原样嵌入**，
// 不另写一份也不加外层标题。另写一份必然与栈帧漂移——栈帧是权威，正文只是它的
// 回读投影。摘要自带「上一压缩栈摘要 / 压缩内容」两章节标题，再包一层就成了双重目录。
func TestCompactionFrameBodyEmbedsSummaryVerbatim(t *testing.T) {
	const summary = "## 上一压缩栈摘要 (Previous Compact Stack)\nsegment_id: compact-prev\n\n" +
		"## 压缩内容 (Compacted Context)\n### 目标 (Goal)\n接通压缩帧索引面\n\n" +
		"### 错误与修复 (Errors and Fixes)\nOneLineSummary 会选中 JSON 行 → 加行首守卫"
	body := compactionFrameBody(compactionFrameInput{
		Version: 3, Reason: "context_budget", Origin: "auto",
		Summary:       summary,
		SummarySource: "replay",
	})
	if !strings.Contains(body, summary) {
		t.Fatalf("读后感没有原样嵌入：\n%s", body)
	}
	// 有真摘要时不得退回本地兜底材料，也不得声称"没有模型生成的读后感"。
	for _, unwanted := range []string{"## 折叠材料 (Folded Material)", "本次没有模型生成的读后感"} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("有栈帧摘要时不得出现兜底措辞 %q：\n%s", unwanted, body)
		}
	}
}

// TestCompactionFrameBodyAdmitsMissingEvidence：没有栈帧摘要（开关默认关、重放失败
// 回退本地折叠）时走兜底材料，并**明说这不是对被折原文的总结**。这条是默认路径
// （limits.context_compaction_summary 默认关），所以它不能是空壳，也不能假装是摘要。
func TestCompactionFrameBodyAdmitsMissingEvidence(t *testing.T) {
	withEvidence := compactionFrameBody(compactionFrameInput{
		Version: 2, Reason: "context_budget_autonomous", Origin: "auto", Injected: true,
		Evidence:    "objective: 修复 compact\ncompleted=\"改了记录门槛\"\n",
		PlanMessage: `{"plan_id":"p-1"}`,
	})
	for _, want := range []string{
		"objective: 修复 compact",
		"### 任务证据检查点 (Task Evidence Checkpoint)",
		"### 计划尾部 (Plan Tail，仍保留在 provider 历史里)",
		`{"plan_id":"p-1"}`,
		"不是**对被折原文的总结",
	} {
		if !strings.Contains(withEvidence, want) {
			t.Fatalf("兜底正文缺少 %q：\n%s", want, withEvidence)
		}
	}
	// 既无证据也无 plan：如实说明，不留空段、不编造数字。
	bare := compactionFrameBody(compactionFrameInput{Version: 1, Reason: "context_budget"})
	if !strings.Contains(bare, "没有可回读的任务证据摘要") {
		t.Fatalf("没有证据时不得留空段：\n%s", bare)
	}
	if strings.Contains(bare, "### 计划尾部") {
		t.Fatalf("没有 plan 尾部时不得写出该段：\n%s", bare)
	}
}

// TestCompactionFrameBodyReadbackSaysWhyNoDrillDown：缺 segment_id 时，正文必须
// 说清"为什么没有 read_compressed_turn 这一跳"。装配层折叠此前从不推帧，模型对
// 这段区间根本拿不到细筛入口；"看起来有原文、其实没有入口"比明说没有更糟。
// 推帧失败时还要带上真实原因，不能只说"没有"。
func TestCompactionFrameBodyReadbackSaysWhyNoDrillDown(t *testing.T) {
	disabled := frameMetadataFrom(t, compactionFrameBody(compactionFrameInput{
		Version: 4, Reason: "context_budget", Origin: "auto",
	}))
	if disabled.Readback.CompressedTurn != "" {
		t.Fatalf("没有栈帧时不得凭空给出 segment_id：%+v", disabled.Readback)
	}
	if !strings.Contains(disabled.Readback.Note, "索引面未启用") ||
		!strings.Contains(disabled.Readback.Note, "search_history") {
		t.Fatalf("缺入口时没有如实说明原因与替代路径：%+v", disabled.Readback)
	}

	failed := frameMetadataFrom(t, compactionFrameBody(compactionFrameInput{
		Version: 5, Reason: "context_budget", Origin: "auto",
		IndexError: "compact stack is unavailable",
	}))
	if !strings.Contains(failed.Readback.Note, "compact stack is unavailable") {
		t.Fatalf("推帧失败必须带真实原因：%+v", failed.Readback)
	}
}
