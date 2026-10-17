package fork

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/backend/limit/loop"
	"github.com/RedHuang-0622/seelex/backend/tool/task"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/model"
	seetelemetry "github.com/RedHuang-0622/seelex/internal/telemetry"
)

// Deps 是 fork_subagents 工具执行所需的运行时回调集合，由根包（Runtime）注入。
type Deps struct {
	CurrentSubagentPolicy func(context.Context) dto.SubagentPolicy
	// RunSubagent 直接执行一条子代理（不经过 Plan DAG）：入参是 spec 身份与目标，
	// 返回该子代理的产出。worktree/SceneScope/merge-back 仍在执行体里。
	RunSubagent              func(ctx context.Context, id, goal, taskID string, maxLoops int) (string, error)
	TaskResolveByKeyFor      func(sessionID, key string) (task.TaskRecord, bool, error)
	TaskAddFor               func(sessionID string, spec task.TaskSpec) (task.TaskRecord, bool, error)
	TaskSetStatusFor         func(sessionID, id string, status task.TaskStatus, evidence string) (task.TaskRecord, error)
	TaskAttachParticipant    func(id, participant string) (task.TaskRecord, error)
	SubagentTreeRegisterFork func(mainSessionID, parentID string, specs []SubagentSpec)
	SubagentTreeSummaryFor   func(specID string) string
	ForkTimeoutSec           int
	PlanNodeMaxLoops         int
	// LoopLimitCeiling 是**进程级循环次数天花板**（limits.input_loop_limit，默认
	// 9999，与循环次数拉条同一份哨兵）：一条子代理自己声明的 max_loops 不得越过它。
	// 刻意不接 effort 档位的节点上限（SubagentPolicy.MaxNodeLoops）——那是主代理
	// plan_load 节点的判据，拿它卡子代理就是"设置 reasoning effort 连带改变了工作
	// 单元的次数"（2026-10-08 口径）。0 = 未配置（不限）。
	LoopLimitCeiling int
	// Jobs 是子代理作业的登记面（Kind=subagent，打点 L-5）。nil = async 模式不可用。
	Jobs SubagentJobs
}

// Tool 是 fork_subagents 的执行编排：B6 task 幂等登记 → 结果复用（省 token）
// → 构造 fork DAG → 子代理树登记 → planExecutor 执行（含超时与取消级联）。
type Tool struct {
	deps Deps
}

// NewTool 构造 fork 工具（deps 全部为闭包，域内不依赖根包）。
func NewTool(deps Deps) *Tool {
	return &Tool{deps: deps}
}

// Handle 是 fork_subagents 的执行入口。
func (t *Tool) Handle(ctx context.Context, argsJSON string) (string, error) {
	var input Input
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("fork_subagents: invalid args: %w", err)
	}
	if len(input.Subagents) == 0 {
		return "", fmt.Errorf("fork_subagents: at least one subagent is required")
	}
	seen := make(map[string]bool, len(input.Subagents))
	policy := t.deps.CurrentSubagentPolicy(ctx)
	for _, spec := range input.Subagents {
		if spec.ID == "" || strings.TrimSpace(spec.Goal) == "" {
			return "", fmt.Errorf("fork_subagents: subagent %q must have id and goal", spec.ID)
		}
		if seen[spec.ID] {
			return "", fmt.Errorf("fork_subagents: duplicate subagent id %q", spec.ID)
		}
		seen[spec.ID] = true
		// 子代理循环上限的判据只有一份：backend/limit/loop（进程级天花板）。
		if err := loop.ValidateSubagentMaxLoops(spec.ID, spec.MaxLoops, t.deps.LoopLimitCeiling); err != nil {
			return "", err
		}
	}
	// 护杠：数量上限（SubagentPolicy.MaxNodes；未配置不限）。
	if policy.MaxNodes > 0 && len(input.Subagents) > policy.MaxNodes {
		return "", fmt.Errorf("fork_subagents: %d subagents exceeds policy limit %d", len(input.Subagents), policy.MaxNodes)
	}

	// B6 装配件：fork 派工前做 task 幂等校验——按归一化 goal 查注册表；
	// 命中 → 绑既有 task_id；未命中 → 子代理自己开一个 task。只给 task_id，
	// 不注入 task 内容（保持子代理 prompt 格式纯净）。
	//
	// 会话归属：读写一律走**发起 fork 的会话**（ctx 会话键）的 scope，不用
	// 无会话的实时注册表入口——后台会话（视图已切走，它仍在并行跑）里 fork 的
	// 子代理否则会把行写进**当前视图会话**的注册表，"这一行属于谁"取决于谁在
	// 看，工作表格「仅本会话」当场张冠李戴（2026-09-29 与子代理树同一批收口）。
	sessionID := seetelemetry.SessionIDFromContext(ctx)
	taskBindings := make(map[string]string, len(input.Subagents))
	for _, spec := range input.Subagents {
		if taskID := t.bindSubagentTask(sessionID, spec); taskID != "" {
			taskBindings[spec.ID] = taskID
		}
	}

	// 结果复用（省 token）：若所有子代理都命中"既有已完成 task + 子代理树
	// 保留完整输出"（典型场景：结果返回失败——final_output 被截断或
	// read_tool_result 失败——需要 retry），这一批不重新执行，直接把已保存输出
	// 交给作业正文。只有全部命中才走复用；部分命中仍整体重跑，避免一批作业
	// 出现混合来源的正文（保守策略，README 注明）。
	//
	// 复用同样走作业面：作业照样登记（回执形状与真跑一批逐字段一致），执行体立刻
	// 把已保存输出写进各自正文并合成终态——省 token 的效果保留，"调用即返回句柄"
	// 这条契约不被破例。
	if summaries, ok := t.reusableForkSummaries(sessionID, input.Subagents); ok {
		return t.dispatchReusedJobs(ctx, input, taskBindings, summaries)
	}

	// 子代理树（内存态，不落盘）：记录 parent/child 链——父节点是发起
	// fork 的子代理（SceneScope 携带节点 ID；嵌套 fork）或主代理（main 合成根）。
	// 归属主会话按执行 ctx 的会话键标注（嵌套 fork 由树从父节点继承）：工作
	// 表格行按它归属，缺了它就只能按"谁触发同步"归属（跨会话污染）。
	parentID := dto.MainAgentNodeID
	if scope, ok := dto.SceneScopeFromContext(ctx); ok && scope.SceneID != "" && scope.Role == model.RoleSubAgent {
		parentID = scope.SceneID
	}
	t.deps.SubagentTreeRegisterFork(seetelemetry.SessionIDFromContext(ctx), parentID, input.Subagents)
	// fork 超时护栏：一批子代理的编排总时长 = 全部子代理工作量之和；通用工具
	// 超时会掐死长任务，故用自己的上限。任务可按需分配：长任务不填
	// （limits.fork_timeout，默认 2h）；简单审查/只读任务用 input.timeout_sec
	// 给 1200s（20 分钟）等更紧上限。
	forkTimeout := time.Duration(input.TimeoutSec) * time.Second
	if forkTimeout <= 0 {
		forkTimeout = time.Duration(t.deps.ForkTimeoutSec) * time.Second
	}
	if forkTimeout <= 0 {
		forkTimeout = 2 * time.Hour
	}
	// 作业化派发（打点 L-5）：这一批子代理登记成 Kind=subagent 的作业，**派发即返回**。
	//
	// 为什么必须作业化（而不是在阻塞调用里同步跑）：阻塞调用期间模型没有下一次
	// 调用，"主动查看/提前终止子代理"在阻塞形态下根本没有入口——不是缺工具，是缺
	// 时机。作业化之后 observe / kill / done 才有意义（设计文档 §B.3）。
	return t.dispatchJobs(ctx, input, taskBindings, forkTimeout)
}

// dispatchJobs 把这一批子代理**作业化派发**：登记 N 条 Kind=subagent 作业 → 立刻返回
// 受理回执（每个子代理一个句柄）→ 编排跑在后台 goroutine 里，收尾时把每个子代理的
// 产出写进它自己的作业正文并合成终态。
//
// 三条纪律与后台命令同源（见 tools/async_run.go 的头注）：
//   - ctx 用 `context.WithoutCancel` 摘掉工具调用的截止时间：回执一返回，本次调用的
//     ctx 就失效，沿用它会把刚起的编排立刻取消；存活上限由自己的 forkTimeout 兜住；
//   - **不**挂 `context.AfterFunc(ctx, cancel)`：作业活过这一轮，取消只能来自
//     `bg_manage(op=kill)` 与会话销毁（它们走登记表里的取消口）；
//   - 收尾必须落到 Complete（恰好一次）：跑完、失败、被取消三条路都不能让句柄停在
//     running——否则那一行会永远显示在打点表上。
func (t *Tool) dispatchJobs(ctx context.Context, input Input, taskBindings map[string]string, timeout time.Duration) (string, error) {
	if t.deps.Jobs == nil {
		return "", fmt.Errorf("fork_subagents: 子代理作业面不可用（未装配或 limits.async_exec.enabled=false）；" +
			"子代理派发只走作业面，不静默退化成阻塞调用")
	}
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	if sessionID := seetelemetry.SessionIDFromContext(ctx); sessionID != "" {
		runCtx = seetelemetry.WithSessionID(runCtx, sessionID)
	}
	sessionID := seetelemetry.SessionIDFromContext(ctx)

	type dispatched struct {
		spec   SubagentSpec
		handle string
	}
	jobs := make([]dispatched, 0, len(input.Subagents))
	for _, spec := range input.Subagents {
		handle, err := t.deps.Jobs.Add(SubagentJobSpec{ID: spec.ID, Goal: spec.Goal, SessionID: sessionID}, cancel)
		if err != nil {
			cancel()
			// 已经登记的那些不能让它们停在 running：合成失败终态（终态只由执行体判定
			// 的纪律在这里的落实是"编排根本没起来"）。
			for _, created := range jobs {
				t.deps.Jobs.Complete(created.handle, dto.AsyncStateFailed)
			}
			return "", fmt.Errorf("fork_subagents: 登记子代理作业失败: %w", err)
		}
		jobs = append(jobs, dispatched{spec: spec, handle: handle})
	}

	go func() {
		defer cancel()
		var wg sync.WaitGroup
		for _, item := range jobs {
			wg.Add(1)
			go func(item dispatched) {
				defer wg.Done()
				loops := t.deps.PlanNodeMaxLoops
				if item.spec.MaxLoops > 0 {
					loops = item.spec.MaxLoops
				}
				output, runErr := t.deps.RunSubagent(runCtx, item.spec.ID, item.spec.Goal, taskBindings[item.spec.ID], loops)
				state := dto.AsyncStateDone
				switch {
				case runErr != nil && runCtx.Err() != nil:
					state = dto.AsyncStateKilled
				case runErr != nil:
					state = dto.AsyncStateFailed
				}
				body := strings.TrimSpace(output)
				if body == "" {
					body = strings.TrimSpace(t.deps.SubagentTreeSummaryFor(item.spec.ID))
				}
				if runErr != nil {
					reason := strings.TrimSpace(runErr.Error())
					if reason != "" && !strings.Contains(body, reason) {
						if body != "" {
							body += "\n"
						}
						body += reason
					}
				}
				if body != "" {
					t.deps.Jobs.Note(item.handle, body+"\n")
				}
				t.deps.Jobs.Complete(item.handle, state)
			}(item)
		}
		wg.Wait()
	}()

	receipts := make([]map[string]string, 0, len(jobs))
	for _, item := range jobs {
		receipts = append(receipts, map[string]string{
			// 这一栏是**后台作业状态那一格**（`dto.AsyncState*`）——同文件下面调
			// `deps.Jobs.Complete` 时用的就是同一格的枚举，回执不许再写一份词。
			"handle": item.handle, "id": item.spec.ID, "state": dto.AsyncStateRunning.String(),
		})
	}
	return acceptanceReceipt(receipts, "这批子代理已作业化派发（调用本身不等结果）。用 bg_manage(op=observe, handle) "+
		"看它们在干什么（不消费输出）、op=fetch 取回产出、op=kill 提前终止（已产出内容不丢）、"+
		"op=done 结清终态行。注意：任意一个句柄的 kill 会取消**整批**编排（它们共用一次 plan run）。")
}

// dispatchReusedJobs 把"结果复用"的批次也作业化：作业照样登记（回执形状与真跑一批
// 逐字段一致，模型侧看不到两套形状），执行体立刻把子代理树里已保存的输出写进各自
// 正文并合成终态——既不重跑（省 token），也不破"调用即返回句柄"这条契约。
//
// 只有**全部** spec 命中才走到这里（见 reusableForkSummaries）：部分命中仍整体重跑，
// 避免一批作业出现混合来源的正文。
func (t *Tool) dispatchReusedJobs(ctx context.Context, input Input, taskBindings map[string]string, summaries map[string]string) (string, error) {
	if t.deps.Jobs == nil {
		return "", fmt.Errorf("fork_subagents: 子代理作业面不可用（未装配或 limits.async_exec.enabled=false）；" +
			"子代理派发只走作业面，不静默退化成阻塞调用")
	}
	sessionID := seetelemetry.SessionIDFromContext(ctx)
	// 取消口照挂：作业面要求每条作业都能被 kill / 会话销毁取消，即使这一批注定
	// 立刻收尾（形状一致比"这次用不上"重要）。
	_, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()

	receipts := make([]map[string]string, 0, len(input.Subagents))
	for _, spec := range input.Subagents {
		handle, err := t.deps.Jobs.Add(SubagentJobSpec{ID: spec.ID, Goal: spec.Goal, SessionID: sessionID}, cancel)
		if err != nil {
			// 已登记的那些不能让它们停在 running（同 dispatchJobs 的收尾纪律）。
			for _, created := range receipts {
				t.deps.Jobs.Complete(created["handle"], dto.AsyncStateFailed)
			}
			return "", fmt.Errorf("fork_subagents: 登记子代理作业失败: %w", err)
		}
		if body := strings.TrimSpace(summaries[spec.ID]); body != "" {
			t.deps.Jobs.Note(handle, body+"\n")
		}
		t.deps.Jobs.Complete(handle, dto.AsyncStateDone)
		if taskID := taskBindings[spec.ID]; taskID != "" {
			// bindSubagentTask 已把终态 task 置 retry（RetryCount 自增）；复用成功 →
			// 置回 completed，计数保留（worktable 显示 DONE，retry_count 保留）。
			_, _ = t.deps.TaskSetStatusFor(sessionID, taskID, task.TaskCompleted, "fork reused stored output")
		}
		receipts = append(receipts, map[string]string{
			"handle": handle, "id": spec.ID, "state": dto.AsyncStateDone.String(),
		})
	}
	return acceptanceReceipt(receipts, "这批子代理的结论**复用上次已保存输出**（未重新执行，省 token）："+
		"句柄已是终态，用 bg_manage(op=fetch, handle) 取回各自正文。如需真正重跑，先清子代理树再派发。")
}

// acceptanceReceipt 渲染作业化派发的受理回执：这是本次工具调用的**全部返回**
// （派发即结束）。回执只带句柄与状态，绝不携带子代理产出——产出走
// bg_manage(op=fetch, handle)（消费式增量），两条路径共用同一份载荷形状。
func acceptanceReceipt(receipts []map[string]string, hint string) (string, error) {
	payload := map[string]any{
		"status":     "accepted",
		"node_count": len(receipts),
		"jobs":       receipts,
		"hint":       hint,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("fork_subagents: 渲染受理回执失败: %w", err)
	}
	return string(encoded), nil
}

// reusableForkSummaries 检查每个 spec 是否可复用已保存输出：goal 命中的
// 既有 task 已完成，且子代理树仍保留该节点的完整输出。全部命中才返回
// （摘要表, true）；任一缺失返回 (nil, false)。
//
// sessionID 是发起 fork 的会话：查重必须落在**这个会话的 scope**——后台会话里
// 的 fork 若查的是实时注册表（别的会话），命中判定会张冠李戴（可能复用别的会话
// 的同 goal task，也可能看不见自己在办的）。
func (t *Tool) reusableForkSummaries(sessionID string, specs []SubagentSpec) (map[string]string, bool) {
	if len(specs) == 0 {
		return nil, false
	}
	summaries := make(map[string]string, len(specs))
	for _, spec := range specs {
		if _, found, _ := t.deps.TaskResolveByKeyFor(sessionID, task.TaskKeyForGoal(spec.Goal)); !found {
			return nil, false
		}
		if summary := t.deps.SubagentTreeSummaryFor(spec.ID); summary == "" {
			return nil, false
		} else {
			summaries[spec.ID] = summary
		}
	}
	return summaries, true
}

// bindSubagentTask 解析/创建子代理 task 并返回 task_id（幂等：相同 goal
// 命中同一 task；新开时以 subagent:<id> 作为 ID，随后参与者合并）。
//
// sessionID 是发起 fork 的会话（调用方按 ctx 会话键给出）：查重、新建与状态
// 打点都落在**这个会话的 scope**（后台会话写自身分区，不用实时注册表入口）。
func (t *Tool) bindSubagentTask(sessionID string, spec SubagentSpec) string {
	key := task.TaskKeyForGoal(spec.Goal)
	if existing, found, _ := t.deps.TaskResolveByKeyFor(sessionID, key); found {
		// 既有 task 被子代理重新接手：
		//   - 终态（completed/failed）→ 重试语义：置 retry（RetryCount
		//     自增，worktable 显示 RETRY n），节点真正启动时再转 running；
		//   - 已在 retry/running/doing → 保持现状（不降级）；
		//   - 其余（pending/queued）→ 排队（B5 生命周期打点）。
		switch existing.Status {
		case task.TaskCompleted, task.TaskFailed:
			_, _ = t.deps.TaskSetStatusFor(sessionID, existing.ID, task.TaskRetry, "fork retried")
		case task.TaskRetry, task.TaskRunning, task.TaskDoing:
			// 保持当前状态（retry 保留计数；running 不允许回退）。
		default:
			_, _ = t.deps.TaskSetStatusFor(sessionID, existing.ID, task.TaskQueued, "fork scheduled")
		}
		return existing.ID
	}
	created, _, err := t.deps.TaskAddFor(sessionID, task.TaskSpec{
		ID: "subagent:" + spec.ID, Key: key, Phase: task.TaskPhaseSubagent, Task: spec.Goal,
		Kind: "subagent", SourceID: spec.ID,
	})
	if err != nil {
		return ""
	}
	_, _ = t.deps.TaskSetStatusFor(sessionID, created.ID, task.TaskQueued, "fork scheduled")
	return created.ID
}
