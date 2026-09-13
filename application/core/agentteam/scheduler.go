package agentteam

import (
	"strings"
	"sync"
)

// scheduler.go — 群聊轮转控制（channel + 链表）。
//
// 口径（用户裁决 2026-09-13）：顺序调整环节里每个参与者先把自己的发言意向
// struct 发到一个 channel；每个 agent loop 从 channel 领取"下一个该发言的
// 会话"；顺序本身用**链表**维护——上移/下移/摘除/恢复只改链表指针，下一次
// 领取自然落到新指定的 agent session。链表是顺序的唯一运行时事实，
// 落盘仍是 lifecycle.order_policy/order_roles（本原语不写盘）。

// TurnRequest 是一个参与者投递的发言意向（谁、哪个会话、第几轮）。
type TurnRequest struct {
	RoleName      string
	RoleSessionID string
	RoundID       uint64
	// Prefix 是 team work 起点到当前位置的上下文前缀（装配好的正文文本）。
	// 链表指针移到下一个 agent 时一并传递：下一个 agent 拿到的是从 team work
	// 起点到当前这一步的上下文，而不是只有它自己的 draft。
	Prefix string
}

type roleNode struct {
	roleName      string
	roleSessionID string
	next          *roleNode
}

// TurnScheduler 是 channel + 链表形式的轮转控制器（并发安全）。
type TurnScheduler struct {
	mu       sync.Mutex
	head     *roleNode
	current  *roleNode // 上一次领取的位置；nil = 从表头开始
	prefix   string    // team work 起点 → 当前位置的上下文前缀
	requests chan TurnRequest
}

// NewTurnScheduler 按 order 建链；sessions 提供 role_name → role_session_id，
// 缺省用 role_name 兜底（main 复用主会话时传主会话号）。
func NewTurnScheduler(order []string, sessions map[string]string, buffer int) *TurnScheduler {
	if buffer < 1 {
		buffer = 8
	}
	scheduler := &TurnScheduler{requests: make(chan TurnRequest, buffer)}
	scheduler.setOrderLocked(order, sessions)
	return scheduler
}

// Requests 返回发言意向投递口（参与者 actor 用；满则丢，调用方补重试）。
func (s *TurnScheduler) Requests() chan<- TurnRequest { return s.requests }

// Request 非阻塞投递一条发言意向。
func (s *TurnScheduler) Request(request TurnRequest) bool {
	if s == nil {
		return false
	}
	select {
	case s.requests <- request:
		return true
	default:
		return false
	}
}

// Next 领取下一个该发言的参与者：从 channel 收到意向 struct 后，按链表把
// current 推进到下一个节点，返回该节点的角色与会话（会话号优先用链表上
// 的绑定，其次用意向里的会话号）。
func (s *TurnScheduler) Next() TurnRequest {
	if s == nil {
		return TurnRequest{}
	}
	request := <-s.requests
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.advanceLocked(request.RoleName)
	if next == nil {
		request.Prefix = s.prefix
		return request
	}
	request.RoleName = next.roleName
	if next.roleSessionID != "" {
		request.RoleSessionID = next.roleSessionID
	}
	// 交接即带上"team work 起点 → 当前"的前缀：sequencer 每次发布后用
	// SetPrefix 更新，这里保证下一个 agent 拿到当前完整前缀。
	request.Prefix = s.prefix
	return request
}

// SetPrefix 更新 team work 起点到当前位置的上下文前缀（sequencer 每次发布后
// 调用；Next 交接时下发）。
func (s *TurnScheduler) SetPrefix(prefix string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.prefix = prefix
	s.mu.Unlock()
}

// Prefix 返回当前上下文前缀快照。
func (s *TurnScheduler) Prefix() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.prefix
}

// advanceLocked 把 current 推进到链表下一节点并按 roleName 对齐（若意向来自
// 非当前节点，则从该节点起算）；空链表返回 nil。
func (s *TurnScheduler) advanceLocked(roleName string) *roleNode {
	if s.head == nil {
		return nil
	}
	if roleName != "" && (s.current == nil || s.current.roleName != roleName) {
		for node := s.head; node != nil; node = node.next {
			if node.roleName == roleName {
				s.current = node
				return node
			}
		}
	}
	if s.current == nil || s.current.next == nil {
		s.current = s.head
		return s.current
	}
	s.current = s.current.next
	return s.current
}

// SetOrder 整表替换顺序（前端顺序编辑的下发路径）。
func (s *TurnScheduler) SetOrder(order []string, sessions map[string]string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.setOrderLocked(order, sessions)
	s.mu.Unlock()
}

func (s *TurnScheduler) setOrderLocked(order []string, sessions map[string]string) {
	var head, tail *roleNode
	for _, name := range order {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		node := &roleNode{roleName: name, roleSessionID: strings.TrimSpace(sessions[name])}
		if head == nil {
			head = node
		} else {
			tail.next = node
		}
		tail = node
	}
	s.head = head
	s.current = nil
}

// Order 返回链表当前顺序（快照）。
func (s *TurnScheduler) Order() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var order []string
	for node := s.head; node != nil; node = node.next {
		order = append(order, node.roleName)
	}
	return order
}

// Move 上移/下移一个角色（delta<0 上移，delta>0 下移），越界返回 false。
func (s *TurnScheduler) Move(roleName string, delta int) bool {
	if s == nil || delta == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	order := s.orderLocked()
	index := indexOfRole(order, roleName)
	target := index + delta
	if index < 0 || target < 0 || target >= len(order) {
		return false
	}
	order[index], order[target] = order[target], order[index]
	s.setOrderLocked(order, nil)
	return true
}

// Remove 摘除一个角色（保留注册表；顺序表移除）。
func (s *TurnScheduler) Remove(roleName string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	order := s.orderLocked()
	index := indexOfRole(order, roleName)
	if index < 0 {
		return false
	}
	s.setOrderLocked(append(order[:index:index], order[index+1:]...), nil)
	return true
}

// Restore 把角色追加到链尾（加入顺序末尾）。
func (s *TurnScheduler) Restore(roleName string) bool {
	if s == nil || strings.TrimSpace(roleName) == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	order := s.orderLocked()
	if indexOfRole(order, roleName) >= 0 {
		return false
	}
	s.setOrderLocked(append(order, roleName), nil)
	return true
}

func (s *TurnScheduler) orderLocked() []string {
	var order []string
	for node := s.head; node != nil; node = node.next {
		order = append(order, node.roleName)
	}
	return order
}

func indexOfRole(order []string, roleName string) int {
	for index, name := range order {
		if name == roleName {
			return index
		}
	}
	return -1
}
