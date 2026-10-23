package workspace

// connection_test.go —— 项目级连接配置的三条硬判据：
//   1. 索引文件里**不含口令明文**（口令只在密文库，且是密文）；
//   2. 读面只给 `*Set` 布尔，明文只有运行时消费面能取；
//   3. 口令三态：nil = 不改 / 非空 = 写入 / 指向空串 = 清除。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestRepo 建一个落盘到临时目录的 Repo，返回它和索引/密文两个路径。
func newTestRepo(t *testing.T) (*Repo, string, string) {
	t.Helper()
	dir := t.TempDir()
	repo, err := NewRepoWithStore(dir)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}
	return repo, filepath.Join(dir, "workspace_index.json"), filepath.Join(dir, "workspace_secrets.json")
}

// seedWorkspace 造一个真实存在的项目目录并登记，返回工作区 ID。
func seedWorkspace(t *testing.T, repo *Repo) string {
	t.Helper()
	root := t.TempDir()
	info, err := repo.Create("演示项目", root, "")
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	return info.ID
}

func TestSetConnectionKeepsPasswordOutOfIndex(t *testing.T) {
	repo, indexPath, secretPath := newTestRepo(t)
	id := seedWorkspace(t, repo)

	const (
		sshPass = "P@ssw0rd-should-never-appear-in-index"
		gitPass = "ghp_token_should_never_appear_in_index"
	)
	password := sshPass
	gitPassword := gitPass
	conn, err := repo.SetConnection(id, ConnectionUpdate{
		SSH:         &SSHEndpoint{Host: "deploy.example.internal", User: "deploy"},
		SSHPassword: &password,
		Git:         &GitAccess{Username: "git-bot", Kind: "https"},
		GitPassword: &gitPassword,
	})
	if err != nil {
		t.Fatalf("set connection: %v", err)
	}

	// 端口默认补 22；读面只报"有没有口令"
	if conn.SSH == nil || conn.SSH.Port != 22 {
		t.Fatalf("ssh port default = %+v, want 22", conn.SSH)
	}
	if !conn.SSHPass || !conn.GitPass {
		t.Fatalf("password flags = ssh:%v git:%v, want both true", conn.SSHPass, conn.GitPass)
	}

	// 判据 1：索引文件里没有明文
	indexRaw, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	for _, secret := range []string{sshPass, gitPass} {
		if strings.Contains(string(indexRaw), secret) {
			t.Fatalf("index file must not contain secrets, found %q", secret)
		}
	}
	if !strings.Contains(string(indexRaw), "deploy.example.internal") {
		t.Fatal("index file should carry the non-secret endpoint")
	}

	// 判据 1b：密文文件里也没有明文（值必须是密文/编码串）
	secretRaw, err := os.ReadFile(secretPath)
	if err != nil {
		t.Fatalf("read secrets: %v", err)
	}
	for _, secret := range []string{sshPass, gitPass} {
		if strings.Contains(string(secretRaw), secret) {
			t.Fatalf("secrets file must not contain plaintext, found %q", secret)
		}
	}

	// 判据 2：运行时消费面能取到原文
	gotSSH, err := repo.SSHPassword(id)
	if err != nil {
		t.Fatalf("read ssh password: %v", err)
	}
	if gotSSH != sshPass {
		t.Fatalf("ssh password = %q, want round-trip", gotSSH)
	}
	gotGit, err := repo.GitPassword(id)
	if err != nil {
		t.Fatalf("read git password: %v", err)
	}
	if gotGit != gitPass {
		t.Fatalf("git password = %q, want round-trip", gotGit)
	}
}

func TestConnectionPasswordTriState(t *testing.T) {
	repo, _, _ := newTestRepo(t)
	id := seedWorkspace(t, repo)

	original := "original-secret"
	if _, err := repo.SetConnection(id, ConnectionUpdate{
		SSH:         &SSHEndpoint{Host: "h.example", User: "u"},
		SSHPassword: &original,
	}); err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	// nil ⇒ 不改：只改 host，口令必须还在
	if _, err := repo.SetConnection(id, ConnectionUpdate{
		SSH: &SSHEndpoint{Host: "h2.example", User: "u"},
	}); err != nil {
		t.Fatalf("update host: %v", err)
	}
	if got, _ := repo.SSHPassword(id); got != original {
		t.Fatalf("nil password must keep the old one, got %q", got)
	}

	// 指向空串 ⇒ 清除
	empty := ""
	conn, err := repo.SetConnection(id, ConnectionUpdate{SSHPassword: &empty})
	if err != nil {
		t.Fatalf("clear password: %v", err)
	}
	if conn.SSHPass {
		t.Fatal("ssh_password_set must be false after clearing")
	}
	if got, _ := repo.SSHPassword(id); got != "" {
		t.Fatalf("cleared password = %q, want empty", got)
	}
}

func TestSetConnectionValidation(t *testing.T) {
	repo, _, _ := newTestRepo(t)
	id := seedWorkspace(t, repo)

	cases := []struct {
		name    string
		update  ConnectionUpdate
		wantErr bool
	}{
		{"缺 user", ConnectionUpdate{SSH: &SSHEndpoint{Host: "h"}}, true},
		{"缺 host", ConnectionUpdate{SSH: &SSHEndpoint{User: "u"}}, true},
		{"端口越界", ConnectionUpdate{SSH: &SSHEndpoint{Host: "h", User: "u", Port: 70000}}, true},
		{"git 方式非法", ConnectionUpdate{Git: &GitAccess{Username: "x", Kind: "ftp"}}, true},
		{"空端点 = 清空", ConnectionUpdate{SSH: &SSHEndpoint{}}, false},
		{"正常", ConnectionUpdate{SSH: &SSHEndpoint{Host: "h", User: "u", Port: 2222}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := repo.SetConnection(id, tc.update)
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestDeleteDropsSecrets(t *testing.T) {
	repo, _, secretPath := newTestRepo(t)
	id := seedWorkspace(t, repo)
	password := "to-be-forgotten"
	if _, err := repo.SetConnection(id, ConnectionUpdate{
		SSH:         &SSHEndpoint{Host: "h", User: "u"},
		SSHPassword: &password,
	}); err != nil {
		t.Fatalf("set connection: %v", err)
	}
	if err := repo.Delete(id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	raw, err := os.ReadFile(secretPath)
	if err != nil {
		t.Fatalf("read secrets: %v", err)
	}
	if strings.Contains(string(raw), id) {
		t.Fatalf("secrets for %q must be dropped with the workspace: %s", id, raw)
	}
}

// TestConnectionSurvivesReload 证明重启后连接配置还在（索引 + 密文档都要读回）。
func TestConnectionSurvivesReload(t *testing.T) {
	dir := t.TempDir()
	repo, err := NewRepoWithStore(dir)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}
	id := seedWorkspace(t, repo)
	password := "persisted-secret"
	if _, err := repo.SetConnection(id, ConnectionUpdate{
		SSH:         &SSHEndpoint{Host: "h.example", User: "u", Port: 2200},
		SSHPassword: &password,
		Git:         &GitAccess{Username: "bot", Kind: "ssh"},
	}); err != nil {
		t.Fatalf("set connection: %v", err)
	}

	reloaded, err := NewRepoWithStore(dir)
	if err != nil {
		t.Fatalf("reload repo: %v", err)
	}
	conn, err := reloaded.Connection(id)
	if err != nil {
		t.Fatalf("connection after reload: %v", err)
	}
	if conn.SSH == nil || conn.SSH.Host != "h.example" || conn.SSH.Port != 2200 {
		t.Fatalf("ssh endpoint lost across reload: %+v", conn.SSH)
	}
	if conn.Git == nil || conn.Git.Username != "bot" || conn.Git.Kind != "ssh" {
		t.Fatalf("git access lost across reload: %+v", conn.Git)
	}
	if !conn.SSHPass {
		t.Fatal("ssh password must survive reload")
	}
	if got, _ := reloaded.SSHPassword(id); got != password {
		t.Fatalf("password after reload = %q", got)
	}
}
