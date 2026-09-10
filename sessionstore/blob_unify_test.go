package sessionstore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestToolResultLivesInBigToolResultChannel 对应 S13（§2.0 规则 6、§2.6、D9）：
// ToolResult 记录与超大输出同走 big_tool_result 单通道；旧 tool-results/ 与
// metadata/toolresult.json 不再产生，refs 索引是可重建派生物
// （metadata-index/toolresult.json）。
func TestToolResultLivesInBigToolResultChannel(t *testing.T) {
	repository, err := Open(context.Background(), Config{Backend: BackendJSON, Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	key := Key{ProjectID: "p-s13", SessionID: "s-s13"}
	content := "归档原文-" + time.Now().UTC().String()
	if err := repository.WriteCommit(context.Background(), key, Commit{
		Events: []Event{{Role: "user", Content: "hi", MessageID: "m1"}},
		ToolResults: []ToolResult{{
			Ref: CompressedTurnRefPrefix + "seg-1", Tool: "compact_frame",
			Content: content, Digest: "d1", Size: len(content), TokenCount: 1,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	sessionRoot := repository.(*jsonRepository).sessionDir(key)
	if _, err := os.Stat(filepath.Join(sessionRoot, "metadata", "toolresult.json")); err == nil {
		t.Fatal("metadata/toolresult.json 仍存在（S13 退役项）")
	}
	if _, err := os.Stat(filepath.Join(sessionRoot, "tool-results")); err == nil {
		t.Fatal("tool-results/ 目录仍存在（S13 退役项）")
	}
	if _, err := os.Stat(filepath.Join(sessionRoot, "metadata-index", "toolresult.json")); err != nil {
		t.Fatalf("可重建 refs 索引缺失: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(sessionRoot, "big_tool_result"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("big_tool_result entries = %+v err=%v, want 1 个结果文件", entries, err)
	}
	if filepath.Ext(entries[0].Name()) != ".json" {
		t.Fatalf("结果文件后缀 = %q, want .result.json（避开 blob GC 的 .jsonl 枚举）", entries[0].Name())
	}

	// 读回语义不变：ReadToolResult / ListToolResults 都经索引读到该结果。
	got, err := repository.ReadToolResult(context.Background(), key, CompressedTurnRefPrefix+"seg-1")
	if err != nil || got.Content != content {
		t.Fatalf("ReadToolResult = %q err=%v", got.Content, err)
	}
	listed, err := repository.ListToolResults(context.Background(), key)
	if err != nil || len(listed) != 1 || listed[0].Ref != CompressedTurnRefPrefix+"seg-1" {
		t.Fatalf("ListToolResults = %+v err=%v", listed, err)
	}
}
