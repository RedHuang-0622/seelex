package search

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func contains(text, substring string) bool { return strings.Contains(text, substring) }

func TestStandardStrategy_Search(t *testing.T) {
	var gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"answer": "这是AI摘要",
			"results": [
				{"title": "标题1", "url": "https://example.com/1", "content": "内容1", "score": 0.9},
				{"title": "标题2", "url": "https://example.com/2", "content": "内容2", "score": 0.8}
			]
		}`))
	}))
	defer srv.Close()

	cfg := WebSearchConfig{
		MaxResults:    5,
		IncludeAnswer: true,
		SearchDepth:   "advanced",
		Strategies: []StrategyConfig{
			{Name: "my-search", Endpoint: srv.URL, APIKey: "sk-test"},
		},
	}
	p, err := Assemble(cfg)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.Search(context.Background(), "golang", 0)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Answer != "这是AI摘要" {
		t.Errorf("expected answer, got %q", resp.Answer)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(resp.Items))
	}
	if resp.Items[0].Title != "标题1" || resp.Items[0].URL != "https://example.com/1" || resp.Items[0].Score != 0.9 {
		t.Errorf("unexpected item: %+v", resp.Items[0])
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("expected Bearer auth, got %q", gotAuth)
	}
	if !contains(gotBody, `"max_results":5`) {
		t.Errorf("expected default max_results 5 in body, got %s", gotBody)
	}
	if !contains(gotBody, `"search_depth":"advanced"`) {
		t.Errorf("expected search_depth advanced in body, got %s", gotBody)
	}
}

func TestStandardStrategy_MaxResultsBounds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()

	cfg := WebSearchConfig{MaxResults: 3, Strategies: []StrategyConfig{{Name: "s", Endpoint: srv.URL, APIKey: "k"}}}
	p, err := Assemble(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// 请求值超限（0/20）时回退配置默认 3，服务端不报错即通过。
	for _, requested := range []int{0, -1, 20} {
		if _, err := p.Search(context.Background(), "test", requested); err != nil {
			t.Fatalf("requested %d: %v", requested, err)
		}
	}
}

func TestStandardStrategy_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()

	cfg := WebSearchConfig{MaxResults: 5, Strategies: []StrategyConfig{{Name: "s", Endpoint: srv.URL, APIKey: "k"}}}
	p, err := Assemble(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Search(context.Background(), "test", 5)
	if err == nil || !contains(err.Error(), "401") {
		t.Fatalf("expected API error, got %v", err)
	}
}

func TestStandardStrategy_ContextCancelled(t *testing.T) {
	cfg := WebSearchConfig{MaxResults: 5, Strategies: []StrategyConfig{{Name: "s", Endpoint: "https://e", APIKey: "k"}}}
	p, err := Assemble(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Search(ctx, "test", 5); err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestFormatResponse_WithAnswer(t *testing.T) {
	resp := SearchResponse{
		Answer: "这是AI摘要",
		Items: []SearchItem{
			{Title: "标题1", URL: "https://example.com/1", Content: "内容1", Score: 0.9},
			{Title: "标题2", URL: "https://example.com/2", Content: "内容2", Score: 0.8},
		},
	}
	result := FormatResponse(resp)
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if !contains(result, "AI 摘要") || !contains(result, "这是AI摘要") {
		t.Error("expected AI 摘要 section")
	}
	if !contains(result, "搜索结果") {
		t.Error("expected 搜索结果 section")
	}
	if !contains(result, "标题1") || !contains(result, "标题2") {
		t.Error("expected both titles")
	}
	if !contains(result, "https://example.com/1") || !contains(result, "内容1") {
		t.Error("expected URL and content")
	}
}

func TestFormatResponse_NoAnswer(t *testing.T) {
	resp := SearchResponse{Items: []SearchItem{{Title: "Title", URL: "https://example.com", Content: "Content"}}}
	result := FormatResponse(resp)
	if !contains(result, "搜索结果") {
		t.Error("expected 搜索结果 section")
	}
	if contains(result, "AI 摘要") {
		t.Error("should not have AI 摘要 section")
	}
}

func TestFormatResponse_EmptyResults(t *testing.T) {
	if result := FormatResponse(SearchResponse{}); result != "" {
		t.Errorf("expected empty for no results, got %q", result)
	}
}

func TestFormatResponse_ResultsWithoutContent(t *testing.T) {
	resp := SearchResponse{Items: []SearchItem{{Title: "Title Only", URL: "https://example.com"}}}
	result := FormatResponse(resp)
	if !contains(result, "Title Only") || !contains(result, "https://example.com") {
		t.Error("expected title and URL")
	}
	if result == "" {
		t.Fatal("expected non-empty even without content")
	}
}
