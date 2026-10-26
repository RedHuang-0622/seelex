package core

// service_account.go — 账号的两条界面链路：手动添加 与 逐账号查余额。
//
// 分层口径（与 service_account_window.go 同一条纪律）：本层**只做端口解析与转发**，
// 一个判据都不自己写——
//
//   - 添加账号：写账号档的形状与校验在 internal/config，热注册在 Runtime；
//   - 查余额：供应商端点、凭据使用与解析在账号域（backend/engine/account）。
//
// 两边都是"窄可选能力"：宿主没装配时给一句明确错误，而不是静默无效。这样 fake /
// headless harness 不必为了实现这两个方法而长出一堆空壳。

import applicationadmin "github.com/RedHuang-0622/seelex/application/admin"

func (service *Service) adminApplicationComponent() *applicationadmin.AccountApplication {
	if service == nil {
		return nil
	}
	return service.components.adminApplication
}
