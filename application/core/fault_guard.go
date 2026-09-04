package core

// 并发故障的 fail-fast 与降级策略：
// - 数据竞争/死锁类问题先以可复现用例 + -race 门禁检测（检测即失败，不吞掉）；
// - 生命周期消费者等已知 goroutine 中发生 panic 时，不再“记录后继续”静默
//   运行（半坏状态可能持续污染其它会话），而是先执行降级退出：
//   取消全部运行会话 → 停止目录 worker → 标记 closed/draining → 发布
//   EventExitRequested，随后重新抛出 panic，让宿主及时退出；
// - 死锁类问题由带超时的探针用例负责：超时即 t.Fatal/panic，不无限挂起。

import "log"

// degradeAndRequestExit 进入降级退出路径（幂等）。调用方不得持有任何锁。
func (service *Service) degradeAndRequestExit(reason string) {
	if service == nil {
		return
	}
	service.degradeOnce.Do(func() {
		log.Printf("[fault-guard] %s —— 进入降级退出：取消全部会话并停止后台 worker", reason)
		service.ViewMu.Lock()
		service.closed = true
		service.draining = true
		// 取消所有运行中会话的执行（与会话域持有的 cancel）。
		for _, sid := range service.sessions.UnitIDs() {
			if unit := service.sessions.Unit(sid); unit != nil {
				if cancel := unit.CancelFunc(); cancel != nil {
					cancel()
				}
			}
		}
		service.ViewMu.Unlock()
		service.components.sessions.StopCatalogRefresh()
		service.sessions.Close()
		service.stopLifecycleConsumers()
		if service.workTablePublisher != nil {
			service.workTablePublisher.Close()
		}
		service.Approval.Shutdown()
		// 进程级退出通知（空 sid；订阅方据此关闭窗口/退出 CLI）。
		service.Events.Publish(EventExitRequested, 0, "", nil)
	})
}

// handleLifecycleFault 处理生命周期消费者中的并发/逻辑故障：先降级退出，
// 再重新抛出 panic（fail-fast），不让半坏状态继续运行。
func (service *Service) handleLifecycleFault(recovered any) {
	service.degradeAndRequestExit(logPanicReason(recovered))
	panic(recovered)
}

func logPanicReason(recovered any) string {
	if err, ok := recovered.(error); ok {
		return "lifecycle consumer panic: " + err.Error()
	}
	return "lifecycle consumer panic"
}
