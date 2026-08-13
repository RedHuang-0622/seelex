package seelebridge

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	seenode "github.com/RedHuang-0622/seelex/seelebridge/node"
	"github.com/RedHuang-0622/seelex/seelexctx"
)

// TestNodeToolResultArchiverPrefixAndReadback 验证 P1 桥：节点会话的工具
// 结果落到节点专属归档（ref 带 node:<nodeID>: 前缀），运行中/结束后均可
// 经 Runtime.NodeToolResult 读回；非节点上下文回退共享归档（无前缀）。
func TestNodeToolResultArchiverPrefixAndReadback(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()

	arch := seenode.ToolResultArchiver{ArchiverFor: runtime.node.ToolResultArchiverFor, Shared: seelexctx.NewInMemoryToolResultArchiver()}
	nodeCtx := seenode.WithNodeScope(context.Background(), seenode.NodeScope{
		NodeID: "agent-1", Role: model.RoleSubAgent, BranchID: "agent-1",
	})

	// 节点上下文：ref 带前缀，可读回原始内容。
	ref, err := arch.Store(nodeCtx, "call-1", "read_file", "窗口内容-abcdef")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if !strings.HasPrefix(ref, "node:agent-1:result:") {
		t.Fatalf("node ref must carry prefix, got %q", ref)
	}
	raw, ok := runtime.NodeToolResult("agent-1", ref)
	if !ok || raw != "窗口内容-abcdef" {
		t.Fatalf("NodeToolResult readback = %q ok=%v", raw, ok)
	}
	// 错误节点/错误 ref → 读不到。
	if _, ok := runtime.NodeToolResult("other", ref); ok {
		t.Fatal("wrong node must not resolve ref")
	}
	if _, ok := runtime.NodeToolResult("agent-1", "node:agent-1:result:missing"); ok {
		t.Fatal("unknown ref must not resolve")
	}

	// 非节点上下文：走共享归档，无 node: 前缀。
	plain, err := arch.Store(context.Background(), "call-2", "read_file", "shared")
	if err != nil {
		t.Fatalf("store plain: %v", err)
	}
	if strings.HasPrefix(plain, "node:") {
		t.Fatalf("plain ref must not carry node prefix, got %q", plain)
	}
	// 同节点再存：同一归档幂等读回（call-1 仍在）。
	ref2, err := arch.Store(nodeCtx, "call-1", "read_file", "窗口内容-abcdef")
	if err != nil || ref2 != ref {
		t.Fatalf("idempotent store = %q err=%v, want %q", ref2, err, ref)
	}
}

func TestNodeToolResultNilSafety(t *testing.T) {
	var runtime *Runtime
	if _, ok := runtime.NodeToolResult("n", "node:n:result:x"); ok {
		t.Fatal("nil runtime must not resolve")
	}
	if _, ok := runtime.NodeToolResult("", "node:x:result:y"); ok {
		t.Fatal("empty node id must not resolve")
	}
}
