package sessionstore

import (
	"path/filepath"
	"testing"
)

// TestTeamGlobalPathLayout：全局母本三份文件都落在 `<root>/team/`（不随项目分目录）。
func TestTeamGlobalPathLayout(t *testing.T) {
	root := t.TempDir()
	store := newStoreEngine(root, storageSettings{})
	for _, tc := range []struct {
		name string
		got  string
		want string
	}{
		{"employees", store.teamGlobalEmployeesPath(), filepath.Join(root, "team", "employees.json")},
		{"order", store.teamGlobalOrderPath(), filepath.Join(root, "team", "order.json")},
	} {
		if tc.got != tc.want {
			t.Fatalf("%s path = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

// TestEmployeeLibraryRoundTrip：员工库整份往返、重复角色显式报错、未知 schema 拒绝、
// 排序稳定（OrderPriority → role_name）。
func TestEmployeeLibraryRoundTrip(t *testing.T) {
	router := newTestRouter(t)

	empty, err := router.ReadEmployeeLibraryGlobal()
	if err != nil {
		t.Fatalf("未建员工库读取不应报错：%v", err)
	}
	if empty.Configured || len(empty.Employees) != 0 {
		t.Fatalf("未建库应为空库：%+v", empty)
	}

	if err := router.WriteEmployeeLibraryGlobal(EmployeeLibrary{Employees: []TeamRoleSpec{
		{RoleName: "b", RoleKind: "agent", OrderPriority: 2},
		{RoleName: "a", RoleKind: "agent", OrderPriority: 1},
	}}); err != nil {
		t.Fatalf("write employee library: %v", err)
	}
	if err := router.WriteEmployeeLibraryGlobal(EmployeeLibrary{Employees: []TeamRoleSpec{
		{RoleName: "dup"}, {RoleName: "dup"},
	}}); err == nil {
		t.Fatal("重复员工必须显式报错")
	}
	if err := router.WriteEmployeeLibraryGlobal(EmployeeLibrary{SchemaVersion: 99}); err == nil {
		t.Fatal("未知 schema 必须被拒绝")
	}

	stored, err := router.ReadEmployeeLibraryGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Configured || len(stored.Employees) != 2 {
		t.Fatalf("读回员工库 = %+v", stored)
	}
	if stored.Employees[0].RoleName != "a" || stored.Employees[1].RoleName != "b" {
		t.Fatalf("员工库必须按 OrderPriority 排序：%+v", stored.Employees)
	}
}

// TestDefaultOrderRoundTrip：默认顺序整份往返、顺序表保序去空。
func TestDefaultOrderRoundTrip(t *testing.T) {
	router := newTestRouter(t)

	empty, err := router.ReadDefaultOrderGlobal()
	if err != nil {
		t.Fatalf("未建默认顺序读取不应报错：%v", err)
	}
	if empty.Configured {
		t.Fatalf("未建应为空：%+v", empty)
	}

	if err := router.WriteDefaultOrderGlobal(DefaultOrder{
		OrderPolicy: "user_main_decided",
		OrderRoles:  []string{"user", "main", "reviewer", "reviewer", ""},
	}); err != nil {
		t.Fatal(err)
	}
	stored, err := router.ReadDefaultOrderGlobal()
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Configured || stored.OrderPolicy != "user_main_decided" {
		t.Fatalf("读回默认顺序 = %+v", stored)
	}
	if len(stored.OrderRoles) != 3 || stored.OrderRoles[0] != "user" || stored.OrderRoles[2] != "reviewer" {
		t.Fatalf("顺序表必须保序去空去重：%v", stored.OrderRoles)
	}
	if err := router.WriteDefaultOrderGlobal(DefaultOrder{SchemaVersion: 99}); err == nil {
		t.Fatal("未知 schema 必须被拒绝")
	}
}
