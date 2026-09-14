package seelebridge

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/seelebridge/imageattach"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	seeletelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
	bridgecomputer "github.com/RedHuang-0622/seelex/seelebridge/tools/computer"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

func TestComputerUseEnabledParsing(t *testing.T) {
	for _, value := range []string{"0", "off", "OFF", "false", "no", "  off "} {
		t.Setenv(computerUseEnv, value)
		if computerUseEnabled() {
			t.Fatalf("%s=%q 应关闭 computer use", computerUseEnv, value)
		}
	}
	for _, value := range []string{"", "1", "on", "true", "yes"} {
		t.Setenv(computerUseEnv, value)
		if !computerUseEnabled() {
			t.Fatalf("%s=%q 应开启 computer use", computerUseEnv, value)
		}
	}
}

// TestRegisterBuiltinsRegistersComputerTools 验证正式接入点：支持桌面的平台
// 上 RegisterBuiltins 会挂上整个 computer use 工具族；SEELEX_COMPUTER_USE
// 关闭时不注册（宁可没有，也不挂一串必然失败的摆设）。
func TestRegisterBuiltinsRegistersComputerTools(t *testing.T) {
	if !bridgecomputer.Supported() {
		t.Skip("当前平台没有桌面 computer use 实现")
	}
	want := []string{
		bridgecomputer.ToolScreenshot, bridgecomputer.ToolWindows, bridgecomputer.ToolFocus,
		bridgecomputer.ToolClick, bridgecomputer.ToolMove, bridgecomputer.ToolDrag,
		bridgecomputer.ToolScroll, bridgecomputer.ToolType, bridgecomputer.ToolKeys,
		bridgecomputer.ToolWait,
	}

	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	registered := map[string]bool{}
	for _, tool := range runtime.AllTools() {
		registered[tool.Name] = true
	}
	for _, name := range want {
		if !registered[name] {
			t.Errorf("RegisterBuiltins 未注册 %q", name)
		}
	}

	t.Setenv(computerUseEnv, "off")
	disabled := newTestRuntime(t)
	defer disabled.Shutdown()
	disabled.RegisterBuiltins()
	for _, tool := range disabled.AllTools() {
		if tool.Name == bridgecomputer.ToolScreenshot {
			t.Fatalf("%s=off 时不应注册 computer use 工具", computerUseEnv)
		}
	}
}

// TestStoreSessionMediaWritesIntoExecutingSessionPartition 验证截图落在**执行
// 会话自己**的媒体分区：项目按会话绑定解析，另一个会话按同一 ref 读不到。
func TestStoreSessionMediaWritesIntoExecutingSessionPartition(t *testing.T) {
	dir := t.TempDir()
	router, err := sessionstore.NewRouter(filepath.Join(dir, "session-storage.json"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	runtime := &Runtime{historyRouter: router, sessionWorkspaces: map[string]string{"sess-a": "proj-a"}}
	ctx := seeletelemetry.WithSessionID(context.Background(), "sess-a")

	payload := []byte("png-bytes")
	stored, err := runtime.storeSessionMedia(ctx, bridgecomputer.MediaAsset{
		Name: "screenshot-1.png", MimeType: "image/png", Kind: "screenshot",
		Data: payload, Width: 320, Height: 180, Scale: 0.5,
	})
	if err != nil {
		t.Fatalf("storeSessionMedia: %v", err)
	}
	if stored.Ref == "" || stored.Hash == "" {
		t.Fatalf("媒体引用为空: %+v", stored)
	}
	item, data, err := router.ReadMediaWorkspace(context.Background(), "proj-a", "sess-a", stored.Ref)
	if err != nil {
		t.Fatalf("按 ref 读回: %v", err)
	}
	if !bytes.Equal(data, payload) || item.Kind != "screenshot" || item.Width != 320 {
		t.Fatalf("读回媒体 = %+v data=%q", item, data)
	}
	// 会话隔离：换一个会话号，同一个 ref 读不到（各写自己的分片）。
	if _, _, err := router.ReadMediaWorkspace(context.Background(), "proj-a", "sess-b", stored.Ref); err == nil {
		t.Fatal("ref 不应跨会话可见")
	}
	// ref 兜底读回：随图队列的 Loader 路径（只给引用时用）。
	file, err := runtime.loadSessionMedia(ctx, stored.Ref)
	if err != nil {
		t.Fatalf("loadSessionMedia: %v", err)
	}
	if !file.IsImage() || !bytes.Equal(file.Data, payload) || file.Name != "screenshot-1.png" {
		t.Fatalf("读回附件 = %+v", file)
	}
	if _, err := runtime.loadSessionMedia(seeletelemetry.WithSessionID(context.Background(), "sess-b"), stored.Ref); err == nil {
		t.Fatal("Loader 不应跨会话读回")
	}
	// 缺会话上下文时显式失败，不落进默认项目。
	if _, err := runtime.storeSessionMedia(context.Background(), bridgecomputer.MediaAsset{Name: "x.png", Data: payload}); err == nil {
		t.Fatal("缺少会话上下文必须报错")
	}

	// 子代理（node 会话）执行：node 会话没有独立工作区绑定，项目取 ctx 里的
	// 节点工作区——不落进空项目，也不借视图会话的绑定。
	nodeCtx := seeletelemetry.WithSessionID(context.Background(), "node-1")
	nodeCtx = model.WithNodeScope(nodeCtx, model.NodeScope{NodeID: "node-1", Role: model.RoleSubAgent, WorkspaceID: "proj-b"})
	nodeStored, err := runtime.storeSessionMedia(nodeCtx, bridgecomputer.MediaAsset{
		Name: "screenshot-node.png", MimeType: "image/png", Kind: "screenshot", Data: payload, Width: 320, Height: 180,
	})
	if err != nil {
		t.Fatalf("子代理截图落盘: %v", err)
	}
	if _, _, err := router.ReadMediaWorkspace(context.Background(), "proj-b", "node-1", nodeStored.Ref); err != nil {
		t.Fatalf("子代理媒体应在节点工作区项目下: %v", err)
	}
	if _, _, err := router.ReadMediaWorkspace(context.Background(), "", "node-1", nodeStored.Ref); err == nil {
		t.Fatal("子代理媒体不应落进空项目")
	}
}

// TestMediaProjectIDForResolution 验证媒体归属项目的三级解析：执行会话自己的
// 绑定 → 节点作用域的工作区 → 主会话绑定兜底（都不读 Router 活跃写作用域）。
func TestMediaProjectIDForResolution(t *testing.T) {
	runtime := &Runtime{sessionWorkspaces: map[string]string{"sess-main": "ws-main", "node-bound": "ws-node-bound"}}

	if got := runtime.mediaProjectIDFor(context.Background(), "sess-main"); got != "ws-main" {
		t.Fatalf("主会话项目 = %q, want ws-main", got)
	}
	nodeScope := model.NodeScope{NodeID: "node-1", Role: model.RoleSubAgent, WorkspaceID: "ws-node"}
	nodeCtx := model.WithNodeScope(context.Background(), nodeScope)
	if got := runtime.mediaProjectIDFor(nodeCtx, "node-1"); got != "ws-node" {
		t.Fatalf("节点执行项目 = %q, want ws-node（取节点工作区）", got)
	}
	if got := runtime.mediaProjectIDFor(nodeCtx, "node-bound"); got != "ws-node-bound" {
		t.Fatalf("节点会话自带绑定时应优先 = %q, want ws-node-bound", got)
	}
	// 既没有会话绑定也没有节点工作区：回落默认项目——刻意不借活跃视图会话的
	// 绑定（并行执行期间视图可能已切走）。
	emptyScopeCtx := model.WithNodeScope(context.Background(), model.NodeScope{NodeID: "node-2"})
	if got := runtime.mediaProjectIDFor(emptyScopeCtx, "node-2"); got != "" {
		t.Fatalf("无节点工作区时应回落默认项目 = %q, want 空", got)
	}
	// 无 ctx 作用域的会话同样回落默认项目。
	if got := (&Runtime{}).mediaProjectIDFor(context.Background(), "unknown"); got != "" {
		t.Fatalf("无任何绑定 = %q, want 空项目", got)
	}
}

type stubCompleter struct{}

func (stubCompleter) Complete(context.Context, []types.Message, []types.Tool) (types.Message, error) {
	return types.Message{}, nil
}

// TestWrapImageAttachmentsWiresMediaLoader 验证随图包装挂上了真实媒体分区的
// Loader：附件只带 `media:<hash>` 时也能把字节读回来（截屏工具默认带字节，
// 这条路径是"只给引用"形态的保障）。
func TestWrapImageAttachmentsWiresMediaLoader(t *testing.T) {
	runtime := &Runtime{completer: stubCompleter{}, images: imageattach.NewRegistry()}
	runtime.wrapImageAttachments()
	wrapper, ok := runtime.completer.(*imageattach.Wrapper)
	if !ok {
		t.Fatalf("completer 未被随图包装: %T", runtime.completer)
	}
	if wrapper.Loader == nil {
		t.Fatal("随图包装缺少媒体 Loader")
	}
}

// TestAttachSessionImageQueuesForExecutingSession 验证画面随图：入队到执行会话，
// 附件的载体是图像 FilePart（bytes + mime + 尺寸），且不泄漏到别的会话。
func TestAttachSessionImageQueuesForExecutingSession(t *testing.T) {
	runtime := &Runtime{images: imageattach.NewRegistry()}
	ctx := seeletelemetry.WithSessionID(context.Background(), "sess-a")
	payload := []byte("png-bytes")
	if err := runtime.attachSessionImage(ctx, bridgecomputer.PendingImage{
		Ref: "media:hash", Label: "screenshot 320x180",
		Data: payload, MimeType: "image/png", Name: "screenshot-1.png", Width: 320, Height: 180,
	}); err != nil {
		t.Fatalf("attachSessionImage: %v", err)
	}
	if got := runtime.PendingImageCount(ctx); got != 1 {
		t.Fatalf("待随图 = %d, want 1", got)
	}
	pending := runtime.images.Take("sess-a")
	if len(pending) != 1 {
		t.Fatalf("队列 = %d 条, want 1", len(pending))
	}
	if pending[0].Ref != "media:hash" || !bytes.Equal(pending[0].File.Data, payload) {
		t.Fatalf("附件 = %+v", pending[0])
	}
	if !pending[0].File.IsImage() || pending[0].File.MimeType != "image/png" ||
		pending[0].File.Width != 320 || pending[0].File.Height != 180 {
		t.Fatalf("FilePart = %+v", pending[0].File)
	}
	if got := runtime.PendingImageCount(seeletelemetry.WithSessionID(context.Background(), "sess-b")); got != 0 {
		t.Fatalf("其它会话不应看到这张图: %d", got)
	}
	if err := runtime.attachSessionImage(context.Background(), bridgecomputer.PendingImage{Ref: "media:x"}); err == nil {
		t.Fatal("缺少会话上下文必须报错")
	}
}
