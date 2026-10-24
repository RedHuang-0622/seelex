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
	if len(order) != 3 || order[0] != "teammate" || order[1] != "agent" || order[2] != "subagent" {
		t.Fatalf("order = %v", order)
	}
	if len(groups["teammate"]) != 1 || groups["teammate"][0].Name != "credential-for-teammate" {
		t.Fatalf("teammate group = %+v", groups["teammate"])
	}
}

type roleSettingsFakeApplication struct {
	*fakeApplication
	account string
}

func (fake *roleSettingsFakeApplication) RolePermissionVector(_, _ string) ([]string, error) {
	return []string{}, nil
}

func (fake *roleSettingsFakeApplication) SetRolePermissionVector(_, _ string, _ []string) error {
	return nil
}

func (fake *roleSettingsFakeApplication) RoleDefaultVector(_, _ string) ([]string, bool, error) {
	return []string{}, false, nil
}

func (fake *roleSettingsFakeApplication) SetRoleDefaultVector(_, _ string, _ []string) error {
	return nil
}

func (fake *roleSettingsFakeApplication) RoleAccount(_, _ string) (string, bool, error) {
	return fake.account, fake.account != "", nil
}

func (fake *roleSettingsFakeApplication) SetRoleAccount(_, _, account string) error {
	fake.account = account
	return nil
}

func TestBridgeSetRoleAccountRejectsCrossRoleAccount(t *testing.T) {
	base := newFakeApplication()
	base.snapshot.Session.ID = "sess-role-account"
	base.snapshot.Runtime.Accounts = []model.AccountInfo{
		{Name: "agent-1", Role: "agent"},
		{Name: "teammate-1", Role: "teammate"},
	}
	fake := &roleSettingsFakeApplication{fakeApplication: base}
	bridge, err := NewBridge(fake, Options{})
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
