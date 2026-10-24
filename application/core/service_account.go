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
	"errors"
	"strings"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// AddAccount 把一个新账号写进账号档并热注册进运行时（角色卡下的「添加账户」）。
//
// 返回的**账号名**必须被界面采纳：名字由账号档里的位置推导（`agent-1`、`agent-2`…），
// 界面自己猜不出下一个是谁。
func (service *Service) AddAccount(_ context.Context, draft dto.AccountDraft) (dto.AccountAddResult, error) {
	if service == nil {
		return dto.AccountAddResult{}, errors.New("account admin: service is unavailable")
	}
	if service.Deps.AccountAdmin == nil {
		return dto.AccountAddResult{}, errors.New("account admin: 宿主未提供账号管理能力（无法写入账号档）")
	}
	return service.Deps.AccountAdmin.AddAccount(draft)
}

// AccountDetails 读取账号编辑所需的非秘密字段。
func (service *Service) AccountDetails(name string) (dto.AccountDetails, error) {
	if service == nil {
		return dto.AccountDetails{}, errors.New("account admin: service is unavailable")
	}
	if service.Deps.AccountAdmin == nil {
		return dto.AccountDetails{}, errors.New("account admin: 宿主未提供账号管理能力（无法读取账号档）")
	}
	return service.Deps.AccountAdmin.AccountDetails(strings.TrimSpace(name))
}

// UpdateAccount 原位更新一条账号（APIKey 留空保留原密钥）。
func (service *Service) UpdateAccount(update dto.AccountUpdate) (dto.AccountDetails, error) {
	if service == nil {
		return dto.AccountDetails{}, errors.New("account admin: service is unavailable")
	}
	if service.Deps.AccountAdmin == nil {
		return dto.AccountDetails{}, errors.New("account admin: 宿主未提供账号管理能力（无法更新账号档）")
	}
	update.Name = strings.TrimSpace(update.Name)
	return service.Deps.AccountAdmin.UpdateAccount(update)
}

// DeleteAccount 删除一条账号；账户所属角色与名称不允许借更新迁移。
func (service *Service) DeleteAccount(name string) (dto.AccountDetails, error) {
	if service == nil {
		return dto.AccountDetails{}, errors.New("account admin: service is unavailable")
	}
	if service.Deps.AccountAdmin == nil {
		return dto.AccountDetails{}, errors.New("account admin: 宿主未提供账号管理能力（无法删除账号档）")
	}
	return service.Deps.AccountAdmin.DeleteAccount(strings.TrimSpace(name))
}

// AccountBalance 查询一个账号此刻的余额（角色卡下账户行的逐行读数）。
//
// ctx 会一路传到 HTTP 请求：界面关掉/切走时这次探测随之取消，不留悬挂请求。
func (service *Service) AccountBalance(ctx context.Context, name string) (dto.AccountBalance, error) {
	if service == nil {
		return dto.AccountBalance{}, errors.New("account balance: service is unavailable")
	}
	port, _ := service.Deps.Runtime.(contract.AccountBalancePort)
	if port == nil {
		return dto.AccountBalance{}, errors.New("account balance: 宿主未提供余额查询能力")
	}
	return port.AccountBalance(ctx, name)
}
