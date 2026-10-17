package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// defaultTavilyEndpoint 是标准 websearch 协议的默认端点，旧字段未配置
// endpoint 时沿用（Tavily Search API）。
const defaultTavilyEndpoint = "https://api.tavily.com/search"

// DefaultTimeout 是所有搜索策略的默认 HTTP 超时。由 seele.yaml limits 段的
// search_timeout（兼容旧名 tavily_timeout）经 ApplyLimits 注入；默认 15 秒。
var DefaultTimeout = 15 * time.Second

// ApplyLimits 注入 seele.yaml limits 段中 search 相关的配置。
func ApplyLimits(searchTimeoutSec int) {
	if searchTimeoutSec > 0 {
		DefaultTimeout = time.Duration(searchTimeoutSec) * time.Second
	}
}

// providerTimeout 返回策略超时：配置为 0 时使用全局 DefaultTimeout。
func providerTimeout(timeoutSeconds int) time.Duration {
	if timeoutSeconds > 0 {
		return time.Duration(timeoutSeconds) * time.Second
	}
	return DefaultTimeout
}

// standardStrategy 是标准 websearch 协议策略：POST JSON + Bearer 鉴权，
// 响应按 Tavily 兼容格式解析（results 数组 + 可选 answer）。端点可配置，
// 指向自建代理或任何兼容该协议的搜索 API，工具本身不绑定具体引擎。
type standardStrategy struct {
	name     string
	endpoint string
	apiKey   string
	opts     WebSearchConfig
	client   *http.Client
}

// newStandardStrategy 构建标准协议策略；未配置 API Key 或 endpoint 时报错。
func newStandardStrategy(cfg StrategyConfig, opts WebSearchConfig) (Strategy, error) {
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		name = "websearch"
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("web_search: API Key 未配置")
	}
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		// 仅旧字段兼容：provider: tavily 未声明 endpoint 时沿用默认端点。
		if strings.EqualFold(name, "tavily") {
			endpoint = defaultTavilyEndpoint
		} else {
			return nil, fmt.Errorf("search: 代理策略 %q 缺少 endpoint", name)
		}
	}
	return &standardStrategy{
		name:     name,
		endpoint: endpoint,
		apiKey:   cfg.APIKey,
		opts:     opts,
		client:   &http.Client{Timeout: providerTimeout(opts.TimeoutSeconds)},
	}, nil
}

// Name 返回策略名称。
func (p *standardStrategy) Name() string { return p.name }

// Search 按标准协议调用搜索端点并归一化响应；继承 caller context，
// context 取消能中断请求。
func (p *standardStrategy) Search(ctx context.Context, query string, maxResults int) (SearchResponse, error) {
	reqBody := map[string]any{
		"query":          query,
		"search_depth":   p.opts.SearchDepth,
		"max_results":    limitResults(p.opts.MaxResults, maxResults),
		"include_answer": p.opts.IncludeAnswer,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return SearchResponse{}, fmt.Errorf("web_search: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return SearchResponse{}, fmt.Errorf("web_search: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return SearchResponse{}, fmt.Errorf("web_search: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return SearchResponse{}, fmt.Errorf("web_search: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return SearchResponse{}, fmt.Errorf("web_search: API error %d: %s", resp.StatusCode, string(respBytes))
	}

	var tr tavilyResponse
	if err := json.Unmarshal(respBytes, &tr); err != nil {
		return SearchResponse{}, fmt.Errorf("web_search: parse response: %w", err)
	}
	out := SearchResponse{Answer: tr.Answer}
	for _, r := range tr.Results {
		out.Items = append(out.Items, SearchItem{Title: r.Title, URL: r.URL, Content: r.Content, Score: r.Score})
	}
	return out, nil
}

// tavilyResponse / tavilyResult 是标准协议的响应结构（Tavily 兼容）。
type tavilyResponse struct {
	Answer  string         `json:"answer"`
	Results []tavilyResult `json:"results"`
}

type tavilyResult struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}
