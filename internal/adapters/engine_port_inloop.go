package adapters

import (
	"context"

	frameworkSession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/seelex/application/contract"
)

// 编译期断言：EnginePort 实现环内自持锁历史通道。把手只存在于 Session-backed
// 引擎的进行中回合里；非环内 ctx 一律 ok=false，调用方回落到常规方法。
var _ contract.InLoopEngine = (*EnginePort)(nil)

// HistoryInLoop 返回本轮引擎历史。ok=false = 该 ctx 不在进行中的回合里。
func (port *EnginePort) HistoryInLoop(ctx context.Context) ([]contract.EngineMessage, bool) {
	handle, ok := port.inLoopHandle(ctx)
	if !ok {
		return nil, false
	}
	history, err := handle.History()
	if err != nil {
		return nil, false
	}
	return adaptMessages(history), true
}

// ReplaceHistoryInLoop 就地替换本轮引擎历史。
//
// (false, nil) = 不在环内（调用方回落到 ReplaceHistoryFor）；(true, err) = 在环内
// 但被引擎拒绝（例如替换会丢掉正在飞的 tool_call 单元）——**此时不得回落**，
// 回落等于再去取一次本回合已持有的锁。
//
// 与锁外替换的唯一差别是不 arm「下一次装载」：out-of-loop 替换要经
// PrepareMainSessionHistory 把历史交给下一回合的加载点，而环内替换改的就是**这
// 一回**正在用的历史，本轮收尾时循环自己会持久化（Seele ReActLoop.Run 的
// saveToCache → DurableHistory.Save）。压缩帧因此当场进入下一次模型请求，不必
// 等下一次装载。
func (port *EnginePort) ReplaceHistoryInLoop(ctx context.Context, history []contract.EngineMessage) (bool, error) {
	handle, ok := port.inLoopHandle(ctx)
	if !ok {
		return false, nil
	}
	if err := handle.ReplaceHistory(canonicalEngineHistory(restoreMessages(history))); err != nil {
		return true, err
	}
	return true, nil
}

// SetSystemPromptInLoop 用 Seele 的同一实现（Session.setSystemPromptLocked）把
// system prompt 推进本轮历史，只是不再二次取会话锁。
//
// 它刻意**不等于** SetSystemPromptFor：后者顺手改写 port.systemPrompt（进程级
// 默认，供之后新建的引擎继承）并要拿 port.mu；环内折叠只是把「本会话此刻的
// prompt」送进引擎，既不该动进程级默认，也不该再碰那把全进程锁。
func (port *EnginePort) SetSystemPromptInLoop(ctx context.Context, prompt string) (bool, error) {
	handle, ok := port.inLoopHandle(ctx)
	if !ok {
		return false, nil
	}
	if err := handle.SetSystemPrompt(prompt); err != nil {
		return true, err
	}
	return true, nil
}

// inLoopHandle 取本轮把手；只有 sessionBacked 引擎（生产路径）可能取到。
func (port *EnginePort) inLoopHandle(ctx context.Context) (*frameworkSession.InLoop, bool) {
	if port == nil || !port.sessionBacked || ctx == nil {
		return nil, false
	}
	return frameworkSession.InLoopFrom(ctx)
}
