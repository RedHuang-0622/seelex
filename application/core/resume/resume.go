// Package resume 承载“终止前未完成工作”的通用恢复模板。
//
// 模板只有一条流水线，七个声明式步骤不可乱序：
//
//	1 定位   定位        已发出的派发记录 + 缺失的结果记录（幂等键 = 派发 ID）
//	2 判定   判定        active/未终结 → 需要重启；已终结（done/failed）→ 只补历史
//	3 补历史 补历史      父侧缺失结果 → provider-only 占位，成对合法
//	4 重建   重建现场    恢复被执行单元自己的输入上下文
//	5 注入   注入说明    system：说明这是中断后的恢复、已完成到哪、接下来继续什么
//	6 重跑   重新执行    同一幂等键重启执行；结果回流按正常路径写回
//	7 收敛   收敛        完成后删除临时现场，结论跟随被恢复单元的父级持久化
//
// 本包是领域无关的：它不认识 subagent、goal、role draft 或任何具体存储，
// 只认识 Unit（未完成单元）与 Port（可插拔端口）。subagent 是第一个使用者，
// 见 seelebridge/runtime_subagent_resume.go。
//
// 设计依据：docs/arch/a2a-agent-team-factory.md §5.1 与 §9 的 AT10。
package resume

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ErrPortNotAssembled 表示恢复模板未装配端口（组合根漏接线）。
var ErrPortNotAssembled = errors.New("resume: port is not assembled")

// Step 是恢复模板的声明式步骤标识（审计/报告/测试断言用）。
type Step string

const (
	// StepLocate 定位未完成单元。
	StepLocate Step = "locate"
	// StepDecide 判定单元是否需要重启。
	StepDecide Step = "decide"
	// StepRepairParent 在被恢复单元的父侧补齐缺失结果（provider-only 占位）。
	StepRepairParent Step = "repair_parent"
	// StepRestoreScene 重建被执行单元自己的现场与输入上下文。
	StepRestoreScene Step = "restore_scene"
	// StepInjectNote 以 system 注入恢复说明（只进被执行单元自己的上下文）。
	StepInjectNote Step = "inject_note"
	// StepReexecute 以同一幂等键重新执行。
	StepReexecute Step = "reexecute"
	// StepConverge 收敛：清理临时现场、写回结论。
	StepConverge Step = "converge"
)

// Order 返回模板的标准步骤顺序（唯一权威，调用方不得自排）。
func Order() []Step {
	return []Step{
		StepLocate, StepDecide, StepRepairParent, StepRestoreScene,
		StepInjectNote, StepReexecute, StepConverge,
	}
}

// UnitStatus 是未完成单元在持久化事实里的状态。
type UnitStatus string

const (
	// UnitActive 表示单元未终结（派发已发生、结果未记录），需要重启续跑。
	UnitActive UnitStatus = "active"
	// UnitDone 表示单元已成功终结，只补历史不重启。
	UnitDone UnitStatus = "done"
	// UnitFailed 表示单元已失败终结，只补历史不重启。
	UnitFailed UnitStatus = "failed"
)

// Unit 是“未完成单元”的领域无关投影。
//
// Key 是幂等键（例如子代理派发 ID / 节点 ID）：同一次派发在任何恢复轮次里
// 都必须保持同一个 Key，模板据此去重并保证不产生第二条结论。
type Unit struct {
	// Key 是幂等键，必须非空且在同一派发生命周期内稳定。
	Key string
	// Kind 是单元种类（subagent / role_draft / …），仅用于报告与过滤。
	Kind string
	// Status 是持久化事实里的单元状态。
	Status UnitStatus
	// SessionID 是被执行单元自己的会话 ID（现场/上下文归属）。
	SessionID string
	// ParentSessionID 是发起派发的父级会话 ID（占位结果写回处）。
	ParentSessionID string
	// Goal 是原始派发目标（重建现场与同键重跑的必要输入）。
	Goal string
	// UpdatedAt 是持久化记录最后更新时间（报告/排序用）。
	UpdatedAt time.Time
}

// Active 报告单元是否仍需重启续跑。
func (u Unit) Active() bool { return u.Status == UnitActive }

// Scene 是被执行单元重建出来的现场（不透明载荷 + 人类可读摘要）。
//
// Payload 由端口自行编码（例如 NodeSessionRecord 的 JSON），模板不解释它，
// 只在 RestoreScene → InjectNote → Reexecute 之间原样传递。
type Scene struct {
	// Payload 是现场载荷（端口自定义编码）。
	Payload []byte
	// Summary 是现场摘要（报告/恢复说明用）。
	Summary string
}

// Outcome 是一次重新执行的结果。
type Outcome struct {
	// Status 是执行后的单元状态（done / failed / active）。
	Status UnitStatus
	// Summary 是结果摘要。
	Summary string
	// Err 是执行错误；非 nil 时模板记为失败并允许重试。
	Err error
}

// Port 是恢复模板的可插拔端口：七个步骤对应七个方法。
//
// 实现方必须保证每个方法自身幂等（同一 Unit.Key 重复调用不产生第二条事实）；
// 模板只负责步骤顺序、去重与报告。
type Port interface {
	// Locate 定位候选未完成单元（含已终结单元，供补历史分支使用）。
	Locate(ctx context.Context) ([]Unit, error)
	// RepairParent 在父侧补齐缺失结果（provider-only 占位）；必须幂等。
	RepairParent(ctx context.Context, unit Unit) error
	// RestoreScene 重建被执行单元的现场与输入上下文。
	RestoreScene(ctx context.Context, unit Unit) (Scene, error)
	// InjectNote 以 system 注入恢复说明（只进被执行单元自己的上下文）。
	InjectNote(ctx context.Context, unit Unit, scene Scene) error
	// Reexecute 以同一幂等键重新执行单元。
	Reexecute(ctx context.Context, unit Unit, scene Scene) (Outcome, error)
	// Converge 收敛：清理临时现场、写回结论（必须幂等）。
	Converge(ctx context.Context, unit Unit, outcome Outcome) error
}

// UnitResult 是单个单元本轮恢复的结果。
type UnitResult struct {
	// Key 是单元幂等键。
	Key string
	// Kind 是单元种类。
	Kind string
	// Steps 是实际执行的步骤（按 Order 顺序）。
	Steps []Step
	// Resumed 表示是否真的重建现场并重新执行。
	Resumed bool
	// Skipped 表示本轮未重启（已终结、已在途或未定位到）。
	Skipped bool
	// Summary 是恢复/执行的摘要。
	Summary string
	// Err 是本轮失败原因（非空即未收敛）。
	Err string
}

// Failed 报告本轮是否失败。
func (r UnitResult) Failed() bool { return r.Err != "" }

// Report 是一轮恢复的总报告。
type Report struct {
	// Located 是定位到的单元数（去重后）。
	Located int
	// Units 是逐单元结果（按 Key 稳定排序）。
	Units []UnitResult
	// StartedAt/FinishedAt 是本轮起止时间。
	StartedAt  time.Time
	FinishedAt time.Time
}

// Resumed 返回本轮真正重启的单元键（稳定排序）。
func (r Report) Resumed() []string {
	keys := make([]string, 0, len(r.Units))
	for _, unit := range r.Units {
		if unit.Resumed {
			keys = append(keys, unit.Key)
		}
	}
	sort.Strings(keys)
	return keys
}

// Skipped 返回本轮未重启的单元键（稳定排序）。
func (r Report) Skipped() []string {
	keys := make([]string, 0, len(r.Units))
	for _, unit := range r.Units {
		if unit.Skipped {
			keys = append(keys, unit.Key)
		}
	}
	sort.Strings(keys)
	return keys
}

// Failed 返回本轮失败的单元结果（稳定排序）。
func (r Report) Failed() []UnitResult {
	failures := make([]UnitResult, 0, len(r.Units))
	for _, unit := range r.Units {
		if unit.Failed() {
			failures = append(failures, unit)
		}
	}
	sort.Slice(failures, func(i, j int) bool { return failures[i].Key < failures[j].Key })
	return failures
}

// Runner 按模板顺序驱动 Port，并保证同一幂等键不会并发重复重跑。
//
// 并发语义：Runner 在“重建现场 → 重跑 → 收敛”这段持有 Key 级在途标记；
// 同一 Key 的并发调用直接跳过，避免同一派发产生两条结论。标记在收敛或失败
// 后释放 —— 失败允许调用方稍后重试。
type Runner struct {
	port Port
	// allowReexecute 在测试中用于观察步骤顺序；生产恒为 true。
	allowReexecute bool

	mu       sync.Mutex
	inFlight map[string]struct{}
}

// NewRunner 构造恢复执行器；port 为 nil 时 Execute 返回 ErrPortNotAssembled。
func NewRunner(port Port) *Runner {
	return &Runner{port: port, allowReexecute: true, inFlight: make(map[string]struct{})}
}

// Execute 执行一轮恢复：定位 → 判定 → 补历史 → 重建 → 注入 → 重跑 → 收敛。
// 已终结单元只走「补历史 + 收敛残留现场」，不重建、不重跑。
//
// 返回的 error 只表示“定位失败”这类整轮阻塞；单单元失败写进 Report.Units，
// 不影响同轮其它单元（一个坏单元不得拖垮其它未完成工作）。
func (r *Runner) Execute(ctx context.Context) (Report, error) {
	report := Report{StartedAt: time.Now()}
	if r == nil || r.port == nil {
		return report, ErrPortNotAssembled
	}
	units, err := r.port.Locate(ctx)
	if err != nil {
		report.FinishedAt = time.Now()
		return report, fmt.Errorf("resume: locate: %w", err)
	}
	units = dedupeUnits(units)
	report.Located = len(units)
	report.Units = make([]UnitResult, 0, len(units))
	for _, unit := range units {
		report.Units = append(report.Units, r.executeUnit(ctx, unit))
	}
	sort.Slice(report.Units, func(i, j int) bool { return report.Units[i].Key < report.Units[j].Key })
	report.FinishedAt = time.Now()
	return report, nil
}

// ExecuteKey 只恢复指定幂等键的单元（定点重试入口；未定位到 → Skipped）。
func (r *Runner) ExecuteKey(ctx context.Context, key string) (UnitResult, error) {
	if r == nil || r.port == nil {
		return UnitResult{Key: key}, ErrPortNotAssembled
	}
	units, err := r.port.Locate(ctx)
	if err != nil {
		return UnitResult{Key: key}, fmt.Errorf("resume: locate: %w", err)
	}
	for _, unit := range dedupeUnits(units) {
		if unit.Key == key {
			return r.executeUnit(ctx, unit), nil
		}
	}
	return UnitResult{Key: key, Skipped: true, Summary: "unit not located"}, nil
}

// executeUnit 跑完一个单元的全部适用步骤。
func (r *Runner) executeUnit(ctx context.Context, unit Unit) UnitResult {
	result := UnitResult{Key: unit.Key, Kind: unit.Kind}
	// 步骤 2：判定。已终结单元只补历史 + 收敛残留，不重启（AT10）。
	if !unit.Active() {
		result.Steps = append(result.Steps, StepDecide)
		if err := r.port.RepairParent(ctx, unit); err != nil {
			result.Steps = append(result.Steps, StepRepairParent)
			result.Err = fmt.Sprintf("%s: %v", StepRepairParent, err)
			result.Skipped = true
			return result
		}
		result.Steps = append(result.Steps, StepRepairParent)
		// 已终结单元的收敛只做幂等清理（结论已跟随父级，残留现场可删），
		// 不得重新执行。
		outcome := Outcome{Status: unit.Status, Summary: "terminal unit"}
		if err := r.port.Converge(ctx, unit, outcome); err != nil {
			result.Steps = append(result.Steps, StepConverge)
			result.Err = fmt.Sprintf("%s: %v", StepConverge, err)
			result.Skipped = true
			return result
		}
		result.Steps = append(result.Steps, StepConverge)
		result.Skipped = true
		result.Summary = "terminal unit: history repaired only"
		return result
	}
	if !r.claim(unit.Key) {
		result.Steps = append(result.Steps, StepDecide)
		result.Skipped = true
		result.Summary = "already in flight"
		return result
	}
	defer r.release(unit.Key)

	result.Steps = append(result.Steps, StepDecide, StepRepairParent)
	if err := r.port.RepairParent(ctx, unit); err != nil {
		result.Err = fmt.Sprintf("%s: %v", StepRepairParent, err)
		return result
	}
	scene, err := r.port.RestoreScene(ctx, unit)
	result.Steps = append(result.Steps, StepRestoreScene)
	if err != nil {
		result.Err = fmt.Sprintf("%s: %v", StepRestoreScene, err)
		return result
	}
	if err := r.port.InjectNote(ctx, unit, scene); err != nil {
		result.Steps = append(result.Steps, StepInjectNote)
		result.Err = fmt.Sprintf("%s: %v", StepInjectNote, err)
		return result
	}
	result.Steps = append(result.Steps, StepInjectNote)
	outcome, err := r.port.Reexecute(ctx, unit, scene)
	result.Steps = append(result.Steps, StepReexecute)
	if err == nil {
		err = outcome.Err
	}
	if err != nil {
		result.Err = fmt.Sprintf("%s: %v", StepReexecute, err)
		// 失败也走收敛：端口负责把“未完成”事实留成可重试状态。
		_ = r.port.Converge(ctx, unit, Outcome{Status: UnitFailed, Summary: outcome.Summary, Err: err})
		result.Steps = append(result.Steps, StepConverge)
		return result
	}
	if err := r.port.Converge(ctx, unit, outcome); err != nil {
		result.Steps = append(result.Steps, StepConverge)
		result.Err = fmt.Sprintf("%s: %v", StepConverge, err)
		return result
	}
	result.Steps = append(result.Steps, StepConverge)
	result.Resumed = true
	result.Summary = outcome.Summary
	return result
}

// claim 标记 Key 在途（幂等：同一 Key 不并发重跑）。
func (r *Runner) claim(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.inFlight[key]; exists {
		return false
	}
	r.inFlight[key] = struct{}{}
	return true
}

// release 释放在途标记（收敛或失败后都释放，失败可重试）。
func (r *Runner) release(key string) {
	r.mu.Lock()
	delete(r.inFlight, key)
	r.mu.Unlock()
}

// dedupeUnits 按 Key 去重并稳定排序（幂等键唯一，重复定位取首个）。
func dedupeUnits(units []Unit) []Unit {
	seen := make(map[string]struct{}, len(units))
	unique := make([]Unit, 0, len(units))
	for _, unit := range units {
		if unit.Key == "" {
			continue
		}
		if _, exists := seen[unit.Key]; exists {
			continue
		}
		seen[unit.Key] = struct{}{}
		unique = append(unique, unit)
	}
	sort.Slice(unique, func(i, j int) bool { return unique[i].Key < unique[j].Key })
	return unique
}
