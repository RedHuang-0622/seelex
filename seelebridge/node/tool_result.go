package node

import (
	"context"

	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/seelexctx"
)

// ToolResultArchiver 是节点会话的 ToolResultArchiver 适配（P1 修复
// result_ref 读回断链）：从 ctx 的 NodeScope 解析节点 ID，工具结果落到节点
// 专属内存归档器并带 node: 前缀；非节点上下文回退共享归档（行为与旧实现
// 一致）。
type ToolResultArchiver struct {
	// ArchiverFor 返回节点专属归档器（惰性创建；由 Coordinator 提供）。
	ArchiverFor func(nodeID string) *seelexctx.InMemoryToolResultArchiver
	// Shared 是主会话共享归档器（非节点上下文回退）。
	Shared seelexctx.ToolResultArchiver
}

// Store 实现 seelexctx.ToolResultArchiver（幂等由底层归档器保证）。
func (a ToolResultArchiver) Store(ctx context.Context, callID, tool, raw string) (string, error) {
	if scope, ok := model.NodeScopeFromContext(ctx); ok && scope.NodeID != "" && scope.Role == model.RoleSubAgent {
		arch := a.ArchiverFor(scope.NodeID)
		if arch == nil {
			return "", nil
		}
		ref, err := arch.Store(ctx, callID, tool, raw)
		if err != nil {
			return "", err
		}
		return model.NodeResultRefPrefix + scope.NodeID + ":" + ref, nil
	}
	return a.Shared.Store(ctx, callID, tool, raw)
}
