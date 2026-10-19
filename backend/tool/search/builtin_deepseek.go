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

// DeepSeek 官方 Search API 适配器（2026-10-17 上线的 Search & Fetch 之一）。
//
// 与博查/SearXNG 一视同仁：厂商专有协议在本文件内归一化，出参与其它策略完全
// 一致（SearchResponse），切换引擎只改配置里的 type。
//
// 协议事实（api-docs.deepseek.com，2026-10-19 核对）：
//   - POST https://api.deepseek.com/v1/tools/search，Bearer 鉴权，JSON body；
//   - 请求：query（1..128 字符，支持 site:/lang:/after: 等算子）、count（1..50，
//     默认 10）、ori_question（可选，≤128）、localization（可选 IP）、
//     need_content（默认 false；为 false 时 content 恒为空串）；
//   - 响应：{results:[{rerank_score(0..3), title, url, snippet(100~200字),
//     summary(600~800字), content, site_name, site_icon, date, authority(-1..4)}]}；
//   - 计费：每次**成功**请求 $0.75/千次（空结果不计费），QPS 100/账号；
//   - 错误：OpenAI 兼容格式；400/401/402 不可重试，408/429/5xx 可等等再试。
//
// 两处刻意的取舍（不新增配置旋钮）：
//   - **不取正文**：need_content 恒 false。要正文是另一件事（Fetch API），而
//     summary（600~800 字）已经是给模型读的长摘要；取正文会把每次调用变成
//     几十 KB 的载荷，且计费与耗时都换不来更好的召回。
//   - Content 的取值顺序 = summary → snippet：前者是长摘要（模型可读），后者
//     是 100~200 字的 UI 摘要（兜底）。空结果不是错误（未命中就是未命中，
//     且不计费）。
const defaultDeepSeekSearchEndpoint = "https://api.deepseek.com/v1/tools/search"

func init() {
	registerBuiltin("deepseek", newDeepSeekStrategy)
}

// deepSeekStrategy 是 DeepSeek 官方 Search API 的内置适配器。
type deepSeekStrategy struct {
	name     string
	endpoint string
	apiKey   string
	opts     WebSearchConfig
	client   *http.Client
}

// newDeepSeekStrategy 构建适配器；未声明密钥报错，端点缺省用官方端点。
func newDeepSeekStrategy(cfg StrategyConfig, opts WebSearchConfig) (Strategy, error) {
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		name = "deepseek"
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("web_search: API Key 未配置（DeepSeek Search 用同一把 DeepSeek API Key）")
	}
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		endpoint = defaultDeepSeekSearchEndpoint
	}
	return &deepSeekStrategy{
		name:     name,
		endpoint: endpoint,
		apiKey:   cfg.APIKey,
		opts:     opts,
		client:   &http.Client{Timeout: providerTimeout(opts.TimeoutSeconds)},
	}, nil
}

// Name 返回策略名称。
func (p *deepSeekStrategy) Name() string { return p.name }

// Search 按 DeepSeek Search 协议请求并归一化为统一 SearchResponse。
func (p *deepSeekStrategy) Search(ctx context.Context, query string, maxResults int) (SearchResponse, error) {
	body := map[string]any{
		"query": query,
		"count": clampDeepSeekCount(limitResults(p.opts.MaxResults, maxResults)),
		// 固定 false：正文归 Fetch API，这里只要长摘要（见文件头取舍）。
		"need_content": false,
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
		return SearchResponse{}, fmt.Errorf("web_search: API error %d: %s", resp.StatusCode, deepSeekErrorText(respBytes))
	}

	var raw deepSeekSearchResponse
	if err := json.Unmarshal(respBytes, &raw); err != nil {
		return SearchResponse{}, fmt.Errorf("web_search: parse response: %w", err)
	}

	out := SearchResponse{}
	// include_answer 在 DeepSeek 这侧没有"跨结果综合答案"这一格：用 Top-1 的
	// summary 顶上（它就是这一条结果的长摘要），不编造拼接式答案。
	if p.opts.IncludeAnswer && len(raw.Results) > 0 {
		out.Answer = strings.TrimSpace(raw.Results[0].Summary)
	}
	for _, item := range raw.Results {
		out.Items = append(out.Items, SearchItem{
			Title:   item.Title,
			URL:     item.URL,
			Content: firstNonEmpty(item.Summary, item.Snippet),
			Score:   item.RerankScore,
		})
	}
	return out, nil
}

// clampDeepSeekCount 把结果数收敛到协议允许的 1..50（count 只影响每页条数，
// 不影响按次计费）。
func clampDeepSeekCount(count int) int {
	if count < 1 {
		return 1
	}
	if count > 50 {
		return 50
	}
	return count
}

// firstNonEmpty 返回第一个非空白字符串（TrimSpace 后的值）。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// deepSeekErrorText 取可展示的错误文案：优先 OpenAI 兼容的 error.message，
// 拿不到就用响应正文（截断，避免把整页 HTML 塞进错误里）。
func deepSeekErrorText(body []byte) string {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil {
		if message := strings.TrimSpace(envelope.Error.Message); message != "" {
			if code := strings.TrimSpace(envelope.Error.Code); code != "" {
				return message + " (" + code + ")"
			}
			return message
		}
	}
	text := strings.TrimSpace(string(body))
	const maxErrorChars = 512
	if len(text) > maxErrorChars {
		text = text[:maxErrorChars] + "…"
	}
	if text == "" {
		text = "empty response body"
	}
	return text
}

// deepSeekSearchResponse 是 DeepSeek Search 响应的最小契约结构；未使用的字段
// （site_name / site_icon / date / authority / content）不解析。
type deepSeekSearchResponse struct {
	Results []struct {
		RerankScore float64 `json:"rerank_score"`
		Title       string  `json:"title"`
		URL         string  `json:"url"`
		Snippet     string  `json:"snippet"`
		Summary     string  `json:"summary"`
	} `json:"results"`
}
