package core

import (
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// agentteam_role_index.go — "角色会话 → 归属主会话 + 权责"的反向索引。
//
// 为什么需要它：权限门只拿到"谁在调用"（角色会话号）就要判定主体类，而注册表是
// 按**主会话**存的（sessionstore: <session>/team/roles.json）。权限门此前的读面
// 锚在"当前视图会话"上（main.go 的 SetRoleSessionPolicyResolver 用
// app.Snapshot().Session.ID），于是**后台会话**的角色会话一律解析不到 → 落回 root
// = 不拦。权限面不该取决于"用户此刻正看着哪个会话"。
//
// 这里把"注册表读到的角色会话 → (主会话, 角色名, ToolsPolicy)"记在进程内，任何
// 持有角色会话号的一方（权限门、审计、未来的角色回合执行体）都能问出归属。
//
// 歧义口径（2026-10-01 从"待消除"变为"已消除"）：角色会话号此前是
// `teamID-roleName`，**不含主会话身份**——两个主会话若用了同一个 team_id，就会出现
// "同一个角色会话号、两套权责"。当时这里取**最严**的一个兜底（fail-closed）。
// 现在主会话身份已经编进角色会话号（`agentteam.RoleSessionID(mainSessionID, ...)`），
// 这条歧义在**构造上**不存在了；`byRole` 仍是切片形态 + 最严口径，因为它同时还承担
// "同一 (主会话, 角色) 改名/改权责后以最新读到的为准"的覆盖语义，多候选只在陈旧读
// 与最新读短暂并存时出现——那种情况下取最严依然是正确的方向。

type roleSessionBinding struct {
	mainSessionID string
	roleName      string
	toolsPolicy   string
}

type roleSessionIndex struct {
	mu     sync.RWMutex
	byRole map[string][]roleSessionBinding
}

// remember 记下一次"注册表读到的事实"。同一 (主会话, 角色) 覆盖旧值（角色改名/
// 改权责后以最新读到的为准），不同主会话的同名角色会话并存（供最严口径投票）。
func (index *roleSessionIndex) remember(mainSessionID string, view dto.TeamView) {
	if index == nil {
		return
	}
	mainSessionID = strings.TrimSpace(mainSessionID)
	if mainSessionID == "" || !view.Configured {
		return
	}
	members := make([]dto.TeamMember, 0, len(view.Members)+len(view.Scheduled))
	members = append(members, view.Members...)
	members = append(members, view.Scheduled...)

	index.mu.Lock()
	defer index.mu.Unlock()
	if index.byRole == nil {
		index.byRole = make(map[string][]roleSessionBinding)
	}
	for _, member := range members {
		roleSessionID := strings.TrimSpace(member.RoleSessionID)
		roleName := strings.TrimSpace(member.RoleName)
		if roleSessionID == "" || roleName == "" {
			continue
		}
		binding := roleSessionBinding{
			mainSessionID: mainSessionID,
			roleName:      roleName,
			toolsPolicy:   effectiveToolsPolicy(member),
		}
		bindings := index.byRole[roleSessionID]
		replaced := false
		for position := range bindings {
			if bindings[position].mainSessionID == mainSessionID && bindings[position].roleName == roleName {
				bindings[position] = binding
				replaced = true
				break
			}
		}
		if !replaced {
			bindings = append(bindings, binding)
		}
		index.byRole[roleSessionID] = bindings
	}
}

// owner 返回该角色会话的归属（多个候选 = 角色会话号跨会话重号，返回全部）。
func (index *roleSessionIndex) owner(roleSessionID string) []roleSessionBinding {
	if index == nil {
		return nil
	}
	index.mu.RLock()
	defer index.mu.RUnlock()
	bindings := index.byRole[strings.TrimSpace(roleSessionID)]
	return append([]roleSessionBinding(nil), bindings...)
}

// policy 返回该角色会话的权责口径（歧义时取最严）。
// found=false = 本进程还没读到过它的归属注册表（调用方按"不是角色会话"处理）。
func (index *roleSessionIndex) policy(roleSessionID string) (string, bool) {
	bindings := index.owner(roleSessionID)
	if len(bindings) == 0 {
		return "", false
	}
	policy := bindings[0].toolsPolicy
	for _, binding := range bindings[1:] {
		policy = mostRestrictiveToolsPolicy(policy, binding.toolsPolicy)
	}
	return policy, true
}

// mostRestrictiveToolsPolicy 取两个口径里更严的一个：readonly < readwrite < 其他。
// 其他（full / inherit(空) / 未识别）在权限面上等价于"继承宿主默认"，比任何显式
// 员工口径都宽，所以只在两个都是"其他"时才胜出。
func mostRestrictiveToolsPolicy(left, right string) string {
	if toolsPolicyRank(right) < toolsPolicyRank(left) {
		return right
	}
	return left
}

// effectiveToolsPolicy 把成员表行折算成**权限面上等价的口径**：显式权限格子非空时
// 按格子折算（有任何非 ro 能力 → readwrite，否则 readonly），否则用登记的
// tools_policy。
//
// 为什么必须折算（不能直接回登记值）：显式装配了权限格子的员工，tools_policy 往往是
// 空的（"继承"）——反查路径拿到空口径会按 root 判，即"前端装配好的权限在反查路径上
// 完全失效"（fail-open）。折算后的口径至少保证"它被当成一个员工"，判定落在共享的
// emp_ro / emp_rw 主体上（**取更严的一档**）。
//
// 边界（诚实标注）：反查路径只拿得到会话号，不知道角色名，因此它只能给出这个粗档位，
// 给不出"这个员工自己那套格子"。逐格权限的权威判定在**按构造**那条路（角色回合起手
// 用 seelebridge/tools.WithEmployeeGrant 把角色名 + 格子放进 ctx），那条路不依赖反查。
func effectiveToolsPolicy(member dto.TeamMember) string {
	if len(member.PermissionGroups) == 0 {
		return member.ToolsPolicy
	}
	for group, bits := range member.PermissionGroups {
		if group == dto.PermissionGroupRO {
			continue
		}
		if bits != 0 {
			return dto.ToolPolicyReadWrite
		}
	}
	return dto.ToolPolicyReadonly
}

func toolsPolicyRank(policy string) int {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "readonly":
		return 0
	case "readwrite":
		return 1
	default:
		return 2
	}
}

// RoleSessionToolsPolicy 回答"这个角色会话是谁的、权责是什么"（权限门读面）。
//
// found=false 表示本进程尚未读到该角色会话的归属注册表。这**不是**"它没有权责"，
// 而是"还不知道"：调用方（权限门）据此落回无角色处理。为了让归属尽早可查，
// 角色回合执行体在起手时应按构造把主体类放进 ctx（seelebridge/tools 的
// WithEmployeeSubjectClass），那条路不依赖任何索引。
func (service *Service) RoleSessionToolsPolicy(roleSessionID string) (string, bool) {
	if service == nil {
		return "", false
	}
	return service.roleSessions.policy(roleSessionID)
}

// RoleSessionOwner 返回角色会话的归属主会话（歧义时返回第一个读到的；仅供诊断/
// 审计展示，权限判定请用 RoleSessionToolsPolicy）。
func (service *Service) RoleSessionOwner(roleSessionID string) (string, bool) {
	if service == nil {
		return "", false
	}
	bindings := service.roleSessions.owner(roleSessionID)
	if len(bindings) == 0 {
		return "", false
	}
	return bindings[0].mainSessionID, true
}
