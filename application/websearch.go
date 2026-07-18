package application

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// WebSearchConfig holds web search tool configuration.
type WebSearchConfig struct {
	Provider      string
	APIKey        string
	MaxResults    int
	IncludeAnswer bool
	SearchDepth   string
}

// WebSearch 调用 Tavily Search API 搜索互联网。
// 返回格式化的 Markdown 搜索结果。
func WebSearch(ctx context.Context, cfg WebSearchConfig, query string, maxResults int) (string, error) {
	if cfg.APIKey == "" {
		return "", fmt.Errorf("web_search: API Key 未配置")
	}
	if maxResults <= 0 || maxResults > 10 {
		maxResults = cfg.MaxResults
	}

	reqBody := map[string]any{
		"query":          query,
		"search_depth":   cfg.SearchDepth,
		"max_results":    maxResults,
		"include_answer": cfg.IncludeAnswer,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("web_search: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.tavily.com/search", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("web_search: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("web_search: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("web_search: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("web_search: API error %d: %s", resp.StatusCode, string(respBytes))
	}

	var tr tavilyResponse
	if err := json.Unmarshal(respBytes, &tr); err != nil {
		return "", fmt.Errorf("web_search: parse response: %w", err)
	}
	return formatTavilyResult(tr), nil
}

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

func formatTavilyResult(resp tavilyResponse) string {
	var buf bytes.Buffer
	if resp.Answer != "" {
		buf.WriteString("## AI 摘要\n\n")
		buf.WriteString(resp.Answer)
		buf.WriteString("\n\n")
	}
	if len(resp.Results) > 0 {
		buf.WriteString("## 搜索结果\n\n")
		for i, r := range resp.Results {
			fmt.Fprintf(&buf, "%d. **%s**\n", i+1, r.Title)
			fmt.Fprintf(&buf, "   URL: %s\n", r.URL)
			if r.Content != "" {
				fmt.Fprintf(&buf, "   %s\n", r.Content)
			}
			buf.WriteString("\n")
		}
	}
	return buf.String()
}
