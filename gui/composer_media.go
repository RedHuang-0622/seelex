package gui

// composer_media.go —— 输入框图片附件的桥面（拖拽 / 粘贴进来的图片）。
//
// 两个动作，都是**用户动作**而不是主代理动作：不过权限档位、不进执行选择页面，
// 与「文件详情」面板的保存同一条口径（见 bridge.go 的 WorkspaceWriteFile）。
//
// 与工作区文件引用（file-reference.js）的分工：引用传的是**路径文本**，由 agent
// 自己去读；图片传的是**字节**，由模型直接看。两个入口都长在 composer 上，但后端
// 是两条链——图片绝不退化成"附一条路径让模型去 OCR"，那等于把识别能力换成了猜。

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// composerImageHardLimit 是桥面接受的单张图片字节上限（解码后）。
//
// 真正的配额在存储侧（media.max_item_bytes，默认 8 MiB）：这里只挡"一次 RPC 把
// 几百 MB 拉进后端解码"，让超大 payload 在入口就失败，而不是先吃满内存再被拒。
const composerImageHardLimit = 32 << 20

// AttachComposerImage 收下用户拖拽 / 粘贴进输入框的一张图片。
//
// dataBase64 是 wire 编码（渲染层传不了裸字节）：解码只发生在这一层，领域只认字节。
// 返回的 ComposerImage 是前端后续「点开原图」与去重的凭据；文件位置不回渲染层。
func (bridge *Bridge) AttachComposerImage(sessionID, name, mimeType string, width, height int, dataBase64 string) (dto.ComposerImage, error) {
	media := bridge.composerMedia()
	if media == nil {
		return dto.ComposerImage{}, fmt.Errorf("图片附件未装配（当前宿主不支持）")
	}
	data, err := decodeComposerImage(dataBase64)
	if err != nil {
		return dto.ComposerImage{}, err
	}
	return media.AttachUserImage(bridge.requestContext(), dto.ComposerImageRequest{
		SessionID: sessionID,
		Name:      name,
		MimeType:  mimeType,
		Width:     width,
		Height:    height,
		Data:      data,
	})
}

// OpenComposerImage 用系统默认查看器打开一张已附加图片的原始文件。
//
// 「不打开的预览」是这条交互的前提：缩略图留在输入框 / 对话区里，不抢占前台、不弹窗；
// 只有用户**明确点开**时才交给系统。所以这里不等待、不挂窗口，只把请求交给默认应用。
func (bridge *Bridge) OpenComposerImage(sessionID, ref string) error {
	media := bridge.composerMedia()
	if media == nil {
		return fmt.Errorf("图片附件未装配（当前宿主不支持）")
	}
	path, err := media.MediaFilePath(bridge.requestContext(), sessionID, ref)
	if err != nil {
		return fmt.Errorf("打开图片失败：%w", err)
	}
	return openWithSystemViewer(path)
}

// ComposerImageData 读回一张已附加图片的字节，供对话区画缩略图。
//
// 为什么需要这条读面：用户消息随行图片在消息里只留 ref（字节不进快照与
// transcript），渲染层手上没有路径（路径不回渲染层），所以缩略图必须按 (会话, ref)
// 向宿主读回。它只读、只按会话媒体分区解析，跨会话/跨项目的 ref 在这里读不到。
func (bridge *Bridge) ComposerImageData(sessionID, ref string) (dto.ComposerImageData, error) {
	media := bridge.composerMedia()
	if media == nil {
		return dto.ComposerImageData{}, fmt.Errorf("图片附件未装配（当前宿主不支持）")
	}
	return media.ReadUserImage(bridge.requestContext(), sessionID, ref)
}

// composerMedia 取图片附件端口；未装配时返回 nil（调用方给显式错误）。
func (bridge *Bridge) composerMedia() ComposerMedia {
	if bridge == nil || bridge.deps.Composer == nil {
		return nil
	}
	return bridge.deps.Composer
}

// decodeComposerImage 解码 wire 上的图片正文：容忍 `data:` URL 前缀（从 canvas /
// 剪贴板拿到的常常是 dataURL），去掉换行空白，严格 base64，并挡掉超大 payload。
func decodeComposerImage(raw string) ([]byte, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, fmt.Errorf("图片内容为空")
	}
	if rest, ok := strings.CutPrefix(value, "data:"); ok {
		comma := strings.IndexByte(rest, ',')
		if comma < 0 {
			return nil, fmt.Errorf("图片 data URL 形状不合法")
		}
		if !strings.Contains(rest[:comma], "base64") {
			return nil, fmt.Errorf("图片 data URL 不是 base64 编码")
		}
		value = rest[comma+1:]
	}
	value = strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\r':
			return -1
		}
		return r
	}, value)
	// 长度先于解码判一次：base64 膨胀 4/3，先用编码长度挡掉明显过大的 payload，
	// 免得把几百 MB 先解出来再拒绝。
	if len(value) > composerImageHardLimit/3*4+8 {
		return nil, fmt.Errorf("图片过大：超过 %d 字节上限", composerImageHardLimit)
	}
	data, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("图片 base64 解码失败：%w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("图片内容为空")
	}
	if len(data) > composerImageHardLimit {
		return nil, fmt.Errorf("图片过大：%d > %d 字节", len(data), composerImageHardLimit)
	}
	return data, nil
}
