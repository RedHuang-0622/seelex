package assemble

// runtime_teamwork_jobs.go — teammate 作业表的**只读投影 + 信号口**（契约
// contract.TeamworkJobCompletion）。
//
// 它是"做完自动返回"那条链在 seelebridge 一侧的入口：application 侧的空闲会话触发
// （triggerTeamworkJobCompletions）读这里，与 tools 那张表走 AsyncRunsSnapshot /
// AsyncRunEvents 完全同形——只是事实源不同（jobs.Manager vs tools 登记表），
// 因此取回工具也不同（jobs_manage vs bg_manage）。

import (
	"github.com/RedHuang-0622/Seele/jobs"

	"github.com/RedHuang-0622/seelex/backend/multiagent/teamwork"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// 同身份别名：投影判据只有一份实现（团队域 `job_projection.go`，P21-25 归位）。
var (
	teamworkJobCompletionFrom = teamwork.JobCompletionFrom
	asyncStateFromJobs        = teamwork.AsyncStateFromJobs
)

// TeamworkJobCompletions 实现在册 teammate 作业的只读投影。
//
// 全量快照、**不推进游标、不销项**：判定"这条是不是终态、该不该唤醒"是消费方的事
// （与 AsyncRunsSnapshot 同一分工：投影只搬运，判定不在搬运里）。
func (r *Runtime) TeamworkJobCompletions() []dto.TeamworkJobCompletionRecord {
	manager := r.teamworkJobManager()
	if manager == nil {
		return nil
	}
	records := manager.Snapshot(jobs.Scope{})
	if len(records) == 0 {
		return nil
	}
	// 搬运（含状态折算）在域包：投影只搬，判定不在搬运里。
	return teamwork.ProjectJobCompletions(records)
}

// TeamworkJobEvents 返回 teammate 作业表的变化信号口（扇出后的订阅通道）。
//
// 未装配 teamwork 时返回 nil：消费方的 select 忽略 nil 通道，等于"没有这条链"。
func (r *Runtime) TeamworkJobEvents() <-chan struct{} {
	if r == nil {
		return nil
	}
	r.teamworkMu.Lock()
	defer r.teamworkMu.Unlock()
	return r.teamworkJobEvents
}

// teamworkJobManager 取当前装配的作业表（未装配 = nil）。
func (r *Runtime) teamworkJobManager() jobs.Manager {
	if r == nil {
		return nil
	}
	r.teamworkMu.Lock()
	defer r.teamworkMu.Unlock()
	return r.teamworkJobs
}
