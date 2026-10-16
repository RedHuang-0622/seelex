package multimodal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultMaxImageBytes 是单张图片在 wire 层的上限（默认 5 MB）。
//
// 刻意低于会话媒体分区的落盘上限：落盘上限管磁盘配额，wire 上限管
// 「provider 收不收得下」——data URL 还要再涨 33%，两者不是同一把尺子。
const DefaultMaxImageBytes = 5 << 20

// ErrImageTooLarge 表示图片超过 wire 上限，应在调用前降采样。
var ErrImageTooLarge = errors.New("multimodal: image exceeds wire limit")

// Config 是一次多模态请求所需的连接与预算参数（不含日志用途，禁止打印 APIKey）。
type Config struct {
	BaseURL     string
	APIKey      string
	Model       string
	MaxTokens   int
	Temperature float64
	Timeout     time.Duration
	// MaxImageBytes 为 0 时取 DefaultMaxImageBytes。
	MaxImageBytes int
	// HTTPClient 为 nil 时按 Timeout 新建。
	HTTPClient *http.Client
}

// Answer 是一次多模态请求的可观察结果。
type Answer struct {
	Text             string
	Reasoning        string
	FinishReason     string
	Model            string
	PromptTokens     int
	CompletionTokens int
	ImageCount       int
	RequestBytes     int
}

// FinalText 返回可用于断言的完整回答：部分 provider（如 DeepSeek 思考模式）
// 会把内容放在 reasoning_content，而 message.content 为空——只读 content
// 会把「模型其实答了」误判成「模型没看到图」。
func (answer Answer) FinalText() string {
	text := strings.TrimSpace(answer.Text)
	if text != "" {
		return text
	}
	return strings.TrimSpace(answer.Reasoning)
}

type completionResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// endpoint 归一化 chat/completions 地址（兼容带或不带 /v1 的 base_url）。
func (config Config) endpoint() (string, error) {
	base := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if base == "" {
		return "", errors.New("multimodal: base URL is required")
	}
	if strings.HasSuffix(base, "/chat/completions") {
		return base, nil
	}
	return base + "/chat/completions", nil
}

// CompleteWithImages 发起一次带图同步请求并返回模型回答。
//
// 失败语义：网络错误、非 2xx、响应缺少 choices 都显式报错，绝不把空回答
// 当作“模型没看到图”的证据——那正是多模态链路最容易误判的地方。
func CompleteWithImages(ctx context.Context, config Config, prompt string, images []ImageSource) (Answer, error) {
	endpoint, err := config.endpoint()
	if err != nil {
		return Answer{}, err
	}
	if strings.TrimSpace(config.Model) == "" {
		return Answer{}, errors.New("multimodal: model is required")
	}
	if strings.TrimSpace(config.APIKey) == "" {
		return Answer{}, errors.New("multimodal: api key is required")
	}
	maxImageBytes := config.MaxImageBytes
	if maxImageBytes <= 0 {
		maxImageBytes = DefaultMaxImageBytes
	}
	for index, image := range images {
		if len(image.Data) > maxImageBytes {
			return Answer{}, fmt.Errorf("%w: image %d is %d bytes > %d", ErrImageTooLarge, index, len(image.Data), maxImageBytes)
		}
	}
	message, err := BuildMessage(prompt, images)
	if err != nil {
		return Answer{}, err
	}
	payload, err := json.Marshal(RequestBody(config.Model, message, config.MaxTokens, config.Temperature))
	if err != nil {
		return Answer{}, err
	}
	client := config.HTTPClient
	if client == nil {
		timeout := config.Timeout
		if timeout <= 0 {
			timeout = 120 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return Answer{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+config.APIKey)
	response, err := client.Do(request)
	if err != nil {
		return Answer{}, fmt.Errorf("multimodal: POST %s: %w", endpoint, err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return Answer{}, fmt.Errorf("multimodal: read response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Answer{}, fmt.Errorf("multimodal: %s rejected the request (%s): %s",
			endpoint, response.Status, errorSnippet(body))
	}
	var decoded completionResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return Answer{}, fmt.Errorf("multimodal: decode response: %w", err)
	}
	if decoded.Error != nil && decoded.Error.Message != "" {
		return Answer{}, fmt.Errorf("multimodal: provider error: %s", decoded.Error.Message)
	}
	if len(decoded.Choices) == 0 {
		return Answer{}, fmt.Errorf("multimodal: response carries no choices: %s", errorSnippet(body))
	}
	answer := Answer{
		Text:         decoded.Choices[0].Message.Content,
		Reasoning:    decoded.Choices[0].Message.ReasoningContent,
		FinishReason: decoded.Choices[0].FinishReason,
		Model:        decoded.Model,
		ImageCount:   len(images),
		RequestBytes: len(payload),
	}
	if decoded.Usage != nil {
		answer.PromptTokens = decoded.Usage.PromptTokens
		answer.CompletionTokens = decoded.Usage.CompletionTokens
	}
	return answer, nil
}

// errorSnippet 截断错误正文用于诊断；响应正文可能很长，但绝不含请求凭据。
func errorSnippet(body []byte) string {
	text := strings.Join(strings.Fields(string(body)), " ")
	if len(text) > 400 {
		text = text[:400] + "…"
	}
	return text
}
