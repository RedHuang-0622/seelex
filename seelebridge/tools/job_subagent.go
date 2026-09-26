package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// job_subagent.go — 子代理作业（Kind=subagent）的契约面（打点 L-5）。
//
// 子代理与后台命令走**同一张登记表、同一套状态机**，差别只有执行体：
//
//	| 诉求                       | 落点                              | 依赖 OS 进程化吗 |
//	| main agent 主动看它在干什么 | job_manage(op=observe, handle)    | 不需要          |
//	| main agent 提前终止         | job_manage(op=kill, handle)       | 不需要          |
//	| 执行体自己收尾              | Router.CompleteJob（被动 done）    | 不需要          |
//	| 结清那一行                  | job_manage(op=done, handle)       | 不需要          |
//
// 为什么不需要进程化：观测/干预只依赖**句柄契约**，与执行体是 goroutine 还是 OS 进程
// 正交。进程化会同时引入生命周期、结果序列化与数十个 subagent 测试面的改动，把"最小
// 改动"变成"最大改动"（设计文档 §B.3）。
//
// 产出载体与其他作业一致：**作业输出文件**。运行体系把节点摘要/发现追加进正文
// （Router.NoteJob），fetch 因此与 bash_bg / read_batch 逐字同语义（消费式增量），
// 不需要为子代理单开一条取回路径。

// subagentTool 是子代理作业的 Add 实现（Kind=subagent）。
//
// 它与进程/扇出作业唯一的结构差别：执行体不在本包，而在 fork 的 plan 编排里。
// 所以 `Add` 只登记，不启动任何东西——启动与收尾由运行体系（Runtime → fork）负责，
// 契约面只保证"登记 + 句柄 + 回执"这一步的形状与另外两类作业一致。
type subagentTool struct{ router *Router }

// Add 登记一条子代理作业并回受理回执。
func (t *subagentTool) Add(ctx context.Context, spec JobSpec) ([]byte, error) {
	if t == nil || t.router == nil {
		return nil, fmt.Errorf("subagent_job: 执行域未装配")
	}
	if strings.TrimSpace(spec.Title) == "" {
		return nil, fmt.Errorf("subagent_job: title is required（它是工作打点表的行标题）")
	}
	spec.Kind = JobKindSubagent
	run, started, err := t.router.async.beginJob(spec)
	if err != nil {
		return nil, err
	}
	return renderJobAccepted(handleForRun(run, !started))
}

// Status / Fetch / Kill / Done 委派给 jobManager（三类作业同一份实现）。
func (t *subagentTool) Status(ctx context.Context, handle JobHandle) ([]byte, error) {
	return (&jobManager{router: t.router}).Status(ctx, handle)
}
func (t *subagentTool) Fetch(ctx context.Context, handle JobHandle) ([]byte, error) {
	return (&jobManager{router: t.router}).Fetch(ctx, handle)
}
func (t *subagentTool) Kill(ctx context.Context, handle JobHandle) ([]byte, error) {
	return (&jobManager{router: t.router}).Kill(ctx, handle)
}
func (t *subagentTool) Done(ctx context.Context, handle JobHandle) ([]byte, error) {
	return (&jobManager{router: t.router}).Done(ctx, handle)
}

var _ JobTool = (*subagentTool)(nil)

// AddSubagentJob 是**运行体系**的登记入口：登记一条子代理作业并挂上取消口，
// 返回句柄原文。
//
// 与契约面 Add 的关系：契约面保证形状（回执），这里多一件事——把 cancel 交给登记表，
// 于是 `job_manage(op=kill)` 与"会话销毁即杀"能取消整条子代理编排（非进程作业没有
// 进程树，取消口就是 ctx）。
func (r *Router) AddSubagentJob(spec JobSpec, cancel func()) (string, error) {
	if r == nil || r.async == nil {
		return "", fmt.Errorf("subagent_job: 执行域未装配")
	}
	if !r.asyncEnabled() {
		return "", fmt.Errorf("subagent_job: %s", asyncDisabledText)
	}
	spec.Kind = JobKindSubagent
	run, started, err := r.async.beginJob(spec)
	if err != nil {
		return "", err
	}
	if started && cancel != nil {
		r.async.setCancel(run.handle, cancel)
	}
	return run.handle, nil
}

// NoteJob 追加子代理作业的正文（节点摘要 / 中途发现）。运行体系在执行过程中调用它，
// 于是"已经产出的内容"在 kill 之后仍然可取回（打点 L-4 的判据）。
func (r *Router) NoteJob(handle, text string) error {
	if r == nil || r.async == nil {
		return fmt.Errorf("job: 执行域未装配")
	}
	return r.async.appendNote(handle, text)
}

// CompleteJob 是运行体系的**被动 done** 入口：执行体收尾时合成终态。
//
// 与 job_manage(op=done) 的分工（K-6 的两个入口）：
//   - 这里 = 执行体自己判定终态（子代理编排跑完 / 失败 / 被取消）；
//   - job_manage(op=done) = 模型侧确认销项（把已落定的终态行从投影里划掉）。
//     它既不迁移终态，也不允许对在途作业使用——终态只由执行体判定。
//
// 幂等：已经是终态的句柄返回 false，不产生第二次迁移（K-5 的 K-4 约束）。
func (r *Router) CompleteJob(handle, state string) bool {
	if r == nil || r.async == nil {
		return false
	}
	snapshot, ok := r.async.snapshot(handle)
	if !ok || snapshot.state != asyncStateRunning {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(state)) {
	case asyncStateDone:
		r.async.finish(handle, 0)
	case asyncStateFailed:
		r.async.finish(handle, 1)
	case asyncStateKilled:
		r.async.markKilled(handle)
		r.async.finish(handle, asyncKilledExit)
	default:
		return false
	}
	return true
}

// SubagentJobHandles 返回本会话在册的**子代理作业**句柄（运行体系对账用：会话销毁、
// 恢复时确认哪些子代理还没结清）。只读，不推进任何游标。
func (r *Router) SubagentJobHandles(sessionID string) []string {
	if r == nil || r.async == nil || !r.asyncEnabled() {
		return nil
	}
	handles := make([]string, 0)
	for _, info := range r.async.infos() {
		if info.Kind != asyncKindSubagent {
			continue
		}
		if sessionID != "" && info.SessionID != sessionID {
			continue
		}
		handles = append(handles, info.Handle)
	}
	return handles
}

// JobReceiptHandle 从受理回执字节里取句柄（运行体系跨层传递时用；与
// decodeJobReceipt 同口径，导出是因为调用方在别的包）。
func JobReceiptHandle(payload []byte) string {
	var accepted struct {
		Handle string `json:"handle"`
	}
	_ = json.Unmarshal(payload, &accepted)
	return accepted.Handle
}
