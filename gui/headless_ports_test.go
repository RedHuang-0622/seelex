package gui

// headless_ports_test.go 是 headless 控制面用例的 per-owner 窄 fake 与测试装配表。
//
// 旧形态：这些用例围着 application 宽门面写，一个 fakeApplication 同时实现 30+ 个
// 方法，无论驱动哪条 RPC 都交同一个 fake——门面因此永远"仍被使用"，兼容层无法归零
// （AGENTS.md §10.2 规则 8：禁止用旧门面测试替代新 owner 测试）。
//
// 迁移后端口按 owner 切，fake 也按 owner 给：每个类型只满足一个端口，方法名逐字
// 等于 owner 的方法名；用例装配哪几个 owner 就填哪几个字段，形状与组合根装配表
// （wiring/bootstrap/frontend_ports.go）一致。
//
// 没有任何用例驱动的端口（Fork / Interaction / Workspace / ToolRecords）在这里不造
// fake：无人消费的替身只是第二份门面实现（plan.md 秩序与不变量 4）。

import (
	"context"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/headless"
	"github.com/RedHuang-0622/seelex/wiring"
)

// fakeSubmission 是 Submitter 端口（owner：application/input 的提交用例）。
type fakeSubmission struct {
	submitted string
	err       error
}

func (fake *fakeSubmission) Submit(_ context.Context, text string) error {
	fake.submitted = text
	return fake.err
}

// fakeDraft 是 DraftStarter 端口（owner：session 的草稿入口用例）。
type fakeDraft struct{ began bool }

func (fake *fakeDraft) Begin() error {
	fake.began = true
	return nil
}

// fakeResume 是 SessionResumer 端口（owner：session 的恢复用例）；
// ResumeSession 与 ActivateSession 两条 RPC 落到同一个 Resume。
type fakeResume struct{ resumed string }

func (fake *fakeResume) Resume(sessionID string) error {
	fake.resumed = sessionID
	return nil
}

// fakeTurn 是 TurnControl 端口（owner：application/lifecycle）。
// idleGate 非 nil 时 WaitForIdle 阻塞到 gate 关闭或 ctx 到期，用于钉 WaitIdle
// 的超时护栏（headless 把 ctx 错误包装为「等待空闲失败」）。
type fakeTurn struct {
	cancelled string
	idleGate  chan struct{}
}

func (fake *fakeTurn) CancelChat(requestID string) bool {
	fake.cancelled = requestID
	return true
}

func (fake *fakeTurn) WaitForIdle(ctx context.Context) error {
	if fake.idleGate == nil {
		return nil
	}
	select {
	case <-fake.idleGate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// fakeEvents 是 ViewEvents 端口，内部直接用真实 EventHub（owner：session/viewstate）：
// /events 与按会话过滤用例要验证的是真实投递与路由语义，造假 hub 会让用例只测到 fake。
type fakeEvents struct{ hub *application.EventHub }

func (fake *fakeEvents) Subscribe(buffer int) application.Subscription {
	return fake.hub.Subscribe(buffer)
}

// fakeSnapshot 是 ViewSnapshot 端口（goal.* 分派用它解析「当前视图会话」）。
type fakeSnapshot struct{ snap application.Snapshot }

func (fake *fakeSnapshot) Snapshot() application.Snapshot { return fake.snap }

// fakeHistoryPager 是 HistoryPager 端口（owner：application/history）。
type fakeHistoryPager struct {
	lastLimit    int
	loadedLatest bool
}

func (fake *fakeHistoryPager) LoadMoreHistory(limit int) error {
	fake.lastLimit = limit
	return nil
}

func (fake *fakeHistoryPager) LoadLatestHistory() error {
	fake.loadedLatest = true
	return nil
}

// fakeCatalog 是 SessionCatalog 端口（owner：session/query）。refreshGate 非 nil 时
// WaitCatalogRefresh 阻塞到 gate 关闭或 ctx 到期，用于钉目录收敛等待的超时护栏。
type fakeCatalog struct {
	sessions    []application.SessionInfo
	refreshGate chan struct{}
	settles     int
}

func (fake *fakeCatalog) ListSessions() []application.SessionInfo { return fake.sessions }

// SnapshotOf 未被任何 headless 用例驱动（目录读数面由 session/query 自己的用例覆盖），
// 这里只为满足端口形状存在。
func (fake *fakeCatalog) SnapshotOf(string) (application.SessionSnapshot, error) {
	return application.SessionSnapshot{}, nil
}

func (fake *fakeCatalog) WaitCatalogRefresh(ctx context.Context) error {
	fake.settles++
	if fake.refreshGate == nil {
		return nil
	}
	select {
	case <-fake.refreshGate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// fakePerf 是 PerfReader 端口（owner：application/observability）。
type fakePerf struct{ stats application.PerfStats }

func (fake *fakePerf) PerfStats() application.PerfStats { return fake.stats }

// fakePolicy 是 PermissionTierSwitcher 端口（owner：application/policy 的开关面）。
type fakePolicy struct{ selectedTier string }

func (fake *fakePolicy) SetPermissionTier(tier string) (string, error) {
	fake.selectedTier = tier
	return tier, nil
}

// headlessTestOwners 是测试侧的装配表：一行一个 owner，与组合根的
// headlessDependencies 同形。用例通过 deps 起控制面，通过各字段读自己那个 owner 的
// 记录状态；本包没有同时驱动多个 owner 的用例，因此字段之间也没有任何调用顺序约定。
type headlessTestOwners struct {
	deps headless.Dependencies
	hub  *application.EventHub

	submission *fakeSubmission
	draft      *fakeDraft
	resume     *fakeResume
	turn       *fakeTurn
	events     *fakeEvents
	snapshot   *fakeSnapshot
	history    *fakeHistoryPager
	catalog    *fakeCatalog
	perf       *fakePerf
	policy     *fakePolicy
}

func newHeadlessTestOwners() *headlessTestOwners {
	owners := &headlessTestOwners{
		hub:        wiring.NewEventHub(),
		submission: &fakeSubmission{},
		draft:      &fakeDraft{},
		resume:     &fakeResume{},
		turn:       &fakeTurn{},
		events:     &fakeEvents{},
		snapshot:   &fakeSnapshot{snap: application.Snapshot{ProtocolVersion: application.ProtocolVersion, Revision: 1}},
		history:    &fakeHistoryPager{},
		catalog:    &fakeCatalog{},
		perf:       &fakePerf{},
		policy:     &fakePolicy{},
	}
	owners.events.hub = owners.hub
	owners.deps = headless.Dependencies{
		Events:     owners.events,
		Snapshot:   owners.snapshot,
		Turn:       owners.turn,
		Submission: owners.submission,
		Draft:      owners.draft,
		Resume:     owners.resume,
		History:    owners.history,
		Catalog:    owners.catalog,
		Perf:       owners.perf,
		Policy:     owners.policy,
	}
	return owners
}
