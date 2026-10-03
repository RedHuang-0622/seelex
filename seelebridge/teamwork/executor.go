package teamwork

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/RedHuang-0622/Seele/jobs"
)

// ItemSettler 是 teammate 一轮跑完之后的**尾插接线点**：执行体把"这一轮结束了（成功
// 或失败）"交给它，由它去做"合并 worktree → 插入消息队列"（见 items.go 的
// SettleWorkItem）。抽成端口而不是让执行体自己做：合并归编排面（它知道这件事的绑定），
// 执行体只管跑。
//
// 缺失（nil）= 不尾插：结论只留审计行。降级是显式的，不会静默变成"假装插过了"。
type ItemSettler interface {
	SettleWorkItem(ctx context.Context, request WorkerRequest, runErr error) error
}

// WorkerExecutor 返回注册进 jobs.Manager 的 teammate 执行体（Kind = "worker"）。
//
// 执行体只做四件事：解载荷、把作业交给 Runner、**尾插**（settler）、把结果收敛成一条
// 终态。它**不**自己判定顺序、不自己回收工作区——那些是 Coordinator 的事（leader
// 掌控顺序，执行体只管跑）。
func WorkerExecutor(runner WorkerRunner, settler ItemSettler, maxTurns int) jobs.Executor {
	return &workerExecutor{runner: runner, settler: settler, maxTurns: maxTurns}
}

type workerExecutor struct {
	runner   WorkerRunner
	settler  ItemSettler
	maxTurns int
}

func (e *workerExecutor) Kind() jobs.Kind { return KindWorker }

func (e *workerExecutor) Start(ctx context.Context, spec jobs.Spec, sink jobs.Sink) error {
	if e.runner == nil {
		return errors.New("teamwork: worker 执行体未装配（缺 WorkerRunner）")
	}
	var request WorkerRequest
	if err := json.Unmarshal(spec.Payload, &request); err != nil {
		sink.Note("\n[teamwork] worker 载荷解码失败：" + err.Error() + "\n")
		sink.Exit(1)
		sink.Complete(jobs.StateFailed, "worker 载荷解码失败")
		return nil
	}
	if request.MaxTurns == 0 {
		request.MaxTurns = e.maxTurns
	}
	runErr := e.runner.RunWorker(ctx, request, sink)
	// 尾插：跑完（成功或失败）都插——失败时"bug 也要直接打印到 teammate 的消息中"。
	// 尾插自身的错误**不改判终态**：结论已经产生，收尾动作失败不该把一次成功的
	// 工作说成失败的（它的痕迹在审计行与消息正文里）。
	if e.settler != nil {
		if settleErr := e.settler.SettleWorkItem(ctx, request, runErr); settleErr != nil {
			sink.Note("\n[teamwork] 尾插失败：" + settleErr.Error() + "\n")
		}
	}
	if runErr != nil {
		sink.Note("\n[teamwork] worker 回合失败：" + runErr.Error() + "\n")
		sink.Exit(1)
		sink.Complete(jobs.StateFailed, runErr.Error())
		return nil
	}
	// 幂等：Runner 若已自行收敛终态，这一次是空操作（jobs 的不变式 I-1）。
	sink.Complete(jobs.StateDone, "")
	return nil
}

var _ jobs.Executor = (*workerExecutor)(nil)
var _ ItemSettler = (*Coordinator)(nil)
