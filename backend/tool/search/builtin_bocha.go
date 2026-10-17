package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// defaultBochaEndpoint 是博查（BochaAI）Web Search API 的官方端点。
//
// 适配依据（Bocha 公开文档）：请求 POST JSON，鉴权 Authorization: Bearer，
// body 为 {query, count, summary, freshness}；响应为 {code, msg, data}，
// data.webPages.value[] 每项含 name（标题）/ url / snippet（摘要），
// summary: true 时 data.summary 返回 AI 摘要。字段按该契约映射到统一
// SearchResponse（title=name, url=url, content=snippet, answer=summary）。
const defaultBochaEndpoint = "https://api.bochaai.com/v1/web-search"

func init() {
	registerBuiltin("bochaai", newBochaStrategy)
}

// bochaStrategy 是博查内置适配器：厂商专有协议在此归一化，出参与其它策略
// （标准协议 / tavily / searxng）完全一致，可被同一 FormatResponse 消费。
type bochaStrategy struct {
	name     string
	endpoint string
	apiKey   string
	opts     WebSearchConfig
	client   *http.Client
}

// newBochaStrategy 构建博查适配器；未声明密钥报错，端点缺省用官方端点。
func newBochaStrategy(cfg StrategyConfig, opts WebSearchConfig) (Strategy, error) {
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		name = "bochaai"
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("web_search: API Key 未配置")
	}
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		endpoint = defaultBochaEndpoint
	}
	return &bochaStrategy{
		name:     name,
		endpoint: endpoint,
		apiKey:   cfg.APIKey,
		opts:     opts,
		client:   &http.Client{Timeout: providerTimeout(opts.TimeoutSeconds)},
	}, nil
}

// Name 返回策略名称。
func (p *bochaStrategy) Name() string { return p.name }

// Search 按博查协议请求并归一化为统一 SearchResponse；context 取消能中断请求。
func (p *bochaStrategy) Search(ctx context.Context, query string, maxResults int) (SearchResponse, error) {
	body := map[string]any{
		"query": query,
		"count": limitResults(p.opts.MaxResults, maxResults),
	}
	// include_answer → summary: true（要求厂商返回 AI 摘要），与标准协议语义对齐。
	if p.opts.IncludeAnswer {
		body["summary"] = true
	}
	bodyBytes, err := json.Marshal(body)
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

	var raw bochaResponse
	if err := json.Unmarshal(respBytes, &raw); err != nil {
		return SearchResponse{}, fmt.Errorf("web_search: parse response: %w", err)
	}
	// 博查业务错误可能以 HTTP 200 + code != 200 形式返回，同样前置拦截。
	if raw.Code != 0 && raw.Code != 200 {
		msg := strings.TrimSpace(raw.Msg)
		if msg == "" {
			msg = "unknown error"
		}
		return SearchResponse{}, fmt.Errorf("web_search: bocha API error code %d: %s", raw.Code, msg)
	}

	out := SearchResponse{Answer: raw.Data.Summary}
	for _, v := range raw.Data.WebPages.Value {
		out.Items = append(out.Items, SearchItem{Title: v.Name, URL: v.URL, Content: v.Snippet})
	}
	return out, nil
}

// bochaResponse 是博查响应的最小契约结构；未使用的字段不解析。
type bochaResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Summary  string `json:"summary"`
		WebPages struct {
			Value []struct {
				Name    string `json:"name"`
				URL     string `json:"url"`
				Snippet string `json:"snippet"`
			} `json:"value"`
		} `json:"webPages"`
	} `json:"data"`
}
