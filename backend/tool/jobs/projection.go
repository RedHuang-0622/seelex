package jobs

// projection.go 是**后台执行表到 Application 侧的只读投影**（P21-10「域方法随类型走」：
// 从 `backend/engine/assemble/runtime_async.go` 归位，plan §2.6 把 `seelebridge/runtime_async.go`
// 列为 `backend/tool/jobs/` 的目标）。
//
// 它是执行域状态的对外读侧（工作表格与请求尾部打点块的输入）。写侧（派发、取回、
// 终止）全在 `backend/tool/registry` 里，本文件只做字段搬运，不含任何判定。
//
// 投影是只读的、且永不落盘：后台句柄活在内存，而 core 的 task 注册表随会话 record
// 持久化。把它写进注册表，重启后就会留下一条永远 running 的假行（不变量 I-21）。

import (
	"github.com/RedHuang-0622/seelex/backend/tool/registry"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// ProjectAsyncRuns 把后台执行表的探针读数投影为 DTO，按派发顺序。
// 能力未开（`limits.async_exec.enabled=false`）时读面返回空——没有执行，也就没有行。
func ProjectAsyncRuns(infos []registry.AsyncRunInfo) []dto.AsyncRunRecord {
	if len(infos) == 0 {
		return nil
	}
	records := make([]dto.AsyncRunRecord, 0, len(infos))
	for _, info := range infos {
		records = append(records, recordFrom(info))
	}
	return records
}

// recordFrom 搬运探针读数到 DTO。单独成函数是为了让"漏搬一列"能被用例钉住
// ——投影面只做搬运，任何判定都不许长在这里。
func recordFrom(info registry.AsyncRunInfo) dto.AsyncRunRecord {
	return dto.AsyncRunRecord{
		SessionID:   info.SessionID,
		Handle:      info.Handle,
		Kind:        info.Kind,
		Description: info.Description,
		Command:     info.Command,
		State:       info.State,
		ExitCode:    info.Exit,
		LogBytes:    info.LogBytes,
		Summary:     info.Summary,
		Lines:       info.Lines,
		Notified:    info.Notified,
		Index:       info.Index,
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
