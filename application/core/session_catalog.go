package core

import (
	"context"

	"github.com/RedHuang-0622/seelex/middle/residency"
)

// WaitCatalogRefresh 等待会话目录 worker 完成一轮"覆盖了本次请求"的刷新，使
// 紧随其后的 Snapshot()/SnapshotOf() 直接携带权威会话目录。
//
// 判据（等待回执、ctx 超时不算失败）已在 M5-R6-session-catalog 随接收者转移归位
// `middle/residency.WaitCatalogRefresh`（plan §2.4）；本方法只把会话协调器交给它。
func (service *Service) WaitCatalogRefresh(ctx context.Context) error {
	return residency.WaitCatalogRefresh(ctx, service.components.sessions)
}
