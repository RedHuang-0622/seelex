package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

// environments_test.go 钉住项目侧多环境表的口径：
//  1. 环境名 → 端点，逐环境口令分开存（给 dev 存的口令不能被 prod 读到）；
//  2. 第一个环境自动成为默认；默认可以显式改，但不能指向不存在的环境；
//  3. 空端点保存 = 本地环境（不经过 SSH）；删除必须显式调用删除面；
//  4. 换一个 Repo 实例（重新读盘）也读得回来。
func newEnvironmentRepo(t *testing.T) (*Repo, string) {
	t.Helper()
	dir := t.TempDir()
	repo, err := NewRepoWithStore(dir)
	if err != nil {
		t.Fatalf("NewRepoWithStore: %v", err)
	}
	workspace, err := repo.Create("app", dir, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return repo, workspace.ID
}

func TestEnvironmentTableKeepsPasswordsPerEnvironment(t *testing.T) {
	repo, id := newEnvironmentRepo(t)
	devPass := "dev-secret"
	if _, err := repo.SetEnvironment(id, "dev", Environment{SSHEndpoint: SSHEndpoint{Host: "dev.example.com", User: "builder", Port: 22}}, &devPass); err != nil {
		t.Fatalf("SetEnvironment(dev): %v", err)
	}
	prodPass := "prod-secret"
	if _, err := repo.SetEnvironment(id, "prod", Environment{SSHEndpoint: SSHEndpoint{Host: "prod.example.com", User: "root", Port: 2202, RemoteRoot: "/srv/app"}}, &prodPass); err != nil {
		t.Fatalf("SetEnvironment(prod): %v", err)
	}

	list, defaultEnv, err := repo.Environments(id)
	if err != nil {
		t.Fatalf("Environments: %v", err)
	}
	if len(list) != 2 || list[0].Name != "dev" || list[1].Name != "prod" {
		t.Fatalf("环境表 = %#v, want [dev prod]（按名字升序）", list)
	}
	if defaultEnv != "dev" {
		t.Fatalf("默认环境 = %q, want dev（第一个写入的自动成为默认）", defaultEnv)
	}
	if !list[0].PasswordSet || !list[1].PasswordSet {
		t.Fatalf("两个环境都该报告已设口令: %#v", list)
	}
	if list[1].RemoteRoot != "/srv/app" {
		t.Fatalf("prod 远端根 = %q, want /srv/app", list[1].RemoteRoot)
	}

	// 逐环境口令互不串
	gotDev, err := repo.EnvironmentPassword(id, "dev")
	if err != nil || gotDev != "dev-secret" {
		t.Fatalf("dev 口令 = %q (err=%v), want dev-secret", gotDev, err)
	}
	gotProd, err := repo.EnvironmentPassword(id, "prod")
	if err != nil || gotProd != "prod-secret" {
		t.Fatalf("prod 口令 = %q (err=%v), want prod-secret", gotProd, err)
	}

	// 重新读盘仍在
	reopened, err := NewRepoWithStore(filepath.Dir(repo.savePath))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	again, defaultAgain, err := reopened.Environments(id)
	if err != nil {
		t.Fatalf("Environments(reopen): %v", err)
	}
	if len(again) != 2 || defaultAgain != "dev" {
		t.Fatalf("重新读盘 = %#v default=%q, want 2 个环境 + dev 默认", again, defaultAgain)
	}
	// 索引文件里不许出现口令明文
	raw, err := os.ReadFile(repo.savePath)
	if err != nil {
		t.Fatalf("ReadFile(index): %v", err)
	}
	if contains(string(raw), "dev-secret") || contains(string(raw), "prod-secret") {
		t.Fatalf("口令不该进索引文件: %s", raw)
	}
}

func TestEnvironmentDefaultValidation(t *testing.T) {
	repo, id := newEnvironmentRepo(t)
	pass := "x"
	if _, err := repo.SetEnvironment(id, "dev", Environment{SSHEndpoint: SSHEndpoint{Host: "dev.example.com", User: "u"}}, &pass); err != nil {
		t.Fatalf("SetEnvironment: %v", err)
	}
	if err := repo.SetDefaultEnvironment(id, "prod"); err == nil {
		t.Fatal("默认环境指向不存在的环境必须报错")
	}
	if err := repo.SetDefaultEnvironment(id, ""); err != nil {
		t.Fatalf("清空默认该允许: %v", err)
	}
	_, defaultEnv, err := repo.Environments(id)
	if err != nil {
		t.Fatalf("Environments: %v", err)
	}
	if defaultEnv != "" {
		t.Fatalf("默认环境 = %q, want 空", defaultEnv)
	}
}

func TestEnvironmentEmptyEndpointPersistsAsLocal(t *testing.T) {
	repo, id := newEnvironmentRepo(t)
	pass := "x"
	if _, err := repo.SetEnvironment(id, "dev", Environment{SSHEndpoint: SSHEndpoint{Host: "dev.example.com", User: "u"}}, &pass); err != nil {
		t.Fatalf("SetEnvironment: %v", err)
	}
	if _, err := repo.SetEnvironment(id, "dev", Environment{}, nil); err != nil {
		t.Fatalf("空端点应保存为本地环境: %v", err)
	}
	list, defaultEnv, err := repo.Environments(id)
	if err != nil {
		t.Fatalf("Environments: %v", err)
	}
	if len(list) != 1 || list[0].Name != "dev" || list[0].Host != "" || defaultEnv != "dev" {
		t.Fatalf("本地环境 = %#v default=%q, want dev + 空 host", list, defaultEnv)
	}
	if list[0].PasswordSet {
		t.Fatal("本地环境不该保留 SSH 口令")
	}
	if err := repo.DeleteEnvironment(id, "dev"); err != nil {
		t.Fatalf("显式删除: %v", err)
	}
	list, defaultEnv, _ = repo.Environments(id)
	if len(list) != 0 || defaultEnv != "" {
		t.Fatalf("删除后 = %#v default=%q", list, defaultEnv)
	}
}

func TestEnvironmentNameValidation(t *testing.T) {
	repo, id := newEnvironmentRepo(t)
	pass := "x"
	bad := []string{"", "  ", "dev prod", "dev/prod", "环境"}
	for _, name := range bad {
		if _, err := repo.SetEnvironment(id, name, Environment{SSHEndpoint: SSHEndpoint{Host: "h", User: "u"}}, &pass); err == nil {
			t.Fatalf("环境名 %q 该被拒绝", name)
		}
	}
	good := []string{"dev", "test", "staging-cn", "prod.eu", "env_1"}
	for _, name := range good {
		if _, err := repo.SetEnvironment(id, name, Environment{SSHEndpoint: SSHEndpoint{Host: "h", User: "u"}}, &pass); err != nil {
			t.Fatalf("环境名 %q 该被接受: %v", name, err)
		}
	}
}

func TestLocalEnvironmentRejectsSSHPasswordAndSurvivesReload(t *testing.T) {
	repo, id := newEnvironmentRepo(t)
	password := "should-not-exist"
	if _, err := repo.SetEnvironment(id, "dev", Environment{}, &password); err == nil {
		t.Fatal("本地环境不该接受 SSH 口令")
	}
	if _, err := repo.SetEnvironment(id, "dev", Environment{Note: "local checkout"}, nil); err != nil {
		t.Fatalf("SetEnvironment(local): %v", err)
	}
	reopened, err := NewRepoWithStore(filepath.Dir(repo.savePath))
	if err != nil {
		t.Fatal(err)
	}
	list, defaultEnv, err := reopened.Environments(id)
	if err != nil || len(list) != 1 || list[0].Host != "" || list[0].Note != "local checkout" || defaultEnv != "dev" {
		t.Fatalf("list=%+v default=%q err=%v", list, defaultEnv, err)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
