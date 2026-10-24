package store

import "testing"

func TestRoleAccountPersistsWithPermissionDefault(t *testing.T) {
	router := openRolePermissionRouter(t)
	store := NewRolePermissionStore(router)
	if _, err := store.PutDefault("project-a", "teammate", []string{"ro"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRoleAccount("project-a", "teammate", "teammate-2"); err != nil {
		t.Fatal(err)
	}
	account, ok, err := NewRolePermissionStore(router).RoleAccount("project-a", "teammate")
	if err != nil || !ok || account != "teammate-2" {
		t.Fatalf("account=%q ok=%v err=%v", account, ok, err)
	}
	vector, ok, err := NewRolePermissionStore(router).Default("project-a", "teammate")
	if err != nil || !ok || len(vector) != 1 || vector[0] != "ro" {
		t.Fatalf("权限默认被账号设置覆盖: vector=%v ok=%v err=%v", vector, ok, err)
	}
}
