package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	seelessh "github.com/RedHuang-0622/seelex/backend/tool/ssh"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// 作业契约的**工具面**（打点 L-1/L-2/L-3）：
//
//	bash_bg      —— Add：派发一条后台受管命令（Kind=process）
//	read_batch   —— Add：批量派发进程内读作业（Kind=inline）
//	bg_manage    —— Manage：observe / fetch / kill / done（三类作业共用）
//
// 为什么取回与终止并成一个名字（M5 + L-4）：旧的取回工具把"取回"耦死在"后台命令"
// 上，而 inline / subagent 作业同样要取回；旧终止工具又把"终止"单独开一个名字，
// 于是管理面被拆成两个名字、且各自只认一种作业。契约里它们是**同一组管理动作**，
// 工具面就该长成同一个名字 + 一个 op 入参——这也正是 L-2/L-4 的判据（两个旧名字
// 在代码与配置里零残留）。
//
// 旧名 `job_manage` 为什么改成 `bg_manage`（2026-10-15 审计 D1）：本进程里**并存两张
// 作业表**——这张表（后台工具 / 子代理作业，句柄 `bt-` / `bs-`）与团队的 Seele
// `jobs.Manager`（teammate 作业，句柄 `bteam-`，管理工具 `jobs_manage`）。旧名字与新名字
// 只差一个字母，模型照旧会把两张表的句柄交叉投递；`bg_` 与句柄命名空间同源，与
// `jobs_manage` 不共享骨架。句柄命名空间与文法见 `dto.JobNamespace`。

// ── bg_manage：管理（Manage 的工具面）─────────────────────────────────

// bgManageToolName 是这张管理工具的**注册名**：注册面、权限组与门禁用例都从这一处取，
// 别在第二处再写一份字面量。
const bgManageToolName = "bg_manage"

type jobManageInput struct {
	Op JobOp `json:"op"`
	// Handle 是单条作业的句柄；Handles 是**多条**（一次等一批 / 一批读数）。
	// 两者都给时合并（去重后按给定顺序），这样"只加一个 handles"不会让 handle 失效。
	Handle  string   `json:"handle,omitempty"`
	Handles []string `json:"handles,omitempty"`
	WaitMS  int      `json:"wait_ms,omitempty"`
}

// scopedJobManage 是 Manage 的唯一工具入口。
//
// 授权：句柄只对本会话有效（跨会话管理一律拒绝）；管理本身不重复弹审批——它管的
// 是**已经获批的那次派发**（与旧取回工具同源的理由）。真正的门在 handler：
// 句柄必须属于本会话。
//
// 两条执行面（本工作项）：本地 = 本机作业表（bt- / bs-），远端 = 远端作业目录句柄
// （`ssh:` 族，见 backend/tool/ssh/job.go）。两者的句柄**语法上互不相认**，所以把本机
// 句柄交给远端面（或反向）只会得到一句"该用哪张表"的错误，不会静默命中一条无关作业。
func (r *Router) scopedJobManage(ctx context.Context, argsJSON string) (string, error) {
	if !r.asyncEnabled() {
		return "", fmt.Errorf("bg_manage: %s", asyncDisabledText)
	}
	var input jobManageInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("bg_manage: invalid args: %w", err)
	}
	op := JobOp(strings.ToLower(strings.TrimSpace(string(input.Op))))
	if op == "" {
		return "", fmt.Errorf("bg_manage: op is required (observe | fetch | kill | done)")
	}
	switch op {
	case JobOpObserve, JobOpFetch, JobOpKill, JobOpDone:
	default:
		return "", fmt.Errorf("bg_manage: 未知 op %q（可用 observe | fetch | kill | done）", input.Op)
	}
	handles := requestedHandles(input)
	if op != JobOpObserve && len(handles) == 0 {
		return "", fmt.Errorf("bg_manage: op=%s 需要 handle（或 handles）", op)
	}
	if op == JobOpKill || op == JobOpDone {
		// 终止/销项一次只做一条：kill 会终止整批共用编排的子代理（一条 kill 影响 N 条
		// 作业），销项是"我确认收到这一条"的动作——两者都不该被一个数组悄悄放大。
		if len(handles) > 1 {
			return "", fmt.Errorf("bg_manage: op=%s 一次只能给一条句柄（收到 %d 条）；多条请逐条确认", op, len(handles))
		}
	}
	// 远端面：句柄以 `ssh:` 开头就是远端作业（判据在 ssh 包，一处）。
	if transport, ok := RemoteTransport(ctx); ok && hasRemoteJobHandle(handles) {
		if len(handles) > 1 {
			return "", fmt.Errorf("bg_manage: 远端面一次只能管一条句柄（收到 %d 条）；远端作业各自独立，请逐条管理", len(handles))
		}
		return jobManageRemote(ctx, transport, op, handles[0], input.WaitMS)
	}
	// 句柄必须属于**本表**（bt- / bs-）：见 requireOwnedJobHandles。
	if err := requireOwnedJobHandles(handles); err != nil {
		return "", err
	}
	waitMS := input.WaitMS
	manager := &jobManager{router: r}
	var (
		payload []byte
		err     error
	)
	switch {
	case op == JobOpObserve && len(handles) > 1:
		payload, err = manager.StatusMany(ctx, handleRequests(handles, waitMS))
	case op == JobOpFetch && len(handles) > 1:
		payload, err = manager.FetchMany(ctx, handleRequests(handles, waitMS))
	default:
		handle := JobHandle{WaitMS: waitMS}
		if len(handles) > 0 {
			handle.Handle = handles[0]
		}
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
	}
	if err != nil {
		return "", err
	}
	// 工具边界上转 string：framework 的 ToolHandler / tool_result 都是 string，
	// 契约内部保持 bytes（见 JobTool 的出参口径）。
	return markedJobPayload(ctx, "bg_manage", payload), nil
}

// hasRemoteJobHandle 报告句柄表里有没有远端作业句柄（`ssh:` 族）。
func hasRemoteJobHandle(handles []string) bool {
	for _, handle := range handles {
		if seelessh.IsRemoteJobHandle(handle) {
			return true
		}
	}
	return false
}

// jobManageRemote 是 bg_manage 的远端面：四个 op 转给远端作业面，回执自报执行面。
func jobManageRemote(ctx context.Context, transport Transport, op JobOp, handle string, waitMS int) (string, error) {
	out, err := transport.Manage(ctx, TransportManageRequest{Op: string(op), Handle: handle, WaitMS: waitMS})
	if err != nil {
		return "", err
	}
	fields := withExecutionFace(ctx, map[string]any{
		"status":        "ok",
		"handle":        handle,
		"op":            string(op),
		"state":         out.State,
		"exit_code":     out.ExitCode,
		"still_running": out.StillRunning,
		"truncated":     out.Truncated,
	})
	if out.Stdout != "" || op == JobOpFetch {
		fields["stdout"] = out.Stdout
		fields["stderr"] = out.Stderr
	}
	if out.LogPath != "" {
		fields["log_path"] = out.LogPath
	}
	if out.Note != "" {
		fields["note"] = out.Note
	}
	return receiptJSON("bg_manage", fields)
}

// markedJobPayload 给**本地**作业回执补上执行面两格（保持原键不动，加法式改动）。
//
// 载荷坏掉时原样返回：管理面的既有形状优先于这两格（多两格是信息，少一份载荷是故障）。
func markedJobPayload(ctx context.Context, tool string, payload []byte) string {
	var fields map[string]any
	if err := json.Unmarshal(payload, &fields); err != nil || fields == nil {
		return string(payload)
	}
	merged, err := receiptJSON(tool, withExecutionFace(ctx, fields))
	if err != nil {
		return string(payload)
	}
	return merged
}

// requireOwnedJobHandles 校验每个句柄都属于**本表**拥有的命名空间（后台工具作业 / 子代理作业）。
//
// 为什么这条判断必须在这里、且必须显式报错：两张作业表并存（本表 vs 团队的 `jobs.Manager`），
// 句柄曾经同形（两边都铸 `a5`），于是把 A 表的句柄交给 B 表的管理工具会**静默命中一条无关
// 作业**（2026-10-15 审计 D1 的实测：leader 想停 teammate 的 `a5`，这里 op=done 销掉的是一条
// 无关后台作业，还回了"成功"）。命名空间让这种调用在语法上不再可能命中；剩下的责任是把
// "不可能命中"变成**一句能照做的错误**：告诉模型这个句柄属于哪张表、该用哪个工具。
//
// 判据只此一处，且**正向白名单**（只认本表的两个 token）：将来再加一张作业表，这里默认拒绝，
// 不会因为"不认得的就放行"而漏掉第三种形态。
func requireOwnedJobHandles(handles []string) error {
	for _, handle := range handles {
		namespace, known := dto.JobNamespaceOf(handle)
		if known && (namespace == dto.JobNamespaceToolJob || namespace == dto.JobNamespaceSubagent) {
			continue
		}
		return fmt.Errorf("bg_manage: 句柄 %q 不是后台作业面的句柄（本表是 %s- / %s-）："+
			"团队作业（teammate）的句柄形如 %s-7，它的管理工具是 jobs_manage",
			handle, dto.JobNamespaceToolJob, dto.JobNamespaceSubagent, dto.JobNamespaceTeamwork)
	}
	return nil
}

// requestedHandles 把 handle / handles 两个入参合并成一份去重后的句柄表
// （保持给定顺序：handle 在前）。
func requestedHandles(input jobManageInput) []string {
	handles := make([]string, 0, len(input.Handles)+1)
	seen := make(map[string]struct{}, len(input.Handles)+1)
	appendHandle := func(raw string) {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return
		}
		if _, ok := seen[trimmed]; ok {
			return
		}
		seen[trimmed] = struct{}{}
		handles = append(handles, trimmed)
	}
	appendHandle(input.Handle)
	for _, handle := range input.Handles {
		appendHandle(handle)
	}
	return handles
}

// handleRequests 把句柄名折成请求结构（同一个 wait_ms 摊给整批：一次调用只有一份预算）。
func handleRequests(handles []string, waitMS int) []JobHandle {
	requests := make([]JobHandle, 0, len(handles))
	for _, handle := range handles {
		requests = append(requests, JobHandle{Handle: handle, WaitMS: waitMS})
	}
	return requests
}

// jobManageSchema 是 bg_manage 的入参 schema。
func jobManageSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"op":     map[string]interface{}{"type": "string", "description": "observe | fetch | kill | done"},
			"handle": map[string]interface{}{"type": "string"},
			"handles": map[string]interface{}{
				"type": "array", "items": map[string]interface{}{"type": "string"},
				"description": "多条句柄：op=fetch 一次等一批并各自取回增量、op=observe 一次看一批。整批共用一份 wait_ms。",
			},
			"wait_ms": map[string]interface{}{"type": "integer"},
		},
		"required": []string{"op"},
	}
}

// jobManageDescription 说明四个 op 的语义，并明确四件容易踩的事：
// observe 不吃输出、fetch 是消费式、handles 可以一次管多条、确认进展不必花一次往返。
func jobManageDescription() string {
	return "Manage dispatched jobs by handle (bash_bg / read_batch / fork_subagents). " +
		"op=observe: read-only progress look (never advances the fetch cursor, so it never " +
		"consumes output); with no handle it lists every job in this session. " +
		"op=fetch: return only the bytes produced since the previous fetch for that handle " +
		"(consumer-style, never a replay of the whole log) — wait_ms: negative returns " +
		"immediately, 0 or omitted waits up to 5s, values above 60000 are capped; set it near " +
		"the expected remaining time. A terminal job is retired once fetched (its row leaves " +
		"the work table; the fetched result stays in the transcript). " +
		"Give `handles` (an array) instead of `handle` to wait for and fetch a WHOLE batch in one " +
		"call: the wait budget is shared across the batch (not multiplied per handle), and each " +
		"job's own increment comes back under `jobs`. " +
		"op=kill: terminate the job (whole process tree / cancellation cascade); output already " +
		"produced is kept and still fetchable. One handle only. " +
		"op=done: retire a terminal job (terminal state is only ever decided by the execution " +
		"body, so done on a running job is refused — use kill). One handle only. Repeat calls are " +
		"idempotent. Handles are scoped to the calling session. Do not call observe just to check " +
		"whether a job is still running: the work-table trace block already lists every live job."
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
		"serial bash refuses a `timeout` above it. That call's result is only an " +
		"acceptance receipt (handle + log_path), never the command output — use " +
		"bg_manage(op=fetch, handle) for results, bg_manage(op=kill, handle) to terminate, " +
		"bg_manage(op=observe, handle) to look at progress without consuming output, and " +
		"bg_manage(op=done, handle) to retire it. description is required: one line on what " +
		"the command is doing, which becomes the work-table row title. " +
		// 与 bash 同一个事实源：后台命令与前台命令跑在同一个壳里（审计 D4）。
		ResolveShellFact().DialectHint()
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
			"handle": handle.Handle, "path": path, "state": asyncStateRunning.String(),
		})
	}
	payload := map[string]interface{}{
		"status":  "accepted",
		"count":   len(receipts),
		"jobs":    receipts,
		"hint":    "这批读作业已派发（调用本身不等结果）。用 bg_manage(op=fetch, handle) 逐个取回，或 bg_manage(op=observe) 列出全部在册作业。",
		"op_hint": "bg_manage",
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
		"back in the same turn). Fetch each job with bg_manage(op=fetch, handle)."
}

// decodeJobReceipt 从受理回执字节里取回句柄与落点。
//
// 为什么这里是"解自己刚渲染的载荷"而不是改 Add 的出参：契约的出参就是那份 JSON
// 回执（JobTool 的口径），批量路径要把 N 条回执汇总成一张表，读回自己写下的那份
// 字节是唯一不需要第二份句柄来源的做法——两份来源必然漂移。
func decodeJobReceipt(payload []byte) JobHandle {
	var accepted struct {
		Handle  string         `json:"handle"`
		LogPath string         `json:"log_path"`
		State   dto.AsyncState `json:"state"`
	}
	_ = json.Unmarshal(payload, &accepted)
	return JobHandle{Handle: accepted.Handle, LogPath: accepted.LogPath}
}
