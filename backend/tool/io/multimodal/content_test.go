package multimodal

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testImage() ImageSource {
	return ImageSource{
		Ref:      "media:" + strings.Repeat("a", 64),
		Name:     "shot-001.png",
		MimeType: "image/png",
		Data:     []byte{0x89, 'P', 'N', 'G', 0x00, 0xFF},
	}
}

// TestBuildPartsShapes 锁定 wire 形状：文本在前、图片为 image_url data URL。
func TestBuildPartsShapes(t *testing.T) {
	cases := []struct {
		name       string
		text       string
		images     []ImageSource
		wantParts  int
		wantErr    error
		wantPrefix string
	}{
		{name: "text only", text: "只看不动", wantParts: 1},
		{name: "text and image", text: "这是什么颜色", images: []ImageSource{testImage()}, wantParts: 2, wantPrefix: "data:image/png;base64,"},
		{name: "image only", images: []ImageSource{testImage()}, wantParts: 1, wantPrefix: "data:image/png;base64,"},
		{
			name: "image without mime", images: []ImageSource{{Ref: testImage().Ref, Data: []byte{1}}},
			wantErr: ErrImageInvalid,
		},
		{
			name: "image without bytes", images: []ImageSource{{MimeType: "image/png"}},
			wantErr: ErrImageInvalid,
		},
		{
			name: "non media ref", images: []ImageSource{{Ref: "blob:abc", MimeType: "image/png", Data: []byte{1}}},
			wantErr: errors.New("not a session media ref"),
		},
		{name: "empty message", wantErr: errors.New("requires text or at least one image")},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			parts, err := BuildParts(testCase.text, testCase.images)
			if testCase.wantErr != nil {
				if err == nil || !strings.Contains(err.Error(), testCase.wantErr.Error()) {
					t.Fatalf("err = %v, want containing %q", err, testCase.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("BuildParts: %v", err)
			}
			if len(parts) != testCase.wantParts {
				t.Fatalf("parts = %d, want %d (%+v)", len(parts), testCase.wantParts, parts)
			}
			if testCase.wantPrefix != "" {
				last := parts[len(parts)-1]
				if last.Type != "image_url" || last.ImageURL == nil || !strings.HasPrefix(last.ImageURL.URL, testCase.wantPrefix) {
					t.Fatalf("image part = %+v", last)
				}
				wantPayload := base64.StdEncoding.EncodeToString(testImage().Data)
				if !strings.HasSuffix(last.ImageURL.URL, wantPayload) {
					t.Fatalf("data url payload = %q, want suffix %q", last.ImageURL.URL, wantPayload)
				}
			}
		})
	}
}

// TestAnswerFinalTextFallsBackToReasoning 锁定真机踩到的差异：思考模式下
// message.content 可能为空、内容落在 reasoning_content，只读 content 会把
// 「模型答了」误判成「模型没看到图」。
func TestAnswerFinalTextFallsBackToReasoning(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		wantText    string
		wantFinal   string
		wantFinish  string
		wantReasons string
	}{
		{
			name:       "content present",
			body:       `{"model":"m","choices":[{"message":{"content":"red blue","reasoning_content":"思考"},"finish_reason":"stop"}]}`,
			wantText:   "red blue",
			wantFinal:  "red blue",
			wantFinish: "stop",
		},
		{
			name:      "content empty, reasoning carries the answer",
			body:      `{"model":"m","choices":[{"message":{"content":"","reasoning_content":"左半红色，右半蓝色"},"finish_reason":"length"}]}`,
			wantFinal: "左半红色，右半蓝色",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = writer.Write([]byte(testCase.body))
			}))
			defer server.Close()
			answer, err := CompleteWithImages(context.Background(), Config{
				BaseURL: server.URL, APIKey: "k", Model: "m",
			}, "hi", nil)
			if err != nil {
				t.Fatalf("CompleteWithImages: %v", err)
			}
			if answer.Text != testCase.wantText || answer.FinalText() != testCase.wantFinal {
				t.Fatalf("text = %q final = %q, want %q / %q",
					answer.Text, answer.FinalText(), testCase.wantText, testCase.wantFinal)
			}
			if testCase.wantFinish != "" && answer.FinishReason != testCase.wantFinish {
				t.Fatalf("finish = %q, want %q", answer.FinishReason, testCase.wantFinish)
			}
		})
	}
}

// TestConfigEndpoint 覆盖 base_url 归一化（带 /v1、带尾斜杠、已含路径）。
func TestConfigEndpoint(t *testing.T) {
	cases := []struct {
		base string
		want string
	}{
		{base: "https://api.deepseek.com", want: "https://api.deepseek.com/chat/completions"},
		{base: "https://api.openai.com/v1", want: "https://api.openai.com/v1/chat/completions"},
		{base: "https://gw.example/v1/", want: "https://gw.example/v1/chat/completions"},
		{base: "https://gw.example/v1/chat/completions", want: "https://gw.example/v1/chat/completions"},
	}
	for _, testCase := range cases {
		got, err := Config{BaseURL: testCase.base}.endpoint()
		if err != nil || got != testCase.want {
			t.Fatalf("endpoint(%q) = %q err=%v, want %q", testCase.base, got, err, testCase.want)
		}
	}
	if _, err := (Config{}).endpoint(); err == nil {
		t.Fatal("empty base URL must fail")
	}
}

// TestCompleteWithImagesPostsParts 用假服务端校验请求体、鉴权头与响应解析。
func TestCompleteWithImagesPostsParts(t *testing.T) {
	var captured map[string]any
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		authHeader = request.Header.Get("Authorization")
		if err := json.NewDecoder(request.Body).Decode(&captured); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request.URL.Path != "/chat/completions" {
			t.Errorf("path = %q", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"model":"fake-vision","choices":[{"message":{"content":"red blue"}}],` +
			`"usage":{"prompt_tokens":42,"completion_tokens":3}}`))
	}))
	defer server.Close()

	answer, err := CompleteWithImages(context.Background(), Config{
		BaseURL: server.URL, APIKey: "test-key", Model: "fake-vision",
		MaxTokens: 32, Timeout: 5 * time.Second,
	}, "只回答颜色", []ImageSource{testImage()})
	if err != nil {
		t.Fatalf("CompleteWithImages: %v", err)
	}
	if answer.Text != "red blue" || answer.Model != "fake-vision" ||
		answer.PromptTokens != 42 || answer.CompletionTokens != 3 || answer.ImageCount != 1 {
		t.Fatalf("answer = %+v", answer)
	}
	if authHeader != "Bearer test-key" {
		t.Fatalf("authorization = %q", authHeader)
	}
	messages, ok := captured["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("messages = %+v", captured["messages"])
	}
	content, ok := messages[0].(map[string]any)["content"].([]any)
	if !ok || len(content) != 2 {
		t.Fatalf("content parts = %+v", messages[0].(map[string]any)["content"])
	}
	if content[0].(map[string]any)["type"] != "text" || content[1].(map[string]any)["type"] != "image_url" {
		t.Fatalf("content part types = %+v", content)
	}
	if captured["model"] != "fake-vision" || captured["max_tokens"] != float64(32) {
		t.Fatalf("request body = %+v", captured)
	}
}

// TestCompleteWithImagesFailsExplicitly 覆盖三类失败必须显式报错，不得返回空回答。
func TestCompleteWithImagesFailsExplicitly(t *testing.T) {
	cases := []struct {
		name    string
		config  Config
		images  []ImageSource
		handler http.HandlerFunc
		want    string
	}{
		{
			name: "missing model", config: Config{BaseURL: "https://example.invalid", APIKey: "k"},
			want: "model is required",
		},
		{
			name: "missing key", config: Config{BaseURL: "https://example.invalid", Model: "m"},
			want: "api key is required",
		},
		{
			name:   "image over wire limit",
			config: Config{BaseURL: "https://example.invalid", APIKey: "k", Model: "m", MaxImageBytes: 2},
			images: []ImageSource{testImage()},
			want:   ErrImageTooLarge.Error(),
		},
		{
			name:   "http 400",
			config: Config{BaseURL: "", APIKey: "k", Model: "m"},
			handler: func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(http.StatusBadRequest)
				_, _ = writer.Write([]byte(`{"error":{"message":"content must be a string"}}`))
			},
			want: "rejected the request",
		},
		{
			name:   "no choices",
			config: Config{BaseURL: "", APIKey: "k", Model: "m"},
			handler: func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = writer.Write([]byte(`{"model":"m"}`))
			},
			want: "no choices",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			config := testCase.config
			if testCase.handler != nil {
				server := httptest.NewServer(testCase.handler)
				defer server.Close()
				config.BaseURL = server.URL
			}
			_, err := CompleteWithImages(context.Background(), config, "hi", testCase.images)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("err = %v, want containing %q", err, testCase.want)
			}
		})
	}
}
