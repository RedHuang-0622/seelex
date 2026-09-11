// Package govern 提供多代理治理循环抽象（2026-09-08）。
//
// 定位：把 DS-A2A 的"评审/执行来回"上升为可复用的**回合制治理循环**
// （类似桌游的"玩家按座位次序轮流行动，直到有人喊停"）。goal 域不是
// 唯一使用方——EXEC/TL 以外的新治理角色（评审、审计、审批代理等）都可
// 实现 Seat 参与同一治理循环；具体回合动作由装配方注入，本包只定义
// "次序、循环与断环"契约，不绑定任何单角色实现。
//
// 与 seelex 其它模块的关系：
//   - application/core/goal（DS-A2A 实现）经 adapter 接入本包；
//   - seelebridge/application 装配层未来可直接使用本包驱动多代理会话治理；
//   - 本包不依赖 goal/session/seelebridge（纯编排原语）。
//
// 设计来源：docs/2026-09-07-seele-a2a-framework-req/（DS-A2A 协议 §6/§9）
// + docs/research/2026-09-07-a2a-techleader-startup-research.md。
package govern

import (
	"context"
	"errors"
	"fmt"
)

// AgentKind 是治理参与者的角色标识（EXEC/TL/其它未来的评审者）。
type AgentKind string

const (
	// AgentKindExec 是执行代理（main agent / EXEC，协议中的 a）。
	AgentKindExec AgentKind = "exec"
	// AgentKindAdvisor 是评审代理（TechLeader / ADVISOR，协议中的 b）。
	AgentKindAdvisor AgentKind = "advisor"
)

// TurnAction 是一次回合里"某个参与者做了什么"的最小结果。
// 它只携带让出回合与断环判定，不假设内容格式——具体动作语义由
// Seat 实现负责（EXEC 可返回进度信号，TL 可返回 TLDirective）。
type TurnAction struct {
	// BreakLoop 是否请求打破整个治理循环（例如 TL verdict_done /
	// escalate_human、EXEC 声明无法推进）。置 true 时循环应结束。
	BreakLoop bool
	// Note 供日志/审计的一句话摘要（不假设协议字段）。
	Note string
}

// ShouldBreak 是每轮结束时的断环判定：任一参与者请求打破循环即停。
// 供 Governor 实现（或外部驱动）在收集完一轮 actions 后调用。
func ShouldBreak(actions []TurnAction) bool {
	for _, action := range actions {
		if action.BreakLoop {
			return true
		}
	}
	return false
}

// Seat 是桌游里的"一个座位"：表示某个参与者在循环中的身份与行动入口。
// 同一个物理实现可以以不同身份坐多个座位（例如同一条 LLM 通道既当 EXEC
// 又当 TL），因此身份由 Seat 携带，不放在实现对象上。
type Seat interface {
	// Name 返回座位名（审计/日志用，例如 "exec-a" / "advisor-b"）。
	Name() string
	// Kind 返回角色类型（AgentKindExec / AgentKindAdvisor / 自定义）。
	Kind() AgentKind
	// Act 执行一次本座位回合；ctx 由治理循环传入（含取消）。
	Act(ctx context.Context) (TurnAction, error)
}

// Governor 是治理循环的"主持人/发牌者"：决定座次、推进回合、收束循环。
type Governor interface {
	// AddSeat 注册一个座位（追加到座次末尾；同名重复注册报错）。
	AddSeat(seat Seat) error
	// Next 推进一个回合：让当前座位 Act，然后把发牌权交给下一座位。
	// 返回 false 表示循环已收束（无更多回合可推进）。
	Next(ctx context.Context) (bool, error)
	// Current 返回当前拥有发牌权的座位名（无座位时返回 ""）。
	Current() string
	// Seats 返回当前座次（按注册顺序）。
	Seats() []string
	// Round 返回当前轮次（0-based；每轮所有座位各行动一次后 +1）。
	Round() int
	// Break 主动请求打破循环（外部/超时/用户中断等场景；幂等）。
	Break(reason string)
	// Broken 返回循环是否已被打破，以及打破原因。
	Broken() (bool, string)
}

// NewTurnGovernor 构造一个固定座次的回合制治理循环。
//
// 语义（对应桌游）：第 n 轮由 seats 按注册顺序各 Act 一次；每个座位
// Act 返回的 TurnAction 若 BreakLoop=true，则该回合后立即收束；
// maxRounds>0 时到达上限也收束（防死循环护栏）。外部可用 Break 主动停。
func NewTurnGovernor(seats []Seat, maxRounds int) Governor {
	return newTurnGovernor(seats, maxRounds)
}

// Snapshot 是治理循环的只读快照（审计/前端投影素材）。
type Snapshot struct {
	Round       int      `json:"round"`
	CurrentSeat string   `json:"current_seat,omitempty"`
	Seats       []string `json:"seats"`
	Broken      bool     `json:"broken"`
	BreakReason string   `json:"break_reason,omitempty"`
}

// SnapshotOf 从任意 Governor 快照出可 JSON 化的读面。
func SnapshotOf(g Governor) Snapshot {
	if g == nil {
		return Snapshot{}
	}
	broken, reason := g.Broken()
	return Snapshot{
		Round:       g.Round(),
		CurrentSeat: g.Current(),
		Seats:       append([]string(nil), g.Seats()...),
		Broken:      broken,
		BreakReason: reason,
	}
}

// turnGovernor 是 NewTurnGovernor 的默认实现（固定座次、顺序推进）。
type turnGovernor struct {
	seats     []Seat
	maxRounds int

	round       int // 已完成的完整轮数
	index       int // 当前座位在 seats 中的下标
	broken      bool
	breakReason string
}

func newTurnGovernor(seats []Seat, maxRounds int) *turnGovernor {
	cleaned := make([]Seat, 0, len(seats))
	seen := make(map[string]bool, len(seats))
	for _, seat := range seats {
		if seat == nil {
			continue
		}
		name := seat.Name()
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		cleaned = append(cleaned, seat)
	}
	return &turnGovernor{
		seats:     cleaned,
		maxRounds: maxRounds,
	}
}

func (g *turnGovernor) AddSeat(seat Seat) error {
	if seat == nil {
		return nil
	}
	name := seat.Name()
	if name == "" {
		return errors.New("govern: 座位名不可为空")
	}
	for _, existing := range g.seats {
		if existing.Name() == name {
			return fmt.Errorf("govern: 重复座位 %q", name)
		}
	}
	g.seats = append(g.seats, seat)
	return nil
}

func (g *turnGovernor) Next(ctx context.Context) (bool, error) {
	if len(g.seats) == 0 {
		return false, nil
	}
	if g.broken {
		return false, nil
	}
	if g.maxRounds > 0 && g.round >= g.maxRounds {
		return false, nil
	}
	seat := g.seats[g.index]
	action, err := seat.Act(ctx)
	if err != nil {
		return false, err
	}
	if action.BreakLoop {
		reason := action.Note
		if reason == "" {
			reason = "seat " + seat.Name() + " requested break"
		}
		g.broken = true
		g.breakReason = reason
		return false, nil
	}
	g.index++
	if g.index >= len(g.seats) {
		g.index = 0
		g.round++
	}
	return true, nil
}

func (g *turnGovernor) Current() string {
	if len(g.seats) == 0 {
		return ""
	}
	return g.seats[g.index].Name()
}

func (g *turnGovernor) Seats() []string {
	out := make([]string, 0, len(g.seats))
	for _, seat := range g.seats {
		out = append(out, seat.Name())
	}
	return out
}

func (g *turnGovernor) Round() int {
	return g.round
}

func (g *turnGovernor) Break(reason string) {
	if g.broken {
		return
	}
	g.broken = true
	g.breakReason = reason
}

func (g *turnGovernor) Broken() (bool, string) {
	return g.broken, g.breakReason
}
