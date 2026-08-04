package core

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// testStringPtr 返回字符串指针（测试消息正文）。
func testStringPtr(value string) *string { return &value }

// fakeCommitSession 实现 sessionCommitPort：记录 SaveCommit 调用。
type fakeCommitSession struct {
	commits []sessionstore.Commit
}

func (f *fakeCommitSession) SaveCommit(_ string, commit sessionstore.Commit) error {
	f.commits = append(f.commits, commit)
	return nil
}

// fakeTranscriptSession 实现 sessionTranscriptPort 读侧（LoadToolResultWorkspace）。
type fakeTranscriptSession struct {
	fakeSessions
	results map[string]StoredToolResult
}

func (f *fakeTranscriptSession) LoadTranscriptTailWorkspace(_, _ string, _, _ int) ([]TranscriptEvent, error) {
	return nil, nil
}

func (f *fakeTranscriptSession) LoadToolResultWorkspace(_, _, ref string) (StoredToolResult, error) {
	if result, ok := f.results[ref]; ok {
		return result, nil
	}
	return StoredToolResult{}, errors.New("stored tool result not found")
}

// TestCompressedTurnArchiverPersistsOriginal 写侧：溢出轮次原文序列化后
// 经 SaveCommit 持久化（ToolResults 通道，ref = compressed:<segment_id>）。
func TestCompressedTurnArchiverPersistsOriginal(t *testing.T) {
	store := &fakeCommitSession{}
	archiver := &CompressedTurnArchiver{
		Sessions:          store,
		SessionIDProvider: func() string { return "session-1" },
	}
	messages := []types.Message{
		{Role: "user", Content: testStringPtr("审计上下文压缩")},
		{Role: "assistant", Content: testStringPtr("已读取 controller.go")},
	}
	ref, err := archiver.StoreTurn(context.Background(), "compact-sess-123", messages)
	if err != nil {
		t.Fatal(err)
	}
	if ref != "compressed:compact-sess-123" {
		t.Fatalf("ref = %q, want compressed:compact-sess-123", ref)
	}
	if len(store.commits) != 1 || len(store.commits[0].ToolResults) != 1 {
		t.Fatalf("commits = %+v, want 1 commit with 1 tool result", store.commits)
	}
	result := store.commits[0].ToolResults[0]
	if result.Ref != ref || result.Tool != "compact_frame" {
		t.Fatalf("tool result = %+v, want ref %q tool compact_frame", result, ref)
	}
	var decoded []types.Message
	if err := json.Unmarshal([]byte(result.Content), &decoded); err != nil {
		t.Fatalf("decode stored content: %v", err)
	}
	if len(decoded) != 2 || *decoded[0].Content != "审计上下文压缩" {
		t.Fatalf("decoded messages = %+v", decoded)
	}
}

// TestReadCompressedTurnHandlerReadsOriginal 读侧：read_compressed_turn
// 从持久化原文读回，分页/过滤生效。
func TestReadCompressedTurnHandlerReadsOriginal(t *testing.T) {
	original := []types.Message{
		{Role: "user", Content: testStringPtr("轮0：审计窗口外压缩路径")},
		{Role: "assistant", ToolCalls: []types.ToolCall{{ID: "c1", Function: types.ToolCallFunction{Name: "read_file"}}}},
		{Role: "tool", ToolCallID: "c1", Name: "read_file", Content: testStringPtr("controller.go:365 overflow 选取")},
		{Role: "user", Content: testStringPtr("轮1：验证 CompactFrame 边界")},
		{Role: "assistant", Content: testStringPtr("已断言 From/To 与窗口一致")},
	}
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeTranscriptSession{results: map[string]StoredToolResult{
		"compressed:compact-sess-9": {ToolResultRef: model.ToolResultRef{Ref: "compressed:compact-sess-9"}, Content: string(encoded)},
	}}
	service := newTestService(t, &fakeEngine{sessionID: "session-1"}, withTestSessions(store))

	args, _ := json.Marshal(map[string]interface{}{"segment_id": "compact-sess-9"})
	page, err := service.ReadCompressedTurnHandler(context.Background(), string(args))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[user] 轮0：审计窗口外压缩路径", "[tool read_file] controller.go:365 overflow 选取", "[user] 轮1：验证 CompactFrame 边界"} {
		if !strings.Contains(page, want) {
			t.Fatalf("page missing %q:\n%s", want, page)
		}
	}

	// 过滤：contains 只返回匹配行。
	filterArgs, _ := json.Marshal(map[string]interface{}{"segment_id": "compact-sess-9", "contains": "轮1"})
	filtered, err := service.ReadCompressedTurnHandler(context.Background(), string(filterArgs))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(filtered, "轮0") || !strings.Contains(filtered, "轮1") {
		t.Fatalf("filtered page = %q, want only 轮1 line", filtered)
	}

	// 未知 segment：报错。
	missingArgs, _ := json.Marshal(map[string]interface{}{"segment_id": "compact-missing"})
	if _, err := service.ReadCompressedTurnHandler(context.Background(), string(missingArgs)); err == nil {
		t.Fatal("missing segment must fail")
	}
	// 缺 segment_id：报错。
	if _, err := service.ReadCompressedTurnHandler(context.Background(), `{}`); err == nil {
		t.Fatal("missing segment_id must fail")
	}
}
