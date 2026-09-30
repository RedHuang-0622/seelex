package teamwork

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/RedHuang-0622/Seele/jobs"
)

// WorkerExecutor 返回注册进 jobs.Manager 的 teammate 执行体（Kind = "worker"）。
//
// 执行体只做三件事：解载荷、把作业交给 Runner、把结果收敛成一条终态。它**不**
// 自己判定顺序、不自己回收工作区——那些是 Coordinator 的事（leader 掌控顺序，
// 执行体只管跑）。
func WorkerExecutor(runner WorkerRunner, maxTurns int) jobs.Executor {
	return &workerExecutor{runner: runner, maxTurns: maxTurns}
}

type workerExecutor struct {
	runner   WorkerRunner
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
	sink.Progress(fmt.Sprintf("worker 起跑 role=%s stage=%s worktree=%s", request.Role, request.Stage, request.Worktree))
	if err := e.runner.RunWorker(ctx, request, sink); err != nil {
		sink.Note("\n[teamwork] worker 回合失败：" + err.Error() + "\n")
		sink.Exit(1)
		sink.Complete(jobs.StateFailed, err.Error())
		return nil
	}
	// 幂等：Runner 若已自行收敛终态，这一次是空操作（jobs 的不变式 I-1）。
	sink.Complete(jobs.StateDone, "")
	return nil
}

// SeatExecutor 返回注册进 jobs.Manager 的座位循环执行体（Kind = "seat"，D4：
// goal 座位轮转不再是**与 jobs 并列的第二套驱动**，而是 jobs 契约下的一个实现）。
func SeatExecutor(runner SeatRunner) jobs.Executor {
	return &seatExecutor{runner: runner}
}

type seatExecutor struct {
	runner SeatRunner
}

func (e *seatExecutor) Kind() jobs.Kind { return KindSeat }

func (e *seatExecutor) Start(ctx context.Context, spec jobs.Spec, sink jobs.Sink) error {
	if e.runner == nil {
		return errors.New("teamwork: seat 执行体未装配（缺 SeatRunner）")
	}
	var request SeatRequest
	if len(spec.Payload) > 0 {
		if err := json.Unmarshal(spec.Payload, &request); err != nil {
			sink.Note("\n[teamwork] seat 载荷解码失败：" + err.Error() + "\n")
			sink.Exit(1)
			sink.Complete(jobs.StateFailed, "seat 载荷解码失败")
			return nil
		}
	}
	sink.Progress(fmt.Sprintf("seat 起跑 goal=%s stage=%s", request.GoalID, request.Stage))
	if err := e.runner.RunSeat(ctx, request, sink); err != nil {
		sink.Note("\n[teamwork] 座位循环失败：" + err.Error() + "\n")
		sink.Exit(1)
		sink.Complete(jobs.StateFailed, err.Error())
		return nil
	}
	sink.Complete(jobs.StateDone, "")
	return nil
}

var (
	_ jobs.Executor = (*workerExecutor)(nil)
	_ jobs.Executor = (*seatExecutor)(nil)
)
