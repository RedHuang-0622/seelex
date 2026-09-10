package sessionstore

import (
	"fmt"
	"testing"
	"time"
)

func wireFixture(t *testing.T) (*storeEngine, Key) {
	t.Helper()
	store := newStoreEngine(t.TempDir(), storageSettings{})
	return store, Key{ProjectID: "p-r2", SessionID: "s-r2"}
}

// roundRows 生成 seq 连续的轮次行（user + assistant 交替）。
func roundRows(count int) []Event {
	rows := make([]Event, 0, count)
	for index := 1; index <= count; index++ {
		if index%2 == 1 {
			rows = append(rows, messageRow(uint64(index), fmt.Sprintf("msg%d", index), "user", EventKindUserInput, fmt.Sprintf("问题-%d", index)))
		} else {
			rows = append(rows, messageRow(uint64(index), fmt.Sprintf("msg%d", index), "assistant", EventKindLLM, fmt.Sprintf("回答-%d", index)))
		}
	}
	return rows
}

func commitRoundRows(t *testing.T, store *storeEngine, key Key, rows []Event) {
	t.Helper()
	if _, err := store.messageCommit(key, "m2-"+fmt.Sprintf("%d", time.Now().UnixNano()), rows); err != nil {
		t.Fatal(err)
	}
}

// ---------- R1 ----------

// TestHistoryPagePagingStableAndMarksInternal 对应 T-R1-01。
func TestHistoryPagePagingStableAndMarksInternal(t *testing.T) {
	store, key := wireFixture(t)
	rows := roundRows(100)
	rows[49] = messageRow(50, "msg50", "internal_user", EventKindInternal, "内部材料")
	rows[50] = messageRow(51, "msg51", "context", EventKindInternal, "上下文块")
	commitRoundRows(t, store, key, rows)

	page, total, err := store.pageHistoryRows(key, 40, 20)
	if err != nil || total != 100 || len(page) != 20 {
		t.Fatalf("page len=%d total=%d err=%v", len(page), total, err)
	}
	// 页序稳定：第二次读同页逐条一致。
	page2, _, err := store.pageHistoryRows(key, 40, 20)
	if err != nil {
		t.Fatal(err)
	}
	for index := range page {
		if page[index].Seq != page2[index].Seq || page[index].Content != page2[index].Content {
			t.Fatalf("page unstable at %d", index)
		}
	}
	// internal/context 按展示规则标记。
	if !page[9].Internal || page[9].MessageID != "msg50" {
		t.Fatalf("internal row not marked: %+v", page[9])
	}
	if !page[10].Internal || page[10].MessageID != "msg51" {
		t.Fatalf("context row not marked: %+v", page[10])
	}
}

// TestHistoryPageCompactKeepsOriginalRows 对应 T-R1-02：压缩不删正文，R1 仍能读到
// 被压缩覆盖的原始行。
func TestHistoryPageCompactKeepsOriginalRows(t *testing.T) {
	store, key := wireFixture(t)
	commitRoundRows(t, store, key, roundRows(60))
	frame := compactFrameRecord{
		FrameID: "f1", MessageFrom: "msg1", MessageTo: "msg50", MessageFromSeq: 1, MessageToSeq: 50,
		Summary: "覆盖前 50 行的摘要", BoundaryStatus: "complete",
	}
	if _, err := store.compactCommit(key, frame); err != nil {
		t.Fatal(err)
	}
	page, total, err := store.pageHistoryRows(key, 0, 20)
	if err != nil || total != 60 || len(page) != 20 {
		t.Fatalf("page len=%d total=%d err=%v", len(page), total, err)
	}
	if page[0].MessageID != "msg1" || page[0].Content != "问题-1" {
		t.Fatalf("compressed rows gone: %+v", page[0])
	}
}

// TestHistoryPageLRUPlaceholderNoHoles 对应 T-R1-03：LRU 删除 watermark 前行后，
// R1 历史读取返回摘要占位；不 panic、不返回空洞错位。
func TestHistoryPageLRUPlaceholderNoHoles(t *testing.T) {
	store, key := wireFixture(t)
	commitRoundRows(t, store, key, roundRows(40))
	if _, err := store.lRUDelete(key, 10, true); err != nil {
		t.Fatal(err)
	}
	page, total, err := store.pageHistoryRows(key, 0, 15)
	if err != nil || total != 40 || len(page) != 15 {
		t.Fatalf("page len=%d total=%d err=%v", len(page), total, err)
	}
	for index, row := range page {
		if index < 10 {
			if !row.Placeholder {
				t.Fatalf("row[%d] not placeholder: %+v", index, row)
			}
			if row.Seq != uint64(index+1) {
				t.Fatalf("placeholder seq=%d want %d", row.Seq, index+1)
			}
		}
	}
	if page[10].MessageID != "msg11" {
		t.Fatalf("row[10]=%q want msg11", page[10].MessageID)
	}
}

// TestHistoryPageAssistantToolCallsSingleRow 对应 T-R1-04：assistant 带多个
// tool_calls 存储仍是 1 事件行。
func TestHistoryPageAssistantToolCallsSingleRow(t *testing.T) {
	store, key := wireFixture(t)
	row := Event{
		Seq: 1, MessageID: "msg1", Role: "assistant", Kind: EventKindToolCall,
		ToolCalls: []EventToolCall{
			{ID: "c1", Name: "bash", Arguments: "{}"},
			{ID: "c2", Name: "read_file", Arguments: "{}"},
		},
		CreatedAt: time.Now().UTC(),
	}
	commitRoundRows(t, store, key, []Event{row})
	page, total, err := store.pageHistoryRows(key, 0, 10)
	if err != nil || total != 1 || len(page) != 1 {
		t.Fatalf("page len=%d total=%d err=%v", len(page), total, err)
	}
	if len(page[0].ToolCalls) != 2 {
		t.Fatalf("tool calls count = %d", len(page[0].ToolCalls))
	}
}

// ---------- R2 ----------

func runWireAssembly(t *testing.T, store *storeEngine, key Key, cache *attemptCache, budget, k int) wireResult {
	t.Helper()
	result, err := store.assembleWire(key, cache, wireParams{Budget: budget, K: k})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func wireRoles(result wireResult) []string {
	roles := make([]string, len(result.Messages))
	for index, message := range result.Messages {
		roles[index] = message.Role
	}
	return roles
}

// TestWireAssemblyStablePrefix 对应 T-R2-01：无 frame、无新消息两次 R2 前缀一致。
func TestWireAssemblyStablePrefix(t *testing.T) {
	store, key := wireFixture(t)
	commitRoundRows(t, store, key, roundRows(5))
	cache := NewAttemptCache(0, 0)
	first := runWireAssembly(t, store, key, cache, 200_000, 3)
	second := runWireAssembly(t, store, key, cache, 200_000, 3)
	if first.PrefixDigest != second.PrefixDigest {
		t.Fatalf("digest unstable: %s vs %s", first.PrefixDigest, second.PrefixDigest)
	}
	if !wirePrefixEqual(first.Messages, second.Messages) {
		t.Fatal("wire prefix changed without new messages")
	}
}

// TestWireAssemblyTailStartsAfterFrame 对应 T-R2-02：frame=[msg3,msg7] → 摘要在前；
// 首条 tail = msg8。
func TestWireAssemblyTailStartsAfterFrame(t *testing.T) {
	store, key := wireFixture(t)
	commitRoundRows(t, store, key, roundRows(10))
	frame := compactFrameRecord{
		FrameID: "f1", MessageFrom: "msg3", MessageTo: "msg7",
		MessageFromSeq: 3, MessageToSeq: 7,
		Summary: "### 摘要内容", BoundaryStatus: "complete",
	}
	if _, err := store.compactCommit(key, frame); err != nil {
		t.Fatal(err)
	}
	result := runWireAssembly(t, store, key, nil, 200_000, 3)
	if len(result.Messages) == 0 || result.Messages[0].Content != "### 摘要内容" {
		t.Fatalf("summary not first: %+v", result.Messages)
	}
	if result.TailStartSeq != 8 {
		t.Fatalf("tail start = %d want 8", result.TailStartSeq)
	}
	if len(result.Messages) < 2 || result.Messages[1].Seq != 8 {
		t.Fatalf("first tail message not msg8: %+v", result.Messages[1:])
	}
	if result.Messages[1].Seq != 8 || result.Messages[1].Role != "assistant" {
		t.Fatalf("msg8 missing from wire: %+v", result.Messages[1:])
	}
}

// TestWireAssemblyFilterInternalRows 对应 T-R2-03：internal/context 行仅
// wire_material=true 进 wire。
func TestWireAssemblyFilterInternalRows(t *testing.T) {
	store, key := wireFixture(t)
	internalOff := messageRow(2, "i-off", "internal_user", EventKindInternal, "不装配")
	internalOn := messageRow(3, "i-on", "internal_user", EventKindInternal, "装配材料")
	internalOn.WireMaterial = true
	rows := []Event{
		messageRow(1, "u1", "user", EventKindUserInput, "hi"),
		internalOff,
		internalOn,
		messageRow(4, "a1", "assistant", EventKindLLM, "ok"),
	}
	commitRoundRows(t, store, key, rows)
	result := runWireAssembly(t, store, key, nil, 200_000, 3)
	count := 0
	for _, message := range result.Messages {
		if message.Internal {
			count++
			if message.Content != "装配材料" {
				t.Fatalf("internal wire content = %q", message.Content)
			}
		}
	}
	if count != 1 {
		t.Fatalf("wire_material rows = %d want 1", count)
	}
}

// TestWireAssemblyCacheKeepsRecentK 对应 T-R2-04：同操作重试 > K，wire 只含最近
// K 条、时间序正确。
func TestWireAssemblyCacheKeepsRecentK(t *testing.T) {
	store, key := wireFixture(t)
	rows := []Event{
		messageRow(1, "u1", "user", EventKindUserInput, "跑一下"),
		{Seq: 2, MessageID: "a1", Role: "assistant", Kind: EventKindToolCall, ToolCalls: []EventToolCall{{ID: "c1", Name: "bash", Arguments: "{}"}}, CreatedAt: time.Now()},
		messageRow(3, "t1", "tool", EventKindToolOutput, "结果"),
	}
	commitRoundRows(t, store, key, rows)
	cache := NewAttemptCache(0, 0)
	for index := 1; index <= 5; index++ {
		cache.Add(2, "bash", "assistant", fmt.Sprintf("第 %d 次失败", index), "failed")
	}
	result := runWireAssembly(t, store, key, cache, 200_000, 3)
	attempts := 0
	for _, message := range result.Messages {
		if message.Attempt {
			attempts++
		}
	}
	if attempts != 3 {
		t.Fatalf("K=3 wire attempts = %d want 3", attempts)
	}
	cache2 := NewAttemptCache(0, 0)
	for index := 1; index <= 5; index++ {
		cache2.Add(2, "bash", "assistant", fmt.Sprintf("第 %d 次失败", index), "failed")
	}
	result2 := runWireAssembly(t, store, key, cache2, 200_000, 2)
	attempts = 0
	for _, message := range result2.Messages {
		if message.Attempt {
			attempts++
		}
	}
	if attempts != 2 {
		t.Fatalf("K=2 wire attempts = %d want 2", attempts)
	}
}

// TestWireAssemblyCacheClearedPurePersistent 对应 T-R2-05：缓存清空后输出与纯持久
// 数据装配一致（无尝试说明行）。
func TestWireAssemblyCacheClearedPurePersistent(t *testing.T) {
	store, key := wireFixture(t)
	commitRoundRows(t, store, key, roundRows(4))
	cache := NewAttemptCache(0, 0)
	cache.Add(2, "op", "assistant", "尝试内容", "failed")
	withCache := runWireAssembly(t, store, key, cache, 200_000, 3)
	cache.Clear()
	cleared := runWireAssembly(t, store, key, cache, 200_000, 3)
	for _, message := range cleared.Messages {
		if message.Attempt {
			t.Fatal("attempt row should not exist after clear")
		}
	}
	if cleared.PrefixDigest != wireDigest(wirePrefixStrip(withCache.Messages)) {
		t.Fatal("cleared output differs from pure persistent assembly")
	}
}

// TestWireAssemblyOrphanToolSkipped 对应 T-R2-06：孤儿 tool 行不进入 wire。
func TestWireAssemblyOrphanToolSkipped(t *testing.T) {
	store, key := wireFixture(t)
	rows := []Event{
		messageRow(1, "u1", "user", EventKindUserInput, "问题"),
		messageRow(2, "a1", "assistant", EventKindLLM, "回答"),
		{Seq: 3, MessageID: "orphan", Role: "tool", Kind: EventKindToolOutput, ToolCallID: "no-declare", Content: "孤儿", CreatedAt: time.Now()},
	}
	commitRoundRows(t, store, key, rows)
	result := runWireAssembly(t, store, key, nil, 200_000, 3)
	for _, message := range result.Messages {
		if message.ToolCallID == "no-declare" {
			t.Fatal("orphan tool row entered wire")
		}
	}
}

// TestWireAssemblyOpenUnitRepairNoSuffixDrop 对应 T-R2-07：流尾残缺工具轮保留 open
// 单元 + c2 修复占位，不丢尾。
func TestWireAssemblyOpenUnitRepairNoSuffixDrop(t *testing.T) {
	store, key := wireFixture(t)
	rows := []Event{
		messageRow(1, "u1", "user", EventKindUserInput, "工具"),
		{Seq: 2, MessageID: "a1", Role: "assistant", Kind: EventKindToolCall, ToolCalls: []EventToolCall{{ID: "c1", Name: "bash", Arguments: "{}"}, {ID: "c2", Name: "read", Arguments: "{}"}}, CreatedAt: time.Now()},
		{Seq: 3, MessageID: "t1", Role: "tool", Kind: EventKindToolOutput, ToolCallID: "c1", Content: "结果1", CreatedAt: time.Now()},
	}
	commitRoundRows(t, store, key, rows)
	result := runWireAssembly(t, store, key, nil, 200_000, 3)
	if !result.Open {
		t.Fatal("open flag missing")
	}
	repair := 0
	for _, message := range result.Messages {
		if message.Repair {
			repair++
			if message.ToolCallID != "c2" {
				t.Fatalf("repair call = %q want c2", message.ToolCallID)
			}
		}
	}
	if repair != 1 {
		t.Fatalf("repair rows = %d want 1", repair)
	}
}

// TestWireAssemblyOpenRepairThenNextUser 对应 T-R2-08：缺失结果后有后续 user →
// 修复后继续，不跳 next user、无重排。
func TestWireAssemblyOpenRepairThenNextUser(t *testing.T) {
	store, key := wireFixture(t)
	rows := []Event{
		messageRow(1, "u1", "user", EventKindUserInput, "第一问"),
		{Seq: 2, MessageID: "a1", Role: "assistant", Kind: EventKindToolCall, ToolCalls: []EventToolCall{{ID: "c1", Name: "bash", Arguments: "{}"}}, CreatedAt: time.Now()},
		messageRow(3, "u2", "user", EventKindUserInput, "第二问"),
		messageRow(4, "a2", "assistant", EventKindLLM, "第二答"),
	}
	commitRoundRows(t, store, key, rows)
	result := runWireAssembly(t, store, key, nil, 200_000, 3)
	roles := wireRoles(result)
	lastUser := -1
	for index, role := range roles {
		if role == "user" {
			lastUser = index
		}
	}
	if lastUser < 0 || result.Messages[lastUser].Content != "第二问" {
		t.Fatalf("next user missing or reordered: %+v", result.Messages)
	}
}

// TestWireAssemblyBudgetStopsAtUnitBoundary 对应 T-R2-09：预算极小 → 停在完整单元
// 边界并置 need_compact。
func TestWireAssemblyBudgetStopsAtUnitBoundary(t *testing.T) {
	store, key := wireFixture(t)
	rows := []Event{
		{Seq: 1, MessageID: "u1", Role: "user", Kind: EventKindUserInput, Content: "第一轮长内容", TokenCount: 5, CreatedAt: time.Now()},
		{Seq: 2, MessageID: "a1", Role: "assistant", Kind: EventKindLLM, Content: "第一轮回答长内容", TokenCount: 5, CreatedAt: time.Now()},
		{Seq: 3, MessageID: "u2", Role: "user", Kind: EventKindUserInput, Content: "第二轮长内容", TokenCount: 5, CreatedAt: time.Now()},
		{Seq: 4, MessageID: "a2", Role: "assistant", Kind: EventKindLLM, Content: "第二轮回答长内容", TokenCount: 5, CreatedAt: time.Now()},
	}
	commitRoundRows(t, store, key, rows)
	result := runWireAssembly(t, store, key, nil, 5, 3)
	if !result.NeedCompact {
		t.Fatal("need_compact missing")
	}
	if len(result.Messages) != 1 || result.Messages[0].Seq != 1 {
		t.Fatalf("wire len=%d want 1 (完整单元边界)", len(result.Messages))
	}
}

// TestWireAssemblyEventMissingNoEffect 对应 T-R2-10：删除 event.json 条目（短窗口
// 崩溃）不影响 R2。
func TestWireAssemblyEventMissingNoEffect(t *testing.T) {
	store, key := wireFixture(t)
	commitRoundRows(t, store, key, roundRows(6))
	if _, err := store.structuralEventCommit(key, "ev-1", []structuralEvent{{Kind: structuralEventCompacted, AnchorSeq: 4, Payload: rawJSON(`{"reason":"test"}`)}}); err != nil {
		t.Fatal(err)
	}
	withEvent := runWireAssembly(t, store, key, nil, 200_000, 3)
	// 删除 event head 与数据（模拟短窗口崩溃：message 已发布、event 缺失）。
	if err := store.deleteModule(key, moduleEvent); err != nil {
		t.Fatal(err)
	}
	withoutEvent := runWireAssembly(t, store, key, nil, 200_000, 3)
	if withEvent.PrefixDigest != withoutEvent.PrefixDigest {
		t.Fatal("R2 output changed after event deletion")
	}
}

// TestWireAssemblyLRUDeletedRegionOnlyFrameAndTail 对应 T-R2-11：LRU 已删旧区后 R2
// 只依赖最新 frame 与其后 tail，正常输出。
func TestWireAssemblyLRUDeletedRegionOnlyFrameAndTail(t *testing.T) {
	store, key := wireFixture(t)
	commitRoundRows(t, store, key, roundRows(20))
	frame := compactFrameRecord{
		FrameID: "f1", MessageFrom: "msg1", MessageTo: "msg10",
		MessageFromSeq: 1, MessageToSeq: 10,
		Summary: "摘要覆盖前 10 行", BoundaryStatus: "complete",
	}
	if _, err := store.compactCommit(key, frame); err != nil {
		t.Fatal(err)
	}
	if _, err := store.lRUDelete(key, 8, true); err != nil {
		t.Fatal(err)
	}
	result := runWireAssembly(t, store, key, nil, 200_000, 3)
	if result.Messages[0].Content != "摘要覆盖前 10 行" {
		t.Fatalf("frame summary missing: %+v", result.Messages[0])
	}
	if result.TailStartSeq != 11 {
		t.Fatalf("tail start = %d want 11", result.TailStartSeq)
	}
}

// ---------- R3 ----------

// TestResumeTailInterruptedAnchorTail 对应 T-R3-01：interrupted 锚 msg95，后续
// user 到 msg120 → 只处理断点后尾段。
func TestResumeTailInterruptedAnchorTail(t *testing.T) {
	store, key := wireFixture(t)
	commitRoundRows(t, store, key, roundRows(120))
	if _, err := store.structuralEventCommit(key, "ev-i", []structuralEvent{{
		Kind: structuralEventInterrupted, AnchorMessageID: "msg95", AnchorSeq: 95,
		Payload: rawJSON(`{"reason":"interrupted"}`),
	}}); err != nil {
		t.Fatal(err)
	}
	point, err := store.resumeTail(key)
	if err != nil {
		t.Fatal(err)
	}
	if point.AnchorSeq != 95 {
		t.Fatalf("anchor = %d want 95", point.AnchorSeq)
	}
	if len(point.Rows) != 25 {
		t.Fatalf("tail rows = %d want 25", len(point.Rows))
	}
	if point.Rows[0].Seq != 96 || point.Rows[len(point.Rows)-1].Seq != 120 {
		t.Fatalf("tail range = [%d,%d]", point.Rows[0].Seq, point.Rows[len(point.Rows)-1].Seq)
	}
}

// TestResumeTailCompleteNoFakeInterrupted 对应 T-R3-02：消息完整但 interrupted
// 事件缺失 → 不合成虚假 interrupted。
func TestResumeTailCompleteNoFakeInterrupted(t *testing.T) {
	store, key := wireFixture(t)
	commitRoundRows(t, store, key, roundRows(10))
	point, err := store.resumeTail(key)
	if err != nil {
		t.Fatal(err)
	}
	if point.Synthetic || point.Incomplete || len(point.Rows) != 0 {
		t.Fatalf("complete session fabricated resume: %+v", point)
	}
}

// TestResumeTailIncompleteSynthesizesInterrupted 对应 T-R3-03：消息残缺（缺 tool
// 结果）且 event 缺失 → 合成 interrupted 并给修复占位。
func TestResumeTailIncompleteSynthesizesInterrupted(t *testing.T) {
	store, key := wireFixture(t)
	rows := []Event{
		messageRow(1, "u1", "user", EventKindUserInput, "执行"),
		{Seq: 2, MessageID: "a1", Role: "assistant", Kind: EventKindToolCall, ToolCalls: []EventToolCall{{ID: "c1", Name: "bash", Arguments: "{}"}}, CreatedAt: time.Now()},
	}
	commitRoundRows(t, store, key, rows)
	point, err := store.resumeTail(key)
	if err != nil {
		t.Fatal(err)
	}
	if !point.Synthetic || !point.Incomplete {
		t.Fatalf("synthetic flag missing: %+v", point)
	}
	if len(point.Repair) != 1 || point.Repair[0].ToolCallID != "c1" {
		t.Fatalf("repair = %+v", point.Repair)
	}
	if len(point.Rows) != 1 || point.Rows[0].Seq != 2 {
		t.Fatalf("rows = %+v", point.Rows)
	}
}

// TestResumeTailRestartCacheEmptyR2OK 对应 T-R3-04：重启后尝试缓存为空，恢复后
// R2 不依赖缓存即可装配、不 panic。
func TestResumeTailRestartCacheEmptyR2OK(t *testing.T) {
	store, key := wireFixture(t)
	commitRoundRows(t, store, key, roundRows(6))
	// 重启语义：新建空缓存（进程退出即清空）。
	fresh := NewAttemptCache(0, 0)
	result, err := store.assembleWire(key, fresh, wireParams{Budget: 200_000})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) == 0 {
		t.Fatal("empty wire after restart")
	}
}

func rawJSON(value string) []byte {
	return []byte(value)
}

func wirePrefixStrip(messages []wireMessage) []wireMessage {
	out := make([]wireMessage, 0, len(messages))
	for _, message := range messages {
		if !message.Attempt {
			out = append(out, message)
		}
	}
	return out
}
