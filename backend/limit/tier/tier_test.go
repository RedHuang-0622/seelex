package tier

import (
	"errors"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// 本文件钉的是**宿主端口契约**（判据在 tier.go，只有一份）：
// 生效档位解析与回退、落地动作的落点（槽/门/审批面）、脏值不落槽但快照仍重算、
// 未装配端口的降级语义、非视图会话不写快照。

type fakeUnits struct {
	tiers map[string]string
	sets  []string
}

func (units *fakeUnits) UnitPermissionTier(sessionID string) (string, bool) {
	tier, ok := units.tiers[sessionID]
	return tier, ok
}

func (units *fakeUnits) SetUnitPermissionTier(sessionID, tier string) {
	if units.tiers == nil {
		units.tiers = map[string]string{}
	}
	units.tiers[sessionID] = tier
	units.sets = append(units.sets, sessionID+"="+tier)
}

type fakeView struct {
	viewID     string
	snapshotID string
	writes     []string
}

func (view *fakeView) ViewSessionID() string { return view.viewID }

func (view *fakeView) SnapshotSessionID() string { return view.snapshotID }

func (view *fakeView) WriteTier(sessionID, tier string) {
	view.writes = append(view.writes, sessionID+"="+tier)
}

type fakeSettings struct {
	stored   map[string]string
	readErr  error
	writeErr error
	writes   []string
}

func (settings *fakeSettings) SessionPermissionTier(sessionID string) (string, error) {
	if settings.readErr != nil {
		return "", settings.readErr
	}
	return settings.stored[sessionID], nil
}

func (settings *fakeSettings) SetSessionPermissionTier(sessionID, tier string) error {
	if settings.writeErr != nil {
		return settings.writeErr
	}
	if settings.stored == nil {
		settings.stored = map[string]string{}
	}
	settings.stored[sessionID] = tier
	settings.writes = append(settings.writes, sessionID+"="+tier)
	return nil
}

type fakeApproval struct {
	auto   []string
	people int
}

func (approval *fakeApproval) SetPermissionAutoApprovalFor(sessionID string, on bool) {
	value := "off"
	if on {
		value = "on"
	}
	approval.auto = append(approval.auto, sessionID+"="+value)
}

// TestEffectiveTierPrefersSlotThenDefault：会话槽优先；未选择回退进程默认；都没有 = manual。
func TestEffectiveTierPrefersSlotThenDefault(t *testing.T) {
	units := &fakeUnits{tiers: map[string]string{"s1": dto.PermissionTierAuto}}
	deps := Deps{Units: units, DefaultTier: func() string { return dto.PermissionTierFull }}

	if got := deps.EffectiveTier("s1"); got != dto.PermissionTierAuto {
		t.Fatalf("会话槽应优先，got %q", got)
	}
	if got := deps.EffectiveTier("s2"); got != dto.PermissionTierFull {
		t.Fatalf("未选择应回退进程默认，got %q", got)
	}
	if got := (Deps{Units: units}).EffectiveTier("s2"); got != dto.PermissionTierManual {
		t.Fatalf("没有默认时应回退 manual，got %q", got)
	}
	if got := (Deps{}).EffectiveTier("s2"); got != dto.PermissionTierManual {
		t.Fatalf("空 Deps 也应安全回退 manual，got %q", got)
	}
}

// TestApplyTierTouchesSlotGateAndApproval：一次落地写三处（槽 / 门 / 审批面）。
func TestApplyTierTouchesSlotGateAndApproval(t *testing.T) {
	units := &fakeUnits{}
	approval := &fakeApproval{}
	gates := []string{}
	deps := Deps{
		Units:    units,
		Approval: approval,
		Gate:     func(sessionID, tier string) { gates = append(gates, sessionID+"="+tier) },
	}

	deps.ApplyTier("s1", dto.PermissionTierFull)
	if len(units.sets) != 1 || units.sets[0] != "s1=full" {
		t.Fatalf("会话槽应被写，got %v", units.sets)
	}
	if len(gates) != 1 || gates[0] != "s1=full" {
		t.Fatalf("执行门应按会话写，got %v", gates)
	}
	if len(approval.auto) != 1 || approval.auto[0] != "s1=on" {
		t.Fatalf("full 档应打开审批自动放行，got %v", approval.auto)
	}
	// 门未装配时其余两处照旧（降级而不是报错）。
	deps.Gate = nil
	deps.ApplyTier("s2", dto.PermissionTierManual)
	if len(approval.auto) != 2 || approval.auto[1] != "s2=off" {
		t.Fatalf("manual 档应关掉自动放行，got %v", approval.auto)
	}
}

// TestApplyStoredKeepsSlotCleanButMirrorsView：脏值不落槽，但视图快照仍要重算；
// 非视图会话只落自己的槽、不写视图快照。
func TestApplyStoredKeepsSlotCleanButMirrorsView(t *testing.T) {
	units := &fakeUnits{}
	view := &fakeView{snapshotID: "s1"}
	deps := Deps{Units: units, View: view, DefaultTier: func() string { return dto.PermissionTierManual }}

	// 存储里的脏值：不写会话槽，但因为是视图会话，快照仍按"生效档位（回退默认）"重算。
	deps.ApplyStored("s1", "superuser")
	if len(units.sets) != 0 {
		t.Fatalf("脏值不得落会话槽，got %v", units.sets)
	}
	if len(view.writes) != 1 || view.writes[0] != "s1=manual" {
		t.Fatalf("视图快照仍要重算成生效档位，got %v", view.writes)
	}

	// 合法值：落槽 + 落快照。
	deps.ApplyStored("s1", dto.PermissionTierAuto)
	if len(units.sets) != 1 || units.sets[0] != "s1=auto" {
		t.Fatalf("合法值应落槽，got %v", units.sets)
	}
	if view.writes[1] != "s1=auto" {
		t.Fatalf("快照应重算成新档位，got %v", view.writes)
	}

	// 非视图会话：只落槽，不写视图快照。
	deps.ApplyStored("s2", dto.PermissionTierFull)
	if len(view.writes) != 2 {
		t.Fatalf("非视图会话不得改视图快照，got %v", view.writes)
	}
}

// TestSyncViewLockedOnlyForViewSession：只有当前视图会话会被写；空 ID 不写。
func TestSyncViewLockedOnlyForViewSession(t *testing.T) {
	units := &fakeUnits{tiers: map[string]string{"s1": dto.PermissionTierFull}}
	view := &fakeView{snapshotID: "s1"}
	deps := Deps{Units: units, View: view}

	deps.SyncViewLocked("s2")
	deps.SyncViewLocked("  ")
	if len(view.writes) != 0 {
		t.Fatalf("非视图会话/空 ID 不得写快照，got %v", view.writes)
	}
	deps.SyncViewLocked("s1")
	if len(view.writes) != 1 || view.writes[0] != "s1=full" {
		t.Fatalf("视图会话应重算成生效档位，got %v", view.writes)
	}
}

// TestPersistAndReadDegradeWithoutSettings：未装配端口 = 内存态降级（不报错、读回空）。
func TestPersistAndReadDegradeWithoutSettings(t *testing.T) {
	deps := Deps{}
	if err := deps.Persist("s1", dto.PermissionTierFull); err != nil {
		t.Fatalf("未装配端口不该报错：%v", err)
	}
	if got := deps.Read("s1"); got != "" {
		t.Fatalf("未装配端口应读回空，got %q", got)
	}

	settings := &fakeSettings{}
	deps = Deps{Settings: settings}
	if err := deps.Persist(" ", dto.PermissionTierFull); err != nil {
		t.Fatalf("空会话 ID 是 no-op，不该报错：%v", err)
	}
	if len(settings.writes) != 0 {
		t.Fatalf("空会话 ID 不该写盘，got %v", settings.writes)
	}
	if err := deps.Persist("s1", dto.PermissionTierFull); err != nil {
		t.Fatal(err)
	}
	if got := deps.Read("s1"); got != dto.PermissionTierFull {
		t.Fatalf("读回应等于写入值，got %q", got)
	}

	// 写盘失败要显式冒泡（调用方据此整步不改）；读失败按"未选择"。
	settings.writeErr = errors.New("disk full")
	if err := deps.Persist("s1", dto.PermissionTierAuto); err == nil {
		t.Fatal("写盘失败必须报错")
	}
	settings.readErr = errors.New("unreadable")
	if got := deps.Read("s1"); got != "" {
		t.Fatalf("读失败按未选择处理，got %q", got)
	}
}
