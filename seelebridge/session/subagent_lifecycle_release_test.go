package session

import (
	"context"
	"testing"

	frameworkSession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/seelex/seelebridge/fork"
	"github.com/RedHuang-0622/seelex/seelexctx"
)

// subagent_lifecycle_release_test.go — 子代理"完成后"的生命周期释放验收：
//
//  1. 终态写入（CompleteSubagentNode）后，节点记录不再持有 framework Session
//     引用——那个对象图带着整条 ReAct 历史 + 最后一次 loop 的 trace 树，是每个
//     已完成子代理最大的一块常驻内存；
//  2. 节点注销（Unregister）后，超大三工具结果归档（archiver）被释放并从注册表
//     摘除——被折叠的结果原文只在节点活着时有消费者。

// TestSubagentTreeReleasesLiveSessionOnComplete 钉住终态即摘活会话引用。
func TestSubagentTreeReleasesLiveSessionOnComplete(t *testing.T) {
	tree := NewSubagentTree(nil)
	tree.RegisterFork("", []fork.SubagentSpec{{ID: "node-1"}, {ID: "node-2"}})
	tree.NoteSession("node-1", &frameworkSession.Session{})
	if record := tree.nodes["node-1"]; record == nil || record.session == nil {
		t.Fatal("活跃节点应当持有活会话引用（运行期实时投影要用）")
	}

	tree.CompleteSubagentNode("node-1", "done", nil)

	record := tree.nodes["node-1"]
	if record == nil {
		t.Fatal("终态记录应当保留（工作表格证据面）")
	}
	if record.status != SubAgentDone {
		t.Fatalf("status = %v, want done", record.status)
	}
	if record.session != nil {
		t.Fatal("终态后必须摘掉活会话引用，否则整个会话对象图不可回收")
	}

	// 失败路径同样摘引用（现场由错误信息 + 阶段日志保留）。
	tree.NoteSession("node-2", &frameworkSession.Session{})
	tree.CompleteSubagentNode("node-2", "", context.DeadlineExceeded)
	if record := tree.nodes["node-2"]; record.session != nil {
		t.Fatal("失败终态同样要摘掉活会话引用")
	}
}

// TestSubagentSessionsReleaseArchiverOnUnregister 钉住注销即释放结果归档。
func TestSubagentSessionsReleaseArchiverOnUnregister(t *testing.T) {
	sessions := NewSubagentSessions(nil)
	sessions.Register("node-1", &frameworkSession.Session{}, "goal")
	archiver := sessions.ToolResultArchiverFor("node-1")
	if archiver == nil {
		t.Fatal("已注册节点应当有归档器")
	}
	if _, err := archiver.Store(context.Background(), "call-1", "read_file", "超大工具结果原文"); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if archiver.RetainedBytes() == 0 {
		t.Fatal("归档后应当有保留字节")
	}

	sessions.Unregister("node-1")

	if archiver.RetainedBytes() != 0 {
		t.Fatalf("注销后归档内容应当被释放，仍保留 %d 字节", archiver.RetainedBytes())
	}
	if _, ok := sessions.ToolResult("node-1", "result:call-1"); ok {
		t.Fatal("注销后不该再能读出被折叠的结果原文")
	}
}

// TestToolResultArchiverReleaseIsIdempotent 归档释放可重复调用且返回释放字节数。
func TestToolResultArchiverReleaseIsIdempotent(t *testing.T) {
	archiver := seelexctx.NewInMemoryToolResultArchiver()
	if _, err := archiver.Store(context.Background(), "c1", "bash", "0123456789"); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if _, err := archiver.Store(context.Background(), "c2", "bash", "abcdef"); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if got := archiver.RetainedBytes(); got != 16 {
		t.Fatalf("RetainedBytes = %d, want 16", got)
	}
	if freed := archiver.Release(); freed != 16 {
		t.Fatalf("Release 释放字节 = %d, want 16", freed)
	}
	if freed := archiver.Release(); freed != 0 {
		t.Fatalf("重复 Release 应当释放 0，got %d", freed)
	}
	if _, ok := archiver.Read("result:c1"); ok {
		t.Fatal("释放后不该再读出内容")
	}
}
