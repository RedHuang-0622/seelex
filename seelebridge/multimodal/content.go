// Package multimodal 把会话媒体资产编码成 provider 可读的多模态内容。
//
// 定位：Seele v0.1.3 的 types.Message.Content 是 *string（纯文本），
// 带图请求无法经由引擎下发；本包是 Seelex 侧的最小能力适配：
//
//   - 把 sessionstore 媒体分区的 `media:<sha256>` 资产读成 wire 可用的字节；
//   - 编码成 OpenAI 兼容的 content parts（text + image_url data URL）；
//   - 提供最小流式之外的同步客户端，供真机 smoke 与后续接入使用。
//
// 非职责：不做截图、不落盘、不管配额与 GC（那是 sessionstore 媒体分区的事），
// 也不替换引擎的对话循环（等 Seele 支持 content parts 后由引擎接管）。
package multimodal

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// ErrImageInvalid 表示图片缺少 MIME 类型或字节。
var ErrImageInvalid = errors.New("multimodal: image requires mime type and bytes")

// ImageSource 是一张可下发图片：会话媒体资产（引用 + 元数据）+ 原文字节。
type ImageSource struct {
	// Ref 是会话媒体引用 `media:<sha256>`；为空表示非会话资产（例如临时图片）。
	Ref string
	// Name 是原始文件名，仅用于诊断展示。
	Name string
	// MimeType 是内容类型，例如 image/png。
	MimeType string
	// Data 是二进制原文（不预先 base64，编码在 wire 层做）。
	Data []byte
}

// ImageURL 对应 OpenAI 兼容的 image_url 字段。
type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

// Part 是一条 content part：文本或图片，二者互斥由 Type 决定。
type Part struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// Message 是一条 OpenAI 兼容的 user 消息（content 为 parts 数组）。
type Message struct {
	Role    string `json:"role"`
	Content []Part `json:"content"`
}

// LoadImage 从会话媒体分区读出一条媒体资产，装配成可下发图片。
func LoadImage(ctx context.Context, store sessionstore.MediaStore, key sessionstore.Key, ref string) (ImageSource, error) {
	meta, data, err := store.ReadMedia(ctx, key, ref)
	if err != nil {
		return ImageSource{}, err
	}
	return ImageSource{Ref: meta.Ref, Name: meta.Name, MimeType: meta.MimeType, Data: data}, nil
}

// ImageDataURL 编码 data URL：`data:<mime>;base64,<payload>`。
func ImageDataURL(mimeType string, data []byte) string {
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// BuildParts 把提示文本与图片编码成 content parts；图片顺序即展示顺序。
func BuildParts(text string, images []ImageSource) ([]Part, error) {
	parts := make([]Part, 0, len(images)+1)
	if strings.TrimSpace(text) != "" {
		parts = append(parts, Part{Type: "text", Text: text})
	}
	for index, image := range images {
		if strings.TrimSpace(image.MimeType) == "" || len(image.Data) == 0 {
			return nil, fmt.Errorf("%w (index %d)", ErrImageInvalid, index)
		}
		if image.Ref != "" && sessionstore.MediaRefHash(image.Ref) == "" {
			return nil, fmt.Errorf("multimodal: image ref %q is not a session media ref", image.Ref)
		}
		parts = append(parts, Part{
			Type:     "image_url",
			ImageURL: &ImageURL{URL: ImageDataURL(image.MimeType, image.Data)},
		})
	}
	if len(parts) == 0 {
		return nil, errors.New("multimodal: message requires text or at least one image")
	}
	return parts, nil
}

// BuildMessage 装配一条带图 user 消息。
func BuildMessage(text string, images []ImageSource) (Message, error) {
	parts, err := BuildParts(text, images)
	if err != nil {
		return Message{}, err
	}
	return Message{Role: "user", Content: parts}, nil
}

// RequestBody 返回 wire 请求体所需的字段（供客户端与测试复用同一编码）。
func RequestBody(model string, message Message, maxTokens int, temperature float64) map[string]any {
	body := map[string]any{"model": model, "messages": []Message{message}}
	if maxTokens > 0 {
		body["max_tokens"] = maxTokens
	}
	if temperature > 0 {
		body["temperature"] = temperature
	}
	return body
}
