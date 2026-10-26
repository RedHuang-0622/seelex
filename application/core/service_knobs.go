package core

// service_knobs.go — 两条独立拉条（思考强度 / 循环次数）的**宿主适配 + 门面转发**。
//
// 判据与状态本体已在 M5-R-limitb 随接收者转移归位 `backend/limit.Knobs`（plan §2.8）；
// 本文件只剩两件事：
//  1. `serviceKnobsHost`：把 Service 的视图锁、快照写面、引擎与档位读数实现成
//     `limit.KnobsHost`（全部是既有内部调用，不含新判据）；
//  2. 两个门面方法（`SetReasoningEffort` / `SetLoopLimit`）的一行转发——外部调用点零改动。

import (
	"github.com/RedHuang-0622/seelex/backend/limit"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// serviceKnobsHost 把 Service 的能力实现成 limit.KnobsHost。
//
// 加锁次序由本适配保持与迁移前一致：视图锁（ViewMu）→ 组件锁（Knobs 自己的锁），只单向；
// 投影的读数收集（Knobs.Readings）刻意**不取** ViewMu（收集在锁外、应用在锁内）。
type serviceKnobsHost struct{ service *Service }

func (host serviceKnobsHost) ViewSession() (string, bool) {
	service := host.service
	service.ViewMu.RLock()
	sessionID := service.Core.Snapshot.Session.ID
	running := false
	if unit := service.sessions.Unit(sessionID); unit != nil {
		running = unit.ChatState().Running
	}
	service.ViewMu.RUnlock()
	return sessionID, running
}

func (host serviceKnobsHost) MutateRuntime(mutate func(*model.RuntimeState)) {
	if mutate == nil {
		return
	}
	service := host.service
	service.ViewMu.Lock()
	defer service.ViewMu.Unlock()
	mutate(&service.Core.Snapshot.Runtime)
}

func (host serviceKnobsHost) PublishRuntime(mutate func(*model.RuntimeState)) error {
	service := host.service
	service.ViewMu.Lock()
	if mutate != nil {
		mutate(&service.Core.Snapshot.Runtime)
	}
	revision := service.bumpLocked()
	runtime := cloneRuntimeState(service.Core.Snapshot.Runtime)
	service.ViewMu.Unlock()
	// 会话取"当前视图会话"：与迁移前一样在**锁外**取（currentViewSessionID 自己持锁）。
	service.publishSessionEvent(EventRuntimeChanged, revision, "", service.currentViewSessionID(), runtime)
	return nil
}

func (host serviceKnobsHost) SetEngineMaxLoops(limit int) {
	if host.service.Deps.Engine != nil {
		host.service.Deps.Engine.SetMaxLoops(limit)
	}
}

func (host serviceKnobsHost) CurrentEffort() string {
	return host.service.effortManager.Current()
}

func (host serviceKnobsHost) MaxLoopsForEffort(level string) int {
	return maxLoopsFor(level)
}

func (host serviceKnobsHost) ReasoningEffortSink() contract.ReasoningEffortPort {
	if host.service.Deps.Runtime == nil {
		return nil
	}
	sink, _ := host.service.Deps.Runtime.(contract.ReasoningEffortPort)
	return sink
}

// newServiceKnobs 在装配期为某个 Service 建拉条组件（宿主就是它自己）。
func newServiceKnobs(service *Service) *limit.Knobs {
	return limit.NewKnobs(serviceKnobsHost{service: service})
}
