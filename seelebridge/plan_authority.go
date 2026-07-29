package seelebridge

import (
	"context"
	"fmt"
	"sync"
)

type planActPhase uint8

const (
	planActPreflight planActPhase = iota + 1
	planActAuthoritative
)

type planActScopeContextKey struct{}

// PlanActScope atomically owns one request's preflight load and following
// authoritative ReAct turn. PreflightContext grants the scope's internal
// plan_load call; Promote then hides Plan mutation tools from normal ReAct.
// Release is idempotent.
type PlanActScope interface {
	PreflightContext(context.Context) context.Context
	Promote() error
	Release()
}

type planActScope struct {
	runtime   *Runtime
	requestID string
	phase     planActPhase
	once      sync.Once
}

// AcquirePlanActScope reserves Runtime's single Seele Agent for requestID
// before preflight starts. A concurrent request is rejected rather than being
// allowed to replace the WorkPlan between another request's load and run.
func (r *Runtime) AcquirePlanActScope(requestID string) (PlanActScope, error) {
	if requestID == "" {
		return nil, fmt.Errorf("plan scope: request ID is required")
	}
	r.planAuthorityMu.Lock()
	if r.planActScope != nil {
		owner := r.planActScope.requestID
		r.planAuthorityMu.Unlock()
		return nil, fmt.Errorf("plan scope: request %q is active", owner)
	}
	scope := &planActScope{runtime: r, requestID: requestID, phase: planActPreflight}
	r.planActScope = scope
	r.planAuthorityMu.Unlock()
	return scope, nil
}

func (scope *planActScope) PreflightContext(ctx context.Context) context.Context {
	if scope == nil {
		return ctx
	}
	return context.WithValue(ctx, planActScopeContextKey{}, scope)
}

func (scope *planActScope) Promote() error {
	if scope == nil || scope.runtime == nil {
		return fmt.Errorf("plan scope: nil scope")
	}
	runtime := scope.runtime
	runtime.planAuthorityMu.Lock()
	if runtime.planActScope != scope {
		runtime.planAuthorityMu.Unlock()
		return fmt.Errorf("plan scope: request %q is no longer active", scope.requestID)
	}
	if scope.phase != planActPreflight {
		runtime.planAuthorityMu.Unlock()
		return fmt.Errorf("plan scope: request %q is not in preflight", scope.requestID)
	}
	scope.phase = planActAuthoritative
	runtime.planAuthorityMu.Unlock()
	runtime.refreshPlanToolVisibility()
	return nil
}

func (scope *planActScope) Release() {
	if scope == nil || scope.runtime == nil {
		return
	}
	scope.once.Do(func() {
		runtime := scope.runtime
		runtime.planAuthorityMu.Lock()
		if runtime.planActScope != scope {
			runtime.planAuthorityMu.Unlock()
			return
		}
		wasAuthoritative := scope.phase == planActAuthoritative
		runtime.planActScope = nil
		runtime.planAuthorityMu.Unlock()
		if wasAuthoritative {
			runtime.refreshPlanToolVisibility()
		}
	})
}

func (r *Runtime) preflightPlanAuthoritative() bool {
	r.planAuthorityMu.RLock()
	defer r.planAuthorityMu.RUnlock()
	return r.planActScope != nil && r.planActScope.phase == planActAuthoritative
}

// authorizePlanMutation allows plan_load only for the private context of the
// active preflight. It rejects all external mutations during both phases,
// including stale model tool snapshots.
func (r *Runtime) authorizePlanMutation(ctx context.Context, toolName string) error {
	r.planAuthorityMu.RLock()
	scope := r.planActScope
	phase := planActPhase(0)
	if scope != nil {
		phase = scope.phase
	}
	r.planAuthorityMu.RUnlock()
	if scope == nil {
		return nil
	}
	if phase == planActPreflight {
		if caller, _ := ctx.Value(planActScopeContextKey{}).(*planActScope); caller == scope {
			return nil
		}
		return fmt.Errorf("%s: plan preflight is reserved for request %q", toolName, scope.requestID)
	}
	return fmt.Errorf("%s: authoritative preflight plan is already loaded; use plan_run or explicit replan", toolName)
}
