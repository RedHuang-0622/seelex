// 历史记录检索（search_history 工具 + GUI 数据面，2026-08-07）：
// 压缩栈帧是语义索引——seelexctx/search 用 memory.Select 在全部 CompactStack
// 帧上选相关帧，命中帧按 [From..To] 单元范围从事件库读回真实聊天记录，
// token 预算内内联返回（无需读回句柄分页）。本文件只做参数校验与转发，
// 检索逻辑全部在 seelexctx/search（纯数据 + 构造注入，可单测）。
package core

import (
	"context"

	conversationsearch "github.com/RedHuang-0622/seelex/application/conversation/search"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// SearchHistory 返回会话历史检索结果（GUI 历史检索面板数据源；query 非空
// 校验；limit=0 与 token 预算由 Runtime/search 实现按同一默认值 clamp）。
func (service *Service) SearchHistory(ctx context.Context, query string, limit int) (dto.SearchResult, error) {
	return service.historySearchApplicationComponent().SearchHistory(ctx, query, limit)
}

// SearchHistoryHandler 实现 search_history 工具：模型在上下文缺少相关历史
// 时可主动调用（如用户提到早先的讨论/决定）；命中内联真实聊天记录
// （受 token 预算约束，Result 为权威返回）。
func (service *Service) SearchHistoryHandler(_ context.Context, argsJSON string) (string, error) {
	return service.historySearchApplicationComponent().SearchHistoryHandler(argsJSON)
}

func (service *Service) historySearchApplicationComponent() *conversationsearch.Application {
	if service == nil {
		return nil
	}
	return service.components.historySearchApplication
}

func newServiceHistorySearchApplication(service *Service) *conversationsearch.Application {
	return conversationsearch.New(conversationsearch.Deps{
		Available: func() bool {
			return service != nil && service.Deps.Runtime != nil
		},
		Search: func(ctx context.Context, query string, limit int) (dto.SearchResult, error) {
			return service.Deps.Runtime.SearchHistory(ctx, query, limit)
		},
	})
}
