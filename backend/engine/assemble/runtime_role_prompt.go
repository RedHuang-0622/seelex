// runtime_role_prompt.go — 员工提示词的"装配读面"（宿主状态 + 一行转发）。
//
// 落点沿革（P21-15「域方法随类型走」）：**优化那一半**（元指令、请求渲染、输出契约解析、
// 回执组装）搬进提示词域 [`backend/engine/prompt`](../../engine/prompt/README.md) 的
// `role_prompt_optimize.go`（plan §2.5：`seelebridge/runtime_role_prompt.go` →
// `backend/engine/prompt/`）；本文件只留**读面状态**与两格宿主读数。
//
// 生态位（把 Agent Team 面板里登记的提示词接到真实回合上）：
//
//   - 读面：`SetRolePromptProvider` 注入"按角色名读已装配提示词"的闭包（装配根
//     从 Agent Team 角色注册表读），ADVISOR/TL 回合据此用用户登记的提示词，未
//     登记时回退内置提示词。这是"提示词装配"真正生效的那一段——此前
//     RoleSpec.SystemPrompt 只落盘、无人消费。
//   - 优化：`OptimizeRolePrompt` 实现 contract.RolePromptPort（判据在域包；本层只把
//     completer 接成"一次无工具回合"，把模型名接成第二格读数）。
//
// 状态（A08，2026-10-15）：**两条都在，但读面的装配根源已退场**。
//   - `rolePromptFor` 的**默认回退是活动的**：ADVISOR/TL 回合（runtime_goal_tl.go）
//     与 worker 回合照旧调用它；未注入读面时返回空串 → 调用方用内置提示词，
//     输出契约不变。这段不能删（删了会改 ADVISOR 的提示词口径）。
//   - `SetRolePromptProvider` / `OptimizeRolePrompt` 保留为**可选对外读面/能力面**：
//     J-1a 之后组合根不再注入来源（角色注册表已退场），静态索引找不到应用层生产
//     调用者；它们是见 `contract.RolePromptPort` 的对外契约，移除须走版本化 API
//     变更（与 `Dependencies.RolePrompt` 同批），不在退场面清理里单独删。
package assemble

import (
	"context"
	"strings"

	"github.com/RedHuang-0622/Seele/types"

	promptlayer "github.com/RedHuang-0622/seelex/backend/engine/prompt"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// SetRolePromptProvider 注入"已装配提示词"读面；传 nil 取消注入（回退内置）。
// 读面在同一次启动内只注入一次，重复调用以最后一次为准。
func (r *Runtime) SetRolePromptProvider(provider func(roleName string) string) {
	if r == nil {
		return
	}
	r.rolePromptMu.Lock()
	r.rolePrompt = provider
	r.rolePromptMu.Unlock()
}

// rolePromptFor 读某个角色登记的提示词；未注入读面或未登记时返回空串
// （调用方用内置提示词兜底）。
func (r *Runtime) rolePromptFor(roleName string) string {
	if r == nil {
		return ""
	}
	r.rolePromptMu.RLock()
	provider := r.rolePrompt
	r.rolePromptMu.RUnlock()
	if provider == nil {
		return ""
	}
	return strings.TrimSpace(provider(roleName))
}

// OptimizeRolePrompt 实现 contract.RolePromptPort：一次有界 LLM 回合，把员工提示词
// 改写成候选版本（判据在提示词域；本层只接两格读数）。
func (r *Runtime) OptimizeRolePrompt(ctx context.Context, request dto.RolePromptOptimizeRequest) (dto.RolePromptOptimizeResult, error) {
	return promptlayer.NewRolePromptOptimizer(promptlayer.RolePromptOptimizerDeps{
		Complete: r.completePromptOptimizeRound,
		Model:    r.Model,
	}).Optimize(ctx, request)
}

// completePromptOptimizeRound 是"一次无工具回合"那一格宿主读数（未装配 completer ⇒
// 返回域包的"不可用"错误）。
func (r *Runtime) completePromptOptimizeRound(ctx context.Context, systemPrompt, userContent string) (string, error) {
	if r == nil || r.completer == nil {
		return "", promptlayer.ErrOptimizerUnavailable
	}
	message, err := r.completer.Complete(ctx, []types.Message{
		{Role: "system", Content: stringPtr(systemPrompt)},
		{Role: "user", Content: stringPtr(userContent)},
	}, nil)
	if err != nil {
		return "", err
	}
	if message.Content == nil {
		return "", nil
	}
	return *message.Content, nil
}
