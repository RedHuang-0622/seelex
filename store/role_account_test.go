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

func TestRoleInheritanceAndUserSpacesShareRecordWithoutErasingVector(t *testing.T) {
	router := openRolePermissionRouter(t)
	store := NewRolePermissionStore(router)
	if _, err := store.PutDefault("project-a", "teammate", []string{"read", "ssh.userspace"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRoleInheritance("project-a", "teammate", true); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRoleSSHUserSpaces("project-a", "teammate", []string{"/home/seelex/.cache"}); err != nil {
		t.Fatal(err)
	}
	reloaded := NewRolePermissionStore(router)
	inherit, explicit, err := reloaded.RoleInheritance("project-a", "teammate")
	if err != nil || !explicit || !inherit {
		t.Fatalf("inherit=%v explicit=%v err=%v", inherit, explicit, err)
	}
	spaces, err := reloaded.RoleSSHUserSpaces("project-a", "teammate")
	if err != nil || len(spaces) != 1 || spaces[0] != "/home/seelex/.cache" {
		t.Fatalf("spaces=%v err=%v", spaces, err)
	}
	vector, ok, err := reloaded.Default("project-a", "teammate")
	if err != nil || !ok || len(vector) != 2 {
		t.Fatalf("权限向量被选项写入擦除: %v %v %v", vector, ok, err)
	}
}
