package adapters

import (
	"context"

	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
	"github.com/RedHuang-0622/seelex/seelebridge"
)

// PushCompactionFrame 实现 context_runtime.CompactionIndexPort：把装配层折叠产出的
// 帧推进会话压缩栈，让 search_history 的帧索引与记忆块初筛有东西可选。
//
// 本层只做两件事：**类型转换**（contract.EngineMessage → types.Message，复用
// messages.go 的 restoreMessages，不另写一套）与**转发**。链锚点补齐、原文归档、
// 区间校验都在 seelebridge 侧，因为它们要读压缩栈与归档器，那是 Runtime 的家。
//
// 这是个**可选**能力：调用方用类型断言探测（见 context_runtime.CompactionIndexPort
// 的注释）。断言失败 = 不索引，折叠本身照常成立——所以 RuntimePort 之外的
// fake/harness 不需要实现它，也不必被迫长出一个空方法。
func (port RuntimePort) PushCompactionFrame(
	ctx context.Context,
	sessionID string,
	request context_runtime.CompactionIndexRequest,
) (context_runtime.CompactionIndexReceipt, error) {
	if port.Runtime == nil {
		return context_runtime.CompactionIndexReceipt{}, context_runtime.ErrCompactionIndexUnavailable
	}
	receipt, err := port.Runtime.PushCompactionFrame(ctx, sessionID, seelebridge.CompactionFrameRequest{
		Overflow:      restoreMessages(request.Overflow),
		UnitCount:     request.UnitCount,
		ReplayHistory: restoreMessages(request.ReplayHistory),
		EventFrom:     request.EventFrom,
		EventTo:       request.EventTo,
		MessageFrom:   request.MessageFrom,
		MessageTo:     request.MessageTo,
	})
	if err != nil {
		return context_runtime.CompactionIndexReceipt{}, err
	}
	return context_runtime.CompactionIndexReceipt{
		SegmentID:     receipt.SegmentID,
		Summary:       receipt.Summary,
		SummarySource: receipt.SummarySource,
		SummaryNote:   receipt.SummaryNote,
	}, nil
}
