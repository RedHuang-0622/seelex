package agentteam

// runtime.go — 会话级的「发言调度运行态」。
//
// 生态位：`TurnScheduler` 只提供链表轮转原语（顺序 + 前缀 + 上移/下移/摘除），
// 它不回答三个产品问题：
//
//  1. **谁来维护这张成员表**——注册表每次增删改后，环里的员工必须跟着变；
//  2. **user 算不算环里的一环**——user 可以用消息队列插话，但「作为员工一样的
//     一环固定占位」需要有明确口径（见 UserSeatPolicy）；
//  3. **循环怎么逃生**——链表是闭链，没有上限就会一直转下去。
//
// Runtime 就是这三个问题的唯一落点，并把状态投影成 dto.TeamSchedule 供前端/
// 巡检面观察。它**不新增第二份顺序事实**：顺序仍然只有 lifecycle.order_policy /
// order_roles 一份，Runtime 只是它在运行时的镜像 + 记账。

import (
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// UserSeatPolicy 决定 user 是否作为循环里的一环参与发言。
//
// 背景（产品口径）：user 永远在 `order_roles` 里（它是群聊的起手与收口），
// 但「在顺序里」不等于「每轮都固定占一个座位」：
//
//   - UserSeatQueued（缺省）：user 通过**消息队列**插话。只有当队列里有未消费
//     的 user 输入时，user 才占位；否则调度器跳过 user 继续转，不阻塞 agent 循环。
//     这样 user 可以随时插入会话，但不会因为「人还没说话」把整条环卡住。
//   - UserSeatMember：user 与员工同权，每轮固定占位（在 user_main_decided 这类
//     「由 user/main 编排」的策略下才成立）。
//   - UserSeatAbsent：user 不占位（scheduled_only：只有定时 agent 插话）。
type UserSeatPolicy string

const (
	UserSeatQueued UserSeatPolicy = dto.UserSeatQueued
	UserSeatMember UserSeatPolicy = dto.UserSeatMember
	UserSeatAbsent UserSeatPolicy = dto.UserSeatAbsent
)

// UserSeatPolicyFor 由顺序策略推导 user 席位口径：顺序策略是唯一开关，不再
// 新增第二个人工配置项（口径与 order_policy 一一对应，避免两处打架）。
func UserSeatPolicyFor(orderPolicy string) UserSeatPolicy {
	switch strings.TrimSpace(orderPolicy) {
	case dto.OrderPolicyUserMainDecided:
		return UserSeatMember
	case dto.OrderPolicyScheduledOnly:
		return UserSeatAbsent
	default:
		return UserSeatQueued
	}
}

// 逃生路径的停止原因。停止 ≠ 出错：它是循环的正常收束方式之一，调用方据此
// 决定"让位给用户/收口/升级"。
const (
	StopRoundLimit = "round_limit"    // 到达轮次上限
	StopNoProgress = "no_progress"    // 连续多轮没有推进
	StopNoExecutor = "no_executor"    // 环内没有任何有执行者的角色
	StopEmptyRing  = "empty_ring"     // 环是空的（没有成员）
	StopExternal   = "external_break" // 外部显式停止（用户/裁决/Break）
)

// RuntimeOptions 是运行态的装配输入。
type RuntimeOptions struct {
	// RoundLimit ≤0 = 治理层不设轮次上限（显式选择，只靠裁决/Break 收束）。
	RoundLimit int
	// NoProgressLimit ≤0 = 不设无进展上限。
	NoProgressLimit int
	// UserSeat 缺省由顺序策略推导（UserSeatPolicyFor）。
	UserSeat UserSeatPolicy
	// Buffer 是 Requests() 的 channel 缓冲（≤0 取默认 8）。
	Buffer int
}

// Runtime 是会话级的发言调度运行态：链表顺序 + 逃生记账 + user 席位策略。
// 所有方法都可在多 goroutine 下调用（内部锁）。
type Runtime struct {
	mu        sync.Mutex
	scheduler *TurnScheduler
	policy    string
	userSeat  UserSeatPolicy
	// userQueued = 消息队列里有未消费的 user 输入（user 才占位）。
	userQueued bool

	round           int
	roundLimit      int
	noProgress      int
	noProgressLimit int

	stopped    bool
	stopReason string
	// userSeatExplicit 非 nil 表示 user 席位口径被显式覆盖过：此时顺序策略
	// 变化不再改写它（显式选择优先于推导）。
	userSeatExplicit *UserSeatPolicy
}

// NewRuntime 构造运行态。sessions 提供 role_name → role_session_id（成员表的
// 角色会话坐标）；executors 为 nil 时按包的 RolesWithExecutor 事实表判断。
func NewRuntime(order []string, sessions map[string]string, orderPolicy string, opts RuntimeOptions) *Runtime {
	policy := UserSeatPolicyFor(orderPolicy)
	if opts.UserSeat != "" {
		policy = opts.UserSeat
	}
	buffer := opts.Buffer
	if buffer < 1 {
		buffer = 8
	}
	order = cleanOrder(order)
	return &Runtime{
		scheduler:       NewTurnScheduler(order, sessions, buffer),
		policy:          strings.TrimSpace(orderPolicy),
		userSeat:        policy,
		roundLimit:      opts.RoundLimit,
		noProgressLimit: opts.NoProgressLimit,
	}
}

// SyncOrder 把注册表/顺序的当前事实同步进环（每次角色增删改或顺序调整后调用）。
// 已经停止的环不因为一次同步就被"复活"：逃生是显式的，同步只改成员与顺序。
func (r *Runtime) SyncOrder(order []string, sessions map[string]string, orderPolicy string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scheduler.SetOrder(cleanOrder(order), sessions)
	r.policy = strings.TrimSpace(orderPolicy)
	if seat := UserSeatPolicyFor(orderPolicy); seat != "" && r.userSeatExplicit == nil {
		r.userSeat = seat
	}
}

// SetUserSeat 显式覆盖 user 席位口径（缺省由顺序策略推导）。
func (r *Runtime) SetUserSeat(policy UserSeatPolicy) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.userSeat = policy
	r.userSeatExplicit = &policy
	r.mu.Unlock()
}

// NoteUserQueued 更新"消息队列里有没有未消费的 user 输入"。user 席位口径为
// UserSeatQueued 时，这正是 user 是否占位的唯一依据。
func (r *Runtime) NoteUserQueued(pending bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.userQueued = pending
	r.mu.Unlock()
}

// Order 返回环当前的链表顺序（快照）。
func (r *Runtime) Order() []string {
	if r == nil {
		return nil
	}
	return r.scheduler.Order()
}

// Round 返回已经走过的轮数。
func (r *Runtime) Round() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.round
}

// noteprogress 记账一次回合：progressed=false 累计"连续无进展"，到达上限即
// 触发逃生（停止环）。返回值是本次记账后的停止态。
func (r *Runtime) NoteTurn(progressed bool) (bool, string) {
	if r == nil {
		return false, ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return true, r.stopReason
	}
	r.round++
	if progressed {
		r.noProgress = 0
	} else {
		r.noProgress++
	}
	switch {
	case r.roundLimit > 0 && r.round >= r.roundLimit:
		r.stopLocked(StopRoundLimit)
	case r.noProgressLimit > 0 && r.noProgress >= r.noProgressLimit:
		r.stopLocked(StopNoProgress)
	}
	return r.stopped, r.stopReason
}

// Stop 显式停止环（用户中断 / 裁决收口 / 外部 Break）。
func (r *Runtime) Stop(reason string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.stopLocked(strings.TrimSpace(reason))
	r.mu.Unlock()
}

func (r *Runtime) stopLocked(reason string) {
	if r.stopped {
		return
	}
	if reason == "" {
		reason = StopExternal
	}
	r.stopped = true
	r.stopReason = reason
}

// Stopped 返回环是否已被逃生路径收束，以及原因。
func (r *Runtime) Stopped() (bool, string) {
	if r == nil {
		return false, ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopped, r.stopReason
}

// Next 推进一格并返回下一个该发言的成员。ok=false 表示环内没有人能发言
// （空环 / 全员无执行者 / 已收束），调用方据此走逃生路径，不要空转。
//
// 推进成功时会把 user 席位对应的排队输入消费掉（user 发言一次 = 消费一次）。
func (r *Runtime) Next() (TurnRequest, bool) {
	if r == nil {
		return TurnRequest{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return TurnRequest{}, false
	}
	request, ok := r.scheduler.Advance(r.skipLocked)
	if !ok {
		switch {
		case len(r.scheduler.Order()) == 0:
			r.stopLocked(StopEmptyRing)
		case len(UnexecutedRoles(r.scheduler.Order())) > 0:
			r.stopLocked(StopNoExecutor)
		}
		return TurnRequest{}, false
	}
	if request.RoleName == string(dto.RoleKindUser) {
		r.userQueued = false
	}
	return request, true
}

// skipLocked 报告某个成员本轮不应占位。
func (r *Runtime) skipLocked(roleName string) bool {
	if !RolesWithExecutor[roleName] {
		// 环里挂着没有执行者的角色（review-team 的 reviewer 等）：跳过，
		// 但会在 Snapshot().Unexecuted 里如实报出来。
		return true
	}
	if roleName != string(dto.RoleKindUser) {
		return false
	}
	switch r.userSeat {
	case UserSeatAbsent:
		return true
	case UserSeatMember:
		return false
	default: // UserSeatQueued
		return !r.userQueued
	}
}

// Snapshot 投影成只读运行态（前端「下一个谁发言 / 第几轮 / 是否已逃生」）。
func (r *Runtime) Snapshot() dto.TeamSchedule {
	if r == nil {
		return dto.TeamSchedule{}
	}
	r.mu.Lock()
	policy, seat := r.policy, r.userSeat
	round, roundLimit := r.round, r.roundLimit
	noProgress, noProgressLimit := r.noProgress, r.noProgressLimit
	stopped, reason := r.stopped, r.stopReason
	r.mu.Unlock()

	order := r.scheduler.Order()
	view := dto.TeamSchedule{
		OrderPolicy:     policy,
		Order:           order,
		Round:           round,
		RoundLimit:      roundLimit,
		NoProgress:      noProgress,
		NoProgressLimit: noProgressLimit,
		Stopped:         stopped,
		StopReason:      reason,
		UserSeat:        string(seat),
		Unexecuted:      UnexecutedRoles(order),
	}
	if !stopped {
		if request, ok := r.peekNext(); ok {
			view.NextRole = request.RoleName
		}
	}
	return view
}

// peekNext 在不改动游标的前提下算出"下一个谁发言"（Snapshot 用）。
func (r *Runtime) peekNext() (TurnRequest, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return TurnRequest{}, false
	}
	order := r.scheduler.Order()
	sessions := r.scheduler.sessionsLocked()
	for _, roleName := range order {
		if r.skipLocked(roleName) {
			continue
		}
		return TurnRequest{RoleName: roleName, RoleSessionID: sessions[roleName]}, true
	}
	return TurnRequest{}, false
}

// cleanOrder 去掉空名与重复项（顺序事实来自 lifecycle，容错但不伪造）。
func cleanOrder(order []string) []string {
	out := make([]string, 0, len(order))
	seen := make(map[string]struct{}, len(order))
	for _, name := range order {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}
