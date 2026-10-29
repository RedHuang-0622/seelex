package media

import (
	"bytes"
	"context"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	seeletelemetry "github.com/RedHuang-0622/seelex/internal/telemetry"
	"github.com/RedHuang-0622/seelex/store"
)

// 2026-10-29 用户实测：点开对话里的图片时报
//
//	open .seelex\sessions-json\project-<x>\session-<y>\meta\<digest>\meta.json:
//	The system cannot find the path specified
//
// 磁盘上的真相比对：图实际落在 project-e3b0c44298fc1c14（hash16("")，即默认项目），
// 而读的时候去 project-03b5b8d29af3f44b（会话后来绑定的真实项目）找。
// 也就是说**写入时会话还没绑定项目、绑定是之后才建立的**，读侧只按当前绑定找就永远
// 找不到——图片于是有"打不开的风险"，带图的消息也发不出去。
//
// 修法：读侧允许「当前绑定项目 → 默认项目」两个候选分区（会话段不变，隔离性不降）。
// 这条用例把那个时序完整走一遍。
func TestMediaReadFallsBackWhenBindingArrivesAfterWrite(t *testing.T) {
	dir := t.TempDir()
	stores, err := store.OpenStores(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()

	const sessionID = "sess-late-bind"
	const realProject = "proj-real"
	host := &stubHost{media: stores.Media, projects: map[string]string{}}
	media := New(host)
	ctx := seeletelemetry.WithSessionID(context.Background(), sessionID)

	// ① 会话还没绑定项目 → 图落在默认项目分区（hash16("")）。
	if host.SessionProjectID(sessionID) != "" {
		t.Fatal("前置条件：此刻会话不应有项目绑定")
	}
	stored, err := media.AttachUserImage(ctx, dto.ComposerImageRequest{
		SessionID: sessionID, Name: "shot.png", MimeType: "image/png",
		Width: 12, Height: 8, Data: []byte("png-bytes"),
	})
	if err != nil {
		t.Fatalf("AttachUserImage: %v", err)
	}
	if stored.Ref == "" {
		t.Fatal("媒体引用为空")
	}

	// ② 之后会话绑定了真实项目（先有会话、后绑项目是正常时序，不是异常）。
	host.projects[sessionID] = realProject
	// 漂移自证：图确实只在默认项目分区里，按当前绑定项目读是"路径不存在"。
	if _, err := stores.Media.MediaPath(context.Background(), realProject, sessionID, stored.Ref); err == nil {
		t.Fatal("前置条件：按绑定项目本不该读到（否则这条用例测的不是回退）")
	}
	if _, err := stores.Media.MediaPath(context.Background(), "", sessionID, stored.Ref); err != nil {
		t.Fatalf("图应在默认项目分区里：%v", err)
	}

	// ③ 三条读面都必须还能找到这张图。
	path, err := media.MediaFilePath(context.Background(), sessionID, stored.Ref)
	if err != nil {
		t.Fatalf("MediaFilePath 应回退到默认项目分区：%v", err)
	}
	if path == "" {
		t.Fatal("MediaFilePath 返回空路径")
	}
	img, err := media.ReadUserImage(context.Background(), sessionID, stored.Ref)
	if err != nil {
		t.Fatalf("ReadUserImage 应回退到默认项目分区：%v", err)
	}
	if !bytes.Equal([]byte("png-bytes"), []byte(img.Ref)) && img.Bytes == 0 {
		t.Fatalf("读回的图片元数据不对：%+v", img)
	}
	images, err := media.ResolveUserImages(context.Background(), sessionID, []string{stored.Ref})
	if err != nil {
		t.Fatalf("ResolveUserImages 应回退到默认项目分区（否则带图消息整条发不出去）：%v", err)
	}
	if len(images) != 1 || images[0].Ref != stored.Ref {
		t.Fatalf("解析出的图片 = %+v", images)
	}

	// ④ 隔离性不因回退而松动：换一个会话号仍然读不到。
	if _, err := media.MediaFilePath(context.Background(), "sess-other", stored.Ref); err == nil {
		t.Fatal("回退不应让 ref 跨会话可读")
	}
	if _, err := media.ResolveUserImages(context.Background(), "sess-other", []string{stored.Ref}); err == nil {
		t.Fatal("回退不应让 ref 跨会话可解析")
	}
}
