package sessionstore

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

// TestFirstUserInputsSkipsInjectedUserRows：会话最前面的若干条用户输入要么是
// 真用户输入，要么是"以 user 身份写盘"的内部注入（技能/上下文注入行 kind=
// internal；老数据 kind 为空但正文以 <!-- seelex: 开头）。标题回填只能取前者
// ——否则会话标题会变成注入正文（"<!-- seelex:active-skill:v1 -->"）。
func TestFirstUserInputsSkipsInjectedUserRows(t *testing.T) {
	store, key := messageFixture(t, 0)
	rows := []Event{
		messageRow(0, "m1", "user", EventKindInternal, "<!-- seelex:active-skill:v1 -->\n## Trusted Active Skill: goal"),
		// 老数据：kind 为空（EventKindOf 按 role=user 回退成 user_input），
		// 但正文是内部注入前缀 → 必须按注入行跳过。
		messageRow(0, "m2", "user", "", "<!-- seelex:skill-context:v1 display=abc -->\nbody"),
		messageRow(0, "m3", "user", EventKindUserInput, "  注入后的第一条真问题  "),
		messageRow(0, "m4", "assistant", EventKindLLM, "回答"),
	}
	if _, err := store.messageCommit(key, "c1", rows); err != nil {
		t.Fatal(err)
	}

	got, hasLayout, err := store.firstShardUserInputs(key, FirstUserInputProbeRows)
	if err != nil || !hasLayout {
		t.Fatalf("firstShardUserInputs = %v hasLayout=%v err=%v", got, hasLayout, err)
	}
	if want := []string{"注入后的第一条真问题"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("firstShardUserInputs = %q, want %q（注入行不得成为标题候选）", got, want)
	}
}

// TestFirstUserInputsStopsEarlyInFirstShard：标题回填是**有界读**——只打开首个
// 消息分片，且**扫到够数就停**（不回读整片、不解码后续大行、不读尾部窗口）。
// b512284 删掉的旧兜底会为每个无标题会话打开历史窗口（长会话等于全量读），
// 这里用两个可观测契约钉住：
//
//   - 语义：后续分片里的用户输入不得出现在候选里；
//   - 成本：首片后段塞满大行（每行 ~128 KB）时，早停扫描仍必须显著快于
//     整片读——退回"读完整片再取前几条"会立刻慢一个数量级。
func TestFirstUserInputsStopsEarlyInFirstShard(t *testing.T) {
	const shardRows = 100
	const totalRows = shardRows * 3
	store, key := messageFixture(t, shardRows)
	big := ""
	for index := 0; index < 128*1024; index++ {
		big += "x"
	}
	rows := make([]Event, 0, totalRows)
	rows = append(rows, messageRow(0, "m1", "user", EventKindUserInput, "会话第一问"))
	for index := 1; index < totalRows; index++ {
		// 第二个分片起放"诱饵用户输入"：有界读若越过首片就会把它们带出来。
		if index%shardRows == 0 {
			rows = append(rows, messageRow(0, "decoy", "user", EventKindUserInput, "后续分片的用户输入"))
			continue
		}
		rows = append(rows, messageRow(0, "", "tool", EventKindToolOutput, fmt.Sprintf("%d-%s", index, big)))
	}
	if _, err := store.messageCommit(key, "bulk", rows); err != nil {
		t.Fatal(err)
	}
	head, err := store.readMessageHead(key)
	if err != nil || len(head.Shards) < 3 {
		t.Fatalf("fixture shards = %d err=%v, want >= 3", len(head.Shards), err)
	}

	got, hasLayout, err := store.firstShardUserInputs(key, FirstUserInputProbeRows)
	if err != nil || !hasLayout {
		t.Fatalf("firstShardUserInputs = %v hasLayout=%v err=%v", got, hasLayout, err)
	}
	if want := []string{"会话第一问"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("firstShardUserInputs = %q, want %q（有界读不得越出首个分片）", got, want)
	}

	// 先做一次整片/全量读把分片文件读进页缓存，再量早停扫描：比较的是**解码
	// 与读取量**，不是冷热盘。
	start := time.Now()
	full, err := store.readRows(key, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	fullDuration := time.Since(start)
	start = time.Now()
	bounded, _, err := store.firstShardUserInputs(key, FirstUserInputProbeRows)
	if err != nil {
		t.Fatal(err)
	}
	boundedDuration := time.Since(start)
	// 复测一次：区分"早停扫描本身的成本"与首次读的开销（冷页/杀软扫描新文件）。
	start = time.Now()
	if _, _, err := store.firstShardUserInputs(key, FirstUserInputProbeRows); err != nil {
		t.Fatal(err)
	}
	repeatDuration := time.Since(start)
	t.Logf("rows=%d shards=%d shard1_bytes≈%d full_read=%d rows in %s bounded_title_probe=%d inputs in %s repeat=%s",
		totalRows, len(head.Shards), len(big)*shardRows, len(full), fullDuration, len(bounded), boundedDuration, repeatDuration)
	if boundedDuration*2 > fullDuration {
		t.Fatalf("早停扫描未生效：bounded=%s full_read=%s（退回整片解码？）", boundedDuration, fullDuration)
	}
}

// TestFirstUserInputsReportsMissingLayout：会话还没有已发布行（未落盘的新会话/
// 空会话）时 hasLayout=false——调用方按"暂时没有答案"处理并在下一轮目录刷新
// 重试；确实没有用户输入（如只有注入行）则 hasLayout=true + 空候选，调用方
// 不必再探。
func TestFirstUserInputsReportsMissingLayout(t *testing.T) {
	store, key := messageFixture(t, 0)

	if got, hasLayout, err := store.firstShardUserInputs(key, 0); err != nil || hasLayout || got != nil {
		t.Fatalf("未落盘会话: got=%v hasLayout=%v err=%v, want 无布局", got, hasLayout, err)
	}

	// 布局存在但没有用户输入行（只有内部注入行）。
	if _, err := store.messageCommit(key, "c1", []Event{
		messageRow(0, "m1", "user", EventKindInternal, "<!-- seelex:context -->"),
		messageRow(0, "m2", "assistant", EventKindLLM, "回答"),
	}); err != nil {
		t.Fatal(err)
	}
	got, hasLayout, err := store.firstShardUserInputs(key, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !hasLayout || len(got) != 0 {
		t.Fatalf("无用户输入会话: got=%v hasLayout=%v, want 空候选 + hasLayout=true", got, hasLayout)
	}
}

// TestFirstUserInputsRefusesTrimmedPrefix：首问所在前缀已被 LRU 淘汰（首个分片
// 不从 seq 1 起）时不得拿会话中段的提问冒充"用户第一问"——返回空候选 + 确定性
// 结论（hasLayout=true，调用方不必再探）。
func TestFirstUserInputsRefusesTrimmedPrefix(t *testing.T) {
	store, key := messageFixture(t, 0)
	rows := []Event{
		messageRow(1, "u1", "user", EventKindUserInput, "会话第一问"),
		messageRow(2, "a1", "assistant", EventKindLLM, "回答"),
		messageRow(3, "u2", "user", EventKindUserInput, "会话中段的提问"),
	}
	if _, err := store.messageCommit(key, "c1", rows); err != nil {
		t.Fatal(err)
	}
	// 模拟 LRU 前缀淘汰：删掉 seq 1（首片重写为只剩 seq 2..3）。
	if _, err := store.lRUDelete(key, 1, true); err != nil {
		t.Fatalf("lru delete: %v", err)
	}
	head, err := store.readMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(head.Shards) == 0 || head.Shards[0].FromSeq == 1 {
		t.Fatalf("fixture 未形成淘汰前缀：%+v", head.Shards)
	}

	got, hasLayout, err := store.firstShardUserInputs(key, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !hasLayout || len(got) != 0 {
		t.Fatalf("淘汰前缀会话: got=%v hasLayout=%v, want 空候选（不拿中段提问冒充首问）", got, hasLayout)
	}
}
