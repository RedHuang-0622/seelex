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
		"compacted: 本次没有可记的区间边界",
		"## Context zones",
	} {
		if strings.Contains(body, stale) {
			t.Fatalf("帧正文仍在用 v1 散文元数据行 %q：\n%s", stale, body)
		}
	}
	// 读后感那一半必须在（此处无栈帧摘要 → 本地兜底材料）。
	if !strings.Contains(body, "## 压缩材料 (Folded Material)") {
		t.Fatalf("帧正文缺少 Markdown 一半：\n%s", body)
	}
}

// TestCompactionFrameBodyMetadataCarriesCompactionFacts：元数据块里的每个字段都对得上
// 这次压缩的事实，不重算、不推算。injected 尤其不能撒谎——普通压缩走保留窗口
// 路径，并没有把读后感注入 provider 历史，写成 true 等于告诉读者"模型看得到"。
func TestCompactionFrameBodyMetadataCarriesCompactionFacts(t *testing.T) {
	layout := ContextLayout{
		Zones: []ContextZone{
			{Kind: ZoneStable, Tokens: 4_200, Messages: 1, Source: "system"},
			{Kind: ZoneCompacted, Tokens: 90_000, Messages: 132, Source: "transcript"},
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
		Range: compactionCompactedRange{
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
	if meta.Compacted == nil {
		t.Fatal("有区间边界时 compacted 不得为空")
	}
	if meta.Compacted.EventFrom != 1 || meta.Compacted.EventTo != 6 || meta.Compacted.Units != 5 ||
		meta.Compacted.MessageTo != "message-103" {
		t.Fatalf("被折区间不是记录值：%+v", meta.Compacted)
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
		t.Fatalf("压缩时刻不对：%s", meta.At)
	}
}

// TestCompactionFrameBodyOmitsEmptyRange：没有可记的区间边界时 compacted 整个
// 字段缺席（omitempty），而不是留一个全零对象——空区间不是"区间为 0..0"，
// 是"没有边界可记"，两者必须能区分。
func TestCompactionFrameBodyOmitsEmptyRange(t *testing.T) {
	meta := frameMetadataFrom(t, compactionFrameBody(compactionFrameInput{
		Version: 2, Reason: "context_budget_autonomous", Origin: "auto", Injected: true,
	}))
	if meta.Compacted != nil {
		t.Fatalf("空区间不得渲染 compacted 对象：%+v", meta.Compacted)
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
	for _, unwanted := range []string{"## 压缩材料 (Folded Material)", "本次没有模型生成的读后感"} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("有栈帧摘要时不得出现兜底措辞 %q：\n%s", unwanted, body)
		}
	}
}

// TestCompactionFrameBodyAdmitsMissingEvidence：没有栈帧摘要（开关默认关、重放失败
// 回退本地压缩）时走兜底材料，并**明说这不是对被折原文的总结**。这条是默认路径
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
// 说清"为什么没有 read_compressed_turn 这一跳"。装配层压缩此前从不推帧，模型对
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

	// 索引面已就绪、只是这次压缩没有折出任何完整协议单元（区间为空）：必须说这
	// 一种，不能顺手说成"索引面未启用"——那是把"这次没事可做"报成"这一跳没接"，
	// 读帧的人会去查一个并不存在的配置事故。
	skipped := frameMetadataFrom(t, compactionFrameBody(compactionFrameInput{
		Version: 6, Reason: "context_budget", Origin: "manual",
		IndexSkipped: true,
	}))
	if !strings.Contains(skipped.Readback.Note, "没有折出任何完整协议单元") {
		t.Fatalf("索引面在但没有区间可推时必须说清这一种：%+v", skipped.Readback)
	}
	if strings.Contains(skipped.Readback.Note, "索引面未启用") {
		t.Fatalf("索引面已就绪却说成未启用：%+v", skipped.Readback)
	}
}

// TestCompactionFrameBodyWritesWhyNoModelSummary：落到本地压缩时，正文必须写出
// **为什么没有模型摘要**（开关关闭 / 无重放素材 / 重放调用失败及其真实报错）。
//
// 此前这里只有一句"开关未开启，或前缀重放失败"的 or 措辞，读帧的人分不清是哪一种
// ——而两件事的处置完全不同（一个是配置，一个是故障）。一次现场：新进程确认读到
// enabled: true，折出的帧仍是 local，回执只有一个 `index 458ms`，读不出"没调用"
// 还是"调用失败"。
func TestCompactionFrameBodyWritesWhyNoModelSummary(t *testing.T) {
	const failure = "account lease refused: rate limited"
	const note = "前缀重放两次调用均失败，已回退本地压缩：" + failure
	input := compactionFrameInput{
		Version: 7, Reason: "context_budget", Origin: "explicit_after_turn",
		SummarySource: "local", SummaryNote: note,
	}

	meta := frameMetadataFrom(t, compactionFrameBody(input))
	if meta.SummarySource != "local" {
		t.Fatalf("summary_source = %q，want local", meta.SummarySource)
	}
	if !strings.Contains(meta.SummaryNote, failure) {
		t.Fatalf("元数据块应写出降级原因（含真实报错），实际 %q", meta.SummaryNote)
	}

	body := compactionFrameBody(input)
	for _, want := range []string{"前缀重放两次调用均失败", failure} {
		if !strings.Contains(body, want) {
			t.Fatalf("压缩材料一段应写出 %q：\n%s", want, body)
		}
	}

	// 没有原因（更早版本写的帧、推帧失败）：保留兜底措辞，不编一个原因。
	plain := compactionFrameBody(compactionFrameInput{Version: 1, Reason: "context_budget"})
	if !strings.Contains(plain, "压缩摘要开关未开启，或前缀重放失败已回退本地压缩") {
		t.Fatalf("没有原因时应保留兜底措辞：\n%s", plain)
	}
	if !strings.Contains(plain, "不是**对被折原文的总结") {
		t.Fatalf("兜底措辞里「这不是总结」的口径不能丢：\n%s", plain)
	}
}

// TestCompactionFrameBodyBlamesTheRecordedPushFailure：帧自己记着"连栈帧都没推成"时，
// 兜底措辞不得把这次压缩读成**开关问题**。
//
// 现场（2026-10-04，dev GUI 折出的一帧）：同一帧里两句话互相矛盾——JSON 的
// readback.note 写着 `推帧失败：compaction index: 会话上下文存储未绑定（压缩栈不可用）`，
// Markdown 正文却写着"压缩摘要开关未开启，或前缀重放失败已回退本地压缩"。前者是
// **接线**（那一跳没有可用的会话上下文存储），后者是**配置**：读帧的人按后者去查，
// 会去查一个并不存在的配置事故（当时配置里 `enabled: true`，开关是开的）。用户据此
// 提问"压缩摘要开关怎么开启"——一次接线故障被表述成了配置事故。
//
// 判据：帧里已有的推帧事实必须被用上。有 IndexError → 写出真实的推帧失败原因；
// IndexSkipped → 说清"没有可归档区间"；两者都没有（更早版本写的帧）才回到原来的
// 兜底措辞（口径由上面那条用例钉住）。
func TestCompactionFrameBodyBlamesTheRecordedPushFailure(t *testing.T) {
	const pushFailure = "compaction index: 会话上下文存储未绑定（压缩栈不可用）"

	failed := compactionFrameBody(compactionFrameInput{
		Version: 2, Reason: "context_budget_autonomous", Origin: "auto",
		IndexError: pushFailure,
	})
	if strings.Contains(failed, "压缩摘要开关未开启") {
		t.Fatalf("帧自己记着推帧失败，正文不得把这次压缩说成开关问题：\n%s", failed)
	}
	if !strings.Contains(failed, pushFailure) {
		t.Fatalf("正文应带上帧里已有的推帧失败原因（不是换一种说法，是原样带出）：\n%s", failed)
	}
	if meta := frameMetadataFrom(t, failed); !strings.Contains(meta.Readback.Note, pushFailure) {
		t.Fatalf("readback.note 应照旧写出推帧失败（同一事实两处不换口径）：%+v", meta.Readback)
	}

	// 索引面就绪、只是这次没有可归档区间：也不是开关问题。
	skipped := compactionFrameBody(compactionFrameInput{
		Version: 3, Reason: "context_budget", Origin: "manual", IndexSkipped: true,
	})
	if strings.Contains(skipped, "压缩摘要开关未开启") {
		t.Fatalf("没有折出区间不是开关问题：\n%s", skipped)
	}
	if !strings.Contains(skipped, "没有折出任何完整协议单元") {
		t.Fatalf("没有折出区间应如实写出这一种：\n%s", skipped)
	}
}
