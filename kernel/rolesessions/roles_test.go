package rolesessions

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// fakeRolePort 只实现转发用例需要的一个方法；其余方法用结构满足的最小集合补齐。
type fakeRolePort struct {
	calls []string
}

func (port *fakeRolePort) CreateRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (dto.RoleSessionInfo, error) {
	port.calls = append(port.calls, "create:"+mainSessionID+"/"+roleName)
	return dto.RoleSessionInfo{RoleSessionID: roleSessionID}, nil
}

func (port *fakeRolePort) AppendRoleDraft(string, string, string, []dto.RoleDraftRow) error {
	return nil
}

func (port *fakeRolePort) ReadRoleDraft(string, string, string) ([]dto.RoleDraftRow, error) {
	return nil, nil
}

func (port *fakeRolePort) SyncRoleDraft(string, string, string, []string) (dto.RoleDraftSyncResult, error) {
	return dto.RoleDraftSyncResult{}, nil
}

func (port *fakeRolePort) AppendRoleSessionRows(string, string, string, []dto.RoleRow) error {
	return nil
}

func (port *fakeRolePort) ReadRoleSessionRows(string, string, string) ([]dto.RoleRow, error) {
	return nil, nil
}

func (port *fakeRolePort) RoleSnapshot(string, string, string) (dto.RoleSnapshot, error) {
	return dto.RoleSnapshot{}, nil
}

func (port *fakeRolePort) AssembleRoleWire(string, string, string, int, int) (dto.RoleWireSnapshot, error) {
	return dto.RoleWireSnapshot{}, nil
}

func (port *fakeRolePort) SetLifecycleOrder(string, string, []string) error { return nil }

func (port *fakeRolePort) SetRoleLifecycle(string, string, string, uint64, *dto.CompactFrameRef) error {
	return nil
}

func (port *fakeRolePort) ListRoleSessions(string) ([]string, error) { return nil, nil }

// fakeSchedulePort 只实现转发用例需要的一个方法。
type fakeSchedulePort struct {
	fired []string
}

func (port *fakeSchedulePort) ScheduleRegister(string, dto.ScheduleEventPayload) error { return nil }

func (port *fakeSchedulePort) ScheduleCancel(string, dto.ScheduleEventPayload) error { return nil }

func (port *fakeSchedulePort) ScheduleFire(sessionID string, _ dto.ScheduleEventPayload) error {
	port.fired = append(port.fired, sessionID)
	return nil
}

// TestRoleSessionsPortErrors：三种"没有可用端口"的情形各自给固定文案（判据只此一份）。
func TestRoleSessionsPortErrors(t *testing.T) {
	if _, err := (RoleSessions{}).RolePort(); err == nil ||
		err.Error() != "role session storage is not assembled" {
		t.Fatalf("未装配会话端口时的文案必须保留，got %v", err)
	}
	if _, err := (RoleSessions{Assembled: true}).RolePort(); err == nil ||
		err.Error() != "session port does not expose role session storage" {
		t.Fatalf("未暴露角色能力时的文案必须保留，got %v", err)
	}
	if _, err := (RoleSessions{Assembled: true}).SchedulePort(); err == nil ||
		err.Error() != "session port does not expose schedule events" {
		t.Fatalf("未暴露定时插话能力时的文案必须保留，got %v", err)
	}
	// 只暴露一项时，另一项仍按"未暴露"报错（不是"未装配"）。
	only := RoleSessions{Assembled: true, Roles: &fakeRolePort{}}
	if _, err := only.SchedulePort(); err == nil || !strings.Contains(err.Error(), "schedule events") {
		t.Fatalf("缺一项能力时文案应指向那一项，got %v", err)
	}
}

// TestRoleSessionsForwards：解析成功后原样转发（角色面与定时面各一条）。
func TestRoleSessionsForwards(t *testing.T) {
	roles := &fakeRolePort{}
	schedule := &fakeSchedulePort{}
	sessions := RoleSessions{Assembled: true, Roles: roles, Schedule: schedule}

	info, err := sessions.CreateRoleSession("main-1", "reviewer", "role-9", 7)
	if err != nil {
		t.Fatal(err)
	}
	if info.RoleSessionID != "role-9" || len(roles.calls) != 1 || roles.calls[0] != "create:main-1/reviewer" {
		t.Fatalf("角色面应原样转发，got %v / %+v", roles.calls, info)
	}
	if err := sessions.ScheduleFire("main-1", dto.ScheduleEventPayload{}); err != nil {
		t.Fatal(err)
	}
	if len(schedule.fired) != 1 || schedule.fired[0] != "main-1" {
		t.Fatalf("定时面应原样转发，got %v", schedule.fired)
	}
}
