package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/RedHuang-0622/seelex/seelebridge/fs"
	"github.com/RedHuang-0622/seelex/seelebridge/security"
)

// sessionKeyContextKey 是本用例的会话键（生产为 telemetry 会话键；此处不
// import 该包以免形成 tools → telemetry → tools 的测试期 import cycle）。
type sessionKeyContextKey struct{}

// TestScopedToolsUseSessionRoot 是「工作区污染」的工具面回归：进程默认根指向
// 视图会话的项目，执行 ctx 属于另一个会话时，read_file/write_file 必须落在该
// 会话自己的项目根内，不得借用视图会话的根。
func TestScopedToolsUseSessionRoot(t *testing.T) {
	viewRoot := t.TempDir()
	sessionRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(viewRoot, "view.txt"), []byte("view"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionRoot, "own.txt"), []byte("own"), 0o644); err != nil {
		t.Fatal(err)
	}

	scope := security.NewProjectScope()
	if err := scope.Bind(viewRoot); err != nil {
		t.Fatalf("bind view root: %v", err)
	}
	if err := scope.BindFor("session-a", sessionRoot); err != nil {
		t.Fatalf("bind session root: %v", err)
	}
	router := NewRouter(Deps{
		ProjectScope: scope,
		SessionKey: func(ctx context.Context) string {
			sessionID, _ := ctx.Value(sessionKeyContextKey{}).(string)
			return sessionID
		},
		FileSystem: fs.NewFileSystemActor(),
	})
	ctx := context.WithValue(context.Background(), sessionKeyContextKey{}, "session-a")

	// 读：会话 A 只能读到自己根内的文件。
	output, err := router.scopedReadFile(ctx, `{"path":"own.txt"}`)
	if err != nil || output != "own" {
		t.Fatalf("会话 A 读 own.txt = %q, err=%v", output, err)
	}
	if _, err := router.scopedReadFile(ctx, `{"path":"view.txt"}`); err == nil {
		t.Fatal("会话 A 不得读到视图会话项目根内的文件（工作区污染）")
	}

	// 写：落点必须在会话 A 自己的根，视图根不能被写脏。
	if _, err := router.scopedWriteFile(ctx, `{"path":"out/note.txt","content":"hello"}`); err != nil {
		t.Fatalf("会话 A 写文件: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sessionRoot, "out", "note.txt")); err != nil {
		t.Fatalf("写入未落在会话自己的项目根：%v", err)
	}
	if _, err := os.Stat(filepath.Join(viewRoot, "out", "note.txt")); err == nil {
		t.Fatal("写入泄漏到视图会话的项目根（工作区污染）")
	}
}
