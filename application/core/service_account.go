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

import (
	"context"

	applicationadmin "github.com/RedHuang-0622/seelex/application/admin"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

func (service *Service) adminApplicationComponent() *applicationadmin.AccountApplication {
	if service == nil {
		return nil
	}
	return service.components.adminApplication
}

// AddAccount 转发 Admin 账号用例；返回的账号名由宿主按账号档位置推导。
func (service *Service) AddAccount(ctx context.Context, draft dto.AccountDraft) (dto.AccountAddResult, error) {
	return service.adminApplicationComponent().AddAccount(ctx, draft)
}

// AccountDetails 转发账号非秘密字段读取。
func (service *Service) AccountDetails(name string) (dto.AccountDetails, error) {
	return service.adminApplicationComponent().AccountDetails(name)
}

// UpdateAccount 转发账号原位更新。
func (service *Service) UpdateAccount(update dto.AccountUpdate) (dto.AccountDetails, error) {
	return service.adminApplicationComponent().UpdateAccount(update)
}

// DeleteAccount 转发账号删除。
func (service *Service) DeleteAccount(name string) (dto.AccountDetails, error) {
	return service.adminApplicationComponent().DeleteAccount(name)
}

// AccountBalance 转发指定账号的余额探测。
func (service *Service) AccountBalance(ctx context.Context, name string) (dto.AccountBalance, error) {
	return service.adminApplicationComponent().AccountBalance(ctx, name)
}
