package core

// 并发故障的 fail-fast 与降级策略：
// - 数据竞争/死锁类问题先以可复现用例 + -race 门禁检测（检测即失败，不吞掉）；
// - 生命周期消费者等已知 goroutine 中发生 panic 时，不再“记录后继续”静默
//   运行（半坏状态可能持续污染其它会话），而是先执行降级退出：
//   取消全部运行会话 → 停止目录 worker → 标记 closed/draining → 发布
//   EventExitRequested，随后重新抛出 panic，让宿主及时退出；
// - 死锁类问题由带超时的探针用例负责：超时即 t.Fatal/panic，不无限挂起。

import applicationlifecycle "github.com/RedHuang-0622/seelex/application/lifecycle"

// degradeAndRequestExit 进入降级退出路径（幂等）。调用方不得持有任何锁。
func (service *Service) degradeAndRequestExit(reason string) {
	service.faultGuardApplicationComponent().DegradeAndRequestExit(reason)
}

// handleLifecycleFault 处理生命周期消费者中的并发/逻辑故障：先降级退出，
// 再重新抛出 panic（fail-fast），不让半坏状态继续运行。
func (service *Service) handleLifecycleFault(recovered any) {
	service.faultGuardApplicationComponent().HandleLifecycleFault(recovered)
}

func (service *Service) faultGuardApplicationComponent() *applicationlifecycle.FaultGuardApplication {
	if service == nil {
		return nil
	}
	return service.components.faultGuardApplication
}

func newServiceFaultGuardApplication(service *Service) *applicationlifecycle.FaultGuardApplication {
	return applicationlifecycle.NewFaultGuard(applicationlifecycle.FaultGuardDeps{
		EnterDegraded: func() bool {
			service.ViewMu.Lock()
			defer service.ViewMu.Unlock()
			service.closed = true
			service.draining = true
			for _, sid := range service.sessions.UnitIDs() {
				if unit := service.sessions.Unit(sid); unit != nil {
					if cancel := unit.CancelFunc(); cancel != nil {
						cancel()
					}
				}
			}
			return true
		},
		StopCatalog: func() {
			service.components.sessions.StopCatalogRefresh()
		},
		CloseSessions: service.sessions.Close,
		StopConsumers: service.stopLifecycleConsumers,
		CloseWorkTable: func() {
			if service.workTable != nil {
				service.workTable.Close()
			}
		},
		ShutdownApproval: service.Approval.Shutdown,
		PublishExit: func() {
			service.Events.Publish(EventExitRequested, 0, "", nil)
		},
	})
}
