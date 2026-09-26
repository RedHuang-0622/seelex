package tools

import (
	"context"
	"fmt"
	"strings"
)

// 作业契约（打点 K-1）：**凡"有进程添加与管理"的工具都实现这两个函数**。
//
// 判据：串行 bash 不实现——它跑完即返回，没有句柄、没有生命周期；只有"跑起来之后
// 还要被观察/取回/终止/销项的执行体"才需要实现。三类执行体共用同一张登记表与同一套
// 状态机（process / inline / subagent），差别只在执行体本身：
//
//	Add      添加一个执行体并**立刻**返回受理回执；调用方不等待结果，回执就是这次
//	         tool call 的全部返回（派发即结束 ⇒ loop 不等待、不占轮次）。
//	Manage   管理一个已添加的执行体：
//	         OpObserve = 只读看进度（**不推进游标**、不消费输出）
//	         OpFetch   = 取回增量（推进游标，消费式）
//	         OpKill    = 终止执行体（进程树 / 取消级联），**已产出内容不丢**
//	         OpDone    = 销项：终态迁移 + 回填（终态只由执行体判定，见下）
//
// `done` 的主动/被动只是**调用方不同**（设计文档 §A.1 的 M3 更正）：
//   - 后台进程退出 → 运行体系**被动**调（awaitAsync 收尾）；
//   - 子代理 / 扇出作业干完 → 监督它执行体自己收敛，main agent 或 subagent 也可以
//     **主动**调 OpDone 销项。
//
// 但**终态字面量只由执行体判定**（不变量：不许按墙钟猜状态）：OpDone 在作业还在跑
// 时只报错，不谎报"完成"；它销的是已经落定的终态行。

// JobKind 是作业类别（决定读取口与 kill 语义）。
type JobKind string

const (
	// JobKindProcess = 后台 shell 子进程：进程树可终止，输出走日志文件。
	JobKindProcess JobKind = asyncKindProcess
	// JobKindInline = 进程内的扇出作业（批量读）：没有进程，取消靠 ctx。
	JobKindInline JobKind = asyncKindInline
	// JobKindSubagent = 子代理：执行体是 plan 节点，取消靠 ctx，产出走日志文件。
	JobKindSubagent JobKind = asyncKindSubagent
)

// JobSpec 是 Add 的入参：一次派发的全部输入。
type JobSpec struct {
	Kind      JobKind
	SessionID string
	// Command 是这条作业"在做什么"的原文（命令行 / 目标摘述）→ 工作表格描述列。
	Command string
	// Title 是行标题（一句话）→ 工作打点表那一行的内容。派发时必填：后台作业会活过
	// 这一轮，行标题只能来自派发时的这句话，没有别的诚实来源。
	Title string
	// BatchID = 派发它的那次 chat 请求（工作表格的批次归属）。
	BatchID string
	// Workdir 是执行体的工作目录（进程作业用；其余类别忽略）。
	Workdir string
	// StartLine / EndLine 是 inline 读作业的行窗口（1 起算；0 = 不限）。
	// 只有"读一个已知文件"这一族用它，进程作业与子代理作业忽略。
	StartLine int
	EndLine   int
	// Index 是同批内 wire 下标（批量派发用）：排序键，不是完成序。
	Index int
	// Dedup 打开"同一会话同一条 Command 在途只留一个"的去重（后台命令）。
	Dedup bool
}

// JobHandle 是一次派发/管理请求的句柄面。
type JobHandle struct {
	Handle    string
	Kind      JobKind
	SessionID string
	Title     string
	// LogPath / Repeated 是**受理回执**的渲染输入（handle + log_path + 是否重复
	// 派发）：有了它们，`bash_bg` 的回执与旧 `bash(background=true)` 逐字段一致
	// （打点 L-1 的判据）。
	LogPath  string
	Repeated bool
	// WaitMS 是本次**管理请求**的等待预算（只对 OpFetch 有意义）：0 = 默认 5s、
	// 负数 = 立即返回当前增量、超上限按上限。它属于"这一次请求"，所以挂在请求结构
	// 上，而不是给 Manage 再开一个函数（契约固定为两个函数）。
	WaitMS int
}

// JobOp 是 Manage 支持的操作。
type JobOp string

const (
	JobOpObserve JobOp = "observe"
	JobOpFetch   JobOp = "fetch"
	JobOpKill    JobOp = "kill"
	JobOpDone    JobOp = "done"
)

// JobTool 是"进程型工具"的契约。实现方必须是"有执行体生命周期"的工具：串行 bash /
// read_file / grep_search 这类跑完即返回的工具**不实现**。
//
// 形状：**Add + 四个具名管理动作**（Status / Fetch / Kill / Done）。
//
//   - 出参一律 []byte（JSON 载荷），不写 string：载荷是"将来要变成结构化输出的
//     那份字节"，bytes 直接就是 JSON，往外塞 string 只会让每个调用点多做一次
//     编解码往返；工具面对框架的那层边界（framework ToolHandler 只吃 string）
//     再转 string，转换点因此只有一处、且是框架契约要求的。
//   - 查看用 **Status**（不是笼统的 Manage）：它对应设计文档里的 observe =
//     只读读数，不推进游标、不消费输出。"管理"是一个动作集合的名字，写在接口上
//     就看不见"这一次到底干了什么"，而它恰恰是最容易被当成 getter 误用的一个。
//   - **Done 在契约里**（不是内部钩子）：销项是契约的终态动作——后台进程由运行
//     体系被动调用、subagent / 扇出作业可以自己调、main agent 也可以主动调
//     （设计文档 §A.1 的 M3 更正）。漏了它，三类作业的收尾就只剩"运行体系内部
//     悄悄做掉"这一条路，模型侧永远无法确认自己派的作业已经结清。
type JobTool interface {
	// Add 添加一个作业并**立刻**返回受理回执；调用方不等待执行结果。
	Add(ctx context.Context, spec JobSpec) ([]byte, error)

	// Status 查看一个作业的只读读数（observe）：不推进游标、不消费输出。
	Status(ctx context.Context, handle JobHandle) ([]byte, error)

	// Fetch 取回增量（消费式）：取过的字节不再给第二次。
	Fetch(ctx context.Context, handle JobHandle) ([]byte, error)

	// Kill 终止执行体（进程树 / 取消级联）：已产出内容保留，仍可 fetch。
	Kill(ctx context.Context, handle JobHandle) ([]byte, error)

	// Done 销项一个**已终态**的作业：终态迁移 + 回填，幂等。
	Done(ctx context.Context, handle JobHandle) ([]byte, error)
}

// jobManager 是四个**管理动作**的唯一实现：它们对三类作业语义一致（差异都在执行体
// 那一侧，不在这里），因此不需要每个工具各写一份。
//
// 工具侧的实现（bashBgTool / inlineReadTool / subagentTool）把 Status / Fetch /
// Kill / Done 委派到它，契约的实现面只有一处——这既省代码，也保证"查看/取回/终止/
// 销项"的语义不会因为工具不同而漂移。
type jobManager struct {
	router *Router
}

// Status 查看一个作业：只读读数，**不推进游标**、不消费输出（探针纪律，见
// async_probe.go）。它回答的是"此刻它在不在动、动到哪一行"。
//
// 出参是 []byte（JSON 载荷）：契约里不把载荷定死成 string——将来要给模型/前端换
// 结构化字段时，bytes 直接就是那份 JSON；反过来"先定成 string、外面再包一层"，
// 每次都要多做一次往返转换，还得先解开再拼回去。
//
// 带 handle = 一条作业的读数；不带 handle = 本会话在册作业的清单（main agent
// "主动查看子代理在干什么"走的就是这一条）。
func (m *jobManager) Status(ctx context.Context, handle JobHandle) ([]byte, error) {
	sessionID := m.sessionID(ctx)
	if m.router == nil || m.router.async == nil {
		return nil, fmt.Errorf("job_manage: %s", asyncDisabledText)
	}
	if handle.Handle == "" {
		lines := m.router.async.observeSession(sessionID)
		return renderObserved(lines)
	}
	run, ok := m.router.async.snapshot(handle.Handle)
	if !ok {
		if state, retired := m.router.async.retiredState(handle.Handle); retired {
			// 已销项：不是错误，但必须说清"它已经不在册"。
			return renderObserved([]string{fmt.Sprintf(
				"- %s %s 已销项（终态已回填过一次；输出已在那次 fetch 的结果里）", handle.Handle, state)})
		}
		return nil, fmt.Errorf("job_manage: 未知句柄 %q（可能已被销项或驱逐，或进程重启后登记表已清空）", handle.Handle)
	}
	if run.sessionID != sessionID {
		return nil, fmt.Errorf("job_manage: 句柄 %q 不属于本会话", handle.Handle)
	}
	return renderObserved([]string{m.router.async.observeLine(run)})
}

// Fetch 取回增量（**消费式**：取过的增量不会再给第二次）。
//
// 终态作业取回之后**销项**（设计文档 §A.3 的行生命周期）：history 里的这次结果成为
// 唯一事实，投影里的那一行随之消失。在途作业取回不销项——它还在跑。
func (m *jobManager) Fetch(ctx context.Context, handle JobHandle) ([]byte, error) {
	sessionID := m.sessionID(ctx)
	if handle.Handle == "" {
		return nil, fmt.Errorf("job_manage: op=fetch 需要 handle")
	}
	run, ok := m.router.async.snapshot(handle.Handle)
	if !ok {
		if state, retired := m.router.async.retiredState(handle.Handle); retired {
			return nil, fmt.Errorf("job_manage: 句柄 %q 已销项（终态 %s）：输出已在销项前那次取回的结果里", handle.Handle, state)
		}
		return nil, fmt.Errorf("job_manage: 未知句柄 %q（可能已被驱逐，或进程重启后登记表已清空）", handle.Handle)
	}
	if run.sessionID != sessionID {
		return nil, fmt.Errorf("job_manage: 句柄 %q 不属于本会话", handle.Handle)
	}
	if run.state == asyncStateRunning {
		awaitAsyncDeadline(ctx, run.done, handle.WaitMS)
	}
	next, delta, truncated, ok := m.router.async.advanceTail(handle.Handle, asyncPollTailBudget)
	if !ok {
		return nil, fmt.Errorf("job_manage: 句柄 %q 已不在登记表里", handle.Handle)
	}
	m.router.async.markCursor(handle.Handle, next, truncated)
	fresh, ok := m.router.async.snapshot(handle.Handle)
	if !ok {
		return nil, fmt.Errorf("job_manage: 句柄 %q 已不在登记表里", handle.Handle)
	}
	payload, err := renderPolled(fresh, delta, truncated)
	if err != nil {
		return nil, err
	}
	if fresh.state != asyncStateRunning {
		// 终态 + 已交付 ⇒ 销项：行从投影里消失（回填的生命周期终点）。
		m.router.async.retire(handle.Handle)
	}
	return payload, nil
}

// Kill 终止一棵执行体：进程作业终止进程树，非进程作业取消 ctx。两者都**保留已产出
// 内容**（打点 L-4 的判据：kill 不是丢结果）。只终止、不取回——结果一律走 fetch。
func (m *jobManager) Kill(ctx context.Context, handle JobHandle) ([]byte, error) {
	sessionID := m.sessionID(ctx)
	if handle.Handle == "" {
		return nil, fmt.Errorf("job_manage: op=kill 需要 handle")
	}
	tree, cancel, alreadyDone, err := m.router.async.killHandle(handle.Handle, sessionID)
	if err != nil {
		return nil, err
	}
	if alreadyDone {
		run, _ := m.router.async.snapshot(handle.Handle)
		return renderAlreadyFinished(run)
	}
	// 终止在锁外做：finish 也要那把锁，等 TerminateJobObject / 取消级联返回不能把
	// 整张表按住。失败就报错，绝不谎报已杀。
	if tree != nil {
		if err := tree.Terminate(); err != nil {
			return nil, fmt.Errorf("job_manage: 句柄 %q 终止失败: %w", handle.Handle, err)
		}
	} else if cancel != nil {
		cancel()
	} else {
		return nil, fmt.Errorf("job_manage: 句柄 %q 没有可终止的执行体", handle.Handle)
	}
	// 只有确认发出终止才落 killed 意图——否则状态会跑在进程前面。
	m.router.async.markKilled(handle.Handle)
	run, _ := m.router.async.snapshot(handle.Handle)
	return renderKilled(run)
}

// Done 销项：终态迁移 + 回填（K-6 的"主动入口"）。
//
// 幂等（K-5 的 K-4）：重复 done 无副作用——第二次只回报"已销项"，不再迁移、不再回填。
// 在途作业**不能**这样销项：终态只由执行体判定，模型说"它完了"不算（那正是
// "按墙钟猜状态"的另一种写法）。要停就 kill。
func (m *jobManager) Done(ctx context.Context, handle JobHandle) ([]byte, error) {
	sessionID := m.sessionID(ctx)
	if handle.Handle == "" {
		return nil, fmt.Errorf("job_manage: op=done 需要 handle")
	}
	run, ok := m.router.async.snapshot(handle.Handle)
	if !ok {
		if state, retired := m.router.async.retiredState(handle.Handle); retired {
			return renderObserved([]string{fmt.Sprintf("- %s %s 已销项（重复 done 无副作用）", handle.Handle, state)})
		}
		return nil, fmt.Errorf("job_manage: 未知句柄 %q（可能已被驱逐，或进程重启后登记表已清空）", handle.Handle)
	}
	if run.sessionID != sessionID {
		return nil, fmt.Errorf("job_manage: 句柄 %q 不属于本会话", handle.Handle)
	}
	if run.state == asyncStateRunning {
		return nil, fmt.Errorf("job_manage: 句柄 %q 还在跑（state=running）：终态只由执行体判定，"+
			"done 只销已落定的终态行；要停它用 op=kill", handle.Handle)
	}
	if !m.router.async.retire(handle.Handle) {
		return nil, fmt.Errorf("job_manage: 句柄 %q 无法销项（已被销项或驱逐）", handle.Handle)
	}
	return renderRetired(run)
}

// sessionID 解析本次调用的会话归属（跨会话取回/终止一律拒绝）。
func (m *jobManager) sessionID(ctx context.Context) string {
	if m.router == nil {
		return ""
	}
	return m.router.sessionKey(ctx)
}

// ── 三类作业的 Add 实现 ────────────────────────────────────────────────

// bashBgTool 是后台 shell 作业（Kind=process）的 Add 实现。
type bashBgTool struct{ router *Router }

// Add 派发一条后台命令：登记（去重）→ 起执行体 → 回受理回执。
func (t *bashBgTool) Add(ctx context.Context, spec JobSpec) ([]byte, error) {
	if t == nil || t.router == nil {
		return nil, fmt.Errorf("bash_bg: 执行域未装配")
	}
	spec.Kind = JobKindProcess
	spec.Dedup = true
	run, started, err := t.router.async.beginJob(spec)
	if err != nil {
		return nil, err
	}
	if started {
		if err := t.router.startAsync(ctx, run, spec.Command, spec.Workdir); err != nil {
			// 起不来就当场失败：受理回执不得谎报 running（模型会一直轮询一个
			// 永远不会结束的句柄）。合成终态后重发同一命令不会被去重挡住
			// （去重键只对 state=running 生效）。
			t.router.async.finish(run.handle, 1)
			return nil, err
		}
	}
	return renderJobAccepted(handleForRun(run, !started))
}

// Status / Fetch / Kill / Done 委派给 jobManager：四个动作对三类作业语义一致，
// 只留一份实现（见 job_contract.go 的 jobManager 头注）。
func (t *bashBgTool) Status(ctx context.Context, handle JobHandle) ([]byte, error) {
	return (&jobManager{router: t.router}).Status(ctx, handle)
}
func (t *bashBgTool) Fetch(ctx context.Context, handle JobHandle) ([]byte, error) {
	return (&jobManager{router: t.router}).Fetch(ctx, handle)
}
func (t *bashBgTool) Kill(ctx context.Context, handle JobHandle) ([]byte, error) {
	return (&jobManager{router: t.router}).Kill(ctx, handle)
}
func (t *bashBgTool) Done(ctx context.Context, handle JobHandle) ([]byte, error) {
	return (&jobManager{router: t.router}).Done(ctx, handle)
}

var _ JobTool = (*bashBgTool)(nil)

// handleForRun 把登记表里的一条记录折成句柄面（回执渲染的输入）。
func handleForRun(run asyncRun, repeated bool) JobHandle {
	return JobHandle{
		Handle: run.handle, Kind: JobKind(run.kind), SessionID: run.sessionID,
		Title: run.description, LogPath: run.logPath, Repeated: repeated,
	}
}

// renderJobAccepted 渲染受理回执：这是 Add 的全部返回（派发即结束）。
func renderJobAccepted(handle JobHandle) ([]byte, error) {
	hint := "作业已派发。要结果就调 job_manage(op=fetch, handle) 并把 wait_ms 给到预期剩余时长" +
		"（上限 60000）；只想确认进展看工作打点表，不必为此花一次往返。不要重复派发同一条命令。"
	if handle.Repeated {
		hint = "同一条命令已在跑（未重复派发）。用 job_manage(op=fetch, handle) 取回它的进展。"
	}
	return encodeAsync(asyncPayload{
		Status: "accepted", Handle: handle.Handle, State: asyncStateRunning,
		ExitCode: -1, LogPath: handle.LogPath, Repeated: handle.Repeated, Hint: hint,
	})
}

// jobSummaryLine 是**投影**用的一行（观察读数与工作打点表共用同一口径）：
// 句柄、类别、状态、字节数、标题（或命令短截断）。
func jobSummaryLine(run asyncRun, logBytes int64) string {
	fields := []string{asyncWorkRowPrefixForLine + run.handle, run.kind, run.state, formatBytesCompact(logBytes)}
	if run.state != asyncStateRunning {
		// 终态带**有界摘要**（K-5 的回填内容）：退出码 + 行数 + 字节数 + 末行。
		if run.summary != "" {
			fields = append(fields, run.summary)
		}
	} else if detail := strings.TrimSpace(run.description); detail != "" {
		fields = append(fields, truncateRunes(detail, 60))
	} else if command := strings.TrimSpace(run.command); command != "" {
		fields = append(fields, truncateRunes(command, 60))
	}
	return strings.Join(fields, " ")
}

// asyncWorkRowPrefixForLine 是投影行 ID 的前缀（与 application 侧的
// asyncWorkRowPrefix 同字面量：行身份跨层必须一致）。
const asyncWorkRowPrefixForLine = "async:"
