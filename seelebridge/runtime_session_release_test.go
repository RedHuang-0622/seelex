package seelebridge

import (
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge/teamwork"
	"github.com/RedHuang-0622/seelex/store"
)

// hasBundle 读桥侧注册表里有没有该会话本体（包内用例直读，不为此export一个
// 只给测试用的方法）。
func hasBundle(runtime *Runtime, sessionID string) bool {
	runtime.bundlesMu.RLock()
	defer runtime.bundlesMu.RUnlock()
	return runtime.bundles[sessionID] != nil
}

// TestReleaseSessionBundleDropsBundleAndResidue：桥侧会话本体释放面——移出
// bundles 注册表并清同会话残格；活跃别名 / 空 ID 拒绝，重复释放幂等。
//
// 为什么这条用例重要：2026-10-14 之前 bundles 只增不删，application 侧的"驱逐"
// 只丢引擎端口引用，本体（框架 Session + 上下文存储 + 主历史）活到进程结束。
func TestReleaseSessionBundleDropsBundleAndResidue(t *testing.T) {
	runtime := &Runtime{
		bundles:            map[string]*sessionBundle{},
		servedRoutes:       map[string]dto.ServedRoute{},
		routingNoticeSeen:  map[string]bool{},
		teamworkCoords:     map[store.Key]*teamwork.Coordinator{},
		teamworkBoardCache: map[store.Key]teamworkBoardSnapshotCache{},
	}
	runtime.bundleFor("sess-a")
	runtime.bundleFor("sess-b")
	// bundleFor 会把 activeSessionID 指到最近一次打开的那个（活跃别名守卫的输入）。
	if runtime.activeSessionID != "sess-b" {
		t.Fatalf("activeSessionID = %q, want sess-b", runtime.activeSessionID)
	}
	runtime.servedRoutes["sess-a"] = dto.ServedRoute{}
	runtime.routingNoticeSeen["sess-a|acct"] = true
	runtime.routingNoticeSeen["sess-b|acct"] = true
	runtime.teamworkCoords[store.Key{ProjectID: "proj", SessionID: "sess-a"}] = nil
	runtime.teamworkBoardCache[store.Key{ProjectID: "proj", SessionID: "sess-a"}] = teamworkBoardSnapshotCache{}

	if !runtime.ReleaseSessionBundle("sess-a") {
		t.Fatal("sess-a 应被释放（非活跃别名、无在飞 fork/后台命令）")
	}
	if hasBundle(runtime, "sess-a") {
		t.Fatal("sess-a 的会话本体仍在 bundles 注册表里")
	}
	if len(runtime.servedRoutes) != 0 {
		t.Fatalf("servedRoutes 残格未清: %#v", runtime.servedRoutes)
	}
	if runtime.routingNoticeSeen["sess-a|acct"] {
		t.Fatal("routingNoticeSeen 中该会话的键未清")
	}
	if !runtime.routingNoticeSeen["sess-b|acct"] {
		t.Fatal("别的会话的残格被误删")
	}
	if len(runtime.teamworkCoords) != 0 || len(runtime.teamworkBoardCache) != 0 {
		t.Fatalf("teamwork 残格未清: coords=%d cache=%d",
			len(runtime.teamworkCoords), len(runtime.teamworkBoardCache))
	}

	// 活跃别名不得释放（legacy 读面仍指着它）。
	if runtime.ReleaseSessionBundle("sess-b") {
		t.Fatal("桥的活跃别名会话不得释放")
	}
	if !hasBundle(runtime, "sess-b") {
		t.Fatal("被拒绝的会话本体不该消失")
	}
	// 幂等：已释放的会话再释放返回 false，不产生副作用。
	if runtime.ReleaseSessionBundle("sess-a") {
		t.Fatal("重复释放应返回 false")
	}
	// 空会话 ID 不释放。
	if runtime.ReleaseSessionBundle("   ") {
		t.Fatal("空会话 ID 不得释放")
	}
}
