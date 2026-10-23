package workspace

// connection_remote_root_test.go —— **远端工作目录**（RemoteRoot）的存储判据。
//
// 为什么它值得单独一组：这一格是路径门在远端面上的取根口径（根内不需要
// ssh.userspace，走出它才需要）。存错一个值，闸门就会judge错方向，而且不会有任何
// 报错面——所以"能存、能读回、坏值被拒"必须有用例钉住。

import (
	"strings"
	"testing"
)

// TestConnectionRemoteRootRoundTrip：远端工作目录能写进索引、能读回，且被 POSIX 归一。
func TestConnectionRemoteRootRoundTrip(t *testing.T) {
	dir := t.TempDir()
	repo, err := NewRepoWithStore(dir)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}
	id := seedWorkspace(t, repo)

	conn, err := repo.SetConnection(id, ConnectionUpdate{
		SSH: &SSHEndpoint{Host: "deploy.example", User: "deploy", RemoteRoot: "/srv/apps/../app/"},
	})
	if err != nil {
		t.Fatalf("set connection: %v", err)
	}
	if conn.SSH == nil || conn.SSH.RemoteRoot != "/srv/app" {
		t.Fatalf("远端工作目录 = %+v，want 归一为 /srv/app（path.Clean，不是 filepath）", conn.SSH)
	}

	// 读回（新 Repo 从盘上重读）必须还在：它是连接配置的一部分，不是内存态。
	reloaded, err := NewRepoWithStore(dir)
	if err != nil {
		t.Fatalf("reload repo: %v", err)
	}
	got, err := reloaded.Connection(id)
	if err != nil {
		t.Fatalf("connection after reload: %v", err)
	}
	if got.SSH == nil || got.SSH.RemoteRoot != "/srv/app" {
		t.Fatalf("重读后的远端工作目录 = %+v，want /srv/app（必须落盘）", got.SSH)
	}
}

// TestConnectionRemoteRootRejectsRelative：相对路径**直接拒**，不替人猜一个根。
//
// 猜一个根会让"越界"判错方向：猜大了把越界读成根内（闸门失效），猜小了把根内读成
// 越界（每次调用都被拒）。空（= 未知）是允许的，路径门会退回更保守的口径。
func TestConnectionRemoteRootRejectsRelative(t *testing.T) {
	repo, _, _ := newTestRepo(t)
	id := seedWorkspace(t, repo)

	for _, bad := range []string{"srv/app", "./app", `C:\app`, "../app"} {
		if _, err := repo.SetConnection(id, ConnectionUpdate{
			SSH: &SSHEndpoint{Host: "deploy.example", User: "deploy", RemoteRoot: bad},
		}); err == nil {
			t.Fatalf("远端工作目录 %q 应被拒（必须是对端 POSIX 绝对路径）", bad)
		} else if !strings.Contains(err.Error(), "absolute POSIX path") {
			t.Fatalf("拒绝文案应点明口径：%v", err)
		}
	}

	empty, err := repo.SetConnection(id, ConnectionUpdate{
		SSH: &SSHEndpoint{Host: "deploy.example", User: "deploy"},
	})
	if err != nil {
		t.Fatalf("不填远端工作目录应当允许（空 = 未知）：%v", err)
	}
	if empty.SSH == nil || empty.SSH.RemoteRoot != "" {
		t.Fatalf("未填的远端工作目录 = %+v，want 空", empty.SSH)
	}
}
