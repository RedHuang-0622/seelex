// account.go — 账号的界面契约（添加账号 / 查余额）。
//
// 为什么落在 contract/dto：这两个 DTO 要同时穿过三层（GUI 桥 → 应用层用例 →
// 宿主适配器），谁都不该为了它们去依赖别人的实现包。它们是**纯数据**，不含
// 判定：字段校验在账号档写入器（internal/config），provider 口径在账号域
// （backend/engine/account），本层只负责把形状固定下来。
package dto

import "time"

// AccountRoleFromName 从账号 ID 推断角色，只用于兼容尚未携带显式 Role 的旧快照。
// 新快照始终使用 AccountSpec.Role / AccountInfo.role；这里的词表仍来自本包常量，
// 不另造一份前缀表。
func AccountRoleFromName(name string) AccountRole {
	for _, role := range AccountRoleVocabulary() {
		if len(name) > len(role) && name[:len(role)] == role {
			return AccountRole(role)
		}
	}
	return RoleAgent
}

// AccountDraft 是「设置 → 账户 → 手动添加」的输入面。
//
// 口径与 accounts.yaml 的一条账号条目一致：留空的字段即继承 `defaults`
// （Provider/BaseURL/ReasoningEffort 都可留空），所以界面可以只填"角色 + 模型 +
// 密钥"就加出一条可用账号。APIKey 只往宿主方向走，不回传、不落日志。
type AccountDraft struct {
	// Role 决定这条账号落进哪个角色块（agent / subagent / goalplan / teammate），
	// 也决定它生成的名字（`<role>-<序号>`，序号由账号档里已有条目数推导）。
	Role            string `json:"role"`
	Provider        string `json:"provider,omitempty"`
	Model           string `json:"model"`
	BaseURL         string `json:"base_url,omitempty"`
	APIKey          string `json:"api_key"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

// AccountAddResult 是添加成功后的回执：**生效的名字**与这条账号的关键字段。
//
// 名字必须回传：账号名由账号档里的位置推导（`agent-1`、`agent-2`…），界面不能
// 自己猜——猜错了后面"选中账号/改窗口/查余额"全会打到不存在的账号上。
type AccountAddResult struct {
	Name     string `json:"name"`
	Role     string `json:"role"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// AccountBalance 是一个账号此刻的余额读数（界面「账户」列表的逐行读数）。
//
// Available 是供应商自报的"当前是否可用"——额度耗尽时为 false，这是 402
// Insufficient Balance 之前唯一能提前看到的信号，所以它必须独立于金额存在。
type AccountBalance struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	// Infos 按供应商给的顺序；每种币种一条（人民币账户通常只有 CNY）。
	Infos []AccountBalanceInfo `json:"infos,omitempty"`
	// FetchedAt 是这次探测的时刻：余额是**快照**，界面要能说明它是"刚才"的读数。
	FetchedAt time.Time `json:"fetched_at"`
}

// AccountBalanceInfo 是一种币种的余额读数。
//
// 金额保持供应商给的字符串原样（"110.00"）：货币与精度由服务端定，解析成
// float 再格式化回去会把末尾的 0 丢掉，读数就和账单对不上了。
type AccountBalanceInfo struct {
	Currency string `json:"currency"`
	// Total 是总余额；Granted 是赠送余额；ToppedUp 是充值余额。
	Total    string `json:"total"`
	Granted  string `json:"granted,omitempty"`
	ToppedUp string `json:"topped_up,omitempty"`
}

// AccountRoleVocabulary 返回可手动添加的账号角色名（顺序即界面下拉顺序）。
//
// 词表本体就是本包的 AccountRole 常量（见 plan.go）：账号角色是跨层契约
// （internal/model 别名、账号档的角色块、权限的角色名录都用它），所以"有哪些角色"
// 只在这里定一次——界面下拉从宿主读它，不自己写死一份。
func AccountRoleVocabulary() []string {
	return []string{
		string(RoleAgent),
		string(RoleSubAgent),
		string(RoleGoalPlan),
		string(RoleTeammate),
	}
}
