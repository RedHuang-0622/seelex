package residency

import "context"

// catalog.go 是"等一轮覆盖本次请求的会话目录刷新"的判据落点。
//
// 落点沿革：2026-10-16 随 **M5-R6-session-catalog** 接收者转移从
// `application/core/session_catalog.go` 归位（plan §2.4 的 `session_catalog.go` → `middle/residency/`）。

// CatalogRefresher 是"请求一次目录刷新"的窄口（结构满足会话协调器的同名方法：
// 返回一个在本轮刷新完成时关闭的通道）。**必须非 nil**——迁移前语义即"直接调用宿主"，
// 本包不放宽也不兜底。
type CatalogRefresher interface {
	RequestCatalogRefresh() <-chan struct{}
}

// WaitCatalogRefresh 等一轮"覆盖了本次请求"的目录刷新，使紧随其后的 Snapshot()/SnapshotOf()
// 直接携带权威会话目录。
//
// 目录枚举与标题恢复留在 worker 内（它们走外部 SessionPort/WorkspacePort，不能塞进变更命令的
// 同步路径），因此需要"列表已收敛"的调用方显式等待回执，而不是由前端在拿到空列表时回填上一次的状态。
//
// ctx 超时时返回 ctx.Err()：这不是失败——worker 稍后收尾仍会发布 snapshot.changed，
// 等待方按最佳努力收敛即可。
func WaitCatalogRefresh(ctx context.Context, refresher CatalogRefresher) error {
	done := refresher.RequestCatalogRefresh()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
