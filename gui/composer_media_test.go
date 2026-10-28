package gui

import (
	"encoding/base64"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// TestDecodeComposerImageShapes 钉住 wire 解码的三件事：裸 base64 与 data URL 都要
// 认（剪贴板给的是 dataURL、arrayBuffer 转出来的是裸 base64）、空白要吃掉、
// 非 base64 与超大 payload 必须显式失败。
func TestDecodeComposerImageShapes(t *testing.T) {
	payload := []byte("png-bytes")
	encoded := base64.StdEncoding.EncodeToString(payload)

	data, err := decodeComposerImage(encoded)
	if err != nil || string(data) != string(payload) {
		t.Fatalf("裸 base64: data=%q err=%v", data, err)
	}
	data, err = decodeComposerImage("data:image/png;base64," + encoded)
	if err != nil || string(data) != string(payload) {
		t.Fatalf("data URL: data=%q err=%v", data, err)
	}
	data, err = decodeComposerImage("  " + encoded[:4] + "\n" + encoded[4:] + "  ")
	if err != nil || string(data) != string(payload) {
		t.Fatalf("带空白: data=%q err=%v", data, err)
	}
	for _, bad := range []string{"", "   ", "not-base64!!", "data:image/png,abc", "data:image/png;base64"} {
		if _, err := decodeComposerImage(bad); err == nil {
			t.Fatalf("%q 必须显式失败", bad)
		}
	}
}

// TestBridgeAttachComposerImageForwardsDecodedBytes 钉住桥面这一段：参数不改写、
// base64 在桥面解码、返回值原样交回前端（前端据此渲染缩略图与去重）。
func TestBridgeAttachComposerImageForwardsDecodedBytes(t *testing.T) {
	fake := newFakeApplication()
	deps := fake.deps()
	composer := &fakeComposerMedia{}
	deps.Composer = composer
	bridge, err := NewBridge(deps, Options{Title: "Seelex Test", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}

	encoded := base64.StdEncoding.EncodeToString([]byte("png-bytes"))
	image, err := bridge.AttachComposerImage("sess-a", "clipboard.png", "image/png", 640, 480, encoded)
	if err != nil {
		t.Fatalf("AttachComposerImage: %v", err)
	}
	if len(composer.attached) != 1 {
		t.Fatalf("媒体面收到 %d 次调用, want 1", len(composer.attached))
	}
	sent := composer.attached[0]
	if sent.SessionID != "sess-a" || sent.Name != "clipboard.png" || sent.MimeType != "image/png" {
		t.Fatalf("参数没原样透传: %+v", sent)
	}
	if sent.Width != 640 || sent.Height != 480 || string(sent.Data) != "png-bytes" {
		t.Fatalf("尺寸/字节没透传: %+v", sent)
	}
	if image.Ref == "" || image.Bytes != len("png-bytes") {
		t.Fatalf("返回值 = %+v", image)
	}
}

// TestBridgeAttachComposerImageSurfacesErrors 钉住错误语义：坏 base64 不落到媒体面
// （先失败，不写盘），媒体面报错原样冒泡（前端据此提示而不是静默吞图）。
func TestBridgeAttachComposerImageSurfacesErrors(t *testing.T) {
	fake := newFakeApplication()
	deps := fake.deps()
	composer := &fakeComposerMedia{}
	deps.Composer = composer
	bridge, err := NewBridge(deps, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bridge.AttachComposerImage("sess-a", "x.png", "image/png", 0, 0, "%%%"); err == nil {
		t.Fatal("坏 base64 必须报错")
	}
	if len(composer.attached) != 0 {
		t.Fatal("解码失败不该落到媒体面")
	}
	composer.attachErr = errors.New("composer: 不支持的图片格式")
	if _, err := bridge.AttachComposerImage("sess-a", "x.png", "image/png", 0, 0, base64.StdEncoding.EncodeToString([]byte("x"))); err == nil {
		t.Fatal("媒体面的错误必须原样冒出来")
	} else if !strings.Contains(err.Error(), "不支持的图片格式") {
		t.Fatalf("错误被改写: %v", err)
	}

	// 未装配宿主：显式报"不支持"，不是静默成功。
	undeployedDeps := fake.deps()
	undeployedDeps.Composer = nil
	bare, err := NewBridge(undeployedDeps, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bare.AttachComposerImage("sess-a", "x.png", "image/png", 0, 0, base64.StdEncoding.EncodeToString([]byte("x"))); err == nil {
		t.Fatal("未装配 Composer 必须报错")
	}
	if err := bare.OpenComposerImage("sess-a", "media:abc"); err == nil {
		t.Fatal("未装配 Composer 必须报错")
	}
}

// TestBridgeOpenComposerImageResolvesOriginalFile 钉住「点开缩略图 → 系统默认查看器」：
// 路径来自媒体面（渲染层拿不到），文件存在才交给打开器；媒体面报错则原样冒泡。
func TestBridgeOpenComposerImageResolvesOriginalFile(t *testing.T) {
	fake := newFakeApplication()
	deps := fake.deps()
	composer := &fakeComposerMedia{}
	deps.Composer = composer
	bridge, err := NewBridge(deps, Options{})
	if err != nil {
		t.Fatal(err)
	}

	// 媒体面报错（ref 不属于该会话 / 资产已回收）：原样冒泡，不去调平台打开器。
	composer.openPath = filepath.Join(t.TempDir(), "gone.png")
	composer.openErr = errors.New("session storage: media ref invalid")
	if err := bridge.OpenComposerImage("sess-a", "media:gone"); err == nil {
		t.Fatal("媒体面报错必须冒泡")
	} else if !strings.Contains(err.Error(), "打开图片失败") {
		t.Fatalf("错误缺少上下文: %v", err)
	}

	// 路径不存在：打开器之前就失败（不把一个不存在的文件交给系统）。
	composer.openErr = nil
	if err := bridge.OpenComposerImage("sess-a", "media:abc"); err == nil {
		t.Fatal("不存在的原图必须报错")
	}
	if len(composer.openCalls) != 2 {
		t.Fatalf("归属解析参数 = %v", composer.openCalls)
	}
}
