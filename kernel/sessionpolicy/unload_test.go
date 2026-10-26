package switching

import (
	"errors"
	"strings"
	"testing"
)

// 本文件钉 `Unload` 的判据与次序（M5-R6-unload 接收者转移）：门控两轴、非活跃先持久化、
// 活跃会话收口视图、以及五个动作的先后顺序与三条文案。

type unloadRecorder struct {
	calls        []string
	persistErr   error
	releaseErr   error
	active       bool
	gate         UnloadGate
	releases     int
	renews       int
	publishes    int
	refreshes    int
	sessionInPub string
}

func (rec *unloadRecorder) deps() UnloadDeps {
	return UnloadDeps{
		Gate:          func() UnloadGate { return rec.gate },
		IsViewSession: func(string) bool { return rec.active },
		Persist: func(string) error {
			rec.calls = append(rec.calls, "persist")
			return rec.persistErr
		},
		Release: func(string) error {
			rec.calls = append(rec.calls, "release")
			rec.releases++
			return rec.releaseErr
		},
		RenewView: func() {
			rec.calls = append(rec.calls, "renew")
			rec.renews++
		},
		PublishSnapshotChanged: func(sessionID string) {
			rec.calls = append(rec.calls, "publish")
			rec.publishes++
			rec.sessionInPub = sessionID
		},
		RefreshCatalog: func() {
			rec.calls = append(rec.calls, "refresh")
			rec.refreshes++
		},
	}
}

func TestUnloadRejectsEmptyIDAndBusyGate(t *testing.T) {
	rec := &unloadRecorder{gate: UnloadGate{ErrBusy: ErrQueueNotRunning}}
	if err := Unload(rec.deps(), "  "); err == nil || err.Error() != "session ID is required" {
		t.Fatalf("空会话 ID 文案必须保留，got %v", err)
	}
	if len(rec.calls) != 0 {
		t.Fatalf("空 ID 不该触发任何宿主动作，got %v", rec.calls)
	}

	busy := errors.New("chat is already running")
	rec = &unloadRecorder{gate: UnloadGate{Restoring: true, ErrBusy: busy}}
	if err := Unload(rec.deps(), "s1"); !errors.Is(err, busy) {
		t.Fatalf("恢复中应返回宿主忙碌错误，got %v", err)
	}
	rec = &unloadRecorder{gate: UnloadGate{Running: true, ErrBusy: busy}}
	if err := Unload(rec.deps(), "s1"); !errors.Is(err, busy) {
		t.Fatalf("运行中应返回宿主忙碌错误，got %v", err)
	}
	// 装配缺陷：该轴拒绝却没带错误值 → 响亮失败（不是静默卸载）。
	rec = &unloadRecorder{gate: UnloadGate{Running: true}}
	if err := Unload(rec.deps(), "s1"); !errors.Is(err, ErrUnloadGateUnassembled) {
		t.Fatalf("缺错误值应报 ErrUnloadGateUnassembled，got %v", err)
	}
	if rec.releases != 0 {
		t.Fatal("被拒的卸载不得释放内存态")
	}
}

func TestUnloadNonActivePersistsThenReleases(t *testing.T) {
	rec := &unloadRecorder{}
	if err := Unload(rec.deps(), " s1 "); err != nil {
		t.Fatal(err)
	}
	want := []string{"persist", "release", "publish", "refresh"}
	if strings.Join(rec.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("次序 = %v, want %v", rec.calls, want)
	}
	if rec.renews != 0 {
		t.Fatal("非活跃会话不该收口视图单例")
	}
	if rec.sessionInPub != "s1" {
		t.Fatalf("事件应带 trim 后的会话 ID，got %q", rec.sessionInPub)
	}
}

func TestUnloadActiveSkipsPersistAndRenewsView(t *testing.T) {
	rec := &unloadRecorder{active: true}
	if err := Unload(rec.deps(), "s1"); err != nil {
		t.Fatal(err)
	}
	want := []string{"release", "renew", "publish", "refresh"}
	if strings.Join(rec.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("活跃会话次序 = %v, want %v", rec.calls, want)
	}
}

func TestUnloadFailuresStopTheChain(t *testing.T) {
	rec := &unloadRecorder{persistErr: errors.New("disk full")}
	err := Unload(rec.deps(), "s1")
	if err == nil || err.Error() != "unload persist \"s1\": disk full" {
		t.Fatalf("持久化失败文案必须保留，got %v", err)
	}
	if rec.releases != 0 {
		t.Fatal("持久化失败不得释放内存态（数据不丢）")
	}

	rec = &unloadRecorder{releaseErr: errors.New("busy")}
	err = Unload(rec.deps(), "s1")
	if err == nil || err.Error() != "release session \"s1\": busy" {
		t.Fatalf("释放失败文案必须保留，got %v", err)
	}
	if rec.publishes != 0 || rec.refreshes != 0 {
		t.Fatal("释放失败不得发布事件/刷新目录")
	}
}
