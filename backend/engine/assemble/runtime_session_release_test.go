package assemble

import (
	"testing"

	"github.com/RedHuang-0622/seelex/backend/engine/account"
	"github.com/RedHuang-0622/seelex/backend/multiagent/teamwork"
	"github.com/RedHuang-0622/seelex/contract/dto"
	seeletelemetry "github.com/RedHuang-0622/seelex/internal/telemetry"
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
	// 路由残格现在住在账号域的注册表里（P21-3）：用例经它的公开面播种与断言，
	// 摘要落点数出来即可判"去重表有没有被清"。
	recorded := 0
	runtime := &Runtime{
		bundles: map[string]*sessionBundle{},
		servedRoute: account.NewServedRouteRegistry(account.ServedRouteDeps{
			RecordSummary: func(event seeletelemetry.SummaryEvent) { recorded++ },
		}),
		teamworkCoords:     map[store.Key]*teamwork.Coordinator{},
		teamworkBoardCache: map[store.Key]teamworkBoardSnapshotCache{},
	}
	runtime.bundleFor("sess-a")
	runtime.bundleFor("sess-b")
	// bundleFor 会把 activeSessionID 指到最近一次打开的那个（活跃别名守卫的输入）。
	if runtime.activeSessionID != "sess-b" {
		t.Fatalf("activeSessionID = %q, want sess-b", runtime.activeSessionID)
	}
	// 承接路由（读即清）+ unpinned 去重表（键 = 会话|账号）：sess-a 两类残格都留，sess-b 只有去重键。
	runtime.servedRoute.Observe(dto.RoutingNotice{Reason: dto.RoutingReasonRequest, SessionID: "sess-a", To: "acct"})
	runtime.servedRoute.Observe(dto.RoutingNotice{Reason: dto.RoutingReasonUnpinnedSelection, SessionID: "sess-a", To: "acct"})
	runtime.servedRoute.Observe(dto.RoutingNotice{Reason: dto.RoutingReasonUnpinnedSelection, SessionID: "sess-a", To: "acct"})
	runtime.servedRoute.Observe(dto.RoutingNotice{Reason: dto.RoutingReasonUnpinnedSelection, SessionID: "sess-b", To: "acct"})
	if recorded != 2 {
		t.Fatalf("unpinned 摘要应每个会话各记一次（同会话第二次被去重）：recorded = %d, want 2", recorded)
	}
	runtime.teamworkCoords[store.Key{ProjectID: "proj", SessionID: "sess-a"}] = nil
	runtime.teamworkBoardCache[store.Key{ProjectID: "proj", SessionID: "sess-a"}] = teamworkBoardSnapshotCache{}

	if !runtime.ReleaseSessionBundle("sess-a") {
		t.Fatal("sess-a 应被释放（非活跃别名、无在飞 fork/后台命令）")
	}
	if hasBundle(runtime, "sess-a") {
		t.Fatal("sess-a 的会话本体仍在 bundles 注册表里")
	}
	if route, ok := runtime.TakeServedRoute("sess-a"); ok {
		t.Fatalf("sess-a 的承接路由残格未清: %+v", route)
	}
	// 去重表残格已清 ⇒ 同会话再来一条 unpinned 会**重新**记一次摘要（对比 sess-b 仍被去重）。
	runtime.servedRoute.Observe(dto.RoutingNotice{Reason: dto.RoutingReasonUnpinnedSelection, SessionID: "sess-a", To: "acct"})
	if recorded != 3 {
		t.Fatalf("去重表里 sess-a 的键未清（应重新记摘要）：recorded = %d, want 3", recorded)
	}
	runtime.servedRoute.Observe(dto.RoutingNotice{Reason: dto.RoutingReasonUnpinnedSelection, SessionID: "sess-b", To: "acct"})
	if recorded != 3 {
		t.Fatalf("别的会话的去重键被误删：recorded = %d, want 3", recorded)
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
