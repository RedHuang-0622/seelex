package task_context

// RecordUserWireContentLocked 把「本轮实际发给 provider 的 user 正文」写回该轮
// 的 user transcript 事件，作为**已发出字节**记录（不变量 I1：字节冻结）。
//
// 为什么需要它：装配会把动态尾块（工作打点表、环境表等）拼进当前输入再发给
// provider，而 transcript 追加 user 事件时记的是**原始输入**。下一轮重投影按
// 记录重建，byte 自该消息起分叉——由于历史整体排在 system 之后，一处污染会让
// **整段历史**重算（实测见 docs/arch/context-prefix-stability-l1.md §6.3）。
//
// wire 与 Content 相同时不写 ProviderContent（保持记录紧凑，语义等价）。
// 幂等：重复调用写入同一个值。返回是否命中该轮的 user 事件。
func (c *Coordinator) RecordUserWireContentLocked(sessionID, requestID, wire string) bool {
	if c == nil || wire == "" {
		return false
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._RecordUserWireContentLocked(sessionID, requestID, wire)
}

func (c *Coordinator) _RecordUserWireContentLocked(sessionID, requestID, wire string) bool {
	st := c.sessionStateLocked(sessionID)
	if st == nil {
		return false
	}
	// 从尾向前找该请求的 user 行：一轮里 user 事件在开头，尾向扫描命中最快。
	for index := len(st.transcript) - 1; index >= 0; index-- {
		event := &st.transcript[index]
		if event.Role != "user" {
			continue
		}
		if requestID != "" && event.TaskID != requestID {
			continue
		}
		if event.ProviderContent == wire {
			return true
		}
		if event.Content == wire {
			event.ProviderContent = ""
		} else {
			event.ProviderContent = wire
		}
		return true
	}
	return false
}

// UserWireContentFor 返回该轮 user 事件的已发出字节（测试与打点用）。
// 未记录 ProviderContent 时返回空串。
func (c *Coordinator) UserWireContentFor(sessionID, requestID string) string {
	if c == nil {
		return ""
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	st := c.sessionStateLocked(sessionID)
	if st == nil {
		return ""
	}
	for index := len(st.transcript) - 1; index >= 0; index-- {
		event := st.transcript[index]
		if event.Role != "user" {
			continue
		}
		if requestID != "" && event.TaskID != requestID {
			continue
		}
		return event.ProviderContent
	}
	return ""
}
