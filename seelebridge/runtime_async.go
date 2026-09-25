package seelebridge

import (
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	seeltools "github.com/RedHuang-0622/seelex/seelebridge/tools"
)

// 后台命令（bash background=true）到 Application 侧的**只读投影面**。
//
// 为什么单独立一个文件而不是塞进 runtime_tools.go：这一面不属于"工具注册与调度"，
// 它是执行域状态的对外读侧（工作表格与请求尾部打点块的输入）。写侧（派发、取回、
// 终止）全在 seelebridge/tools 里，这里只做字段搬运，不含任何判定。
//
// 投影是只读的、且永不落盘：后台句柄活在内存，而 core 的 task 注册表随会话 record
// 持久化。把它写进注册表，重启后就会留下一条永远 running 的假行（不变量 I-21）。

// AsyncRunsSnapshot 返回后台执行表的探针读数，按派发顺序。
// 能力未开（limits.async_exec.enabled=false）时 tools 层直接返回空——没有执行，
// 也就没有行。
func (r *Runtime) AsyncRunsSnapshot() []dto.AsyncRunRecord {
	if r == nil || r.scopedTools == nil {
		return nil
	}
	infos := r.scopedTools.AsyncRuns()
	if len(infos) == 0 {
		return nil
	}
	records := make([]dto.AsyncRunRecord, 0, len(infos))
	for _, info := range infos {
		records = append(records, asyncRunRecordFrom(info))
	}
	return records
}

// asyncRunRecordFrom 搬运探针读数到 DTO。单独成函数是为了让"漏搬一列"能被用例钉住
// ——投影面只做搬运，任何判定都不许长在这里。
func asyncRunRecordFrom(info seeltools.AsyncRunInfo) dto.AsyncRunRecord {
	return dto.AsyncRunRecord{
		SessionID:   info.SessionID,
		Handle:      info.Handle,
		Description: info.Description,
		Command:     info.Command,
		State:       info.State,
		ExitCode:    info.Exit,
		LogBytes:    info.LogBytes,
		Tail:        info.Tail,
		LastByteAt:  info.LastByteAt,
		LogPath:     info.LogPath,
		Truncated:   info.Truncated,
		Degraded:    info.Degraded,
		BatchID:     info.BatchID,
		StartedAt:   info.StartedAt,
		EndedAt:     info.EndedAt,
	}
}

// AsyncRunEvents 返回后台执行表的变化信号口（core 的第四个生命周期消费者用它）。
func (r *Runtime) AsyncRunEvents() <-chan struct{} {
	if r == nil || r.scopedTools == nil {
		return nil
	}
	return r.scopedTools.AsyncRunEvents()
}
