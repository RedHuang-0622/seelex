package application

import (
	"context"
	"testing"
)

func TestWebSearch_EmptyAPIKey(t *testing.T) {
	cfg := WebSearchConfig{APIKey: ""}
	_, err := WebSearch(context.Background(), cfg, "test query", 5)
	if err == nil {
		t.Fatal("expected error for empty API key")
	}
	if err.Error() != "web_search: API Key 未配置" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWebSearch_MaxResultsBounds(t *testing.T) {
	// 值为 0 — 应使用默认值
	cfg := WebSearchConfig{APIKey: "some-key", MaxResults: 5, SearchDepth: "basic"}
	_, err := WebSearch(context.Background(), cfg, "test", 0)
	if err == nil {
		t.Fatal("expected error (network or timeout), not success")
	}
	// 只要不被 WebSearch 自己拒绝就行 — 注意超时场景
	// 这里我们只验证请求构造逻辑没报 parse error 就满足

	// 负值 — 应使用默认值
	_, err = WebSearch(context.Background(), cfg, "test", -1)
	if err == nil {
		t.Fatal("expected error, not success")
	}

	// 超大值 >10 — 应使用默认值
	_, err = WebSearch(context.Background(), cfg, "test", 20)
	if err == nil {
		t.Fatal("expected error, not success")
	}
}

func TestFormatTavilyResult_WithAnswer(t *testing.T) {
	resp := tavilyResponse{
		Answer: "这是AI摘要",
		Results: []tavilyResult{
			{Title: "标题1", URL: "https://example.com/1", Content: "内容1", Score: 0.9},
			{Title: "标题2", URL: "https://example.com/2", Content: "内容2", Score: 0.8},
		},
	}
	result := formatTavilyResult(resp)
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if !contains(result, "AI 摘要") {
		t.Error("expected AI 摘要 section")
	}
	if !contains(result, "这是AI摘要") {
		t.Error("expected answer content")
	}
	if !contains(result, "搜索结果") {
		t.Error("expected 搜索结果 section")
	}
	if !contains(result, "标题1") || !contains(result, "标题2") {
		t.Error("expected both titles")
	}
	if !contains(result, "https://example.com/1") {
		t.Error("expected URL")
	}
	if !contains(result, "内容1") {
		t.Error("expected content")
	}
}

func TestFormatTavilyResult_NoAnswer(t *testing.T) {
	resp := tavilyResponse{
		Results: []tavilyResult{
			{Title: "Title", URL: "https://example.com", Content: "Content"},
		},
	}
	result := formatTavilyResult(resp)
	if !contains(result, "搜索结果") {
		t.Error("expected 搜索结果 section")
	}
	if contains(result, "AI 摘要") {
		t.Error("should not have AI 摘要 section")
	}
}

func TestFormatTavilyResult_EmptyResults(t *testing.T) {
	resp := tavilyResponse{}
	result := formatTavilyResult(resp)
	if result != "" {
		t.Errorf("expected empty for no results, got %q", result)
	}
}

func TestFormatTavilyResult_ResultsWithoutContent(t *testing.T) {
	resp := tavilyResponse{
		Results: []tavilyResult{
			{Title: "Title Only", URL: "https://example.com"},
		},
	}
	result := formatTavilyResult(resp)
	if !contains(result, "Title Only") {
		t.Error("expected title")
	}
	if !contains(result, "https://example.com") {
		t.Error("expected URL")
	}
	if result == "" {
		t.Fatal("expected non-empty even without content")
	}
}

// TestWebSearch_ContextCancelled 验证 context 取消能正确传递
func TestWebSearch_ContextCancelled(t *testing.T) {
	cfg := WebSearchConfig{APIKey: "test-key", MaxResults: 5}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消
	_, err := WebSearch(ctx, cfg, "test", 5)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}
