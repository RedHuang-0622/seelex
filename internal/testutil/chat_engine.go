// Package testutil 提供仅供测试使用的共享桩：测试引擎嵌入未实现即 panic
// 的底座，只覆写测试路径真正用到的方法。给 ChatEngine 接口新增方法时，
// 只需在 EmbeddedChatEngine 补一个 panic 实现，各测试桩自动满足接口。
package testutil

import (
	"context"
	"fmt"

	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

// EmbeddedChatEngine 是 ChatEngine 的可嵌底座：所有方法默认 panic，测试
// 引擎以 struct 内嵌方式继承，仅覆写所使用的方法（Go 惯用 embedded stub）。
type EmbeddedChatEngine struct{}

func panicUnimplemented(method string) {
	panic(fmt.Sprintf("testutil.EmbeddedChatEngine: %s is not implemented; override it in the embedding test engine", method))
}

func (*EmbeddedChatEngine) ChatStream(context.Context, string, func(string)) (string, error) {
	panicUnimplemented("ChatStream")
	return "", nil
}
func (*EmbeddedChatEngine) History() []contract.EngineMessage {
	panicUnimplemented("History")
	return nil
}
func (*EmbeddedChatEngine) ClearHistory() { panicUnimplemented("ClearHistory") }
func (*EmbeddedChatEngine) ReplaceHistory(string, []contract.EngineMessage) error {
	panicUnimplemented("ReplaceHistory")
	return nil
}
func (*EmbeddedChatEngine) SessionID() string {
	panicUnimplemented("SessionID")
	return ""
}
func (*EmbeddedChatEngine) StartSession() string {
	panicUnimplemented("StartSession")
	return ""
}
func (*EmbeddedChatEngine) SetSystemPrompt(string) { panicUnimplemented("SetSystemPrompt") }
func (*EmbeddedChatEngine) SetMaxLoops(int)        { panicUnimplemented("SetMaxLoops") }
func (*EmbeddedChatEngine) TraceText() string {
	panicUnimplemented("TraceText")
	return ""
}
func (*EmbeddedChatEngine) TokenCount() string {
	panicUnimplemented("TokenCount")
	return ""
}
func (*EmbeddedChatEngine) AppendHistory(types.Message) { panicUnimplemented("AppendHistory") }
func (*EmbeddedChatEngine) NodeSessionConversation(string) ([]types.Message, bool) {
	panicUnimplemented("NodeSessionConversation")
	return nil, false
}
func (*EmbeddedChatEngine) NodeContextSnapshot(string) (*snapshot.ContextSnapshot, bool) {
	panicUnimplemented("NodeContextSnapshot")
	return nil, false
}
func (*EmbeddedChatEngine) NodeToolResult(string, string) (string, bool) {
	panicUnimplemented("NodeToolResult")
	return "", false
}
func (*EmbeddedChatEngine) NodeWorktreeInfoFor(string) (dto.NodeWorktreeInfo, bool) {
	panicUnimplemented("NodeWorktreeInfoFor")
	return dto.NodeWorktreeInfo{}, false
}
func (*EmbeddedChatEngine) SubscribeSubagentLive(string) ([]dto.SubagentLiveEvent, <-chan dto.SubagentLiveEvent, func(), error) {
	panicUnimplemented("SubscribeSubagentLive")
	return nil, nil, func() {}, nil
}
func (*EmbeddedChatEngine) SubAgentTree() []dto.SubAgentTreeNode {
	panicUnimplemented("SubAgentTree")
	return nil
}
