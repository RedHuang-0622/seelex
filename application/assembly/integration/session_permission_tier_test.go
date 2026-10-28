package integration

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// session_permission_tier_test.go — 主会话权限档位（tier）在 application 层的验收：
// 选择归属会话单元、投影按会话读取、非法档位报错不改档、旧全权兼容壳等价映射。
//
// 迁移说明：原先读 `permissionTierForSession` / `fullAccessForSession` 两个未导出
// 转发方法。它们的唯一消费者就是运行时投影（view 协调器的 CurrentPermissionTier /
// CurrentFullAccess），所以这里改读 `CollectRuntimeProjectionFor` 的两个字段——
// 同一判据、同一来源，断言不变。

// projectedTier 返回指定会话的运行时投影里生效的权限档位。
func projectedTier(t *testing.T, service *legacyTestService, sessionID string) string {
	t.Helper()
	return service.components.view.CollectRuntimeProjectionFor(context.Background(), sessionID).Runtime.PermissionTier
}

// projectedFullAccess 返回指定会话的运行时投影里生效的全权派生位。
func projectedFullAccess(t *testing.T, service *legacyTestService, sessionID string) bool {
	t.Helper()
	return service.components.view.CollectRuntimeProjectionFor(context.Background(), sessionID).Runtime.FullAccess
}

// TestPermissionTierOwnershipPerSession：档位选择归属进 SessionUnit——每个会话保存
// 自己的档位，切换/新建后互不覆盖；未选择会话回退进程默认档位。
func TestPermissionTierOwnershipPerSession(t *testing.T) {
	service := newLegacyTestService(t, &fakeEngine{})
	sessionA := service.components.frontendSnapshot.Snapshot().Session.ID

	effective, err := service.components.policyApplication.SetPermissionTier(dto.PermissionTierAuto)
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
	if got := projectedTier(t, service, sessionA); got != dto.PermissionTierAuto {
		t.Fatalf("projected tier(A) = %q, want auto", got)
	}
	if projectedFullAccess(t, service, sessionA) {
		t.Fatal("auto 档不得被当成旧全权（full_access 应为 false）")
	}

	if err := service.components.sessionDrafts.entry.Begin(); err != nil {
		t.Fatalf("BeginNewSession: %v", err)
	}
	draftID := service.components.frontendSnapshot.Snapshot().Session.ID
	if unit := service.sessions.Unit(draftID); unit == nil {
		t.Fatal("draft unit missing")
	} else if _, ok := unit.PermissionTier(); ok {
		t.Fatal("draft must start without a tier choice")
	}
	// 未选择的草稿回退进程默认（manual），不继承 A 的 auto。
	if got := projectedTier(t, service, draftID); got != dto.PermissionTierManual {
		t.Fatalf("unset draft tier = %q, want manual（进程默认）", got)
	}
	if got := projectedTier(t, service, sessionA); got != dto.PermissionTierAuto {
		t.Fatalf("A's tier must survive BeginNewSession: %q", got)
	}
}

// TestPermissionTierProjectionPerSession：运行时投影按会话读取生效档位（view 协调器
// 经 CurrentPermissionTier 注入），并携带档位目录供前端渲染列表。
func TestPermissionTierProjectionPerSession(t *testing.T) {
	service := newLegacyTestService(t, &fakeEngine{})
	sessionA := service.components.frontendSnapshot.Snapshot().Session.ID
	if _, err := service.components.policyApplication.SetPermissionTier(dto.PermissionTierEdit); err != nil {
		t.Fatalf("SetPermissionTier(edit): %v", err)
	}
	if err := service.components.sessionDrafts.entry.Begin(); err != nil {
		t.Fatalf("BeginNewSession: %v", err)
	}
	draftID := service.components.frontendSnapshot.Snapshot().Session.ID

	projectionA := service.components.view.CollectRuntimeProjectionFor(context.Background(), sessionA)
	if projectionA.Runtime.PermissionTier != dto.PermissionTierEdit {
		t.Fatalf("projection of A tier = %q, want edit", projectionA.Runtime.PermissionTier)
	}
	if len(projectionA.Runtime.PermissionTiers) == 0 {
		t.Fatal("projection must carry the tier catalog for the frontend list")
	}
	projectionDraft := service.components.view.CollectRuntimeProjectionFor(context.Background(), draftID)
	if projectionDraft.Runtime.PermissionTier != dto.PermissionTierManual {
		t.Fatalf("projection of unset draft tier = %q, want process default manual", projectionDraft.Runtime.PermissionTier)
	}
}

// TestSetPermissionTierRejectsUnknown：非法档位 id 报错且**不改变**当前档位。
func TestSetPermissionTierRejectsUnknown(t *testing.T) {
	service := newLegacyTestService(t, &fakeEngine{})
	sessionA := service.components.frontendSnapshot.Snapshot().Session.ID
	if _, err := service.components.policyApplication.SetPermissionTier(dto.PermissionTierFull); err != nil {
		t.Fatalf("SetPermissionTier(full): %v", err)
	}
	if _, err := service.components.policyApplication.SetPermissionTier("superuser"); err == nil {
		t.Fatal("未知档位应当报错")
	}
	if got := projectedTier(t, service, sessionA); got != dto.PermissionTierFull {
		t.Fatalf("非法档位不得改变当前档位：got %q, want full", got)
	}
}

// TestSetFullAccessCompatMapsToTier：旧全权开关是档位的兼容壳（true ⇔ full，
// false ⇔ manual），且投影同步 full_access 派生位。
func TestSetFullAccessCompatMapsToTier(t *testing.T) {
	service := newLegacyTestService(t, &fakeEngine{})
	sessionA := service.components.frontendSnapshot.Snapshot().Session.ID

	if !service.components.policyApplication.SetFullAccess(true) {
		t.Fatal("SetFullAccess(true) 应返回 true（full 档生效）")
	}
	if got := projectedTier(t, service, sessionA); got != dto.PermissionTierFull {
		t.Fatalf("SetFullAccess(true) 后档位 = %q, want full", got)
	}
	if !projectedFullAccess(t, service, sessionA) {
		t.Fatal("full 档应派生 full_access=true")
	}
	projection := service.components.view.CollectRuntimeProjectionFor(context.Background(), sessionA)
	if !projection.Runtime.FullAccess || projection.Runtime.PermissionTier != dto.PermissionTierFull {
		t.Fatalf("投影 = full_access:%v tier:%q, want true/full",
			projection.Runtime.FullAccess, projection.Runtime.PermissionTier)
	}

	if service.components.policyApplication.SetFullAccess(false) {
		t.Fatal("SetFullAccess(false) 应返回 false（manual 档）")
	}
	if got := projectedTier(t, service, sessionA); got != dto.PermissionTierManual {
		t.Fatalf("SetFullAccess(false) 后档位 = %q, want manual", got)
	}
}
