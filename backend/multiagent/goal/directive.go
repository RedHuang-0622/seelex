package goal

// directive.go：Controller 的 TL 指令记录扩展（Part I 遗留 API，deprecated）。
//
// 旧"同会话共享"治理把 TLDirective 摘要写回 active goal 指令环（GoalRecord.Directives），
// 供 Goal 帧/嵌入回读 TLMemory。DS-A2A 起该写回已移除：b 的记忆由其自身上下文
// （AdvisorSession.Rounds）承担，产物经 DirectiveBus corr 信封受信注入 EXEC，**不再写
// goal 共享状态**（见 advisor.go / techleader.go）。本 API 保留仅供审计/兼容调用，
// 治理代码不得再调用（新增用例须断言 active.Directives 不被 b 回合写回）。

import (
	"context"
	"fmt"
)

// EventDirective 是 goal 指令记录事件（投影随事件全量下发，快照语义）。
const EventDirective EventKind = "goal.directive"

// ActiveGoal 返回当前栈顶 active goal 的深拷贝（无则返回 nil, false）。
// 供 TL 回合/嵌入构建读取（读面免锁外拷贝）。
func (c *Controller) ActiveGoal() (*GoalRecord, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	top := c.stack.Top()
	if top == nil || IsTerminal(top.Status) {
		return nil, false
	}
	return top.Clone(), true
}

// AppendDirective 把一条 TL 指令摘要追加进 active goal 的指令环
// （环形保留最近 ≤MaxDirectives 条；超限丢最旧）。空栈/终态报错。
func (c *Controller) AppendDirective(ctx context.Context, content string) (*GoalRecord, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	top := c.stack.Top()
	if top == nil {
		return nil, ErrStackEmpty
	}
	if IsTerminal(top.Status) {
		return nil, fmt.Errorf("%w: goal %s 已是终态 %s，不再接收指令", ErrInvalidArgument, top.ID, top.Status)
	}
	content = trimDirectiveSummary(content)
	if content == "" {
		return nil, fmt.Errorf("%w: 指令摘要为空", ErrInvalidArgument)
	}
	now := c.now()
	top.Directives = append(top.Directives, content)
	if len(top.Directives) > MaxDirectives {
		top.Directives = top.Directives[len(top.Directives)-MaxDirectives:]
	}
	top.UpdatedAt = now
	if err := c.persistLocked(ctx); err != nil {
		return nil, err
	}
	c.emitLocked(Event{
		Kind:       EventDirective,
		At:         now,
		GoalID:     top.ID,
		Status:     top.Status,
		Projection: c.projectionLocked(),
	})
	return top.Clone(), nil
}

// trimDirectiveSummary 截断摘要到 MaxDirectiveRunes（防御：注入内容有界）。
func trimDirectiveSummary(content string) string {
	if len([]rune(content)) <= MaxDirectiveRunes {
		return content
	}
	return string([]rune(content)[:MaxDirectiveRunes])
}
