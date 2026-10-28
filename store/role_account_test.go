package store

import "testing"

func TestRoleAccountPersistsWithPermissionDefault(t *testing.T) {
	router := openRolePermissionRouter(t)
	store := newRolePermissionStore(router)
	if _, err := store.PutDefault("project-a", "teammate", []string{"ro"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRoleAccount("project-a", "teammate", "teammate-2"); err != nil {
		t.Fatal(err)
	}
	account, ok, err := newRolePermissionStore(router).RoleAccount("project-a", "teammate")
	if err != nil || !ok || account != "teammate-2" {
		t.Fatalf("account=%q ok=%v err=%v", account, ok, err)
	}
	vector, ok, err := newRolePermissionStore(router).Default("project-a", "teammate")
	if err != nil || !ok || len(vector) != 1 || vector[0] != "ro" {
		t.Fatalf("权限默认被账号设置覆盖: vector=%v ok=%v err=%v", vector, ok, err)
	}
}

func TestRoleInheritanceAndUserSpacesShareRecordWithoutErasingVector(t *testing.T) {
	router := openRolePermissionRouter(t)
	store := newRolePermissionStore(router)
	if _, err := store.PutDefault("project-a", "teammate", []string{"read", "ssh"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRoleInheritance("project-a", "teammate", true); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRoleSSHUserSpaces("project-a", "teammate", []string{"/home/seelex/.cache"}); err != nil {
		t.Fatal(err)
	}
	reloaded := newRolePermissionStore(router)
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

func TestPermissionVectorUpdatePreservesRoleRecordFields(t *testing.T) {
	router := openRolePermissionRouter(t)
	store := newRolePermissionStore(router)
	if _, err := store.PutDefault("project-a", "teammate", []string{"read"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRoleAccount("project-a", "teammate", "teammate-2"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRoleInheritance("project-a", "teammate", true); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRoleSSHUserSpaces("project-a", "teammate", []string{"/srv/shared"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutDefault("project-a", "teammate", []string{"read", "ssh"}); err != nil {
		t.Fatal(err)
	}

	reloaded := newRolePermissionStore(router)
	account, ok, err := reloaded.RoleAccount("project-a", "teammate")
	if err != nil || !ok || account != "teammate-2" {
		t.Fatalf("权限更新擦掉账号: account=%q ok=%v err=%v", account, ok, err)
	}
	inherit, explicit, err := reloaded.RoleInheritance("project-a", "teammate")
	if err != nil || !explicit || !inherit {
		t.Fatalf("权限更新擦掉继承: inherit=%v explicit=%v err=%v", inherit, explicit, err)
	}
	paths, err := reloaded.RoleSSHUserSpaces("project-a", "teammate")
	if err != nil || len(paths) != 1 || paths[0] != "/srv/shared" {
		t.Fatalf("权限更新擦掉 SSH 路径: paths=%v err=%v", paths, err)
	}
}

func TestSessionSSHPathsOverrideDefaultAndSurviveVectorUpdate(t *testing.T) {
	router := openRolePermissionRouter(t)
	store := newRolePermissionStore(router)
	if err := store.SetRoleSSHUserSpaces("project-a", "subagent", []string{"/srv/default"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRoleSessionSSHUserSpaces("project-a", "session-a", "subagent", []string{"/srv/session-a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put("project-a", "session-a", "subagent", []string{"read", "ssh"}); err != nil {
		t.Fatal(err)
	}

	reloaded := newRolePermissionStore(router)
	paths, err := reloaded.RoleEffectiveSSHUserSpaces("project-a", "session-a", "subagent")
	if err != nil || len(paths) != 1 || paths[0] != "/srv/session-a" {
		t.Fatalf("session-a paths=%v err=%v, want session override", paths, err)
	}
	paths, err = reloaded.RoleEffectiveSSHUserSpaces("project-a", "session-b", "subagent")
	if err != nil || len(paths) != 1 || paths[0] != "/srv/default" {
		t.Fatalf("session-b paths=%v err=%v, want role default", paths, err)
	}
}
