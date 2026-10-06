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
	"github.com/RedHuang-0622/seelex/seelebridge/workunit"
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
// 取值直接绑契约（workunit.RecoveryNoteRole）：两层注入同一个 role，不许各写一份。
const SubagentRecoveryNoteRole = workunit.RecoveryNoteRole

// subagentRecoveryNotePrefix 是恢复说明的稳定前缀（测试与审计据此识别）。
//
// 它是契约前缀族 workunit.RecoveryNotePrefix（`[Seelex recovery note: interrupted`）
// 的 subagent 一支——`workunit.RecoveryNote(KindSubagent, record)` 生成的正文必然以
// 本前缀开头（workunit_node_test.go 把这条等式钉成硬断言）。
const subagentRecoveryNotePrefix = "[Seelex recovery note: interrupted subagent"

// subagentResumeState 在 Runtime 内保存恢复期状态：
//   - notes：节点 → 恢复说明（仅该节点的下一次装配读取，收敛后清除；容器只有一份，
//     见 resume_notes.go —— 键是节点 id 这一件事留在本层）；
//   - parentRepair：父侧历史补齐钩子（application 组合根注入）。
type subagentResumeState struct {
	mu           sync.Mutex
	notes        resumeNotes
	parentRepair func(sessionID string) error
	// redispatch 覆盖“同键重跑”的传输实现（默认 DirectDispatch fork_subagents；
	// 测试与替换传输用）。与 parentRepair 同锁保护。
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
// 非恢复轮次返回空串）。**非消费读**：同一个节点的同一次装配可能读多次。
func (r *Runtime) SubagentResumeNote(nodeID string) string {
	if r == nil {
		return ""
	}
	return r.subagentResume.notes.peek(nodeID)
}

func (r *Runtime) setSubagentResumeNote(nodeID, note string) {
	if r == nil {
		return
	}
	r.subagentResume.notes.set(nodeID, note)
}

func (r *Runtime) clearSubagentResumeNote(nodeID string) {
	if r == nil {
		return
	}
	r.subagentResume.notes.clear(nodeID)
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
			Status:          nodeStateOfRecord(record.Status),
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
	// 读法只有一份（`workunit.UnitReader`：List → 定位 → 折算）；折算也只有一份
	// （`workunit.ProgressOf`）——此前这里是"自己 List + 自己判 InFlight + 自己取字段"的
	// 第二份实现，与 teammate 侧、与生命周期的记录读面各写一遍。
	reader := workunit.NewUnitReader(p.runtime.nodeSessionStore, p.projectID, p.mainSessionID, workunit.KindSubagent, nil)
	records, err := reader.Records()
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
		progress := workunit.ProgressOf(workunit.KindSubagent, record)
		key := progress.NodeID
		if key == "" {
			key = progress.SessionID
		}
		if key == "" {
			continue
		}
		p.records[key] = record
		status := resume.UnitDone
		switch {
		// "还在跑"的判据只有一份（workunit.InFlight / Progress.InFlight）：与 teammate 侧、
		// 与树投影用的是同一条，不再在这里写一遍字面量。
		case progress.InFlight:
			status = resume.UnitActive
		case nodeStateOfRecord(progress.Status) == dto.SubAgentFailed:
			status = resume.UnitFailed
		}
		if _, concluded := conclusions[key]; concluded {
			// 结论唯一：已有结论 → 残留记录只是半同步残留，按终结处理。
			status = resume.UnitDone
		}
		units = append(units, resume.Unit{
			Key: key, Kind: subagentResumeKind, Status: status,
			SessionID: progress.SessionID, ParentSessionID: p.mainSessionID,
			Goal: progress.Goal, UpdatedAt: progress.UpdatedAt,
		})
	}
	return units, nil
}

// ── 记录那一格的终态词表（字面量只有一份，在对外契约里）────────────────────
//
// 终态**不进 workunit 契约**：它由记录写方按自己的语义定名（teammate 记 done|failed，子代理
// 记它自己的两个词），契约只钉"在跑"那两个（workunit.StatusQueued / StatusRunning + InFlight）。
// 但"终态不进契约"不等于"每个写方各写一份字面量"——③U6 之后四个取值面只有 `dto.SubAgent*`
// 一份（记录词 = wire 词，逐条互锁见 TestSubagentStatusVocabularyAgreesWithTheWire），
// 本层这两个名是**转调**：Locate 判"是否终结"、ensureConclusion 判"记录本身已是终结态"
// 都读这里，teammate 侧（teamUnitStatusDone/Failed）读同一处。
//
// nodeStateOfRecord 把**落盘记录里的状态词**折成契约枚举——落盘格的唯一转换点。
//
// sessionstore 在契约之下（存的是词，不是枚举），所以这里折一次。认不得的词落
// dto.SubAgentUnknown：**读旧文件/外来文件不许炸**，而且 Unknown **不是终态**——
// "读不懂"绝不会被折算成"已完成"（判定面在 Locate/ensureConclusion 的保守分支里）。
//
// 与进程内 wire 的严格口径（dto.UnmarshalJSON 认不得就报错）是**两种取舍**，不是重复：
// 进程内的词由我们同一份代码写出，读不懂就是 bug；落盘的词要跨版本、跨机器读回来。
func nodeStateOfRecord(wire string) dto.SubAgentNodeStatus {
	if state, ok := dto.ParseSubAgentNodeStatus(wire); ok {
		return state
	}
	return dto.SubAgentUnknown
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
//
// 正文来自契约的唯一构建器（workunit.RecoveryNote）：前缀族与事实项集合
// （目标/状态/阶段/结论/错误/现场）与 teammate 层因此是同一份东西。契约 builder
// 不收的两个字段——阶段打点推出的"最后已知进展"与回灌的历史长度——在 builder
// **外面**追上去，不丢事实。
func (p *subagentResumePort) InjectNote(_ context.Context, unit resume.Unit, scene resume.Scene) error {
	record := p.records[unit.Key]
	note := workunit.RecoveryNote(workunit.KindSubagent, record) +
		subagentResumeExtraFacts(record, scene.Summary)
	p.runtime.setSubagentResumeNote(unit.Key, note)
	return nil
}

// subagentResumeExtraFacts 追加契约 builder 之外的记录事实（可以为空）：
//
//   - last known progress：阶段打点里最后一条预览（人可读的"之前做到哪"）；
//   - recovered messages：回灌回来的历史长度。
//
// 两条都有界（progress 已被 RestoreScene 截到 240 字节内，条数是一个整数）。
func subagentResumeExtraFacts(record sessionstore.NodeSessionRecord, progress string) string {
	builder := &strings.Builder{}
	if progress = strings.TrimSpace(progress); progress != "" {
		builder.WriteString("\n- last known progress: ")
		builder.WriteString(progress)
	}
	if len(record.History) > 0 {
		builder.WriteString(fmt.Sprintf("\n- recovered messages: %d", len(record.History)))
	}
	return builder.String()
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
	if state := nodeStateOfRecord(record.Status); state != dto.SubAgentDone && state != dto.SubAgentFailed {
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

// lastSubagentStagePreview 读取阶段日志里最后一条预览（恢复说明的“之前做到哪”）。
//
// 解码走契约那**唯一一份**（`workunit.DecodeStages`）：此前这里手写了一个匿名结构自己解
// 同一串载荷——同一件事的第二份解码实现，载荷形状一变只有一处会跟着改。
func lastSubagentStagePreview(record sessionstore.NodeSessionRecord) string {
	stages := workunit.DecodeStages(record.StagesJSON)
	if len(stages) == 0 {
		return ""
	}
	last := stages[len(stages)-1]
	if last.Preview == "" {
		return last.Stage
	}
	return last.Stage + ": " + last.Preview
}

// truncateSubagentResumePreview 截断摘要（报告/恢复说明用）——转调契约那**唯一一份**预览
// 裁剪（`workunit.ClipPreview`：按 rune 裁）。
//
// 此前这里是第四份实现，而且按**字节**裁：超过 240 字节的中文摘要会被从多字节字符中间切开，
// 恢复说明与报告里因此会出现非法 UTF-8；同一件事的"有界"也漂成了两个数（200 / 240）。
func truncateSubagentResumePreview(value string) string {
	return workunit.ClipPreview(value)
}
