package media

import (
	"bytes"
	"context"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"testing"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/types"

	bridgecomputer "github.com/RedHuang-0622/seelex/backend/tool/computer"
	"github.com/RedHuang-0622/seelex/backend/tool/io/imageattach"
	"github.com/RedHuang-0622/seelex/internal/model"
	seeletelemetry "github.com/RedHuang-0622/seelex/internal/telemetry"
	"github.com/RedHuang-0622/seelex/store"
)

// stubHost 是 media.Host 的测试替身：两格读面直接读夹具。
type stubHost struct {
	router   *store.Router
	projects map[string]string
}

func (h *stubHost) HistoryRouter() *store.Router { return h.router }

func (h *stubHost) SessionProjectID(sessionID string) string {
	if h == nil {
		return ""
	}
	return h.projects[sessionID]
}

// TestStoreSessionMediaWritesIntoExecutingSessionPartition 验证截图落在**执行
// 会话自己**的媒体分区：项目按会话绑定解析，另一个会话按同一 ref 读不到。
//
// 2026-10-16 随 M4-J2b 从 `backend/engine/assemble/runtime_computer_test.go`
// 原样搬来（被测对象从 `Runtime.storeSessionMedia` 换成 `SessionMedia.StoreMedia`，
// 断言逐条不变；宿主的会话绑定改用 stubHost 提供）。
func TestStoreSessionMediaWritesIntoExecutingSessionPartition(t *testing.T) {
	dir := t.TempDir()
	router, err := store.NewRouter(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	media := New(&stubHost{router: router, projects: map[string]string{"sess-a": "proj-a"}})
	ctx := seeletelemetry.WithSessionID(context.Background(), "sess-a")

	payload := []byte("png-bytes")
	stored, err := media.StoreMedia(ctx, bridgecomputer.MediaAsset{
		Name: "screenshot-1.png", MimeType: "image/png", Kind: "screenshot",
		Data: payload, Width: 320, Height: 180, Scale: 0.5,
	})
	if err != nil {
		t.Fatalf("StoreMedia: %v", err)
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
	file, err := media.LoadMedia(ctx, stored.Ref)
	if err != nil {
		t.Fatalf("LoadMedia: %v", err)
	}
	if !file.IsImage() || !bytes.Equal(file.Data, payload) || file.Name != "screenshot-1.png" {
		t.Fatalf("读回附件 = %+v", file)
	}
	if _, err := media.LoadMedia(seeletelemetry.WithSessionID(context.Background(), "sess-b"), stored.Ref); err == nil {
		t.Fatal("Loader 不应跨会话读回")
	}
	// 缺会话上下文时显式失败，不落进默认项目。
	if _, err := media.StoreMedia(context.Background(), bridgecomputer.MediaAsset{Name: "x.png", Data: payload}); err == nil {
		t.Fatal("缺少会话上下文必须报错")
	}

	// 子代理（node 会话）执行：node 会话没有独立工作区绑定，项目取 ctx 里的
	// 节点工作区——不落进空项目，也不借视图会话的绑定。
	nodeCtx := seeletelemetry.WithSessionID(context.Background(), "node-1")
	nodeCtx = dto.WithSceneScope(nodeCtx, dto.SceneScope{SceneID: "node-1", Role: model.RoleSubAgent, WorkspaceID: "proj-b"})
	nodeStored, err := media.StoreMedia(nodeCtx, bridgecomputer.MediaAsset{
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
	media := New(&stubHost{projects: map[string]string{"sess-main": "ws-main", "node-bound": "ws-node-bound"}})

	if got := media.ProjectIDFor(context.Background(), "sess-main"); got != "ws-main" {
		t.Fatalf("主会话项目 = %q, want ws-main", got)
	}
	sceneScope := dto.SceneScope{SceneID: "node-1", Role: model.RoleSubAgent, WorkspaceID: "ws-node"}
	nodeCtx := dto.WithSceneScope(context.Background(), sceneScope)
	if got := media.ProjectIDFor(nodeCtx, "node-1"); got != "ws-node" {
		t.Fatalf("节点执行项目 = %q, want ws-node（取节点工作区）", got)
	}
	if got := media.ProjectIDFor(nodeCtx, "node-bound"); got != "ws-node-bound" {
		t.Fatalf("节点会话自带绑定时应优先 = %q, want ws-node-bound", got)
	}
	// 既没有会话绑定也没有节点工作区：回落默认项目——刻意不借活跃视图会话的
	// 绑定（并行执行期间视图可能已切走）。
	emptyScopeCtx := dto.WithSceneScope(context.Background(), dto.SceneScope{SceneID: "node-2"})
	if got := media.ProjectIDFor(emptyScopeCtx, "node-2"); got != "" {
		t.Fatalf("无节点工作区时应回落默认项目 = %q, want 空", got)
	}
	// 无宿主（未装配）的会话同样回落默认项目。
	if got := (&SessionMedia{}).ProjectIDFor(context.Background(), "unknown"); got != "" {
		t.Fatalf("无任何绑定 = %q, want 空项目", got)
	}
}

// TestAttachSessionImageQueuesForExecutingSession 验证画面随图：入队到执行会话，
// 附件的载体是图像 FilePart（bytes + mime + 尺寸），且不泄漏到别的会话。
func TestAttachSessionImageQueuesForExecutingSession(t *testing.T) {
	media := New(nil)
	ctx := seeletelemetry.WithSessionID(context.Background(), "sess-a")
	payload := []byte("png-bytes")
	if err := media.AttachPending(ctx, bridgecomputer.PendingImage{
		Ref: "media:hash", Label: "screenshot 320x180",
		Data: payload, MimeType: "image/png", Name: "screenshot-1.png", Width: 320, Height: 180,
	}); err != nil {
		t.Fatalf("AttachPending: %v", err)
	}
	if got := media.PendingCount(ctx); got != 1 {
		t.Fatalf("待随图 = %d, want 1", got)
	}
	pending := media.pending.Take("sess-a")
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
	if got := media.PendingCount(seeletelemetry.WithSessionID(context.Background(), "sess-b")); got != 0 {
		t.Fatalf("其它会话不应看到这张图: %d", got)
	}
	if err := media.AttachPending(context.Background(), bridgecomputer.PendingImage{Ref: "media:x"}); err == nil {
		t.Fatal("缺少会话上下文必须报错")
	}
}

// streamEventStub 实现 agent.StreamEventCompleter（链式接线的探测点）。
type streamEventStub struct{}

func (streamEventStub) CompleteStream(
	context.Context, []types.Message, []types.Tool, func(string),
) (string, string, []types.ToolCall, error) {
	return "", "", nil, nil
}

// stubCompleter 是最小的 agent.Completer（包装链的下游探测点）。
type stubCompleter struct{}

func (stubCompleter) Complete(context.Context, []types.Message, []types.Tool) (types.Message, error) {
	return types.Message{}, nil
}

func (streamEventStub) CompleteStreamEvents(
	context.Context, []types.Message, []types.Tool, func(types.StreamEvent),
) (string, string, []types.ToolCall, error) {
	return "", "", nil, nil
}

// TestWrapKeepsStreamChain 验证包装的链式接线：非 nil 的 streamer 进 wrapper.Stream
// 且返回的一对里 streamer 也是 wrapper（引擎按组件快照取，不回填就等于丢了流式）；
// next 为 nil（未装配）时原样返回，不造包装。
func TestWrapKeepsStreamChain(t *testing.T) {
	media := New(nil)
	next := stubCompleter{}
	streamer := streamEventStub{}
	wrappedNext, wrappedStreamer := media.Wrap(next, streamer)
	wrapper, ok := wrappedNext.(*imageattach.Wrapper)
	if !ok {
		t.Fatalf("completer 未被随图包装: %T", wrappedNext)
	}
	if wrapper.Next != agent.Completer(next) {
		t.Fatalf("包装的下游不是原 completer: %T", wrapper.Next)
	}
	if wrapper.Stream == nil {
		t.Fatal("非 nil 的 streamer 应挂进 wrapper.Stream")
	}
	if wrapper.StreamEvents == nil {
		t.Fatal("实现 StreamEventCompleter 的 streamer 应同时挂 StreamEvents")
	}
	if wrappedStreamer != agent.StreamCompleter(wrapper) {
		t.Fatalf("返回的 streamer 应是同一个包装: %T", wrappedStreamer)
	}

	untouchedNext, untouchedStreamer := media.Wrap(nil, nil)
	if untouchedNext != nil || untouchedStreamer != nil {
		t.Fatalf("未装配 completer 时不应造包装: %v / %v", untouchedNext, untouchedStreamer)
	}
}

// TestNilMediaIsInert 验证未装配（零值）时四格读写都是空操作/显式错误，不 panic。
func TestNilMediaIsInert(t *testing.T) {
	var media *SessionMedia
	media.Attach(context.Background(), imageattach.Attachment{Ref: "media:x"})
	if got := media.PendingCount(context.Background()); got != 0 {
		t.Fatalf("零值待随图 = %d, want 0", got)
	}
	if _, err := media.StoreMedia(seeletelemetry.WithSessionID(context.Background(), "s"), bridgecomputer.MediaAsset{}); err == nil {
		t.Fatal("未装配存储应显式报错")
	}
	if _, err := media.LoadMedia(seeletelemetry.WithSessionID(context.Background(), "s"), "media:x"); err == nil {
		t.Fatal("未装配存储应显式报错")
	}
}
