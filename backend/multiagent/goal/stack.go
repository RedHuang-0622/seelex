package goal

// Stack 是 goal 的 LIFO 栈。索引 0 为栈底，末元素为栈顶（= active goal）。
// 本类型为纯值对象，并发安全由持有方（Controller.mu）保证。
type Stack struct {
	items []*GoalRecord
}

// Len 返回栈中 goal 数。
func (s *Stack) Len() int {
	if s == nil {
		return 0
	}
	return len(s.items)
}

// Push 压栈。
func (s *Stack) Push(record *GoalRecord) {
	s.items = append(s.items, record)
}

// Pop 弹栈顶；空栈返回 nil。
func (s *Stack) Pop() *GoalRecord {
	if len(s.items) == 0 {
		return nil
	}
	top := s.items[len(s.items)-1]
	s.items = s.items[:len(s.items)-1]
	return top
}

// Top 返回栈顶（不弹）。
func (s *Stack) Top() *GoalRecord {
	if len(s.items) == 0 {
		return nil
	}
	return s.items[len(s.items)-1]
}

// All 返回栈底→栈顶的切片（引用；调用方需持锁或仅读）。
func (s *Stack) All() []*GoalRecord {
	if s == nil {
		return nil
	}
	return s.items
}
