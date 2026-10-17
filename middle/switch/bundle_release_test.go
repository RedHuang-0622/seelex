package switching

// bundle_release_test.go 是**桥侧会话本体释放**判据的域级用例（P21-28：判据从
// `backend/engine/assemble/runtime_session_release.go` 搬进本层）。宿主级用例（真 Runtime 的
// 注册表与残格）留在装配层，走同一条链。

import "testing"

// recordingReleaseDeps 造一组可观察的宿主读数：取出/清残格各记一次。
type recordingReleaseDeps struct {
	forkInFlight bool
	asyncPending int
	take         bool
	taken        []string
	dropped      []string
}

func (d *recordingReleaseDeps) deps() BundleReleaseDeps {
	return BundleReleaseDeps{
		ForkInFlight: func(string) bool { return d.forkInFlight },
		AsyncPending: func(string) int { return d.asyncPending },
		TakeBundle: func(sessionID string) bool {
			d.taken = append(d.taken, sessionID)
			return d.take
		},
		DropResidue: func(sessionID string) { d.dropped = append(d.dropped, sessionID) },
	}
}

// TestReleaseBundleRefusesWhileBusy 钉住三种拒绝情形里的两种"还在忙"：fork 在飞、还有在途
// 后台命令——两者都必须**在取走本体之前**判，且不惊动注册表（take 一次都不调）。
func TestReleaseBundleRefusesWhileBusy(t *testing.T) {
	for name, deps := range map[string]*recordingReleaseDeps{
		"fork 在飞": {forkInFlight: true, take: true},
		"有在途后台命令": {asyncPending: 2, take: true},
	} {
		if ReleaseBundle("sess-a", deps.deps()) {
			t.Fatalf("%s：必须拒绝释放", name)
		}
		if len(deps.taken) != 0 || len(deps.dropped) != 0 {
			t.Fatalf("%s：忙的会话不该惊动注册表：%v %v", name, deps.taken, deps.dropped)
		}
	}
}

// TestReleaseBundleRefusesWhenTakeFails 钉住第三种拒绝情形与幂等：活跃别名 / 不在册都由宿主
// 的 TakeBundle 表达（取不到 = 没释放），此时**不清残格**（清残格只在确实移出之后）。
func TestReleaseBundleRefusesWhenTakeFails(t *testing.T) {
	deps := &recordingReleaseDeps{take: false}
	if ReleaseBundle("sess-a", deps.deps()) {
		t.Fatal("取不到本体时不该报告释放成功（幂等）")
	}
	if len(deps.taken) != 1 {
		t.Fatalf("应当问过一次注册表：%v", deps.taken)
	}
	if len(deps.dropped) != 0 {
		t.Fatalf("没移出就不该清残格：%v", deps.dropped)
	}
}

// TestReleaseBundleHappyPath：移出成功后才清残格，且会话号先归一（首尾空白 trim）。
func TestReleaseBundleHappyPath(t *testing.T) {
	deps := &recordingReleaseDeps{take: true}
	if !ReleaseBundle("  sess-a  ", deps.deps()) {
		t.Fatal("空闲且可取走时应当释放")
	}
	if len(deps.taken) != 1 || deps.taken[0] != "sess-a" {
		t.Fatalf("会话号必须归一后取本体：%v", deps.taken)
	}
	if len(deps.dropped) != 1 || deps.dropped[0] != "sess-a" {
		t.Fatalf("移出之后清一次残格：%v", deps.dropped)
	}
}

// TestReleaseBundleEmptyAndMissingDeps：空会话号（错的输入）⇒ false，且不碰读数；
// 缺 TakeBundle 端口（未装配）⇒ false，不 panic。
func TestReleaseBundleEmptyAndMissingDeps(t *testing.T) {
	deps := &recordingReleaseDeps{take: true}
	for _, blank := range []string{"", "   "} {
		if ReleaseBundle(blank, deps.deps()) {
			t.Fatalf("空会话号必须回 false：%q", blank)
		}
	}
	if len(deps.taken) != 0 {
		t.Fatalf("空会话号不该惊动宿主：%v", deps.taken)
	}
	if ReleaseBundle("sess-a", BundleReleaseDeps{}) {
		t.Fatal("没有 TakeBundle 端口时不该报告释放成功")
	}
}
