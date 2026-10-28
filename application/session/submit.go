package session

import (
	"context"
	"errors"
	"strings"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// SubmitDeps supplies draft materialization, loaded/content state sampling,
// restore and the conversation submit command.
type SubmitDeps struct {
	Materialize func(sessionID, text string) error
	Loaded      func(sessionID string) (loaded, contentUnloaded bool)
	Resume      func(sessionID string) error
	// Submit 把一条输入交给对话提交口；images 是这条输入随行的图片附件
	// （输入框拖拽 / 粘贴），没有图时为 nil。
	Submit func(context.Context, string, string, []dto.ComposerImage) error
}

// SubmitApplication owns explicit-session admission order: draft gate, load
// check, restore when needed, then submit. It does not own queue or chat state.
type SubmitApplication struct {
	deps SubmitDeps
}

func NewSubmit(deps SubmitDeps) *SubmitApplication {
	return &SubmitApplication{deps: deps}
}

// SubmitToSession 提交一条纯文本输入（无图片附件）。
//
// 它是**唯一的准入路径**，带图的入口（SubmitToSessionWithImages）走同一份实现：
// 准入顺序（草稿门禁 → 装载检查 → 必要时恢复 → 提交）不许出现第二份。
func (application *SubmitApplication) SubmitToSession(ctx context.Context, sessionID, text string) error {
	return application.SubmitToSessionWithImages(ctx, sessionID, text, nil)
}

// SubmitToSessionWithImages 提交一条带图片附件的输入。
//
// 图片由调用方（GUI 桥面）按会话媒体分区解析成展示元数据；本层不解释图片，
// 只把它挂在这次输入上——归属、配额与「能不能随请求送进模型」仍归媒体域。
func (application *SubmitApplication) SubmitToSessionWithImages(
	ctx context.Context,
	sessionID, text string,
	images []dto.ComposerImage,
) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session ID is required")
	}
	if err := application.deps.Materialize(sessionID, text); err != nil {
		return err
	}
	loaded, contentUnloaded := application.deps.Loaded(sessionID)
	if !loaded || contentUnloaded {
		if err := application.deps.Resume(sessionID); err != nil {
			return err
		}
	}
	return application.deps.Submit(ctx, sessionID, text, images)
}
