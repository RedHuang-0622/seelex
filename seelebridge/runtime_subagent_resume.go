package seelebridge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/resume"
	"github.com/RedHuang-0622/seelex/seelebridge/fork"
	seetelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// 子代理中断恢复：subagent 是劳务派遣式 tool calling 能力（不是 AgentTeam
// 成员）。终止前没做完的子代理按 interrupted tool-chain 语义续跑：
//
//	残留记录（active 且无结论）→ 重建现场 → system 注入恢复说明 → 同键重跑
//
// 七步模板本体在 application/core/resume（领域无关）；本文件只提供 subagent
// 适配器与现场/注入/重跑/收敛的具体实现。
//
// 设计依据：docs/arch/a2a-agent-team-factory.md §5/§5.1，AT7/AT10。

// subagentResumeKind 是恢复模板里的单元种类标识。
const subagentResumeKind = "subagent"

// SubagentRecoveryNoteRole 是恢复说明使用的 provider role。
//
// 恒为 system：恢复说明是 Seelex 编排事实，不是模型发言，也不是用户输入
// （AT9）。自定义角色名不被 provider 接受，身份只能走 role_name metadata。
const SubagentRecoveryNoteRole = "system"

// subagentRecoveryNotePrefix 是恢复说明的稳定前缀（测试与审计据此识别）。
const subagentRecoveryNotePrefix = "[Seelex recovery note: interrupted subagent"

// subagentResumeState 在 Runtime 内保存恢复期状态：
//   - notes：节点 → 恢复说明（仅该节点的下一次装配读取，收敛后清除）；
//   - parentRepair：父侧历史补齐钩子（application 组合根注入）。
type subagentResumeState struct {
	mu           sync.Mutex
	notes        map[string]string
	parentRepair func(sessionID string) error
	// redispatch 覆盖“同键重跑”的传输实现（默认 DirectDispatch fork_subagents；
	// 测试与替换传输用）。与 notes/parentRepair 同锁保护。
	redispatch func(ctx context.Context, nodeID, goal string) (string, error)
}

// SetSubagentParentRepairer 注入父侧历史补齐钩子（缺失的子代理结果 →
// provider-only tool 占位）。组合根须在冷恢复前接线；未接线时该步退化为
// no-op —— 装配层的 RepairInterruptedToolChains 仍会在每次请求前补齐。
func (r *Runtime) SetSubagentParentRepairer(fn func(sessionID string) error) {
	if r == nil {
		return
	}
	r.subagentResume.mu.Lock()
	r.subagentResume.parentRepair = fn
	r.subagentResume.mu.Unlock()
}

// SubagentResumeNote 返回节点当前的恢复说明（node 装配 PromptBlocks 时读取；
// 非恢复轮次返回空串）。
func (r *Runtime) SubagentResumeNote(nodeID string) string {
	if r == nil || strings.TrimSpace(nodeID) == "" {
		return ""
	}
	r.subagentResume.mu.Lock()
	defer r.subagentResume.mu.Unlock()
	return r.subagentResume.notes[nodeID]
}

func (r *Runtime) setSubagentResumeNote(nodeID, note string) {
	if r == nil || nodeID == "" || note == "" {
		return
	}
	r.subagentResume.mu.Lock()
	if r.subagentResume.notes == nil {
		r.subagentResume.notes = map[string]string{}
	}
	r.subagentResume.notes[nodeID] = note
	r.subagentResume.mu.Unlock()
}

func (r *Runtime) clearSubagentResumeNote(nodeID string) {
	if r == nil || nodeID == "" {
		return
	}
	r.subagentResume.mu.Lock()
	delete(r.subagentResume.notes, nodeID)
	r.subagentResume.mu.Unlock()
}

// setSubagentRedispatchHook 覆盖同键重跑的传输实现（nil = 默认
// DirectDispatch fork_subagents）。测试与替换传输用，不扩大公开契约。
func (r *Runtime) setSubagentRedispatchHook(fn func(ctx context.Context, nodeID, goal string) (string, error)) {
	if r == nil {
		return
	}
	r.subagentResume.mu.Lock()
	r.subagentResume.redispatch = fn
	r.subagentResume.mu.Unlock()
}

// ListSubagentRecovery 列出目标主会话下残留的子代理单元及其恢复态
// （headless/GUI 表格数据源；不改变任何状态、不重跑）。
func (r *Runtime) ListSubagentRecovery(sessionID string) ([]dto.SubagentRecoveryView, error) {
	port, err := r.newSubagentResumePort(sessionID)
	if err != nil {
		return nil, err
	}
	units, err := port.Locate(context.Background())
	if err != nil {
		return nil, err
	}
	views := make([]dto.SubagentRecoveryView, 0, len(units))
	for _, unit := range units {
		record := port.records[unit.Key]
		_, concluded := port.conclusions[unit.Key]
		views = append(views, dto.SubagentRecoveryView{
			NodeID:          unit.Key,
			SessionID:       record.SessionID,
			Goal:            unit.Goal,
			Status:          record.Status,
			Active:          unit.Active(),
			ConclusionFound: concluded,
			Resumable:       unit.Active() && !concluded,
			Summary:         record.Summary,
			Error:           record.Error,
			WorktreePath:    record.Worktree.Path,
			UpdatedAt:       record.UpdatedAt,
		})
	}
	return views, nil
}

// ResumeInterruptedSubagents 冷恢复续跑目标会话下所有未完成子代理：
// 七步模板（定位 → 判定 → 补历史 → 重建现场 → system 注入 → 同键重跑 →
// 收敛）；已终结残留只做幂等清理，不重启。
func (r *Runtime) ResumeInterruptedSubagents(ctx context.Context, sessionID string) (dto.SubagentResumeReport, error) {
	report := dto.SubagentResumeReport{
		SessionID:        sessionID,
		RecoveryNoteRole: SubagentRecoveryNoteRole,
		StartedAt:        time.Now(),
	}
	port, err := r.newSubagentResumePort(sessionID)
	if err != nil {
		report.FinishedAt = time.Now()
		return report, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	outcome, err := resume.NewRunner(port).Execute(ctx)
	report.FinishedAt = time.Now()
	report.Located = outcome.Located
	report.Units = make([]dto.SubagentResumeResult, 0, len(outcome.Units))
	for _, unit := range outcome.Units {
		steps := make([]string, 0, len(unit.Steps))
		for _, step := range unit.Steps {
			steps = append(steps, string(step))
		}
		report.Units = append(report.Units, dto.SubagentResumeResult{
			NodeID: unit.Key, Resumed: unit.Resumed, Skipped: unit.Skipped,
			Steps: steps, NoteRole: resumeNoteRole(unit.Steps),
			Summary: unit.Summary, Error: unit.Err,
		})
	}
	report.Resumed = outcome.Resumed()
	report.Skipped = outcome.Skipped()
	for _, failure := range outcome.Failed() {
		report.Failed = append(report.Failed, failure.Key)
	}
	if err != nil {
		return report, err
	}
	return report, nil
}

// ResumeSubagent 定点续跑单个子代理（幂等键 = 节点 ID；失败可重试）。
func (r *Runtime) ResumeSubagent(ctx context.Context, sessionID, nodeID string) (dto.SubagentResumeResult, error) {
	result := dto.SubagentResumeResult{NodeID: nodeID}
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return result, fmt.Errorf("resume subagent: node id is required")
	}
	port, err := r.newSubagentResumePort(sessionID)
	if err != nil {
		return result, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	unit, err := resume.NewRunner(port).ExecuteKey(ctx, nodeID)
	if err != nil {
		return result, err
	}
	steps := make([]string, 0, len(unit.Steps))
	for _, step := range unit.Steps {
		steps = append(steps, string(step))
	}
	result.Resumed = unit.Resumed
	result.Skipped = unit.Skipped
	result.Steps = steps
	result.NoteRole = resumeNoteRole(unit.Steps)
	result.Summary = unit.Summary
	result.Error = unit.Err
	return result, nil
}

// ForkSubagents 直接派发一批子代理（自动化/冒烟入口）。
//
// 与模型调用 fork_subagents 走同一条执行链：fork DAG → 真实子代理会话
// （worktree/预算/skill 全走既有路径）→ summary 汇总 → 结论回流。它只是
// 把"由模型决定派发"换成"由调用方指定派发"，不新增旁路；子代理依旧不是
// AgentTeam 成员（AT1）。
func (r *Runtime) ForkSubagents(ctx context.Context, sessionID string, specs []dto.SubagentForkSpec) (string, error) {
	if r == nil || r.agt == nil {
		return "", fmt.Errorf("fork subagents: agent is not assembled")
	}
	if len(specs) == 0 {
		return "", fmt.Errorf("fork subagents: at least one subagent is required")
	}
	input := fork.Input{Subagents: make([]fork.SubagentSpec, 0, len(specs))}
	for _, spec := range specs {
		if strings.TrimSpace(spec.ID) == "" || strings.TrimSpace(spec.Goal) == "" {
			return "", fmt.Errorf("fork subagents: subagent %q must have id and goal", spec.ID)
		}
		input.Subagents = append(input.Subagents, fork.SubagentSpec{ID: spec.ID, Goal: spec.Goal})
	}
	args, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("fork subagents: encode input: %w", err)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if sessionID != "" {
		ctx = seetelemetry.WithSessionID(ctx, sessionID)
	}
	return r.agt.DirectDispatch(ctx, "fork_subagents", string(args))
}

// resumeNoteRole 报告实际注入过的恢复说明 role（未注入 → 空串）。
// 恒为 system：恢复说明是编排事实，不是模型发言也不是用户输入（AT9）。
func resumeNoteRole(steps []resume.Step) string {
	for _, step := range steps {
		if step == resume.StepInjectNote {
			return SubagentRecoveryNoteRole
		}
	}
	return ""
}

// newSubagentResumePort 组装一次恢复所需的适配器（每次调用独立，不共享
// 可变状态；恢复现场以持久化记录为准）。
func (r *Runtime) newSubagentResumePort(sessionID string) (*subagentResumePort, error) {
	if r == nil {
		return nil, fmt.Errorf("resume subagent: runtime is nil")
	}
	if r.nodeSessionStore == nil {
		return nil, fmt.Errorf("resume subagent: node session store is not assembled")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("resume subagent: main session id is required")
	}
	projectID := ""
	if router := r.durableHistoryRouter(); router != nil {
		projectID = r.sessionProjectIDFor(sessionID)
	}
	return &subagentResumePort{
		runtime:       r,
		mainSessionID: sessionID,
		projectID:     projectID,
		records:       map[string]sessionstore.NodeSessionRecord{},
		conclusions:   map[string]struct{}{},
	}, nil
}

// subagentResumePort 是恢复模板的 subagent 适配器。
type subagentResumePort struct {
	runtime       *Runtime
	mainSessionID string
	projectID     string

	// Locate 的产物（同一次恢复轮次内复用）。
	records     map[string]sessionstore.NodeSessionRecord
	conclusions map[string]struct{}
}

// Locate 定位残留子代理单元：残留记录 + 父级结论事件。
//
// 判定口径（持久化事实，不用内存 active 标记）：
//   - 有结论事件 → 终结（done），只需幂等清理残留；
//   - status=queued/running 且无结论 → active，需要重启续跑；
//   - status=done/failed → 终结，不重启。
func (p *subagentResumePort) Locate(ctx context.Context) ([]resume.Unit, error) {
	records, err := p.runtime.nodeSessionStore.List(p.projectID, p.mainSessionID)
	if err != nil {
		return nil, fmt.Errorf("list node sessions: %w", err)
	}
	conclusions, err := p.runtime.subagentConclusionSet(ctx, p.projectID, p.mainSessionID)
	if err != nil {
		return nil, err
	}
	p.conclusions = conclusions
	p.records = make(map[string]sessionstore.NodeSessionRecord, len(records))
	units := make([]resume.Unit, 0, len(records))
	for _, record := range records {
		key := record.NodeID
		if key == "" {
			key = record.SessionID
		}
		if key == "" {
			continue
		}
		p.records[key] = record
		status := resume.UnitDone
		switch record.Status {
		case "queued", "running":
			status = resume.UnitActive
		case "failed":
			status = resume.UnitFailed
		}
		if _, concluded := conclusions[key]; concluded {
			// 结论唯一：已有结论 → 残留记录只是半同步残留，按终结处理。
			status = resume.UnitDone
		}
		units = append(units, resume.Unit{
			Key: key, Kind: subagentResumeKind, Status: status,
			SessionID: record.SessionID, ParentSessionID: p.mainSessionID,
			Goal: record.Goal, UpdatedAt: record.UpdatedAt,
		})
	}
	return units, nil
}

// RepairParent 补齐父侧历史：缺失的子代理结果 → provider-only tool 占位。
func (p *subagentResumePort) RepairParent(ctx context.Context, _ resume.Unit) error {
	p.runtime.subagentResume.mu.Lock()
	repair := p.runtime.subagentResume.parentRepair
	p.runtime.subagentResume.mu.Unlock()
	if repair == nil {
		// 未注入钩子时退化为 no-op：装配层每次请求前都会执行
		// RepairInterruptedToolChains，占位结果不会缺失。
		return nil
	}
	return repair(p.mainSessionID)
}

// RestoreScene 重建被执行单元自己的现场：目标/已知进展/工作树。
func (p *subagentResumePort) RestoreScene(_ context.Context, unit resume.Unit) (resume.Scene, error) {
	record, ok := p.records[unit.Key]
	if !ok {
		return resume.Scene{}, fmt.Errorf("scene record missing for %q", unit.Key)
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return resume.Scene{}, fmt.Errorf("encode scene: %w", err)
	}
	summary := strings.TrimSpace(record.Summary)
	if summary == "" {
		summary = lastSubagentStagePreview(record)
	}
	return resume.Scene{Payload: payload, Summary: summary}, nil
}

// InjectNote 生成并以 system 注入恢复说明：只进被执行单元自己的上下文
// （节点装配 PromptBlocks 时按节点 ID 读取），不进 main 历史。
func (p *subagentResumePort) InjectNote(_ context.Context, unit resume.Unit, scene resume.Scene) error {
	record := p.records[unit.Key]
	note := subagentRecoveryNote(record, scene.Summary)
	p.runtime.setSubagentResumeNote(unit.Key, note)
	return nil
}

// Reexecute 以同一幂等键（节点 ID = fork 子代理 id）重新派发：
// 同一 goal → 同一节点会话 ID → 同一份工作历史与结论槽位。
func (p *subagentResumePort) Reexecute(ctx context.Context, unit resume.Unit, _ resume.Scene) (resume.Outcome, error) {
	record := p.records[unit.Key]
	goal := strings.TrimSpace(record.Goal)
	if goal == "" {
		goal = strings.TrimSpace(unit.Goal)
	}
	if goal == "" {
		return resume.Outcome{}, fmt.Errorf("subagent %q has no goal to re-dispatch", unit.Key)
	}
	input := fork.Input{Subagents: []fork.SubagentSpec{{ID: unit.Key, Goal: goal}}}
	args, err := json.Marshal(input)
	if err != nil {
		return resume.Outcome{}, fmt.Errorf("encode fork input: %w", err)
	}
	p.runtime.subagentResume.mu.Lock()
	redispatch := p.runtime.subagentResume.redispatch
	p.runtime.subagentResume.mu.Unlock()
	if redispatch != nil {
		result, err := redispatch(ctx, unit.Key, goal)
		if err != nil {
			return resume.Outcome{}, err
		}
		return resume.Outcome{Status: resume.UnitDone, Summary: truncateSubagentResumePreview(result)}, nil
	}
	dispatchCtx := ctx
	if p.mainSessionID != "" {
		// fork 的 plan_run 按会话登记事件与工具结果：必须把主会话 ID
		// 重新注入 ctx，绝不能落进 legacy 默认槽。
		dispatchCtx = seetelemetry.WithSessionID(ctx, p.mainSessionID)
	}
	result, err := p.runtime.agt.DirectDispatch(dispatchCtx, "fork_subagents", string(args))
	if err != nil {
		return resume.Outcome{}, err
	}
	return resume.Outcome{Status: resume.UnitDone, Summary: truncateSubagentResumePreview(result)}, nil
}

// Converge 收敛：清除恢复说明；已有结论的残留记录幂等删除（结论唯一）。
// 重跑失败时保留记录，允许下一轮重试。
func (p *subagentResumePort) Converge(ctx context.Context, unit resume.Unit, outcome resume.Outcome) error {
	p.runtime.clearSubagentResumeNote(unit.Key)
	// 重跑失败：保留残留记录供下一轮重试，不伪造结论。
	if outcome.Err != nil || outcome.Status == resume.UnitFailed {
		return nil
	}
	if err := p.ensureConclusion(ctx, unit); err != nil {
		return err
	}
	conclusions, err := p.runtime.subagentConclusionSet(ctx, p.projectID, p.mainSessionID)
	if err != nil {
		return err
	}
	if _, concluded := conclusions[unit.Key]; !concluded {
		// 没有结论却"完成"是异常路径（例如事件库未装配），保留记录待下一轮。
		return nil
	}
	record, exists := p.records[unit.Key]
	if !exists || record.SessionID == "" {
		return nil
	}
	if err := p.runtime.nodeSessionStore.Delete(p.projectID, p.mainSessionID, record.SessionID); err != nil {
		return fmt.Errorf("delete converged node record: %w", err)
	}
	delete(p.records, unit.Key)
	return nil
}

// ensureConclusion 保证残留单元的结论已跟随父级（结论唯一）。
//
// 两条来源，都不伪造结果：
//   - 本次重跑正常收尾 → 运行期 finalize 已写结论；
//   - 残留记录本身已是终结态（done/failed）→ 记录里的终态事实即结论，
//     崩溃只发生在“写结论 + 删记录”之间，按其重放一次即可。
//
// 返回 nil 表示“已尽力”，调用方仍需复查结论是否存在才删除残留记录。
func (p *subagentResumePort) ensureConclusion(ctx context.Context, unit resume.Unit) error {
	conclusions, err := p.runtime.subagentConclusionSet(ctx, p.projectID, p.mainSessionID)
	if err != nil {
		return err
	}
	if _, concluded := conclusions[unit.Key]; concluded {
		return nil
	}
	record, exists := p.records[unit.Key]
	if !exists || record.SessionID == "" {
		return nil
	}
	// 重跑可能已删除/改写记录：以磁盘上的最新态为准。
	if latest, loadErr := p.runtime.nodeSessionStore.Load(p.projectID, p.mainSessionID, record.SessionID); loadErr == nil {
		record = latest
		p.records[unit.Key] = latest
	}
	if record.Status != "done" && record.Status != "failed" {
		return nil
	}
	record.MainSessionID = p.mainSessionID
	p.runtime.persistSubagentConclusion(p.mainSessionID, record)
	return nil
}

// subagentConclusionSet 读取父级事件库里的子代理结论（幂等键集合）。
func (r *Runtime) subagentConclusionSet(ctx context.Context, projectID, sessionID string) (map[string]struct{}, error) {
	router := r.durableHistoryRouter()
	if router == nil {
		return map[string]struct{}{}, nil
	}
	records, err := loadSubagentConclusions(ctx, router, projectID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("load subagent conclusions: %w", err)
	}
	set := make(map[string]struct{}, len(records))
	for _, record := range records {
		if record.NodeID != "" {
			set[record.NodeID] = struct{}{}
		}
	}
	return set, nil
}

// subagentRecoveryNote 生成 system 恢复说明：这是恢复、之前做到哪、接下来
// 继续什么。正文不谎称执行成功，也不把未完成状态写成模型发言。
func subagentRecoveryNote(record sessionstore.NodeSessionRecord, progress string) string {
	builder := &strings.Builder{}
	builder.WriteString(subagentRecoveryNotePrefix)
	builder.WriteString(": parent process stopped before the result was recorded.")
	builder.WriteString(" This dispatch was re-issued with the same node id, so continue the work instead of restarting it.]\n")
	builder.WriteString("- node_id: ")
	builder.WriteString(record.NodeID)
	builder.WriteString("\n- goal: ")
	builder.WriteString(strings.TrimSpace(record.Goal))
	if summary := strings.TrimSpace(record.Summary); summary != "" {
		builder.WriteString("\n- recorded summary: ")
		builder.WriteString(summary)
	}
	if progress = strings.TrimSpace(progress); progress != "" {
		builder.WriteString("\n- last known progress: ")
		builder.WriteString(progress)
	}
	if record.Worktree.Path != "" {
		builder.WriteString("\n- worktree: ")
		builder.WriteString(record.Worktree.Path)
	}
	if len(record.History) > 0 {
		builder.WriteString(fmt.Sprintf("\n- recovered messages: %d", len(record.History)))
	}
	builder.WriteString("\nContinue the unfinished part, verify side effects before redoing anything, and report the result once.")
	return builder.String()
}

// lastSubagentStagePreview 读取阶段日志里最后一条预览（恢复说明的“之前做到哪”）。
func lastSubagentStagePreview(record sessionstore.NodeSessionRecord) string {
	if len(record.StagesJSON) == 0 {
		return ""
	}
	var stages []struct {
		Stage   string `json:"stage"`
		Preview string `json:"preview"`
	}
	if err := json.Unmarshal(record.StagesJSON, &stages); err != nil || len(stages) == 0 {
		return ""
	}
	last := stages[len(stages)-1]
	if last.Preview == "" {
		return last.Stage
	}
	return last.Stage + ": " + truncateSubagentResumePreview(last.Preview)
}

// truncateSubagentResumePreview 截断摘要（报告/恢复说明用）。
func truncateSubagentResumePreview(value string) string {
	value = strings.TrimSpace(value)
	const limit = 240
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}
