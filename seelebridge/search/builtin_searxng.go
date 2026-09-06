package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// init 注册 "searxng" 内置厂商：SearXNG 自建/公共实例的 JSON API 适配器。
// 请求为 GET {endpoint}?q=...&format=json（无鉴权头），响应 results[] 的
// title/url/content/score 直接映射到统一 SearchResponse；answers[] 归一为
// AI 摘要（换行连接）。SearXNG 返回条数不随请求变化，故按结果数上限截断，
// 保证与其它厂商一致的「最多 max_results 条」出参语义。
func init() {
	registerBuiltin("searxng", newSearxngStrategy)
}

// searxngStrategy 是 SearXNG JSON API 内置适配器。
type searxngStrategy struct {
	name     string
	endpoint string
	opts     WebSearchConfig
	client   *http.Client
}

// newSearxngStrategy 构建 SearXNG 适配器；endpoint 必填（自建实例地址），
// api_key 可选（SearXNG 通常无鉴权，配置了也不发送，保持实例兼容）。
func newSearxngStrategy(cfg StrategyConfig, opts WebSearchConfig) (Strategy, error) {
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		name = "searxng"
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, fmt.Errorf("search: 代理策略 %q 缺少 endpoint（SearXNG 需自建/公共实例地址）", name)
	}
	return &searxngStrategy{
		name:     name,
		endpoint: strings.TrimSpace(cfg.Endpoint),
		opts:     opts,
		client:   &http.Client{Timeout: providerTimeout(opts.TimeoutSeconds)},
	}, nil
}

// Name 返回策略名称。
func (p *searxngStrategy) Name() string { return p.name }

// Search 调用 SearXNG JSON API 并归一化为统一 SearchResponse。
func (p *searxngStrategy) Search(ctx context.Context, query string, maxResults int) (SearchResponse, error) {
	u, err := url.Parse(p.endpoint)
	if err != nil {
		return SearchResponse{}, fmt.Errorf("web_search: parse endpoint: %w", err)
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("format", "json")
	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return SearchResponse{}, fmt.Errorf("web_search: create request: %w", err)
	}

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

	var raw searxngResponse
	if err := json.Unmarshal(respBytes, &raw); err != nil {
		return SearchResponse{}, fmt.Errorf("web_search: parse response: %w", err)
	}

	// answers[] 是引擎聚合的直接答案（字符串），归一为 AI 摘要段。
	out := SearchResponse{Answer: strings.Join(raw.Answers, "\n")}
	n := limitResults(p.opts.MaxResults, maxResults)
	for i, r := range raw.Results {
		if i >= n {
			break
		}
		out.Items = append(out.Items, SearchItem{Title: r.Title, URL: r.URL, Content: r.Content, Score: r.Score})
	}
	return out, nil
}

// searxngResponse 是 SearXNG JSON API 响应的最小契约结构。
type searxngResponse struct {
	Answers []string `json:"answers"`
	Results []struct {
		Title   string  `json:"title"`
		URL     string  `json:"url"`
		Content string  `json:"content"`
		Score   float64 `json:"score"`
	} `json:"results"`
}
