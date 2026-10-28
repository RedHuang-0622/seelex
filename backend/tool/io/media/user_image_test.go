package media

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/store"
)

// decodeBase64 解开缩略图载荷（读面只回裸 base64，前缀由渲染层拼）。
func decodeBase64(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		t.Fatalf("base64 解码失败: %v", err)
	}
	return decoded
}

// userImageFixture 起一个真实存储（临时目录）+ stubHost，返回媒体面。
// 用户图片的落盘/配额/归属都是存储侧事实，用假存储测不出「真的写进去了」。
func userImageFixture(t *testing.T) (*SessionMedia, *store.Stores) {
	t.Helper()
	stores, err := store.OpenStores(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stores.Close() })
	return New(&stubHost{media: stores.Media, projects: map[string]string{"sess-a": "proj-a"}}), stores
}

// TestAttachUserImageStoresIntoSessionPartitionAndQueuesOnce 钉住「拖拽/粘贴进来的
// 图片」这条链的四条事实：
//  1. 字节确实落进**该会话自己**的媒体分区（别的会话按同一 ref 读不到）；
//  2. 入队到该会话的待随图队列，别的会话不受影响（模型只看见自己会话的图）；
//  3. 落盘名的扩展名按 MIME 归一（粘贴常常没有文件名，`*.bin` 会让"默认查看器打开"
//     找不到接管程序）；
//  4. 原图位置可交给系统查看器（文件真实存在、内容就是原图）。
func TestAttachUserImageStoresIntoSessionPartitionAndQueuesOnce(t *testing.T) {
	media, stores := userImageFixture(t)
	payload := []byte("png-bytes-of-a-screenshot")

	image, err := media.AttachUserImage(context.Background(), dto.ComposerImageRequest{
		SessionID: "sess-a", Name: "clipboard", MimeType: "image/png",
		Width: 640, Height: 480, Data: payload,
	})
	if err != nil {
		t.Fatalf("AttachUserImage: %v", err)
	}
	if image.Ref == "" || image.Bytes != len(payload) || image.Name != "clipboard.png" {
		t.Fatalf("回读形状 = %+v", image)
	}
	if image.Width != 640 || image.Height != 480 {
		t.Fatalf("尺寸没原样带出: %dx%d", image.Width, image.Height)
	}

	// 1. 会话隔离。
	if _, data, err := stores.Media.ReadMedia(context.Background(), "proj-a", "sess-a", image.Ref); err != nil {
		t.Fatalf("按 ref 读回: %v", err)
	} else if !bytes.Equal(data, payload) {
		t.Fatalf("读回字节 = %q", data)
	}
	if _, _, err := stores.Media.ReadMedia(context.Background(), "proj-a", "sess-b", image.Ref); err == nil {
		t.Fatal("ref 不应跨会话可见")
	}

	// 2. 入队：只进本会话（Take 即清空是 imageattach 的语义，这里只看归属）。
	queued := media.pending.Take("sess-a")
	if len(queued) != 1 {
		t.Fatalf("待随图 = %d 条, want 1", len(queued))
	}
	if queued[0].Ref != image.Ref || !bytes.Equal(queued[0].File.Data, payload) || queued[0].Label != "clipboard.png" {
		t.Fatalf("待随图内容 = %+v", queued[0])
	}
	if got := media.pending.Len("sess-b"); got != 0 {
		t.Fatalf("别的会话不该有待随图: %d", got)
	}

	// 4. 原图位置。
	path, err := media.MediaFilePath(context.Background(), "sess-a", image.Ref)
	if err != nil {
		t.Fatalf("MediaFilePath: %v", err)
	}
	stored, readErr := os.ReadFile(path)
	if readErr != nil || !bytes.Equal(stored, payload) {
		t.Fatalf("MediaFilePath 指向的不是原图: %q err=%v", path, readErr)
	}
	if _, err := media.MediaFilePath(context.Background(), "sess-b", image.Ref); err == nil {
		t.Fatal("别的会话不该能定位这条图片（归属解析要跟着会话走）")
	}
}

// TestAttachUserImageRejectsUnsupportedInput 钉住入口的拒绝面：非图片、空内容、
// 缺会话、白名单外的图片格式都必须**显式失败**（不猜、不降级成路径文本）。
//
// 白名单（png/jpeg/webp/gif/bmp）不是洁癖：扩展名决定系统用哪个程序接管，
// 也决定 provider 收不收（见 backend/tool/io/attachment/README.md 的真机表）。
func TestAttachUserImageRejectsUnsupportedInput(t *testing.T) {
	media, stores := userImageFixture(t)
	cases := []struct {
		name    string
		request dto.ComposerImageRequest
		want    string
	}{
		{
			name:    "非图片",
			request: dto.ComposerImageRequest{SessionID: "sess-a", Name: "notes.txt", MimeType: "text/plain", Data: []byte("hi")},
			want:    "不支持的图片格式",
		},
		{
			name:    "svg 不在白名单",
			request: dto.ComposerImageRequest{SessionID: "sess-a", Name: "x.svg", MimeType: "image/svg+xml", Data: []byte("<svg/>")},
			want:    "不支持的图片格式",
		},
		{
			name:    "空内容",
			request: dto.ComposerImageRequest{SessionID: "sess-a", Name: "empty.png", MimeType: "image/png"},
			want:    "图片内容为空",
		},
		{
			name:    "缺会话",
			request: dto.ComposerImageRequest{Name: "x.png", MimeType: "image/png", Data: []byte("bytes")},
			want:    "缺少会话 ID",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := media.AttachUserImage(context.Background(), testCase.request); err == nil {
				t.Fatal("必须显式失败")
			} else if !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("错误 = %v, want 含 %q", err, testCase.want)
			}
		})
	}
	// 被拒的输入一条也不许落盘：拒绝发生在存储之前，不是"写完再删"。
	if items, err := stores.Media.ListMedia(context.Background(), "proj-a", "sess-a"); err != nil {
		t.Fatal(err)
	} else if len(items) != 0 {
		t.Fatalf("被拒的请求不该留下媒体资产: %+v", items)
	}
}

// TestAttachUserImageKeepsJPEGNameAndQueuesBytePayload 钉住第二个 MIME 走同一条
// 路径（别把 png 硬编码进判据），且队列里的 FilePart 是 image 种类——种类与 MIME
// 打架的附件会被 provider 拒掉（FilePart.Validate）。
func TestAttachUserImageKeepsJPEGNameAndQueuesBytePayload(t *testing.T) {
	media, _ := userImageFixture(t)
	image, err := media.AttachUserImage(context.Background(), dto.ComposerImageRequest{
		SessionID: "sess-a", Name: "截图 2026-10-30.jpeg", MimeType: "image/jpeg", Data: []byte("jpeg-bytes"),
	})
	if err != nil {
		t.Fatalf("AttachUserImage: %v", err)
	}
	if image.Name != "截图 2026-10-30.jpg" {
		t.Fatalf("落盘名 = %q, want 扩展名按 MIME 归一", image.Name)
	}
	queued := media.pending.Take("sess-a")
	if len(queued) != 1 {
		t.Fatalf("待随图 = %d 条", len(queued))
	}
	if !queued[0].File.IsImage() || queued[0].File.MimeType != "image/jpeg" {
		t.Fatalf("队列附件形状 = %+v", queued[0].File)
	}
	if err := queued[0].File.Validate(); err != nil {
		t.Fatalf("队列附件必须能通过发射前校验: %v", err)
	}
}

// TestMediaFilePathRejectsMissingFacts 钉住「点开原图」的失败面：缺会话、缺 ref、
// 未装配存储都必须显式报错——编一条路径交给系统打开器只会打开一个不存在的东西。
func TestMediaFilePathRejectsMissingFacts(t *testing.T) {
	media, _ := userImageFixture(t)
	if _, err := media.MediaFilePath(context.Background(), "", "media:abc"); err == nil {
		t.Fatal("缺会话必须报错")
	}
	if _, err := media.MediaFilePath(context.Background(), "sess-a", "  "); err == nil {
		t.Fatal("缺 ref 必须报错")
	}
	var inert *SessionMedia
	if _, err := inert.MediaFilePath(context.Background(), "sess-a", "media:abc"); err == nil {
		t.Fatal("未装配存储必须报错")
	}
}

// TestResolveUserImagesBuildsMessageMetadata 钉住「提交带图消息时把 ref 解析成
// 展示元数据」这条链：
//   - 只认**本会话媒体分区**里的 ref（别的会话的 ref 一律拒绝：消息里不该出现一条
//     打不开的图）；
//   - 顺序按发送时给的顺序、按 ref 去重（用户眼里的顺序就是发送顺序）；
//   - 不认识的 ref 让整条提交失败，而不是默默少一张图（少图比"标了图打不开"更难发现）。
//
// 这条元数据是**消息的一部分**：它随后被写进用户消息行，对话区据此把图摆在
// 用户消息下面。
func TestResolveUserImagesBuildsMessageMetadata(t *testing.T) {
	media, stores := userImageFixture(t)
	ctx := context.Background()
	first, err := media.AttachUserImage(ctx, dto.ComposerImageRequest{
		SessionID: "sess-a", Name: "shot", MimeType: "image/png", Width: 800, Height: 600, Data: []byte("png-bytes"),
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := media.AttachUserImage(ctx, dto.ComposerImageRequest{
		SessionID: "sess-a", Name: "clip", MimeType: "image/jpeg", Data: []byte("jpg-bytes"),
	})
	if err != nil {
		t.Fatal(err)
	}

	images, err := media.ResolveUserImages(ctx, "sess-a", []string{second.Ref, first.Ref, second.Ref, "  "})
	if err != nil {
		t.Fatalf("ResolveUserImages: %v", err)
	}
	if len(images) != 2 {
		t.Fatalf("解析结果 = %+v, want 2 条（去重 + 丢空串）", images)
	}
	if images[0].Ref != second.Ref || images[1].Ref != first.Ref {
		t.Fatalf("顺序没保留: %+v", images)
	}
	if images[1].Name != "shot.png" || images[1].MimeType != "image/png" || images[1].Width != 800 || images[1].Height != 600 {
		t.Fatalf("元数据 = %+v", images[1])
	}
	if images[1].Bytes != len("png-bytes") {
		t.Fatalf("字节数 = %d", images[1].Bytes)
	}

	// 别的会话：同一 ref 解析不到（归属解析跟着会话走）。
	if _, err := media.ResolveUserImages(ctx, "sess-b", []string{first.Ref}); err == nil {
		t.Fatal("跨会话的 ref 必须拒绝")
	}
	// 不存在的 ref：整条失败，不返回"少一张"的部分结果。
	if _, err := media.ResolveUserImages(ctx, "sess-a", []string{first.Ref, "media:deadbeef"}); err == nil {
		t.Fatal("不存在的 ref 必须让整条解析失败")
	}
	// 没图：返回 nil 且不报错（绝大多数提交都走这条）。
	if images, err := media.ResolveUserImages(ctx, "sess-a", nil); err != nil || images != nil {
		t.Fatalf("空 refs = %+v, %v", images, err)
	}
	// 缺会话：显式报错（不许把元数据算到别的会话上）。
	if _, err := media.ResolveUserImages(ctx, " ", []string{first.Ref}); err == nil {
		t.Fatal("缺会话必须报错")
	}
	_ = stores
}

// TestReadUserImageReturnsBytesForThumbnail 钉住对话区缩略图的读面：按 (会话, ref)
// 读回**原字节**并给出 MIME（渲染层拼 data URL 用）；跨会话读不到。
func TestReadUserImageReturnsBytesForThumbnail(t *testing.T) {
	media, _ := userImageFixture(t)
	ctx := context.Background()
	image, err := media.AttachUserImage(ctx, dto.ComposerImageRequest{
		SessionID: "sess-a", Name: "shot", MimeType: "image/png", Data: []byte("png-bytes"),
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := media.ReadUserImage(ctx, "sess-a", image.Ref)
	if err != nil {
		t.Fatalf("ReadUserImage: %v", err)
	}
	if data.MimeType != "image/png" || data.Bytes != len("png-bytes") {
		t.Fatalf("元数据 = %+v", data)
	}
	if decoded := decodeBase64(t, data.DataBase64); !bytes.Equal(decoded, []byte("png-bytes")) {
		t.Fatalf("读回字节 = %q", decoded)
	}
	if _, err := media.ReadUserImage(ctx, "sess-b", image.Ref); err == nil {
		t.Fatal("别的会话不该能读这张图")
	}
	if _, err := media.ReadUserImage(ctx, "sess-a", ""); err == nil {
		t.Fatal("缺 ref 必须报错")
	}
}
