package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// 作业契约的**工具面**（打点 L-1/L-2/L-3）：
//
//	bash_bg      —— Add：派发一条后台受管命令（Kind=process）
//	read_batch   —— Add：批量派发进程内读作业（Kind=inline）
//	job_manage   —— Manage：observe / fetch / kill / done（三类作业共用）
//
// 为什么取回与终止并成一个名字（M5 + L-4）：旧的取回工具把"取回"耦死在"后台命令"
// 上，而 inline / subagent 作业同样要取回；旧终止工具又把"终止"单独开一个名字，
// 于是管理面被拆成两个名字、且各自只认一种作业。契约里它们是**同一组管理动作**，
// 工具面就该长成同一个名字 + 一个 op 入参——这也正是 L-2/L-4 的判据（两个旧名字
// 在代码与配置里零残留）。

// ── job_manage：管理（Manage 的工具面）─────────────────────────────────

type jobManageInput struct {
	Op     JobOp  `json:"op"`
	Handle string `json:"handle,omitempty"`
	WaitMS int    `json:"wait_ms,omitempty"`
}

// scopedJobManage 是 Manage 的唯一工具入口。
//
// 授权：句柄只对本会话有效（跨会话管理一律拒绝）；管理本身不重复弹审批——它管的
// 是**已经获批的那次派发**（与旧取回工具同源的理由）。真正的门在 handler：
// 句柄必须属于本会话。
func (r *Router) scopedJobManage(ctx context.Context, argsJSON string) (string, error) {
	if !r.asyncEnabled() {
		return "", fmt.Errorf("job_manage: %s", asyncDisabledText)
	}
	var input jobManageInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("job_manage: invalid args: %w", err)
	}
	op := JobOp(strings.ToLower(strings.TrimSpace(string(input.Op))))
	if op == "" {
		return "", fmt.Errorf("job_manage: op is required (observe | fetch | kill | done)")
	}
	switch op {
	case JobOpObserve, JobOpFetch, JobOpKill, JobOpDone:
	default:
		return "", fmt.Errorf("job_manage: 未知 op %q（可用 observe | fetch | kill | done）", input.Op)
	}
	if op != JobOpObserve && strings.TrimSpace(input.Handle) == "" {
		return "", fmt.Errorf("job_manage: op=%s 需要 handle", op)
	}
	handle := JobHandle{Handle: strings.TrimSpace(input.Handle), WaitMS: input.WaitMS}
	manager := &jobManager{router: r}
	var (
		payload []byte
		err     error
	)
	switch op {
	case JobOpObserve:
		payload, err = manager.Status(ctx, handle)
	case JobOpFetch:
		payload, err = manager.Fetch(ctx, handle)
	case JobOpKill:
		payload, err = manager.Kill(ctx, handle)
	case JobOpDone:
		payload, err = manager.Done(ctx, handle)
	}
	if err != nil {
		return "", err
	}
	// 工具边界上转 string：framework 的 ToolHandler / tool_result 都是 string，
	// 契约内部保持 bytes（见 JobTool 的出参口径）。
	return string(payload), nil
}

// jobManageSchema 是 job_manage 的入参 schema。
func jobManageSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"op":      map[string]interface{}{"type": "string", "description": "observe | fetch | kill | done"},
			"handle":  map[string]interface{}{"type": "string"},
			"wait_ms": map[string]interface{}{"type": "integer"},
		},
		"required": []string{"op"},
	}
}

// jobManageDescription 说明四个 op 的语义，并明确三件容易踩的事：
// observe 不吃输出、fetch 是消费式、确认进展不必花一次往返（打点块已经列出来了）。
func jobManageDescription() string {
	return "Manage a dispatched job by handle (bash_bg / read_batch / subagent). " +
		"op=observe: read-only progress look (never advances the fetch cursor, so it never " +
		"consumes output); with no handle it lists every job in this session. " +
		"op=fetch: return only the bytes produced since the previous fetch for that handle " +
		"(consumer-style, never a replay of the whole log) — wait_ms: negative returns " +
		"immediately, 0 or omitted waits up to 5s, values above 60000 are capped; set it near " +
		"the expected remaining time. A terminal job is retired once fetched (its row leaves " +
		"the work table; the fetched result stays in the transcript). " +
		"op=kill: terminate the job (whole process tree / cancellation cascade); output already " +
		"produced is kept and still fetchable. " +
		"op=done: retire a terminal job (terminal state is only ever decided by the execution " +
		"body, so done on a running job is refused — use kill). Repeat calls are idempotent. " +
		"Handles are scoped to the calling session. Do not call observe just to check whether a " +
		"job is still running: the work-table trace block already lists every live job."
}

// ── bash_bg：后台受管命令（Add 的工具面）───────────────────────────────

type bashBgInput struct {
	// Description 是这条后台命令在做什么的一句话——它是工作打点表的行标题，必填
	// （同步执行不需要：结果就在那一轮的输出里）。
	Description string `json:"description"`
	Command     string `json:"command"`
	Timeout     int    `json:"timeout,omitempty"`
	Workdir     string `json:"workdir,omitempty"`
}

// scopedBashBg 派发一条后台命令：解析 workdir → Add → 返回受理回执。
func (r *Router) scopedBashBg(ctx context.Context, argsJSON string) (string, error) {
	if !r.asyncEnabled() {
		return "", fmt.Errorf("bash_bg: %s", asyncDisabledText)
	}
	var input bashBgInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("bash_bg: invalid args: %w", err)
	}
	if strings.TrimSpace(input.Command) == "" {
		return "", fmt.Errorf("bash_bg: command is required")
	}
	// 行标题只能来自派发时的这句话——没有别的诚实来源（它会活过这一轮）。
	if strings.TrimSpace(input.Description) == "" {
		return "", fmt.Errorf("bash_bg: description is required（一句话说明这条命令在做什么，它会成为工作打点表的行标题）")
	}
	workdir, err := r.resolveNodePath(ctx, input.Workdir, false)
	if err != nil {
		return "", err
	}
	payload, err := (&bashBgTool{router: r}).Add(ctx, JobSpec{
		SessionID: r.sessionKey(ctx),
		Command:   input.Command,
		Title:     clampProbeLine(input.Description),
		BatchID:   r.currentBatch(r.sessionKey(ctx)),
		Workdir:   workdir,
	})
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

// bashBgSchema 是 bash_bg 的入参 schema（description 必填：它就是行标题）。
func bashBgSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command":     map[string]interface{}{"type": "string"},
			"description": map[string]interface{}{"type": "string"},
			"timeout":     map[string]interface{}{"type": "integer"},
			"workdir":     map[string]interface{}{"type": "string"},
		},
		"required": []string{"command", "description"},
	}
}

// bashBgDescription 说明"派发即结束"：回执不含输出，取回与终止各走哪个 op。
// 不写清，模型会以为它只是"超时更长的那种 bash"——所以时长边界（超过串行预算的命令
// 属于这里）写在最前面，而不是让模型自己去串行描述的拒绝消息里反推。
func bashBgDescription() string {
	return "Dispatch a command to the background job store — this is the entry for anything " +
		"expected to run longer than the serial budget (" + serialBashBudgetLabel() + "): " +
		"serial bash / bash_read refuse a `timeout` above it. That call's result is only an " +
		"acceptance receipt (handle + log_path), never the command output — use " +
		"job_manage(op=fetch, handle) for results, job_manage(op=kill, handle) to terminate, " +
		"job_manage(op=observe, handle) to look at progress without consuming output, and " +
		"job_manage(op=done, handle) to retire it. description is required: one line on what " +
		"the command is doing, which becomes the work-table row title."
}

// ── read_batch：批量读作业（inline，Add 的工具面）──────────────────────

type readBatchInput struct {
	Paths []string `json:"paths"`
	// StartLine / EndLine 是整批共用的窗口（单文件精确窗口用 read_file）。
	StartLine int `json:"start_line,omitempty"`
	EndLine   int `json:"end_line,omitempty"`
}

// inlineReadTool 是批量读作业（Kind=inline）的 Add 实现：一次调用派发 N 个作业，
// 回执含 N 个 handle，**调用本身不等结果**（派发即返回）。
type inlineReadTool struct{ router *Router }

// Add 派发一批读作业。
//
// 为什么要作业化（打点 L-3 / 设计文档 D-1）：读/搜是 IO 密集，N 个文件的串行读
// 把墙钟叠起来，而它们之间没有任何共享状态。走作业派发可以先拿到并行收益，且
// **零框架改动**——代价是结果晚一轮（P2 的 S-2 才由 seele 原生 loop 并发接管）。
func (t *inlineReadTool) Add(ctx context.Context, spec JobSpec) ([]byte, error) {
	// 单条 Add 只负责起一个读作业（批量派发在 scopedReadBatch 里循环）；
	// 这样"一批 N 个 handle"与"每条作业一个句柄"是同一件事的两种视角。
	if t == nil || t.router == nil {
		return nil, fmt.Errorf("read_batch: 执行域未装配")
	}
	spec.Kind = JobKindInline
	run, started, err := t.router.async.beginJob(spec)
	if err != nil {
		return nil, err
	}
	if started {
		if err := t.router.startInlineJob(ctx, run, spec); err != nil {
			t.router.async.finish(run.handle, 1)
			return nil, err
		}
	}
	return renderJobAccepted(handleForRun(run, !started))
}

// Status / Fetch / Kill / Done 委派给 jobManager（与 bash_bg 同一份实现）。
func (t *inlineReadTool) Status(ctx context.Context, handle JobHandle) ([]byte, error) {
	return (&jobManager{router: t.router}).Status(ctx, handle)
}
func (t *inlineReadTool) Fetch(ctx context.Context, handle JobHandle) ([]byte, error) {
	return (&jobManager{router: t.router}).Fetch(ctx, handle)
}
func (t *inlineReadTool) Kill(ctx context.Context, handle JobHandle) ([]byte, error) {
	return (&jobManager{router: t.router}).Kill(ctx, handle)
}
func (t *inlineReadTool) Done(ctx context.Context, handle JobHandle) ([]byte, error) {
	return (&jobManager{router: t.router}).Done(ctx, handle)
}

var _ JobTool = (*inlineReadTool)(nil)

// scopedReadBatch 派发一批读作业，返回含 N 个 handle 的受理回执。
func (r *Router) scopedReadBatch(ctx context.Context, argsJSON string) (string, error) {
	if !r.asyncEnabled() {
		return "", fmt.Errorf("read_batch: %s", asyncDisabledText)
	}
	var input readBatchInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("read_batch: invalid args: %w", err)
	}
	paths := make([]string, 0, len(input.Paths))
	for _, path := range input.Paths {
		if trimmed := strings.TrimSpace(path); trimmed != "" {
			paths = append(paths, trimmed)
		}
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("read_batch: paths is required（一次调用里给出要读的文件；单个文件用 read_file）")
	}
	if len(paths) > asyncMaxRunning {
		return "", fmt.Errorf("read_batch: %d 个路径超过单批上限 %d", len(paths), asyncMaxRunning)
	}
	sessionID := r.sessionKey(ctx)
	batchID := r.currentBatch(sessionID)
	tool := &inlineReadTool{router: r}

	receipts := make([]map[string]string, 0, len(paths))
	for index, path := range paths {
		// 路径解析在派发侧完成：作业跑在后台，执行体不该再去解析会话根
		// （那一轮的 ctx 已经失效）。
		resolved, err := r.resolveNodePath(ctx, path, false)
		if err != nil {
			return "", err
		}
		payload, err := tool.Add(ctx, JobSpec{
			SessionID: sessionID,
			Command:   path,
			Title:     "读文件: " + path,
			BatchID:   batchID,
			Index:     index,
			Workdir:   resolved,
			StartLine: input.StartLine,
			EndLine:   input.EndLine,
		})
		if err != nil {
			return "", err
		}
		handle := decodeJobReceipt(payload)
		receipts = append(receipts, map[string]string{
			"handle": handle.Handle, "path": path, "state": asyncStateRunning,
		})
	}
	payload := map[string]interface{}{
		"status":  "accepted",
		"count":   len(receipts),
		"jobs":    receipts,
		"hint":    "这批读作业已派发（调用本身不等结果）。用 job_manage(op=fetch, handle) 逐个取回，或 job_manage(op=observe) 列出全部在册作业。",
		"op_hint": "job_manage",
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("read_batch: 渲染受理回执失败: %w", err)
	}
	return string(encoded), nil
}

// readBatchSchema 是 read_batch 的入参 schema。
func readBatchSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"paths":      map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			"start_line": map[string]interface{}{"type": "integer"},
			"end_line":   map[string]interface{}{"type": "integer"},
		},
		"required": []string{"paths"},
	}
}

// readBatchDescription 说明"派发即返回"与"结果走取回"，并把它与 read_file 的
// 适用边界写清（否则模型会拿它替代单文件读，白花一轮往返）。
func readBatchDescription() string {
	return "Read several files in one call as background jobs: the call returns one handle per " +
		"path immediately and never waits for the contents. Use it when you already know the " +
		"paths and want the reads to overlap; for a single file use read_file (its result comes " +
		"back in the same turn). Fetch each job with job_manage(op=fetch, handle)."
}

// decodeJobReceipt 从受理回执字节里取回句柄与落点。
//
// 为什么这里是"解自己刚渲染的载荷"而不是改 Add 的出参：契约的出参就是那份 JSON
// 回执（JobTool 的口径），批量路径要把 N 条回执汇总成一张表，读回自己写下的那份
// 字节是唯一不需要第二份句柄来源的做法——两份来源必然漂移。
func decodeJobReceipt(payload []byte) JobHandle {
	var accepted struct {
		Handle  string `json:"handle"`
		LogPath string `json:"log_path"`
		State   string `json:"state"`
	}
	_ = json.Unmarshal(payload, &accepted)
	return JobHandle{Handle: accepted.Handle, LogPath: accepted.LogPath}
}
