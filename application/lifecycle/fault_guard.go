package lifecycle

import (
	"log"
	"sync"
)

// FaultGuardDeps supplies the cleanup actions after a fatal lifecycle fault.
type FaultGuardDeps struct {
	EnterDegraded    func() bool
	StopCatalog      func()
	CloseSessions    func()
	StopConsumers    func()
	CloseWorkTable   func()
	ShutdownApproval func()
	PublishExit      func()
}

// FaultGuardApplication owns the one-shot degraded-exit sequence.
type FaultGuardApplication struct {
	deps        FaultGuardDeps
	degradeOnce sync.Once
}

// NewFaultGuard constructs a fault guard application.
func NewFaultGuard(deps FaultGuardDeps) *FaultGuardApplication {
	return &FaultGuardApplication{deps: deps}
}

// DegradeAndRequestExit enters degraded state and performs cleanup once.
func (application *FaultGuardApplication) DegradeAndRequestExit(reason string) {
	if application == nil {
		return
	}
	application.degradeOnce.Do(func() {
		if !application.deps.EnterDegraded() {
			return
		}
		log.Printf("[fault-guard] %s —— 进入降级退出：取消全部会话并停止后台 worker", reason)
		application.deps.StopCatalog()
		application.deps.CloseSessions()
		application.deps.StopConsumers()
		application.deps.CloseWorkTable()
		application.deps.ShutdownApproval()
		application.deps.PublishExit()
	})
}

// HandleLifecycleFault degrades and then rethrows the original panic value.
func (application *FaultGuardApplication) HandleLifecycleFault(recovered any) {
	application.DegradeAndRequestExit(logPanicReason(recovered))
	panic(recovered)
}

func logPanicReason(recovered any) string {
	if err, ok := recovered.(error); ok {
		return "lifecycle consumer panic: " + err.Error()
	}
	return "lifecycle consumer panic"
}
