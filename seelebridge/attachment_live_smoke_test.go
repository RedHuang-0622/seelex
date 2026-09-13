//go:build imagelive

// seelex 附件形状的真实 API 冒烟（opt-in）：图片与文档各发一次，只问一件事——
// 模型是不是**真的因为附件而答对**。
//
// 与 imageattach 冒烟的分工：那条走的是「引擎 + 待随图」的装配链路（截屏 → 接缝），
// 这条走的是**Seele 自己的发射形状**（types.Message.MarshalJSON / Anthropic block），
// 直接把请求打到真实端点上，因此它同时是两个问题的证据：
//
//  1. 我们发射的 OpenAI image_url / file 形状，端点到底怎么对待；
//  2. 端点接受了形状之后，内容有没有真的进模型（形状被接受 ≠ 内容被消费）。
//
// 前提：
//
//	go mod edit -replace "github.com/RedHuang-0622/Seele=G:/Program/go/Seele"
//	$env:SEELEX_SMOKE_ACCOUNTS='G:\Program\go\seelex\config\accounts.yaml'
//	go test -tags imagelive ./seelebridge/ -run TestAttachmentShapeLiveSmoke -count=1 -v
//	联调后复原：go mod edit -dropreplace github.com/RedHuang-0622/Seele
//
// 可选：
//
//	SEELEX_SMOKE_ANTHROPIC_BASE  覆盖 Anthropic 兼容端点（默认 <base_url>/anthropic）
//	SEELEX_SMOKE_DOC_STRICT=1    要求文档附件必须被模型读出（本机端点做不到，默认只告警）
package seelebridge

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/seelebridge/attachment"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/config"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

// attachmentNonce 是文档的地面真值：只有内容真的进了模型，才可能出现这串字符。
const attachmentNonce = "SEELEX-DOC-7731"

func TestAttachmentShapeLiveSmoke(t *testing.T) {
	accountsPath := strings.TrimSpace(os.Getenv("SEELEX_SMOKE_ACCOUNTS"))
	if accountsPath == "" {
		t.Skip("设置 SEELEX_SMOKE_ACCOUNTS 指向 accounts.yaml 才能运行真实 API 附件冒烟")
	}
	loaded, err := config.LoadTolerant(accountsPath)
	if err != nil {
		t.Fatalf("加载账号配置: %v", err)
	}
	if len(loaded.Specs) == 0 {
		t.Fatal("账号配置里没有可用账号")
	}
	spec := loaded.Specs[0]
	t.Logf("target: provider=%s baseURL=%s model=%s", spec.Provider, spec.BaseURL, spec.Model)

	pngBytes, err := solidPNG(200, 120, color.RGBA{R: 220, G: 20, B: 60, A: 255})
	if err != nil {
		t.Fatalf("合成图片: %v", err)
	}

	t.Run("图片附件(OpenAI image_url)", func(t *testing.T) {
		message := types.Message{Role: "user"}.
			WithText("What color is this image? Reply with the color name only.").
			WithFiles(types.FilePart{
				Kind: types.FileKindImage, MimeType: "image/png",
				Data: pngBytes, Width: 200, Height: 120, Name: "shot.png",
			})
		status, reply, finish, errBody, err := postChatCompletions(spec, []types.Message{message}, 512)
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		t.Logf("HTTP %d finish=%s 回答=%q", status, finish, reply)
		if status != http.StatusOK {
			t.Fatalf("图片附件被端点拒绝：HTTP %d %s", status, strings.TrimSpace(errBody))
		}
		lower := strings.ToLower(reply)
		if !strings.Contains(lower, "red") && !strings.Contains(lower, "crimson") {
			t.Fatalf("模型没读出图片颜色（像素没进模型？）：%q", reply)
		}
	})

	// 文档：两种官方形状各发一次，把端点的裁决记下来。判据只有一条——
	// 200 却不含编号 = 静默忽略（形状被接受、内容没进模型），这是最危险的情况。
	t.Run("文档附件(形状接受 ≠ 内容消费)", func(t *testing.T) {
		docData := []byte("INTERNAL MEMO: attachment code is " + attachmentNonce + ", owner Zhang San.")
		const question = "Read the attachment and reply with the attachment code only."
		strict := strings.TrimSpace(os.Getenv("SEELEX_SMOKE_DOC_STRICT")) == "1"

		// A. OpenAI Chat Completions 的 file part：官方文档形状，兼容端点可能直接 400。
		openAIMessage := types.Message{Role: "user"}.WithText(question).WithFiles(types.FilePart{
			Kind: types.FileKindDocument, MimeType: "text/plain", Data: docData, Name: "memo.txt",
		})
		status, reply, finish, errBody, err := postChatCompletions(spec, []types.Message{openAIMessage}, 512)
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		switch {
		case status == http.StatusOK && strings.Contains(reply, attachmentNonce):
			t.Logf("✅ OpenAI file 形状：端点接受文档，且模型读出了编号")
		case status >= 400:
			t.Logf("⛔ OpenAI file 形状：端点明确拒绝（HTTP %d %s）——本端点没有文档通道；"+
				"发射形状由 types 的单测钉住，等支持该形状的端点再真机验证", status, strings.TrimSpace(errBody))
		case finish == "length" || finish == "max_tokens":
			t.Logf("⚠ OpenAI file 形状：HTTP %d 但 finish=%s 回答为空——这是 max_tokens 截断，"+
				"不能当成附件被吞；先加大 max_tokens 再判", status, finish)
		default:
			t.Errorf("静默忽略：端点 HTTP %d(finish=%s) 接受了 file 形状，模型却答不出编号（回答=%q）", status, finish, reply)
		}

		// B. Anthropic 兼容端点的 document block：形状与 Seele 策略发射的一致。
		status, reply, stop, errBody, err := postAnthropicDocument(spec, "text/plain", docData, question)
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		switch {
		case status == http.StatusOK && strings.Contains(reply, attachmentNonce):
			t.Logf("✅ Anthropic document 形状：端点接受文档，且模型读出了编号")
		case status >= 400:
			t.Logf("⛔ Anthropic document 形状：端点明确拒绝（HTTP %d %s）", status, strings.TrimSpace(errBody))
		case stop == "max_tokens":
			t.Logf("⚠ Anthropic document 形状：HTTP %d 但 stop=%s——先加大 max_tokens 再判是否被吞", status, stop)
		case strict:
			t.Errorf("静默忽略：端点 HTTP %d 接受了 document 形状，模型却答不出编号（回答=%q）", status, reply)
		default:
			t.Logf("⚠ Anthropic document 形状：端点 HTTP %d 接受了形状但模型答不出编号（回答=%q）"+
				"——形状校验通过不等于内容进模型；要把它当失败看待就设 SEELEX_SMOKE_DOC_STRICT=1", status, reply)
		}
	})

	// 文档兜底：端点没有文档通道时，把能当文本读的文档内联进提问——这条是真机验证
	// 「给模型一份文件，它真的答出内容」的通路；不能内联的要给诊断，不许静默丢。
	t.Run("文档兜底(内联文本)", func(t *testing.T) {
		docData := []byte("INTERNAL MEMO: attachment code is " + attachmentNonce + ", owner Zhang San.")
		plan := attachment.PlanFor([]types.FilePart{{
			Kind: types.FileKindDocument, Name: "memo.txt", MimeType: "text/plain", Data: docData,
		}})
		t.Logf("处置: %v", plan.Notes)
		if len(plan.Files) != 0 {
			t.Fatalf("文本文档不该占用附件通道：%+v", plan.Files)
		}
		message := types.Message{Role: "user"}.
			WithText("Read the attachment and reply with the attachment code only." + plan.TextSuffix())
		status, reply, finish, errBody, err := postChatCompletions(spec, []types.Message{message}, 512)
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		t.Logf("HTTP %d finish=%s 回答=%q", status, finish, reply)
		if status != http.StatusOK {
			t.Fatalf("内联文本请求失败：HTTP %d %s", status, strings.TrimSpace(errBody))
		}
		if !strings.Contains(reply, attachmentNonce) {
			t.Fatalf("内联文本没被模型读到（回答=%q，finish=%s）", reply, finish)
		}

		// PDF 这类不能内联的：必须留下诊断，不能静默丢。
		pdfPlan := attachment.PlanFor([]types.FilePart{{
			Kind: types.FileKindDocument, Name: "memo.pdf", MimeType: "application/pdf", Data: []byte("%PDF-1.4 probe"),
		}})
		if len(pdfPlan.Notes) != 1 || len(pdfPlan.Files) != 0 || pdfPlan.Text != "" {
			t.Fatalf("PDF 应当只留诊断（不发送），实际 %+v", pdfPlan)
		}
		t.Logf("PDF 处置: %v", pdfPlan.Notes)
	})
}

// solidPNG 合成一张纯色 PNG：内容已知、判据可判，避免把「截图恰好糊了」算成模型失明。
func solidPNG(width, height int, fill color.RGBA) ([]byte, error) {
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: fill}, image.Point{}, draw.Src)
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, canvas); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// postChatCompletions 用 Seele 自己的消息形状组请求：messages 逐个走
// types.Message.MarshalJSON，因此这条路径测的就是我们的发射代码。
//
// 返回值里的 finish 是判据的一部分：deepseek-v4-flash 带 reasoning，max_tokens 不够时
// content 会是空串而 finish=length——那是截断，不是"附件被吞"，两者必须分开。
func postChatCompletions(spec model.AccountSpec, messages []types.Message, maxTokens int) (int, string, string, string, error) {
	raw := make([]json.RawMessage, 0, len(messages))
	for i := range messages {
		encoded, err := json.Marshal(messages[i])
		if err != nil {
			return 0, "", "", "", fmt.Errorf("marshal message %d: %w", i, err)
		}
		raw = append(raw, encoded)
	}
	envelope := map[string]any{"model": spec.Model, "max_tokens": maxTokens, "temperature": 0, "messages": raw}
	body, status, errBody, err := postJSON(strings.TrimSuffix(spec.BaseURL, "/")+"/chat/completions",
		map[string]string{"Authorization": "Bearer " + spec.APIKey}, envelope)
	if err != nil {
		return status, "", "", errBody, err
	}
	var parsed struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return status, "", "", errBody, fmt.Errorf("解析响应: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return status, "", "", errBody, nil
	}
	return status, parsed.Choices[0].Message.Content, parsed.Choices[0].FinishReason, errBody, nil
}

// postAnthropicDocument 发射与 Seele Anthropic 策略一致的 document block
// （image/document 共用一套 source 规则，见 types/file_test.go 的形状断言）。
func postAnthropicDocument(spec model.AccountSpec, mediaType string, payload []byte, question string) (int, string, string, string, error) {
	base := strings.TrimSpace(os.Getenv("SEELEX_SMOKE_ANTHROPIC_BASE"))
	if base == "" {
		base = strings.TrimSuffix(spec.BaseURL, "/") + "/anthropic"
	}
	envelope := map[string]any{
		"model": spec.Model, "max_tokens": 512,
		"messages": []map[string]any{{
			"role": "user",
			"content": []map[string]any{
				{"type": "document", "source": map[string]any{
					"type": "base64", "media_type": mediaType, "data": base64.StdEncoding.EncodeToString(payload),
				}},
				{"type": "text", "text": question},
			},
		}},
	}
	body, status, errBody, err := postJSON(strings.TrimSuffix(base, "/")+"/v1/messages",
		map[string]string{"x-api-key": spec.APIKey, "anthropic-version": "2023-06-01"}, envelope)
	if err != nil {
		return status, "", "", errBody, err
	}
	var parsed struct {
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return status, "", "", errBody, fmt.Errorf("解析响应: %w", err)
	}
	for i := range parsed.Content {
		if parsed.Content[i].Type == "text" {
			return status, parsed.Content[i].Text, parsed.StopReason, errBody, nil
		}
	}
	return status, "", parsed.StopReason, errBody, nil
}

// postJSON 返回 (响应体, HTTP 状态, 错误体, 传输错误)：4xx/5xx 不算传输错误，
// 因为「端点怎么拒绝」本身就是我们要记录的裁决。
func postJSON(url string, headers map[string]string, envelope any) ([]byte, int, string, error) {
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil, 0, "", fmt.Errorf("marshal request: %w", err)
	}
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return nil, 0, "", err
	}
	request.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	client := &http.Client{Timeout: 180 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, "", err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, response.StatusCode, "", err
	}
	if response.StatusCode >= 400 {
		return body, response.StatusCode, string(body), nil
	}
	return body, response.StatusCode, "", nil
}
