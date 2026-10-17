package workunit

import (
	"context"
	"fmt"
	"time"
)

// merge_retry.go — 「主工作区挡路」的等待策略。
//
// 这是一条**生命周期判据**，不是现场适配器的机械动作：档路要不要等、等多久、取消时怎么收口，
// 都由 Host（唯一的生命周期实现）决定。现场适配器只负责跑一次 merge，并把
// `ErrMergeBlockedByMain` 这个事实交回来。

const (
	// defaultMergeRetryBudget / defaultMergeRetryInterval 与既有行为一致：挡路时最多等 2 分钟，
	// 每 5 秒重试一次。等待发生在 MergeScene 之外，因此不占现场的收尾单写者。
	defaultMergeRetryBudget   = 2 * time.Minute
	defaultMergeRetryInterval = 5 * time.Second

	// MergePhaseAwaiting 是"等主工作区干净"的阶段名（观察者可投影到前端）。
	MergePhaseAwaiting = "awaiting_merge"
)

// MergeRetry 是「主工作区挡路」的等待预算与可选阶段观察者。
//
//	Budget   <= 0：不等待，第一次被挡即收口；
//	Interval <= 0：立即重试（仍受 Budget 约束）；
//	Observe  为 nil：不报阶段（生产装配默认不注入观察者）。
type MergeRetry struct {
	Budget   time.Duration
	Interval time.Duration
	Observe  func(ctx context.Context, nodeID, phase string)
}

// DefaultMergeRetry 返回既有的默认等待预算。
func DefaultMergeRetry() MergeRetry {
	return MergeRetry{Budget: defaultMergeRetryBudget, Interval: defaultMergeRetryInterval}
}

// HostOption 是 Host 的构造选项（只用于注入策略，不改变三格端口）。
type HostOption func(*Host)

// WithMergeRetry 覆盖默认的挡路等待策略（测试与需要不同预算的装配点使用）。
func WithMergeRetry(retry MergeRetry) HostOption {
	return func(h *Host) {
		if h != nil {
			h.mergeRetry = retry
		}
	}
}

// mergeScene 调用现场端口一次，并在**主工作区挡路**时按预算等待重试。
//
//	只有 `IsMergeBlockedByMain` 认的那一族才重试：真冲突、rebase 失败、缺栏都是确定性失败，
//	重试只会对着半成品反复撞墙。
//	等待发生在 MergeScene 之外：现场的单写者只被占一次调用，不占整段等待。
//	预算耗尽或等待被取消 → 交回带 `ErrMergeBlockedByMain` 的分类错误（现场保留）。
func (h *Host) mergeScene(ctx context.Context, own Ownership, u Unit) error {
	retry := h.mergeRetry
	deadline := time.Now().Add(retry.Budget)
	attempts := 0
	for {
		attempts++
		err := h.scenes.MergeScene(ctx, own, u.ID(), u.SessionPath())
		if err == nil || !IsMergeBlockedByMain(err) {
			return err
		}
		if ctx.Err() != nil || time.Now().After(deadline) {
			return fmt.Errorf("%w：%v（本次已尝试 %d 次；先提交或暂存主工作区的在途改动，再重试合并）", ErrMergeBlockedByMain, err, attempts)
		}
		if retry.Observe != nil {
			retry.Observe(ctx, u.ID(), MergePhaseAwaiting)
		}
		timer := time.NewTimer(retry.Interval)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("%w：%v（等待被取消；现场保留）", ErrMergeBlockedByMain, err)
		}
	}
}
