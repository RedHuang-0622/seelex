package core

import (
	"context"
	"log"
)

// 同会话压缩串行门（状态字段见 service_state.go 的 compacting/compactSig）。
//
// 形状抄的是本仓库已有的 restoring 门（session_scope.go 的 awaitRestore +
// deferSubmitUntilRestored），不是随手新发明一套并发原语：两条不变量同构——
// 「某个不改 Running 的会话级动作正在进行时不得开新回合」+「提交入口不得阻塞
// （交互与后台两条路径都要立即受理），所以把提交挂到动作收口点再在同一会话上
// 重放」。复用同一形状的收益是读代码的人只需要理解一次。
//
// 归属：门是**会话生命周期**的判据（谁能在这个会话上开回合），不是上下文装配
// 算法的一部分，因此住在 application/core（提交入口这一侧）；领轮/收口由显式
// 压缩入口 CompactContextNow 包住折叠那一段（context_compact.go）。
//
// 加锁纪律（破坏任一条都会变成挂死而不是排队）：
//  1. 领轮/收口/复判只在 Core.ViewMu 内做，**持锁期间不等任何东西**；
//  2. 等待方睡在信号通道上：锁内先把通道读出来，锁外 select；
//  3. 压缩轮自己绝不 await 自己的门（领轮是阻塞获取，拿到就跑、跑完即收口），
//     因此折叠内部（装配路径）不需要也不能再查这道门。

// isCompactingLocked 报告该会话是否有一轮上下文压缩正在进行（调用方持有
// Core.ViewMu）。
func (service *Service) isCompactingLocked(sessionID string) bool {
	if service == nil || service.compacting == nil {
		return false
	}
	_, ok := service.compacting[sessionID]
	return ok
}

// signalCompactionLocked 广播一次「compacting 集合已变化」（调用方持有
// Core.ViewMu）：关闭当前信号并重建，等待方在通道关闭后重读集合复判。
func (service *Service) signalCompactionLocked() {
	if service.compactSig == nil {
		service.compactSig = make(chan struct{})
		return
	}
	close(service.compactSig)
	service.compactSig = make(chan struct{})
}

// compactionSignalLocked 返回当前收口信号（调用方持有 Core.ViewMu）。惰性
// 初始化保证直接构造 serviceState 的宿主也不会拿到 nil 通道。
func (service *Service) compactionSignalLocked() <-chan struct{} {
	if service.compactSig == nil {
		service.compactSig = make(chan struct{})
	}
	return service.compactSig
}

// acquireCompactionRound 领取该会话的压缩轮：已有轮在跑时先等它收口（同会话
// 串行），别的会话不受影响。返回 nil 表示轮已归调用方，必须配对 release。
// ctx 取消时返回 ctx.Err()，调用方不得继续折叠。
func (service *Service) acquireCompactionRound(ctx context.Context, sessionID string) error {
	for {
		service.ViewMu.Lock()
		if !service.isCompactingLocked(sessionID) {
			if service.compacting == nil {
				service.compacting = make(map[string]struct{})
			}
			service.compacting[sessionID] = struct{}{}
			service.ViewMu.Unlock()
			return nil
		}
		signal := service.compactionSignalLocked()
		service.ViewMu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-signal:
		}
	}
}

// releaseCompactionRound 收口该会话的压缩轮并唤醒等待方。幂等：没领过轮的
// 收口是空操作，不会把别人的轮关掉。
func (service *Service) releaseCompactionRound(sessionID string) {
	service.ViewMu.Lock()
	if _, ok := service.compacting[sessionID]; ok {
		delete(service.compacting, sessionID)
		service.signalCompactionLocked()
	}
	service.ViewMu.Unlock()
}

// awaitCompactionRound 等到该会话没有压缩轮在跑（ctx 取消即返回错误）。
func (service *Service) awaitCompactionRound(ctx context.Context, sessionID string) error {
	for {
		service.ViewMu.Lock()
		compacting := service.isCompactingLocked(sessionID)
		signal := service.compactionSignalLocked()
		service.ViewMu.Unlock()
		if !compacting {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-signal:
		}
	}
}

// deferSubmitUntilCompacted 把一次对话提交挂到该会话压缩轮的收口点：门开着时
// 既不开新回合也不拒绝，收口后在**同一目标会话**上重放。为什么不能改成"丢进
// 队列就行"：显式压缩没有回合，队列的正常提升点在回合结束，那条消息会一直躺
// 在队列里等一个永远不会到来的回合结束。
// 失败只记日志，口径与 deferSubmitUntilRestored 一致。
func (service *Service) deferSubmitUntilCompacted(ctx context.Context, sessionID, text string) {
	go func() {
		if err := service.awaitCompactionRound(ctx, sessionID); err != nil {
			return
		}
		if err := service.submitConversationFor(ctx, sessionID, text); err != nil {
			log.Printf("[submit] deferred submit to %q after compaction: %v", sessionID, err)
		}
	}()
}
