package assemble

import (
	"context"
	"errors"
	"fmt"

	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/search"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/model"
)

// ports.go 承载 Runtime 对 contract 端口的实现方法（组合根公开面）。
// 域实现已下沉子包；本文件只保留类型别名与逐行委托，DTO 统一走
// contract/dto。mainAgentNodeID 是子代理树的合成根节点 ID。
const mainAgentNodeID = model.MainAgentNodeID

// ── plan 端口 ─────────────────────────────────────────────────────

// PrepareReplan atomically replaces a failed WorkPlan with a recovery plan.
// It only plans; it never invokes plan_run or retries side effects.
func (r *Runtime) PrepareReplan(ctx context.Context, request dto.ReplanRequest) (dto.PlanPreflight, error) {
	if r == nil || r.planExecutor == nil {
		return dto.PlanPreflight{}, fmt.Errorf("plan replan: plan executor is unavailable")
	}
	return r.planExecutor.PrepareReplan(ctx, request)
}

// ── 历史检索端口 ──────────────────────────────────────────────────

// SearchHistory 在会话压缩栈（语义索引）上检索历史聊天记录（GUI 历史检索
// 面板与 search_history 工具共享的数据面；query 非空校验由调用方/检索器
// 双层执行，limit 为 token 预算在 search 包内 clamp 到硬上限）。
func (r *Runtime) SearchHistory(ctx context.Context, query string, limit int) (search.Result, error) {
	if r == nil {
		return search.Result{}, errors.New("search_history: runtime is unavailable")
	}
	router := r.HistoryRouter()
	if router == nil {
		return search.Result{}, errors.New("search_history: 事件库未装配（会话持久化未启用）")
	}
	var stack search.StackSource
	if store := r.sessionContextStore(); store != nil {
		stack = store
	}
	searcher := search.New(stack, search.NewRouterEventSource(router, router.Workspace(), r.MainSessionID()))
	return searcher.Search(ctx, query, search.Options{Limit: limit})
}

// ── actor 消息边界（应用 → Runtime 单向发布）──────────────────────

// RuntimeVisibilityProjection is an immutable application-to-runtime message.
type RuntimeVisibilityProjection = dto.RuntimeVisibilityProjection

// ParentEvidenceProjection is the minimal application-owned data needed for a
// Runtime-local parent evidence snapshot. Runtime adds its own telemetry and
// stores the resulting immutable snapshot for subagents to read.
type ParentEvidenceProjection = model.ParentEvidenceProjection

// SetRuntimeVisibilityProjection publishes a value copy from Application. It
// has no synchronous reverse callback and is safe from Runtime tool hooks.
func (r *Runtime) SetRuntimeVisibilityProjection(projection RuntimeVisibilityProjection) {
	if r == nil {
		return
	}
	copy := projection
	r.visibilityProjection.Store(&copy)
}

// SetParentEvidenceProjection turns the application projection into a
// Runtime-owned immutable snapshot. A blank session clears stale evidence.
func (r *Runtime) SetParentEvidenceProjection(projection ParentEvidenceProjection) {
	if r == nil {
		return
	}
	r.subagentContext.SetParentEvidenceProjection(projection)
}
