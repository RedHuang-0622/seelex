package core

import (
	"context"
)

// WaitCatalogRefresh 等待会话目录 worker 完成一轮"覆盖了本次请求"的刷新，使
// 紧随其后的 Snapshot()/SnapshotOf() 直接携带权威会话目录。
//
// 目录枚举与标题恢复留在 worker 内（它们走外部 SessionPort/WorkspacePort，
// 不能塞进变更命令的同步路径），因此需要"列表已收敛"的调用方显式等待回执，
// 而不是由前端在拿到空列表时回填上一次的状态。
//
// ctx 超时时返回 ctx.Err()：这不是失败——worker 稍后收尾仍会发布
// snapshot.changed，等待方按最佳努力收敛即可。
func (service *Service) WaitCatalogRefresh(ctx context.Context) error {
	done := service.components.sessions.RequestCatalogRefresh()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
