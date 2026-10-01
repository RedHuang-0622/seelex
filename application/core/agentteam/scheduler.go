package agentteam

import (
	"strings"
	"sync"
)

// scheduler.go — 群聊轮转控制（链表 + team work 前缀载体）。
//
// **接线状态（2026-10-01 复核）**：本原语已接线，且生产调用点唯一——
// application/core/agentteam/runtime.go 的 Runtime 持有它（构造时建链，SyncOrder /
// SetOrder 整表替换顺序，Snapshot() / Order() 只读投影）。判据见
// scheduler_wiring_test.go：它既钉源码只能有这一个生产调用点，也钉本包 README 与
// 源码不得再写"本原语还没接上 / 没有生产调用点"的旧结论（测试按字面量扫描包内
// 源码，因此本注释只陈述事实、不复述旧措辞）。
//
// 生产实际消费的四件事（`Runtime` 侧）：
//   - SetOrder：SyncOrder 把注册表顺序 − user 同步成环（整表替换；顺序事实仍然
//     只有 lifecycle 一份）；
//   - Order：座位存在性（`newGovernor` 据此决定 main/tl 座位要不要长出来）与投影；
//   - SetPrefix：team work 前缀的运行时载体（作者是存储侧的只读装配，见
//     Runtime.NoteMainContext），推进时下发给下一名成员；
//   - sessionsLocked：Snapshot 的"下一个谁发言"投影取会话号。
//
// 2026-10-01（M4 §9 #3）**已退场**的四组：channel 投递推进路径
// （`Requests` / `Request` / `Next`）、顺序编辑三件（`Move` / `Remove` / `Restore`）、
// 前缀只读 getter（`Prefix`），以及只服务它们的 `requests` 通道字段、`buffer`
// 构造参数与顺序编辑私有助手（`orderLocked` / `indexOfRole`）。理由是同一条：
// **没有生产消费者**——投递方是"每个角色自己的 agent loop"这件事从未落地，前端
// 拖拽调序那条手势也已退场（前端只能只读 team 快照）。留下的是**一份顺序事实 +
// 一条推进路径**：`Advance`（经 `Runtime.Next` 在其上补逃生记账）。
//
// 推进路径现状：`Advance` 与 `Runtime.Next` 目前**同样只有用例在走**——真正驱动
// 轮次的是 goal 治理的座位循环（见 README「运行时轮次驱动」），所以"下一个谁发言"
// 是表头扫描的静态投影，不随轮转变化。
//
// 顺序的唯一运行时事实是链表；落盘仍是 lifecycle.order_policy/order_roles
// （本原语不写盘）。

// TurnRequest 是一次「下一个该发言的成员」读数：谁、哪个会话、以及交接用的
// team work 前缀。
type TurnRequest struct {
	RoleName      string
	RoleSessionID string
	// Prefix 是 team work 起点到当前位置的上下文前缀（装配好的正文文本）。
	// 作者是主会话上下文的只读装配（见 Runtime.NoteMainContext）：指针移到下一个
	// 成员时一并交接，下一个成员拿到的是从 team work 起点到当前这一步的上下文，
	// 而不是只有它自己的 draft。
	Prefix string
}

type roleNode struct {
	roleName      string
	roleSessionID string
	next          *roleNode
}

// TurnScheduler 是链表形式的轮转控制器（并发安全）。
type TurnScheduler struct {
	mu      sync.Mutex
	head    *roleNode
	current *roleNode // 上一次领取的位置；nil = 从表头开始
	prefix  string    // team work 起点 → 当前位置的上下文前缀
}

// NewTurnScheduler 按 order 建链；sessions 提供 role_name → role_session_id，
// 缺省用 role_name 兜底（main 复用主会话时传主会话号）。
func NewTurnScheduler(order []string, sessions map[string]string) *TurnScheduler {
	scheduler := &TurnScheduler{}
	scheduler.setOrderLocked(order, sessions)
	return scheduler
}

// SetPrefix 更新 team work 起点到当前位置的上下文前缀（装配侧每次读出新事实后
// 调用；推进时交接）。本原语不生产正文，只当载体。
func (s *TurnScheduler) SetPrefix(prefix string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.prefix = prefix
	s.mu.Unlock()
}

// advanceLocked 把 current 推进到链表下一节点（走到表尾则回到表头）；空链表
// 返回 nil。
func (s *TurnScheduler) advanceLocked() *roleNode {
	if s.head == nil {
		return nil
	}
	if s.current == nil || s.current.next == nil {
		s.current = s.head
		return s.current
	}
	s.current = s.current.next
	return s.current
}

// Advance 按链表推进一格并返回下一名**可发言**成员。
//
// skip 返回 true 的成员被跳过：环内没有运行时执行者的角色、以及按 user 席位
// 策略不该占位的 user。因为环是闭链，最多遍历一圈；一圈内全部被跳过时返回
// ok=false（= 环内没有人能发言，调用方按逃生路径收束，不停在这里空转）。
//
// 交接即带上"team work 起点 → 当前"的前缀：装配侧每次发布后用 SetPrefix 更新，
// 这里保证下一名成员拿到当前完整前缀。
func (s *TurnScheduler) Advance(skip func(roleName string) bool) (TurnRequest, bool) {
	if s == nil {
		return TurnRequest{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.head == nil {
		return TurnRequest{}, false
	}
	total := 0
	for node := s.head; node != nil; node = node.next {
		total++
	}
	for step := 0; step < total; step++ {
		node := s.advanceLocked()
		if node == nil {
			return TurnRequest{}, false
		}
		if skip != nil && skip(node.roleName) {
			continue
		}
		return TurnRequest{
			RoleName:      node.roleName,
			RoleSessionID: node.roleSessionID,
			Prefix:        s.prefix,
		}, true
	}
	return TurnRequest{}, false
}

// SetOrder 整表替换顺序（顺序写路径的下发口：Runtime.SyncOrder）。
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

// sessionsLocked 返回链表当前的 role_name → role_session_id 快照
// （调用方需持有 s.mu）。
func (s *TurnScheduler) sessionsLocked() map[string]string {
	out := make(map[string]string)
	for node := s.head; node != nil; node = node.next {
		out[node.roleName] = node.roleSessionID
	}
	return out
}
