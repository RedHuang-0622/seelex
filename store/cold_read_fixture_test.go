package store

// cold_read_fixture_test.go — M0 基线门禁：代表性会话的冷读 fixture。
//
// 为什么需要它：docs/2026-10-27-store-ddd-split/milestones.md §4 要求 M1–M9
// 每一批迁移后，当前版本仍能**冷读**拆分前写出的数据。golden 用例
// （layout_golden_test.go）只钉"写出的路径与 head 字段"，不钉"读回来的语义"：
// 路径一个字都没变，却把 payload 的键名或解码顺序改掉，golden 照样全绿。
//
// 判据是"只读"：本文件不调用任何写入口去生成数据再读，而是把
// `store/testdata/coldread/` 里**已提交的磁盘数据**复制到临时数据根，然后用
// `newJSONRepositoryWithLayout` 打开并逐通道断言语义。写入侧只有一个
// 环境变量保护的重建用例（TestColdReadFixtureRegenerate），它只改 testdata，
// 不参与日常回归。
//
// 为什么断言语义而不是字节：head 信封的 updated_at 来自 time.Now()，
// draft/queue 的 saved_at、created_at 也带当前时间，每次重建的值都不同。
// 把这些当成 golden 会让用例要么每天红一次，要么被迫把时间字段从数据里删掉
// ——后者才是真正的格式变更。所以这里钉的是"每个通道读回来的业务值"。

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	storeartifact "github.com/RedHuang-0622/seelex/store/artifact"
	storeroot "github.com/RedHuang-0622/seelex/store/root"
)

const (
	coldReadProjectID = "project-coldread"
	coldReadSessionID = "session-coldread"

	// coldReadFixtureDir 是相对包目录的 fixture 根；里面只有 project-<hash>。
	coldReadFixtureDir = "testdata/coldread"
	// coldReadRegenerateEnv 是重建开关：只有显式置 1 才允许改 testdata。
	coldReadRegenerateEnv = "SEELEX_COLDREAD_REGENERATE"

	coldReadToolRef       = "tool-result-coldread"
	coldReadToolCallID    = "call-coldread"
	coldReadToolName      = "bash"
	coldReadToolArguments = `{"command":"echo coldread"}`
	coldReadToolBody      = "coldread artifact body"
	coldReadUserText      = "coldread user input"
	coldReadReplyText     = "coldread assistant reply"
	coldReadToolText      = "coldread tool output"

	coldReadDraftText     = "coldread draft content"
	coldReadQueueRequest  = "request-coldread"
	coldReadQueueText     = "coldread queued content"
	coldReadMediaName     = "coldread-note.txt"
	coldReadMediaMimeType = "text/plain"
	coldReadMediaKind     = "screenshot"
	coldReadMediaBody     = "coldread media bytes"

	coldReadCompactFrameID  = "frame-coldread"
	coldReadCompactSummary  = "coldread compact summary"
	coldReadCompactCommitID = "compact-commit-coldread"
	coldReadEventCommitID   = "event-commit-coldread"
	coldReadEventPayload    = `{"reason":"coldread"}`
	coldReadCompactionBody1 = `{"turn":1,"reason":"budget"}`
	coldReadCompactionBody2 = `{"turn":2,"reason":"budget"}`
)

// coldReadCreatedAt 是固定的写入时间；它进入 message 行、工具结果、压缩帧与
// 结构事件的业务字段，冷读时应原样读回。
var coldReadCreatedAt = time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)

func coldReadKey() Key {
	return Key{ProjectID: coldReadProjectID, SessionID: coldReadSessionID}
}

// coldReadProjectDirName 复刻项目目录的派生规则，供测试定位 fixture。
// 独立实现是刻意的：若落盘规则变了，这里会跟着发现（golden 有同一手法）。
func coldReadProjectDirName() string {
	sum := sha256.Sum256([]byte(coldReadProjectID))
	return "project-" + hex.EncodeToString(sum[:8])
}

// TestColdReadFixture 是 M0 的主判据：已提交的磁盘数据必须能被当前代码读回。
func TestColdReadFixture(t *testing.T) {
	fixtureRoot := filepath.Join(storePackageDir(t), coldReadFixtureDir)
	fixtureProject := filepath.Join(fixtureRoot, coldReadProjectDirName())
	if _, err := os.Stat(fixtureProject); err != nil {
		t.Fatalf("冷读 fixture 缺失：%v\n重建命令：%s=1 go test ./store -run TestColdReadFixtureRegenerate -count=1",
			err, coldReadRegenerateEnv)
	}

	// 复制到临时数据根再打开：读路径有"读即写"修补（head 缺失补建、
	// lifecycle 按数据文件校准 head），直接在 fixture 上读会污染已提交的数据。
	root := t.TempDir()
	coldReadCopyTree(t, fixtureRoot, root)

	repository, err := newJSONRepositoryWithLayout(root, storeroot.Settings{})
	if err != nil {
		t.Fatalf("打开冷读数据根：%v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	key := coldReadKey()
	ctx := context.Background()

	if !repository.active(key) {
		t.Fatal("冷读会话未被识别为新布局：metadata/guide.json 缺失或布局判定被改")
	}

	t.Run("消息行", func(t *testing.T) {
		rows, err := repository.layout.readAllRows(key)
		if err != nil {
			t.Fatalf("readAllRows: %v", err)
		}
		want := []struct {
			seq     uint64
			message string
			role    string
			kind    string
			content string
		}{
			{1, "m0", "user", EventKindUserInput, coldReadUserText},
			{2, "m1", "assistant", EventKindLLM, coldReadReplyText},
			{3, "m2", "tool", EventKindToolOutput, coldReadToolText},
		}
		if len(rows) != len(want) {
			t.Fatalf("message 行数 = %d，want %d", len(rows), len(want))
		}
		for i, expected := range want {
			row := rows[i]
			if row.Seq != expected.seq || row.MessageID != expected.message ||
				row.Role != expected.role || row.Kind != expected.kind || row.Content != expected.content {
				t.Errorf("第 %d 行读回不一致：got {%d %q %q %q %q} want {%d %q %q %q %q}",
					i, row.Seq, row.MessageID, row.Role, row.Kind, row.Content,
					expected.seq, expected.message, expected.role, expected.kind, expected.content)
			}
		}
		if last := rows[len(rows)-1]; last.ResultRef != coldReadToolRef || last.Name != coldReadToolName {
			t.Errorf("工具行引用读回不一致：result_ref=%q name=%q", last.ResultRef, last.Name)
		}
	})

	t.Run("会话范围读", func(t *testing.T) {
		// limit<=0 是 total-only 语义：会话切换先探 total，再按窗口读尾部。
		probe, probeTotal, err := repository.ReadRange(ctx, key, 0, 0)
		if err != nil {
			t.Fatalf("ReadRange(total-only): %v", err)
		}
		if probeTotal != 3 || len(probe) != 0 {
			t.Fatalf("ReadRange(total-only) total=%d len=%d，want 3/0", probeTotal, len(probe))
		}

		messages, total, err := repository.ReadRange(ctx, key, 0, 10)
		if err != nil {
			t.Fatalf("ReadRange: %v", err)
		}
		// provider 消息来自完整事件单元：user、assistant(含 tool_calls)、tool。
		if total != 3 || len(messages) != 3 {
			t.Fatalf("ReadRange total=%d len=%d，want 3/3", total, len(messages))
		}
		wantRoles := []string{"user", "assistant", "tool"}
		for i, want := range wantRoles {
			if messages[i].Role != want {
				t.Errorf("provider 消息 %d role=%q，want %q", i, messages[i].Role, want)
			}
		}
		if calls := messages[1].ToolCalls; len(calls) != 1 || calls[0].ID != coldReadToolCallID {
			t.Errorf("assistant tool_calls 读回不一致：%+v", calls)
		}

	})

	t.Run("工具结果", func(t *testing.T) {
		result, err := repository.ReadToolResult(ctx, key, coldReadToolRef)
		if err != nil {
			t.Fatalf("ReadToolResult: %v", err)
		}
		if result.Tool != coldReadToolName || result.Content != coldReadToolBody {
			t.Errorf("工具结果读回不一致：tool=%q content=%q", result.Tool, result.Content)
		}
		if result.Size != len(coldReadToolBody) {
			t.Errorf("工具结果 Size=%d，want %d", result.Size, len(coldReadToolBody))
		}
	})

	t.Run("压缩记录", func(t *testing.T) {
		records, err := repository.layout.readCompactionRecords(key)
		if err != nil {
			t.Fatalf("readCompactionRecords: %v", err)
		}
		if len(records) != 2 {
			t.Fatalf("压缩记录行数 = %d，want 2", len(records))
		}
		for i, want := range []string{coldReadCompactionBody1, coldReadCompactionBody2} {
			if got := coldReadCompactJSON(t, records[i].Payload); got != want {
				t.Errorf("第 %d 条压缩记录 payload = %s，want %s（payload 是应用侧原文，存储层不得改写）",
					i+1, got, want)
			}
			if records[i].Seq != uint64(i+1) {
				t.Errorf("第 %d 条压缩记录 Seq = %d，want %d", i+1, records[i].Seq, i+1)
			}
		}
	})

	t.Run("压缩帧", func(t *testing.T) {
		head, err := repository.layout.readCompactHead(key)
		if err != nil {
			t.Fatalf("readCompactHead: %v", err)
		}
		if head.LastFrameID != coldReadCompactFrameID || head.FrameCount != 1 || head.LastSeq != 2 {
			t.Errorf("compact head 读回不一致：frame=%q count=%d last_seq=%d",
				head.LastFrameID, head.FrameCount, head.LastSeq)
		}
		if head.LatestFrame == nil {
			t.Fatal("compact head 缺少 latest_frame：R2 单读依赖它")
		}
		if head.LatestFrame.Summary != coldReadCompactSummary ||
			head.LatestFrame.BoundaryStatus != CompactBoundaryComplete ||
			head.LatestFrame.MessageToSeq != 2 {
			t.Errorf("最新帧读回不一致：summary=%q boundary=%q to_seq=%d",
				head.LatestFrame.Summary, head.LatestFrame.BoundaryStatus, head.LatestFrame.MessageToSeq)
		}
	})

	t.Run("草稿与队列", func(t *testing.T) {
		state, err := repository.layout.readLifecycleState(key)
		if err != nil {
			t.Fatalf("readLifecycleState: %v", err)
		}
		if state.Draft == nil {
			t.Fatal("草稿丢失：input/draft.json 未被读路径识别")
		}
		if state.Draft.Content != coldReadDraftText {
			t.Errorf("草稿内容 = %q，want %q", state.Draft.Content, coldReadDraftText)
		}
		if len(state.Queue) != 1 {
			t.Fatalf("队列条目 = %d，want 1", len(state.Queue))
		}
		item := state.Queue[0]
		if item.Content != coldReadQueueText || item.RequestID != coldReadQueueRequest || item.State != queueQueued {
			t.Errorf("队首读回不一致：content=%q request=%q state=%q",
				item.Content, item.RequestID, item.State)
		}
	})

	t.Run("媒体", func(t *testing.T) {
		items, err := repository.layout.artifacts.ListMedia(layoutScope(key))
		if err != nil {
			t.Fatalf("listMediaItems: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("媒体条目 = %d，want 1", len(items))
		}
		item := items[0]
		if item.Name != coldReadMediaName || item.MimeType != coldReadMediaMimeType || item.Kind != coldReadMediaKind {
			t.Errorf("媒体元数据读回不一致：name=%q mime=%q kind=%q", item.Name, item.MimeType, item.Kind)
		}
		if item.Bytes != len(coldReadMediaBody) {
			t.Errorf("媒体 Bytes=%d，want %d", item.Bytes, len(coldReadMediaBody))
		}
		_, data, err := repository.layout.artifacts.ReadMedia(layoutScope(key), item.Ref)
		if err != nil {
			t.Fatalf("readMediaLocked: %v", err)
		}
		if string(data) != coldReadMediaBody {
			t.Errorf("媒体正文 = %q，want %q", string(data), coldReadMediaBody)
		}
	})

	t.Run("retention 水位", func(t *testing.T) {
		head, err := repository.layout.readRetentionHead(key)
		if err != nil {
			t.Fatalf("readRetentionHead: %v", err)
		}
		if head.Mode != storeroot.RetentionModeManual || head.WatermarkSeq != 0 {
			t.Errorf("retention head 读回不一致：mode=%q watermark=%d", head.Mode, head.WatermarkSeq)
		}
	})

	t.Run("结构事件", func(t *testing.T) {
		events, err := repository.layout.readEvents(key, 0, 0)
		if err != nil {
			t.Fatalf("readEvents: %v", err)
		}
		if len(events) != 1 {
			t.Fatalf("结构事件 = %d，want 1", len(events))
		}
		event := events[0]
		if event.Kind != structuralEventFork || event.CommitID != coldReadEventCommitID {
			t.Errorf("结构事件读回不一致：kind=%q commit=%q", event.Kind, event.CommitID)
		}
		if got := coldReadCompactJSON(t, event.Payload); got != coldReadEventPayload {
			t.Errorf("结构事件 payload = %s，want %s", got, coldReadEventPayload)
		}
	})
}

// TestColdReadFixtureRegenerate 重建 testdata 里的冷读 fixture。
//
// 只在显式设置 SEELEX_COLDREAD_REGENERATE=1 时运行：日常回归读**已提交**的
// 数据；重建是本文件唯一允许写 testdata 的路径。
func TestColdReadFixtureRegenerate(t *testing.T) {
	if os.Getenv(coldReadRegenerateEnv) != "1" {
		t.Skipf("仅在 %s=1 时重建冷读 fixture", coldReadRegenerateEnv)
	}
	packageDir := storePackageDir(t)
	fixtureRoot := filepath.Join(packageDir, coldReadFixtureDir)
	if filepath.Base(fixtureRoot) != "coldread" || filepath.Base(filepath.Dir(fixtureRoot)) != "testdata" {
		t.Fatalf("拒绝重建：目标目录不是 <package>/testdata/coldread（got %s）", fixtureRoot)
	}

	staging := t.TempDir()
	seedColdReadSession(t, staging)

	// 只搬 project-<hash>：数据根锁 lock.owner 是使用者的事实，不属于 fixture。
	if err := os.RemoveAll(fixtureRoot); err != nil {
		t.Fatalf("清理旧 fixture：%v", err)
	}
	if err := os.MkdirAll(fixtureRoot, 0o700); err != nil {
		t.Fatalf("创建 fixture 根：%v", err)
	}
	source := filepath.Join(staging, coldReadProjectDirName())
	coldReadCopyTree(t, source, filepath.Join(fixtureRoot, coldReadProjectDirName()))
	t.Logf("冷读 fixture 已重建：%s", fixtureRoot)
}

// seedColdReadSession 用当前写入口生成一份覆盖全部通道的代表性会话。
//
// 它是重建用例与（可选）未来特征用例的唯一写入来源：探针写入口越多份，
// 越容易在拆分后出现"两份写入语义"。
func seedColdReadSession(t *testing.T, root string) {
	t.Helper()
	repository, err := newJSONRepositoryWithLayout(root, storeroot.Settings{})
	if err != nil {
		t.Fatalf("seed 数据根：%v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })

	key := coldReadKey()
	ctx := context.Background()

	// 1) 一次提交写三条 message 行、一个工具结果与两条压缩记录。
	if err := repository.WriteCommit(ctx, key, Commit{
		Events: []Event{
			{MessageID: "m0", Role: "user", Kind: EventKindUserInput, Content: coldReadUserText, CreatedAt: coldReadCreatedAt},
			{MessageID: "m1", Role: "assistant", Kind: EventKindLLM, Content: coldReadReplyText,
				ToolCalls: []EventToolCall{{
					ID: coldReadToolCallID, Name: coldReadToolName, Arguments: coldReadToolArguments,
				}},
				CreatedAt: coldReadCreatedAt.Add(time.Second)},
			{MessageID: "m2", Role: "tool", Kind: EventKindToolOutput, Content: coldReadToolText,
				ToolCallID: coldReadToolCallID, Name: coldReadToolName, ResultRef: coldReadToolRef,
				CreatedAt: coldReadCreatedAt.Add(2 * time.Second)},
		},
		ToolResults: []ToolResult{{
			Ref:       coldReadToolRef,
			Tool:      coldReadToolName,
			Content:   coldReadToolBody,
			Size:      len(coldReadToolBody),
			CreatedAt: coldReadCreatedAt,
		}},
		CompactionRecords: []json.RawMessage{
			json.RawMessage(coldReadCompactionBody1),
			json.RawMessage(coldReadCompactionBody2),
		},
	}); err != nil {
		t.Fatalf("写入提交：%v", err)
	}

	// 2) 结构事件（event 通道）。
	if _, err := repository.layout.structuralEventCommit(key, coldReadEventCommitID, []structuralEvent{{
		EventID:   1,
		Kind:      structuralEventFork,
		CommitID:  coldReadEventCommitID,
		Payload:   json.RawMessage(coldReadEventPayload),
		CreatedAt: coldReadCreatedAt,
	}}); err != nil {
		t.Fatalf("写入结构事件：%v", err)
	}

	// 3) 压缩帧（只能覆盖已发布行：message head 的 LastSeq = 2）。
	if _, err := repository.layout.compactCommit(key, compactFrameRecord{
		FrameID:        coldReadCompactFrameID,
		MessageFrom:    "m0",
		MessageTo:      "m2",
		MessageFromSeq: 0,
		MessageToSeq:   2,
		Summary:        coldReadCompactSummary,
		BoundaryStatus: CompactBoundaryComplete,
		CommitID:       coldReadCompactCommitID,
		CompressedAt:   coldReadCreatedAt,
	}); err != nil {
		t.Fatalf("写入压缩帧：%v", err)
	}

	// 4) 草稿与队列（lifecycle 通道）。
	if err := repository.layout.saveDraft(key, coldReadDraftText); err != nil {
		t.Fatalf("写入草稿：%v", err)
	}
	if err := repository.layout.queueEnqueue(key, coldReadQueueRequest, coldReadQueueText); err != nil {
		t.Fatalf("写入队列：%v", err)
	}

	// 5) 媒体（meta/ 内容寻址分区）。
	if _, err := repository.layout.artifacts.WriteMedia(layoutScope(key), storeartifact.MediaItem{
		Name:     coldReadMediaName,
		MimeType: coldReadMediaMimeType,
		Kind:     coldReadMediaKind,
		Data:     []byte(coldReadMediaBody),
	}); err != nil {
		t.Fatalf("写入媒体：%v", err)
	}

	// 6) retention 水位：读即写，缺失时补建默认 head，fixture 里才有 metadata/retention.json。
	if _, err := repository.layout.readRetentionHead(key); err != nil {
		t.Fatalf("补建 retention head：%v", err)
	}
}

// coldReadCopyTree 把 fixture 复制到目标目录（保留相对路径，不保留时间戳）。
func coldReadCopyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !entry.Type().IsRegular() {
			return &os.PathError{Op: "copy", Path: path, Err: os.ErrInvalid}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
	if err != nil {
		t.Fatalf("复制冷读 fixture %s → %s：%v", src, dst, err)
	}
}

// coldReadCompactJSON 归一化 JSON 文本，用于比较存储层原样保留的 payload。
func coldReadCompactJSON(t *testing.T, raw []byte) string {
	t.Helper()
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, raw); err != nil {
		t.Fatalf("压缩 payload：%v", err)
	}
	return compacted.String()
}
