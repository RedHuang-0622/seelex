package assemble

// runtime_async.go 是**后台执行表投影面**的宿主转发面。
//
// 落点沿革（P21-10「域方法随类型走」）：字段搬运与其判据（每一列都要到、空表归一为 nil）
// 都搬进作业域 [`backend/tool/jobs`](../../tool/jobs/README.md) 的 `ProjectAsyncRuns`；
// 这里只留两格宿主读数——执行域状态在装配层的 `scopedTools`（`backend/tool/registry` 的
// 路由器）上，宿主只做 nil 守卫与转发。
//
// 投影是只读的、且永不落盘（不变量 I-21：写进注册表会留下永远 running 的假行）。

import (
	"github.com/RedHuang-0622/seelex/backend/tool/jobs"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// AsyncRunsSnapshot 返回后台执行表的探针读数，按派发顺序。
// 能力未开（limits.async_exec.enabled=false）时 tools 层直接返回空——没有执行，
// 也就没有行。
func (r *Runtime) AsyncRunsSnapshot() []dto.AsyncRunRecord {
	if r == nil || r.scopedTools == nil {
		return nil
	}
	return jobs.ProjectAsyncRuns(r.scopedTools.AsyncRuns())
}

// AsyncRunEvents 返回后台执行表的变化信号口（core 的第四个生命周期消费者用它）。
func (r *Runtime) AsyncRunEvents() <-chan struct{} {
	if r == nil || r.scopedTools == nil {
		return nil
	}
	return r.scopedTools.AsyncRunEvents()
}
