package core

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// session_permission_tier_test.go — 主会话权限档位（tier）在 application 层的验收：
// 选择归属会话单元、投影按会话读取、非法档位报错不改档、旧全权兼容壳等价映射。

// TestPermissionTierOwnershipPerSession：档位选择归属进 SessionUnit——每个会话保存
// 自己的档位，切换/新建后互不覆盖；未选择会话回退进程默认档位。
func TestPermissionTierOwnershipPerSession(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	sessionA := service.Snapshot().Session.ID

	effective, err := service.SetPermissionTier(dto.PermissionTierAuto)
	if err != nil {
		t.Fatalf("SetPermissionTier(auto): %v", err)
	}
	if effective != dto.PermissionTierAuto {
		t.Fatalf("SetPermissionTier(auto) = %q, want auto", effective)
	}
	if unit := service.sessions.Unit(sessionA); unit == nil {
		t.Fatal("session A unit missing")
	} else if tier, ok := unit.PermissionTier(); !ok || tier != dto.PermissionTierAuto {
		t.Fatalf("session A tier not owned by unit: tier=%q ok=%v", tier, ok)
	}
	if got := service.permissionTierForSession(sessionA); got != dto.PermissionTierAuto {
		t.Fatalf("permissionTierForSession(A) = %q, want auto", got)
	}
	if service.fullAccessForSession(sessionA) {
		t.Fatal("auto 档不得被当成旧全权（fullAccessForSession 应为 false）")
	}

	if err := service.BeginNewSession(); err != nil {
		t.Fatalf("BeginNewSession: %v", err)
	}
	draftID := service.Snapshot().Session.ID
	if unit := service.sessions.Unit(draftID); unit == nil {
		t.Fatal("draft unit missing")
	} else if _, ok := unit.PermissionTier(); ok {
		t.Fatal("draft must start without a tier choice")
	}
	// 未选择的草稿回退进程默认（manual），不继承 A 的 auto。
	if got := service.permissionTierForSession(draftID); got != dto.PermissionTierManual {
		t.Fatalf("unset draft tier = %q, want manual（进程默认）", got)
	}
	if got := service.permissionTierForSession(sessionA); got != dto.PermissionTierAuto {
		t.Fatalf("A's tier must survive BeginNewSession: %q", got)
	}
}

// TestPermissionTierProjectionPerSession：运行时投影按会话读取生效档位（view 协调器
// 经 CurrentPermissionTier 注入），并携带档位目录供前端渲染列表。
func TestPermissionTierProjectionPerSession(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	sessionA := service.Snapshot().Session.ID
	if _, err := service.SetPermissionTier(dto.PermissionTierEdit); err != nil {
		t.Fatalf("SetPermissionTier(edit): %v", err)
	}
	if err := service.BeginNewSession(); err != nil {
		t.Fatalf("BeginNewSession: %v", err)
	}
	draftID := service.Snapshot().Session.ID

	projectionA := service.collectRuntimeProjectionFor(context.Background(), sessionA)
	if projectionA.Runtime.PermissionTier != dto.PermissionTierEdit {
		t.Fatalf("projection of A tier = %q, want edit", projectionA.Runtime.PermissionTier)
	}
	if len(projectionA.Runtime.PermissionTiers) == 0 {
		t.Fatal("projection must carry the tier catalog for the frontend list")
	}
	projectionDraft := service.collectRuntimeProjectionFor(context.Background(), draftID)
	if projectionDraft.Runtime.PermissionTier != dto.PermissionTierManual {
		t.Fatalf("projection of unset draft tier = %q, want process default manual", projectionDraft.Runtime.PermissionTier)
	}
}

// TestSetPermissionTierRejectsUnknown：非法档位 id 报错且**不改变**当前档位。
func TestSetPermissionTierRejectsUnknown(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	sessionA := service.Snapshot().Session.ID
	if _, err := service.SetPermissionTier(dto.PermissionTierFull); err != nil {
		t.Fatalf("SetPermissionTier(full): %v", err)
	}
	if _, err := service.SetPermissionTier("superuser"); err == nil {
		t.Fatal("未知档位应当报错")
	}
	if got := service.permissionTierForSession(sessionA); got != dto.PermissionTierFull {
		t.Fatalf("非法档位不得改变当前档位：got %q, want full", got)
	}
}

// TestSetFullAccessCompatMapsToTier：旧全权开关是档位的兼容壳（true ⇔ full，
// false ⇔ manual），且投影同步 full_access 派生位。
func TestSetFullAccessCompatMapsToTier(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	sessionA := service.Snapshot().Session.ID

	if !service.SetFullAccess(true) {
		t.Fatal("SetFullAccess(true) 应返回 true（full 档生效）")
	}
	if got := service.permissionTierForSession(sessionA); got != dto.PermissionTierFull {
		t.Fatalf("SetFullAccess(true) 后档位 = %q, want full", got)
	}
	if !service.fullAccessForSession(sessionA) {
		t.Fatal("full 档应派生 full_access=true")
	}
	projection := service.collectRuntimeProjectionFor(context.Background(), sessionA)
	if !projection.Runtime.FullAccess || projection.Runtime.PermissionTier != dto.PermissionTierFull {
		t.Fatalf("投影 = full_access:%v tier:%q, want true/full",
			projection.Runtime.FullAccess, projection.Runtime.PermissionTier)
	}

	if service.SetFullAccess(false) {
		t.Fatal("SetFullAccess(false) 应返回 false（manual 档）")
	}
	if got := service.permissionTierForSession(sessionA); got != dto.PermissionTierManual {
		t.Fatalf("SetFullAccess(false) 后档位 = %q, want manual", got)
	}
}
