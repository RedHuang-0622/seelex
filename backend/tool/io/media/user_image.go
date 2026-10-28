package media

// 输入框图片附件（拖拽 / 粘贴）：AttachUserImage + MediaFilePath。
//
// 为什么落在媒体域：要让模型**真的看见**用户拖进来的截图，需要两件事同时成立——
// 字节落进该会话自己的媒体分区、并且挂到「下一次请求」上且只挂一次。这正是截图
// 工具已经在用的那条链路（storeMedia + attach），拖拽/粘贴只是第二个生产者：字节
// 从哪来不改变归属、配额与「至多送一次」的语义。所以这里不新开存储通道、不新开
// 入队路径，只把第二个入口接到同一份实现上。
//
// 与「工作区文件引用」的分工（file-reference.js 那一套）：文件引用传的是**路径
// 文本**，让 agent 自己去读；图片是**字节**，由模型直接看。两者都在 composer 上，
// 但一件走正文、一件走媒体，互不替代。

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/RedHuang-0622/Seele/types"

	bridgecomputer "github.com/RedHuang-0622/seelex/backend/tool/computer"
	"github.com/RedHuang-0622/seelex/backend/tool/io/imageattach"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// userImageKind 是媒体资产的来源标记（只用于溯源展示，与截图的 "screenshot" 并列）。
const userImageKind = "user_image"

// imageExtensions 是可接受的图片 MIME → 落盘扩展名。
//
// 为什么是白名单而不是 `image/*` 全放：扩展名决定「用系统默认查看器打开」时
// 谁来接管这个文件，也决定 provider 收不收（png/jpeg/webp/gif 是 OpenAI 兼容
// 端点确实认的那几种，见 backend/tool/io/attachment/README.md 的真机表）。放进来
// 一个没人认识的类型，只会得到"点了没反应"或一次 400。
var imageExtensions = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/jpg":  ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
	"image/bmp":  ".bmp",
}

// AttachUserImage 把用户从输入框附上来的图片落进该会话的媒体分区，并挂进
// 「下一次模型请求随图」队列。
//
// 会话由请求显式给出（不读视图）：同一时刻可能有多个会话在跑，而这张图属于用户
// 正在编辑的那个会话。归属项目仍走 ProjectIDFor 那一套（会话绑定 → 节点作用域），
// 因此这里不需要、也不允许再算一次。
func (m *SessionMedia) AttachUserImage(ctx context.Context, request dto.ComposerImageRequest) (dto.ComposerImage, error) {
	sessionID := strings.TrimSpace(request.SessionID)
	if sessionID == "" {
		return dto.ComposerImage{}, errors.New("composer: 缺少会话 ID，图片无法送入模型")
	}
	mimeType := strings.ToLower(strings.TrimSpace(request.MimeType))
	extension, supported := imageExtensions[mimeType]
	if !supported {
		return dto.ComposerImage{}, fmt.Errorf("composer: 不支持的图片格式 %q（只接受 png / jpeg / webp / gif / bmp）", request.MimeType)
	}
	if len(request.Data) == 0 {
		return dto.ComposerImage{}, errors.New("composer: 图片内容为空")
	}
	stored, err := m.storeMedia(ctx, sessionID, bridgecomputer.MediaAsset{
		// 扩展名先按 MIME 归一：截图粘贴常常没有文件名，落成 `*.bin` 会让「用默认
		// 查看器打开」找不到接管程序（扩展名是系统选默认应用的唯一线索）。
		Name:     imageName(strings.TrimSpace(request.Name), extension),
		MimeType: mimeType,
		Kind:     userImageKind,
		Data:     request.Data,
		Width:    request.Width,
		Height:   request.Height,
	})
	if err != nil {
		return dto.ComposerImage{}, err
	}
	name := stored.Name
	if name == "" {
		name = "图片" + extension
	}
	m.attach(sessionID, imageattach.Attachment{
		Ref:   stored.Ref,
		Label: name,
		// 直接带字节：这张图刚刚才从渲染进程过来，不必再按 ref 读一次盘；
		// ref 仍写进队列，诊断与「只给引用」形态都能用。
		File: types.FilePart{
			Kind:     types.FileKindImage,
			MimeType: mimeType,
			Data:     request.Data,
			Width:    request.Width,
			Height:   request.Height,
			Name:     name,
		},
	})
	return dto.ComposerImage{
		Ref:      stored.Ref,
		Name:     name,
		MimeType: mimeType,
		Bytes:    stored.Bytes,
		Width:    request.Width,
		Height:   request.Height,
	}, nil
}

// MediaFilePath 返回某条已落盘媒体在当前会话分区里的真实文件路径，供宿主用
// 系统默认查看器打开原图。路径只在宿主进程内使用，不回渲染层。
func (m *SessionMedia) MediaFilePath(ctx context.Context, sessionID, ref string) (string, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "", errors.New("composer: 缺少会话 ID，无法定位图片")
	}
	if strings.TrimSpace(ref) == "" {
		return "", errors.New("composer: 缺少图片引用，无法定位图片")
	}
	if m == nil || m.host == nil {
		return "", errors.New("media: 会话存储未装配")
	}
	return m.host.MediaPathWorkspace(ctx, m.ProjectIDFor(ctx, sessionID), sessionID, ref)
}

// imageName 给出落盘名：保留用户给的文件名主干，扩展名按 MIME 归一。
//
// 名字只用于展示与「谁接管这个文件」，所以不需要（也不该）承担路径语义——真正的
// 净化在存储侧（store 的 sanitizeMediaName），这里只保证扩展名与 MIME 一致。
func imageName(raw, extension string) string {
	base := filepath.Base(strings.ReplaceAll(raw, "\\", "/"))
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	stem = strings.TrimSpace(stem)
	if stem == "" || stem == "." {
		stem = "image"
	}
	return stem + extension
}
