package teamwork

// plan_query.go 收纳**计划查询**的两条判据（P21-33「域方法随类型走」：从
// `backend/engine/assemble/runtime_teamwork.go` 的两个调用点搬进本域）。
//
// 为什么要收口：工作项查找本来就有**唯一一份**（`items.go` 的 `findItem`，收尾/验收/失败
// 全走它）；派发回执那边却另写了一次"遍历里程碑找 item.Role"的循环。两次遍历口径略不同
//（派发那一份会 trim 入参），迟早漂移成"查得到与查不到"两套答案。现在查询只在这里。

import (
	storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"
	"strings"
)

// ItemRole 返回计划里某工作项的执行角色（按 id 查；入参 trim 后比较）。
//
// 找不到 / id 为空 / 计划里没这个工作项 ⇒ 空串（调用方据此"不按角色算读数"，不猜）。
func ItemRole(plan storeteamwork.Plan, itemID string) string {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return ""
	}
	_, item, ok := findItem(&plan, itemID)
	if !ok {
		return ""
	}
	return item.Role
}

// MemberOf 返回计划里某角色的在编成员（按角色名查；入参 trim 后比较）。
//
// 找不到 ⇒ ok=false（调用方据此让那一格读数留空，而不是对一个人给出别人的读数）。
func MemberOf(plan storeteamwork.Plan, role string) (storeteamwork.Member, bool) {
	role = strings.TrimSpace(role)
	if role == "" {
		return storeteamwork.Member{}, false
	}
	for _, member := range plan.Members {
		if member.Role == role {
			return member, true
		}
	}
	return storeteamwork.Member{}, false
}
