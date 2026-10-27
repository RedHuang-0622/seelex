package store

// layout_golden_test.go — M0 基线门禁：会话物理布局 golden。
//
// 为什么需要它：store 拆分（docs/2026-10-27-store-ddd-split/milestones.md §4）
// 会把 message / event / compact / retention / lifecycle / tool-result / media
// 的实现搬到不同领域包。搬迁本身不该改变一个字节的落盘位置，但"名字改了一点"
// 恰恰是评审最容易放过去的那类回归——它不报错，只是让旧数据读不到。
//
// 判据分两层：
//  1. 路径集合：会话目录下的相对路径必须与 golden 逐字一致。内容寻址的段
//     （project/session/tool-result 的 16 位前缀、media 的 64 位摘要）由本文件
//     用 crypto/sha256 独立算出，不调用 store 自己的 hash 函数——否则实现改了
//     命名规则，期望值会跟着一起改，门禁就成了同义反复。
//  2. head 信封字段：每个 metadata/<module>.json 的顶层 key 集合固定，钉住 wire。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	storeroot "github.com/RedHuang-0622/seelex/store/root"
)

const (
	layoutGoldenProjectID = "project-layout-golden"
	layoutGoldenSessionID = "session-layout-golden"
)

// layoutGoldenHash16 复刻落盘目录名的派生规则：sha256 前 8 字节的小写 hex。
// 这里独立实现是为了让 golden 能发现"hash 规则被改"类回归。
func layoutGoldenHash16(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

// layoutGoldenSHA256 是内容寻址段（media）的派生规则。
func layoutGoldenSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// layoutGoldenRelativePaths 返回会话根下的全部相对路径（目录与文件），已排序。
func layoutGoldenRelativePaths(t *testing.T, sessionRoot string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(sessionRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == sessionRoot {
			return nil
		}
		rel, err := filepath.Rel(sessionRoot, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk session root: %v", err)
	}
	sort.Strings(paths)
	return paths
}

// layoutGoldenHeadKeys 读取一个模块 head，返回稳定的顶层 key 集合。
func layoutGoldenHeadKeys(t *testing.T, sessionRoot, module string) []string {
	t.Helper()
	path := filepath.Join(sessionRoot, "metadata", module+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s head: %v", module, err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("decode %s head: %v", module, err)
	}
	keys := make([]string, 0, len(envelope))
	for key := range envelope {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// TestSessionLayoutGolden 钉住一次"全模块都写过"的会话的物理布局。
//
// 每个模块只驱动它自己的公开/包内写入口（不经过 application 编排），
// 这样 golden 只对 store 的落盘形状负责，不受上层用例变化牵连。
func TestSessionLayoutGolden(t *testing.T) {
	root := t.TempDir()
	repository, err := newJSONRepositoryWithLayout(root, storeroot.Settings{})
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })

	key := Key{ProjectID: layoutGoldenProjectID, SessionID: layoutGoldenSessionID}
	ctx := context.Background()

	// 1) 一次提交同时带 tool result、message 事件行与压缩记录（= writeCommitLayout 的三段）。
	created := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	toolResult := ToolResult{
		Ref:        "tool-result-1",
		Tool:       "bash",
		Content:    "hello from artifact",
		Digest:     "sha256:placeholder",
		Size:       len("hello from artifact"),
		TokenCount: 3,
		CreatedAt:  created,
	}
	if err := repository.WriteCommit(ctx, key, Commit{
		Events: []Event{
			{MessageID: "m0", Role: "user", Kind: EventKindUserInput, Content: "hi", CreatedAt: created},
			{MessageID: "m1", Role: "assistant", Kind: EventKindLLM, Content: "hello", CreatedAt: created},
		},
		ToolResults:       []ToolResult{toolResult},
		CompactionRecords: []json.RawMessage{json.RawMessage(`{"summary":"first"}`)},
	}); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// 2) 结构事件（event 通道）。
	if _, err := repository.layout.structuralEventCommit(key, "event-commit-1", []structuralEvent{{
		EventID:   1,
		Kind:      structuralEventFork,
		CommitID:  "event-commit-1",
		CreatedAt: created,
	}}); err != nil {
		t.Fatalf("structural event: %v", err)
	}

	// 3) 压缩帧（compact 通道）。
	if _, err := repository.layout.compactCommit(key, compactFrameRecord{
		FrameID:        "frame-1",
		MessageFrom:    "m0",
		MessageTo:      "m1",
		MessageFromSeq: 0,
		MessageToSeq:   1,
		Summary:        "compact summary",
		BoundaryStatus: CompactBoundaryComplete,
		CommitID:       "compact-commit-1",
		CompressedAt:   created,
	}); err != nil {
		t.Fatalf("compact frame: %v", err)
	}

	// 4) 草稿与队列（lifecycle 通道）。
	if err := repository.layout.saveDraft(key, "draft content"); err != nil {
		t.Fatalf("save draft: %v", err)
	}
	if err := repository.layout.queueEnqueue(key, "request-1", "queued content"); err != nil {
		t.Fatalf("queue enqueue: %v", err)
	}

	// 5) 媒体（meta/ 内容寻址分区）。
	mediaData := []byte("media bytes")
	if _, err := repository.WriteMedia(ctx, key, MediaItem{
		Name:     "note.txt",
		MimeType: "text/plain",
		Kind:     "screenshot",
		Data:     mediaData,
	}); err != nil {
		t.Fatalf("write media: %v", err)
	}

	// 6) retention 水位。注意：读即写——缺失时 readRetentionHead 会落默认 head。
	if _, err := repository.layout.readRetentionHead(key); err != nil {
		t.Fatalf("read retention head: %v", err)
	}

	sessionRoot := repository.sessionDir(key)
	wantRoot := filepath.Join(
		root,
		"project-"+layoutGoldenHash16(layoutGoldenProjectID),
		"session-"+layoutGoldenHash16(layoutGoldenSessionID),
	)
	if filepath.Clean(sessionRoot) != filepath.Clean(wantRoot) {
		t.Fatalf("会话根路径派生规则变化：got %s want %s（旧数据将读不到）", sessionRoot, wantRoot)
	}

	mediaDigest := layoutGoldenSHA256(mediaData)
	want := []string{
		"big_tool_result",
		"big_tool_result/" + layoutGoldenHash16(toolResult.Ref) + ".result.json",
		"compact.jsonl",
		"compaction-records.jsonl",
		"event",
		"event/event_1_1.jsonl",
		"input",
		"input/draft.json",
		"message",
		"message/message_1_2.jsonl",
		"meta",
		"meta/" + mediaDigest,
		"meta/" + mediaDigest + "/meta.json",
		"meta/" + mediaDigest + "/note.txt",
		"metadata",
		"metadata/compact.json",
		"metadata/compaction.json",
		"metadata/event.json",
		"metadata/guide.json",
		"metadata/lifecycle.json",
		"metadata/media.json",
		"metadata/message.json",
		"metadata/retention.json",
		"metadata-index",
		"metadata-index/toolresult.json",
		"queue",
		"queue/queue.jsonl",
	}
	sort.Strings(want)

	got := layoutGoldenRelativePaths(t, sessionRoot)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("会话物理布局变化（store 拆分不得改路径）：\n缺少：%v\n多出：%v",
			layoutGoldenMissing(got, want), layoutGoldenMissing(want, got))
	}

	// head 信封字段固定（wire 兼容）：拆分后 domain 包写出的信封必须仍然是这些 key。
	wantHeadKeys := map[string]string{
		"guide":      "created_at layout_version schema_version session_id updated_at",
		"message":    "checksum commit_id module_id payload schema_version session_id updated_at",
		"event":      "checksum commit_id module_id payload schema_version session_id updated_at",
		"compact":    "checksum commit_id module_id payload schema_version session_id updated_at",
		"compaction": "checksum commit_id module_id payload schema_version session_id updated_at",
		"retention":  "checksum commit_id module_id payload schema_version session_id updated_at",
		"lifecycle":  "checksum commit_id module_id payload schema_version session_id updated_at",
		"media":      "checksum commit_id module_id payload schema_version session_id updated_at",
	}
	modules := make([]string, 0, len(wantHeadKeys))
	for module := range wantHeadKeys {
		modules = append(modules, module)
	}
	sort.Strings(modules)
	for _, module := range modules {
		got := strings.Join(layoutGoldenHeadKeys(t, sessionRoot, module), " ")
		if got != wantHeadKeys[module] {
			t.Errorf("metadata/%s.json 顶层字段变化：\n got: %s\nwant: %s", module, got, wantHeadKeys[module])
		}
	}
}

// layoutGoldenMissing 返回 a 中有、b 中没有的元素（用于失败信息）。
func layoutGoldenMissing(a, b []string) []string {
	present := make(map[string]struct{}, len(b))
	for _, item := range b {
		present[item] = struct{}{}
	}
	var missing []string
	for _, item := range a {
		if _, ok := present[item]; !ok {
			missing = append(missing, item)
		}
	}
	return missing
}
