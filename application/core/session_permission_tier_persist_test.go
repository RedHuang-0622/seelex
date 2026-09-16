package core

// 主会话权限档位的**跨重启归属**回归（P1-1：用户口径「这一 session 的权限设置」
// 必须记住）。
//
// 背景：档位落地时（b55382a）只写了 SessionUnit 内存槽，重启即回默认值。本文件先
// 用"共享同一份会话级设置存储的两个 Service 实例"复现该缺口，再作为落盘实现的
// 回归钉：写入必须落到该会话的设置条目上，重新装配必须读回同一个档位。

import (
	"errors"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// errTierSettingRejected 是"会话级设置写盘失败"的桩错误。
var errTierSettingRejected = errors.New("session setting write rejected")

// tierMemSessions 是带"会话级设置"持久化的内存会话桩：模拟 sessionstore 的项目级
// 会话元数据 blob 中 per-session 的权限档位条目，并在"重启前/重启后"两个 Service
// 之间共享同一份数据。
type tierMemSessions struct {
	fakeSessions
	mu    sync.Mutex
	tiers map[string]string
}

func newTierMemSessions() *tierMemSessions {
	return &tierMemSessions{tiers: map[string]string{}}
}

// SessionPermissionTier 读回会话级档位设置（空 = 该会话从未选择，回退进程默认）。
func (sessions *tierMemSessions) SessionPermissionTier(sessionID string) (string, error) {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	return sessions.tiers[sessionID], nil
}

// SetSessionPermissionTier 写入会话级档位设置。
func (sessions *tierMemSessions) SetSessionPermissionTier(sessionID, tier string) error {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	if sessions.tiers == nil {
		sessions.tiers = map[string]string{}
	}
	sessions.tiers[sessionID] = tier
	return nil
}

func (sessions *tierMemSessions) tierFor(sessionID string) string {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	return sessions.tiers[sessionID]
}

// TestPermissionTierSurvivesSessionReload：选 full → 重新装配（= 重启）→ 仍是
// full，且执行门按该会话的档位解析。
func TestPermissionTierSurvivesSessionReload(t *testing.T) {
	store := newTierMemSessions()

	before := newTestService(t, &fakeEngine{}, withTestSessions(store))
	if _, err := before.SetPermissionTier(dto.PermissionTierFull); err != nil {
		t.Fatalf("SetPermissionTier(full): %v", err)
	}
	if got := store.tierFor("session-1"); got != dto.PermissionTierFull {
		t.Fatalf("会话级设置里的档位 = %q, want %q（档位必须写入会话持久设置）", got, dto.PermissionTierFull)
	}
	before.Shutdown()

	after := newTestService(t, &fakeEngine{}, withTestSessions(store))
	if got := after.Snapshot().Runtime.PermissionTier; got != dto.PermissionTierFull {
		t.Fatalf("重启后 Runtime.PermissionTier = %q, want %q", got, dto.PermissionTierFull)
	}
	if !after.fullAccessForSession("session-1") {
		t.Fatal("重启后执行门档位未恢复：fullAccessForSession(session-1) = false")
	}
	if tier := after.permissionTierForSession("session-1"); tier != dto.PermissionTierFull {
		t.Fatalf("重启后 permissionTierForSession = %q, want %q", tier, dto.PermissionTierFull)
	}
}

// TestPermissionTierReloadKeepsManualChoice：显式选了 manual 也必须记住——它不同于
// "从未选择"（后者回退进程默认档位，可能是 -permission 给的更高档）。
func TestPermissionTierReloadKeepsManualChoice(t *testing.T) {
	store := newTierMemSessions()

	before := newTestService(t, &fakeEngine{}, withTestSessions(store))
	if got := store.tierFor("session-1"); got != "" {
		t.Fatalf("初始不该有档位条目：%q", got)
	}
	if _, err := before.SetPermissionTier(dto.PermissionTierManual); err != nil {
		t.Fatalf("SetPermissionTier(manual): %v", err)
	}
	if got := store.tierFor("session-1"); got != dto.PermissionTierManual {
		t.Fatalf("显式 manual 未落盘：%q", got)
	}
	before.Shutdown()

	after := newTestService(t, &fakeEngine{}, withTestSessions(store))
	if got := after.permissionTierForSession("session-1"); got != dto.PermissionTierManual {
		t.Fatalf("重启后档位 = %q, want manual", got)
	}
}

// TestPermissionTierWithoutSettingPortStaysInMemory：未装配会话级设置端口的最小宿主
// （测试桩 / 旧装配）不得让切档报错——档位退回内存态语义，与落盘前一致。
func TestPermissionTierWithoutSettingPortStaysInMemory(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	effective, err := service.SetPermissionTier(dto.PermissionTierFull)
	if err != nil {
		t.Fatalf("未装配设置端口时切档不得失败: %v", err)
	}
	if effective != dto.PermissionTierFull {
		t.Fatalf("生效档位 = %q, want full", effective)
	}
	if tier := service.permissionTierForSession("session-1"); tier != dto.PermissionTierFull {
		t.Fatalf("内存态档位 = %q, want full", tier)
	}
}

// TestPermissionTierSettingPortErrorSurfaces：会话级设置写失败必须显式报错且**不改**
// 内存态档位（否则用户看到"已切全权"，重启后又变回去，且没有任何失败面）。
func TestPermissionTierSettingPortErrorSurfaces(t *testing.T) {
	store := &failingTierSessions{tierMemSessions: newTierMemSessions()}
	service := newTestService(t, &fakeEngine{}, withTestSessions(store))

	if _, err := service.SetPermissionTier(dto.PermissionTierFull); err == nil {
		t.Fatal("设置写失败时切档必须报错")
	}
	if got := service.permissionTierForSession("session-1"); got == dto.PermissionTierFull {
		t.Fatalf("写失败后档位不得变成 full（当前 %q）", got)
	}
	if got := service.Snapshot().Runtime.PermissionTier; got == dto.PermissionTierFull {
		t.Fatalf("写失败后快照档位不得变成 full（当前 %q）", got)
	}
}

type failingTierSessions struct {
	*tierMemSessions
}

func (sessions *failingTierSessions) SetSessionPermissionTier(string, string) error {
	return errTierSettingRejected
}

// TestPermissionTierSwitchMirrorsViewSnapshot：切到某会话时，**目标会话**的生效
// 档位必须重算进进程快照——chip 与运行面板读的就是快照里的 permission_tier。
//
// 回归场景（2026-09-17 实机发现）：切会话只落地会话槽、不重算快照时，快照里留着
// 上一个会话的档位。方向不对称：上一个是 full 而目标是 manual，用户看到"免审"其实
// 要问人只是别扭；反过来"看着 manual 其实免审"是安全问题。
func TestPermissionTierSwitchMirrorsViewSnapshot(t *testing.T) {
	store := newTierMemSessions()
	service := newTestService(t, &fakeEngine{}, withTestSessions(store))
	if err := store.SetSessionPermissionTier("session-b", dto.PermissionTierFull); err != nil {
		t.Fatalf("seed session-b tier: %v", err)
	}

	// 与冷/热挂载同序：读盘在锁外，落地在拿到会话单元之后。
	switchView := func(sessionID string) {
		stored := service.readStoredPermissionTier(sessionID)
		service.ViewMu.Lock()
		service.Core.Snapshot.Session = SessionState{ID: sessionID}
		service.sessions.SetActive(sessionID)
		service.sessionUnitLocked(sessionID)
		service.applyStoredPermissionTier(sessionID, stored)
		service.ViewMu.Unlock()
	}

	switchView("session-b")
	if got := service.Snapshot().Runtime.PermissionTier; got != dto.PermissionTierFull {
		t.Fatalf("切到 session-b 后快照档位 = %q, want full（切换未重算视图快照）", got)
	}
	if !service.Snapshot().Runtime.FullAccess {
		t.Fatal("切到 session-b 后快照 full_access = false, want true（兼容读面必须同步）")
	}
	if tier := service.permissionTierForSession("session-b"); tier != dto.PermissionTierFull {
		t.Fatalf("session-b 生效档位 = %q, want full", tier)
	}

	// 切回没有持久档位的会话：快照必须回到进程默认，不得留着 session-b 的 full。
	switchView("session-a")
	if got := service.Snapshot().Runtime.PermissionTier; got == dto.PermissionTierFull {
		t.Fatalf("切到无档位会话后快照仍显示 full（%q）：显示与实际不一致", got)
	}
	if service.Snapshot().Runtime.FullAccess {
		t.Fatal("切到无档位会话后快照 full_access 仍为 true（危险方向：看着要问人其实免审）")
	}
	if tier := service.permissionTierForSession("session-a"); tier != dto.PermissionTierManual {
		t.Fatalf("session-a 生效档位 = %q, want manual（进程默认）", tier)
	}
}
