package seelebridge

import (
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
	"github.com/RedHuang-0622/seelex/skill"
)

// 节点域已迁入 seelebridge/node（Coordinator：会话注册、fork 树终态、task
// 打点、plan 阶段事件、PromptBlocks/预算/skill 目录）。本文件只保留 Runtime
// 公开端口委托，跨域协作由 Coordinator 的 Deps 闭包注入。

// NodeSessionConversation 返回节点子代理的会话记录：运行中 → 子会话
// History（实时）；已结束 → 最后快照。只读子代理 actor，绝不触碰主会话。
func (r *Runtime) NodeSessionConversation(nodeID string) ([]types.Message, bool) {
	if r == nil || r.node == nil {
		return nil, false
	}
	return r.node.Conversation(nodeID)
}

// NodeContextSnapshot 返回节点子代理的结构化上下文快照（详情弹窗"上下文"
// 标签）：运行中实时导出（Goal/Findings/Decisions/TokenEstimate，同
// mergeBack 导出面）；已结束返回结束时刻快照。
func (r *Runtime) NodeContextSnapshot(nodeID string) (*snapshot.ContextSnapshot, bool) {
	if r == nil || r.node == nil {
		return nil, false
	}
	return r.node.ContextSnapshot(nodeID)
}

// SetSkillRegistry 装配子代理 skill 目录 actor（skill.Registry 自带锁，
// 读写经其方法进出；装配一次性写入、运行期只读消费，与 filesystem actor
// 同构，无需外层锁）。传入 nil 关闭 skill 块（降级）。
func (r *Runtime) SetSkillRegistry(registry *skill.Registry) {
	if r == nil || r.node == nil {
		return
	}
	r.node.SetSkills(registry)
}
