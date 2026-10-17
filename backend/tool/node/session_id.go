package node

import (
	"fmt"

	"github.com/RedHuang-0622/seelex/internal/stablehash"
)

// NodeSessionID 派生节点会话 ID：以系统提示（节点目标）为种子做稳定哈希；
// 同一节点路径可复现（供 checkpoints 定位）；空提示返回空串，让 Session
// 自动生成不透明 ID。
func NodeSessionID(systemPrompt string) string {
	if systemPrompt == "" {
		return ""
	}
	return fmt.Sprintf("node-%x", stablehash.FNV1a32(systemPrompt))
}
