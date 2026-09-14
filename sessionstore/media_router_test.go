package sessionstore

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
)

// openMediaRouter 构造走真实 JSON 后端的 Router（媒体分区在 jsonRepository 上）。
func openMediaRouter(t *testing.T) *Router {
	t.Helper()
	dir := t.TempDir()
	router, err := NewRouter(filepath.Join(dir, "session-storage.json"), dir)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })
	return router
}

// TestRouterWriteReadMediaWorkspace 验证 Router 级媒体接缝：显式项目作用域
// 写入/读回，不依赖（也不改变）活跃写作用域；内容相同幂等复用同一 ref。
func TestRouterWriteReadMediaWorkspace(t *testing.T) {
	router := openMediaRouter(t)
	ctx := context.Background()
	payload := []byte("png-bytes")
	item := MediaItem{Name: "screenshot-1.png", MimeType: "image/png", Kind: "screenshot", Data: payload, Width: 320, Height: 180, Scale: 0.5}

	ref, err := router.WriteMediaWorkspace(ctx, "proj-a", "sess-1", item)
	if err != nil {
		t.Fatalf("WriteMediaWorkspace: %v", err)
	}
	if ref.Ref != MediaRefPrefix+ref.Hash || ref.Kind != "screenshot" || ref.SessionID != "sess-1" {
		t.Fatalf("媒体引用 = %+v", ref)
	}
	if router.Workspace() != "" {
		t.Fatalf("写入不应改变活跃写作用域，得到 %q", router.Workspace())
	}
	again, err := router.WriteMediaWorkspace(ctx, "proj-a", "sess-1", item)
	if err != nil {
		t.Fatalf("重复写入: %v", err)
	}
	if again.Hash != ref.Hash {
		t.Fatalf("同内容应复用同一 hash: %s vs %s", again.Hash, ref.Hash)
	}

	read, data, err := router.ReadMediaWorkspace(ctx, "proj-a", "sess-1", ref.Ref)
	if err != nil {
		t.Fatalf("ReadMediaWorkspace: %v", err)
	}
	if !bytes.Equal(data, payload) || read.Width != 320 || read.MimeType != "image/png" {
		t.Fatalf("读回媒体 = %+v data=%q", read, data)
	}

	// 会话隔离：同一项目里换会话号读同一 ref 必须失败。
	if _, _, err := router.ReadMediaWorkspace(ctx, "proj-a", "sess-2", ref.Ref); err == nil {
		t.Fatal("媒体不应跨会话可见")
	}
	// 会话号缺失显式报错（不落进默认项目）。
	if _, err := router.WriteMediaWorkspace(ctx, "proj-a", "  ", item); err == nil {
		t.Fatal("缺少会话号必须报错")
	}
}

// TestRouterMediaWorkspaceUnsupportedBackend 验证后端没有媒体分区能力时
// 显式返回 ErrMediaUnsupported，而不是静默成功。
func TestRouterMediaWorkspaceUnsupportedBackend(t *testing.T) {
	router := openMediaRouter(t)
	router.repository = repositoryWithoutMedia{Repository: router.repository}
	if _, err := router.WriteMediaWorkspace(context.Background(), "p", "s", MediaItem{Name: "a", MimeType: "image/png", Data: []byte("x")}); !errors.Is(err, ErrMediaUnsupported) {
		t.Fatalf("错误 = %v, want ErrMediaUnsupported", err)
	}
	if _, _, err := router.ReadMediaWorkspace(context.Background(), "p", "s", "media:x"); !errors.Is(err, ErrMediaUnsupported) {
		t.Fatalf("读取错误 = %v, want ErrMediaUnsupported", err)
	}
}

// repositoryWithoutMedia 只实现 Repository 主契约（嵌入接口整体转走），刻意
// 不实现 MediaStore——模拟"后端不支持媒体分区"。
type repositoryWithoutMedia struct{ Repository }
