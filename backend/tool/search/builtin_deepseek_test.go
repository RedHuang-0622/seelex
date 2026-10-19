package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// DeepSeek 官方 Search API 适配器测试。
//
// 协议事实来自 api-docs.deepseek.com（2026-10-19）：POST /v1/tools/search，
// Bearer 鉴权，请求 {query,count,need_content}，响应 {results:[...]}。这里用
// httptest 复刻真实响应样例，钉住三件事：请求形状、字段映射、错误语义。

// newDeepSeekTestStrategy 用 httptest 端点构建适配器（跳过缺省官方端点）。
func newDeepSeekTestStrategy(t *testing.T, endpoint string, opts WebSearchConfig) *deepSeekStrategy {
	t.Helper()
	strategy, err := newDeepSeekStrategy(StrategyConfig{Name: "ds", Endpoint: endpoint, APIKey: "sk-test"}, opts)
	if err != nil {
		t.Fatalf("newDeepSeekStrategy: %v", err)
	}
	concrete, ok := strategy.(*deepSeekStrategy)
	if !ok {
		t.Fatalf("expected *deepSeekStrategy, got %T", strategy)
	}
	return concrete
}

func TestDeepSeekStrategy_Search(t *testing.T) {
	var gotMethod, gotAuth, gotContentType string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"results": [
				{
					"rerank_score": 2.4,
					"title": "DeepSeek V4 model released",
					"url": "https://example.com/deepseek-v4",
					"snippet": "short ui snippet",
					"summary": "long model-readable summary",
					"content": "",
					"site_name": "Example News",
					"site_icon": null,
					"date": "2026-06-01T00:00:00+00:00",
					"authority": 3
				},
				{
					"rerank_score": 0.8,
					"title": "Second result",
					"url": "https://example.com/2",
					"snippet": "only snippet here",
					"summary": "",
					"content": "",
					"site_name": null,
					"site_icon": null,
					"date": null,
					"authority": -1
				}
			]
		}`))
	}))
	defer srv.Close()

	strategy := newDeepSeekTestStrategy(t, srv.URL, WebSearchConfig{MaxResults: 5, IncludeAnswer: true})
	resp, err := strategy.Search(context.Background(), "deepseek v4", 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("expected POST, got %s", gotMethod)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("expected bearer auth, got %q", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Errorf("expected json content type, got %q", gotContentType)
	}
	if gotBody["query"] != "deepseek v4" {
		t.Errorf("expected query passthrough, got %#v", gotBody["query"])
	}
	if gotBody["count"] != float64(3) {
		t.Errorf("expected count=3, got %#v", gotBody["count"])
	}
	// need_content 恒 false：正文归 Fetch API，这里只要长摘要。
	if gotBody["need_content"] != false {
		t.Errorf("expected need_content=false, got %#v", gotBody["need_content"])
	}

	if len(resp.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(resp.Items))
	}
	first := resp.Items[0]
	if first.Title != "DeepSeek V4 model released" || first.URL != "https://example.com/deepseek-v4" {
		t.Errorf("unexpected first item identity: %+v", first)
	}
	// Content 取长摘要（模型可读），不是 UI snippet。
	if first.Content != "long model-readable summary" {
		t.Errorf("expected summary as content, got %q", first.Content)
	}
	if first.Score != 2.4 {
		t.Errorf("expected rerank_score mapped to Score, got %v", first.Score)
	}
	// 没有 summary 时退回 snippet（空内容的结果仍可用）。
	if resp.Items[1].Content != "only snippet here" {
		t.Errorf("expected snippet fallback, got %q", resp.Items[1].Content)
	}
	// Answer 恒空：DeepSeek 没有跨结果综合答案，不把 Top-1 摘要重复印两遍。
	if resp.Answer != "" {
		t.Errorf("expected empty answer (top-1 summary already in content), got %q", resp.Answer)
	}
}

func TestDeepSeekStrategy_EmptyResultsIsNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results": []}`))
	}))
	defer srv.Close()

	strategy := newDeepSeekTestStrategy(t, srv.URL, WebSearchConfig{MaxResults: 5, IncludeAnswer: true})
	resp, err := strategy.Search(context.Background(), "nothing matches this", 5)
	if err != nil {
		t.Fatalf("empty results must not be an error: %v", err)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("expected no items, got %d", len(resp.Items))
	}
	// 没有结果时不该编一个 Answer。
	if resp.Answer != "" {
		t.Fatalf("expected empty answer, got %q", resp.Answer)
	}
}

func TestDeepSeekStrategy_SurfacesErrorEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"'count' must be between 1 and 50","type":"invalid_request_error","param":null,"code":"invalid_request_error"}}`))
	}))
	defer srv.Close()

	strategy := newDeepSeekTestStrategy(t, srv.URL, WebSearchConfig{MaxResults: 5})
	_, err := strategy.Search(context.Background(), "q", 5)
	if err == nil {
		t.Fatal("expected error for HTTP 400")
	}
	message := err.Error()
	if !strings.Contains(message, "400") || !strings.Contains(message, "must be between 1 and 50") {
		t.Fatalf("error should carry status and upstream message, got %q", message)
	}
	if !strings.Contains(message, "invalid_request_error") {
		t.Fatalf("error should carry upstream code, got %q", message)
	}
}

func TestDeepSeekStrategy_MissingAPIKey(t *testing.T) {
	if _, err := newDeepSeekStrategy(StrategyConfig{Name: "ds"}, WebSearchConfig{}); err == nil {
		t.Fatal("expected error when api_key is missing")
	}
}

func TestDeepSeekStrategy_DefaultEndpointAndCountClamp(t *testing.T) {
	strategy, err := newDeepSeekStrategy(StrategyConfig{APIKey: "sk-test"}, WebSearchConfig{})
	if err != nil {
		t.Fatalf("newDeepSeekStrategy: %v", err)
	}
	concrete := strategy.(*deepSeekStrategy)
	if concrete.Name() != "deepseek" {
		t.Errorf("expected default name 'deepseek', got %q", concrete.Name())
	}
	if concrete.endpoint != defaultDeepSeekSearchEndpoint {
		t.Errorf("expected default endpoint, got %q", concrete.endpoint)
	}

	// 协议上限 50：配置写得再大也要钳住。
	if got := clampDeepSeekCount(limitResults(200, 0)); got != 50 {
		t.Errorf("expected count clamped to 50, got %d", got)
	}
	// 下限 1：空/负数不产生非法请求。
	if got := clampDeepSeekCount(0); got != 1 {
		t.Errorf("expected count floor 1, got %d", got)
	}
}

func TestAssemble_DeepSeekByProviderShortcut(t *testing.T) {
	// 旧式简写（provider: deepseek + api_key）也要能选到内置适配器。
	strategy, err := Assemble(WebSearchConfig{Provider: "deepseek", APIKey: "sk-test"})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if _, ok := strategy.(*deepSeekStrategy); !ok {
		t.Fatalf("expected *deepSeekStrategy, got %T", strategy)
	}
	if strategy.Name() != "deepseek" {
		t.Errorf("expected name 'deepseek', got %q", strategy.Name())
	}
}
