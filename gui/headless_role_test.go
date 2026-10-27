package gui

import (
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/headless"
)

// fakeRoleStore 满足 headless 的 RoleSessionStore 端口（owner：contract.RoleSessionsPort
// 的角色会话读写面）。它只记录 RPC 是否落到对应方法，不复制排序/幂等/floor 语义——
// 那些判据由 store 自己的用例与真实 headless 冒烟覆盖。
type fakeRoleStore struct {
	created   dto.RoleSessionInfo
	draftRows []dto.RoleDraftRow
	synced    dto.RoleDraftSyncResult
	snapshot  dto.RoleSnapshot
	wire      dto.RoleWireSnapshot
	order     string
	schedule  string
}

func newFakeRoleStore() *fakeRoleStore {
	return &fakeRoleStore{}
}

func (app *fakeRoleStore) CreateRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (dto.RoleSessionInfo, error) {
	app.created = dto.RoleSessionInfo{
		MainSessionID: mainSessionID, RoleName: roleName,
		RoleSessionID: roleSessionID, Root: "goal-x",
	}
	return app.created, nil
}

func (app *fakeRoleStore) AppendRoleDraft(mainSessionID, roleName, roleSessionID string, rows []dto.RoleDraftRow) error {
	app.draftRows = append(app.draftRows, rows...)
	return nil
}

func (app *fakeRoleStore) ReadRoleDraft(mainSessionID, roleName, roleSessionID string) ([]dto.RoleDraftRow, error) {
	return app.draftRows, nil
}

func (app *fakeRoleStore) SyncRoleDraft(mainSessionID, roleName, roleSessionID string, order []string) (dto.RoleDraftSyncResult, error) {
	app.synced = dto.RoleDraftSyncResult{CommitID: "c1", SyncedRows: len(app.draftRows)}
	return app.synced, nil
}

func (app *fakeRoleStore) AppendRoleSessionRows(mainSessionID, roleName, roleSessionID string, rows []dto.RoleRow) error {
	return nil
}

func (app *fakeRoleStore) ReadRoleSessionRows(mainSessionID, roleName, roleSessionID string) ([]dto.RoleRow, error) {
	return nil, nil
}

func (app *fakeRoleStore) RoleSnapshot(mainSessionID, roleName, roleSessionID string) (dto.RoleSnapshot, error) {
	app.snapshot = dto.RoleSnapshot{MainSessionID: mainSessionID, RoleName: roleName, RoleSessionID: roleSessionID}
	return app.snapshot, nil
}

func (app *fakeRoleStore) AssembleRoleWire(mainSessionID, roleName, roleSessionID string, budget, k int) (dto.RoleWireSnapshot, error) {
	app.wire = dto.RoleWireSnapshot{MainSessionID: mainSessionID, RoleName: roleName, RoleSessionID: roleSessionID, PrefixDigest: "p1"}
	return app.wire, nil
}

func (app *fakeRoleStore) SetLifecycleOrder(sessionID, policy string, roles []string) error {
	app.order = policy
	return nil
}

func (app *fakeRoleStore) SetRoleLifecycle(mainSessionID, roleName, roleSessionID string, joinSeq uint64, ref *dto.CompactFrameRef) error {
	return nil
}

func (app *fakeRoleStore) ListRoleSessions(mainSessionID string) ([]string, error) {
	return []string{"goal-x"}, nil
}

func (app *fakeRoleStore) ScheduleRegister(sessionID string, payload dto.ScheduleEventPayload) error {
	app.schedule = "registered:" + payload.ScheduleID
	return nil
}

func (app *fakeRoleStore) ScheduleCancel(sessionID string, payload dto.ScheduleEventPayload) error {
	app.schedule = "cancelled:" + payload.ScheduleID
	return nil
}

func (app *fakeRoleStore) ScheduleFire(sessionID string, payload dto.ScheduleEventPayload) error {
	app.schedule = "fired:" + payload.ScheduleID
	return nil
}

func TestHeadlessRoleRPC(t *testing.T) {
	app := newFakeRoleStore()
	base := newHeadlessTestServer(t, headless.Dependencies{Roles: app})

	result := headlessRPC(t, base, "role.create", map[string]any{
		"main_session_id": "main-1", "role_name": "tl", "role_session_id": "tl-1", "join_seq_id": 5,
	})
	if !result.OK || app.created.RoleSessionID != "tl-1" {
		t.Fatalf("role.create = ok=%v err=%q created=%+v", result.OK, result.Error, app.created)
	}
	result = headlessRPC(t, base, "role.append_draft", map[string]any{
		"main_session_id": "main-1", "role_name": "tl", "role_session_id": "tl-1",
		"rows": []map[string]any{{
			"round_id": 1, "role_name": "tl", "role_session_id": "tl-1", "unit_seq": 1,
			"event": map[string]any{"role": "assistant", "content": "hello"},
		}},
	})
	if !result.OK || len(app.draftRows) != 1 {
		t.Fatalf("role.append_draft = ok=%v err=%q rows=%+v", result.OK, result.Error, app.draftRows)
	}
	result = headlessRPC(t, base, "role.sync_draft", map[string]any{
		"main_session_id": "main-1", "role_name": "tl", "role_session_id": "tl-1",
		"order": []string{"user", "main", "tl"},
	})
	if !result.OK || app.synced.SyncedRows != 1 {
		t.Fatalf("role.sync_draft = ok=%v err=%q synced=%+v", result.OK, result.Error, app.synced)
	}
	result = headlessRPC(t, base, "role.snapshot", map[string]any{
		"main_session_id": "main-1", "role_name": "tl", "role_session_id": "tl-1",
	})
	if !result.OK || app.snapshot.MainSessionID != "main-1" {
		t.Fatalf("role.snapshot = ok=%v err=%q snapshot=%+v", result.OK, result.Error, app.snapshot)
	}
	result = headlessRPC(t, base, "role.wire", map[string]any{
		"main_session_id": "main-1", "role_name": "tl", "role_session_id": "tl-1", "budget": 1000, "k": 2,
	})
	if !result.OK || app.wire.PrefixDigest != "p1" {
		t.Fatalf("role.wire = ok=%v err=%q wire=%+v", result.OK, result.Error, app.wire)
	}
	result = headlessRPC(t, base, "role.set_order", map[string]any{
		"session_id": "main-1", "order_policy": "user_main_decided", "order_roles": []string{"user", "main", "tl"},
	})
	if !result.OK || app.order != "user_main_decided" {
		t.Fatalf("role.set_order = ok=%v err=%q order=%q", result.OK, result.Error, app.order)
	}
	result = headlessRPC(t, base, "schedule.register", map[string]any{
		"session_id": "main-1",
		"payload":    map[string]any{"schedule_id": "s1", "role_name": "tl", "role_session_id": "tl-1"},
	})
	if !result.OK || app.schedule != "registered:s1" {
		t.Fatalf("schedule.register = ok=%v err=%q schedule=%q", result.OK, result.Error, app.schedule)
	}
}
