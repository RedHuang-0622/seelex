package gui

import (
	"errors"
	"strings"
	"testing"
)

// TestBridgeSubmitToSessionResolvesImageRefs 钉住「提交带图」这一段的桥面职责：
//   - 输入框的 ref 先按**该会话**解析成展示元数据，再交给 owner（图片因此落进
//     这条用户消息，对话区才能在消息下面画缩略图）；
//   - 解析失败就不提交（不写一条指着打不开的图的消息）；
//   - 没有 ref 时一次媒体调用都不发（绝大多数提交都是纯文本）。
func TestBridgeSubmitToSessionResolvesImageRefs(t *testing.T) {
	app := &sessionAwareFakeApplication{fakeApplication: newFakeApplication()}
	deps := app.deps()
	composer := &fakeComposerMedia{}
	deps.Composer = composer
	bridge, err := NewBridge(deps, Options{})
	if err != nil {
		t.Fatal(err)
	}

	if err := bridge.SubmitToSession("sess-a", "看这张图", []string{"media:aa", "media:bb"}); err != nil {
		t.Fatalf("SubmitToSession: %v", err)
	}
	if len(composer.resolveCalls) != 1 || composer.resolveCalls[0] != "sess-a|media:aa,media:bb" {
		t.Fatalf("解析调用 = %v", composer.resolveCalls)
	}
	if len(app.submittedImages) != 2 || app.submittedImages[0].Ref != "media:aa" {
		t.Fatalf("交给 owner 的图片 = %+v", app.submittedImages)
	}

	// 纯文本提交：不碰媒体面。
	composer.resolveCalls = nil
	app.submittedImages = nil
	if err := bridge.SubmitToSession("sess-a", "没有图", nil); err != nil {
		t.Fatalf("SubmitToSession(no images): %v", err)
	}
	if len(composer.resolveCalls) != 0 {
		t.Fatalf("无图提交不该调媒体面: %v", composer.resolveCalls)
	}
	if app.submittedImages != nil {
		t.Fatalf("无图提交不该带图片: %+v", app.submittedImages)
	}
}

// TestBridgeSubmitToSessionRejectsUnresolvableImages 钉住失败面：ref 解析不出来
// （不在本会话媒体分区 / 图已回收）时**整条提交失败**，不静默丢图。
func TestBridgeSubmitToSessionRejectsUnresolvableImages(t *testing.T) {
	app := &sessionAwareFakeApplication{fakeApplication: newFakeApplication()}
	deps := app.deps()
	composer := &fakeComposerMedia{resolveErr: errors.New("composer: 图片不在本会话媒体分区里")}
	deps.Composer = composer
	bridge, err := NewBridge(deps, Options{})
	if err != nil {
		t.Fatal(err)
	}
	app.submitted = ""
	if err := bridge.SubmitToSession("sess-a", "看图", []string{"media:gone"}); err == nil {
		t.Fatal("解析失败必须让提交失败")
	}
	if app.submitted != "" {
		t.Fatalf("解析失败不该把输入交出去: %q", app.submitted)
	}

	// 宿主没装配媒体面：同样显式失败，不是"悄悄当纯文本发出去"。
	undeployed := app.deps()
	undeployed.Composer = nil
	bare, err := NewBridge(undeployed, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := bare.SubmitToSession("sess-a", "看图", []string{"media:aa"}); err == nil {
		t.Fatal("未装配 Composer 时必须报错")
	}
}

// TestBridgeComposerImageDataReadsThumbnail 钉住对话区缩略图的读面：按
// (会话, ref) 读回字节，会话归属原样透传；未装配宿主显式报错。
func TestBridgeComposerImageDataReadsThumbnail(t *testing.T) {
	fake := newFakeApplication()
	deps := fake.deps()
	composer := &fakeComposerMedia{}
	deps.Composer = composer
	bridge, err := NewBridge(deps, Options{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := bridge.ComposerImageData("sess-a", "media:aa")
	if err != nil {
		t.Fatalf("ComposerImageData: %v", err)
	}
	if data.Ref != "media:aa" || data.MimeType != "image/png" || data.DataBase64 == "" {
		t.Fatalf("读取结果 = %+v", data)
	}
	if len(composer.readCalls) != 1 || composer.readCalls[0] != "sess-a|media:aa" {
		t.Fatalf("读取调用 = %v", composer.readCalls)
	}

	composer.readErr = errors.New("media: 会话存储未装配")
	if _, err := bridge.ComposerImageData("sess-a", "media:aa"); err == nil {
		t.Fatal("媒体面报错必须冒泡")
	} else if !strings.Contains(err.Error(), "会话存储未装配") {
		t.Fatalf("错误被改写: %v", err)
	}

	undeployed := fake.deps()
	undeployed.Composer = nil
	bare, err := NewBridge(undeployed, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bare.ComposerImageData("sess-a", "media:aa"); err == nil {
		t.Fatal("未装配 Composer 必须报错")
	}
}
