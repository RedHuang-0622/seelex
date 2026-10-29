package task_context

import model "github.com/RedHuang-0622/seelex/contract/protocol"

// ContextDraftStateFor 返回会话 draft 的持久现场副本。
func (c *Coordinator) ContextDraftStateFor(sessionID string) model.ContextDraftState {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return cloneContextDraftState(c.sessionStateLocked(sessionID).contextDraft)
}

// ReplaceContextDraftStateFor 由 draft actor 的 durable 适配器在 ack 后调用。
// 它只替换持久现场，不判断提交语义。
func (c *Coordinator) ReplaceContextDraftStateFor(sessionID string, state model.ContextDraftState) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c.sessionStateLocked(sessionID).contextDraft = cloneContextDraftState(state)
}

func cloneContextDraftState(state model.ContextDraftState) model.ContextDraftState {
	return model.ContextDraftState{
		Items:       append([]model.ContextDraftItem(nil), state.Items...),
		Watermark:   state.Watermark,
		Uncommitted: state.Uncommitted,
	}
}
