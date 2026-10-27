package gui

import (
	"reflect"
	"testing"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

func TestPermissionRoleGroupsUseExplicitAccountRole(t *testing.T) {
	accounts := []model.AccountInfo{
		{Name: "credential-for-teammate", Role: "teammate"},
		{Name: "agent-1", Role: "agent"},
		{Name: "subagent-2"},
	}
	order, groups := permissionRoleGroups(accounts)
	wantOrder := []string{"agent", "subagent", "goalplan", "teammate"}
	if !reflect.DeepEqual(order, wantOrder) {
		t.Fatalf("order = %v", order)
	}
	if len(groups["teammate"]) != 1 || groups["teammate"][0].Name != "credential-for-teammate" {
		t.Fatalf("teammate group = %+v", groups["teammate"])
	}
	if len(groups["goalplan"]) != 0 {
		t.Fatalf("没有账号的标准角色也必须保留空组，goalplan = %+v", groups["goalplan"])
	}
}

func TestBridgeSetRoleAccountRejectsCrossRoleAccount(t *testing.T) {
	base := newFakeApplication()
	base.snapshot.Session.ID = "sess-role-account"
	base.snapshot.Runtime.Accounts = []model.AccountInfo{
		{Name: "agent-1", Role: "agent"},
		{Name: "teammate-1", Role: "teammate"},
	}
	settings := &fakeRoleSettings{}
	deps := base.deps()
	deps.RoleSettings = settings
	bridge, err := NewBridge(deps, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bridge.SetRoleAccount("teammate", "agent-1"); err == nil {
		t.Fatal("跨角色账号必须被拒绝")
	}
	info, err := bridge.SetRoleAccount("teammate", "teammate-1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(info.Accounts, []string{"teammate-1"}) || info.Account != "teammate-1" {
		t.Fatalf("角色账号回执 = %+v", info)
	}
}

func TestBridgeSessionSSHPathsReturnSessionScope(t *testing.T) {
	base := newFakeApplication()
	base.snapshot.Session.ID = "sess-role-paths"
	base.snapshot.Runtime.Accounts = []model.AccountInfo{{Name: "subagent-1", Role: "subagent"}}
	settings := &fakeRoleSettings{defaultSpace: []string{"/srv/default"}}
	deps := base.deps()
	deps.RoleSettings = settings
	bridge, err := NewBridge(deps, Options{})
	if err != nil {
		t.Fatal(err)
	}
	info, err := bridge.SetRoleSessionSSHUserSpaces("subagent", []string{"/srv/session"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(settings.userSpace, []string{"/srv/session"}) ||
		!reflect.DeepEqual(info.SessionSSHUserSpaces, []string{"/srv/session"}) ||
		!reflect.DeepEqual(info.DefaultSSHUserSpaces, []string{"/srv/default"}) {
		t.Fatalf("session paths = %+v settings=%v default=%v", info, settings.userSpace, settings.defaultSpace)
	}
}
