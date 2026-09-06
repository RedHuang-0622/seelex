package search

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// 内置厂商适配器测试：不同厂商的专有协议（请求/响应各不相同）最终都归一为
// 统一的 Strategy —— 入参 (ctx, query, maxResults) 与出参 SearchResponse
// 一致，消费方无感知。这里用 httptest 按各厂商的真实响应样例做端到端断言。

func TestAssemble_BuiltinTavilyByType(t *testing.T) {
	// type: tavily 显式选择内置 tavily 适配器：复用标准协议，端点缺省官方。
	p, err := Assemble(WebSearchConfig{
		MaxResults: 5,
		Strategies: []StrategyConfig{
			{Name: "my-tavily", Type: "tavily", APIKey: "sk-test"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "my-tavily" {
		t.Fatalf("expected name 'my-tavily', got %q", p.Name())
	}
	sp, ok := p.(*standardStrategy)
	if !ok {
		t.Fatalf("expected *standardStrategy, got %T", p)
	}
	if sp.endpoint != defaultTavilyEndpoint {
		t.Errorf("expected default tavily endpoint, got %q", sp.endpoint)
	}
}

func TestBochaStrategy_Search(t *testing.T) {
	var gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		gotAuth = r.Header.Get("Authorization")
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 200,
			"msg": null,
			"data": {
				"summary": "博查AI摘要",
				"webPages": {
					"value": [
						{"name": "博查标题1", "url": "https://bocha.example.com/1", "snippet": "摘要内容1"},
						{"name": "博查标题2", "url": "https://bocha.example.com/2", "snippet": "摘要内容2"}
					]
				}
			}
		}`))
	}))
	defer srv.Close()

	cfg := WebSearchConfig{
		MaxResults:    3,
		IncludeAnswer: true,
		Strategies: []StrategyConfig{
			{Name: "bocha", Type: "bochaai", Endpoint: srv.URL, APIKey: "sk-test"},
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
	if resp.Answer != "博查AI摘要" {
		t.Errorf("expected answer, got %q", resp.Answer)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(resp.Items))
	}
	if resp.Items[0].Title != "博查标题1" || resp.Items[0].URL != "https://bocha.example.com/1" || resp.Items[0].Content != "摘要内容1" {
		t.Errorf("unexpected item: %+v", resp.Items[0])
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("expected Bearer auth, got %q", gotAuth)
	}
	if !strings.Contains(gotBody, `"query":"golang"`) || !strings.Contains(gotBody, `"count":3`) || !strings.Contains(gotBody, `"summary":true`) {
		t.Errorf("unexpected bocha body: %s", gotBody)
	}
}

func TestBochaStrategy_IncludeAnswerFalseOmitsSummary(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		_, _ = w.Write([]byte(`{"code":200,"data":{"webPages":{"value":[]}}}`))
	}))
	defer srv.Close()

	cfg := WebSearchConfig{MaxResults: 5, Strategies: []StrategyConfig{{Name: "b", Type: "bochaai", Endpoint: srv.URL, APIKey: "sk"}}}
	p, err := Assemble(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Search(context.Background(), "go", 5); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(gotBody, `"summary":true`) {
		t.Errorf("summary should be omitted when include_answer false, got body %s", gotBody)
	}
}

func TestBochaStrategy_DefaultEndpoint(t *testing.T) {
	p, err := newBochaStrategy(StrategyConfig{Name: "bochaai", APIKey: "sk"}, WebSearchConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if p.(*bochaStrategy).endpoint != defaultBochaEndpoint {
		t.Errorf("expected default bocha endpoint, got %q", p.(*bochaStrategy).endpoint)
	}
}

func TestBochaStrategy_BusinessCodeError(t *testing.T) {
	// 博查可能以 HTTP 200 + code != 200 返回业务错误，应拦截并带 code/msg。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":401,"msg":"bad key","data":null}`))
	}))
	defer srv.Close()

	p, err := newBochaStrategy(StrategyConfig{Name: "b", Endpoint: srv.URL, APIKey: "sk"}, WebSearchConfig{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Search(context.Background(), "go", 5)
	if err == nil || !strings.Contains(err.Error(), "code 401") || !strings.Contains(err.Error(), "bad key") {
		t.Fatalf("expected business code error, got %v", err)
	}
}

func TestSearxngStrategy_Search(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "" {
			t.Errorf("searxng must not send Authorization, got %q", r.Header.Get("Authorization"))
		}
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"answers": ["直接答案甲", "直接答案乙"],
			"results": [
				{"title": "sx-1", "url": "https://sx.example.com/1", "content": "内容1", "score": 0.9},
				{"title": "sx-2", "url": "https://sx.example.com/2", "content": "内容2", "score": 0.8},
				{"title": "sx-3", "url": "https://sx.example.com/3", "content": "内容3", "score": 0.7}
			]
		}`))
	}))
	defer srv.Close()

	// MaxResults=2：SearXNG 返回固定条数，适配器按结果数上限截断到 2。
	cfg := WebSearchConfig{
		MaxResults: 2,
		Strategies: []StrategyConfig{
			{Name: "sx", Type: "searxng", Endpoint: srv.URL},
		},
	}
	p, err := Assemble(cfg)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.Search(context.Background(), "rust lang", 0)
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery.Get("q") != "rust lang" || gotQuery.Get("format") != "json" {
		t.Errorf("unexpected query params: %v", gotQuery)
	}
	if resp.Answer != "直接答案甲\n直接答案乙" {
		t.Errorf("expected joined answers, got %q", resp.Answer)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("expected 2 items (trimmed to max_results), got %d", len(resp.Items))
	}
	if resp.Items[0].Title != "sx-1" || resp.Items[0].Score != 0.9 {
		t.Errorf("unexpected item: %+v", resp.Items[0])
	}
}

func TestSearxngStrategy_MissingEndpoint(t *testing.T) {
	_, err := Assemble(WebSearchConfig{
		Strategies: []StrategyConfig{{Name: "sx", Type: "searxng", APIKey: "k"}},
	})
	if err == nil || !strings.Contains(err.Error(), "缺少 endpoint") {
		t.Fatalf("expected missing endpoint error, got %v", err)
	}
}

func TestAssemble_UnknownBuiltinType(t *testing.T) {
	cfg := WebSearchConfig{
		Strategies: []StrategyConfig{
			{Name: "x", Type: "nosuch-vendor", Endpoint: "https://e", APIKey: "k"},
		},
	}
	_, err := Assemble(cfg)
	if err == nil || !strings.Contains(err.Error(), "不支持的 strategies[].type") || !strings.Contains(err.Error(), "nosuch-vendor") {
		t.Fatalf("expected unknown type error, got %v", err)
	}
}

func TestAssemble_LegacyProviderBocha(t *testing.T) {
	// 旧字段 provider: bochaai + api_key 自动翻译为内置 bocha 适配器，
	// 让现有账号池配置（如仓库 config/accounts.yaml）无需改动即可生效。
	p, err := Assemble(WebSearchConfig{Provider: "bochaai", APIKey: "sk-test", MaxResults: 5})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "bochaai" {
		t.Fatalf("expected name 'bochaai', got %q", p.Name())
	}
	sp, ok := p.(*bochaStrategy)
	if !ok {
		t.Fatalf("expected *bochaStrategy, got %T", p)
	}
	if sp.endpoint != defaultBochaEndpoint {
		t.Errorf("expected default bocha endpoint, got %q", sp.endpoint)
	}
}

func TestAssemble_UnknownProviderListsBuiltins(t *testing.T) {
	// 未知 provider 报错时应提示可用内置厂商，帮助用户修正配置。
	_, err := Assemble(WebSearchConfig{Provider: "google", APIKey: "sk-test"})
	if err == nil || !strings.Contains(err.Error(), "未配置任何 websearch 代理策略") || !strings.Contains(err.Error(), "bochaai") {
		t.Fatalf("expected guidance error listing builtins, got %v", err)
	}
}
