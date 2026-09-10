package gui

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// fakeRoleApplication 在 headless RPC 单测里复刻 Application 的 R2/R4 扩展面，
// 不复制存储语义（语义由 sessionstore 测试与真实 headless 冒烟覆盖）。
type fakeRoleApplication struct {
	*fakeApplication
	created   dto.RoleSessionInfo
	draftRows []dto.RoleDraftRow
	synced    dto.RoleDraftSyncResult
	snapshot  dto.RoleSnapshot
	wire      dto.RoleWireSnapshot
	order     string
	schedule  string
}

func newFakeRoleApplication() *fakeRoleApplication {
	return &fakeRoleApplication{fakeApplication: newFakeApplication()}
}

func (app *fakeRoleApplication) CreateRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (dto.RoleSessionInfo, error) {
	app.created = dto.RoleSessionInfo{
		MainSessionID: mainSessionID, RoleName: roleName,
		RoleSessionID: roleSessionID, Root: "goal-x",
	}
	return app.created, nil
}

func (app *fakeRoleApplication) AppendRoleDraft(mainSessionID, roleName, roleSessionID string, rows []dto.RoleDraftRow) error {
	app.draftRows = append(app.draftRows, rows...)
	return nil
}

func (app *fakeRoleApplication) ReadRoleDraft(mainSessionID, roleName, roleSessionID string) ([]dto.RoleDraftRow, error) {
	return app.draftRows, nil
}

func (app *fakeRoleApplication) SyncRoleDraft(mainSessionID, roleName, roleSessionID string, order []string) (dto.RoleDraftSyncResult, error) {
	app.synced = dto.RoleDraftSyncResult{CommitID: "c1", SyncedRows: len(app.draftRows)}
	return app.synced, nil
}

func (app *fakeRoleApplication) AppendRoleSessionRows(mainSessionID, roleName, roleSessionID string, rows []dto.RoleRow) error {
	return nil
}

func (app *fakeRoleApplication) ReadRoleSessionRows(mainSessionID, roleName, roleSessionID string) ([]dto.RoleRow, error) {
	return nil, nil
}

func (app *fakeRoleApplication) RoleSnapshot(mainSessionID, roleName, roleSessionID string) (dto.RoleSnapshot, error) {
	app.snapshot = dto.RoleSnapshot{MainSessionID: mainSessionID, RoleName: roleName, RoleSessionID: roleSessionID}
	return app.snapshot, nil
}

func (app *fakeRoleApplication) AssembleRoleWire(mainSessionID, roleName, roleSessionID string, budget, k int) (dto.RoleWireSnapshot, error) {
	app.wire = dto.RoleWireSnapshot{MainSessionID: mainSessionID, RoleName: roleName, RoleSessionID: roleSessionID, PrefixDigest: "p1"}
	return app.wire, nil
}

func (app *fakeRoleApplication) SetLifecycleOrder(sessionID, policy string, roles []string) error {
	app.order = policy
	return nil
}

func (app *fakeRoleApplication) SetRoleLifecycle(mainSessionID, roleName, roleSessionID string, joinSeq uint64, ref *dto.CompactFrameRef) error {
	return nil
}

func (app *fakeRoleApplication) ListRoleSessions(mainSessionID string) ([]string, error) {
	return []string{"goal-x"}, nil
}

func (app *fakeRoleApplication) ScheduleRegister(sessionID string, payload dto.ScheduleEventPayload) error {
	app.schedule = "registered:" + payload.ScheduleID
	return nil
}

func (app *fakeRoleApplication) ScheduleCancel(sessionID string, payload dto.ScheduleEventPayload) error {
	app.schedule = "cancelled:" + payload.ScheduleID
	return nil
}

func (app *fakeRoleApplication) ScheduleFire(sessionID string, payload dto.ScheduleEventPayload) error {
	app.schedule = "fired:" + payload.ScheduleID
	return nil
}

func TestHeadlessRoleRPC(t *testing.T) {
	app := newFakeRoleApplication()
	base := newHeadlessTestServer(t, app)

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
