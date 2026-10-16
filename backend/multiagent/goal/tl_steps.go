package goal

import (
	"context"
	"strings"
)

// tl_steps.go — b（ADVISOR）回合的**过程**观察面：把"评审者做了什么"从不可见
// 变成可见。
//
// 生态位（与 tl_stream.go 的关系）：tl_stream.go 只送**正文分片**（模型在写什么），
// 本文件送**步骤**（评审者调了哪个只读工具、参数是什么、拿到什么结果）。两者都经
// **同一次调用内的 ctx** 传递：不新增共享状态、不改 TLEvaluator 契约、不引入前端
// 写入口。
//
// 为什么需要它：b 回合的真实执行面在 seelebridge（角色会话 + ReAct 循环）。旧实现
// 里那一轮的工具调用只活在**进程内的引擎会话**里（role_session 刻意不接
// DurableHistory），回合结束即消失——前端因此只看得到终局裁决（tl_directive 行），
// "评审过程"完全不可见。这里把工具步骤接出来，供前端渲染成过程时间线。
//
// 边界：本文件只做**单向**观察（后端 → 前端/审计）。步骤不参与任何裁决：裁决仍然
// 只来自 Evaluate 的返回值（TLDirective）。

// TLStep 是一次 b 回合里的一个可观察步骤。
//
// Kind 的两个取值对应"一次工具调用"的两端（与轨迹视图的请求/响应对齐）：
//   - "tool"：工具**开始**调用（Name/Args 有效）；
//   - "tool_result"：工具**返回**（Result/Err 有效）。
//
// 刻意不承载"模型正文"：正文由 in-flight 正文近端（TLDeltaSink）负责，两步分开
// 可以各自有各自的粒度与上限，不会因为"正文分片每分钟上千次"淹没工具步骤。
type TLStep struct {
	Kind   string `json:"kind"`             // tool | tool_result
	Turn   int    `json:"turn,omitempty"`   // 所在 ReAct 轮次
	Name   string `json:"name,omitempty"`   // 工具名
	Args   string `json:"args,omitempty"`   // 工具参数（原始 JSON，已截断）
	Result string `json:"result,omitempty"` // 工具返回摘要（已截断）
	Err    string `json:"err,omitempty"`    // 工具执行错误（成功时为空）
	At     int64  `json:"at,omitempty"`     // 记录时刻（秒）
}

// TLStepSink 接收 b 回合的一个步骤。
type TLStepSink func(step TLStep)

type tlStepSinkKey struct{}

// WithTLStepSink 把步骤观察回调挂到 ctx 上（回合开始处挂）。
func WithTLStepSink(ctx context.Context, sink TLStepSink) context.Context {
	if ctx == nil || sink == nil {
		return ctx
	}
	return context.WithValue(ctx, tlStepSinkKey{}, sink)
}

// TLStepSinkFrom 取出步骤观察回调；未挂载时返回 nil（执行面按"不观察"处理，
// 不影响回合本身）。员工回合（worker 作业驱动的角色回合）当前不挂回调，
// 因此它取到 nil 是常态。
func TLStepSinkFrom(ctx context.Context) TLStepSink {
	if ctx == nil {
		return nil
	}
	sink, _ := ctx.Value(tlStepSinkKey{}).(TLStepSink)
	return sink
}

// MaxRoundSteps 是本轮过程步骤的保留上限（保留**最近** MaxRoundSteps 条）。
//
// 为什么有界：评审一轮读几个文件就够了（循环上限 8），但工具可能重试；过程列表是
// "看得见在干什么"的只读快照，不是完整审计（完整原文由 recorder 落 role draft）。
const MaxRoundSteps = 40

// StepArgsLimit / StepResultLimit 是进面板前的截断上限（面板是摘要，不是正文）。
const (
	StepArgsLimit   = 240
	StepResultLimit = 400
)

// noteStep 记一个 b 回合步骤（保留最近 MaxRoundSteps 条）。
//
// 它在执行段（evaluateRound，**不持 s.mu**）被引擎的 ReAct 钩子调用，因此写入由
// inFlightMu（s.mu 的叶子）保护——与 noteInFlight 同款理由：引擎换个 goroutine 回调
// 也不会与 Snapshot 读取形成数据竞争。
func (s *Supervisor) noteStep(step TLStep) {
	if s == nil {
		return
	}
	step.Kind = strings.TrimSpace(step.Kind)
	if step.Kind == "" {
		return
	}
	if step.At <= 0 {
		step.At = s.now()
	}
	s.inFlightMu.Lock()
	defer s.inFlightMu.Unlock()
	if len(s.inFlightSteps) >= MaxRoundSteps {
		// 丢最旧的：过程面板关心"最近在干什么"，最前面的步骤价值最低。
		s.inFlightSteps = append(s.inFlightSteps[:0], s.inFlightSteps[len(s.inFlightSteps)-MaxRoundSteps+1:]...)
	}
	s.inFlightSteps = append(s.inFlightSteps, step)
}

// clearRoundSteps 在本轮**开始**时清掉上一轮的过程。
//
// 时机是刻意的：不在回合**结束**时清，是为了让"最近一轮评审过程"在回合结束后仍然
// 留在面板上（否则用户永远只看得到"正在评审"的那几秒）。新一轮开始才换代，因此
// 面板语义是"本轮/最近一轮的过程"，不会把两个回合的步骤混在一起。
func (s *Supervisor) clearRoundSteps() {
	if s == nil {
		return
	}
	s.inFlightMu.Lock()
	defer s.inFlightMu.Unlock()
	s.inFlightSteps = nil
}

// roundStepsSnapshot 复制当前过程步骤（读面用；调用方不限持锁）。
func (s *Supervisor) roundStepsSnapshot() []TLStep {
	if s == nil {
		return nil
	}
	s.inFlightMu.Lock()
	defer s.inFlightMu.Unlock()
	return append([]TLStep(nil), s.inFlightSteps...)
}
