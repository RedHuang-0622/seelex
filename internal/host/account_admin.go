// account_admin.go — 「手动添加账号」的宿主适配器（界面 → 账号档 + 运行时）。
//
// 两件事必须同一个动作里做完，否则界面会显示一条"加了但用不了"的账号：
//
//  1. 写账号档（internal/config.AddAccount：行级插入 + 真实加载器复核 + 原子替换）；
//  2. 热注册进运行时（assemble.Runtime.RegisterAccount：进 P2C 池 + 进账号管理器），
//     让新账号**不重启**就能被选中、承流、查余额。
//
// 读回规格用的是与启动同一条加载器（config.Load），不是把界面输入自己拼成 spec——
// 这样 defaults 继承、name 推导、限额解析与重启后完全一致，不会出现"现在能用、
// 重启就变样"。
package host

import (
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/config"
	"github.com/RedHuang-0622/seelex/internal/model"
)

// AccountRegistrar 是 AccountAdmin 依赖的**最小**运行时面：只要求"能热注册一个账号"。
//
// 为什么不用 *assemble.Runtime：适配器只需要这一个动作，收窄成接口后测试可以直接喂
// 假实现（不必为了加一个账号去构造整个 Runtime），装配根传进来的 *assemble.Runtime
// 结构上就满足它。
type AccountRegistrar interface {
	RegisterAccount(spec model.AccountSpec, limits config.AccountLimits) error
	ReplaceAccount(spec model.AccountSpec, limits config.AccountLimits) error
	UnregisterAccount(name string) error
}

// AccountAdmin 实现 contract.AccountAdminPort。
type AccountAdmin struct {
	// Path 是账号档的落点（宿主解析：wiring/config.AccountsPath，与启动装载同一份）。
	Path string
	// Runtime 是热注册的落点（装配根）；为 nil 时明确报错，不做"只写盘不生效"的半件事。
	Runtime AccountRegistrar
}

// AddAccount 写入一条新账号并返回生效回执。
//
// 错误口径（都要让界面能看懂）：
//   - 校验/落盘失败 → 原样返回写入器的错误（它已经把"哪一步、为什么"写清楚了），
//     此时**运行时没有任何变化**；
//   - 已落盘但热注册失败 → 明确说"已写入账号档，重启后生效"——这时的真相是
//     "配置改了、当前进程还没认"，不能报成"添加失败"（那会让用户重复添加）。
func (admin AccountAdmin) AddAccount(draft dto.AccountDraft) (dto.AccountAddResult, error) {
	if strings.TrimSpace(admin.Path) == "" {
		return dto.AccountAddResult{}, errors.New("account admin: 账号档路径不可用（宿主未解析出 accounts.yaml）")
	}
	if admin.Runtime == nil {
		return dto.AccountAddResult{}, errors.New("account admin: runtime 不可用")
	}

	name, err := config.AddAccount(admin.Path, config.NewAccountInput{
		Role:            draft.Role,
		Provider:        draft.Provider,
		Model:           draft.Model,
		BaseURL:         draft.BaseURL,
		APIKey:          draft.APIKey,
		ReasoningEffort: draft.ReasoningEffort,
	})
	if err != nil {
		return dto.AccountAddResult{}, err
	}

	loaded, err := config.Load(admin.Path)
	if err != nil {
		return dto.AccountAddResult{}, fmt.Errorf("account admin: 账号档已写入，但读回失败（重启后生效）: %w", err)
	}
	spec, ok := specNamed(loaded.Specs, name)
	if !ok {
		return dto.AccountAddResult{}, fmt.Errorf("account admin: 账号档已写入 %q，但加载器没有产出这条账号（重启后请检查账号档）", name)
	}
	if err := admin.Runtime.RegisterAccount(spec, loaded.Limits[name]); err != nil {
		return dto.AccountAddResult{}, fmt.Errorf("account admin: 账号档已写入 %q，但热注册失败（重启后生效）: %w", name, err)
	}

	return dto.AccountAddResult{
		Name:     spec.Name,
		Role:     string(spec.Role),
		Provider: spec.Provider,
		Model:    spec.Model,
	}, nil
}

// AccountDetails 读取账号编辑所需的非秘密字段；APIKey 只报告是否已配置。
func (admin AccountAdmin) AccountDetails(name string) (dto.AccountDetails, error) {
	if strings.TrimSpace(admin.Path) == "" {
		return dto.AccountDetails{}, errors.New("account admin: 账号档路径不可用（宿主未解析出 accounts.yaml）")
	}
	record, err := config.ReadAccount(admin.Path, name)
	if err != nil {
		return dto.AccountDetails{}, err
	}
	return accountDetailsDTO(record), nil
}

// UpdateAccount 先改账号档，再把新规格热替换进运行时。热替换失败时账号档已经
// 是用户看到的新值，错误必须明确写“重启后生效”。
func (admin AccountAdmin) UpdateAccount(update dto.AccountUpdate) (dto.AccountDetails, error) {
	if strings.TrimSpace(admin.Path) == "" {
		return dto.AccountDetails{}, errors.New("account admin: 账号档路径不可用（宿主未解析出 accounts.yaml）")
	}
	if admin.Runtime == nil {
		return dto.AccountDetails{}, errors.New("account admin: runtime 不可用")
	}
	record, err := config.UpdateAccount(admin.Path, config.UpdateAccountInput{
		Name:            update.Name,
		Provider:        update.Provider,
		Model:           update.Model,
		BaseURL:         update.BaseURL,
		APIKey:          update.APIKey,
		ReasoningEffort: update.ReasoningEffort,
	})
	if err != nil {
		return dto.AccountDetails{}, err
	}
	if err := admin.hotReplace(record.Name); err != nil {
		return accountDetailsDTO(record), err
	}
	return accountDetailsDTO(record), nil
}

// DeleteAccount 先删账号档，再从运行时退租。账号仍在承流时池会拒绝退租，此时
// 文件已经删除，错误明确要求重启（不能让界面以为删除没发生）。
func (admin AccountAdmin) DeleteAccount(name string) (dto.AccountDetails, error) {
	if strings.TrimSpace(admin.Path) == "" {
		return dto.AccountDetails{}, errors.New("account admin: 账号档路径不可用（宿主未解析出 accounts.yaml）")
	}
	if admin.Runtime == nil {
		return dto.AccountDetails{}, errors.New("account admin: runtime 不可用")
	}
	record, err := config.DeleteAccount(admin.Path, name)
	if err != nil {
		return dto.AccountDetails{}, err
	}
	if err := admin.Runtime.UnregisterAccount(record.Name); err != nil {
		return accountDetailsDTO(record), fmt.Errorf("account admin: 账号档已删除 %q，但运行时仍在承流（重启后生效）: %w", record.Name, err)
	}
	return accountDetailsDTO(record), nil
}

func (admin AccountAdmin) hotReplace(name string) error {
	loaded, err := config.Load(admin.Path)
	if err != nil {
		return fmt.Errorf("account admin: 账号档已更新，但读回失败（重启后生效）: %w", err)
	}
	spec, ok := specNamed(loaded.Specs, name)
	if !ok {
		return fmt.Errorf("account admin: 账号档已更新 %q，但加载器没有产出这条账号（重启后请检查账号档）", name)
	}
	if err := admin.Runtime.ReplaceAccount(spec, loaded.Limits[name]); err != nil {
		return fmt.Errorf("account admin: 账号档已更新 %q，但热替换失败（重启后生效）: %w", name, err)
	}
	return nil
}

func accountDetailsDTO(record config.AccountRecord) dto.AccountDetails {
	return dto.AccountDetails{
		Name: record.Name, Role: record.Role, Provider: record.Provider, Model: record.Model,
		BaseURL: record.BaseURL, ReasoningEffort: record.ReasoningEffort, APIKeySet: record.APIKeySet,
	}
}

// specNamed 按名字取加载结果里的规格（名字由加载器推导，是唯一身份）。
func specNamed(specs []model.AccountSpec, name string) (model.AccountSpec, bool) {
	for _, spec := range specs {
		if spec.Name == name {
			return spec, true
		}
	}
	return model.AccountSpec{}, false
}
